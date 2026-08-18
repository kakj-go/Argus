package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	cardservice "github.com/kakj-go/Argus/internal/card"
	"github.com/kakj-go/Argus/internal/conversation"
	"github.com/kakj-go/Argus/internal/integration/modelprovider"
	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type cardDraftBundle struct {
	Slug           string                     `json:"slug"`
	Name           string                     `json:"name"`
	Description    string                     `json:"description"`
	EntrypointHTML string                     `json:"entrypoint_html"`
	Manifest       json.RawMessage            `json:"manifest"`
	Bindings       []cardservice.Binding      `json:"bindings"`
	Demos          map[string]json.RawMessage `json:"demos"`
}

func (loop Loop) handleCardCommand(ctx context.Context, run db.Run, user db.EnterpriseUser, revision db.AiModelRevision, provider modelprovider.Provider, input string, command conversation.MessageCommand) error {
	if loop.Cards.Store == nil || loop.Tools == nil || !slices.Contains([]string{"interactive_card.create", "interactive_card.revise"}, command.Type) {
		return loop.finishRun(ctx, run, "failed", "card_command_invalid", "TOOL_INPUT_INVALID")
	}
	permissions, err := loop.Store.Queries.ListEffectiveUserPermissions(ctx, db.ListEffectiveUserPermissionsParams{EnterpriseID: run.EnterpriseID, UserID: user.ID, DepartmentID: user.DepartmentID})
	if err != nil || !slices.Contains(permissions, "*") && !slices.Contains(permissions, "interactive_card.create") {
		return loop.finishRun(ctx, run, "failed", "authorization_denied", "APPROVAL_NOT_ELIGIBLE")
	}
	if command.Type == "interactive_card.revise" && (command.CardID == nil || command.ExpectedRevision == nil) {
		return loop.finishRun(ctx, run, "failed", "card_command_invalid", "TOOL_INPUT_INVALID")
	}
	selectedTools := selectCardDraftTools(loop.Tools.CardCatalog(), input)
	if len(selectedTools) == 0 {
		return loop.finishRun(ctx, run, "failed", "card_context_invalid", "TOOL_INPUT_INVALID")
	}
	catalogItems := make([]map[string]any, 0, len(selectedTools))
	for _, metadata := range selectedTools {
		catalogItems = append(catalogItems, map[string]any{"tool_id": metadata.ID, "tool_family": metadata.ToolFamily,
			"output_schema_version": metadata.OutputVersion, "compatible_output_versions": metadata.CompatibleOutputVersions,
			"schema_hash": metadata.OutputSchemaHash, "output_schema": metadata.OutputSchema, "semantic_fields": metadata.SemanticFields,
			"field_types": metadata.FieldTypes})
	}
	catalog, _ := json.Marshal(catalogItems)
	system := modelprovider.Message{Role: "system", Content: "Generate one Argus CardDraftBundle. Use only the supplied read-only Tool Schema Catalog. Do not include production data, credentials, private plans, tokens, external URLs, network calls, dynamic code, or commit Tools."}
	userPrompt := modelprovider.Message{Role: "user", Content: fmt.Sprintf("Command: %s\nRequest: %s\nTool Schema Catalog: %s", command.Type, input, catalog)}
	projection, err := AssembleContext(ContextInput{ContextWindow: int(revision.ContextWindowTokens), MaxOutput: int(revision.MaxOutputTokens), System: system,
		ToolCatalog: catalogItems, Checkpoint: json.RawMessage(run.Checkpoint), CurrentInput: map[string]any{"command": command.Type, "request": input}})
	if err != nil {
		return loop.finishRun(ctx, run, "failed", "card_context_invalid", "CONTEXT_TOO_LARGE")
	}
	step, run, err := loop.startStep(ctx, run, projection)
	if err != nil {
		return err
	}
	modelCall, err := loop.createModelCall(ctx, run, step, revision, projection)
	if err != nil {
		_ = loop.failStep(ctx, run, step)
		return err
	}
	reservedAmount := tokenAmount(int64(projection.EstimatedTokens), int64(revision.MaxOutputTokens), revision.InputPricePerMillion, revision.OutputPricePerMillion)
	reservation, err := loop.Models.ReserveQuota(ctx, modelCall, user.DepartmentID, user.ID, reservedAmount)
	if err != nil {
		_ = loop.finishModelCall(ctx, modelCall, revision, 0, 0, 0, "quota_exceeded", "failed", "MODEL_QUOTA_EXCEEDED")
		_ = loop.failStep(ctx, run, step)
		return loop.finishRun(ctx, run, "failed", "quota_exceeded", "MODEL_QUOTA_EXCEEDED")
	}
	started := time.Now()
	var output strings.Builder
	var inputTokens, outputTokens int64
	err = provider.Stream(ctx, modelprovider.Request{Model: revision.ProviderModelID, Messages: []modelprovider.Message{system, userPrompt},
		ResponseSchema: cardDraftResponseSchema(), MaxTokens: int(revision.MaxOutputTokens)}, func(event modelprovider.Event) error {
		switch event.Type {
		case "text_delta":
			output.WriteString(event.Text)
		case "tool_call_delta", "tool_call_done":
			return errors.New("Card draft generation cannot call Tools")
		case "usage", "completed":
			inputTokens += event.Input
			outputTokens += event.Output
		}
		return nil
	})
	if inputTokens == 0 {
		inputTokens = int64(projection.EstimatedTokens)
	}
	if outputTokens == 0 {
		outputTokens = int64((output.Len() + 3) / 4)
	}
	actualAmount := tokenAmount(inputTokens, outputTokens, revision.InputPricePerMillion, revision.OutputPricePerMillion)
	if err != nil {
		_ = loop.finishModelCall(ctx, modelCall, revision, inputTokens, outputTokens, time.Since(started), "failed", "failed", "MODEL_COMPATIBILITY_FAILED")
		_ = loop.Models.SettleQuota(ctx, reservation, actualAmount)
		_ = loop.failStep(ctx, run, step)
		return runtime.Error{ErrorCode: "MODEL_COMPATIBILITY_FAILED", Cause: err}
	}
	if err := loop.finishModelCall(ctx, modelCall, revision, inputTokens, outputTokens, time.Since(started), "completed", "succeeded", ""); err != nil {
		return err
	}
	if err := loop.Models.SettleQuota(ctx, reservation, actualAmount); err != nil {
		return err
	}
	var bundle cardDraftBundle
	if err := json.Unmarshal([]byte(output.String()), &bundle); err != nil {
		_ = loop.failStep(ctx, run, step)
		return loop.finishRun(ctx, run, "failed", "card_draft_invalid", "CARD_STATIC_VALIDATION_FAILED")
	}
	draft := cardservice.DraftInput{Slug: bundle.Slug, Name: bundle.Name, Description: bundle.Description, HTML: []byte(bundle.EntrypointHTML),
		Manifest: bundle.Manifest, Bindings: bundle.Bindings, Demos: bundle.Demos}
	var card db.InteractiveCard
	var version db.CardVersion
	if command.Type == "interactive_card.create" {
		card, version, err = loop.Cards.CreateDraft(ctx, user.ID, run.EnterpriseID, draft)
	} else {
		card, version, err = loop.Cards.CreateGeneratedRevision(ctx, user.ID, run.EnterpriseID, *command.CardID, *command.ExpectedRevision, draft)
	}
	if err != nil {
		_ = loop.failStep(ctx, run, step)
		return loop.finishRun(ctx, run, "failed", "card_draft_invalid", "CARD_STATIC_VALIDATION_FAILED")
	}
	err = loop.Store.InTx(ctx, func(q *db.Queries) error {
		_, eventErr := conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID,
			RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, StepID: uuid.NullUUID{UUID: step.ID, Valid: true}, Type: "card_draft_created", ActorType: "service",
			Payload: map[string]any{"card_id": card.ID.String(), "revision": version.Revision, "content_hash": hex.EncodeToString(version.ContentHash)}, Classification: "internal"})
		return eventErr
	})
	if err != nil {
		return err
	}
	message := fmt.Sprintf("Interactive Card draft %s revision %d was created and remains disabled until validation and activation.", card.ID, version.Revision)
	if err := loop.persistAssistant(ctx, run, step, message, inputTokens, outputTokens); err != nil {
		return err
	}
	return loop.finishRun(ctx, run, "succeeded", "completed", "")
}

