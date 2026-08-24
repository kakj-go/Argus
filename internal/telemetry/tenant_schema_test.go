package telemetry

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type tenantSchemaTestStore struct {
	row     db.EnterpriseTelemetryTable
	err     error
	updates []db.UpsertEnterpriseTelemetryTablesParams
}

func (store *tenantSchemaTestStore) GetEnterpriseTelemetryTables(context.Context, uuid.UUID) (db.EnterpriseTelemetryTable, error) {
	return store.row, store.err
}

func (store *tenantSchemaTestStore) UpsertEnterpriseTelemetryTables(_ context.Context, update db.UpsertEnterpriseTelemetryTablesParams) error {
	store.updates = append(store.updates, update)
	store.row = db.EnterpriseTelemetryTable{
		EnterpriseID:  update.EnterpriseID,
		SchemaVersion: update.SchemaVersion,
		Status:        update.Status,
		ReadyAt:       update.ReadyAt,
		LastError:     update.LastError,
	}
	store.err = nil
	return nil
}

type tenantSchemaTestManager struct {
	ensureCalls int
	verifyCalls int
	dropCalls   int
}

func (manager *tenantSchemaTestManager) EnsureTenant(context.Context, uuid.UUID) error {
	manager.ensureCalls++
	return nil
}

func (manager *tenantSchemaTestManager) VerifyTenant(context.Context, uuid.UUID) error {
	manager.verifyCalls++
	return nil
}

func (manager *tenantSchemaTestManager) DropTenant(context.Context, uuid.UUID) error {
	manager.dropCalls++
	return nil
}

type tenantSchemaTestLocker struct{ calls int }

func (locker *tenantSchemaTestLocker) WithTenantSchemaLock(_ context.Context, _ uuid.UUID, run func() error) error {
	locker.calls++
	return run()
}

func TestEnsureTenantSchemaKeepsReadyTenantQueryable(t *testing.T) {
	enterpriseID := uuid.New()
	store := &tenantSchemaTestStore{row: db.EnterpriseTelemetryTable{
		EnterpriseID: enterpriseID, SchemaVersion: int32(TelemetrySchemaVersion), Status: "ready",
	}}
	manager := &tenantSchemaTestManager{}
	locker := &tenantSchemaTestLocker{}
	lifecycle := TenantSchemaLifecycle{Manager: manager, Queries: store, Locker: locker}

	if err := lifecycle.EnsureTenantSchema(context.Background(), enterpriseID); err != nil {
		t.Fatal(err)
	}
	if manager.verifyCalls != 1 || manager.ensureCalls != 0 || manager.dropCalls != 0 {
		t.Fatalf("ready tenant calls verify=%d ensure=%d drop=%d", manager.verifyCalls, manager.ensureCalls, manager.dropCalls)
	}
	if len(store.updates) != 0 {
		t.Fatalf("ready tenant was moved through transient states: %#v", store.updates)
	}
	if locker.calls != 1 {
		t.Fatalf("tenant schema lock calls = %d, want 1", locker.calls)
	}
}

func TestEnsureTenantSchemaCreatesMissingTenantUnderLock(t *testing.T) {
	enterpriseID := uuid.New()
	store := &tenantSchemaTestStore{err: pgx.ErrNoRows}
	manager := &tenantSchemaTestManager{}
	locker := &tenantSchemaTestLocker{}
	lifecycle := TenantSchemaLifecycle{Manager: manager, Queries: store, Locker: locker}

	if err := lifecycle.EnsureTenantSchema(context.Background(), enterpriseID); err != nil {
		t.Fatal(err)
	}
	if manager.ensureCalls != 1 || manager.verifyCalls != 1 || manager.dropCalls != 0 {
		t.Fatalf("new tenant calls verify=%d ensure=%d drop=%d", manager.verifyCalls, manager.ensureCalls, manager.dropCalls)
	}
	if len(store.updates) != 2 || store.updates[0].Status != "pending" || store.updates[1].Status != "ready" {
		t.Fatalf("new tenant state transitions = %#v", store.updates)
	}
	if locker.calls != 1 {
		t.Fatalf("tenant schema lock calls = %d, want 1", locker.calls)
	}
}

func TestDropTenantSchemaDoesNotRepeatCompletedDeletion(t *testing.T) {
	enterpriseID := uuid.New()
	store := &tenantSchemaTestStore{row: db.EnterpriseTelemetryTable{
		EnterpriseID: enterpriseID, SchemaVersion: int32(TelemetrySchemaVersion), Status: "deleting",
	}}
	manager := &tenantSchemaTestManager{}
	locker := &tenantSchemaTestLocker{}
	lifecycle := TenantSchemaLifecycle{Manager: manager, Queries: store, Locker: locker}

	if err := lifecycle.DropTenantSchema(context.Background(), enterpriseID); err != nil {
		t.Fatal(err)
	}
	if manager.dropCalls != 0 || len(store.updates) != 0 || locker.calls != 1 {
		t.Fatalf("completed deletion repeated: drops=%d updates=%d locks=%d", manager.dropCalls, len(store.updates), locker.calls)
	}
}

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
