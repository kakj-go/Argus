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

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}