func selectCardDraftTools(catalog []mcp.Metadata, input string) []mcp.Metadata {
	type scored struct {
		metadata mcp.Metadata
		score    int
	}
	lower := strings.ToLower(input)
	scoredTools := make([]scored, 0, len(catalog))
	for _, metadata := range catalog {
		id := strings.ToLower(metadata.ID)
		score := 0
		if strings.Contains(lower, id) {
			score = 100
		} else {
			parts := strings.FieldsFunc(id, func(value rune) bool { return value == '.' || value == '_' || value == '-' })
			if len(parts) > 0 && len(parts[0]) > 2 && strings.Contains(lower, parts[0]) {
				score += 20
			}
			operation := parts[len(parts)-1]
			switch operation {
			case "list":
				if strings.Contains(lower, "list") || strings.Contains(lower, "inventory") || strings.Contains(lower, "table") || strings.Contains(lower, "overview") {
					score += 10
				}
			case "get":
				if strings.Contains(lower, "detail") {
					score += 10
				}
			case "preview":
				if strings.Contains(lower, "preview") {
					score += 10
				}
			}
		}
		if score > 0 {
			scoredTools = append(scoredTools, scored{metadata: metadata, score: score})
		}
	}
	sort.Slice(scoredTools, func(i, j int) bool {
		if scoredTools[i].score != scoredTools[j].score {
			return scoredTools[i].score > scoredTools[j].score
		}
		return scoredTools[i].metadata.ID < scoredTools[j].metadata.ID
	})
	limit := min(4, len(scoredTools))
	result := make([]mcp.Metadata, 0, limit)
	for _, item := range scoredTools[:limit] {
		result = append(result, item.metadata)
	}
	return result
}

func cardDraftResponseSchema() map[string]any {
	scenarios := []string{"default", "empty", "error", "large", "light", "dark", "zh-CN", "en-US"}
	demoProperties := map[string]any{}
	for _, scenario := range scenarios {
		demoProperties[scenario] = map[string]any{"type": "object"}
	}
	return map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"slug", "name", "description", "entrypoint_html", "manifest", "bindings", "demos"},
		"properties": map[string]any{
			"slug": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
			"entrypoint_html": map[string]any{"type": "string", "maxLength": cardservice.MaxHTMLBytes}, "manifest": map[string]any{"type": "object"},
			"bindings": map[string]any{"type": "array", "maxItems": cardservice.MaxSlots, "items": map[string]any{"type": "object"}},
			"demos":    map[string]any{"type": "object", "additionalProperties": false, "required": scenarios, "properties": demoProperties},
		}}
}
