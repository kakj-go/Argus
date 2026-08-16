package audit

import (
	"encoding/json"
	"strings"
	"testing"
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
	for _, key := range []string{"Password", "SETUP_TOKEN", "apiKeySecret", "CsRf", "session_id", "credential"} {
		if !sensitiveKey(key) {
			t.Fatalf("expected %q to be sensitive", key)
		}
	}
}
