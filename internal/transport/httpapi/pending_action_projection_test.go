package httpapi

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/kakj-go/Argus/internal/gen/openapi/actionapi"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestConvertPendingExposesStableActionType(t *testing.T) {
	now := time.Now().UTC()
	result := convertPending[actionapi.PendingActionPublicSchema](db.PendingAction{
		ActionRef:  "pa-test",
		ActionType: "bastion_scope.create",
		Title:      "Create bastion",
		Summary:    "Create a stable Bastion Scope",
		Risk:       "dangerous",
		Preview:    []byte(`{"name":"edge-gateway"}`),
		Diff:       []byte(`[{"kind":"add","text":"Create bastion edge-gateway"}]`),
		Status:     "awaiting_confirmation",
		ExpiresAt:  pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true},
		CreatedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:  pgtype.Timestamptz{Time: now, Valid: true},
	})

	require.Equal(t, "bastion_scope.create", result.ActionType)
}
