package argusidentity

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/client"
	"go.opentelemetry.io/collector/component"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

const (
	spiffePrefix       = "spiffe://argus/telemetry/collectors/"
	maxIdentityPayload = 96 << 10
)

func collectorDNSName(collectorID string) string {
	return "collector-" + collectorID + ".argus.telemetry"
}

type certificateResult struct {
	CollectorID          string            `json:"collector_id"`
	ClientCertificatePEM string            `json:"client_certificate_pem"`
	ServerCertificatePEM string            `json:"server_certificate_pem"`
	TrustBundle          trustBundleResult `json:"trust_bundle"`
	IngestGRPCEndpoint   string            `json:"ingest_grpc_endpoint"`
	IngestHTTPEndpoint   string            `json:"ingest_http_endpoint"`
	CertificateExpiresAt time.Time         `json:"certificate_expires_at"`
}

type trustBundleResult struct {
	Epoch                 int64     `json:"epoch"`
	State                 string    `json:"state"`
	BundlePEM             string    `json:"bundle_pem"`
	BundleSHA256          string    `json:"bundle_sha256"`
	CurrentCAFingerprints []string  `json:"current_ca_fingerprints"`
	NextCAFingerprints    []string  `json:"next_ca_fingerprints"`
	StartedAt             time.Time `json:"started_at"`
	RetireAt              time.Time `json:"retire_at"`
}

type trustBundleState struct {
	Epoch          int64    `json:"epoch"`
	BundleSHA256   string   `json:"bundle_sha256"`
	CAFingerprints []string `json:"ca_fingerprints"`
}

type identityExtension struct {
	config Config
	logger *zap.Logger
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

type peerAuthData struct {
	collectorID string
	serial      string
}

func (data peerAuthData) GetAttribute(key string) any {
	switch key {
	case "argus.telemetry.collector_id":
		return data.collectorID
	case "argus.telemetry.certificate_serial":
		return data.serial
	default:
		return nil
	}
}

func (peerAuthData) GetAttributeNames() []string {
	return []string{"argus.telemetry.collector_id", "argus.telemetry.certificate_serial"}
}

func (extension *identityExtension) Authenticate(ctx context.Context, _ map[string][]string) (context.Context, error) {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return nil, errors.New("telemetry peer identity is missing")
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) != 1 {
		return nil, errors.New("telemetry peer certificate is required")
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	if len(certificate.URIs) != 1 || !strings.HasPrefix(certificate.URIs[0].String(), spiffePrefix) || certificate.SerialNumber == nil {
		return nil, errors.New("telemetry peer certificate identity is invalid")
	}
	collectorID := strings.TrimPrefix(certificate.URIs[0].String(), spiffePrefix)
	if collectorID == "" || strings.Contains(collectorID, "/") {
		return nil, errors.New("telemetry peer Collector identity is invalid")
	}
	info := client.FromContext(ctx)
	info.Auth = peerAuthData{collectorID: collectorID, serial: strings.ToLower(certificate.SerialNumber.Text(16))}
	return client.NewContext(ctx, info), nil
}

func newIdentityExtension(config Config, logger *zap.Logger) *identityExtension {
	return &identityExtension{config: config, logger: logger, done: make(chan struct{})}
}

func (extension *identityExtension) Start(ctx context.Context, _ component.Host) error {
	if err := bootstrapIdentity(extension.config); err != nil {
		return err
	}
	if err := extension.ensureIdentity(ctx); err != nil {
		return err
	}
	background, cancel := context.WithCancel(context.Background())
	extension.cancel = cancel
	go extension.rotationLoop(background)
	return nil
}

func (extension *identityExtension) Shutdown(context.Context) error {
	if extension.cancel == nil {
		return nil
	}
	extension.cancel()
	<-extension.done
	return nil
}

func (extension *identityExtension) rotationLoop(ctx context.Context) {
	defer close(extension.done)
	ticker := time.NewTicker(extension.config.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := extension.ensureIdentity(ctx); err != nil {
				extension.logger.Error("telemetry certificate rotation failed", zap.Error(err))
			}
		}
	}
}

