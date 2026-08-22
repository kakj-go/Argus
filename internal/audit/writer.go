package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var allowedDetailKeys = map[string]bool{
	"summary": true, "reason": true, "before": true, "after": true,
	"affected_count": true, "status": true, "username": true,
	"enterprise_code": true, "authorization_version": true,
	"language": true, "expression_hash": true, "plan_hash": true,
	"resource_count": true, "elapsed_ms": true, "scanned_bytes": true,
	"scanned_rows": true, "loaded_samples": true, "returned_rows": true,
	"success": true, "error": true,
}

type Entry struct {
	Domain       string
	EnterpriseID uuid.NullUUID
	ActorType    string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	Details      map[string]any
}

func InitializeChain(ctx context.Context, queries *db.Queries, domain string, enterpriseID uuid.NullUUID) error {
	return queries.InitializeAuditChain(ctx, db.InitializeAuditChainParams{
		ChainKey: chainKey(domain, enterpriseID), Domain: domain,
		EnterpriseID: enterpriseID, LastHash: make([]byte, sha256.Size),
	})
}

func Append(ctx context.Context, queries *db.Queries, entry Entry) (db.AuditEvent, error) {
	key := chainKey(entry.Domain, entry.EnterpriseID)
	head, err := queries.LockAuditChain(ctx, key)
	if err != nil {
		return db.AuditEvent{}, fmt.Errorf("lock audit chain: %w", err)
	}
	details, err := json.Marshal(sanitizeDetails(entry.Details))
	if err != nil {
		return db.AuditEvent{}, fmt.Errorf("encode audit details: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return db.AuditEvent{}, err
	}
	payload, err := json.Marshal(struct {
		ID           string          `json:"id"`
		Domain       string          `json:"domain"`
		EnterpriseID string          `json:"enterprise_id,omitempty"`
		ActorType    string          `json:"actor_type"`
		ActorID      string          `json:"actor_id"`
		Action       string          `json:"action"`
		ResourceType string          `json:"resource_type,omitempty"`
		ResourceID   string          `json:"resource_id,omitempty"`
		Result       string          `json:"result"`
		Details      json.RawMessage `json:"details"`
	}{id.String(), entry.Domain, nullUUIDString(entry.EnterpriseID), entry.ActorType, entry.ActorID,
		entry.Action, entry.ResourceType, entry.ResourceID, entry.Result, details})
	if err != nil {
		return db.AuditEvent{}, err
	}
	hasher := sha256.New()
	_, _ = hasher.Write(head.LastHash)
	_, _ = hasher.Write(payload)
	eventHash := hasher.Sum(nil)
	event, err := queries.InsertAuditEvent(ctx, db.InsertAuditEventParams{
		ID: id, Domain: entry.Domain, EnterpriseID: entry.EnterpriseID,
		ActorType: entry.ActorType, ActorID: entry.ActorID, Action: entry.Action,
		ResourceType: nullableText(entry.ResourceType), ResourceID: nullableText(entry.ResourceID),
		Result: entry.Result, Details: details, PreviousHash: head.LastHash, EventHash: eventHash,
	})
	if err != nil {
		return db.AuditEvent{}, fmt.Errorf("insert audit event: %w", err)
	}
	if err := queries.AdvanceAuditChain(ctx, db.AdvanceAuditChainParams{ChainKey: key, LastEventID: uuid.NullUUID{UUID: id, Valid: true}, LastHash: eventHash}); err != nil {
		return db.AuditEvent{}, fmt.Errorf("advance audit chain: %w", err)
	}
	return event, nil
}

func sanitizeDetails(input map[string]any) map[string]any {
	result := make(map[string]any)
	for key, value := range input {
		if allowedDetailKeys[key] && !sensitiveKey(key) {
			result[key] = sanitizeValue(value)
		}
	}
	return result
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeDetails(typed)
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, sanitizeValue(item))
		}
		return result
	case string, bool, float64, int, int64, nil:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, fragment := range []string{"password", "token", "secret", "credential", "csrf", "session", "api_key"} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func chainKey(domain string, enterpriseID uuid.NullUUID) string {
	if enterpriseID.Valid {
		return "enterprise:" + enterpriseID.UUID.String()
	}
	return domain
}

func nullUUIDString(value uuid.NullUUID) string {
	if value.Valid {
		return value.UUID.String()
	}
	return ""
}

func nullableText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func HashHex(value []byte) string { return hex.EncodeToString(value) }
