package connector

import (
	"testing"

	"github.com/google/uuid"
)

func TestIdentityEndpointAddressPreservesHostAndPort(t *testing.T) {
	for input, want := range map[string]string{
		"https://telemetry.example.test:4318/v1/identity/enroll": "telemetry.example.test:4318",
		"https://telemetry.example.test/v1/identity/enroll":      "telemetry.example.test:443",
		"127.0.0.1:14318": "127.0.0.1:14318",
	} {
		got, err := identityEndpointAddress(input)
		if err != nil || got != want {
			t.Fatalf("identityEndpointAddress(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := identityEndpointAddress("telemetry-without-port"); err == nil {
		t.Fatal("identity target without a port was accepted")
	}
}

func TestConnectorTunnelOwnerFencesConnectionEpoch(t *testing.T) {
	connectorID := uuid.New()
	first := connectorTunnelOwner(connectorID, 1)
	second := connectorTunnelOwner(connectorID, 2)
	if first == second || first != "connector:"+connectorID.String()+":1" {
		t.Fatalf("unexpected connector tunnel owners %q and %q", first, second)
	}
}

func TestConnectorTunnelLimitIsBounded(t *testing.T) {
	if got := (Gateway{TelemetryTunnelLimit: 1000}).telemetryTunnelLimit(); got != defaultConnectorTelemetryTunnelLimit {
		t.Fatalf("unbounded Connector tunnel limit: %d", got)
	}
	if got := (Gateway{TelemetryTunnelLimit: 12}).telemetryTunnelLimit(); got != 12 {
		t.Fatalf("configured Connector tunnel limit ignored: %d", got)
	}
}
