package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kakj-go/Argus/internal/mcp"
)

const (
	maxProjectedItems = 50
	maxProjectedLog   = 32 << 10
)

func encodeToolResultProjection(resultRef, toolCallID string, call pendingToolCall, full []byte, result mcp.Result) ([]byte, bool, error) {
	clean, _ := sanitize(result.Structured).(map[string]any)
	summary, refs, partial := projectStructuredResult(call.Name, clean)
	partial = partial || result.Partial
	payload := map[string]any{
		"schema_version":            "argus.tool_result_projection/v1",
		"projection_schema_version": "v1",
		"tool_call_id":              toolCallID,
		"result_ref":                resultRef,
		"result_hash":               fmt.Sprintf("%x", sha256.Sum256(full)),
		"original_bytes":            len(full),
		"projected_bytes":           0,
		"partial":                   partial,
		"resource_refs":             refs,
		"summary":                   summary,
	}
	encoded, err := marshalProjection(payload)
	if err != nil {
		return nil, false, err
	}
	if len(encoded) <= maxProjectionBytes {
		return encoded, partial, nil
	}
	payload["partial"] = true
	payload["resource_refs"] = capResourceRefs(refs, 20)
	payload["summary"] = map[string]any{
		"message": "result projection truncated; retrieve the full result with result_ref",
		"keys":    sortedKeys(clean),
	}
	encoded, err = marshalProjection(payload)
	if err != nil {
		return nil, false, err
	}
	if len(encoded) > maxProjectionBytes {
		return nil, false, fmt.Errorf("projection envelope exceeds %d bytes", maxProjectionBytes)
	}
	return encoded, true, nil
}

func projectStructuredResult(toolID string, clean map[string]any) (any, []any, bool) {
	if clean == nil {
		return map[string]any{}, []any{}, false
	}
	if toolID == "kubernetes.pod.logs" {
		return projectPodLogs(clean)
	}
	items, ok := clean["items"].([]any)
	if !ok {
		return clean, resourceRefs(clean), false
	}
	visible := items
	partial := false
	if len(visible) > maxProjectedItems {
		visible = visible[:maxProjectedItems]
		partial = true
	}
	summary := make(map[string]any, len(clean)+2)
	for key, value := range clean {
		if key != "items" {
			summary[key] = value
		}
	}
	summary["items"] = visible
	summary["total_items"] = len(items)
	summary["projected_items"] = len(visible)
	refs := make([]any, 0, len(visible))
	for _, item := range visible {
		if value, ok := item.(map[string]any); ok {
			refs = append(refs, resourceRefs(value)...)
		}
	}
	return summary, refs, partial
}

func projectPodLogs(clean map[string]any) (any, []any, bool) {
	content, _ := clean["content"].(string)
	partial, _ := clean["truncated"].(bool)
	if len(content) > maxProjectedLog {
		content = content[:maxProjectedLog]
		partial = true
	}
	summary := map[string]any{
		"cluster_id": clean["cluster_id"], "namespace": clean["namespace"], "pod": clean["pod"], "container": clean["container"],
		"content": content, "bytes": clean["bytes"], "truncated": partial, "projected_lines": strings.Count(content, "\n") + 1,
	}
	return summary, resourceRefs(clean), partial
}

func resourceRefs(value map[string]any) []any {
	for _, key := range []string{"id", "host_id", "cluster_id", "connector_id", "action_ref"} {
		if ref, ok := value[key]; ok && fmt.Sprint(ref) != "" {
			return []any{map[string]any{"kind": key, "ref": ref}}
		}
	}
	return []any{}
}

func capResourceRefs(values []any, limit int) []any {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func marshalProjection(payload map[string]any) ([]byte, error) {
	var encoded []byte
	var err error
	for range 3 {
		encoded, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if payload["projected_bytes"] == len(encoded) {
			break
		}
		payload["projected_bytes"] = len(encoded)
	}
	return encoded, nil
}
