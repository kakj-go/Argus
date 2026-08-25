package agent

import (
	"context"
	"slices"
	"testing"

	"github.com/kakj-go/Argus/internal/integration/modelprovider"
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

func TestSelectTurnToolsKeepsBudgetRelevantCatalog(t *testing.T) {
	t.Parallel()
	catalog := []modelprovider.Tool{
		{Name: "card.render"},
		{Name: "connector.list"},
		{Name: "host.create.preview"},
		{Name: "host.list"},
		{Name: "host.update.preview"},
		{Name: "kubernetes.pod.list"},
	}
	messages := []modelprovider.Message{
		{Role: "system", Content: "system"},
		{Role: "assistant", Content: "Tool call: host.list {}"},
		{Role: "user", Content: `Tool result: {"tool_call_id":"01900000-0000-7000-8000-000000000001"}`},
	}
	selected := selectTurnTools(catalog, messages, "Call host.list and present a table card")
	names := make([]string, 0, len(selected))
	for _, tool := range selected {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "host.list") || !slices.Contains(names, "card.render") {
		t.Fatalf("required turn tools missing from %#v", names)
	}
	if slices.Contains(names, "kubernetes.pod.list") || slices.Contains(names, "connector.list") {
		t.Fatalf("unrelated tools survived turn selection: %#v", names)
	}
}

func TestSelectTurnToolsUsesBoundedFallback(t *testing.T) {
	t.Parallel()
	catalog := []modelprovider.Tool{
		{Name: "host.list"}, {Name: "host.get"}, {Name: "kubernetes.cluster.list"},
		{Name: "kubernetes.cluster.get"}, {Name: "connector.list"}, {Name: "connector.get"},
		{Name: "pending_action.list"}, {Name: "card.render"}, {Name: "host.create.preview"},
	}
	selected := selectTurnTools(catalog, nil, "help me inspect resources")
	if len(selected) != 8 {
		t.Fatalf("fallback catalog length = %d, want 8", len(selected))
	}
	for _, tool := range selected {
		if tool.Name == "host.create.preview" {
			t.Fatal("bounded fallback exposed a write preview without matching intent")
		}
	}
}

func TestToolIdempotencyIsBoundToTrustedInvocationAndCall(t *testing.T) {
	t.Parallel()
	base := mcp.Call{ToolID: "host.update.preview", RunID: "run-1", InvocationID: "run-1", CallID: "call-1", Subject: "user-1", Input: map[string]any{"request_id": "model-controlled"}}
	if idempotency(base) != idempotency(base) {
		t.Fatal("the same Tool invocation did not produce a stable idempotency key")
	}
	otherInvocation := base
	otherInvocation.RunID = ""
	otherInvocation.InvocationID = "service-run-2"
	if idempotency(base) == idempotency(otherInvocation) {
		t.Fatal("different invocations shared a Tool idempotency key")
	}
	otherCall := base
	otherCall.CallID = "call-2"
	if idempotency(base) == idempotency(otherCall) {
		t.Fatal("different ToolCalls in one Run shared an idempotency key")
	}
	changedRequest := base
	changedRequest.Input = map[string]any{"request_id": "different-model-controlled-value"}
	if idempotency(base) != idempotency(changedRequest) {
		t.Fatal("model-controlled request_id changed the internal idempotency identity")
	}
	legacyRun := base
	legacyRun.InvocationID = ""
	if idempotency(base) != idempotency(legacyRun) {
		t.Fatal("Agent RunID fallback changed the internal idempotency identity")
	}
}

func TestPendingActionGetUsesThePublicPreviewOutputVersion(t *testing.T) {
	t.Parallel()
	registry := mcp.NewRegistry()
	tools := ResourceTools{Store: &postgres.Store{}}
	if err := tools.Register(registry); err != nil {
		t.Fatal(err)
	}
	get, ok := registry.Lookup("pending_action.get")
	if !ok {
		t.Fatal("pending_action.get is not registered")
	}
	preview, ok := registry.Lookup("host.update.preview")
	if !ok {
		t.Fatal("host.update.preview is not registered")
	}
	if get.OutputVersion != "argus.pending_action/v1" || preview.OutputVersion != get.OutputVersion {
		t.Fatalf("PendingAction output versions differ: get=%q preview=%q", get.OutputVersion, preview.OutputVersion)
	}
	if get.OutputSchemaHash == "" || preview.OutputSchemaHash != get.OutputSchemaHash {
		t.Fatalf("PendingAction output Schema hashes differ: get=%q preview=%q", get.OutputSchemaHash, preview.OutputSchemaHash)
	}
}
