package collectormanager

import (
	"strings"
	"testing"
)

func TestSSHActivateCommandRequiresStableService(t *testing.T) {
	command := sshActivateCommand("/var/lib/argus-otelcol/test-collector")
	if strings.Count(command, "systemctl is-active --quiet argus-otelcol.service") != 2 || !strings.Contains(command, "; sleep 2; ") {
		t.Fatalf("Collector activation does not verify stable service state: %q", command)
	}
}
