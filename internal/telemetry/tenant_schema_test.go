package telemetry

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTenantTableRouterUsesCanonicalUUIDSuffix(t *testing.T) {
	id := uuid.MustParse("6f4a7c3e-9b2d-4f11-a2d0-5a7e9b31e921")
	tables, err := (TenantTableRouter{}).Tables(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{tables.MetricSeries, tables.MetricSamples, tables.Logs, tables.Traces, tables.TraceSummary, tables.TraceSpanEdges} {
		if !strings.HasSuffix(table, "6f4a7c3e9b2d4f11a2d05a7e9b31e921") {
			t.Fatalf("table %q does not use canonical tenant suffix", table)
		}
		if strings.Contains(table, "-") {
			t.Fatalf("table %q contains a separator", table)
		}
	}
}

func TestTenantTableRouterRejectsUnknownTable(t *testing.T) {
	if _, err := (TenantTableRouter{}).Table("arbitrary", uuid.New()); err == nil {
		t.Fatal("expected unknown table rejection")
	}
}

func TestTenantTableExpectationsCoverEveryDDLTable(t *testing.T) {
	tables, err := (TenantTableRouter{}).Tables(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	expectations := tenantTableExpectations(tables)
	for _, table := range []string{tables.MetricSeries, tables.MetricSamples, tables.Logs, tables.Traces, tables.TraceSummary, tables.TraceSpanEdges} {
		expectation, ok := expectations[table]
		if !ok {
			t.Fatalf("missing schema expectation for %s", table)
		}
		if expectation.SortingKey == "" || len(expectation.Columns) == 0 {
			t.Fatalf("incomplete schema expectation for %s", table)
		}
		if expectation.Columns["expires_at"] != "DateTime64(3,'UTC')" {
			t.Fatalf("table %s does not validate the TTL column", table)
		}
	}
}

func TestNormalizeSchemaExpressionAcceptsClickHouseTupleFormatting(t *testing.T) {
	actual := normalizeSchemaExpression("tuple(`metric_name`, labels_hash, resource_id, series_id)")
	want := normalizeSchemaExpression("metric_name, labels_hash, resource_id, series_id")
	if actual != want {
		t.Fatalf("normalized sorting key = %q, want %q", actual, want)
	}
}
