package connector

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	connectorcore "github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/tlsmaterial"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

type enrollOptions struct {
	Server, ConnectorID, Token, Role, Name, DataDirectory string
	CAFile, InstanceID                                    string
	Capabilities                                          []string
}

type enrollResult struct {
	ConnectorID          string            `json:"connector_id"`
	Role                 string            `json:"role"`
	CertificatePEM       string            `json:"certificate_pem"`
	TrustBundle          enrollTrustBundle `json:"trust_bundle"`
	GatewayEndpoint      string            `json:"gateway_endpoint"`
	CertificateExpiresAt time.Time         `json:"certificate_expires_at"`
	Result               string            `json:"result"`
}

type enrollTrustBundle struct {
	Epoch                 int64     `json:"epoch"`
	State                 string    `json:"state"`
	BundlePEM             string    `json:"bundle_pem"`
	BundleSHA256          string    `json:"bundle_sha256"`
	CurrentCAFingerprints []string  `json:"current_ca_fingerprints"`
	NextCAFingerprints    []string  `json:"next_ca_fingerprints"`
	StartedAt             time.Time `json:"started_at"`
	RetireAt              time.Time `json:"retire_at"`
}

func enroll(ctx context.Context, options enrollOptions) (enrollResult, error) {
	id, err := uuid.Parse(options.ConnectorID)
	if err != nil {
		return enrollResult{}, errors.New("Connector ID must be a UUID")
	}
	identity, err := connectorcore.GenerateLocalIdentity(options.DataDirectory, id)
	if err != nil {
		return enrollResult{}, err
	}
	capabilities := append([]string(nil), options.Capabilities...)
	if len(capabilities) == 0 {
		capabilities = roleCapabilities(options.Role)
	}
	localInstanceID := options.InstanceID
	if localInstanceID == "" {
		localInstanceID = instanceID()
	}
	architecture := connectorArchitecture()
	if architecture == "" {
		return enrollResult{}, errors.New("Connector architecture is unsupported")
	}
	payload := map[string]any{"csr_pem": identity.CSRPEM, "device_fingerprint": deviceFingerprint(), "instance_id": localInstanceID, "architecture": architecture,
		"name": options.Name, "software_version": softwareVersion, "capabilities": capabilities}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return enrollResult{}, err
	}
	endpoint, err := enrollmentEndpoint(options.Server)
	if err != nil {
		return enrollResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return enrollResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Argus-Enrollment-Token", options.Token)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	caFile := options.CAFile
	if caFile == "" {
		caFile = os.Getenv("ARGUS_CONNECTOR_CA_FILE")
	}
	if caFile == "" {
		return enrollResult{}, errors.New("Connector CA file is required")
	}
	tlsMaterial, err := tlsmaterial.Load(tlsmaterial.Options{CABundlePath: caFile})
	if err != nil {
		return enrollResult{}, fmt.Errorf("load Connector enrollment Trust Bundle: %w", err)
	}
	base := (*http.Transport)(nil)
	if dialAddress := os.Getenv("ARGUS_CONNECTOR_ENROLL_ADDRESS"); dialAddress != "" {
		base = pinnedAddressTransport(dialAddress)
	}
	transport, err := tlsmaterial.NewHTTPTransport(tlsMaterial, base)
	if err != nil {
		return enrollResult{}, err
	}
	client := &http.Client{Timeout: 45 * time.Second, Transport: transport}
	response, err := client.Do(request)
	if err != nil {
		return enrollResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return enrollResult{}, errors.New("Connector enrollment response is invalid")
	}
	if response.StatusCode != http.StatusCreated {
		return enrollResult{}, fmt.Errorf("Connector enrollment failed with HTTP %d", response.StatusCode)
	}
	var result enrollResult
	if err := json.Unmarshal(body, &result); err != nil || result.ConnectorID != id.String() || result.Role != options.Role ||
		result.CertificatePEM == "" || result.TrustBundle.BundlePEM == "" || result.GatewayEndpoint == "" {
		return enrollResult{}, errors.New("Connector enrollment returned incomplete identity material")
	}
	bundle, err := validateEnrollmentTrustBundle(result.TrustBundle)
	if err != nil {
		return enrollResult{}, err
	}
	keyPEM, err := connectorcore.MarshalPrivateKey(identity.PrivateKey)
	if err != nil {
		return enrollResult{}, err
	}
	if err := validateIssuedIdentity(id, keyPEM, []byte(result.CertificatePEM), bundle.Material.PEM); err != nil {
		return enrollResult{}, err
	}
	state := identityState{ConnectorID: id.String(), Role: result.Role, InstanceID: localInstanceID, Name: options.Name,
		GatewayEndpoint: result.GatewayEndpoint, CertificateExpiresAt: result.CertificateExpiresAt, Capabilities: capabilities,
		TrustBundleEpoch: bundle.Epoch, TrustBundleSHA256: bundle.Material.SHA256, TrustCAFingerprints: bundle.Material.Fingerprints}
	if err := (localStore{directory: options.DataDirectory}).saveIdentity(state, keyPEM, []byte(result.CertificatePEM), bundle.Material.PEM); err != nil {
		return enrollResult{}, err
	}
	return result, nil
}