func (extension *identityExtension) ensureIdentity(ctx context.Context) error {
	extension.mu.Lock()
	defer extension.mu.Unlock()

	certificate, clientErr := loadCertificate(extension.config.CertificateFile)
	serverCertificate, serverErr := loadCertificate(extension.config.ServerCertificateFile)
	if clientErr == nil && serverErr == nil && time.Until(certificate.NotAfter) > extension.config.RotateBefore &&
		time.Until(serverCertificate.NotAfter) > extension.config.RotateBefore {
		if syncErr := extension.syncTrustBundle(ctx); syncErr != nil {
			// Preserve the last validated material and retry on the next interval.
			extension.logger.Error("telemetry Trust Bundle synchronization failed", zap.Error(syncErr))
		} else if mirrorErr := mirrorKubernetesIdentity(ctx, extension.config); mirrorErr != nil {
			return mirrorErr
		}
		return nil
	}
	clientKey, clientKeyPEM, err := loadOrCreatePrivateKey(extension.config.PrivateKeyFile)
	if err != nil {
		return err
	}
	serverKey, serverKeyPEM, err := loadOrCreatePrivateKey(extension.config.ServerPrivateKeyFile)
	if err != nil {
		return err
	}
	clientCSR, err := createCSR(extension.config.CollectorID, clientKey, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return err
	}
	serverCSR, err := createCSR(extension.config.CollectorID, serverKey, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return err
	}
	var result certificateResult
	if certificate == nil {
		result, err = extension.enroll(ctx, clientCSR, serverCSR)
	} else {
		result, err = extension.rotate(ctx, clientCSR, serverCSR, clientKeyPEM)
	}
	if err != nil {
		return err
	}
	bundle, err := validateResult(result, extension.config.CollectorID, clientKey, serverKey, time.Now())
	if err != nil {
		return err
	}
	if err = writeIdentityFiles(extension.config, clientKeyPEM, serverKeyPEM, result, bundle); err != nil {
		return err
	}
	if certificate == nil {
		if err = consumeTokenFile(extension.config.EnrollmentTokenFile); err != nil {
			return err
		}
	}
	if syncErr := extension.syncTrustBundle(ctx); syncErr != nil {
		extension.logger.Error("telemetry Trust Bundle acknowledgement failed", zap.Error(syncErr))
	}
	if mirrorErr := mirrorKubernetesIdentity(ctx, extension.config); mirrorErr != nil {
		return mirrorErr
	}
	extension.logger.Info("telemetry collector identity ready", zap.String("collector_id", extension.config.CollectorID), zap.Time("expires_at", result.CertificateExpiresAt))
	return nil
}

func (extension *identityExtension) enroll(ctx context.Context, clientCSR, serverCSR []byte) (certificateResult, error) {
	token, err := os.ReadFile(extension.config.EnrollmentTokenFile)
	if err != nil || strings.TrimSpace(string(token)) == "" {
		return certificateResult{}, errors.New("telemetry enrollment token is unavailable")
	}
	body, _ := json.Marshal(map[string]string{"collector_id": extension.config.CollectorID,
		"client_csr_pem": string(clientCSR), "server_csr_pem": string(serverCSR)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, extension.config.EnrollmentEndpoint, bytes.NewReader(body))
	if err != nil {
		return certificateResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Argus-Telemetry-Enrollment-Token", strings.TrimSpace(string(token)))
	return extension.do(request, nil)
}

func (extension *identityExtension) rotate(ctx context.Context, clientCSR, serverCSR, keyPEM []byte) (certificateResult, error) {
	body, _ := json.Marshal(map[string]string{"client_csr_pem": string(clientCSR), "server_csr_pem": string(serverCSR)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, extension.config.RotationEndpoint, bytes.NewReader(body))
	if err != nil {
		return certificateResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	certificatePEM, err := os.ReadFile(extension.config.CertificateFile)
	if err != nil {
		return certificateResult{}, err
	}
	clientCertificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		return certificateResult{}, err
	}
	return extension.do(request, &clientCertificate)
}

func (extension *identityExtension) do(request *http.Request, certificate *tls.Certificate) (certificateResult, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	if certificate != nil {
		tlsConfig.Certificates = []tls.Certificate{*certificate}
	}
	caPath := extension.config.ServerCAFile
	if certificate != nil {
		caPath = extension.config.CABundleFile
	}
	if caPath != "" {
		value, err := os.ReadFile(caPath)
		if err != nil {
			return certificateResult{}, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(value) {
			return certificateResult{}, errors.New("telemetry server CA is invalid")
		}
		tlsConfig.RootCAs = pool
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	if extension.config.DialAddress != "" {
		dialAddress := extension.config.DialAddress
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, dialAddress)
		}
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("telemetry identity redirects are forbidden")
		}}
	response, err := client.Do(request)
	if err != nil {
		return certificateResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return certificateResult{}, fmt.Errorf("telemetry identity endpoint returned %d", response.StatusCode)
	}
	var result certificateResult
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxIdentityPayload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return certificateResult{}, err
	}
	return result, nil
}

func (extension *identityExtension) syncTrustBundle(ctx context.Context) error {
	state, err := loadTrustBundleState(extension.config.TrustBundleStateFile, extension.config.CABundleFile)
	if err != nil {
		return err
	}
	certificatePEM, err := os.ReadFile(extension.config.CertificateFile)
	if err != nil {
		return err
	}
	privateKeyPEM, err := os.ReadFile(extension.config.PrivateKeyFile)
	if err != nil {
		return err
	}
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return err
	}
	updated, current, err := extension.reportTrustBundle(ctx, certificate, state)
	if err != nil || current {
		return err
	}
	bundle, err := validateTrustBundleResult(updated, time.Now().UTC())
	if err != nil {
		return err
	}
	// The CA file is written first. If the process is interrupted before the
	// state write, the mismatch is detected and no false ACK is sent.
	if err = writeAtomic(extension.config.CABundleFile, bundle.Material.PEM, 0o600); err != nil {
		return err
	}
	state = trustBundleState{Epoch: bundle.Epoch, BundleSHA256: bundle.Material.SHA256, CAFingerprints: bundle.Material.Fingerprints}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = writeAtomic(extension.config.TrustBundleStateFile, encoded, 0o600); err != nil {
		return err
	}
	_, current, err = extension.reportTrustBundle(ctx, certificate, state)
	if err != nil {
		return err
	}
	if !current {
		return errors.New("telemetry Trust Bundle acknowledgement was not accepted")
	}
	return nil
}

