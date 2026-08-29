package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSanitizeDetailsDropsSensitiveFieldsAtEveryDepth(t *testing.T) {
	input := map[string]any{
		"summary":  "user credential rotated",
		"password": "top-level-password",
		"before": map[string]any{
			"summary":       "before",
			"session_token": "nested-session",
			"after": map[string]any{
				"status":         "active",
				"api_key_secret": "nested-api-key",
			},
		},
		"after": []any{
			map[string]any{"status": "disabled", "csrf_token": "nested-csrf"},
		},
	}

	encoded, err := json.Marshal(sanitizeDetails(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"top-level-password", "nested-session", "nested-api-key", "nested-csrf"} {
		if strings.Contains(text, secret) {
			t.Fatalf("sanitized audit details leaked %q: %s", secret, text)
		}
	}
	for _, expected := range []string{"user credential rotated", "before", "active", "disabled"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("sanitized audit details dropped allowed value %q: %s", expected, text)
		}
	}
}

func TestSensitiveKeyIsCaseInsensitive(t *testing.T) {
	for _, key := range []string{"Password", "SETUP_TOKEN", "apiKeySecret", "CsRf", "http_session_id", "Cookie", "credential"} {
		if !sensitiveKey(key) {
			t.Fatalf("expected %q to be sensitive", key)
		}
	}
}

func TestSanitizeDetailsKeepsRemoteAccessDecisionTrace(t *testing.T) {
	input := map[string]any{
		"snapshot_hash":     "abc123",
		"reason_codes":      []string{"MFA_REQUIRED"},
		"matched_rules":     []map[string]any{{"id": "rule-1", "version": 7}},
		"request_id":        "request-1",
		"lease_id":          "lease-1",
		"remote_session_id": "session-1",
	}
	encoded, err := json.Marshal(sanitizeDetails(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, expected := range []string{"abc123", "MFA_REQUIRED", "rule-1", "request-1", "lease-1", "session-1"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("sanitized audit details dropped %q: %s", expected, text)
		}
	}
}

func TestSanitizeDetailsKeepsRemoteAccessRuntimeTrace(t *testing.T) {
	input := map[string]any{
		"request_id": uuid.New(), "lease_id": uuid.New(), "remote_session_id": uuid.New(), "recording_id": uuid.New(),
		"grant_id": uuid.New(), "host_id": uuid.New(), "managed_account_id": uuid.New(), "protocol": "ssh",
		"connection_mode": "direct_ssh", "session_fence": int64(3), "recording_status": "available",
		"chunk_count": int32(2), "event_count": int64(8), "result_count": 1,
	}
	encoded, err := json.Marshal(sanitizeDetails(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"request_id", "lease_id", "remote_session_id", "recording_id", "grant_id", "host_id", "managed_account_id", "protocol", "session_fence", "chunk_count"} {
		if !bytes.Contains(encoded, []byte(`"`+key+`"`)) {
			t.Fatalf("runtime audit field %s was removed: %s", key, encoded)
		}
	}
}

func TestAuditTraceIDAlwaysExists(t *testing.T) {
	value := auditTraceID(context.Background(), "")
	if len(value) != 32 {
		t.Fatalf("trace id length = %d, want 32", len(value))
	}
}
