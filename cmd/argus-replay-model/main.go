//go:build m4e2e

// Command argus-replay-model is a deterministic OpenAI-compatible endpoint for
// M4 Kubernetes E2E. The production backend image never builds this command.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type replayRequest struct {
	Model          string         `json:"model"`
	Messages       []message      `json:"messages"`
	Input          []message      `json:"input"`
	Tools          []replayTool   `json:"tools"`
	ResponseFormat map[string]any `json:"response_format"`
	Text           map[string]any `json:"text"`
}

type replayTool struct {
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
	Function   *struct {
		Name       string         `json:"name"`
		Parameters map[string]any `json:"parameters"`
	} `json:"function"`
}

var requestSequence atomic.Uint64
var sandboxState = struct {
	sync.Mutex
	items map[string]replaySandbox
}{items: map[string]replaySandbox{}}

type replaySandbox struct {
	ID        string            `json:"id"`
	Status    map[string]string `json:"status"`
	ExpiresAt time.Time         `json:"expiresAt"`
	CreatedAt time.Time         `json:"createdAt"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func main() {
	address := os.Getenv("ARGUS_REPLAY_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/v1/chat/completions", chatCompletions)
	mux.HandleFunc("/v1/responses", responses)
	mux.HandleFunc("/sandbox/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/sandbox/v1/sandboxes", sandboxCollection)
	mux.HandleFunc("/sandbox/v1/sandboxes/", sandboxItem)
	server := &http.Server{Addr: address, Handler: requestLimit(mux), ReadHeaderTimeout: 5 * time.Second}
	if plaintext := os.Getenv("ARGUS_REPLAY_PLAINTEXT_ADDRESS"); plaintext != "" {
		go func() {
			plainServer := &http.Server{Addr: plaintext, Handler: requestLimit(mux), ReadHeaderTimeout: 5 * time.Second}
			log.Printf("argus M4 replay sandbox listening on %s", plaintext)
			log.Fatal(plainServer.ListenAndServe())
		}()
	}
	log.Printf("argus M4 replay model listening on %s", address)
	certificate, privateKey := os.Getenv("ARGUS_REPLAY_TLS_CERT"), os.Getenv("ARGUS_REPLAY_TLS_KEY")
	if certificate == "" || privateKey == "" {
		log.Fatal("ARGUS_REPLAY_TLS_CERT and ARGUS_REPLAY_TLS_KEY are required")
	}
	log.Fatal(server.ListenAndServeTLS(certificate, privateKey))
}

func sandboxCollection(w http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		sandboxState.Lock()
		items := make([]replaySandbox, 0, len(sandboxState.items))
		for _, item := range sandboxState.items {
			items = append(items, item)
		}
		sandboxState.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input struct {
			Timeout  int               `json:"timeout"`
			Metadata map[string]string `json:"metadata"`
		}
		if json.NewDecoder(request.Body).Decode(&input) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC()
		item := replaySandbox{ID: fmt.Sprintf("sandbox-%d", requestSequence.Add(1)), Status: map[string]string{"state": "running"},
			ExpiresAt: now.Add(time.Duration(max(input.Timeout, 10)) * time.Second), CreatedAt: now, Metadata: input.Metadata}
		sandboxState.Lock()
		sandboxState.items[item.ID] = item
		sandboxState.Unlock()
		writeJSON(w, http.StatusCreated, item)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func sandboxItem(w http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, "/sandbox/v1/sandboxes/")
	id := strings.TrimSuffix(path, "/renew-expiration")
	sandboxState.Lock()
	defer sandboxState.Unlock()
	item, exists := sandboxState.items[id]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch {
	case request.Method == http.MethodGet && path == id:
		writeJSON(w, http.StatusOK, item)
	case request.Method == http.MethodDelete && path == id:
		delete(sandboxState.items, id)
		w.WriteHeader(http.StatusNoContent)
	case request.Method == http.MethodPost && strings.HasSuffix(path, "/renew-expiration"):
		var input struct {
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if json.NewDecoder(request.Body).Decode(&input) != nil || input.ExpiresAt.IsZero() {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		item.ExpiresAt = input.ExpiresAt.UTC()
		sandboxState.items[id] = item
		writeJSON(w, http.StatusOK, item)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			log.Printf("replay request %s %s", request.Method, request.URL.Path)
		}
		request.Body = http.MaxBytesReader(w, request.Body, 2<<20)
		next.ServeHTTP(w, request)
	})
}

func chatCompletions(w http.ResponseWriter, request *http.Request) {
	value, ok := decodeRequest(w, request)
	if !ok {
		return
	}
	startSSE(w)
	if tool, arguments, useTool := selectTool(value); useTool {
		callID := fmt.Sprintf("call_%d", requestSequence.Add(1))
		toolCall := map[string]any{
			"index": 0,
			"id":    callID,
			"type":  "function",
			"function": map[string]any{
				"name": tool, "arguments": arguments,
			},
		}
		writeSSE(w, chatChunk(map[string]any{"tool_calls": []any{toolCall}}, ""))
		writeSSE(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}}})
	} else {
		writeSSE(w, chatChunk(map[string]any{"content": replayText(value)}, ""))
		writeSSE(w, chatChunk(map[string]any{}, "stop"))
	}
	writeSSE(w, map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 32, "completion_tokens": 8, "total_tokens": 40}})
	writeRawSSE(w, "[DONE]")
}

func chatChunk(delta map[string]any, finishReason string) map[string]any {
	return map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": finishReason}}}
}

func responses(w http.ResponseWriter, request *http.Request) {
	value, ok := decodeRequest(w, request)
	if !ok {
		return
	}
	startSSE(w)
	if tool, arguments, useTool := selectTool(value); useTool {
		callID := fmt.Sprintf("call_%d", requestSequence.Add(1))
		writeSSE(w, map[string]any{"type": "response.function_call_arguments.delta", "call_id": callID, "name": tool, "delta": arguments})
		writeSSE(w, map[string]any{"type": "response.function_call_arguments.done", "call_id": callID, "name": tool, "arguments": arguments})
	} else {
		writeSSE(w, map[string]any{"type": "response.output_text.delta", "delta": replayText(value)})
	}
	writeSSE(w, map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "usage": map[string]any{"input_tokens": 32, "output_tokens": 8}}})
}

func decodeRequest(w http.ResponseWriter, request *http.Request) (replayRequest, bool) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return replayRequest{}, false
	}
	var value replayRequest
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(&value); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return replayRequest{}, false
	}
	if strings.Contains(strings.ToLower(value.Model), "quota-exceeded") {
		http.Error(w, `{"error":{"code":"quota_exceeded"}}`, http.StatusTooManyRequests)
		return replayRequest{}, false
	}
	return value, true
}

func startSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

func writeSSE(w http.ResponseWriter, value any) {
	payload, _ := json.Marshal(value)
	writeRawSSE(w, string(payload))
}

func writeRawSSE(w http.ResponseWriter, payload string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func replayText(request replayRequest) string {
	if len(request.ResponseFormat) > 0 || len(request.Text) > 0 {
		if requestsCardDraft(request) {
			return replayCardDraft(request)
		}
		return `{"ok":true}`
	}
	for _, item := range append(request.Messages, request.Input...) {
		if item.Role == "tool" {
			return "The requested operation completed successfully."
		}
	}
	return "OK"
}

func requestsCardDraft(request replayRequest) bool {
	encoded, _ := json.Marshal(map[string]any{"response_format": request.ResponseFormat, "text": request.Text})
	return strings.Contains(string(encoded), `"entrypoint_html"`) && strings.Contains(string(encoded), `"bindings"`)
}

func replayCardDraft(request replayRequest) string {
	prompt := lastContent(append(request.Messages, request.Input...))
	presentationKind := "table"
	if strings.Contains(strings.ToLower(prompt), "detail") {
		presentationKind = "detail"
	}
	const marker = "Tool Schema Catalog: "
	var catalog []map[string]any
	if index := strings.Index(prompt, marker); index >= 0 {
		_ = json.Unmarshal([]byte(strings.TrimSpace(prompt[index+len(marker):])), &catalog)
	}
	var selected map[string]any
	for _, item := range catalog {
		if item["tool_id"] == "host.list" {
			selected = item
			break
		}
	}
	if selected == nil && len(catalog) > 0 {
		selected = catalog[0]
	}
	toolID, _ := selected["tool_id"].(string)
	version, _ := selected["output_schema_version"].(string)
	schemaHash, _ := selected["schema_hash"].(string)
	if toolID == "" {
		toolID, version, schemaHash = "host.list", "host.list/v1", strings.Repeat("0", 64)
	}
	html := `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;font:14px system-ui;color:CanvasText;background:Canvas}main{padding:12px}pre{white-space:pre-wrap}</style></head><body><main><strong>Enterprise host inventory</strong><pre data-slot="primary" id="content"></pre></main><script>(function(){var api=window.argusCard;function render(value){document.getElementById("content").textContent=JSON.stringify(value.primary||[],null,2)}render(api.data);api.onData(render)})()</script></body></html>`
	hash := sha256.Sum256([]byte(html))
	contentHash := fmt.Sprintf("%x", hash[:])
	manifest := map[string]any{"schema_version": "argus.card_manifest/v1", "card_id": "00000000-0000-0000-0000-000000000000", "revision": 1,
		"source": "enterprise", "entrypoint_hash": contentHash, "bridge_version": "argus.card_bridge/v1", "max_message_bytes": 1048576,
		"slots":             []any{map[string]any{"name": "primary", "kind": "data", "required": true, "value_type": "array"}},
		"allowed_resources": []string{"inline_style", "inline_script"}, "supported_locales": []string{"zh-CN", "en-US"}, "default_locale": "zh-CN",
		"supported_color_schemes": []string{"light", "dark"}, "presentation_kind": presentationKind}
	demos := map[string]any{}
	for _, scenario := range []string{"default", "empty", "error", "large", "light", "dark", "zh-CN", "en-US"} {
		demos[scenario] = map[string]any{"primary": []any{}}
	}
	bundle := map[string]any{"slug": "m5-enterprise-host-list", "name": "Enterprise host inventory", "description": "Generated by the fixed M5 replay model.",
		"entrypoint_html": html, "manifest": manifest, "bindings": []any{map[string]any{"slot_name": "primary", "slot_kind": "data", "mode": "strict",
			"tool_id": toolID, "output_schema_version": version, "schema_hash": schemaHash, "path": "$.items", "value_type": "array", "semantic_type": "resource_collection"}}, "demos": demos}
	encoded, _ := json.Marshal(bundle)
	return string(encoded)
}

func selectTool(request replayRequest) (string, string, bool) {
	messages := append(request.Messages, request.Input...)
	for _, item := range messages {
		if item.Role == "tool" {
			return "", "", false
		}
	}
	rawPrompt := lastContent(messages)
	prompt := strings.ToLower(rawPrompt)
	type candidate struct {
		name   string
		schema map[string]any
	}
	candidates := make([]candidate, 0, len(request.Tools))
	for _, item := range request.Tools {
		name, schema := item.Name, item.Parameters
		if item.Function != nil {
			name, schema = item.Function.Name, item.Function.Parameters
		}
		if name == "" {
			continue
		}
		candidates = append(candidates, candidate{name: name, schema: schema})
	}
	trimmedPrompt := strings.TrimSpace(prompt)
	if strings.HasPrefix(trimmedPrompt, "verify this deterministic execution result") {
		return "", "", false
	}
	if strings.HasPrefix(trimmedPrompt, "tool result") {
		previousTool := ""
		for index := len(messages) - 2; index >= 0; index-- {
			content, _ := messages[index].Content.(string)
			if strings.HasPrefix(content, "Tool call: ") {
				previousTool = strings.Fields(strings.TrimPrefix(content, "Tool call: "))[0]
				break
			}
		}
		if previousTool == "card.render" {
			return "", "", false
		}
		for _, item := range candidates {
			if item.name != "card.render" {
				continue
			}
			start := strings.Index(rawPrompt, "{")
			var projection map[string]any
			if start < 0 || json.Unmarshal([]byte(rawPrompt[start:]), &projection) != nil {
				return "", "", false
			}
			toolCallID, _ := projection["tool_call_id"].(string)
			if toolCallID == "" {
				return "", "", false
			}
			kind := "table"
			if strings.Contains(previousTool, "pending_action") || strings.HasSuffix(previousTool, ".preview") {
				kind = "pending_action"
			} else {
				for _, message := range messages {
					content, _ := message.Content.(string)
					if strings.Contains(strings.ToLower(content), "detail") {
						kind = "detail"
						break
					}
				}
			}
			arguments, _ := json.Marshal(map[string]any{"tool_call_ids": []string{toolCallID}, "presentation_kind": kind})
			return item.name, string(arguments), true
		}
		return "", "", false
	}
	for _, item := range candidates {
		if item.name == "compatibility_probe" || strings.Contains(prompt, strings.ToLower(item.name)) {
			return selectedToolWithPrompt(item.name, item.schema, rawPrompt)
		}
	}
	for _, item := range candidates {
		if strings.Contains(prompt, toolKeyword(item.name)) {
			return selectedToolWithPrompt(item.name, item.schema, rawPrompt)
		}
	}
	return "", "", false
}

func selectedTool(name string, schema map[string]any) (string, string, bool) {
	arguments, _ := json.Marshal(exampleForSchema(schema))
	return name, string(arguments), true
}

func selectedToolWithPrompt(name string, schema map[string]any, prompt string) (string, string, bool) {
	const encodedMarker = "tool_input_b64:"
	if index := strings.Index(strings.ToLower(prompt), encodedMarker); index >= 0 {
		candidate := strings.Fields(strings.TrimSpace(prompt[index+len(encodedMarker):]))
		if len(candidate) > 0 {
			decoded, err := base64.RawURLEncoding.DecodeString(candidate[0])
			var input map[string]any
			if err == nil && json.Unmarshal(decoded, &input) == nil {
				arguments, _ := json.Marshal(input)
				return name, string(arguments), true
			}
		}
	}
	const marker = "tool_input:"
	if index := strings.Index(strings.ToLower(prompt), marker); index >= 0 {
		candidate := strings.TrimSpace(prompt[index+len(marker):])
		var input map[string]any
		if json.Unmarshal([]byte(candidate), &input) == nil {
			arguments, _ := json.Marshal(input)
			return name, string(arguments), true
		}
	}
	return selectedTool(name, schema)
}

func lastContent(messages []message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if value, ok := messages[index].Content.(string); ok {
			return value
		}
	}
	return ""
}

func toolKeyword(name string) string {
	if index := strings.IndexByte(name, '.'); index > 0 {
		return name[:index]
	}
	return name
}

func exampleForSchema(schema map[string]any) map[string]any {
	result := map[string]any{}
	properties, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, raw := range required {
		name, _ := raw.(string)
		property, _ := properties[name].(map[string]any)
		result[name] = exampleValue(property)
	}
	return result
}

func exampleValue(schema map[string]any) any {
	if value, exists := schema["const"]; exists {
		return value
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	switch schema["type"] {
	case "integer", "number":
		return 1
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		return exampleForSchema(schema)
	default:
		return "m4-e2e"
	}
}
