package collectormanager

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
)

func TestEnrollIdentityUsesPinnedTLSAndValidatesIssuedIdentity(t *testing.T) {
	collectorID := uuid.NewString()
	serverURL, serverCA := enrollmentTLSServer(t, collectorID, http.StatusCreated, 0, nil)
	command := enrollmentCommand(t, collectorID, serverURL, serverCA)

	material, err := EnrollIdentity(t.Context(), command)
	if err != nil {
		t.Fatalf("enroll Collector identity: %v", err)
	}
	if len(material.CertificatePEM) == 0 || len(material.PrivateKeyPEM) == 0 || len(material.CABundlePEM) == 0 {
		t.Fatal("enrollment did not return complete identity material")
	}
	if err = validateIdentityMaterial(collectorID, material); err != nil {
		t.Fatalf("validate enrolled identity: %v", err)
	}
}

func TestEnrollIdentityRejectsUntrustedServerAndInvalidIssuedIdentity(t *testing.T) {
	collectorID := uuid.NewString()
	tests := []struct {
		name   string
		mutate func(*x509.Certificate)
		badCA  bool
	}{
		{name: "untrusted server CA", badCA: true},
		{name: "wrong SPIFFE URI", mutate: func(certificate *x509.Certificate) {
			certificate.URIs = []*url.URL{{Scheme: "spiffe", Host: "argus", Path: "/telemetry/collectors/" + uuid.NewString()}}
		}},
		{name: "wrong DNS SAN", mutate: func(certificate *x509.Certificate) {
			certificate.DNSNames = []string{"collector-" + uuid.NewString() + ".argus.telemetry"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverURL, serverCA := enrollmentTLSServer(t, collectorID, http.StatusCreated, 0, test.mutate)
			if test.badCA {
				serverCA = collectorTestCA(t)
			}
			if _, err := EnrollIdentity(t.Context(), enrollmentCommand(t, collectorID, serverURL, serverCA)); err == nil {
				t.Fatal("invalid enrollment identity was accepted")
			}
		})
	}
}

func TestEnrollIdentityRejectsInvalidResponses(t *testing.T) {
	collectorID := uuid.NewString()
	tests := []struct {
		name       string
		statusCode int
		bodySize   int
	}{
		{name: "non-created response", statusCode: http.StatusConflict},
		{name: "oversized response", statusCode: http.StatusCreated, bodySize: maxEnrollmentResponseBytes + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverURL, serverCA := enrollmentTLSServer(t, collectorID, test.statusCode, test.bodySize, nil)
			if _, err := EnrollIdentity(t.Context(), enrollmentCommand(t, collectorID, serverURL, serverCA)); err == nil {
				t.Fatal("invalid enrollment response was accepted")
			}
		})
	}
}

func enrollmentCommand(t *testing.T, collectorID, endpoint, serverCA string) *connectorv1.CollectorManagementCommand {
	t.Helper()
	config, err := configbundle.Render(configbundle.RenderInput{CollectorID: collectorID, ResourceID: uuid.NewString(), ResourceType: "kubernetes_cluster",
		Role: "kubernetes", RouteKind: "direct_argus", ProfileKeys: []string{"k8s-node-container", "k8s-cluster", "k8s-otlp-gateway"},
		EnrollmentEndpoint: endpoint, IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317",
		IngestHTTPEndpoint: "https://telemetry.example.com:4318", ServerCAPEM: serverCA})
	if err != nil {
		t.Fatal(err)
	}
	command := collectorCommand(config, "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
	command.ResourceType = "kubernetes_cluster"
	command.EnrollmentEndpoint = endpoint
	command.EnrollmentToken = []byte("0123456789abcdef0123456789abcdef")
	return command
}

func enrollmentTLSServer(t *testing.T, collectorID string, statusCode, bodySize int, mutate func(*x509.Certificate)) (string, string) {
	t.Helper()
	ca, caKey, caPEM := testCertificateAuthority(t)
	serverCertificate := issueTestCertificate(t, ca, caKey, &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "telemetry-ingest"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || request.TLS.Version != tls.VersionTLS13 ||
			request.Header.Get("X-Argus-Telemetry-Enrollment-Token") != "0123456789abcdef0123456789abcdef" {
			http.Error(writer, "invalid enrollment transport", http.StatusUnauthorized)
			return
		}
		var body struct {
			CollectorID string `json:"collector_id"`
			CSRPem      string `json:"csr_pem"`
		}
		if json.NewDecoder(request.Body).Decode(&body) != nil || body.CollectorID != collectorID || strings.Contains(body.CSRPem, "0123456789abcdef") {
			http.Error(writer, "invalid enrollment request", http.StatusBadRequest)
			return
		}
		if bodySize > 0 {
			writer.WriteHeader(statusCode)
			_, _ = writer.Write(make([]byte, bodySize))
			return
		}
		block, rest := pem.Decode([]byte(body.CSRPem))
		if block == nil || len(rest) != 0 {
			http.Error(writer, "invalid CSR", http.StatusBadRequest)
			return
		}
		csr, err := x509.ParseCertificateRequest(block.Bytes)
		if err != nil || csr.CheckSignature() != nil {
			http.Error(writer, "invalid CSR", http.StatusBadRequest)
			return
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "argus-otelcol"}, NotBefore: time.Now().Add(-time.Minute),
			NotAfter: time.Now().Add(time.Hour), URIs: csr.URIs, DNSNames: csr.DNSNames, KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
		if mutate != nil {
			mutate(leaf)
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, csr.PublicKey, caKey)
		if err != nil {
			http.Error(writer, "certificate issue failed", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(statusCode)
		_ = json.NewEncoder(writer).Encode(map[string]any{"collector_id": collectorID,
			"certificate_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})), "ca_bundle_pem": string(caPEM),
			"certificate_expires_at": leaf.NotAfter.UTC()})
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server.URL + "/v1/identity/enroll", string(caPEM)
}

func testCertificateAuthority(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "telemetry-test-ca"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func issueTestCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, template *x509.Certificate) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}