func validateEnrollmentTrustBundle(value enrollTrustBundle) (trustbundle.Bundle, error) {
	if value.Epoch < 1 || value.StartedAt.IsZero() || value.State == trustbundle.StateFailed ||
		(value.State != trustbundle.StateStable && value.State != trustbundle.StatePreparing && value.State != trustbundle.StateOverlapping && value.State != trustbundle.StateRetiring) {
		return trustbundle.Bundle{}, errors.New("Connector enrollment Trust Bundle metadata is invalid")
	}
	if (value.State == trustbundle.StateOverlapping || value.State == trustbundle.StateRetiring) && value.RetireAt.IsZero() {
		return trustbundle.Bundle{}, errors.New("Connector enrollment Trust Bundle retirement deadline is invalid")
	}
	material, err := trustbundle.Parse([]byte(value.BundlePEM), time.Now().UTC())
	if err != nil {
		return trustbundle.Bundle{}, err
	}
	bundle := trustbundle.Bundle{Epoch: value.Epoch, State: value.State, Material: material,
		CurrentCAFingerprints: value.CurrentCAFingerprints, NextCAFingerprints: value.NextCAFingerprints,
		StartedAt: value.StartedAt, RetireAt: value.RetireAt}
	all := append(append([]string{}, value.CurrentCAFingerprints...), value.NextCAFingerprints...)
	if len(value.CurrentCAFingerprints) == 0 || !bundle.Matches(trustbundle.Acknowledgement{Epoch: value.Epoch,
		SHA256: value.BundleSHA256, Fingerprints: all}) {
		return trustbundle.Bundle{}, errors.New("Connector enrollment Trust Bundle digest or fingerprints are invalid")
	}
	return bundle, nil
}

func enrollmentEndpoint(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("Connector enrollment URL must be HTTP or HTTPS")
	}
	if parsed.Scheme != "https" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" {
		return "", errors.New("Connector enrollment requires HTTPS outside loopback development")
	}
	if !strings.HasSuffix(parsed.Path, "/api/v1/connectors/enroll") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/v1/connectors/enroll"
	}
	return parsed.String(), nil
}

func roleCapabilities(role string) []string {
	values := []string{"kubernetes.connection_probe", "kubernetes.query", "credential.lease", "connector.uninstall"}
	if role == "bastion" {
		values = append([]string{"host.connection_probe"}, values...)
	}
	return values
}

func instanceID() string {
	value := os.Getenv("ARGUS_CONNECTOR_INSTANCE_ID")
	if value == "" {
		value = hostname()
	}
	return value
}

func connectorArchitecture() string {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH
	default:
		return ""
	}
}

func deviceFingerprint() string {
	parts := []string{hostname()}
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if value, err := os.ReadFile(path); err == nil {
			parts = append(parts, strings.TrimSpace(string(value)))
			break
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func validateIssuedIdentity(connectorID uuid.UUID, keyPEM, certificatePEM, caPEM []byte) error {
	keyBlock, _ := pem.Decode(keyPEM)
	certificateBlock, _ := pem.Decode(certificatePEM)
	if keyBlock == nil || certificateBlock == nil {
		return errors.New("Connector identity PEM is invalid")
	}
	keyValue, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return err
	}
	signer, ok := keyValue.(crypto.Signer)
	if !ok {
		return errors.New("Connector private key does not implement signing")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil || len(certificate.URIs) != 1 || certificate.URIs[0].String() != connectorcore.CertificateURI(connectorID) {
		return errors.New("Connector certificate identity is invalid")
	}
	publicKey, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return err
	}
	certificateKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil || !bytes.Equal(publicKey, certificateKey) {
		return errors.New("Connector certificate does not match its private key")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return errors.New("Connector CA bundle is invalid")
	}
	_, err = certificate.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return err
}

// pinnedAddressTransport only overrides the TCP target. The request URL stays
// on the original Argus hostname, so tlsmaterial preserves Host/SNI checks.
func pinnedAddressTransport(dialAddress string) *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, dialAddress)
		},
	}
}

func probeGateway(ctx context.Context, store localStore) error {
	identity, err := store.loadIdentity()
	if err != nil {
		return err
	}
	endpoint := strings.TrimSpace(identity.GatewayEndpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "grpcs" || parsed.Hostname() == "" {
		return errors.New("Connector probe gateway endpoint is invalid")
	}
	address := parsed.Host
	if parsed.Port() == "" {
		address = net.JoinHostPort(parsed.Hostname(), "443")
	}
	material, err := tlsmaterial.Load(tlsmaterial.Options{CertificatePath: filepath.Join(store.directory, certFile),
		PrivateKeyPath: filepath.Join(store.directory, keyFile), CABundlePath: filepath.Join(store.directory, caFile),
		Usage: x509.ExtKeyUsageClientAuth})
	if err != nil {
		return err
	}
	configuration, err := material.ClientConfig(parsed.Hostname())
	if err != nil {
		return err
	}
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: configuration}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("Connector gateway TLS probe failed: %w", err)
	}
	return connection.Close()
}
