package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestToConversationRunIncludesTerminalReason(t *testing.T) {
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	value := db.Run{
		ID: uuid.New(), ConversationID: uuid.New(), EnterpriseID: uuid.New(), ModelID: uuid.New(),
		ModelRevision: 1, Locale: "en-US", Status: "failed", AuthorizationVersion: 1, Version: 2,
		StopReason: pgtype.Text{String: "quota_exceeded", Valid: true},
		ErrorCode:  pgtype.Text{String: "MODEL_QUOTA_EXCEEDED", Valid: true},
		CreatedAt:  now, UpdatedAt: now,
	}

	result := toConversationRun(value)
	if result.StopReason == nil || *result.StopReason != value.StopReason.String {
		t.Fatalf("stop_reason = %v", result.StopReason)
	}
	if result.ErrorCode == nil || *result.ErrorCode != value.ErrorCode.String {
		t.Fatalf("error_code = %v", result.ErrorCode)
	}
}
