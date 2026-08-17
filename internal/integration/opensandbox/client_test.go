package opensandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientUsesLifecyclePrefixAndAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/sandboxes" || request.Header.Get(apiKeyHeader) != "secret" {
			t.Fatalf("unexpected request %s key=%q", request.URL.Path, request.Header.Get(apiKeyHeader))
		}
		_ = json.NewEncoder(writer).Encode(Sandbox{ID: "sandbox-1", Status: Status{State: "Running"}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "secret")
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.Create(context.Background(), CreateRequest{Image: ImageSpec{URI: "image@sha256:test"}})
	if err != nil || value.ID != "sandbox-1" {
		t.Fatalf("create: value=%+v err=%v", value, err)
	}
}
