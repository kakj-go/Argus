package argusctl

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/trustbundle"
)

func TestPKIRepairCommandsUsePinnedBundleAndExistingBinaries(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.Exposure.EnterpriseHost = "argus.example"
	cfg.Spec.Telemetry.ExternalIngestHost = "telemetry.argus.example"
	bundle := trustbundle.Bundle{Epoch: 4, Material: trustbundle.Material{PEM: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"),
		SHA256: strings.Repeat("a", 64)}}
	commands := []string{
		linuxConnectorRepairCommand(cfg, "one-time-token", bundle, "linux-system"),
		linuxConnectorRepairCommand(cfg, "one-time-token", bundle, "linux-user"),
		linuxCollectorRepairCommand(cfg, "one-time-token", bundle, "linux-system"),
		linuxCollectorRepairCommand(cfg, "one-time-token", bundle, "linux-user"),
		kubernetesConnectorRepairCommand(cfg, uuid.New(), "one-time-token", bundle, "argus-system"),
		kubernetesCollectorRepairCommand(cfg, uuid.New(), "one-time-token", bundle, "argus-system"),
	}
	for _, command := range commands {
		if strings.Contains(command, "--insecure") || strings.Contains(command, " -k") || strings.Contains(command, "curl") {
			t.Fatalf("repair command contains an unsafe or unnecessary downloader:\n%s", command)
		}
		if !strings.Contains(command, "one-time-token") {
			t.Fatal("repair command omitted the one-time identity credential")
		}
	}
	for _, command := range commands[:4] {
		if !strings.Contains(command, "sha256sum -c") || !strings.Contains(command, "openssl crl2pkcs7") {
			t.Fatalf("Linux repair command does not validate its embedded Bundle:\n%s", command)
		}
	}
	if !strings.Contains(commands[5], "repair-collector") || !strings.Contains(commands[5], "rollout status") {
		t.Fatal("Kubernetes Collector repair does not replace identity and verify its rollout")
	}
}

func TestPOSIXQuoteDoesNotExposeSingleQuotes(t *testing.T) {
	if got := posixQuote("a'b"); got != `'a'"'"'b'` {
		t.Fatalf("unexpected POSIX quote: %s", got)
	}
}
