package argusctl

import (
	"strings"
	"testing"
)

func TestOpenSandboxSmokePodUsesWorkloadImageReference(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.Namespaces.Sandbox = "argus-sandbox"
	cfg.Spec.Images = Images{
		Mode:       "local-registry",
		Registry:   "host.docker.internal:5001",
		Tag:        "e2e-test",
		PullPolicy: "Never",
	}

	manifest := openSandboxSmokePod(cfg, "argus-opensandbox-smoke")
	if !strings.Contains(manifest, "image: host.docker.internal:5001/argus/argus-backend:e2e-test") {
		t.Fatalf("smoke pod image does not match the installed workload image:\n%s", manifest)
	}
	if strings.Contains(manifest, "image: localhost:5001/") {
		t.Fatalf("smoke pod must not use the host-only registry reference:\n%s", manifest)
	}
	if !strings.Contains(manifest, "imagePullPolicy: Never") {
		t.Fatalf("smoke pod did not preserve the configured pull policy:\n%s", manifest)
	}
}

func TestMinioSmokePodRetriesTransientWriteReadiness(t *testing.T) {
	manifest := minioSmokePod(&InstallConfig{Spec: InstallSpec{Namespaces: Namespaces{System: "argus-system"}}})
	for _, expected := range []string{"seq 1 12", "sleep 5", "MinIO object roundtrip did not become writable after bounded retries"} {
		if !strings.Contains(manifest, expected) {
			t.Fatalf("MinIO smoke pod is missing bounded retry %q:\n%s", expected, manifest)
		}
	}
}
