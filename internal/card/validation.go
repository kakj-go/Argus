package card

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const (
	MaxHTMLBytes      = 512 * 1024
	MaxDemoBytes      = 256 * 1024
	MaxDemoTotalBytes = 1024 * 1024
	MaxSlots          = 64
)

type ValidationIssue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	SlotName string `json:"slot_name,omitempty"`
}

type StaticInput struct {
	HTML         []byte
	Manifest     json.RawMessage
	Bindings     []Binding
	Demos        map[string]json.RawMessage
	ExpectedHash string
}

type Binding struct {
	SlotName            string `json:"slot_name"`
	SlotKind            string `json:"slot_kind"`
	Mode                string `json:"mode"`
	ToolID              string `json:"tool_id"`
	OutputSchemaVersion string `json:"output_schema_version"`
	SchemaHash          string `json:"schema_hash"`
	FieldPath           string `json:"path"`
	ValueType           string `json:"value_type"`
	SemanticType        string `json:"semantic_type,omitempty"`
}

type manifestDocument struct {
	SchemaVersion         string         `json:"schema_version"`
	BridgeVersion         string         `json:"bridge_version"`
	EntrypointHash        string         `json:"entrypoint_hash"`
	MaxMessageBytes       int            `json:"max_message_bytes"`
	Slots                 []manifestSlot `json:"slots"`
	AllowedResources      []string       `json:"allowed_resources"`
	SupportedLocales      []string       `json:"supported_locales"`
	DefaultLocale         string         `json:"default_locale"`
	SupportedColorSchemes []string       `json:"supported_color_schemes"`
}

type manifestSlot struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
}

var forbiddenScriptPatterns = []struct {
	code    string
	pattern *regexp.Regexp
}{
	{"CARD_DYNAMIC_CODE_FORBIDDEN", regexp.MustCompile(`(?i)\beval\s*\(`)},
	{"CARD_DYNAMIC_CODE_FORBIDDEN", regexp.MustCompile(`(?i)\bnew\s+Function\b`)},
	{"CARD_DYNAMIC_IMPORT_FORBIDDEN", regexp.MustCompile(`(?i)\bimport\s*\(`)},
	{"CARD_WORKER_FORBIDDEN", regexp.MustCompile(`(?i)\b(?:SharedWorker|Worker|ServiceWorkerContainer)\b`)},
	{"CARD_WASM_FORBIDDEN", regexp.MustCompile(`(?i)\bWebAssembly\b`)},
	{"CARD_NETWORK_FORBIDDEN", regexp.MustCompile(`(?i)\b(?:fetch|XMLHttpRequest|WebSocket|EventSource|sendBeacon)\s*\(`)},
	{"CARD_NAVIGATION_FORBIDDEN", regexp.MustCompile(`(?i)\b(?:window\.)?(?:open|location\s*=)`)},
}

func ContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func ValidateStatic(input StaticInput) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if len(input.HTML) == 0 || len(input.HTML) > MaxHTMLBytes {
		issues = append(issues, ValidationIssue{Code: "CARD_CONTENT_TOO_LARGE", Message: "Card HTML must be between 1 byte and 512 KiB"})
	}
	actualHash := ContentHash(input.HTML)
	if input.ExpectedHash != "" && !strings.EqualFold(input.ExpectedHash, actualHash) {
		issues = append(issues, ValidationIssue{Code: "CARD_CONTENT_HASH_MISMATCH", Message: "Card content hash does not match the source bytes"})
	}
	var manifest manifestDocument
	if err := json.Unmarshal(input.Manifest, &manifest); err != nil {
		issues = append(issues, ValidationIssue{Code: "CARD_MANIFEST_INVALID", Message: "Manifest is not valid JSON"})
		return issues
	}
	if manifest.SchemaVersion != "argus.card_manifest/v1" || manifest.BridgeVersion != "argus.card_bridge/v1" || manifest.MaxMessageBytes <= 0 || manifest.MaxMessageBytes > 1024*1024 {
		issues = append(issues, ValidationIssue{Code: "CARD_MANIFEST_INVALID", Message: "Manifest protocol versions or message limit are invalid"})
	}
	if manifest.EntrypointHash != actualHash || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(manifest.EntrypointHash) {
		issues = append(issues, ValidationIssue{Code: "CARD_CONTENT_HASH_MISMATCH", Message: "Manifest entrypoint_hash does not match the Card HTML"})
	}
	if len(manifest.Slots) > MaxSlots || len(input.Bindings) > MaxSlots {
		issues = append(issues, ValidationIssue{Code: "CARD_MANIFEST_INVALID", Message: "A Card version may declare at most 64 slots"})
	}
	issues = append(issues, validateSlots(manifest.Slots, input.Bindings)...)
	issues = append(issues, validateDemos(input.Demos)...)
	issues = append(issues, validateHTML(input.HTML)...)
	return deduplicateIssues(issues)
}

