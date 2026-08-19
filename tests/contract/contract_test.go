package contract_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"
)

var legacyPattern = regexp.MustCompile(`(?i)(\bproject\b|project_id|ProjectId|\bmembership\b|EnterpriseMembership|memberships|scopeType.*project|projectIds|listProjects|createProject|updateProject|resourceGroupIds|ownerTeamId|\btags\b|PendingAction\.params|params: Record<string, unknown>)`)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestM4AutomationRunsBindImmutableRevisions(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	migration, err := os.ReadFile(filepath.Join(root, "migrations/postgresql/00003_m4_action_agent.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(migration)
	for _, required := range []string{
		"CREATE TABLE automation_revisions",
		"automation_revision integer NOT NULL",
		"REFERENCES automation_revisions(automation_id, enterprise_id, revision)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("M4 migration lacks immutable Automation revision guard %q", required)
		}
	}
}

func TestM4AgentActionsRetainTrustedRunBinding(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	resources, err := os.ReadFile(filepath.Join(root, "internal/storage/postgres/queries/resources.sql"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := os.ReadFile(filepath.Join(root, "internal/action/service.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resources), "expires_at, run_id)") {
		t.Fatal("PendingAction creation does not persist the trusted Agent Run binding")
	}
	if !strings.Contains(string(workflow), "RunID: action.RunID") {
		t.Fatal("Execution contract no longer carries the PendingAction Run binding")
	}
	automation, err := os.ReadFile(filepath.Join(root, "internal/storage/postgres/queries/automation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"MarkAutomationRunExecuting", "FinishAutomationRunByPendingAction"} {
		if !strings.Contains(string(automation), required) {
			t.Fatalf("AutomationRun terminal propagation lacks %s", required)
		}
	}
}

func TestSchemas(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		name     string
		schemaID string
		fragment string
	}{
		{"label-selector", "https://argus.io/schemas/v1/labels/labels.schema.json", ""},
		{"pending-action", "https://argus.io/schemas/v1/action/pending-action-public.schema.json", ""},
		{"action-binding", "https://argus.io/schemas/v1/action/workflow.schema.json", ""},
		{"card-manifest", "https://argus.io/schemas/v1/card/card.schema.json", "/$defs/CardManifest"},
		{"bridge-message", "https://argus.io/schemas/v1/card/card.schema.json", "/$defs/BridgeMessage"},
		{"conversation-event", "https://argus.io/schemas/v1/agent/agent.schema.json", ""},
		{"context-snapshot", "https://argus.io/schemas/v1/agent/agent.schema.json", "/$defs/ContextSnapshot"},
		{"tool-result-projection", "https://argus.io/schemas/v1/agent/agent.schema.json", "/$defs/ToolResultProjection"},
		{"tool-metadata", "https://argus.io/schemas/v1/agent/agent.schema.json", "/$defs/ToolMetadata"},
		{"agent-event", "https://argus.io/schemas/v1/agent/agent.schema.json", "/$defs/AgentEvent"},
		{"stream", "https://argus.io/schemas/v1/stream/stream.schema.json", ""},
		{"telemetry-query", "https://argus.io/schemas/v1/telemetry/query.schema.json", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiler := schemaCompiler(t, root)
			uri := tc.schemaID
			if tc.fragment != "" {
				uri += "#" + tc.fragment
			}
			schema, err := compiler.Compile(uri)
			if err != nil {
				t.Fatalf("compile schema: %v", err)
			}
			valid := readJSON(t, filepath.Join(root, "tests/contract/fixtures/valid", tc.name+".json"))
			if err := schema.Validate(valid); err != nil {
				t.Fatalf("valid fixture rejected: %v", err)
			}
			boundary := readJSON(t, filepath.Join(root, "tests/contract/fixtures/boundary", tc.name+".json"))
			if err := schema.Validate(boundary); err != nil {
				t.Fatalf("boundary fixture rejected: %v", err)
			}
			invalid := readJSON(t, filepath.Join(root, "tests/contract/fixtures/invalid", tc.name+".json"))
			if err := schema.Validate(invalid); err == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}

func TestCommonOpenAPISchemas(t *testing.T) {
	root := repoRoot(t)
	compiler := openAPISchemaCompiler(t, root)
	for _, name := range []string{"CursorPage", "BatchResult"} {
		fixtureName := strings.ToLower(name[:1]) + name[1:]
		fixtureName = strings.ReplaceAll(fixtureName, "Page", "-page")
		fixtureName = strings.ReplaceAll(fixtureName, "Result", "-result")
		schema, err := compiler.Compile("https://argus.io/openapi/v1/argus.bundle.json#/components/schemas/" + name)
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		for _, class := range []string{"valid", "boundary"} {
			value := readJSON(t, filepath.Join(root, "tests/contract/fixtures", class, fixtureName+".json"))
			if err := schema.Validate(value); err != nil {
				t.Fatalf("%s %s fixture rejected: %v", name, class, err)
			}
		}
		invalid := readJSON(t, filepath.Join(root, "tests/contract/fixtures/invalid", fixtureName+".json"))
		if err := schema.Validate(invalid); err == nil {
			t.Fatalf("%s invalid fixture accepted", name)
		}
	}
}

func TestAllSchemaDocumentsCompile(t *testing.T) {
	root := repoRoot(t)
	compiler := schemaCompiler(t, root)
	err := filepath.WalkDir(filepath.Join(root, "api/schemas"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".schema.json") {
			return err
		}
		value := readJSON(t, path).(map[string]any)
		id := value["$id"].(string)
		if _, compileErr := compiler.Compile(id); compileErr != nil {
			return fmt.Errorf("compile %s: %w", path, compileErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEveryFrozenDTOHasFixtures(t *testing.T) {
	root := repoRoot(t)
	catalog := loadFixtureCatalog(t, root)
	if len(catalog.Cases) < 60 {
		t.Fatalf("frozen DTO fixture catalog is unexpectedly small: %d", len(catalog.Cases))
	}
	for _, tc := range catalog.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			var compiler *jsonschema.Compiler
			if tc.OpenAPI {
				compiler = openAPISchemaCompiler(t, root)
			} else {
				compiler = schemaCompiler(t, root)
			}
			schema, err := compiler.Compile(tc.URI)
			if err != nil {
				t.Fatalf("compile fixture schema: %v", err)
			}
			normal := catalog.Build(t, tc, fixtureNormal)
			if err := schema.Validate(normal); err != nil {
				t.Fatalf("normal generated fixture rejected: %v\nfixture: %#v", err, normal)
			}
			boundary := catalog.Build(t, tc, fixtureBoundary)
			if err := schema.Validate(boundary); err != nil {
				t.Fatalf("boundary generated fixture rejected: %v\nfixture: %#v", err, boundary)
			}
			invalid := catalog.Invalid(t, tc, normal)
			if err := schema.Validate(invalid); err == nil {
				t.Fatalf("invalid generated fixture accepted: %#v", invalid)
			}
		})
	}
}

func TestRegistries(t *testing.T) {
	root := repoRoot(t)
	var errors struct {
		Version string `json:"version"`
		Codes   map[string]struct {
			HTTPStatus int    `json:"http_status"`
			MessageKey string `json:"message_key"`
		} `json:"codes"`
	}
	readYAML(t, filepath.Join(root, "api/contracts/error-codes.yaml"), &errors)
	if errors.Version != "argus.error_codes/v1" || len(errors.Codes) == 0 {
		t.Fatal("error registry is empty or has the wrong version")
	}
	codePattern := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	messageKeys := map[string]string{}
	for code, item := range errors.Codes {
		if !codePattern.MatchString(code) || item.HTTPStatus < 100 || item.HTTPStatus > 599 {
			t.Fatalf("invalid error registry entry %s", code)
		}
		if previous, ok := messageKeys[item.MessageKey]; ok {
			t.Fatalf("message_key %s reused by %s and %s", item.MessageKey, previous, code)
		}
		messageKeys[item.MessageKey] = code
	}

	var machines stateMachineRegistry
	readYAML(t, filepath.Join(root, "api/contracts/state-machines.yaml"), &machines)
	for name, machine := range machines.Machines {
		states := map[string]bool{machine.Initial: true}
		for from, targets := range machine.Transitions {
			states[from] = true
			for _, target := range targets {
				states[target] = true
			}
		}
		for _, terminal := range machine.Terminal {
			if !states[terminal] {
				t.Fatalf("machine %s terminal %s is undefined", name, terminal)
			}
			if len(machine.Transitions[terminal]) != 0 {
				t.Fatalf("machine %s terminal %s has outgoing transitions", name, terminal)
			}
		}
	}
	if !transitionAllowed(machines, "pending_action", "prepared", "awaiting_confirmation") {
		t.Fatal("required pending action transition rejected")
	}
	for _, transition := range [][3]string{
		{"pending_action", "prepared", "executing"},
		{"pending_action", "succeeded", "executing"},
		{"execution", "result_unknown", "running"},
	} {
		if transitionAllowed(machines, transition[0], transition[1], transition[2]) {
			t.Fatalf("illegal transition accepted: %s %s -> %s", transition[0], transition[1], transition[2])
		}
	}
}

func TestPendingActionStorageSeparation(t *testing.T) {
	root := repoRoot(t)
	privateRecord := readJSON(t, filepath.Join(root, "api/schemas/action/pending-action-private.schema.json")).(map[string]any)
	planRecord := readJSON(t, filepath.Join(root, "api/schemas/action/pending-action-plan.schema.json")).(map[string]any)
	tokenRecord := readJSON(t, filepath.Join(root, "api/schemas/action/pending-action-token.schema.json")).(map[string]any)

	privateProperties := privateRecord["properties"].(map[string]any)
	for _, requiredRef := range []string{"plan_record_id", "token_record_id"} {
		if _, ok := privateProperties[requiredRef]; !ok {
			t.Fatalf("PendingActionPrivateRecord lacks %s", requiredRef)
		}
	}
	for _, forbidden := range []string{"commit_tool", "immutable_plan", "token_hash", "token_ciphertext_ref"} {
		if _, ok := privateProperties[forbidden]; ok {
			t.Fatalf("PendingActionPrivateRecord still embeds %s", forbidden)
		}
	}

	planProperties := planRecord["properties"].(map[string]any)
	for _, requiredPlanField := range []string{"commit_tool", "plan_hash", "immutable_plan"} {
		if _, ok := planProperties[requiredPlanField]; !ok {
			t.Fatalf("PendingActionPlanRecord lacks %s", requiredPlanField)
		}
	}
	for _, forbidden := range []string{"token_hash", "token_ciphertext_ref"} {
		if _, ok := planProperties[forbidden]; ok {
			t.Fatalf("PendingActionPlanRecord embeds token field %s", forbidden)
		}
	}

	tokenProperties := tokenRecord["properties"].(map[string]any)
	for _, requiredTokenField := range []string{"token_hash", "token_ciphertext_ref", "status"} {
		if _, ok := tokenProperties[requiredTokenField]; !ok {
			t.Fatalf("PendingActionTokenRecord lacks %s", requiredTokenField)
		}
	}
	for _, forbidden := range []string{"commit_tool", "immutable_plan", "plan_hash"} {
		if _, ok := tokenProperties[forbidden]; ok {
			t.Fatalf("PendingActionTokenRecord embeds plan field %s", forbidden)
		}
	}
}

func TestLabelSelectorSemanticLimits(t *testing.T) {
	root := repoRoot(t)
	var rules labelRules
	readYAML(t, filepath.Join(root, "api/contracts/label-selector-rules.yaml"), &rules)
	selector := readJSON(t, filepath.Join(root, "tests/contract/fixtures/valid/label-selector.json"))
	if err := validateSelector(selector, rules); err != nil {
		t.Fatal(err)
	}
	duplicate := map[string]any{
		"schema_version": "argus.label_selector/v1",
		"requirements": []any{
			map[string]any{"key": "team", "operator": "exists"},
			map[string]any{"key": "team", "operator": "not_exists"},
		},
	}
	if err := validateSelector(duplicate, rules); err == nil {
		t.Fatal("duplicate selector key accepted")
	}

	canonicalA := canonicalSelector(t, selector)
	reordered := readJSON(t, filepath.Join(root, "tests/contract/fixtures/valid/label-selector.json"))
	requirements := reordered.(map[string]any)["requirements"].([]any)
	sort.Slice(requirements, func(i, j int) bool { return i > j })
	if canonicalA != canonicalSelector(t, reordered) {
		t.Fatal("selector canonicalization depends on input order")
	}
	if rules.CanonicalEncoding != "rfc8785_json_canonicalization_scheme" || rules.HashAlgorithm != "sha256" {
		t.Fatal("selector canonical encoding or hash algorithm changed")
	}
	hashA := sha256.Sum256([]byte(canonicalA))
	hashB := sha256.Sum256([]byte(canonicalSelector(t, reordered)))
	if hashA != hashB {
		t.Fatal("selector hash depends on input order")
	}

	compiler := schemaCompiler(t, root)
	userLabels := compiler.MustCompile("https://argus.io/schemas/v1/labels/labels.schema.json#/$defs/UserLabels")
	if err := userLabels.Validate(map[string]any{"environment": "staging", "team": "payments"}); err != nil {
		t.Fatal(err)
	}
	if err := userLabels.Validate(map[string]any{"argus.io/collector": "leaf"}); err == nil {
		t.Fatal("reserved system label accepted as user input")
	}
	largeLabels := map[string]string{"description": strings.Repeat("a", rules.MaxLabelsJSONBytes)}
	if err := validateLabelsSize(largeLabels, rules); err == nil {
		t.Fatal("oversized labels accepted")
	}
}

func TestContextBoundaries(t *testing.T) {
	root := repoRoot(t)
	var rules struct {
		Groups map[string]struct {
			StartsWith []string `json:"starts_with"`
			Contains   []string `json:"contains"`
			EndsWith   []string `json:"ends_with"`
		} `json:"groups"`
		LegalCutAfter []string `json:"legal_cut_after"`
	}
	readYAML(t, filepath.Join(root, "api/contracts/context-boundaries.yaml"), &rules)
	legal := sliceSet(rules.LegalCutAfter)
	for name, group := range rules.Groups {
		terminalEvents := sliceSet(group.EndsWith)
		for _, event := range append(append([]string{}, group.StartsWith...), group.Contains...) {
			if legal[event] && !terminalEvents[event] {
				t.Fatalf("group %s permits a cut before terminal event %s", name, event)
			}
		}
		for _, event := range group.EndsWith {
			if !legal[event] {
				t.Fatalf("group %s terminal event %s is not a legal cut", name, event)
			}
		}
	}
}

func TestCardBridgeRules(t *testing.T) {
	root := repoRoot(t)
	var rules struct {
		BridgeVersion      string `json:"bridge_version"`
		HandshakeTransport string `json:"handshake_transport"`
		BusinessTransport  string `json:"business_transport"`
		RequireExactOrigin bool   `json:"require_exact_origin"`
		MinNonceLength     int    `json:"min_nonce_length"`
		MaxMessageBytes    int    `json:"max_message_bytes"`
	}
	readYAML(t, filepath.Join(root, "api/contracts/card-bridge-rules.yaml"), &rules)
	if rules.BridgeVersion != "argus.card_bridge/v1" || rules.HandshakeTransport != "window_post_message" || rules.BusinessTransport != "message_port" || !rules.RequireExactOrigin || rules.MinNonceLength < 16 || rules.MaxMessageBytes > 1<<20 {
		t.Fatal("unsafe bridge limits")
	}
	if err := validateBridgeSequence([]uint64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := validateBridgeSequence([]uint64{1, 1}); err == nil {
		t.Fatal("duplicate bridge sequence accepted")
	}
	if err := validateBridgeSequence([]uint64{2, 1}); err == nil {
		t.Fatal("out-of-order bridge sequence accepted")
	}

	valid := readJSON(t, filepath.Join(root, "tests/contract/fixtures/valid/bridge-message.json"))
	compiler := schemaCompiler(t, root)
	schema := compiler.MustCompile("https://argus.io/schemas/v1/card/card.schema.json#/$defs/BridgeMessage")
	wrongVersion := cloneJSON(t, valid).(map[string]any)
	wrongVersion["bridge_version"] = "argus.card_bridge/v2"
	if err := schema.Validate(wrongVersion); err == nil {
		t.Fatal("wrong bridge version accepted")
	}
	if err := validateBridgeContext(valid, "https://cards.argus.test", "https://evil.test", "0123456789abcdef", rules.MaxMessageBytes, map[string]bool{"cab_01": true}); err == nil {
		t.Fatal("wrong bridge origin accepted")
	}
	if err := validateBridgeContext(valid, "https://cards.argus.test", "https://cards.argus.test", "wrong-nonce-value", rules.MaxMessageBytes, map[string]bool{"cab_01": true}); err == nil {
		t.Fatal("wrong bridge nonce accepted")
	}
	forged := cloneJSON(t, valid).(map[string]any)
	forged["payload"].(map[string]any)["action_binding_id"] = "cab_forged"
	if err := validateBridgeContext(forged, "https://cards.argus.test", "https://cards.argus.test", "0123456789abcdef", rules.MaxMessageBytes, map[string]bool{"cab_01": true}); err == nil {
		t.Fatal("forged binding id accepted")
	}
	oversized := cloneJSON(t, valid).(map[string]any)
	oversized["payload"] = map[string]any{"blob": strings.Repeat("a", rules.MaxMessageBytes)}
	if err := validateBridgeContext(oversized, "https://cards.argus.test", "https://cards.argus.test", "0123456789abcdef", rules.MaxMessageBytes, map[string]bool{"cab_01": true}); err == nil {
		t.Fatal("oversized bridge message accepted")
	}
}

func TestStreamRules(t *testing.T) {
	root := repoRoot(t)
	var rules struct {
		SSE struct {
			ResumeHeader    string   `json:"resume_header"`
			HeartbeatFrame  string   `json:"heartbeat_frame"`
			TerminalEvents  []string `json:"terminal_events"`
			StaleCursorCode []string `json:"stale_cursor_error_codes"`
		} `json:"sse"`
		Streams struct {
			CloseReasons []string `json:"close_reasons"`
		} `json:"websocket_and_grpc"`
	}
	readYAML(t, filepath.Join(root, "api/contracts/stream-rules.yaml"), &rules)
	if rules.SSE.ResumeHeader != "Last-Event-ID" || rules.SSE.HeartbeatFrame != "comment" {
		t.Fatal("SSE resume or heartbeat contract changed")
	}
	if len(rules.SSE.TerminalEvents) == 0 || len(rules.SSE.StaleCursorCode) == 0 || len(rules.Streams.CloseReasons) == 0 {
		t.Fatal("stream terminal or close reasons are empty")
	}
}

func TestActionAndContextRules(t *testing.T) {
	root := repoRoot(t)
	var actionRules struct {
		Preview struct {
			MinimumTokenEntropyBits int `json:"minimum_token_entropy_bits"`
		} `json:"preview"`
		Commit struct {
			ModelAgentVisibility  string   `json:"model_agent_visibility"`
			AllowedCallers        []string `json:"allowed_callers"`
			AcceptedBusinessField []string `json:"accepted_business_fields"`
		} `json:"commit"`
	}
	readYAML(t, filepath.Join(root, "api/contracts/action-rules.yaml"), &actionRules)
	if actionRules.Preview.MinimumTokenEntropyBits < 256 || actionRules.Commit.ModelAgentVisibility != "hidden" || len(actionRules.Commit.AcceptedBusinessField) != 0 || !sliceSet(actionRules.Commit.AllowedCallers)["action_executor"] {
		t.Fatal("unsafe preview/commit contract")
	}

	var contextRules struct {
		Formula                         string `json:"hard_input_limit_formula"`
		RetentionUnit                   string `json:"retention_unit"`
		CutBoundary                     string `json:"cut_boundary"`
		CompactionMode                  string `json:"compaction_mode"`
		ProviderCompactionAuthoritative bool   `json:"provider_compaction_authoritative"`
	}
	readYAML(t, filepath.Join(root, "api/contracts/context-budget-rules.yaml"), &contextRules)
	if contextRules.Formula != "context_window_tokens-reserved_output_tokens-safety_margin_tokens" || contextRules.RetentionUnit != "tokens" || contextRules.CutBoundary != "complete_event_group" || contextRules.CompactionMode != "incremental" || contextRules.ProviderCompactionAuthoritative {
		t.Fatal("unsafe context budget contract")
	}
}

func TestAPIRules(t *testing.T) {
	root := repoRoot(t)
	var rules struct {
		JSONFieldCase string `json:"json_field_case"`
		RESTPrefix    string `json:"rest_prefix"`
		Cursor        struct {
			OpaqueAndSigned   bool     `json:"opaque_and_signed"`
			Binds             []string `json:"binds"`
			ClientConstructed bool     `json:"client_constructible"`
		} `json:"cursor"`
		Batch struct {
			DefaultAtomic bool `json:"default_atomic"`
		} `json:"batch"`
		Partial struct {
			MayNotIncludeHiddenNames bool `json:"may_not_include_hidden_resource_names"`
		} `json:"partial"`
	}
	readYAML(t, filepath.Join(root, "api/contracts/api-rules.yaml"), &rules)
	bound := sliceSet(rules.Cursor.Binds)
	for _, required := range []string{"enterprise_id", "authorization_version", "data_scope_hash", "filter_hash", "sort", "expires_at"} {
		if !bound[required] {
			t.Fatalf("cursor is not bound to %s", required)
		}
	}
	if rules.JSONFieldCase != "snake_case" || rules.RESTPrefix != "/api/v1" || !rules.Cursor.OpaqueAndSigned || rules.Cursor.ClientConstructed || !rules.Batch.DefaultAtomic || !rules.Partial.MayNotIncludeHiddenNames {
		t.Fatal("unsafe common API contract")
	}
}

func TestContextSnapshotRange(t *testing.T) {
	root := repoRoot(t)
	valid := readJSON(t, filepath.Join(root, "tests/contract/fixtures/valid/context-snapshot.json"))
	if err := validateSnapshotRange(valid); err != nil {
		t.Fatal(err)
	}
	invalid := readJSON(t, filepath.Join(root, "tests/contract/fixtures/invalid/context-snapshot.json"))
	if err := validateSnapshotRange(invalid); err == nil {
		t.Fatal("reversed snapshot source range accepted")
	}
	cases := []struct {
		name        string
		events      []contextEvent
		invalidCuts []int
		validCut    int
	}{
		{
			name: "tool call and result",
			events: []contextEvent{
				{Sequence: 1, Type: "user_message"},
				{Sequence: 2, Type: "tool_call_requested", GroupID: "tool_01"},
				{Sequence: 3, Type: "tool_call_started", GroupID: "tool_01"},
				{Sequence: 4, Type: "tool_call_result", GroupID: "tool_01"},
				{Sequence: 5, Type: "assistant_message"},
			},
			invalidCuts: []int{2, 3},
			validCut:    4,
		},
		{
			name: "pending action confirmation approval and execution",
			events: []contextEvent{
				{Sequence: 1, Type: "pending_action_created", GroupID: "action_01"},
				{Sequence: 2, Type: "user_confirmation", GroupID: "action_01"},
				{Sequence: 3, Type: "approval_update", GroupID: "action_01"},
				{Sequence: 4, Type: "execution_update", GroupID: "action_01"},
				{Sequence: 5, Type: "assistant_message"},
			},
			invalidCuts: []int{1, 2, 3},
			validCut:    4,
		},
		{
			name: "multi-event execution",
			events: []contextEvent{
				{Sequence: 1, Type: "execution_update", GroupID: "execution_01"},
				{Sequence: 2, Type: "execution_update", GroupID: "execution_01"},
				{Sequence: 3, Type: "execution_update", GroupID: "execution_01"},
				{Sequence: 4, Type: "run_state_changed"},
			},
			invalidCuts: []int{1, 2},
			validCut:    3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]contextEvent(nil), tc.events...)
			for _, cut := range tc.invalidCuts {
				if err := validateCompactionCut(tc.events, cut); err == nil {
					t.Fatalf("context snapshot accepted a cut after sequence %d", cut)
				}
			}
			if err := validateCompactionCut(tc.events, tc.validCut); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, tc.events) {
				t.Fatal("context validation mutated or replaced original conversation events")
			}
		})
	}
}

func TestProtoTrustBoundary(t *testing.T) {
	root := repoRoot(t)
	connector, err := os.ReadFile(filepath.Join(root, "api/proto/argus/connector/v1/connector.proto"))
	if err != nil {
		t.Fatal(err)
	}
	hello := regexp.MustCompile(`(?s)message ConnectorHello \{(.*?)\n\}`).FindSubmatch(connector)
	if len(hello) != 2 {
		t.Fatal("ConnectorHello not found")
	}
	if bytes.Contains(bytes.ToLower(hello[1]), []byte("enterprise_id")) {
		t.Fatal("ConnectorHello may not claim enterprise identity")
	}
	if bytes.Contains(bytes.ToLower(hello[1]), []byte("connector_id")) {
		t.Fatal("ConnectorHello may not claim registered connector identity")
	}
	telemetry, err := os.ReadFile(filepath.Join(root, "api/proto/argus/telemetry/v1/telemetry.proto"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"enterprise_id", "resource_id", "collector_id"} {
		if !bytes.Contains(telemetry, []byte(field)) {
			t.Fatalf("trusted telemetry identity is missing %s", field)
		}
	}
	resolveRequest := regexp.MustCompile(`(?s)message ResolveCollectorRequest \{(.*?)\n\}`).FindSubmatch(telemetry)
	if len(resolveRequest) != 2 {
		t.Fatal("ResolveCollectorRequest not found")
	}
	for _, field := range []string{"enterprise_id", "resource_id", "collector_id"} {
		if bytes.Contains(bytes.ToLower(resolveRequest[1]), []byte(field)) {
			t.Fatalf("collector authentication request may not claim trusted %s", field)
		}
	}
}

func TestForbiddenContractTerms(t *testing.T) {
	root := repoRoot(t)
	for _, directory := range []string{"api/openapi", "api/proto", "api/schemas"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || strings.Contains(path, "pending-action-private") {
				return err
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if regexp.MustCompile(`(?i)(project_id|ProjectId|EnterpriseMembership)`).Match(data) {
				return fmt.Errorf("forbidden first-version term in %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	checkLegacyWebBaseline(t, root)
}

func TestPublicFixturesContainNoPrivateFields(t *testing.T) {
	root := repoRoot(t)
	forbidden := regexp.MustCompile(`(?i)^(argus__token|token|secret|credential|private_params?|commit_tool|remote_access_ticket)$`)
	err := filepath.WalkDir(filepath.Join(root, "tests/contract/fixtures/valid"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		return walkKeys(readJSON(t, path), func(key string) error {
			if forbidden.MatchString(key) {
				return fmt.Errorf("private field %s in %s", key, path)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedContracts(t *testing.T) {
	root := repoRoot(t)
	privateNames := regexp.MustCompile(`(?m)\b(token_hash|token_ciphertext_ref|immutable_plan|commit_tool|plan_record_id|token_record_id|fence_token|lease_owner|payload_ref|eligible_subject_refs)\b`)
	typescriptRoot := filepath.Join(root, "web/packages/api-client/src/generated")
	err := filepath.WalkDir(typescriptRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".ts") {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if match := privateNames.Find(data); match != nil {
			return fmt.Errorf("private server field leaked into generated TypeScript %s: %s", path, match)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"internal/gen", "web/packages/api-client/src/generated"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if lines := bytes.Count(data, []byte("\n")) + 1; lines > 2000 {
				return fmt.Errorf("generated file %s has %d lines", path, lines)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestContractCompatibility(t *testing.T) {
	root := repoRoot(t)
	pathSet := map[string]bool{
		"api/openapi/generated/argus.bundle.json": true,
	}
	for _, directory := range []string{"api/schemas", "api/contracts"} {
		_ = filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !(strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".yaml")) {
				return err
			}
			relative, _ := filepath.Rel(root, path)
			pathSet[filepath.ToSlash(relative)] = true
			return nil
		})
	}
	for _, path := range gitFileList(root, "origin/main", "api/schemas", "api/contracts") {
		pathSet[path] = true
	}
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	checked := 0
	for _, path := range paths {
		base, ok := gitFile(root, "origin/main", path)
		if !ok {
			continue
		}
		current, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("contract file removed: %s", path)
		}
		var oldValue, newValue any
		decodeStructured(t, path, base, &oldValue)
		decodeStructured(t, path, current, &newValue)
		if err := compatible(path, oldValue, newValue); err != nil {
			t.Fatal(err)
		}
		checked++
	}
	if checked == 0 {
		t.Log("origin/main has no M0 contract baseline; this merge establishes it")
	}
}

func TestCompatibilityRules(t *testing.T) {
	oldValue := map[string]any{"type": "object", "required": []any{"id"}, "properties": map[string]any{"id": map[string]any{"type": "string"}, "status": map[string]any{"enum": []any{"active", "disabled"}}}}
	removed := map[string]any{"type": "object", "required": []any{"id"}, "properties": map[string]any{"id": map[string]any{"type": "string"}}}
	if err := compatible("synthetic", oldValue, removed); err == nil {
		t.Fatal("removed property was not detected")
	}
	addedRequired := map[string]any{"type": "object", "required": []any{"id", "name"}, "properties": map[string]any{"id": map[string]any{"type": "string"}, "status": map[string]any{"enum": []any{"active", "disabled"}}, "name": map[string]any{"type": "string"}}}
	if err := compatible("synthetic", oldValue, addedRequired); err == nil {
		t.Fatal("new required property was not detected")
	}
}

type stateMachineRegistry struct {
	Machines map[string]struct {
		Initial     string              `json:"initial"`
		Terminal    []string            `json:"terminal"`
		Transitions map[string][]string `json:"transitions"`
	} `json:"machines"`
}

type labelRules struct {
	MaxRequirements    int    `json:"max_requirements"`
	MaxValuesPerIn     int    `json:"max_values_per_in"`
	MaxTotalValues     int    `json:"max_total_values"`
	MaxLabelsJSONBytes int    `json:"max_labels_json_bytes"`
	CanonicalEncoding  string `json:"canonical_encoding"`
	HashAlgorithm      string `json:"hash_algorithm"`
}

type contextEvent struct {
	Sequence int
	Type     string
	GroupID  string
}

type fixtureMode int

const (
	fixtureNormal fixtureMode = iota
	fixtureBoundary
)

type fixtureCase struct {
	Name     string
	URI      string
	Document string
	Pointer  string
	OpenAPI  bool
}

type fixtureCatalog struct {
	Cases     []fixtureCase
	Documents map[string]map[string]any
}

func loadFixtureCatalog(t *testing.T, root string) fixtureCatalog {
	t.Helper()
	catalog := fixtureCatalog{Documents: map[string]map[string]any{}}
	openAPIPath := filepath.Join(root, "api/openapi/generated/argus.bundle.json")
	openAPI := readJSON(t, openAPIPath).(map[string]any)
	openAPIID := "https://argus.io/openapi/v1/argus.bundle.json"
	catalog.Documents[openAPIID] = openAPI
	components := openAPI["components"].(map[string]any)["schemas"].(map[string]any)
	componentNames := make([]string, 0, len(components))
	for name := range components {
		componentNames = append(componentNames, name)
	}
	sort.Strings(componentNames)
	for _, name := range componentNames {
		component := components[name].(map[string]any)
		if _, imported := component["$id"]; imported {
			continue
		}
		if ref, ok := component["$ref"].(string); ok {
			target := jsonPointer(t, openAPI, strings.TrimPrefix(ref, "#")).(map[string]any)
			if _, imported := target["$id"]; imported {
				continue
			}
		}
		pointer := "/components/schemas/" + escapeJSONPointer(name)
		catalog.Cases = append(catalog.Cases, fixtureCase{
			Name: "openapi/" + name, URI: openAPIID + "#" + pointer,
			Document: openAPIID, Pointer: pointer, OpenAPI: true,
		})
	}

	err := filepath.WalkDir(filepath.Join(root, "api/schemas"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".schema.json") {
			return err
		}
		document := readJSON(t, path).(map[string]any)
		id, ok := document["$id"].(string)
		if !ok || id == "" {
			return fmt.Errorf("schema document lacks $id: %s", path)
		}
		catalog.Documents[id] = document
		relative, _ := filepath.Rel(filepath.Join(root, "api/schemas"), path)
		name := strings.TrimSuffix(filepath.ToSlash(relative), ".schema.json")
		catalog.Cases = append(catalog.Cases, fixtureCase{Name: "schema/" + name, URI: id, Document: id})
		if definitions, ok := document["$defs"].(map[string]any); ok {
			names := make([]string, 0, len(definitions))
			for definitionName := range definitions {
				names = append(names, definitionName)
			}
			sort.Strings(names)
			for _, definitionName := range names {
				pointer := "/$defs/" + escapeJSONPointer(definitionName)
				catalog.Cases = append(catalog.Cases, fixtureCase{
					Name: "schema/" + name + "/" + definitionName,
					URI:  id + "#" + pointer, Document: id, Pointer: pointer,
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(catalog.Cases, func(i, j int) bool { return catalog.Cases[i].Name < catalog.Cases[j].Name })
	return catalog
}

func (catalog fixtureCatalog) Build(t *testing.T, tc fixtureCase, mode fixtureMode) any {
	t.Helper()
	document := catalog.Documents[tc.Document]
	node := any(document)
	if tc.Pointer != "" {
		node = jsonPointer(t, document, tc.Pointer)
	}
	return catalog.sample(t, node, tc.Document, mode, map[string]bool{})
}

func (catalog fixtureCatalog) sample(t *testing.T, raw any, documentID string, mode fixtureMode, stack map[string]bool) any {
	t.Helper()
	node, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	if ref, ok := node["$ref"].(string); ok {
		resolvedID, pointer := resolveSchemaReference(t, documentID, ref)
		key := resolvedID + "#" + pointer
		if stack[key] {
			return nil
		}
		stack[key] = true
		resolved := any(catalog.Documents[resolvedID])
		if pointer != "" {
			resolved = jsonPointer(t, catalog.Documents[resolvedID], pointer)
		}
		value := catalog.sample(t, resolved, resolvedID, mode, stack)
		delete(stack, key)
		return value
	}
	if value, exists := node["const"]; exists {
		return cloneJSON(t, value)
	}
	if values, ok := node["enum"].([]any); ok && len(values) > 0 {
		index := 0
		if mode == fixtureBoundary {
			index = len(values) - 1
		}
		return cloneJSON(t, values[index])
	}
	if schemas, ok := node["allOf"].([]any); ok {
		result := map[string]any{}
		for _, schema := range schemas {
			part := catalog.sample(t, schema, documentID, mode, stack)
			object, ok := part.(map[string]any)
			if !ok {
				continue
			}
			for key, value := range object {
				result[key] = value
			}
		}
		for key, value := range catalog.sampleObject(t, node, documentID, mode, stack) {
			result[key] = value
		}
		return result
	}
	typeName := schemaType(node)
	if typeName == "" {
		for _, union := range []string{"oneOf", "anyOf"} {
			if choices, ok := node[union].([]any); ok && len(choices) > 0 {
				index := 0
				if mode == fixtureBoundary {
					index = len(choices) - 1
				}
				return catalog.sample(t, choices[index], documentID, mode, stack)
			}
		}
	}
	switch typeName {
	case "object":
		return catalog.sampleObject(t, node, documentID, mode, stack)
	case "array":
		count := int(numberOr(node["minItems"], 0))
		if mode == fixtureBoundary && count == 0 {
			count = 1
		}
		result := make([]any, 0, count)
		for index := 0; index < count; index++ {
			itemSchema, _ := node["items"].(map[string]any)
			value := catalog.sample(t, itemSchema, documentID, mode, stack)
			resolvedItem, _ := catalog.resolveNode(t, itemSchema, documentID, map[string]bool{})
			if enumValues, ok := resolvedItem["enum"].([]any); ok && index < len(enumValues) {
				value = cloneJSON(t, enumValues[index])
			} else if index > 0 {
				if textValue, ok := value.(string); ok {
					value = fmt.Sprintf("%s%d", textValue, index+1)
				}
			}
			result = append(result, value)
		}
		return result
	case "integer":
		if mode == fixtureBoundary {
			if maximum, ok := numberValue(node["maximum"]); ok {
				return int64(maximum)
			}
		}
		return int64(numberOr(node["minimum"], 1))
	case "number":
		return numberOr(node["minimum"], 1)
	case "boolean":
		return mode == fixtureBoundary
	case "null":
		return nil
	case "string":
		return schemaString(node, mode)
	default:
		if _, ok := node["properties"]; ok {
			copyNode := cloneJSON(t, node).(map[string]any)
			copyNode["type"] = "object"
			return catalog.sample(t, copyNode, documentID, mode, stack)
		}
		return map[string]any{}
	}
}

func (catalog fixtureCatalog) sampleObject(t *testing.T, node map[string]any, documentID string, mode fixtureMode, stack map[string]bool) map[string]any {
	t.Helper()
	result := map[string]any{}
	properties, _ := node["properties"].(map[string]any)
	required := valueSet(node["required"])
	names := make([]string, 0, len(properties))
	for name := range properties {
		if required[name] || mode == fixtureBoundary {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		result[name] = catalog.sample(t, properties[name], documentID, mode, stack)
	}
	minimum := int(numberOr(node["minProperties"], 0))
	if mode == fixtureBoundary && minimum == 0 && len(properties) == 0 {
		minimum = 1
	}
	additional, additionalSchema := node["additionalProperties"].(map[string]any)
	for len(result) < minimum || (mode == fixtureBoundary && additionalSchema && len(result) == 0) {
		name := fmt.Sprintf("field_%d", len(result)+1)
		result[name] = catalog.sample(t, additional, documentID, mode, stack)
	}
	if result["execution_mode"] == "parallel_safe" {
		result["risk"] = "read"
	}
	if toolName, ok := result["tool_name"].(string); ok && strings.HasSuffix(toolName, ".commit") {
		result["agent_visibility"] = "hidden"
		result["execution_mode"] = "sequential"
	}
	return result
}

func (catalog fixtureCatalog) Invalid(t *testing.T, tc fixtureCase, normal any) any {
	t.Helper()
	document := catalog.Documents[tc.Document]
	node := any(document)
	if tc.Pointer != "" {
		node = jsonPointer(t, document, tc.Pointer)
	}
	resolved, resolvedDocument := catalog.resolveNode(t, node, tc.Document, map[string]bool{})
	if object, ok := normal.(map[string]any); ok {
		invalid := cloneJSON(t, object).(map[string]any)
		if required, ok := resolved["required"].([]any); ok && len(required) > 0 {
			delete(invalid, required[0].(string))
			return invalid
		}
		if additional, exists := resolved["additionalProperties"].(bool); exists && !additional {
			invalid["unexpected_field"] = true
			return invalid
		}
		invalid["INVALID/PRIVATE_TOKEN"] = true
		return invalid
	}
	_ = resolvedDocument
	switch normal.(type) {
	case string:
		return 42
	case float64, int64, int:
		return "not-a-number"
	case bool:
		return "not-a-boolean"
	case []any:
		return map[string]any{}
	case nil:
		return map[string]any{"private_token": true}
	default:
		return nil
	}
}

func (catalog fixtureCatalog) resolveNode(t *testing.T, raw any, documentID string, seen map[string]bool) (map[string]any, string) {
	t.Helper()
	node := raw.(map[string]any)
	ref, ok := node["$ref"].(string)
	if !ok {
		return node, documentID
	}
	resolvedID, pointer := resolveSchemaReference(t, documentID, ref)
	key := resolvedID + "#" + pointer
	if seen[key] {
		return node, documentID
	}
	seen[key] = true
	resolved := any(catalog.Documents[resolvedID])
	if pointer != "" {
		resolved = jsonPointer(t, catalog.Documents[resolvedID], pointer)
	}
	return catalog.resolveNode(t, resolved, resolvedID, seen)
}

func resolveSchemaReference(t *testing.T, documentID, ref string) (string, string) {
	t.Helper()
	base, err := url.Parse(documentID)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := url.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	resolved := base.ResolveReference(reference)
	pointer := resolved.Fragment
	resolved.Fragment = ""
	return resolved.String(), pointer
}

func jsonPointer(t *testing.T, document map[string]any, pointer string) any {
	t.Helper()
	current := any(document)
	for _, rawPart := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(rawPart, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("JSON pointer %s traversed a non-object", pointer)
		}
		current, ok = object[part]
		if !ok {
			t.Fatalf("JSON pointer %s does not exist", pointer)
		}
	}
	return current
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func schemaType(node map[string]any) string {
	switch value := node["type"].(type) {
	case string:
		return value
	case []any:
		for _, item := range value {
			if item != "null" {
				return item.(string)
			}
		}
	}
	return ""
}

func schemaString(node map[string]any, mode fixtureMode) string {
	if format, _ := node["format"].(string); format != "" {
		switch format {
		case "date-time":
			return "2026-08-16T00:00:00Z"
		case "email":
			return "user@example.com"
		case "uuid":
			return "0198b2b4-6dc0-7a2f-8d36-9f8ff244db18"
		case "uri", "url":
			return "https://argus.example.com"
		}
	}
	pattern, _ := node["pattern"].(string)
	samples := []struct {
		contains string
		value    string
	}{
		{"errors\\.", "errors.contract.invalid"},
		{"\\.commit$", "host.update.commit"},
		{"argus\\.io/", "argus.io/system"},
		{"[A-Z]{3}", "USD"},
		{"[A-Z][A-Z0-9_]", "VALID_CODE"},
		{"[a-zA-Z][a-zA-Z0-9_.]", "resource.cpu"},
		{"^[a-z][a-z0-9_]*$", "field_name"},
		{"^[a-z][a-z0-9_.-]+$", "resource.read"},
		{"^[a-z][a-z0-9_.]+$", "resource.read"},
		{"^[a-z][a-z0-9]*", "environment"},
		{"^[a-z0-9]", "production"},
		{"^\\$", "$.items"},
		{"^[A-Za-z0-9", "request_00000001"},
		{"^sha256:[a-f0-9]{64}$", "sha256:" + strings.Repeat("a", 64)},
		{"^[a-f0-9]{64}$", strings.Repeat("a", 64)},
		{"^argus_ak_", "argus_ak_ABC123.secret_abcdefghijklmnopqrstuvwxyz012345"},
		{"^wss://", "wss://remote.argus.example/v1/sessions/0198b2b4-6dc0-7a2f-8d36-9f8ff244db18"},
		{"^[a-z][a-z0-9-]{1,62}$", "example"},
	}
	for _, sample := range samples {
		if strings.Contains(pattern, sample.contains) {
			return sample.value
		}
	}
	length := int(numberOr(node["minLength"], 1))
	if length == 0 {
		length = 1
	}
	if mode == fixtureBoundary {
		if maximum, ok := numberValue(node["maxLength"]); ok && maximum <= 256 {
			length = int(maximum)
		}
	}
	return strings.Repeat("a", length)
}

func numberOr(value any, fallback float64) float64 {
	if number, ok := numberValue(value); ok {
		return number
	}
	return fallback
}

func transitionAllowed(registry stateMachineRegistry, machine, from, to string) bool {
	for _, target := range registry.Machines[machine].Transitions[from] {
		if target == to {
			return true
		}
	}
	return false
}

func validateSelector(value any, rules labelRules) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("selector is not an object")
	}
	requirements, ok := root["requirements"].([]any)
	if !ok || len(requirements) > rules.MaxRequirements {
		return fmt.Errorf("invalid requirement count")
	}
	keys := map[string]bool{}
	totalValues := 0
	for _, raw := range requirements {
		requirement := raw.(map[string]any)
		key := requirement["key"].(string)
		if keys[key] {
			return fmt.Errorf("duplicate selector key %s", key)
		}
		keys[key] = true
		if values, ok := requirement["values"].([]any); ok {
			totalValues += len(values)
			if requirement["operator"] == "in" && len(values) > rules.MaxValuesPerIn {
				return fmt.Errorf("too many values for %s", key)
			}
		}
	}
	if totalValues > rules.MaxTotalValues {
		return fmt.Errorf("selector has too many total values")
	}
	return nil
}

func canonicalSelector(t *testing.T, value any) string {
	t.Helper()
	copyValue := cloneJSON(t, value).(map[string]any)
	requirements := copyValue["requirements"].([]any)
	sort.Slice(requirements, func(i, j int) bool {
		a, b := requirements[i].(map[string]any), requirements[j].(map[string]any)
		return fmt.Sprint(a["key"], a["operator"], a["values"]) < fmt.Sprint(b["key"], b["operator"], b["values"])
	})
	for _, raw := range requirements {
		if values, ok := raw.(map[string]any)["values"].([]any); ok {
			sort.Slice(values, func(i, j int) bool { return fmt.Sprint(values[i]) < fmt.Sprint(values[j]) })
		}
	}
	data, err := json.Marshal(copyValue)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func validateBridgeSequence(sequences []uint64) error {
	for index := 1; index < len(sequences); index++ {
		if sequences[index] <= sequences[index-1] {
			return fmt.Errorf("bridge sequence must be strictly increasing")
		}
	}
	return nil
}

func validateBridgeContext(message any, expectedOrigin, origin, expectedNonce string, maxBytes int, bindings map[string]bool) error {
	if origin != expectedOrigin {
		return fmt.Errorf("bridge origin mismatch")
	}
	root, ok := message.(map[string]any)
	if !ok || root["nonce"] != expectedNonce {
		return fmt.Errorf("bridge nonce mismatch")
	}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return fmt.Errorf("bridge message exceeds %d bytes", maxBytes)
	}
	payload, _ := root["payload"].(map[string]any)
	for _, key := range []string{"query_binding_id", "action_binding_id"} {
		if bindingID, ok := payload[key].(string); ok && !bindings[bindingID] {
			return fmt.Errorf("unknown binding id %s", bindingID)
		}
	}
	return nil
}

func validateCompactionCut(events []contextEvent, cutAfter int) error {
	before := map[string]bool{}
	after := map[string]bool{}
	for _, event := range events {
		if event.GroupID == "" {
			continue
		}
		if event.Sequence <= cutAfter {
			before[event.GroupID] = true
		} else {
			after[event.GroupID] = true
		}
	}
	for groupID := range before {
		if after[groupID] {
			return fmt.Errorf("cut splits event group %s", groupID)
		}
	}
	return nil
}

func validateLabelsSize(labels map[string]string, rules labelRules) error {
	data, err := json.Marshal(labels)
	if err != nil {
		return err
	}
	if len(data) > rules.MaxLabelsJSONBytes {
		return fmt.Errorf("labels exceed %d bytes", rules.MaxLabelsJSONBytes)
	}
	return nil
}

func validateSnapshotRange(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("snapshot is not an object")
	}
	from, fromOK := root["source_from_sequence"].(float64)
	through, throughOK := root["source_through_sequence"].(float64)
	kept, keptOK := root["first_kept_sequence"].(float64)
	if !fromOK || !throughOK || !keptOK || from > through || kept != through+1 {
		return fmt.Errorf("invalid snapshot source range")
	}
	return nil
}

func checkLegacyWebBaseline(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(filepath.Join(root, "web"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || strings.Contains(path, "node_modules") || strings.Contains(path, "/dist/") ||
			strings.Contains(path, "/test-results/") || strings.Contains(path, "/playwright-report/") {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for lineNumber, line := range strings.Split(string(data), "\n") {
			if legacyPattern.MatchString(line) {
				relative, _ := filepath.Rel(root, path)
				t.Errorf("legacy web contract in %s:%d: %s", filepath.ToSlash(relative), lineNumber+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
}

func compatible(path string, oldValue, newValue any) error {
	oldMap, oldOK := oldValue.(map[string]any)
	newMap, newOK := newValue.(map[string]any)
	if oldOK {
		if !newOK {
			return fmt.Errorf("%s changed object shape", path)
		}
		if oldProps, ok := oldMap["properties"].(map[string]any); ok {
			newProps, _ := newMap["properties"].(map[string]any)
			for name := range oldProps {
				if _, exists := newProps[name]; !exists {
					return fmt.Errorf("%s removed property %s", path, name)
				}
			}
		}
		if requiresStableMapKeys(path) {
			for name := range oldMap {
				if _, exists := newMap[name]; !exists {
					return fmt.Errorf("%s removed entry %s", path, name)
				}
			}
		}
		oldRequired := valueSet(oldMap["required"])
		for name := range valueSet(newMap["required"]) {
			if !oldRequired[name] {
				return fmt.Errorf("%s added required field %s", path, name)
			}
		}
		for _, key := range []string{"enum"} {
			newSet := valueSet(newMap[key])
			for value := range valueSet(oldMap[key]) {
				if !newSet[value] {
					return fmt.Errorf("%s removed %s value %s", path, key, value)
				}
			}
		}
		for _, key := range []string{"const", "pattern"} {
			if old, exists := oldMap[key]; exists && fmt.Sprint(old) != fmt.Sprint(newMap[key]) {
				return fmt.Errorf("%s changed %s", path, key)
			}
		}
		if oldType, exists := oldMap["type"]; exists && !typeCompatible(oldType, newMap["type"]) {
			return fmt.Errorf("%s changed type", path)
		}
		for _, limit := range []struct {
			key       string
			increased bool
		}{
			{"minimum", true}, {"exclusiveMinimum", true}, {"minLength", true}, {"minItems", true}, {"minProperties", true},
			{"maximum", false}, {"exclusiveMaximum", false}, {"maxLength", false}, {"maxItems", false}, {"maxProperties", false},
		} {
			oldNumber, oldExists := numberValue(oldMap[limit.key])
			newNumber, newExists := numberValue(newMap[limit.key])
			if oldExists && newExists && ((limit.increased && newNumber > oldNumber) || (!limit.increased && newNumber < oldNumber)) {
				return fmt.Errorf("%s tightened %s", path, limit.key)
			}
		}
		if oldAdditional, ok := oldMap["additionalProperties"].(bool); ok && oldAdditional {
			if current, exists := newMap["additionalProperties"].(bool); exists && !current {
				return fmt.Errorf("%s disallowed additional properties", path)
			}
		}
		for key, oldChild := range oldMap {
			if newChild, exists := newMap[key]; exists {
				if err := compatible(path+"/"+key, oldChild, newChild); err != nil {
					return err
				}
			}
		}
		return nil
	}
	oldSlice, oldOK := oldValue.([]any)
	newSlice, newOK := newValue.([]any)
	if oldOK && newOK {
		if strings.Contains(path, "/transitions/") || strings.HasSuffix(path, "/terminal") {
			newSet := valueSet(newSlice)
			for value := range valueSet(oldSlice) {
				if !newSet[value] {
					return fmt.Errorf("%s removed value %s", path, value)
				}
			}
		}
		for index := range oldSlice {
			if index < len(newSlice) {
				if err := compatible(fmt.Sprintf("%s/%d", path, index), oldSlice[index], newSlice[index]); err != nil {
					return err
				}
			}
		}
	}
	if strings.Contains(path, "error-codes.yaml/codes/") || strings.HasSuffix(path, "/initial") {
		if fmt.Sprint(oldValue) != fmt.Sprint(newValue) {
			return fmt.Errorf("%s changed stable registry value", path)
		}
	}
	return nil
}

func readJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func schemaCompiler(t *testing.T, root string) *jsonschema.Compiler {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	err := filepath.WalkDir(filepath.Join(root, "api/schemas"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".schema.json") {
			return err
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		document, decodeErr := jsonschema.UnmarshalJSON(file)
		if decodeErr != nil {
			return decodeErr
		}
		rootObject, ok := document.(map[string]any)
		if !ok {
			return fmt.Errorf("schema %s is not an object", path)
		}
		id, ok := rootObject["$id"].(string)
		if !ok || id == "" {
			return fmt.Errorf("schema %s has no $id", path)
		}
		return compiler.AddResource(id, document)
	})
	if err != nil {
		t.Fatal(err)
	}
	return compiler
}

func openAPISchemaCompiler(t *testing.T, root string) *jsonschema.Compiler {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "api/openapi/generated/argus.bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("https://argus.io/openapi/v1/argus.bundle.json", document); err != nil {
		t.Fatal(err)
	}
	return compiler
}

func readYAML(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func decodeStructured(t *testing.T, path string, data []byte, target any) {
	t.Helper()
	if strings.HasSuffix(path, ".json") {
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func gitFile(root, ref, path string) ([]byte, bool) {
	command := exec.Command("git", "show", ref+":"+path)
	command.Dir = root
	data, err := command.Output()
	return data, err == nil
}

func gitFileList(root, ref string, directories ...string) []string {
	args := []string{"ls-tree", "-r", "--name-only", ref, "--"}
	args = append(args, directories...)
	command := exec.Command("git", args...)
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" && (strings.HasSuffix(line, ".json") || strings.HasSuffix(line, ".yaml")) {
			result = append(result, line)
		}
	}
	return result
}

func requiresStableMapKeys(path string) bool {
	for _, suffix := range []string{"/schemas", "/properties", "/$defs", "/codes", "/machines", "/transitions", "/paths"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func typeCompatible(oldValue, newValue any) bool {
	oldSet := scalarOrSliceSet(oldValue)
	newSet := scalarOrSliceSet(newValue)
	for value := range oldSet {
		if !newSet[value] {
			return false
		}
	}
	return true
}

func scalarOrSliceSet(value any) map[string]bool {
	if values, ok := value.([]any); ok {
		return valueSet(values)
	}
	if value == nil {
		return map[string]bool{}
	}
	return map[string]bool{fmt.Sprint(value): true}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func valueSet(value any) map[string]bool {
	result := map[string]bool{}
	values, _ := value.([]any)
	for _, item := range values {
		result[fmt.Sprint(item)] = true
	}
	return result
}

func sliceSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}

func walkKeys(value any, visit func(string) error) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if err := visit(key); err != nil {
				return err
			}
			if err := walkKeys(child, visit); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := walkKeys(child, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneJSON(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var cloned any
	if err := decoder.Decode(&cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
