package sandboxsmoke

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunCreatesProbesAndDeletesSandbox(t *testing.T) {
	var deleted atomic.Bool
	var probeCount atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("OPEN-SANDBOX-API-KEY") != "test-key" {
			http.Error(writer, "missing lifecycle key", http.StatusUnauthorized)
			return
		}
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sandboxes":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body["resourceLimits"] == nil {
				http.Error(writer, "invalid body", http.StatusBadRequest)
				return
			}
			writer.WriteHeader(http.StatusAccepted)
			_, _ = writer.Write([]byte(`{"id":"sandbox-1","status":{"state":"Pending"}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sandboxes/sandbox-1":
			_, _ = writer.Write([]byte(`{"id":"sandbox-1","status":{"state":"Running"}}`))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/endpoints/44772"):
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": server.URL + "/proxy", "headers": map[string]string{"X-ROUTE": "sandbox-1"}})
		case request.Method == http.MethodGet && request.URL.Path == "/proxy/ping":
			if request.Header.Get("X-ROUTE") != "sandbox-1" || request.Header.Get("X-EXECD-ACCESS-TOKEN") != "test-key" {
				http.Error(writer, "missing execd headers", http.StatusUnauthorized)
				return
			}
			if probeCount.Add(1) == 1 {
				http.Error(writer, "backend endpoint is starting", http.StatusBadGateway)
				return
			}
			_, _ = writer.Write([]byte("pong"))
		case request.Method == http.MethodDelete && request.URL.Path == "/v1/sandboxes/sandbox-1":
			deleted.Store(true)
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	err := Run(context.Background(), Options{
		BaseURL: server.URL, APIKey: "test-key", Timeout: time.Second,
		PollInterval: time.Millisecond, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Load() {
		t.Fatal("sandbox cleanup was not requested")
	}
	if probeCount.Load() != 2 {
		t.Fatalf("expected execd readiness to be retried once, got %d probes", probeCount.Load())
	}
}
