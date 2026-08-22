package telemetry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClaimAndNodeBindingSQLPreservesIsolationAndEvidenceState(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	migration := readTestFile(t, filepath.Join(root, "migrations", "postgresql", "00006_m7_telemetry.sql"))
	queries := readTestFile(t, filepath.Join(root, "internal", "storage", "postgres", "queries", "telemetry_control.sql"))
	for _, required := range []string{
		"CREATE UNIQUE INDEX collection_claims_active_primary ON collection_claims (enterprise_id, physical_resource_ref, claim_type, selector_hash)",
		"FOREIGN KEY (primary_claim_id, enterprise_id) REFERENCES collection_claims(id, enterprise_id)",
		"CREATE UNIQUE INDEX collection_claims_active_migration_per_collector",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("M7 Claim migration is missing invariant %q", required)
		}
	}
	for _, required := range []string{
		"THEN kubernetes_node_host_bindings.host_id ELSE EXCLUDED.host_id END",
		"AND status = 'proposed'",
		"physical_resource_ref = 'host:' || sqlc.narg('physical_resource_ref')",
		"physical_resource_ref = 'kubernetes_cluster:' || sqlc.narg('physical_resource_ref')",
	} {
		if !strings.Contains(queries, required) {
			t.Fatalf("M7 Claim/Binding query is missing invariant %q", required)
		}
	}
}

func TestTelemetrySchemaV3UsesTenantBootstrap(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	migration := readTestFile(t, filepath.Join(root, "migrations", "clickhouse", "00001_m7_telemetry.sql"))
	for _, required := range []string{"Schema v3", "SELECT 3 WHERE NOT EXISTS", "schema_versions", "metric_series_local"} {
		if !strings.Contains(migration, required) {
			t.Fatalf("telemetry schema v3 is missing invariant %q", required)
		}
	}
	chartMigration := readTestFile(t, filepath.Join(root, "deploy", "helm", "argus-telemetry-pipeline", "files", "00001_m7_telemetry.sql"))
	if migration != chartMigration {
		t.Fatal("ClickHouse migration and Helm schema copy differ")
	}
}

func TestTelemetryQueryRoleHasOnlyRequiredTenantLifecycleWrites(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	migration := readTestFile(t, filepath.Join(root, "migrations", "postgresql", "00008_m10_telemetry_tenants.sql"))
	for _, required := range []string{
		"GRANT SELECT, INSERT, UPDATE ON enterprise_telemetry_tables TO argus_telemetry_query",
		"GRANT SELECT, INSERT, UPDATE ON audit_chain_heads TO argus_telemetry_query",
		"GRANT INSERT ON audit_events TO argus_telemetry_query",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("M10 query role migration is missing %q", required)
		}
	}
	if strings.Contains(migration, "GRANT DELETE ON enterprise_telemetry_tables TO argus_telemetry_query") {
		t.Fatal("M10 query role unexpectedly has tenant readiness delete permission")
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}