func (extension *identityExtension) reportTrustBundle(ctx context.Context, certificate tls.Certificate, state trustBundleState) (trustBundleResult, bool, error) {
	body, _ := json.Marshal(state)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, extension.config.TrustBundleEndpoint, bytes.NewReader(body))
	if err != nil {
		return trustBundleResult{}, false, err
	}
	request.Header.Set("Content-Type", "application/json")
	caPEM, err := os.ReadFile(extension.config.CABundleFile)
	if err != nil {
		return trustBundleResult{}, false, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return trustBundleResult{}, false, errors.New("telemetry runtime Trust Bundle is invalid")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{certificate}}}
	if extension.config.DialAddress != "" {
		dialAddress := extension.config.DialAddress
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, dialAddress)
		}
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("telemetry identity redirects are forbidden")
	}}
	response, err := client.Do(request)
	if err != nil {
		return trustBundleResult{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return trustBundleResult{}, true, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return trustBundleResult{}, false, fmt.Errorf("telemetry Trust Bundle endpoint returned %d", response.StatusCode)
	}
	var result trustBundleResult
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxIdentityPayload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return trustBundleResult{}, false, err
	}
	return result, false, nil
}

func loadTrustBundleState(statePath, caPath string) (trustBundleState, error) {
	var state trustBundleState
	encoded, err := os.ReadFile(statePath)
	if err != nil || json.Unmarshal(encoded, &state) != nil || state.Epoch < 1 {
		return trustBundleState{}, errors.New("telemetry Trust Bundle state is invalid")
	}
	value, err := os.ReadFile(caPath)
	if err != nil {
		return trustBundleState{}, err
	}
	material, err := parseTrustBundle(value, time.Now().UTC())
	if err != nil || material.SHA256 != state.BundleSHA256 || !slices.Equal(material.Fingerprints, state.CAFingerprints) {
		return trustBundleState{}, errors.New("telemetry Trust Bundle state does not match CA file")
	}
	return state, nil
}

func loadOrCreatePrivateKey(path string) (*ecdsa.PrivateKey, []byte, error) {
	value, err := os.ReadFile(path)
	if err == nil {
		block, rest := pem.Decode(value)
		if block == nil || len(rest) != 0 || block.Type != "EC PRIVATE KEY" {
			return nil, nil, errors.New("telemetry private key is invalid")
		}
		key, parseErr := x509.ParseECPrivateKey(block.Bytes)
		return key, value, parseErr
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded}), nil
}

func createCSR(collectorID string, key *ecdsa.PrivateKey, usage x509.ExtKeyUsage) ([]byte, error) {
	identity := spiffePrefix + collectorID
	dnsNames := []string{}
	if usage == x509.ExtKeyUsageServerAuth {
		identity = "spiffe://argus/telemetry/collector-servers/" + collectorID
		dnsNames = []string{collectorDNSName(collectorID)}
	} else if usage != x509.ExtKeyUsageClientAuth {
		return nil, errors.New("telemetry certificate usage is invalid")
	}
	identityURI, err := url.Parse(identity)
	if err != nil {
		return nil, err
	}
	value, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "argus-telemetry-collector"}, URIs: []*url.URL{identityURI}, DNSNames: dnsNames,
	}, key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: value}), nil
}

func loadCertificate(path string) (*x509.Certificate, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(value)
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE" {
		return nil, errors.New("telemetry certificate is invalid")
	}
	return x509.ParseCertificate(block.Bytes)
}

