package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIValidationAcceptsSetupInitializeRequest(t *testing.T) {
	body := `{
  "platform_name": "Argus E2E",
  "default_locale": "zh-CN",
  "timezone": "Asia/Shanghai",
  "external_url": "http://127.0.0.1:4174",
  "super_admin": {
    "username": "platform-admin",
    "display_name": "Platform Admin",
    "email": "platform@example.test",
    "password": "N7!qP4@vL9#sT2$x"
  }
}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/initialize", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Argus-Setup-Token", strings.Repeat("t", 32))
	request.Header.Set("Idempotency-Key", "setup-form-validation-20260823-r3")

	called := false
	handler := openAPIRequestValidationMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		called = true
		writer.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("valid setup request rejected: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOpenAPIValidationRejectsBodyWithoutRequestBodyContract(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", strings.NewReader(`{"unexpected":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "unexpected-body-request")

	called := false
	handler := openAPIRequestValidationMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		called = true
		writer.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("unexpected body response: status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}
	var result openAPIValidationError
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Code != "INVALID_ARGUMENT" || result.RequestID != "unexpected-body-request" || result.Params["rule"] != "unexpected_body" {
		t.Fatalf("unexpected validation response: %#v", result)
	}
}
