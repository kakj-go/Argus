package argusctl

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTelemetryDLQReplayJobUsesWriterDatabaseIdentity(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.Namespaces.Observability = "argus-observability"
	cfg.Spec.ReleaseID = "argus-test"
	cfg.Spec.Images.Registry = "registry.example.com"
	cfg.Spec.Images.Tag = "test"
	cfg.Spec.Images.PullPolicy = "Never"

	manifest := telemetryDLQReplayJob("argus-dlq-replay", uuid.MustParse("00000000-0000-0000-0000-000000000001"), cfg)
	if !strings.Contains(manifest, "key: writer-database-url") {
		t.Fatalf("DLQ replay Job must use the writer database identity:\n%s", manifest)
	}
	if strings.Contains(manifest, "key: database-url") {
		t.Fatalf("DLQ replay Job references the removed generic database URL key:\n%s", manifest)
	}
}
