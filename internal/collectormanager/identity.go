package collectormanager

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
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/tlsmaterial"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

const maxEnrollmentResponseBytes = 96 << 10

type IdentityMaterial struct {
	ClientCertificatePEM, ClientPrivateKeyPEM []byte
	ServerCertificatePEM, ServerPrivateKeyPEM []byte
	CABundlePEM                               []byte
	TrustBundleEpoch                          uint64
	TrustBundleSHA256                         string
	TrustBundleCAFingerprints                 []string
}

type enrollmentResult struct {
	CollectorID          string                `json:"collector_id"`
	ClientCertificatePEM string                `json:"client_certificate_pem"`
	ServerCertificatePEM string                `json:"server_certificate_pem"`
	TrustBundle          enrollmentTrustBundle `json:"trust_bundle"`
	CertificateExpiresAt time.Time             `json:"certificate_expires_at"`
}

type enrollmentTrustBundle struct {
	Epoch                 uint64   `json:"epoch"`
	State                 string   `json:"state"`
	BundlePEM             string   `json:"bundle_pem"`
	BundleSHA256          string   `json:"bundle_sha256"`
	CurrentCAFingerprints []string `json:"current_ca_fingerprints"`
	NextCAFingerprints    []string `json:"next_ca_fingerprints"`
}

