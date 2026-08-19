package httpapi

import (
	"net/http"
	"testing"

	telemetryapi "github.com/kakj-go/Argus/internal/gen/openapi/telemetryapi"
)

func TestTelemetryAuthStatusPreservesAuthorizationVersionConflict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code   string
		status int
	}{
		{"AUTHORIZATION_VERSION_STALE", http.StatusConflict},
		{"SESSION_EXPIRED", http.StatusUnauthorized},
		{"SESSION_REVOKED", http.StatusUnauthorized},
		{"AUTHORIZATION_DENIED", http.StatusForbidden},
	}
	for _, test := range tests {
		if got := telemetryAuthStatus(telemetryapi.ApiError{Code: test.code}); got != test.status {
			t.Fatalf("telemetryAuthStatus(%q) = %d, want %d", test.code, got, test.status)
		}
	}
}
