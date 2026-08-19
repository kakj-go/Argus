package argusidentity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/collector/client"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

func TestEnrollmentCreatesLocalIdentityAndConsumesToken(t *testing.T) {
	collectorID := "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111"
	caCertificate, caKey, caPEM := testCertificateAuthority(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Argus-Telemetry-Enrollment-Token") != "one-time-token" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			CollectorID string `json:"collector_id"`
			CSRPem      string `json:"csr_pem"`
		}
		if json.NewDecoder(request.Body).Decode(&body) != nil || body.CollectorID != collectorID {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		block, _ := pem.Decode([]byte(body.CSRPem))
		csr, err := x509.ParseCertificateRequest(block.Bytes)
		if err != nil || csr.CheckSignature() != nil {
			http.Error(writer, "bad csr", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC()
		template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "collector"}, URIs: csr.URIs, DNSNames: csr.DNSNames,
			NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
		raw, err := x509.CreateCertificate(rand.Reader, template, caCertificate, csr.PublicKey, caKey)
		if err != nil {
			http.Error(writer, "sign failed", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(certificateResult{CollectorID: collectorID,
			CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})), CABundlePEM: caPEM,
			IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318",
			CertificateExpiresAt: template.NotAfter})
	}))
	defer server.Close()

	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "enrollment-token")
	if err := os.WriteFile(tokenPath, []byte("one-time-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverCertificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	serverCAPath := filepath.Join(directory, "server-ca.pem")
	if err := os.WriteFile(serverCAPath, serverCertificate, 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{CollectorID: collectorID, EnrollmentEndpoint: server.URL, RotationEndpoint: server.URL,
		EnrollmentTokenFile: tokenPath, CertificateFile: filepath.Join(directory, "client.pem"), PrivateKeyFile: filepath.Join(directory, "client-key.pem"),
		CABundleFile: filepath.Join(directory, "ca.pem"), ServerCAFile: serverCAPath, RotateBefore: 8 * time.Hour, CheckInterval: time.Hour}
	extension := newIdentityExtension(config, zap.NewNop())
	if err := extension.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := extension.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("enrollment token remains after successful use: %v", err)
	}
	for _, path := range []string{config.CertificateFile, config.PrivateKeyFile, config.CABundleFile} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("identity file %s is missing or has unsafe mode: %v", path, err)
		}
	}
}

func TestAuthenticateRequiresOneValidCollectorCertificate(t *testing.T) {
	extension := newIdentityExtension(Config{}, zap.NewNop())
	if _, err := extension.Authenticate(context.Background(), nil); err == nil {
		t.Fatal("missing telemetry peer accepted")
	}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}, {}},
	}}})
	if _, err := extension.Authenticate(ctx, nil); err == nil {
		t.Fatal("multiple telemetry peer certificates accepted")
	}

	invalidURI, _ := url.Parse("spiffe://argus/connectors/not-telemetry")
	ctx = peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{invalidURI}, SerialNumber: big.NewInt(1)}},
	}}})
	if _, err := extension.Authenticate(ctx, nil); err == nil {
		t.Fatal("non-telemetry URI SAN accepted")
	}

	collectorID := "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111"
	validURI, _ := url.Parse(spiffePrefix + collectorID)
	ctx = peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{validURI}, SerialNumber: big.NewInt(0xab)}},
	}}})
	authenticated, err := extension.Authenticate(ctx, nil)
	if err != nil {
		t.Fatalf("valid telemetry peer rejected: %v", err)
	}
	auth := client.FromContext(authenticated).Auth
	if auth == nil || auth.GetAttribute("argus.telemetry.collector_id") != collectorID ||
		auth.GetAttribute("argus.telemetry.certificate_serial") != "ab" {
		t.Fatalf("trusted authentication attributes missing: %#v", auth)
	}
}

func testCertificateAuthority(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "argus-telemetry-test-ca"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}))
}