func EnrollIdentity(ctx context.Context, command *connectorv1.CollectorManagementCommand) (IdentityMaterial, error) {
	if command.GetOperation() != "install" || len(command.GetEnrollmentToken()) < 32 {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	endpoint, err := url.Parse(command.GetEnrollmentEndpoint())
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	bootstrapTrust, err := commandTrustBundle(command)
	if err != nil {
		return IdentityMaterial{}, err
	}
	clientKey, clientCSRPEM, err := collectorCSR(command.GetCollectorId(), x509.ExtKeyUsageClientAuth)
	if err != nil {
		return IdentityMaterial{}, err
	}
	serverKey, serverCSRPEM, err := collectorCSR(command.GetCollectorId(), x509.ExtKeyUsageServerAuth)
	if err != nil {
		return IdentityMaterial{}, err
	}
	body, _ := json.Marshal(map[string]string{"collector_id": command.GetCollectorId(), "client_csr_pem": string(clientCSRPEM), "server_csr_pem": string(serverCSRPEM)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return IdentityMaterial{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Argus-Telemetry-Enrollment-Token", strings.TrimSpace(string(command.GetEnrollmentToken())))
	tlsConfig, err := tlsmaterial.StaticClientConfig(bootstrapTrust.PEM, nil, nil, endpoint.Hostname())
	if err != nil {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig},
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrInvalidCommand }}
	response, err := client.Do(request)
	if err != nil {
		return IdentityMaterial{}, err
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxEnrollmentResponseBytes+1))
	if err != nil || len(encoded) > maxEnrollmentResponseBytes || response.StatusCode != http.StatusCreated {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	var result enrollmentResult
	if json.Unmarshal(encoded, &result) != nil || result.CollectorID != command.GetCollectorId() || time.Until(result.CertificateExpiresAt) <= 0 {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	returnedTrust, err := validateEnrollmentTrustBundle(command, result.TrustBundle)
	if err != nil {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	clientPrivateKeyPEM, err := marshalPrivateKey(clientKey)
	if err != nil {
		return IdentityMaterial{}, err
	}
	serverPrivateKeyPEM, err := marshalPrivateKey(serverKey)
	if err != nil {
		return IdentityMaterial{}, err
	}
	material := IdentityMaterial{ClientCertificatePEM: []byte(result.ClientCertificatePEM), ClientPrivateKeyPEM: clientPrivateKeyPEM,
		ServerCertificatePEM: []byte(result.ServerCertificatePEM), ServerPrivateKeyPEM: serverPrivateKeyPEM,
		CABundlePEM: returnedTrust.PEM, TrustBundleEpoch: result.TrustBundle.Epoch,
		TrustBundleSHA256: returnedTrust.SHA256, TrustBundleCAFingerprints: returnedTrust.Fingerprints}
	if err = validateIdentityMaterial(command.GetCollectorId(), material); err != nil {
		return IdentityMaterial{}, err
	}
	return material, nil
}

func validateIdentityMaterial(collectorID string, material IdentityMaterial) error {
	trust, err := trustbundle.Parse(material.CABundlePEM, time.Now().UTC())
	if err != nil || material.TrustBundleEpoch < 1 || !strings.EqualFold(trust.SHA256, material.TrustBundleSHA256) {
		return ErrInvalidCommand
	}
	fingerprints := append([]string(nil), material.TrustBundleCAFingerprints...)
	for index := range fingerprints {
		fingerprints[index] = strings.ToLower(strings.TrimSpace(fingerprints[index]))
	}
	slices.Sort(fingerprints)
	if !slices.Equal(trust.Fingerprints, fingerprints) {
		return ErrInvalidCommand
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(material.CABundlePEM) {
		return ErrInvalidCommand
	}
	if err = validateIdentityCertificate(material.ClientCertificatePEM, material.ClientPrivateKeyPEM, collectorID, x509.ExtKeyUsageClientAuth, pool); err != nil {
		return err
	}
	if err = validateIdentityCertificate(material.ServerCertificatePEM, material.ServerPrivateKeyPEM, collectorID, x509.ExtKeyUsageServerAuth, pool); err != nil {
		return errors.Join(ErrInvalidCommand, err)
	}
	return nil
}

func collectorCSR(collectorID string, usage x509.ExtKeyUsage) (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	request := &x509.CertificateRequest{Subject: pkix.Name{CommonName: "argus-otelcol"}}
	if usage == x509.ExtKeyUsageClientAuth {
		request.URIs = []*url.URL{{Scheme: "spiffe", Host: "argus", Path: "/telemetry/collectors/" + collectorID}}
	} else if usage == x509.ExtKeyUsageServerAuth {
		request.URIs = []*url.URL{{Scheme: "spiffe", Host: "argus", Path: "/telemetry/collector-servers/" + collectorID}}
		request.DNSNames = []string{"collector-" + collectorID + ".argus.telemetry"}
	} else {
		return nil, nil, ErrInvalidCommand
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, request, key)
	if err != nil {
		return nil, nil, err
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

func marshalPrivateKey(key *ecdsa.PrivateKey) ([]byte, error) {
	value, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: value}), nil
}

func validateEnrollmentTrustBundle(command *connectorv1.CollectorManagementCommand, returned enrollmentTrustBundle) (trustbundle.Material, error) {
	material, err := trustbundle.Parse([]byte(returned.BundlePEM), time.Now().UTC())
	if err != nil || returned.Epoch < command.GetTrustBundleEpoch() || !strings.EqualFold(material.SHA256, returned.BundleSHA256) {
		return trustbundle.Material{}, ErrInvalidCommand
	}
	fingerprints := append(append([]string{}, returned.CurrentCAFingerprints...), returned.NextCAFingerprints...)
	for index := range fingerprints {
		fingerprints[index] = strings.ToLower(strings.TrimSpace(fingerprints[index]))
	}
	slices.Sort(fingerprints)
	if !slices.Equal(material.Fingerprints, fingerprints) || returned.Epoch == command.GetTrustBundleEpoch() &&
		!strings.EqualFold(material.SHA256, command.GetTrustBundleSha256()) {
		return trustbundle.Material{}, ErrInvalidCommand
	}
	return material, nil
}

func validateIdentityCertificate(certificatePEM, privateKeyPEM []byte, collectorID string, usage x509.ExtKeyUsage, roots *x509.CertPool) error {
	keyPair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil || len(keyPair.Certificate) == 0 {
		return ErrInvalidCommand
	}
	certificate, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil || time.Now().UTC().Before(certificate.NotBefore) || !time.Now().UTC().Before(certificate.NotAfter) ||
		len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != usage || len(certificate.URIs) != 1 {
		return ErrInvalidCommand
	}
	options := x509.VerifyOptions{Roots: roots, CurrentTime: time.Now().UTC(), KeyUsages: []x509.ExtKeyUsage{usage}}
	if usage == x509.ExtKeyUsageClientAuth {
		if certificate.URIs[0].String() != "spiffe://argus/telemetry/collectors/"+collectorID || len(certificate.DNSNames) != 0 {
			return ErrInvalidCommand
		}
	} else {
		options.DNSName = "collector-" + collectorID + ".argus.telemetry"
		if certificate.URIs[0].String() != "spiffe://argus/telemetry/collector-servers/"+collectorID ||
			len(certificate.DNSNames) != 1 || certificate.DNSNames[0] != options.DNSName {
			return ErrInvalidCommand
		}
	}
	if _, err = certificate.Verify(options); err != nil {
		return errors.Join(ErrInvalidCommand, err)
	}
	return nil
}
