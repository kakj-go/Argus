package queryengine

import (
	"context"
	"errors"
	"testing"
	"time"

	clickhouseproto "github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	postgresdb "github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type captureEngine struct {
	request Request
}

type captureAudit struct{ event AuditEvent }

func (sink *captureAudit) Record(_ context.Context, event AuditEvent) error {
	sink.event = event
	return nil
}

func (e *captureEngine) Execute(_ context.Context, request Request) (Result, error) {
	e.request = request
	return Result{Language: LanguagePromQL, ResultType: "vector"}, nil
}

func TestCoordinatorPreservesPromQLInstantMode(t *testing.T) {
	engine := &captureEngine{}
	coordinator := &Coordinator{PromQL: engine}
	_, err := coordinator.Execute(context.Background(), Request{
		Language:   LanguagePromQL,
		Expression: "up",
		Instant:    true,
		Start:      time.Unix(10, 0),
		End:        time.Unix(20, 0),
		Scope:      Scope{EnterpriseID: uuid.New()},
		Budget:     Budget{Timeout: time.Second, MaxRows: 10, MaxSamples: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !engine.request.Instant {
		t.Fatal("instant query mode was dropped by the coordinator")
	}
}

func TestCoordinatorProjectsSensitiveKQLFieldsAndAudits(t *testing.T) {
	audit := &captureAudit{}
	engine := &captureEngineResult{result: Result{Language: LanguageKQL, ResultType: "log_entries", Data: []map[string]any{{"body": "secret", "service_name": "api"}}}}
	coordinator := &Coordinator{KQL: engine, Audit: audit}
	result, err := coordinator.Execute(context.Background(), Request{Language: LanguageKQL, Expression: "service_name = api", Scope: Scope{EnterpriseID: uuid.New()}, Budget: Budget{Timeout: time.Second, MaxRows: 10, MaxSamples: 10}})
	if err != nil {
		t.Fatal(err)
	}
	rows := result.Data.([]map[string]any)
	if rows[0]["body"] != "[REDACTED]" || rows[0]["service_name"] != "api" {
		t.Fatalf("unexpected projected result: %#v", rows)
	}
	if !audit.event.Success || audit.event.ExpressionHash == "" || audit.event.PlanHash == "" {
		t.Fatalf("audit event was not populated: %#v", audit.event)
	}
	if result.Meta.Warnings == nil {
		t.Fatal("warnings must be an empty array, not null")
	}
}

type captureEngineResult struct{ result Result }

func (engine *captureEngineResult) Execute(context.Context, Request) (Result, error) {
	return engine.result, nil
}

func TestCoordinatorRejectsResultOverBudget(t *testing.T) {
	coordinator := &Coordinator{KQL: &captureEngineResult{result: Result{Language: LanguageKQL, Data: []map[string]any{{"body": "large"}}}}}
	_, err := coordinator.Execute(context.Background(), Request{Language: LanguageKQL, Expression: "body : large", Scope: Scope{EnterpriseID: uuid.New()}, Budget: Budget{Timeout: time.Second, MaxRows: 10, MaxSamples: 10, MaxResultBytes: 1}})
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("expected result budget error, got %v", err)
	}
}

func TestPersistentAuditErrorDoesNotStoreQueryErrorText(t *testing.T) {
	event := AuditEvent{Success: false, Error: `parse failed near "secret query text"`}
	if got := persistentAuditError(event); got != "query_failed" {
		t.Fatalf("persistent audit error = %q", got)
	}
	if got := persistentAuditError(AuditEvent{Success: true}); got != "" {
		t.Fatalf("successful persistent audit error = %q", got)
	}
}

type auditTransactionStore struct {
	errors []error
	calls  int
}

func (store *auditTransactionStore) InTx(_ context.Context, _ func(*postgresdb.Queries) error) error {
	store.calls++
	if store.calls <= len(store.errors) {
		return store.errors[store.calls-1]
	}
	return nil
}

func TestPersistentAuditRetriesTransientPostgresTransactions(t *testing.T) {
	store := &auditTransactionStore{errors: []error{
		&pgconn.PgError{Code: "40001"},
		&pgconn.PgError{Code: "40P01"},
	}}
	if err := (PersistentAuditSink{Store: store}).Record(context.Background(), AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	if store.calls != 3 {
		t.Fatalf("transaction calls = %d, want 3", store.calls)
	}
}

func TestPersistentAuditDoesNotRetryPermanentPostgresError(t *testing.T) {
	store := &auditTransactionStore{errors: []error{&pgconn.PgError{Code: "23505"}}}
	err := (PersistentAuditSink{Store: store}).Record(context.Background(), AuditEvent{})
	if err == nil {
		t.Fatal("expected permanent audit error")
	}
	if store.calls != 1 {
		t.Fatalf("transaction calls = %d, want 1", store.calls)
	}
}

func TestPersistentAuditRetryHonorsContextCancellation(t *testing.T) {
	store := &auditTransactionStore{errors: []error{&pgconn.PgError{Code: "40001"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (PersistentAuditSink{Store: store}).Record(ctx, AuditEvent{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("transaction calls = %d, want 1", store.calls)
	}
}

func TestNormalizeExecutionErrorClassifiesClickHouseBudgetLimits(t *testing.T) {
	err := normalizeExecutionError(&clickhouseproto.Exception{Code: 307, Message: "max bytes to read exceeded"})
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("expected ClickHouse scan limit to map to ErrBudget, got %v", err)
	}
	invalid := &clickhouseproto.Exception{Code: 62, Message: "syntax error"}
	if got := normalizeExecutionError(invalid); !errors.Is(got, invalid) {
		t.Fatalf("non-budget ClickHouse error was reclassified: %v", got)
	}
}
