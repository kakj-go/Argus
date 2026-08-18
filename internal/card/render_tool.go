package card

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/conversation"
	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type renderSource struct {
	db.GetCardRenderSourceRow
	metadata   mcp.Metadata
	projection map[string]any
}

type resolvedSlot struct {
	binding db.CardSlotBinding
	source  renderSource
}

type resolvedCandidate struct {
	card      db.InteractiveCard
	version   db.CardVersion
	slots     []resolvedSlot
	candidate Candidate
}

func (service Service) RegisterRenderTool(registry *mcp.Registry) error {
	if registry == nil || service.Store == nil {
		return ErrDependencyUnavailable
	}
	return registry.Register(mcp.Metadata{ID: "card.render", Risk: "read", Visibility: mcp.Visible, ExecutionMode: mcp.Sequential,
		Required: []string{"interactive_card.read"}, InputVersion: "argus.card_render/v1", OutputVersion: "argus.card_instance/v1",
		ProjectionSchema: "argus.card_instance/v1", MaxResultBytes: 64 << 10, InputSchema: map[string]any{"type": "object", "additionalProperties": false,
			"properties": map[string]any{"tool_call_ids": map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "uniqueItems": true, "items": map[string]any{"type": "string", "format": "uuid"}},
				"presentation_kind": map[string]any{"type": "string", "enum": []string{"table", "detail", "pending_action", "metric", "generic"}}},
			"required": []string{"tool_call_ids", "presentation_kind"}}, Authorize: service.authorizeRender, Validate: validateRenderInput, Execute: service.render})
}

func validateRenderInput(input map[string]any) error {
	values, ok := input["tool_call_ids"].([]any)
	if !ok || len(values) == 0 || len(values) > 16 {
		return ErrBindingInvalid
	}
	seen := map[string]bool{}
	for _, value := range values {
		text, ok := value.(string)
		if !ok || seen[text] {
			return ErrBindingInvalid
		}
		if _, err := uuid.Parse(text); err != nil {
			return ErrBindingInvalid
		}
		seen[text] = true
	}
	kind, _ := input["presentation_kind"].(string)
	if !slices.Contains([]string{"table", "detail", "pending_action", "metric", "generic"}, kind) {
		return ErrBindingInvalid
	}
	return nil
}

func (service Service) authorizeRender(ctx context.Context, call mcp.Call) error {
	enterpriseID, err := uuid.Parse(call.Enterprise)
	if err != nil {
		return err
	}
	actorID, err := uuid.Parse(call.Subject)
	if err != nil || call.SubjectType != "user" {
		return ErrBindingInvalid
	}
	user, err := service.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: actorID, EnterpriseID: enterpriseID})
	if err != nil || user.Status != "active" {
		return ErrBindingInvalid
	}
	permissions, err := service.Store.Queries.ListEffectiveUserPermissions(ctx, db.ListEffectiveUserPermissionsParams{EnterpriseID: enterpriseID, UserID: actorID, DepartmentID: user.DepartmentID})
	if err != nil || !slices.Contains(permissions, "interactive_card.read") {
		return ErrBindingInvalid
	}
	return nil
}

func (service Service) render(ctx context.Context, call mcp.Call) (mcp.Result, error) {
	enterpriseID, err := uuid.Parse(call.Enterprise)
	if err != nil {
		return mcp.Result{}, err
	}
	actorID, err := uuid.Parse(call.Subject)
	if err != nil {
		return mcp.Result{}, err
	}
	runID, err := uuid.Parse(call.RunID)
	if err != nil {
		return mcp.Result{}, ErrBindingInvalid
	}
	run, err := service.Store.Queries.GetRun(ctx, db.GetRunParams{ID: runID, EnterpriseID: enterpriseID})
	if err != nil || run.ActorUserID != actorID {
		return mcp.Result{}, ErrBindingInvalid
	}
	sources, err := service.renderSources(ctx, enterpriseID, runID, call.Input["tool_call_ids"].([]any))
	if err != nil {
		return mcp.Result{}, err
	}
	kind := call.Input["presentation_kind"].(string)
	selected, ok, err := service.selectRenderer(ctx, enterpriseID, kind, sources)
	if err != nil {
		return mcp.Result{}, err
	}
	if !ok {
		return mcp.Result{Structured: map[string]any{"rendered": false, "fallback": "text", "summary": renderFallback(sources)}}, nil
	}
	instance, err := service.persistCardInstance(ctx, run, actorID, kind, selected)
	if err != nil {
		return mcp.Result{}, err
	}
	return mcp.Result{Structured: map[string]any{"rendered": true, "card_instance_id": instance.ID.String(), "card_id": selected.card.ID.String(),
		"card_revision": selected.version.Revision, "presentation_kind": kind}}, nil
}

