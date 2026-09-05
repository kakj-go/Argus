package action

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestExecutionFailureCodeDoesNotExposeWorkerInternals(t *testing.T) {
	t.Parallel()
	if got := executionFailureCode(errors.New("database detail")); got != "ACTION_EXECUTION_FAILED" {
		t.Fatalf("generic failure code = %q", got)
	}
	if got := executionFailureCode(runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: errors.New("stale")}); got != "ACTION_INVALIDATED" {
		t.Fatalf("invalidated failure code = %q", got)
	}
	if got := executionFailureCode(runtime.Error{ErrorCode: "RESOURCE_NAME_CONFLICT", Cause: errors.New("duplicate"), Permanent: true}); got != "RESOURCE_NAME_CONFLICT" {
		t.Fatalf("resource name failure code = %q", got)
	}
}

func TestResourceNameConflictClassificationIsNarrow(t *testing.T) {
	t.Parallel()
	if !resourceNameConflict(&pgconn.PgError{Code: "23505", ConstraintName: "hosts_name_unique"}) {
		t.Fatal("live host-name conflict was not classified")
	}
	if resourceNameConflict(&pgconn.PgError{Code: "23505", ConstraintName: "other_unique"}) {
		t.Fatal("unrelated unique constraint was classified as a resource name conflict")
	}
	if resourceNameConflict(errors.New("duplicate")) {
		t.Fatal("plain error was classified as a resource name conflict")
	}
}

func TestReconciledCommandOutcomeWaitsForTerminalFact(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"queued", "dispatched", "acknowledged", "running", "delivery_unknown", "result_unknown"} {
		if result, _, terminal := reconciledCommandOutcome(db.ConnectorCommand{Status: status}); terminal || result != "" {
			t.Fatalf("status %q unexpectedly finalized as %q", status, result)
		}
	}
}

func TestReconciledCommandOutcomeMapsTerminalFacts(t *testing.T) {
	t.Parallel()
	status, code, terminal := reconciledCommandOutcome(db.ConnectorCommand{Status: "succeeded", ErrorCode: pgtype.Text{String: "stale", Valid: true}})
	if !terminal || status != "succeeded" || code.Valid {
		t.Fatalf("success outcome = (%q, %#v, %t)", status, code, terminal)
	}

	status, code, terminal = reconciledCommandOutcome(db.ConnectorCommand{Status: "timed_out"})
	if !terminal || status != "failed" || !code.Valid || code.String != "EXECUTION_RESULT_UNKNOWN" {
		t.Fatalf("timeout outcome = (%q, %#v, %t)", status, code, terminal)
	}

	status, code, terminal = reconciledCommandOutcome(db.ConnectorCommand{Status: "failed", ErrorCode: pgtype.Text{String: "CONNECTOR_REJECTED", Valid: true}})
	if !terminal || status != "failed" || code.String != "CONNECTOR_REJECTED" {
		t.Fatalf("failed outcome = (%q, %#v, %t)", status, code, terminal)
	}
}

func TestTerminalTelemetryTunnelFailureUsesStableCodes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		tunnel db.TelemetryTunnel
		code   string
		failed bool
	}{
		{tunnel: db.TelemetryTunnel{Status: "down", LastDropReason: "loopback_port_conflict"}, code: "COLLECTOR_LOOPBACK_PORT_CONFLICT", failed: true},
		{tunnel: db.TelemetryTunnel{Status: "down", LastDropReason: "credential_revoked"}, code: "CREDENTIAL_UNAVAILABLE", failed: true},
		{tunnel: db.TelemetryTunnel{Status: "removed"}, code: "COLLECTOR_MANAGEMENT_FAILED", failed: true},
		{tunnel: db.TelemetryTunnel{Status: "down", ReconnectAttempt: 30}},
		{tunnel: db.TelemetryTunnel{Status: "establishing"}},
	} {
		code, failed := terminalTelemetryTunnelFailure(test.tunnel)
		if code != test.code || failed != test.failed {
			t.Fatalf("tunnel %#v = (%q, %t), want (%q, %t)", test.tunnel, code, failed, test.code, test.failed)
		}
	}
}
