package action

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

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
