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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	CollectorID          string    `json:"collector_id"`
	CertificatePEM       string    `json:"certificate_pem"`
	CABundlePEM          string    `json:"ca_bundle_pem"`
	IngestGRPCEndpoint   string    `json:"ingest_grpc_endpoint"`
	IngestHTTPEndpoint   string    `json:"ingest_http_endpoint"`
	CertificateExpiresAt time.Time `json:"certificate_expires_at"`
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

	certificate, err := loadCertificate(extension.config.CertificateFile)
	if err == nil && time.Until(certificate.NotAfter) > extension.config.RotateBefore {
		return nil
	}
	key, keyPEM, err := loadOrCreatePrivateKey(extension.config.PrivateKeyFile)
	if err != nil {
		return err
	}
	csr, err := createCSR(extension.config.CollectorID, key)
	if err != nil {
		return err
	}
	var result certificateResult
	if certificate == nil {
		result, err = extension.enroll(ctx, csr)
	} else {
		result, err = extension.rotate(ctx, csr, keyPEM)
	}
	if err != nil {
		return err
	}
	if err = validateResult(result, extension.config.CollectorID, key, time.Now()); err != nil {
		return err
	}
	if err = writeIdentityFiles(extension.config, keyPEM, result); err != nil {
		return err
	}
	if certificate == nil {
		if err = consumeTokenFile(extension.config.EnrollmentTokenFile); err != nil {
			return err
		}
	}
	extension.logger.Info("telemetry collector identity ready", zap.String("collector_id", extension.config.CollectorID), zap.Time("expires_at", result.CertificateExpiresAt))
	return nil
}

func (extension *identityExtension) enroll(ctx context.Context, csr []byte) (certificateResult, error) {
	token, err := os.ReadFile(extension.config.EnrollmentTokenFile)
	if err != nil || strings.TrimSpace(string(token)) == "" {
		return certificateResult{}, errors.New("telemetry enrollment token is unavailable")
	}
	body, _ := json.Marshal(map[string]string{"collector_id": extension.config.CollectorID, "csr_pem": string(csr)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, extension.config.EnrollmentEndpoint, bytes.NewReader(body))
	if err != nil {
		return certificateResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Argus-Telemetry-Enrollment-Token", strings.TrimSpace(string(token)))
	return extension.do(request, nil)
}

func (extension *identityExtension) rotate(ctx context.Context, csr, keyPEM []byte) (certificateResult, error) {
	body, _ := json.Marshal(map[string]string{"csr_pem": string(csr)})
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
	if extension.config.ServerCAFile != "" {
		value, err := os.ReadFile(extension.config.ServerCAFile)
		if err != nil {
			return certificateResult{}, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(value) {
			return certificateResult{}, errors.New("telemetry server CA is invalid")
		}
		tlsConfig.RootCAs = pool
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig},
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

func createCSR(collectorID string, key *ecdsa.PrivateKey) ([]byte, error) {
	identityURI, err := url.Parse(spiffePrefix + collectorID)
	if err != nil {
		return nil, err
	}
	value, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "argus-telemetry-collector"}, URIs: []*url.URL{identityURI}, DNSNames: []string{collectorDNSName(collectorID)},
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

func validateResult(result certificateResult, collectorID string, key *ecdsa.PrivateKey, now time.Time) error {
	if result.CollectorID != collectorID || result.CertificatePEM == "" || result.CABundlePEM == "" || !result.CertificateExpiresAt.After(now) {
		return errors.New("telemetry identity result is incomplete")
	}
	block, rest := pem.Decode([]byte(result.CertificatePEM))
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE" {
		return errors.New("telemetry identity certificate is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(certificate.URIs) != 1 || len(certificate.DNSNames) != 1 ||
		certificate.URIs[0].String() != spiffePrefix+collectorID || certificate.DNSNames[0] != collectorDNSName(collectorID) || certificate.NotAfter.Before(now) {
		return errors.New("telemetry identity certificate does not match Collector")
	}
	public, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || public.X.Cmp(key.PublicKey.X) != 0 || public.Y.Cmp(key.PublicKey.Y) != 0 {
		return errors.New("telemetry identity certificate key mismatch")
	}
	if pool := x509.NewCertPool(); !pool.AppendCertsFromPEM([]byte(result.CABundlePEM)) {
		return errors.New("telemetry identity CA bundle is invalid")
	}
	return nil
}

func writeIdentityFiles(config Config, keyPEM []byte, result certificateResult) error {
	for _, path := range []string{config.CertificateFile, config.PrivateKeyFile, config.CABundleFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
	}
	if err := writeAtomic(config.PrivateKeyFile, keyPEM, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(config.CABundleFile, []byte(result.CABundlePEM), 0o600); err != nil {
		return err
	}
	return writeAtomic(config.CertificateFile, []byte(result.CertificatePEM), 0o600)
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
