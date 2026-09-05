package collectormanager

import (
	"strings"
	"testing"
)

func TestSSHActivateCommandRequiresStableService(t *testing.T) {
	command := sshActivateCommand("/var/lib/argus-otelcol/test-collector")
	for _, required := range []string{"while [ \"$i\" -lt 90 ]", "systemctl is-active --quiet argus-otelcol.service",
		"identity/client.pem", "identity/client-key.pem", "identity/ca.pem", "test ! -e /etc/argus-otelcol/enrollment-token"} {
		if !strings.Contains(command, required) {
			t.Fatalf("Collector activation omitted readiness condition %q: %q", required, command)
		}
	}
}
