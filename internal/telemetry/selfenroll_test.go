package telemetry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kakj-go/Argus/internal/installinstruction"
)

type recordingArtifactChecker struct{ urls []string }

func (checker *recordingArtifactChecker) Check(_ context.Context, urls ...string) error {
	checker.urls = append(checker.urls, urls...)
	return nil
}

func TestBuildInstallInstructions(t *testing.T) {
	bundlePath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(bundlePath, selfEnrollTestCA(t), 0o600); err != nil {
		t.Fatal(err)
	}
	service := SelfEnrollService{IngestHTTPEndpoint: "https://telemetry.example.com:4318/", TrustBundlePath: bundlePath,
		TrustBundleEpoch: 2, BootstrapTLSMode: installinstruction.DownloadTLSInsecureFirstFetch,
		InstallerSHA256: strings.Repeat("a", 64)}
	sets, err := service.BuildInstallInstructions(t.Context(), "https://artifacts.example.com/argus-collector-artifacts/argus-otelcol/v1/linux-arm64.tar.gz", "tok123", "install", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("build install instructions: %v", err)
	}
	if len(sets) != 2 || sets[0].Scope != "linux-system" || sets[1].Scope != "linux-user" {
		t.Fatalf("instruction scopes = %#v", sets)
	}
	for _, set := range sets {
		if !strings.Contains(set.Command, "tok123") || !strings.Contains(set.Command, "--insecure") ||
			!strings.Contains(set.Command, "/v1/host-bootstrap-script") || !strings.Contains(set.BootstrapScript, "--cacert") ||
			!strings.Contains(set.BootstrapScript, "tok123") || strings.Contains(set.BootstrapScript, "--insecure") ||
			strings.Contains(set.BootstrapScript, "| sudo") {
			t.Fatalf("unsafe or incomplete instruction: %#v", set)
		}
	}
	uninstall, err := service.BuildInstallInstructions(t.Context(), "https://artifacts.example.com/x.tar.gz", "tok123", "uninstall", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("build uninstall instructions: %v", err)
	}
	if !strings.Contains(uninstall[0].BootstrapScript, "'--uninstall'") {
		t.Fatalf("uninstall instruction missing --uninstall: %q", uninstall[0].BootstrapScript)
	}
}

func selfEnrollTestCA(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "self-enroll test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
}

func TestArtifactScriptBase(t *testing.T) {
	origin, err := artifactScriptBase("https://artifacts.example.com/bucket/key.tar.gz")
	if err != nil || origin != "https://artifacts.example.com/bucket" {
		t.Fatalf("artifactScriptBase = %q, %v", origin, err)
	}
	if _, err = artifactScriptBase("not-a-url"); err == nil {
		t.Fatal("artifactScriptBase should reject invalid uri")
	}
}

func TestSelfEnrollChecksArtifactAndBucketInstallScript(t *testing.T) {
	checker := new(recordingArtifactChecker)
	service := SelfEnrollService{Artifacts: checker}
	artifact := "https://artifacts.example.com/argus-collector-artifacts/argus-otelcol/v1/linux-amd64.tar.gz"
	if err := service.ensureArtifactAvailability(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}
	want := []string{artifact, "https://artifacts.example.com/argus-collector-artifacts/install/host.sh"}
	if !reflect.DeepEqual(checker.urls, want) {
		t.Fatalf("availability URLs = %#v, want %#v", checker.urls, want)
	}
}

func TestActorTypeForAuditMapsPendingActionSubjects(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "", want: "enterprise_user"},
		{input: "user", want: "enterprise_user"},
		{input: "enterprise_user", want: "enterprise_user"},
		{input: "service_account", want: "service_account"},
		{input: "unsupported", want: "unsupported"},
	} {
		if got := actorTypeForAudit(test.input); got != test.want {
			t.Fatalf("actorTypeForAudit(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestCollectorActionPlanTelemetryTransport(t *testing.T) {
	plan := collectorActionPlan{}
	if transport, port := plan.telemetryTransport(); transport != "" || port.Valid {
		t.Fatalf("empty plan must remain invalid, got %q %v", transport, port)
	}
	plan.RouteTransport, plan.LoopbackPort = "executor_tunnel", 4317
	transport, port := plan.telemetryTransport()
	if transport != "executor_tunnel" || !port.Valid || port.Int32 != 4317 {
		t.Fatalf("tunnel plan transport = %q %v", transport, port)
	}
	// 隧道 transport 但未固化端口:保持原值并缺省端口,由 telemetry_routes 的
	// CHECK(loopback 端口必填)在写入时 fail closed,不做静默回退。
	plan.RouteTransport, plan.LoopbackPort = "bastion_tunnel", 0
	transport, port = plan.telemetryTransport()
	if transport != "bastion_tunnel" || port.Valid {
		t.Fatalf("tunnel without port must stay explicit and portless, got %q %v", transport, port)
	}
}
