package collectormanager

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
)

func TestValidateRejectsTamperedConfigAndPendingPlatform(t *testing.T) {
	command := collectorCommand(testConfigBundle(t), "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
	if err := Validate(command); err != nil {
		t.Fatalf("valid Collector command rejected: %v", err)
	}
	command.RenderedConfig = []byte(`{"schema_version":"argus.otelcol/v1","changed":true}`)
	if err := Validate(command); err == nil {
		t.Fatal("tampered Collector config accepted")
	}
	command = collectorCommand(testConfigBundle(t), "windows_amd64", "https://artifacts.example/collector.zip", []byte("artifact"))
	if err := Validate(command); err != ErrUnsupportedPlatform {
		t.Fatalf("Windows validation-pending command returned %v", err)
	}
	command = collectorCommand(testConfigBundle(t), "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
	command.ResourceType = "kubernetes_cluster"
	if err := Validate(command); err != ErrUnsupportedPlatform {
		t.Fatalf("Kubernetes command without a frozen image returned %v", err)
	}
}

func TestArtifactHTTPClientRequiresTrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("ok")) }))
	defer server.Close()
	client, err := NewArtifactHTTPClient("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Get(server.URL); err == nil {
		t.Fatal("untrusted Artifact TLS certificate accepted")
	}
	caPath := filepath.Join(t.TempDir(), "artifact-ca.pem")
	if err = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err = NewArtifactHTTPClient(caPath)
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
	command := collectorCommand(testConfigBundle(t), "linux_arm64", server.URL, artifact)
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

func testConfigBundle(t *testing.T) []byte {
	t.Helper()
	value, err := configbundle.Render(configbundle.RenderInput{CollectorID: uuid.NewString(), ResourceID: uuid.NewString(),
		ResourceType: "host", Role: "direct", RouteKind: "direct_argus", ProfileKeys: []string{"host-basic", "otlp-receiver"},
		EnrollmentEndpoint: "https://api.example.com/api/v1/telemetry/collectors/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318", ServerCAPEM: collectorTestCA(t)})
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

func collectorCommand(config []byte, platform, uri string, artifact []byte) *connectorv1.CollectorManagementCommand {
	configHash := sha256.Sum256(config)
	artifactHash := sha256.Sum256(artifact)
	collectorID, err := configbundle.CollectorID(config)
	if err != nil {
		collectorID = uuid.NewString()
	}
	return &connectorv1.CollectorManagementCommand{
		CollectorId: collectorID, Operation: "install", ResourceId: uuid.NewString(), CollectorVersion: 1, DesiredRevision: 1,
		RenderedConfig: config, ConfigSha256: hex.EncodeToString(configHash[:]), RouteKind: "direct_argus", ResourceType: "host",
		Artifact: &connectorv1.CollectorArtifact{DistributionVersionId: uuid.NewString(), Platform: platform, Uri: uri,
			Sha256: hex.EncodeToString(artifactHash[:]), Signature: "test-signature-value-with-minimum-length", SigningKeyId: "test-key", ByteSize: uint64(len(artifact))},
	}
}
