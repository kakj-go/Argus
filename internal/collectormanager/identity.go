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
	"strings"
	"time"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
)

const maxEnrollmentResponseBytes = 96 << 10

type IdentityMaterial struct {
	CertificatePEM, PrivateKeyPEM, CABundlePEM []byte
}

type enrollmentResult struct {
	CollectorID          string    `json:"collector_id"`
	CertificatePEM       string    `json:"certificate_pem"`
	CABundlePEM          string    `json:"ca_bundle_pem"`
	CertificateExpiresAt time.Time `json:"certificate_expires_at"`
}

func EnrollIdentity(ctx context.Context, command *connectorv1.CollectorManagementCommand) (IdentityMaterial, error) {
	if command.GetOperation() != "install" || len(command.GetEnrollmentToken()) < 32 {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	endpoint, err := url.Parse(command.GetEnrollmentEndpoint())
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	serverCA, err := configbundle.ServerCA(command.GetRenderedConfig())
	if err != nil {
		return IdentityMaterial{}, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(serverCA) {
		return IdentityMaterial{}, ErrInvalidCommand
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return IdentityMaterial{}, err
	}
	dnsName := "collector-" + command.GetCollectorId() + ".argus.telemetry"
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "argus-otelcol"},
		URIs: []*url.URL{{Scheme: "spiffe", Host: "argus", Path: "/telemetry/collectors/" + command.GetCollectorId()}}, DNSNames: []string{dnsName}}, key)
	if err != nil {
		return IdentityMaterial{}, err
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	body, _ := json.Marshal(map[string]string{"collector_id": command.GetCollectorId(), "csr_pem": string(csrPEM)})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return IdentityMaterial{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Argus-Telemetry-Enrollment-Token", strings.TrimSpace(string(command.GetEnrollmentToken())))
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}},
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
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return IdentityMaterial{}, err
	}
	material := IdentityMaterial{CertificatePEM: []byte(result.CertificatePEM), CABundlePEM: []byte(result.CABundlePEM),
		PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})}
	if err = validateIdentityMaterial(command.GetCollectorId(), material); err != nil {
		return IdentityMaterial{}, err
	}
	return material, nil
}

func validateIdentityMaterial(collectorID string, material IdentityMaterial) error {
	certificate, err := tls.X509KeyPair(material.CertificatePEM, material.PrivateKeyPEM)
	if err != nil || len(certificate.Certificate) != 1 {
		return ErrInvalidCommand
	}
	parsed, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || time.Until(parsed.NotAfter) <= 0 || len(parsed.URIs) != 1 || len(parsed.DNSNames) != 1 ||
		parsed.URIs[0].String() != "spiffe://argus/telemetry/collectors/"+collectorID || parsed.DNSNames[0] != "collector-"+collectorID+".argus.telemetry" {
		return ErrInvalidCommand
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(material.CABundlePEM) {
		return ErrInvalidCommand
	}
	if _, err = parsed.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: time.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return errors.Join(ErrInvalidCommand, err)
	}
	return nil
}