func validateResult(result certificateResult, collectorID string, clientKey, serverKey *ecdsa.PrivateKey, now time.Time) (bundleSnapshot, error) {
	if result.CollectorID != collectorID || result.ClientCertificatePEM == "" || result.ServerCertificatePEM == "" || !result.CertificateExpiresAt.After(now) {
		return bundleSnapshot{}, errors.New("telemetry identity result is incomplete")
	}
	bundle, err := validateTrustBundleResult(result.TrustBundle, now)
	if err != nil {
		return bundleSnapshot{}, err
	}
	if err = validateIdentityCertificate(result.ClientCertificatePEM, collectorID, clientKey, bundle, x509.ExtKeyUsageClientAuth, now); err != nil {
		return bundleSnapshot{}, err
	}
	if err = validateIdentityCertificate(result.ServerCertificatePEM, collectorID, serverKey, bundle, x509.ExtKeyUsageServerAuth, now); err != nil {
		return bundleSnapshot{}, err
	}
	return bundle, nil
}

func validateIdentityCertificate(value, collectorID string, key *ecdsa.PrivateKey, bundle bundleSnapshot, usage x509.ExtKeyUsage, now time.Time) error {
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE" {
		return errors.New("telemetry identity certificate is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(certificate.URIs) != 1 || certificate.NotAfter.Before(now) || len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != usage {
		return errors.New("telemetry identity certificate does not match Collector")
	}
	if usage == x509.ExtKeyUsageClientAuth && (certificate.URIs[0].String() != spiffePrefix+collectorID || len(certificate.DNSNames) != 0) {
		return errors.New("telemetry client certificate identity is invalid")
	}
	if usage == x509.ExtKeyUsageServerAuth && (certificate.URIs[0].String() != "spiffe://argus/telemetry/collector-servers/"+collectorID ||
		len(certificate.DNSNames) != 1 || certificate.DNSNames[0] != collectorDNSName(collectorID)) {
		return errors.New("telemetry server certificate identity is invalid")
	}
	public, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || public.X.Cmp(key.PublicKey.X) != 0 || public.Y.Cmp(key.PublicKey.Y) != 0 {
		return errors.New("telemetry identity certificate key mismatch")
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(bundle.Material.PEM)
	options := x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{usage}, CurrentTime: now}
	if usage == x509.ExtKeyUsageServerAuth {
		options.DNSName = collectorDNSName(collectorID)
	}
	if _, err = certificate.Verify(options); err != nil {
		return errors.New("telemetry identity certificate chain or EKU is invalid")
	}
	return nil
}

func validateTrustBundleResult(value trustBundleResult, now time.Time) (bundleSnapshot, error) {
	material, err := parseTrustBundle([]byte(value.BundlePEM), now)
	if err != nil || value.Epoch < 1 || value.StartedAt.IsZero() || len(value.CurrentCAFingerprints) == 0 {
		return bundleSnapshot{}, errors.New("telemetry Trust Bundle metadata is invalid")
	}
	bundle := bundleSnapshot{Epoch: value.Epoch, State: value.State, Material: material,
		CurrentCAFingerprints: value.CurrentCAFingerprints, NextCAFingerprints: value.NextCAFingerprints,
		StartedAt: value.StartedAt, RetireAt: value.RetireAt}
	all := append(append([]string{}, value.CurrentCAFingerprints...), value.NextCAFingerprints...)
	if !bundleMatches(bundle, value.Epoch, value.BundleSHA256, all) ||
		(value.State != bundleStateStable && value.State != bundleStatePreparing && value.State != bundleStateOverlapping && value.State != bundleStateRetiring) ||
		((value.State == bundleStateOverlapping || value.State == bundleStateRetiring) && value.RetireAt.IsZero()) {
		return bundleSnapshot{}, errors.New("telemetry Trust Bundle digest, fingerprints, or state is invalid")
	}
	return bundle, nil
}

func writeIdentityFiles(config Config, clientKeyPEM, serverKeyPEM []byte, result certificateResult, bundle bundleSnapshot) error {
	for _, path := range []string{config.CertificateFile, config.PrivateKeyFile, config.ServerCertificateFile, config.ServerPrivateKeyFile, config.CABundleFile, config.TrustBundleStateFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
	}
	if err := writeAtomic(config.PrivateKeyFile, clientKeyPEM, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(config.ServerPrivateKeyFile, serverKeyPEM, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(config.CABundleFile, bundle.Material.PEM, 0o600); err != nil {
		return err
	}
	state, err := json.Marshal(trustBundleState{Epoch: bundle.Epoch, BundleSHA256: bundle.Material.SHA256, CAFingerprints: bundle.Material.Fingerprints})
	if err != nil {
		return err
	}
	if err := writeAtomic(config.TrustBundleStateFile, state, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(config.ServerCertificateFile, []byte(result.ServerCertificatePEM), 0o600); err != nil {
		return err
	}
	return writeAtomic(config.CertificateFile, []byte(result.ClientCertificatePEM), 0o600)
}

func consumeTokenFile(path string) error {
	if err := os.WriteFile(path, nil, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Remove(path)
}

func writeAtomic(path string, value []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".argus-identity-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(value)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
