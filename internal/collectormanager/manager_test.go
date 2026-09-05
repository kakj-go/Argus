package collectormanager

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

func TestValidateRejectsTamperedConfigAndPendingPlatform(t *testing.T) {
	command := collectorCommand(t, testConfigBundle(t), "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
	if err := Validate(command); err != nil {
		t.Fatalf("valid Collector command rejected: %v", err)
	}
	command.Transport = ""
	if err := Validate(command); err != ErrInvalidCommand {
		t.Fatalf("command without an explicit route transport returned %v", err)
	}
	command.Transport = "direct"
	command.RenderedConfig = []byte(`{"schema_version":"argus.otelcol/v1","changed":true}`)
	if err := Validate(command); err == nil {
		t.Fatal("tampered Collector config accepted")
	}
	command = collectorCommand(t, testConfigBundle(t), "windows_amd64", "https://artifacts.example/collector.zip", []byte("artifact"))
	if err := Validate(command); err != ErrUnsupportedPlatform {
		t.Fatalf("Windows validation-pending command returned %v", err)
	}
	command = collectorCommand(t, testConfigBundle(t), "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
	command.ResourceType = "kubernetes_cluster"
	if err := Validate(command); err != ErrUnsupportedPlatform {
		t.Fatalf("Kubernetes command without a frozen image returned %v", err)
	}
}

func TestArtifactHTTPClientRequiresTrustedTLS(t *testing.T) {
	ca, caKey, caPEM := testCertificateAuthority(t)
	serverCertificate := issueTestCertificate(t, ca, caKey, &x509.Certificate{SerialNumber: big.NewInt(200), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("ok")) }))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}}
	server.StartTLS()
	defer server.Close()
	if _, err := NewArtifactHTTPClient(""); err == nil {
		t.Fatal("missing Artifact Trust Bundle was accepted")
	}
	caPath := filepath.Join(t.TempDir(), "artifact-ca.pem")
	if writeErr := os.WriteFile(caPath, caPEM, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	client, err := NewArtifactHTTPClient(caPath)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("trusted Artifact TLS certificate rejected: %v", err)
	}
	response.Body.Close()
	if _, err = NewArtifactHTTPClient(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("missing Artifact CA bundle accepted")
	}
}

func TestApplyLocalVerifiesArtifactAndWritesAtomicState(t *testing.T) {
	artifact := []byte("argus-otelcol-test-artifact")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(artifact)
	}))
	defer server.Close()
	command := collectorCommand(t, testConfigBundle(t), "linux_arm64", server.URL, artifact)
	publicKey, privateKey, keyErr := ed25519.GenerateKey(rand.Reader)
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	hash := sha256.Sum256(artifact)
	command.Artifact.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, hash[:]))
	root := t.TempDir()
	result, err := (Manager{Root: root, HTTPClient: server.Client(), TrustedSigningKeys: map[string]ed25519.PublicKey{"test-key": publicKey}}).ApplyLocal(context.Background(), command)
	if err != nil {
		t.Fatalf("apply local Collector: %v", err)
	}
	if result.Status != "converged" || result.EffectiveRevision != command.DesiredRevision {
		t.Fatalf("unexpected Collector result: %#v", result)
	}
	directory := filepath.Join(root, command.CollectorId)
	for _, name := range []string{"collector.artifact", "collector-config.yaml", "state.json"} {
		info, statErr := os.Stat(filepath.Join(directory, name))
		if statErr != nil || info.Size() == 0 {
			t.Fatalf("managed file %s missing: %v", name, statErr)
		}
	}
	command.Operation = "uninstall"
	if _, err = (Manager{Root: root}).ApplyLocal(context.Background(), command); err != nil {
		t.Fatalf("uninstall local Collector: %v", err)
	}
	if _, err = os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("Collector directory remains after uninstall: %v", err)
	}
}

func TestSigningKeysCanBeProvisionedThroughReadOnlyFile(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "signing-keys.json")
	encoded := base64.RawStdEncoding.EncodeToString(publicKey)
	if err = os.WriteFile(path, []byte(`{"release-key":"`+encoded+`"}`), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS", "{}")
	t.Setenv("ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS_FILE", path)
	keys := signingKeysFromEnvironment()
	if !bytes.Equal(keys["release-key"], publicKey) {
		t.Fatal("file-provisioned signing root was not loaded")
	}
}

