package card

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestValidateStaticRejectsUnsafeCapabilities(t *testing.T) {
	t.Parallel()
	html := []byte(`<main><iframe src="https://example.com"></iframe><script>fetch("https://example.com"); eval("1")</script></main>`)
	manifest := testManifest(ContentHash(html), nil)
	issues := ValidateStatic(StaticInput{HTML: html, Manifest: manifest, Demos: testDemos()})
	for _, code := range []string{"CARD_STATIC_VALIDATION_FAILED", "CARD_NETWORK_FORBIDDEN", "CARD_DYNAMIC_CODE_FORBIDDEN"} {
		if !hasIssue(issues, code) {
			t.Fatalf("missing %s in %#v", code, issues)
		}
	}
}

func TestPreferredBindingRequiresDeclaredFamilyVersionTypeAndSemantic(t *testing.T) {
	t.Parallel()
	registry := mcp.NewRegistry()
	execute := func(context.Context, mcp.Call) (mcp.Result, error) { return mcp.Result{}, nil }
	for _, metadata := range []mcp.Metadata{
		{ID: "host.inventory.v1", ToolFamily: "host.inventory", OutputVersion: "host.inventory/v1", MaxResultBytes: 1024, Execute: execute},
		{ID: "host.inventory.v2", ToolFamily: "host.inventory", OutputVersion: "host.inventory/v2", CompatibleOutputVersions: []string{"host.inventory/v1"},
			SemanticFields: map[string]string{"$.items": "resource_collection"}, FieldTypes: map[string]string{"$.items": "array"}, MaxResultBytes: 1024, Execute: execute},
		{ID: "connector.inventory.v2", ToolFamily: "connector.inventory", OutputVersion: "host.inventory/v2", CompatibleOutputVersions: []string{"host.inventory/v1"},
			SemanticFields: map[string]string{"$.items": "resource_collection"}, FieldTypes: map[string]string{"$.items": "array"}, MaxResultBytes: 1024, Execute: execute},
	} {
		if err := registry.Register(metadata); err != nil {
			t.Fatal(err)
		}
	}
	service := Service{Tools: registry}
	binding := db.CardSlotBinding{Mode: "preferred", ToolID: "host.inventory.v1", OutputSchemaVersion: "host.inventory/v1", FieldPath: "$.items",
		ValueType: "array", SemanticType: pgtype.Text{String: "resource_collection", Valid: true}}
	sourceMetadata, _ := registry.Lookup("host.inventory.v2")
	if !service.bindingMatches(binding, renderSource{metadata: sourceMetadata}) {
		t.Fatal("declared compatible version in the same Tool family did not match")
	}
	otherFamily, _ := registry.Lookup("connector.inventory.v2")
	if service.bindingMatches(binding, renderSource{metadata: otherFamily}) {
		t.Fatal("preferred Binding matched a different Tool family")
	}
	binding.ValueType = "object"
	if service.bindingMatches(binding, renderSource{metadata: sourceMetadata}) {
		t.Fatal("preferred Binding matched an incompatible field type")
	}
}

func TestValidateStaticAcceptsSafeCard(t *testing.T) {
	t.Parallel()
	html := []byte(`<main><h1 data-slot="title">Status</h1><script>window.argusCard.ready()</script></main>`)
	manifest := testManifest(ContentHash(html), []map[string]any{{"name": "title", "kind": "data", "required": true}})
	issues := ValidateStatic(StaticInput{HTML: html, Manifest: manifest, Demos: testDemos(), Bindings: []Binding{{
		SlotName: "title", SlotKind: "data", Mode: "strict", ToolID: "host.get", OutputSchemaVersion: "v1", FieldPath: "$.name", ValueType: "string",
	}}})
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %#v", issues)
	}
}

func TestSelectCandidateSystemWinsOnlyAfterCompatibility(t *testing.T) {
	t.Parallel()
	got, ok := SelectCandidate([]Candidate{
		{CardID: "system", Source: "system", Revision: 4, Available: true, RequiredResolved: true, CompatibleSlots: 2, StrictMatches: 1},
		{CardID: "enterprise", Source: "enterprise", Revision: 1, Available: true, RequiredResolved: true, CompatibleSlots: 3, StrictMatches: 2},
	})
	if !ok || got.CardID != "enterprise" {
		t.Fatalf("unexpected candidate: %#v", got)
	}
	got, _ = SelectCandidate([]Candidate{
		{CardID: "enterprise", Source: "enterprise", Revision: 2, Available: true, RequiredResolved: true, CompatibleSlots: 2, StrictMatches: 1},
		{CardID: "system", Source: "system", Revision: 1, Available: true, RequiredResolved: true, CompatibleSlots: 2, StrictMatches: 1},
	})
	if got.CardID != "system" {
		t.Fatalf("system Card did not win a tie: %#v", got)
	}
	got, _ = SelectCandidate([]Candidate{
		{CardID: "system", Source: "system", Revision: 3, Available: true, RequiredResolved: true, CompatibleSlots: 1, StrictMatches: 1},
		{CardID: "enterprise", Source: "enterprise", Revision: 2, Available: true, RequiredResolved: true, CompatibleSlots: 1, StrictMatches: 1, IntentScore: 1},
	})
	if got.CardID != "enterprise" {
		t.Fatalf("a more precise enterprise Card did not beat the system fallback: %#v", got)
	}
}

