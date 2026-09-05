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
	"net"
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

func TestConfigAcceptsOnlyLoopbackDialAddress(t *testing.T) {
	base := Config{CollectorID: "collector", EnrollmentEndpoint: "https://enterprise.argus.test/enroll",
		RotationEndpoint: "https://telemetry.argus.test/rotate", TrustBundleEndpoint: "https://telemetry.argus.test/trust-bundle",
		EnrollmentTokenFile: "/token", CertificateFile: "/client.pem", PrivateKeyFile: "/client-key.pem", CABundleFile: "/ca.pem",
		ServerCertificateFile: "/server.pem", ServerPrivateKeyFile: "/server-key.pem",
		TrustBundleStateFile: "/trust-bundle.json", RotateBefore: 8 * time.Hour, CheckInterval: time.Hour}
	for _, address := range []string{"127.0.0.1:8443", "[::1]:8443"} {
		config := base
		config.DialAddress = address
		if err := config.Validate(); err != nil {
			t.Fatalf("loopback dial address %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"10.0.0.8:8443", "enterprise.argus.test:8443", "127.0.0.1:0", "127.0.0.1"} {
		config := base
		config.DialAddress = address
		if err := config.Validate(); err == nil {
			t.Fatalf("unsafe dial address %q accepted", address)
		}
	}
}

func TestEnrollmentDialOverridePreservesRequestAndTLSHost(t *testing.T) {
	caCertificate, caKey, caPEM := testCertificateAuthority(t)
	serverCertificate := issueServerCertificate(t, caCertificate, caKey, "enterprise.argus.test")
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || request.TLS.ServerName != "enterprise.argus.test" {
			t.Errorf("TLS server name = %q", request.TLS.ServerName)
		}
		host, _, _ := net.SplitHostPort(request.Host)
		if host != "enterprise.argus.test" {
			t.Errorf("request host = %q", request.Host)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(certificateResult{CollectorID: "collector"})
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}}
	server.StartTLS()
	defer server.Close()

	directory := t.TempDir()
	caPath := filepath.Join(directory, "server-ca.pem")
	tokenPath := filepath.Join(directory, "token")
	if err := os.WriteFile(caPath, []byte(caPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Host = net.JoinHostPort("enterprise.argus.test", port)
	extension := newIdentityExtension(Config{CollectorID: "collector", EnrollmentEndpoint: endpoint.String(),
		DialAddress: server.Listener.Addr().String(), EnrollmentTokenFile: tokenPath, ServerCAFile: caPath}, zap.NewNop())
	if _, err = extension.enroll(t.Context(), []byte("test-client-csr"), []byte("test-server-csr")); err != nil {
		t.Fatalf("enrollment through loopback dial override failed: %v", err)
	}
}

func TestEnrollmentCreatesLocalIdentityAndConsumesToken(t *testing.T) {
	collectorID := "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111"
	caCertificate, caKey, caPEM := testCertificateAuthority(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Argus-Telemetry-Enrollment-Token") != "one-time-token" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			CollectorID  string `json:"collector_id"`
			ClientCSRPem string `json:"client_csr_pem"`
			ServerCSRPem string `json:"server_csr_pem"`
		}
		if json.NewDecoder(request.Body).Decode(&body) != nil || body.CollectorID != collectorID {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		clientBlock, _ := pem.Decode([]byte(body.ClientCSRPem))
		serverBlock, _ := pem.Decode([]byte(body.ServerCSRPem))
		clientCSR, clientErr := x509.ParseCertificateRequest(clientBlock.Bytes)
		serverCSR, serverErr := x509.ParseCertificateRequest(serverBlock.Bytes)
		if clientErr != nil || serverErr != nil || clientCSR.CheckSignature() != nil || serverCSR.CheckSignature() != nil {
			http.Error(writer, "bad csr", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC()
		clientTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "collector"}, URIs: clientCSR.URIs, DNSNames: clientCSR.DNSNames,
			NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		serverTemplate := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "collector-server"}, URIs: serverCSR.URIs, DNSNames: serverCSR.DNSNames,
			NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		clientRaw, clientIssueErr := x509.CreateCertificate(rand.Reader, clientTemplate, caCertificate, clientCSR.PublicKey, caKey)
		serverRaw, serverIssueErr := x509.CreateCertificate(rand.Reader, serverTemplate, caCertificate, serverCSR.PublicKey, caKey)
		if clientIssueErr != nil || serverIssueErr != nil {
			http.Error(writer, "sign failed", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		material, parseErr := parseTrustBundle([]byte(caPEM), now)
		if parseErr != nil {
			http.Error(writer, "bad ca", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(writer).Encode(certificateResult{CollectorID: collectorID,
			ClientCertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientRaw})),
			ServerCertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverRaw})),
			TrustBundle: trustBundleResult{Epoch: 1, State: bundleStateStable, BundlePEM: caPEM, BundleSHA256: material.SHA256,
				CurrentCAFingerprints: material.Fingerprints, NextCAFingerprints: []string{}, StartedAt: now},
			IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318",
			CertificateExpiresAt: clientTemplate.NotAfter})
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
	config := Config{CollectorID: collectorID, EnrollmentEndpoint: server.URL, RotationEndpoint: server.URL, TrustBundleEndpoint: server.URL,
		EnrollmentTokenFile: tokenPath, CertificateFile: filepath.Join(directory, "client.pem"), PrivateKeyFile: filepath.Join(directory, "client-key.pem"),
		ServerCertificateFile: filepath.Join(directory, "server.pem"), ServerPrivateKeyFile: filepath.Join(directory, "server-key.pem"),
		CABundleFile: filepath.Join(directory, "ca.pem"), TrustBundleStateFile: filepath.Join(directory, "trust-bundle.json"),
		ServerCAFile: serverCAPath, RotateBefore: 8 * time.Hour, CheckInterval: time.Hour}
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
	for _, path := range []string{config.CertificateFile, config.PrivateKeyFile, config.ServerCertificateFile, config.ServerPrivateKeyFile, config.CABundleFile, config.TrustBundleStateFile} {
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

func issueServerCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dnsName string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: dnsName}, DNSNames: []string{dnsName},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	raw, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}
}
