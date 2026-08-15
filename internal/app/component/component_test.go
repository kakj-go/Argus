package component_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kakj-go/Argus/internal/app/component"
)

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		path   string
		status string
	}{
		{path: "/healthz", status: "ok"},
		{path: "/readyz", status: "ready"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			component.HealthHandler("argus-test").ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
			}
			var body struct {
				Service string `json:"service"`
				Status  string `json:"status"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Service != "argus-test" || body.Status != test.status {
				t.Fatalf("response = %+v", body)
			}
		})
	}
}