func TestLocalCollectorArchiveAndUnitAreHardened(t *testing.T) {
	binary := []byte("collector-binary")
	var archive bytes.Buffer
	compressed := gzip.NewWriter(&archive)
	writer := tar.NewWriter(compressed)
	if err := writer.WriteHeader(&tar.Header{Name: "release/argus-otelcol", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	archivePath, destination := filepath.Join(root, "collector.tar.gz"), filepath.Join(root, "argus-otelcol")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installCollectorArchive(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(installed, binary) {
		t.Fatalf("installed collector = %q, err=%v", installed, err)
	}
	command := collectorCommand(t, testConfigBundle(t), "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
	command.EnrollmentEndpoint = "https://telemetry.example/enroll"
	command.IngestGrpcEndpoint = "grpcs://telemetry.example:4317"
	command.IngestHttpEndpoint = "https://telemetry.example:4318"
	unit := localCollectorSystemdUnit(command, "/var/lib/argus-otelcol")
	for _, required := range []string{"NoNewPrivileges=true", "ProtectSystem=strict", "ReadWritePaths=/var/lib/argus-otelcol /etc/argus-otelcol", "ARGUS_TELEMETRY_ENROLLMENT_TOKEN_FILE"} {
		if !strings.Contains(unit, required) {
			t.Fatalf("local Collector unit omitted %q", required)
		}
	}
	if validLocalCollectorRoot("/") || validLocalCollectorRoot("/tmp/collector") || !validLocalCollectorRoot("/var/lib/argus-otelcol") {
		t.Fatal("local Collector root validation accepted an unsafe path")
	}
}

func TestLocalCollectorIdentityReadinessBindsCollectorAndFileModes(t *testing.T) {
	root := t.TempDir()
	collectorID := uuid.NewString()
	writeTestLocalCollectorIdentity(t, root, collectorID)
	if err := validateLocalCollectorIdentity(root, collectorID); err != nil {
		t.Fatalf("valid local Collector identity rejected: %v", err)
	}
	if err := validateLocalCollectorIdentity(root, uuid.NewString()); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("identity for another Collector returned %v", err)
	}
	keyPath := filepath.Join(root, "identity", "client-key.pem")
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateLocalCollectorIdentity(root, collectorID); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("group/world-readable identity key returned %v", err)
	}
}

func TestFetchArtifactRejectsUntrustedSignatureAndTamperedBytes(t *testing.T) {
	body := []byte("immutable-connector-artifact")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	artifact := &connectorv1.CollectorArtifact{
		Uri: server.URL, Sha256: hex.EncodeToString(digest[:]), ByteSize: uint64(len(body)), SigningKeyId: "release-key",
		Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:])),
	}
	manager := Manager{HTTPClient: server.Client(), TrustedSigningKeys: map[string]ed25519.PublicKey{"release-key": publicKey}}
	if _, err = manager.FetchArtifact(context.Background(), artifact); err != nil {
		t.Fatalf("valid signed artifact rejected: %v", err)
	}

	otherPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manager.TrustedSigningKeys["release-key"] = otherPublic
	if _, err = manager.FetchArtifact(context.Background(), artifact); !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("artifact signed by an untrusted key returned %v", err)
	}

	manager.TrustedSigningKeys["release-key"] = publicKey
	originalSignature := artifact.Signature
	artifact.Signature = base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if _, err = manager.FetchArtifact(context.Background(), artifact); !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("forged artifact signature returned %v", err)
	}
	artifact.Signature = originalSignature
	body = []byte("immutable-connector-artifacU")
	if _, err = manager.FetchArtifact(context.Background(), artifact); !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("tampered artifact bytes returned %v", err)
	}
}

func TestFetchArtifactFailsClosedWithoutTLSClient(t *testing.T) {
	body := []byte("immutable-connector-artifact")
	digest := sha256.Sum256(body)
	artifact := &connectorv1.CollectorArtifact{
		Uri: "https://artifacts.example/collector.tar.gz", Sha256: hex.EncodeToString(digest[:]), ByteSize: uint64(len(body)),
		SigningKeyId: "release-key", Signature: base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
	}
	if _, err := (Manager{}).FetchArtifact(context.Background(), artifact); !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("missing Artifact TLS client returned %v", err)
	}
}