func TestSystemCatalogDeclaresRefreshAndPendingActionBindings(t *testing.T) {
	t.Parallel()
	definitions := SystemCatalog()
	var host, pending SystemCatalogCard
	for _, item := range definitions {
		switch item.Slug {
		case "host-list":
			host = item
		case "pending-action":
			pending = item
		}
	}
	if !host.QueryRefresh || !strings.Contains(string(systemCardHTML(host)), "api.query(id)") {
		t.Fatal("Host system Card does not expose its read-only refresh Binding")
	}
	slots := systemSlots(pending)
	for _, name := range []string{"primary", "confirm", "cancel"} {
		if !slices.ContainsFunc(slots, func(slot map[string]any) bool { return slot["name"] == name }) {
			t.Fatalf("PendingAction system Card is missing %s", name)
		}
	}
	html := string(systemCardHTML(pending))
	if !strings.Contains(html, "api.action(id)") || strings.Contains(html, "commit") {
		t.Fatal("PendingAction Card must use only confirm/cancel Action Bindings")
	}
}

func TestPendingActionSystemBindingAcceptsPreviewOutput(t *testing.T) {
	t.Parallel()
	const schemaHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	service := Service{Tools: mcp.NewRegistry()}
	binding := db.CardSlotBinding{SlotKind: "data", Mode: "strict", ToolID: "pending_action.get",
		OutputSchemaVersion: "argus.pending_action/v1", SchemaHash: mustDecodeHex(t, schemaHash), FieldPath: "$", ValueType: "object"}
	source := renderSource{GetCardRenderSourceRow: db.GetCardRenderSourceRow{ToolID: "host.update.preview"}, metadata: mcp.Metadata{
		ID: "host.update.preview", OutputVersion: "argus.pending_action/v1", OutputSchemaHash: schemaHash,
	}}
	if !service.bindingMatches(binding, source) {
		t.Fatal("PendingAction system Binding rejected a compatible Preview result")
	}
	source.metadata.OutputVersion = "host.update.preview/v2"
	if service.bindingMatches(binding, source) {
		t.Fatal("PendingAction system Binding accepted a different output version")
	}
}

func TestCardActionRequestIDIsStableAndSlotSpecific(t *testing.T) {
	t.Parallel()
	presentationID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	confirm := cardActionRequestID(presentationID, "confirm")
	if confirm != cardActionRequestID(presentationID, "confirm") {
		t.Fatal("Card Action request identity is not stable")
	}
	if confirm == cardActionRequestID(presentationID, "cancel") {
		t.Fatal("confirm and cancel shared a Card Action request identity")
	}
}

func TestValidationAuditResultUsesAuditVocabulary(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"passed":  "success",
		"failed":  "failure",
		"expired": "failure",
	}
	for status, expected := range tests {
		if actual := validationAuditResult(status); actual != expected {
			t.Fatalf("validationAuditResult(%q) = %q, want %q", status, actual, expected)
		}
	}
}

func testManifest(hash string, slots []map[string]any) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"schema_version": "argus.card_manifest/v1", "bridge_version": "argus.card_bridge/v1", "entrypoint_hash": hash,
		"max_message_bytes": 1024 * 1024, "slots": slots, "allowed_resources": []string{"inline_style"},
		"supported_locales": []string{"zh-CN", "en-US"}, "default_locale": "zh-CN", "supported_color_schemes": []string{"light", "dark"},
	})
	return value
}

func testDemos() map[string]json.RawMessage {
	result := map[string]json.RawMessage{}
	for _, scenario := range []string{"default", "empty", "error", "large", "light", "dark", "zh-CN", "en-US"} {
		result[scenario] = json.RawMessage(`{}`)
	}
	return result
}

func hasIssue(issues []ValidationIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hexDecode(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
