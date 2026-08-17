package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAssembleContextIsStableAndRedactsPrivateFields(t *testing.T) {
	t.Parallel()
	input := ContextInput{ContextWindow: 32768, MaxOutput: 4096, System: "system", ToolCatalog: []any{"host.list"},
		Checkpoint: map[string]any{"goal": "inspect", "argus__token": "private"}, RecentTail: []any{}, CurrentInput: "hello"}
	first, err := AssembleContext(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AssembleContext(input)
	if err != nil || first.Hash != second.Hash {
		t.Fatalf("projection hash is not stable: %v %q %q", err, first.Hash, second.Hash)
	}
	checkpoint := first.Parts[2].Content.(map[string]any)
	if _, exists := checkpoint["argus__token"]; exists {
		t.Fatal("private token survived context projection")
	}
}

func TestSanitizeNestedStructAndSensitiveStrings(t *testing.T) {
	type private struct {
		APIKey string `json:"api_key"`
		Name   string `json:"name"`
	}
	value := sanitize(map[string]any{"nested": private{APIKey: "secret", Name: "ok"}, "result": "argus_ak_prefix.secret"})
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "secret") || strings.Contains(text, "api_key") {
		t.Fatalf("sensitive value leaked: %s", text)
	}
}
