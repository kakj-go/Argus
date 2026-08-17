package connector

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	connectorcore "github.com/kakj-go/Argus/internal/connector"
)

type enrollOptions struct {
	Server, ConnectorID, Token, Role, Name, DataDirectory string
}

type enrollResult struct {
	ConnectorID          string    `json:"connector_id"`
	Role                 string    `json:"role"`
	CertificatePEM       string    `json:"certificate_pem"`
	CABundlePEM          string    `json:"ca_bundle_pem"`
	GatewayEndpoint      string    `json:"gateway_endpoint"`
	CertificateExpiresAt time.Time `json:"certificate_expires_at"`
	Result               string    `json:"result"`
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
	capabilities := roleCapabilities(options.Role)
	payload := map[string]any{"csr_pem": identity.CSRPEM, "device_fingerprint": deviceFingerprint(), "instance_id": instanceID(),
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
	response, err := (&http.Client{Timeout: 45 * time.Second}).Do(request)
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
		result.CertificatePEM == "" || result.CABundlePEM == "" || result.GatewayEndpoint == "" {
		return enrollResult{}, errors.New("Connector enrollment returned incomplete identity material")
	}
	keyPEM, err := connectorcore.MarshalPrivateKey(identity.PrivateKey)
	if err != nil {
		return enrollResult{}, err
	}
	if err := validateIssuedIdentity(id, keyPEM, []byte(result.CertificatePEM), []byte(result.CABundlePEM)); err != nil {
		return enrollResult{}, err
	}
	state := identityState{ConnectorID: id.String(), Role: result.Role, InstanceID: instanceID(), Name: options.Name,
		GatewayEndpoint: result.GatewayEndpoint, CertificateExpiresAt: result.CertificateExpiresAt, Capabilities: capabilities}
	if err := (localStore{directory: options.DataDirectory}).saveIdentity(state, keyPEM, []byte(result.CertificatePEM), []byte(result.CABundlePEM)); err != nil {
		return enrollResult{}, err
	}
	return result, nil
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