func validateSlots(slots []manifestSlot, bindings []Binding) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	declared := make(map[string]manifestSlot, len(slots))
	for _, slot := range slots {
		if slot.Name == "" || (slot.Kind != "data" && slot.Kind != "query" && slot.Kind != "action") {
			issues = append(issues, ValidationIssue{Code: "CARD_MANIFEST_INVALID", Message: "Slot name or kind is invalid", SlotName: slot.Name})
		}
		if _, exists := declared[slot.Name]; exists {
			issues = append(issues, ValidationIssue{Code: "CARD_MANIFEST_INVALID", Message: "Slot names must be unique", SlotName: slot.Name})
		}
		declared[slot.Name] = slot
	}
	bound := make(map[string]bool, len(bindings))
	for _, binding := range bindings {
		slot, exists := declared[binding.SlotName]
		if !exists || slot.Kind != binding.SlotKind || (binding.Mode != "strict" && binding.Mode != "preferred") {
			issues = append(issues, ValidationIssue{Code: "CARD_BINDING_INVALID", Message: "Binding does not match its declared slot", SlotName: binding.SlotName})
		}
		if _, err := ParseJSONPath(binding.FieldPath); err != nil {
			issues = append(issues, ValidationIssue{Code: "CARD_BINDING_INVALID", Message: "Binding uses an unsupported JSONPath", SlotName: binding.SlotName})
		}
		if slot.Kind == "query" && strings.HasSuffix(binding.ToolID, ".commit") {
			issues = append(issues, ValidationIssue{Code: "CARD_BINDING_INVALID", Message: "Query slots cannot bind commit Tools", SlotName: binding.SlotName})
		}
		if slot.Kind == "action" && binding.ToolID != "pending_action.confirm" && binding.ToolID != "pending_action.cancel" {
			issues = append(issues, ValidationIssue{Code: "CARD_BINDING_INVALID", Message: "Action slots may only confirm or cancel an existing PendingAction", SlotName: binding.SlotName})
		}
		bound[binding.SlotName] = true
	}
	for _, slot := range slots {
		if slot.Required && !bound[slot.Name] {
			issues = append(issues, ValidationIssue{Code: "CARD_BINDING_INVALID", Message: "Required slot is not bound", SlotName: slot.Name})
		}
	}
	return issues
}

func validateDemos(demos map[string]json.RawMessage) []ValidationIssue {
	required := []string{"default", "empty", "error", "large", "light", "dark", "zh-CN", "en-US"}
	total := 0
	issues := make([]ValidationIssue, 0)
	for _, scenario := range required {
		data, exists := demos[scenario]
		if !exists {
			issues = append(issues, ValidationIssue{Code: "CARD_RUNTIME_VALIDATION_REQUIRED", Message: "Required demo scenario is missing: " + scenario})
			continue
		}
		if len(data) > MaxDemoBytes || !json.Valid(data) {
			issues = append(issues, ValidationIssue{Code: "CARD_MANIFEST_INVALID", Message: "Demo scenario is invalid or too large: " + scenario})
		}
		total += len(data)
	}
	if total > MaxDemoTotalBytes {
		issues = append(issues, ValidationIssue{Code: "CARD_CONTENT_TOO_LARGE", Message: "Card demos exceed the 1 MiB version limit"})
	}
	return issues
}

func validateHTML(source []byte) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	tokenizer := html.NewTokenizer(bytes.NewReader(source))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if err := tokenizer.Err(); err != nil && err != io.EOF {
				issues = append(issues, ValidationIssue{Code: "CARD_STATIC_VALIDATION_FAILED", Message: "HTML parsing failed"})
			}
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		tag := strings.ToLower(token.Data)
		if tag == "iframe" || tag == "object" || tag == "embed" || tag == "base" || tag == "form" {
			issues = append(issues, ValidationIssue{Code: "CARD_STATIC_VALIDATION_FAILED", Message: "Forbidden HTML element: " + tag})
		}
		for _, attribute := range token.Attr {
			name := strings.ToLower(attribute.Key)
			value := strings.TrimSpace(attribute.Val)
			if strings.HasPrefix(name, "on") || name == "srcdoc" || name == "formaction" {
				issues = append(issues, ValidationIssue{Code: "CARD_DYNAMIC_CODE_FORBIDDEN", Message: "Inline event handlers and dynamic documents are forbidden"})
			}
			if name == "src" || name == "href" || name == "action" || name == "poster" {
				if externalResource(value) {
					issues = append(issues, ValidationIssue{Code: "CARD_NETWORK_FORBIDDEN", Message: "External resource URLs are forbidden"})
				}
			}
			if name == "style" {
				issues = append(issues, validateCode(value)...)
			}
		}
	}
	issues = append(issues, validateCode(string(source))...)
	return issues
}

func validateCode(source string) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	for _, item := range forbiddenScriptPatterns {
		if item.pattern.MatchString(source) {
			issues = append(issues, ValidationIssue{Code: item.code, Message: "Card source requests a forbidden runtime capability"})
		}
	}
	if regexp.MustCompile(`(?i)@import\b|url\s*\(\s*['\"]?https?://`).MatchString(source) {
		issues = append(issues, ValidationIssue{Code: "CARD_NETWORK_FORBIDDEN", Message: "External CSS resources are forbidden"})
	}
	return issues
}

func externalResource(raw string) bool {
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "data:") || strings.HasPrefix(raw, "blob:") {
		return false
	}
	parsed, err := url.Parse(raw)
	return err != nil || parsed.IsAbs() || strings.HasPrefix(raw, "//")
}

func deduplicateIssues(issues []ValidationIssue) []ValidationIssue {
	result := make([]ValidationIssue, 0, len(issues))
	seen := map[string]bool{}
	for _, issue := range issues {
		key := fmt.Sprintf("%s\x00%s\x00%s", issue.Code, issue.Message, issue.SlotName)
		if !seen[key] {
			seen[key] = true
			result = append(result, issue)
		}
	}
	return result
}
