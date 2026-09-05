package config

import "testing"

func TestDirectExecutorProductionTunnelLimitsHaveNoImplicitDefaults(t *testing.T) {
	t.Setenv("ARGUS_DEPLOYMENT_PROFILE", "production")
	t.Setenv("ARGUS_TELEMETRY_TUNNEL_LIMIT", "")
	t.Setenv("ARGUS_CONTROL_TUNNEL_LIMIT", "")
	t.Setenv("ARGUS_TUNNEL_BYTES_PER_SECOND", "")
	configuration := LoadDirectExecutor()
	if configuration.TelemetryTunnelLimit != 0 || configuration.ControlTunnelLimit != 0 || configuration.TunnelBytesPerSecond != 0 {
		t.Fatalf("production tunnel limits received implicit defaults: %#v", configuration)
	}
}

func TestDirectExecutorEvaluationTunnelDefaults(t *testing.T) {
	t.Setenv("ARGUS_DEPLOYMENT_PROFILE", "evaluation")
	t.Setenv("ARGUS_TELEMETRY_TUNNEL_LIMIT", "")
	t.Setenv("ARGUS_CONTROL_TUNNEL_LIMIT", "")
	t.Setenv("ARGUS_TUNNEL_BYTES_PER_SECOND", "")
	configuration := LoadDirectExecutor()
	if configuration.TelemetryTunnelLimit != 64 || configuration.ControlTunnelLimit != 32 || configuration.TunnelBytesPerSecond != 64*1024*1024 {
		t.Fatalf("unexpected evaluation tunnel limits: %#v", configuration)
	}
}
