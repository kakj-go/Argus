//go:build m4e2e

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSelectToolPrefersExactIDOverCatalogOrder(t *testing.T) {
	request := replayRequest{
		Messages: []message{{Role: "user", Content: "Call host.list and summarize the visible hosts."}},
		Tools: []replayTool{
			{Name: "host.create.preview", Parameters: map[string]any{"type": "object"}},
			{Name: "host.list", Parameters: map[string]any{"type": "object"}},
		},
	}
	name, _, ok := selectTool(request)
	if !ok || name != "host.list" {
		t.Fatalf("selectTool() = %q, %v; want host.list", name, ok)
	}
}

func TestSelectToolStopsAfterProjectedResult(t *testing.T) {
	request := replayRequest{
		Messages: []message{{Role: "user", Content: `Tool result: {"tool_id":"host.list","items":[]}`}},
		Tools:    []replayTool{{Name: "host.list", Parameters: map[string]any{"type": "object"}}},
	}
	if name, _, ok := selectTool(request); ok {
		t.Fatalf("selectTool() repeated %q after a tool result", name)
	}
}

func TestSelectToolStopsForExecutionVerification(t *testing.T) {
	request := replayRequest{
		Messages: []message{{Role: "user", Content: `Verify this deterministic execution result and report it: {"resource_type":"host"}`}},
		Tools: []replayTool{
			{Name: "host.create.preview", Parameters: map[string]any{"type": "object"}},
			{Name: "host.list", Parameters: map[string]any{"type": "object"}},
		},
	}
	if name, _, ok := selectTool(request); ok {
		t.Fatalf("selectTool() selected %q during deterministic verification", name)
	}
}

func TestSelectToolUsesExplicitPromptInput(t *testing.T) {
	request := replayRequest{
		Messages: []message{{Role: "user", Content: `Call host.update.preview with tool_input: {"host_id":"01900000-0000-7000-8000-000000000001","expected_version":4,"labels":{"team":"m4"}}`}},
		Tools:    []replayTool{{Name: "host.update.preview", Parameters: map[string]any{"type": "object"}}},
	}
	name, arguments, ok := selectTool(request)
	if !ok || name != "host.update.preview" {
		t.Fatalf("selectTool() = %q, %v; want host.update.preview", name, ok)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		t.Fatalf("arguments are not JSON: %v", err)
	}
	if input["host_id"] != "01900000-0000-7000-8000-000000000001" || input["expected_version"] != float64(4) {
		t.Fatalf("unexpected explicit input: %#v", input)
	}
}

func TestSelectToolUsesBase64PromptInput(t *testing.T) {
	request := replayRequest{
		Messages: []message{{Role: "user", Content: "Call host.update.preview with tool_input_b64: eyJob3N0X2lkIjoiMDE5MDAwMDAtMDAwMC03MDAwLTgwMDAtMDAwMDAwMDAwMDAxIiwiZXhwZWN0ZWRfdmVyc2lvbiI6NH0"}},
		Tools:    []replayTool{{Name: "host.update.preview", Parameters: map[string]any{"type": "object"}}},
	}
	name, arguments, ok := selectTool(request)
	if !ok || name != "host.update.preview" {
		t.Fatalf("selectTool() = %q, %v; want host.update.preview", name, ok)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(arguments), &input); err != nil || input["expected_version"] != float64(4) {
		t.Fatalf("unexpected base64 input %q: %#v, %v", arguments, input, err)
	}
}

func TestReplayTextBuildsCardDraftForStructuredCardSchema(t *testing.T) {
	request := replayRequest{Messages: []message{{Role: "user", Content: `Create a host list Card.
Tool Schema Catalog: [{"tool_id":"host.list","output_schema_version":"host.list/v1","schema_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`}},
		ResponseFormat: map[string]any{"json_schema": map[string]any{"schema": map[string]any{"properties": map[string]any{"entrypoint_html": map[string]any{}, "bindings": map[string]any{}}}}}}
	var result map[string]any
	if err := json.Unmarshal([]byte(replayText(request)), &result); err != nil {
		t.Fatal(err)
	}
	if result["slug"] != "m5-enterprise-host-list" {
		t.Fatalf("unexpected Card draft: %#v", result)
	}
	manifest := result["manifest"].(map[string]any)
	if len(manifest["entrypoint_hash"].(string)) != 64 {
		t.Fatalf("unexpected entrypoint hash: %#v", manifest)
	}
}

func TestSelectToolRendersACompletedToolProjectionOnce(t *testing.T) {
	request := replayRequest{Messages: []message{
		{Role: "assistant", Content: `Tool call: host.list {}`},
		{Role: "user", Content: `Tool result result_1: {"tool_call_id":"01900000-0000-7000-8000-000000000001","summary":{"items":[]}}`},
	}, Tools: []replayTool{{Name: "card.render", Parameters: map[string]any{"type": "object"}}}}
	name, arguments, ok := selectTool(request)
	if !ok || name != "card.render" || !strings.Contains(arguments, "01900000-0000-7000-8000-000000000001") {
		t.Fatalf("selectTool() = %q, %q, %v", name, arguments, ok)
	}
	request.Messages = []message{
		{Role: "assistant", Content: `Tool call: card.render {}`},
		{Role: "user", Content: `Tool result result_2: {"tool_call_id":"01900000-0000-7000-8000-000000000002"}`},
	}
	if name, _, ok := selectTool(request); ok {
		t.Fatalf("selectTool() repeated %q after card.render", name)
	}
}
