package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	actionservice "github.com/kakj-go/Argus/internal/action"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestToActionExecutionProjectsDurableReferences(t *testing.T) {
	now := time.Now().UTC()
	executionID, operationID, resourceID := uuid.New(), uuid.New(), uuid.New()
	result := toActionExecution(actionservice.ExecutionView{
		Execution: db.Execution{
			ID: executionID, Status: "result_unknown",
			ConnectorInstallOperationID: uuid.NullUUID{UUID: operationID, Valid: true},
			CreatedAt:                   pgtype.Timestamptz{Time: now, Valid: true},
			UpdatedAt:                   pgtype.Timestamptz{Time: now, Valid: true},
		},
		Action: db.PendingAction{
			ActionRef:             "act_test",
			ResultResourceID:      uuid.NullUUID{UUID: resourceID, Valid: true},
			ResultResourceType:    pgtype.Text{String: "bastion_scope", Valid: true},
			ResultResourceVersion: pgtype.Int8{Int64: 2, Valid: true},
		},
		OneTimeResultState: "unavailable",
	})

	if result.ExecutionId != executionID.String() || result.Status != "result_unknown" {
		t.Fatalf("unexpected execution projection: %#v", result)
	}
	if result.OneTimeResultState != "unavailable" {
		t.Fatalf("one-time result state = %#v", result.OneTimeResultState)
	}
	if result.OperationRef == nil || result.OperationRef.Id != operationID || result.OperationRef.Kind != "connector_install" {
		t.Fatalf("operation ref = %#v", result.OperationRef)
	}
	if result.ResourceRef == nil || result.ResourceRef.ResourceId != resourceID.String() || result.ResourceRef.ResourceType != "bastion_scope" || result.ResourceRef.Version != 2 {
		t.Fatalf("resource ref = %#v", result.ResourceRef)
	}
}
