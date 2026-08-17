package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kakj-go/Argus/internal/mcp"
)

func TestToolResultProjectionBoundsListsAndTracksPartialState(t *testing.T) {
	t.Parallel()
	items := make([]map[string]any, 0, 80)
	for index := range 80 {
		items = append(items, map[string]any{"id": index, "name": strings.Repeat("host", 20)})
	}
	full, _ := json.Marshal(map[string]any{"items": items})
	encoded, partial, err := encodeToolResultProjection("result_1", pendingToolCall{ID: "call_1", Name: "host.list"}, full,
		mcp.Result{Structured: map[string]any{"items": items}})
	if err != nil {
		t.Fatal(err)
	}
	if !partial || len(encoded) > maxProjectionBytes {
		t.Fatalf("partial=%t bytes=%d", partial, len(encoded))
	}
	var projection map[string]any
	if json.Unmarshal(encoded, &projection) != nil {
		t.Fatal("projection is not JSON")
	}
	if int(projection["projected_bytes"].(float64)) != len(encoded) || projection["partial"] != true {
		t.Fatalf("projection metadata is inconsistent: %#v", projection)
	}
	summary := projection["summary"].(map[string]any)
	if len(summary["items"].([]any)) != maxProjectedItems {
		t.Fatalf("projected items = %d", len(summary["items"].([]any)))
	}
}

func TestPodLogProjectionRedactsAndTruncates(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("line\n", 10_000)
	structured := map[string]any{"cluster_id": "cluster", "namespace": "default", "pod": "pod", "content": content, "bytes": len(content), "api_key": "argus_ak_prefix.secret"}
	full, _ := json.Marshal(structured)
	encoded, partial, err := encodeToolResultProjection("result_logs", pendingToolCall{ID: "call_logs", Name: "kubernetes.pod.logs"}, full, mcp.Result{Structured: structured})
	if err != nil {
		t.Fatal(err)
	}
	if !partial || len(encoded) > maxProjectionBytes || strings.Contains(string(encoded), "argus_ak_") {
		t.Fatalf("unsafe log projection: partial=%t bytes=%d", partial, len(encoded))
	}
}
