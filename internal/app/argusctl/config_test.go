package argusctl

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEvaluationConfig(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Spec.Profile != "evaluation" || cfg.Image("argus-web") != "host.docker.internal:5001/argus/argus-web:dev" ||
		cfg.Spec.PKI.BootstrapTLSMode != "insecure-first-fetch" {
		t.Fatalf("unexpected config: %#v", cfg.Spec)
	}
}

func TestConfigRejectsUnknownBootstrapTLSMode(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Spec.PKI.BootstrapTLSMode = "fallback-on-error"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "bootstrapTLSMode") {
		t.Fatalf("Validate() error = %v, want bootstrap TLS mode error", err)
	}
}

func TestConfigRejectsReleaseIDThatCannotFitHelmStageSuffix(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Spec.ReleaseID = strings.Repeat("a", 35)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "34 characters") {
		t.Fatalf("Validate() error = %v, want release ID length error", err)
	}
}

func TestConfigRequiresRootOverlapLongerThanClientIdentityLifetime(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Spec.PKI.Rotation.Overlap = "24h"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "32h") {
		t.Fatalf("Validate() error = %v, want 32h overlap safety error", err)
	}
	cfg.Spec.PKI.Rotation.Overlap = "32h"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("minimum safe overlap rejected: %v", err)
	}
}

func TestLoadLocalHardeningConfigWithoutWindowsArtifact(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "local-hardening.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Spec.Profile != "local-hardening" || cfg.Spec.Telemetry.WindowsAMD64URI != "" {
		t.Fatalf("unexpected local-hardening config: %#v", cfg.Spec)
	}
	credentials := localHardeningTestCredentials()
	data := dataValues(cfg, credentials)
	openBao := data["openbao"].(map[string]any)
	if openBao["enabled"] != true || openBao["transitKey"] != "argus-local-hardening" {
		t.Fatalf("local-hardening data values did not enable OpenBao Transit: %#v", openBao)
	}
	platform := platformValues(cfg, credentials, "setup-secret", "idempotency", "cursor", "pending", "", strings.Repeat("a", 64))
	runtime := platform["runtime"].(map[string]any)
	if runtime["keyWrappingMode"] != "openbao_transit" || runtime["openBaoToken"] != "openbao-token" || runtime["databaseRolesEnabled"] != true {
		t.Fatalf("local-hardening platform values are incomplete: %#v", runtime)
	}
	if _, exists := runtime["secretKEKKeyring"]; exists {
		t.Fatal("local-hardening must not render the static KEK keyring")
	}
	if runtime["otelcolWindowsAmd64Uri"] != "" || runtime["otelcolSigningPublicKey"] == "" || runtime["otelcolKubernetesImage"] == "" {
		t.Fatalf("local-hardening telemetry values are invalid: %#v", runtime)
	}
}

func TestProductionRequiresExplicitDirectExecutorCapacity(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "production.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Spec.DirectExecutor = DirectExecutorCapacity{}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "spec.directExecutor") {
		t.Fatalf("Validate() error = %v, want production Direct Executor capacity error", err)
	}
}

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	data := []byte(`apiVersion: install.argus.io/v1alpha1
kind: ArgusInstallConfig
metadata: {name: test}
spec:
  profile: evaluation
  surprise: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected strict YAML decoding error")
	}
}

func TestNetworkDefaultsAndValidation(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Spec.Network.Mode != "auto" {
		t.Fatalf("network mode = %q, want auto", cfg.Spec.Network.Mode)
	}
	cfg.Spec.Network.Mode = "external"
	cfg.Spec.Network.Egress.ExpectedIPs = []string{"203.0.113.10"}
	cfg.Spec.Network.Egress.VerificationURL = "https://example.com/egress"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Spec.Network.Egress.ExpectedIPs = []string{"not-an-ip"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid expected IP to fail validation")
	}
}

func TestProtectedPrefixesIncludesAddressesAndCIDRs(t *testing.T) {
	profile := NetworkProfile{ProtectedTargets: ProtectedTargetProfile{CIDRs: []string{"10.0.0.0/24"}, Addresses: []string{netip.MustParseAddr("192.168.1.10").String()}}}
	got := protectedPrefixes(profile)
	if len(got) != 2 || got[0] != "10.0.0.0/24" || got[1] != "192.168.1.10/32" {
		t.Fatalf("protectedPrefixes() = %v", got)
	}
}