func TestFetchArtifactToStreamsSignedArtifactInBoundedChunks(t *testing.T) {
	body := bytes.Repeat([]byte("argus-connector-stream\n"), 1<<18)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	artifact := &connectorv1.CollectorArtifact{
		Uri: server.URL, Sha256: hex.EncodeToString(digest[:]), ByteSize: uint64(len(body)), SigningKeyId: "stream-key",
		Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:])),
	}
	destination := &boundedArtifactWriter{limit: 64 << 10}
	manager := Manager{HTTPClient: server.Client(), TrustedSigningKeys: map[string]ed25519.PublicKey{"stream-key": publicKey}}
	if err = manager.FetchArtifactTo(context.Background(), artifact, destination); err != nil {
		t.Fatalf("stream signed artifact: %v", err)
	}
	if !bytes.Equal(destination.Bytes(), body) {
		t.Fatal("streamed artifact differs from the signed source")
	}
	if destination.largest > destination.limit {
		t.Fatalf("artifact write chunk=%d, want <=%d", destination.largest, destination.limit)
	}
}

type boundedArtifactWriter struct {
	bytes.Buffer
	limit   int
	largest int
}

func (writer *boundedArtifactWriter) Write(value []byte) (int, error) {
	if len(value) > writer.largest {
		writer.largest = len(value)
	}
	if len(value) > writer.limit {
		return 0, errors.New("artifact chunk exceeds streaming bound")
	}
	return writer.Buffer.Write(value)
}

func testConfigBundle(t *testing.T) []byte {
	t.Helper()
	value, err := configbundle.Render(configbundle.RenderInput{CollectorID: uuid.NewString(), ResourceID: uuid.NewString(),
		ResourceType: "host", Role: "direct", RouteKind: "direct_argus", Transport: "direct", ProfileKeys: []string{"host-basic", "otlp-receiver"},
		EnrollmentEndpoint: "https://api.example.com/api/v1/telemetry/collectors/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318"})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func collectorTestCA(t *testing.T) string {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "collector-test-ca"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	value, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: value}))
}

func writeTestLocalCollectorIdentity(t *testing.T, root, collectorID string) {
	t.Helper()
	caPublic, caPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "collector-ca"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPublic, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	clientPublic, clientPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identityURI, err := url.Parse("spiffe://argus/telemetry/collectors/" + collectorID)
	if err != nil {
		t.Fatal(err)
	}
	clientTemplate := &x509.Certificate{SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: "collector"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), URIs: []*url.URL{identityURI},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCertificate, clientPublic, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "identity")
	if err = os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"client.pem":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}),
		"client-key.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}),
		"ca.pem":         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
	}
	for name, value := range files {
		if err = os.WriteFile(filepath.Join(directory, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func collectorCommand(t *testing.T, config []byte, platform, uri string, artifact []byte) *connectorv1.CollectorManagementCommand {
	t.Helper()
	configHash := sha256.Sum256(config)
	artifactHash := sha256.Sum256(artifact)
	collectorID, err := configbundle.CollectorID(config)
	if err != nil {
		collectorID = uuid.NewString()
	}
	command := &connectorv1.CollectorManagementCommand{
		CollectorId: collectorID, Operation: "install", ResourceId: uuid.NewString(), CollectorVersion: 1, DesiredRevision: 1,
		RenderedConfig: config, ConfigSha256: hex.EncodeToString(configHash[:]), RouteKind: "direct_argus", Transport: "direct", ResourceType: "host",
		Artifact: &connectorv1.CollectorArtifact{DistributionVersionId: uuid.NewString(), Platform: platform, Uri: uri,
			Sha256: hex.EncodeToString(artifactHash[:]), Signature: "test-signature-value-with-minimum-length", SigningKeyId: "test-key", ByteSize: uint64(len(artifact))},
	}
	setCommandTrustBundle(t, command, []byte(collectorTestCA(t)))
	return command
}

func setCommandTrustBundle(t *testing.T, command *connectorv1.CollectorManagementCommand, value []byte) {
	t.Helper()
	material, err := trustbundle.Parse(value, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	command.TrustBundlePem = material.PEM
	command.TrustBundleEpoch = 1
	command.TrustBundleSha256 = material.SHA256
	command.TrustBundleCaFingerprints = material.Fingerprints
}
