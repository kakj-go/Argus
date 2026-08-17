package agent

import (
	"context"
	"slices"
	"testing"

	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/storage/postgres"
)

func TestParallelSafeRequiresReadOnlyParallelMetadata(t *testing.T) {
	t.Parallel()
	registry := mcp.NewRegistry()
	register := func(id, risk string, mode mcp.ExecutionMode) {
		t.Helper()
		if err := registry.Register(mcp.Metadata{
			ID: id, Risk: risk, Visibility: mcp.Visible, ExecutionMode: mode,
			MaxResultBytes: 1024,
			Execute: func(context.Context, mcp.Call) (mcp.Result, error) {
				return mcp.Result{Structured: map[string]any{}}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	register("host.list", "read", mcp.ParallelSafe)
	register("connector.list", "read", mcp.ParallelSafe)
	register("host.get", "read", mcp.Sequential)
	register("host.update.preview", "write", mcp.ParallelSafe)
	loop := Loop{Tools: registry}
	if !loop.parallelSafe([]*pendingToolCall{{Name: "host.list"}, {Name: "connector.list"}}) {
		t.Fatal("read-only parallel_safe calls should execute concurrently")
	}
	for _, calls := range [][]*pendingToolCall{
		{{Name: "host.list"}},
		{{Name: "host.list"}, {Name: "host.get"}},
		{{Name: "host.list"}, {Name: "host.update.preview"}},
		{{Name: "host.list"}, {Name: "missing"}},
	} {
		if loop.parallelSafe(calls) {
			t.Fatalf("calls unexpectedly considered parallel safe: %#v", calls)
		}
	}
}

func TestResourceToolCatalogIsStrictAndComplete(t *testing.T) {
	t.Parallel()
	registry := mcp.NewRegistry()
	if err := (ResourceTools{Store: &postgres.Store{}}).Register(registry); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"kubernetes.namespace.list", "kubernetes.node.list", "kubernetes.pod.list", "kubernetes.deployment.list",
		"kubernetes.statefulset.list", "kubernetes.daemonset.list", "kubernetes.service.list", "kubernetes.pod.logs",
	}
	ids := make([]string, 0)
	for _, metadata := range registry.ModelCatalog() {
		ids = append(ids, metadata.ID)
		if metadata.InputSchema == nil || metadata.InputSchema["additionalProperties"] != false {
			t.Fatalf("tool %s has a permissive schema: %#v", metadata.ID, metadata.InputSchema)
		}
	}
	for _, id := range want {
		if !slices.Contains(ids, id) {
			t.Errorf("model catalog lacks %s", id)
		}
	}
	cancel, ok := registry.Lookup("pending_action.cancel")
	if !ok || cancel.Risk != "write" || !slices.Contains(cancel.Required, "pending_action.confirm") {
		t.Fatalf("pending_action.cancel metadata = %#v", cancel)
	}
	logs, ok := registry.Lookup("kubernetes.pod.logs")
	if !ok || !slices.Contains(logs.Required, "kubernetes.logs") || logs.Validate(map[string]any{"cluster_id": "bad", "namespace": "ns", "pod": "pod"}) == nil {
		t.Fatalf("pod logs metadata or validation is incomplete: %#v", logs)
	}
}

func TestToolExecutionRejectsTruncatedModelStops(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"length", "max_tokens", "incomplete", "content_filter", "failed", "cancelled"} {
		if allowsToolExecution(reason) {
			t.Fatalf("tool execution allowed for stop reason %q", reason)
		}
	}
	for _, reason := range []string{"stop", "tool_calls", "completed", ""} {
		if !allowsToolExecution(reason) {
			t.Fatalf("tool execution rejected for complete stop reason %q", reason)
		}
	}
}
