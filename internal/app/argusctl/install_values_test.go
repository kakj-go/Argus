package argusctl

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/release"
)

func TestLocalHardeningInstallerValuesRenderCharts(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "local-hardening.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	credentials := localHardeningTestCredentials()
	tests := []struct {
		name      string
		chartName string
		namespace string
		values    map[string]any
	}{
		{name: "data", chartName: "argus-data", namespace: cfg.Spec.Namespaces.System, values: dataValues(cfg, credentials)},
		{name: "platform", chartName: "argus-platform", namespace: cfg.Spec.Namespaces.System, values: platformValues(cfg, credentials, "setup-secret", "idempotency", "cursor", "pending", "")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded, err := loadLocalChart(root, test.chartName)
			if err != nil {
				t.Fatal(err)
			}
			configuration := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
			install := action.NewInstall(configuration)
			install.ReleaseName = "argus-local-render"
			install.Namespace = test.namespace
			install.DryRunStrategy = action.DryRunClient
			if _, err := install.Run(loaded, test.values); err != nil {
				t.Fatalf("render %s chart with installer values: %v", test.chartName, err)
			}
		})
	}
}

func TestPlatformChartAllowsTelemetryToBeDisabled(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "local-hardening.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "")
	runtimeValues := values["runtime"].(map[string]any)
	runtimeValues["telemetryToolCatalogEnabled"] = false
	runtimeValues["otelcolLinuxArm64Uri"] = ""
	runtimeValues["otelcolLinuxArm64Sha256"] = ""
	runtimeValues["otelcolLinuxArm64Signature"] = ""
	runtimeValues["otelcolLinuxArm64ByteSize"] = 0
	runtimeValues["otelcolSigningKeyId"] = ""
	runtimeValues["otelcolSigningPublicKey"] = ""

	loaded, err := loadLocalChart(root, "argus-platform")
	if err != nil {
		t.Fatal(err)
	}
	configuration := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
	install := action.NewInstall(configuration)
	install.ReleaseName = "argus-no-telemetry-render"
	install.Namespace = cfg.Spec.Namespaces.System
	install.DryRunStrategy = action.DryRunClient
	rendered, err := install.Run(loaded, values)
	if err != nil {
		t.Fatalf("render platform chart with telemetry disabled: %v", err)
	}
	accessor, err := release.NewAccessor(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(accessor.Manifest(), "argus-telemetry-catalog-sync") ||
		strings.Contains(accessor.Manifest(), "argus-telemetry-ingest") ||
		strings.Contains(accessor.Manifest(), "argus-server-telemetry-client-tls") {
		t.Fatal("telemetry resources rendered while telemetry was disabled")
	}
}

func localHardeningTestCredentials() map[string]string {
	return map[string]string{
		"postgresql-password": "postgres", "redis-password": "redis", "minio-root-user": "minio", "minio-root-password": "minio-secret",
		"clickhouse-password": "clickhouse", "telemetry-clickhouse-migration-password": "ch-migration", "telemetry-clickhouse-writer-password": "ch-writer",
		"telemetry-clickhouse-query-password": "ch-query", "openbao-token": "openbao-token", "server-database-password": "server",
		"worker-database-password": "worker", "gateway-database-password": "gateway", "direct-executor-database-password": "direct",
		"migration-database-password": "migration", "telemetry-ingest-database-password": "ingest", "telemetry-writer-database-password": "writer",
	}
}
