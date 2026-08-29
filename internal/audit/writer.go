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
	"go.opentelemetry.io/otel/trace"

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
	"trace_id": true, "outcome": true, "reason_code": true, "reason_codes": true,
	"snapshot_hash": true, "matched_grants": true, "matched_rules": true,
	"approval_workflows": true, "session_profile_sources": true, "source_versions": true,
	"request_id": true, "lease_id": true, "remote_session_id": true, "target_summary": true,
	"recording_id": true, "grant_id": true, "host_id": true, "managed_account_id": true,
	"protocol": true, "connection_mode": true, "session_fence": true,
	"recording_status": true, "chunk_count": true, "event_count": true,
	"retention_until": true, "result_count": true, "chunk_sequence": true,
	"id": true, "version": true, "subject_type": true, "subject_id": true,
	"host_ids": true, "managed_account_ids": true, "protocols": true, "actions": true,
	"source_cidrs": true, "effects": true, "workflow_id": true, "profile_id": true,
	"minimum_approvals": true, "approver_role_ids": true, "separation_of_duties": true,
	"timeout_effect": true, "escalation_role_ids": true, "recording_mode": true,
	"command_audit_mode": true, "clipboard_mode": true, "file_upload_mode": true,
	"file_download_mode": true, "port_forward_mode": true, "session_share_mode": true,
	"max_session_seconds": true, "idle_timeout_seconds": true, "retention_days": true,
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
	TraceID      string
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
	detailValues := make(map[string]any, len(entry.Details)+1)
	for key, value := range entry.Details {
		detailValues[key] = value
	}
	detailValues["trace_id"] = auditTraceID(ctx, entry.TraceID)
	details, err := json.Marshal(sanitizeDetails(detailValues))
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
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		var normalized any
		if json.Unmarshal(encoded, &normalized) != nil {
			return fmt.Sprint(typed)
		}
		return sanitizeValue(normalized)
	}
}

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, fragment := range []string{"password", "token", "secret", "credential", "csrf", "cookie", "http_session", "api_key"} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func auditTraceID(ctx context.Context, explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		return spanContext.TraceID().String()
	}
	return strings.ReplaceAll(uuid.NewString(), "-", "")
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
