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
