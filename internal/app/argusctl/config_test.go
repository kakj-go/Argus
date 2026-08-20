package argusctl

import (
	"os"
	"path/filepath"
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
	if cfg.Spec.Profile != "evaluation" || cfg.Image("argus-web") != "host.docker.internal:5001/argus/argus-web:dev" {
		t.Fatalf("unexpected config: %#v", cfg.Spec)
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
	platform := platformValues(cfg, credentials, "setup-secret", "idempotency", "cursor", "pending", "")
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
