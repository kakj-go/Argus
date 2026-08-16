package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kakj-go/Argus/internal/transport/httpapi"
)

type readinessFunc func(context.Context) error

func (function readinessFunc) Ready(ctx context.Context) error {
	return function(ctx)
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