func (service Service) renderSources(ctx context.Context, enterpriseID, runID uuid.UUID, ids []any) ([]renderSource, error) {
	result := make([]renderSource, 0, len(ids))
	for _, raw := range ids {
		id, _ := uuid.Parse(raw.(string))
		row, err := service.Store.Queries.GetCardRenderSource(ctx, db.GetCardRenderSourceParams{ID: id, RunID: runID, EnterpriseID: enterpriseID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingInvalid
		}
		if err != nil {
			return nil, err
		}
		metadata, exists := service.Tools.Lookup(row.ToolID)
		if !exists || !metadata.CardSafe || metadata.OutputSchemaHash == "" {
			return nil, ErrDependencyUnavailable
		}
		var projection map[string]any
		if err := json.Unmarshal(row.Projection, &projection); err != nil {
			return nil, ErrBindingInvalid
		}
		result = append(result, renderSource{GetCardRenderSourceRow: row, metadata: metadata, projection: projection})
	}
	return result, nil
}

func (service Service) selectRenderer(ctx context.Context, enterpriseID uuid.UUID, kind string, sources []renderSource) (resolvedCandidate, bool, error) {
	cards, err := service.List(ctx, enterpriseID)
	if err != nil {
		return resolvedCandidate{}, false, err
	}
	resolved := make([]resolvedCandidate, 0, len(cards))
	scores := make([]Candidate, 0, len(cards))
	for _, card := range cards {
		if !card.Enabled || card.Availability != "available" || !card.ActiveVersionID.Valid {
			continue
		}
		version, err := service.Store.Queries.GetCardVersionByID(ctx, card.ActiveVersionID.UUID)
		if err != nil || version.Status != "active" {
			continue
		}
		bindings, err := service.Store.Queries.ListCardSlotBindings(ctx, version.ID)
		if err != nil {
			return resolvedCandidate{}, false, err
		}
		required := requiredManifestSlots(version.Manifest)
		matched := make([]resolvedSlot, 0, len(bindings))
		strictMatches := 0
		for _, binding := range bindings {
			for _, source := range sources {
				if service.bindingMatches(binding, source) {
					matched = append(matched, resolvedSlot{binding: binding, source: source})
					if binding.SlotKind == "data" && binding.Mode == "strict" {
						strictMatches++
					}
					break
				}
			}
		}
		resolvedNames := make([]string, 0, len(matched))
		for _, item := range matched {
			resolvedNames = append(resolvedNames, item.binding.SlotName)
		}
		requiredResolved := true
		for _, name := range required {
			if !slices.Contains(resolvedNames, name) {
				requiredResolved = false
			}
		}
		intent := 0
		if manifestPresentationKind(version.Manifest) == kind {
			intent = 1
		}
		compatibleDataSlots := 0
		for _, item := range matched {
			if item.binding.SlotKind == "data" {
				compatibleDataSlots++
			}
		}
		score := Candidate{CardID: card.ID.String(), Revision: int(version.Revision), Source: card.Source, Available: true,
			RequiredResolved: requiredResolved, StrictMatches: strictMatches, CompatibleSlots: compatibleDataSlots, IntentScore: intent}
		resolved = append(resolved, resolvedCandidate{card: card, version: version, slots: matched, candidate: score})
		scores = append(scores, score)
	}
	winner, ok := SelectCandidate(scores)
	if !ok {
		return resolvedCandidate{}, false, nil
	}
	for _, candidate := range resolved {
		if candidate.card.ID.String() == winner.CardID {
			return candidate, true, nil
		}
	}
	return resolvedCandidate{}, false, nil
}

func (service Service) bindingMatches(binding db.CardSlotBinding, source renderSource) bool {
	pendingActionSource := source.ToolID == "pending_action.get" || strings.HasSuffix(source.ToolID, ".preview")
	if binding.SlotKind == "action" && pendingActionSource &&
		(binding.ToolID == "pending_action.confirm" || binding.ToolID == "pending_action.cancel") &&
		binding.OutputSchemaVersion == source.metadata.OutputVersion && hashMatches(source.metadata.OutputSchemaHash, binding.SchemaHash) {
		return true
	}
	if binding.SlotKind == "data" && binding.ToolID == "pending_action.get" && pendingActionSource &&
		binding.OutputSchemaVersion == source.metadata.OutputVersion && hashMatches(source.metadata.OutputSchemaHash, binding.SchemaHash) {
		return true
	}
	if binding.OutputSchemaVersion == source.metadata.OutputVersion && binding.ToolID == source.ToolID && hashMatches(source.metadata.OutputSchemaHash, binding.SchemaHash) {
		return true
	}
	if binding.Mode != "preferred" || !binding.SemanticType.Valid || binding.ValueType == "" {
		return false
	}
	bound, exists := service.Tools.Lookup(binding.ToolID)
	return exists && bound.ToolFamily != "" && bound.ToolFamily == source.metadata.ToolFamily &&
		slices.Contains(source.metadata.CompatibleOutputVersions, binding.OutputSchemaVersion) &&
		source.metadata.SemanticFields[binding.FieldPath] == binding.SemanticType.String &&
		source.metadata.FieldTypes[binding.FieldPath] == binding.ValueType
}

func (service Service) persistCardInstance(ctx context.Context, run db.Run, actorID uuid.UUID, kind string, selected resolvedCandidate) (db.CardInstance, error) {
	var actions []renderAction
	for _, slot := range selected.slots {
		if slot.binding.SlotKind != "action" {
			continue
		}
		summary, _ := slot.source.projection["summary"].(map[string]any)
		actionRef, _ := summary["action_ref"].(string)
		action := strings.TrimPrefix(slot.binding.ToolID, "pending_action.")
		actions = append(actions, renderAction{SlotName: slot.binding.SlotName, ActionRef: actionRef, Action: action})
	}
	specBytes, _ := json.Marshal(renderSpec{Actions: actions})
	specHash := sha256Sum(specBytes)
	var instance db.CardInstance
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		var err error
		instance, err = q.CreateCardInstance(ctx, db.CreateCardInstanceParams{ID: newID(), EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID,
			RunID: nullUUID(run.ID), CardID: selected.card.ID, CardVersionID: selected.version.ID, ActorUserID: actorID, PresentationKind: kind,
			RenderSpec: specBytes, RenderSpecHash: specHash})
		if err != nil {
			return err
		}
		for _, slot := range selected.slots {
			sourceHash := sha256Sum(slot.source.Projection)
			if slot.binding.SlotKind == "data" {
				_, err = q.CreateCardDataSource(ctx, db.CreateCardDataSourceParams{ID: newID(), CardInstanceID: instance.ID, SlotName: slot.binding.SlotName,
					ToolCallID: slot.source.ToolCallID, ResultRef: slot.source.ResultRef, FieldPath: slot.binding.FieldPath,
					OutputSchemaVersion: slot.source.metadata.OutputVersion, SourceHash: sourceHash})
			}
			if slot.binding.SlotKind == "query" {
				inputHash := sha256Sum(slot.source.Input)
				_, err = q.CreateCardQueryBindingSpec(ctx, db.CreateCardQueryBindingSpecParams{ID: newID(), CardInstanceID: instance.ID, SlotName: slot.binding.SlotName,
					ToolID: slot.source.ToolID, FixedInput: slot.source.Input, InputHash: inputHash, OutputSchemaVersion: slot.source.metadata.OutputVersion,
					SchemaHash: slot.binding.SchemaHash})
			}
			if err != nil {
				return err
			}
		}
		_, err = conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID,
			RunID: nullUUID(run.ID), Type: "card_instance_created", ActorType: "service", Payload: map[string]any{"card": map[string]any{
				"card_instance_id": instance.ID.String(), "interactive_card_id": selected.card.ID.String(), "version": fmt.Sprint(selected.version.Revision),
				"title": selected.card.Name}, "content_hash": hex.EncodeToString(selected.version.ContentHash)}, Classification: "internal"})
		return err
	})
	return instance, err
}

func requiredManifestSlots(raw json.RawMessage) []string {
	var value struct {
		Slots []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"slots"`
	}
	_ = json.Unmarshal(raw, &value)
	result := make([]string, 0, len(value.Slots))
	for _, slot := range value.Slots {
		if slot.Required {
			result = append(result, slot.Name)
		}
	}
	return result
}

func manifestPresentationKind(raw json.RawMessage) string {
	var value struct {
		PresentationKind string `json:"presentation_kind"`
	}
	_ = json.Unmarshal(raw, &value)
	return value.PresentationKind
}

func renderFallback(sources []renderSource) string {
	identifiers := make([]string, 0, len(sources))
	for _, source := range sources {
		identifiers = append(identifiers, source.ToolID)
	}
	return "No compatible Card renderer is enabled for: " + strings.Join(identifiers, ", ")
}

func sha256Sum(value []byte) []byte {
	hash := sha256.Sum256(value)
	return hash[:]
}

func schemaHashHex(value []byte) string { return hex.EncodeToString(value) }

func renderError(format string, values ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBindingInvalid}, values...)...)
}
