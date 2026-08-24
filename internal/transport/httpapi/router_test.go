package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kakj-go/Argus/internal/transport/httpapi"
)

type readinessFunc func(context.Context) error

func (function readinessFunc) Ready(ctx context.Context) error {
	return function(ctx)
}

func TestRequestLogIncludesCorrelationFields(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	request.Header.Set("X-Request-ID", "request-correlation-test")
	responseRecorder := httptest.NewRecorder()
	httpapi.NewRouterWithOptions(httpapi.RouterOptions{Logger: logger}).ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusNotFound)
	}
	logLine := output.String()
	for _, fragment := range []string{`"request_id":"request-correlation-test"`, `"path":"/missing"`, `"status":404`} {
		if !strings.Contains(logLine, fragment) {
			t.Fatalf("request log %q does not contain %q", logLine, fragment)
		}
	}
}

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path       string
		wantStatus string
	}{
		{path: "/healthz", wantStatus: "ok"},
		{path: "/readyz", wantStatus: "ready"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			responseRecorder := httptest.NewRecorder()
			httpapi.NewRouter().ServeHTTP(responseRecorder, request)

			if responseRecorder.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
			}

			var body struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(responseRecorder.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q", body.Status, test.wantStatus)
			}
		})
	}
}

func TestLanguageNegotiation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header string
		want   string
	}{
		{header: "en-US,en;q=0.9", want: "en-US"},
		{header: "zh-TW,zh;q=0.9", want: "zh-CN"},
		{header: "fr-FR", want: "zh-CN"},
	}

	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Language", test.header)
		responseRecorder := httptest.NewRecorder()
		httpapi.NewRouter().ServeHTTP(responseRecorder, request)

		if got := responseRecorder.Header().Get("Content-Language"); got != test.want {
			t.Fatalf("Content-Language = %q, want %q", got, test.want)
		}
		var body struct {
			Locale string `json:"locale"`
		}
		if err := json.NewDecoder(responseRecorder.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Locale != test.want {
			t.Fatalf("locale = %q, want %q", body.Locale, test.want)
		}
	}
}

func TestReadinessAllowsRedisDegraded(t *testing.T) {
	t.Parallel()
	router := httpapi.NewRouterWithOptions(httpapi.RouterOptions{
		PostgreSQL: readinessFunc(func(context.Context) error { return nil }),
		Redis: readinessFunc(func(context.Context) error {
			return errors.New("redis unavailable")
		}),
	})
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	responseRecorder := httptest.NewRecorder()
	router.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	var body struct {
		Status       string            `json:"status"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.NewDecoder(responseRecorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ready" || body.Dependencies["redis"] != "degraded" {
		t.Fatalf("unexpected readiness response: %#v", body)
	}
}

func TestOpenAPIRequestValidationReturnsSafeFieldDetails(t *testing.T) {
	t.Parallel()
	body := `{
  "name": "model",
  "base_url": "https://model.example.com/v1",
  "model_id": "argus-test",
  "api_protocol": "responses",
  "api_key": "do-not-echo-this-secret",
  "context_window_tokens": 4096,
  "max_output_tokens": 8192,
  "input_price_per_million": 1,
  "output_price_per_million": 2
}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/enterprise/ai-models/test-and-create",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "openapi-validation-test-key")
	request.Header.Set("X-CSRF-Token", strings.Repeat("c", 32))
	request.Header.Set("X-Request-ID", "openapi-validation-request")
	responseRecorder := httptest.NewRecorder()
	httpapi.NewRouter().ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d; body=%s", responseRecorder.Code, http.StatusBadRequest, responseRecorder.Body.String())
	}
	var response struct {
		Code      string         `json:"code"`
		Params    map[string]any `json:"params"`
		RequestID string         `json:"request_id"`
	}
	if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "INVALID_ARGUMENT" || response.RequestID != "openapi-validation-request" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if response.Params["field"] != "context_window_tokens" || response.Params["rule"] != "minimum" || response.Params["minimum"] != float64(8192) {
		t.Fatalf("unexpected safe params: %#v", response.Params)
	}
	encoded := responseRecorder.Body.String()
	for _, forbidden := range []string{"do-not-echo-this-secret", "4096", "pattern", "stack"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("validation response leaked %q: %s", forbidden, encoded)
		}
	}
}
