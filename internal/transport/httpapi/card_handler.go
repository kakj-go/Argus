package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	actionservice "github.com/kakj-go/Argus/internal/action"
	cardservice "github.com/kakj-go/Argus/internal/card"
	cardapi "github.com/kakj-go/Argus/internal/gen/openapi/cardapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type CardHandler struct {
	Identity EnterpriseIdentityHandler
	Service  cardservice.Service
	Workflow actionservice.Service
}

func (handler CardHandler) ListInteractiveCards(ctx context.Context, _ cardapi.ListInteractiveCardsRequestObject) (cardapi.ListInteractiveCardsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "interactive_card.read")
	if apiError != nil {
		return cardapi.ListInteractiveCardsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.List(ctx, p.EnterpriseIDValue())
	if err != nil {
		return cardapi.ListInteractiveCardsdefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	result := make([]cardapi.InteractiveCard, 0, len(items))
	for _, item := range items {
		view, viewErr := handler.toInteractiveCard(ctx, item)
		if viewErr != nil {
			return cardapi.ListInteractiveCardsdefaultJSONResponse{Body: cardError(ctx, viewErr), StatusCode: cardStatus(viewErr)}, nil
		}
		result = append(result, view)
	}
	return cardapi.ListInteractiveCards200JSONResponse{Items: result, Page: cardapi.CursorPage{HasMore: false, Partial: cardapi.PartialMetadata{Partial: false, Reasons: []cardapi.PartialMetadataReasons{}}}}, nil
}

func (handler CardHandler) GetInteractiveCard(ctx context.Context, request cardapi.GetInteractiveCardRequestObject) (cardapi.GetInteractiveCardResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "interactive_card.read")
	if apiError != nil {
		return cardapi.GetInteractiveCarddefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Get(ctx, p.EnterpriseIDValue(), uuid.UUID(request.CardId))
	if err != nil {
		return cardapi.GetInteractiveCarddefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	result, err := handler.toInteractiveCard(ctx, value)
	if err != nil {
		return cardapi.GetInteractiveCarddefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	return cardapi.GetInteractiveCard200JSONResponse(result), nil
}

func (handler CardHandler) ListCardVersions(ctx context.Context, request cardapi.ListCardVersionsRequestObject) (cardapi.ListCardVersionsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "interactive_card.read")
	if apiError != nil {
		return cardapi.ListCardVersionsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListVersions(ctx, p.EnterpriseIDValue(), uuid.UUID(request.CardId))
	if err != nil {
		return cardapi.ListCardVersionsdefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	result := make([]cardapi.CardVersionSummary, 0, len(items))
	for _, item := range items {
		result = append(result, toCardVersionSummary(item))
	}
	return cardapi.ListCardVersions200JSONResponse{Items: result}, nil
}

func (handler CardHandler) GetCardVersion(ctx context.Context, request cardapi.GetCardVersionRequestObject) (cardapi.GetCardVersionResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "interactive_card.read")
	if apiError != nil {
		return cardapi.GetCardVersiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	version, err := handler.Service.GetVersion(ctx, p.EnterpriseIDValue(), uuid.UUID(request.CardId), int32(request.Revision))
	if err != nil {
		return cardapi.GetCardVersiondefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	result, err := handler.toCardVersion(ctx, version)
	if err != nil {
		return cardapi.GetCardVersiondefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	return cardapi.GetCardVersion200JSONResponse(result), nil
}

func (handler CardHandler) CreateCardConfigurationVersion(ctx context.Context, request cardapi.CreateCardConfigurationVersionRequestObject) (cardapi.CreateCardConfigurationVersionResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "interactive_card.update")
	if apiError != nil {
		return cardapi.CreateCardConfigurationVersiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return cardapi.CreateCardConfigurationVersiondefaultJSONResponse{Body: cardError(ctx, cardservice.ErrBindingInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	bindings := make([]cardservice.Binding, 0, len(request.Body.SlotBindings))
	for _, item := range request.Body.SlotBindings {
		bindings = append(bindings, bindingInput(item))
	}
	demos := make(map[string]json.RawMessage, len(request.Body.Demos))
	for _, item := range request.Body.Demos {
		data, _ := json.Marshal(item.Data)
		demos[string(item.Scenario)] = data
	}
	version, err := handler.Service.CreateConfigurationVersion(ctx, uuid.MustParse(p.ActorID()), p.EnterpriseIDValue(), uuid.UUID(request.CardId), cardservice.ConfigurationInput{
		BaseRevision: int32(request.Body.BaseRevision), ExpectedVersion: request.Body.ExpectedVersion, Name: request.Body.Name, Description: request.Body.Description,
		Bindings: bindings, Demos: demos,
	}, request.Params.IdempotencyKey)
	if err != nil {
		return cardapi.CreateCardConfigurationVersiondefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	result, err := handler.toCardVersion(ctx, version)
	if err != nil {
		return cardapi.CreateCardConfigurationVersiondefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	return cardapi.CreateCardConfigurationVersion201JSONResponse(result), nil
}

func (handler CardHandler) StartCardValidation(ctx context.Context, request cardapi.StartCardValidationRequestObject) (cardapi.StartCardValidationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "interactive_card.publish")
	if apiError != nil {
		return cardapi.StartCardValidationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return cardapi.StartCardValidationdefaultJSONResponse{Body: cardError(ctx, cardservice.ErrRuntimeValidation), StatusCode: http.StatusBadRequest}, nil
	}
	started, err := handler.Service.StartValidation(ctx, uuid.MustParse(p.ActorID()), p.EnterpriseIDValue(), uuid.UUID(request.CardId), int32(request.Body.Revision), request.Body.RuntimeVersion, request.Params.IdempotencyKey)
	if err != nil {
		return cardapi.StartCardValidationdefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	version, err := handler.Service.Store.Queries.GetCardVersionByID(ctx, started.Run.CardVersionID)
	if err != nil {
		return cardapi.StartCardValidationdefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	return cardapi.StartCardValidation201JSONResponse(toValidationRun(started.Run, version, started.Nonce)), nil
}

func (handler CardHandler) SubmitCardValidationEvidence(ctx context.Context, request cardapi.SubmitCardValidationEvidenceRequestObject) (cardapi.SubmitCardValidationEvidenceResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "interactive_card.publish")
	if apiError != nil {
		return cardapi.SubmitCardValidationEvidencedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return cardapi.SubmitCardValidationEvidencedefaultJSONResponse{Body: cardError(ctx, cardservice.ErrRuntimeValidation), StatusCode: http.StatusBadRequest}, nil
	}
	scenarios := make([]cardservice.ScenarioEvidence, 0, len(request.Body.Scenarios))
	for _, item := range request.Body.Scenarios {
		scenarios = append(scenarios, cardservice.ScenarioEvidence{Scenario: string(item.Scenario), Ready: item.Ready, ProtocolViolations: item.ProtocolViolations,
			RuntimeErrors: item.RuntimeErrors, SeriousA11yViolations: item.SeriousA11yViolations,
			MissingRequiredSlots: item.MissingRequiredSlots, SizeViolation: item.SizeViolation})
	}
	run, err := handler.Service.SubmitEvidence(ctx, uuid.MustParse(p.ActorID()), p.EnterpriseIDValue(), uuid.UUID(request.RunId), cardservice.ValidationEvidence{
		Nonce: request.Body.Nonce, ContentHash: request.Body.ContentHash, RuntimeVersion: request.Body.RuntimeVersion, Scenarios: scenarios,
	}, request.Params.IdempotencyKey)
	if err != nil {
		return cardapi.SubmitCardValidationEvidencedefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	version, err := handler.Service.Store.Queries.GetCardVersionByID(ctx, run.CardVersionID)
	if err != nil {
		return cardapi.SubmitCardValidationEvidencedefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	return cardapi.SubmitCardValidationEvidence200JSONResponse(toValidationRun(run, version, "")), nil
}

func (handler CardHandler) ChangeInteractiveCardState(ctx context.Context, request cardapi.ChangeInteractiveCardStateRequestObject) (cardapi.ChangeInteractiveCardStateResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "interactive_card.publish")
	if apiError != nil {
		return cardapi.ChangeInteractiveCardStatedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return cardapi.ChangeInteractiveCardStatedefaultJSONResponse{Body: cardError(ctx, cardservice.ErrVersionConflict), StatusCode: http.StatusBadRequest}, nil
	}
	var revision *int32
	if request.Body.Revision != nil {
		value := int32(*request.Body.Revision)
		revision = &value
	}
	value, err := handler.Service.ChangeState(ctx, uuid.MustParse(p.ActorID()), p.EnterpriseIDValue(), uuid.UUID(request.CardId), request.StateAction, request.Body.ExpectedVersion, revision, request.Params.IdempotencyKey)
	if err != nil {
		return cardapi.ChangeInteractiveCardStatedefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	result, err := handler.toInteractiveCard(ctx, value)
	if err != nil {
		return cardapi.ChangeInteractiveCardStatedefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	return cardapi.ChangeInteractiveCardState200JSONResponse(result), nil
}

func (handler CardHandler) ListToolSchemaCatalog(ctx context.Context, _ cardapi.ListToolSchemaCatalogRequestObject) (cardapi.ListToolSchemaCatalogResponseObject, error) {
	_, apiError := handler.auth(ctx, false, "", "interactive_card.read")
	if apiError != nil {
		return cardapi.ListToolSchemaCatalogdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items := handler.Service.Tools.CardCatalog()
	result := make([]cardapi.ToolSchemaCatalogEntry, 0, len(items))
	for _, item := range items {
		fields := make([]map[string]any, 0, len(item.SemanticFields))
		for path, semantic := range item.SemanticFields {
			fields = append(fields, map[string]any{"path": path, "value_type": item.FieldTypes[path], "semantic_type": semantic})
		}
		result = append(result, fromJSON[cardapi.ToolSchemaCatalogEntry](map[string]any{"tool_id": item.ID, "risk": item.Risk, "execution_mode": item.ExecutionMode,
			"tool_family": item.ToolFamily, "compatible_output_versions": item.CompatibleOutputVersions,
			"output_schema_version": item.OutputVersion, "schema_hash": item.OutputSchemaHash, "output_schema": item.OutputSchema, "fields": fields}))
	}
	return cardapi.ListToolSchemaCatalog200JSONResponse{Items: result}, nil
}

func (handler CardHandler) CreateCardPresentation(ctx context.Context, request cardapi.CreateCardPresentationRequestObject) (cardapi.CreateCardPresentationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "conversation.read")
	if apiError != nil {
		return cardapi.CreateCardPresentationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return cardapi.CreateCardPresentationdefaultJSONResponse{Body: cardError(ctx, cardservice.ErrBindingInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	result, err := handler.Service.CreatePresentation(ctx, uuid.MustParse(p.ActorID()), p.EnterpriseIDValue(), uuid.UUID(request.CardInstanceId), p.AuthorizationVersion(), string(request.Body.Locale), string(request.Body.ColorScheme), request.Params.IdempotencyKey)
	if err != nil {
		return cardapi.CreateCardPresentationdefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	return cardapi.CreateCardPresentation201JSONResponse(toCardPresentation(result, string(request.Body.ColorScheme))), nil
}

func (handler CardHandler) InvokeCardQueryBinding(ctx context.Context, request cardapi.InvokeCardQueryBindingRequestObject) (cardapi.InvokeCardQueryBindingResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "conversation.read")
	if apiError != nil {
		return cardapi.InvokeCardQueryBindingdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	result, err := handler.Service.InvokeQueryBinding(ctx, uuid.MustParse(p.ActorID()), p.EnterpriseIDValue(), p.AuthorizationVersion(), request.BindingId, request.Params.IdempotencyKey)
	if err != nil {
		return cardapi.InvokeCardQueryBindingdefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	data := fromJSON[cardapi.PublicJsonValue](result.Value)
	return cardapi.InvokeCardQueryBinding200JSONResponse{Status: "succeeded", Data: &data}, nil
}

func (handler CardHandler) InvokeCardActionBinding(ctx context.Context, request cardapi.InvokeCardActionBindingRequestObject) (cardapi.InvokeCardActionBindingResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "pending_action.confirm")
	if apiError != nil {
		return cardapi.InvokeCardActionBindingdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	requestID := request.Params.IdempotencyKey
	if current, ok := RequestFromContext(ctx); ok {
		requestID = current.RequestID
	}
	invocation, err := handler.Workflow.InvokeCardBinding(ctx, p.ActorID(), requestID, p.EnterpriseIDValue(), p.AuthorizationVersion(), request.BindingId, request.Params.IdempotencyKey)
	if err != nil {
		return cardapi.InvokeCardActionBindingdefaultJSONResponse{Body: cardError(ctx, err), StatusCode: cardStatus(err)}, nil
	}
	if invocation.Action == "cancel" {
		return cardapi.InvokeCardActionBinding200JSONResponse{Status: "cancelled", PendingAction: pointer(convertPending[cardapi.PendingActionPublicSchema](invocation.PendingAction))}, nil
	}
	confirmation := invocation.Confirmation
	result := cardapi.CardBindingInvokeResult{Status: cardapi.CardBindingInvokeResultStatus(confirmation.PendingAction.Status), PendingAction: pointer(convertPending[cardapi.PendingActionPublicSchema](confirmation.PendingAction))}
	if confirmation.Execution != nil {
		result.Execution = pointer(fromJSON[cardapi.Execution](toActionExecution(*confirmation.Execution, confirmation.PendingAction)))
	}
	if confirmation.ApprovalRequest != nil {
		view, viewErr := handler.Workflow.GetApprovalView(ctx, p.EnterpriseIDValue(), confirmation.ApprovalRequest.ID)
		if viewErr != nil {
			return cardapi.InvokeCardActionBindingdefaultJSONResponse{Body: cardError(ctx, viewErr), StatusCode: cardStatus(viewErr)}, nil
		}
		result.ApprovalRequest = pointer(fromJSON[cardapi.ApprovalRequestView](toActionApprovalView(view)))
	}
	return cardapi.InvokeCardActionBinding200JSONResponse(result), nil
}

func (handler CardHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *cardapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &cardapi.ApiError{Code: value.Code, MessageKey: value.MessageKey, RequestId: value.RequestId, Retryable: value.Retryable}
}

func (handler CardHandler) toCardVersion(ctx context.Context, value db.CardVersion) (cardapi.CardVersion, error) {
	bindings, err := handler.Service.Store.Queries.ListCardSlotBindings(ctx, value.ID)
	if err != nil {
		return cardapi.CardVersion{}, err
	}
	demos, err := handler.Service.Store.Queries.ListCardDemoScenarios(ctx, value.ID)
	if err != nil {
		return cardapi.CardVersion{}, err
	}
	result := fromJSON[cardapi.CardVersion](map[string]any{"card_id": value.CardID, "revision": value.Revision, "status": value.Status,
		"manifest": json.RawMessage(value.Manifest), "entrypoint_html": string(value.EntrypointHtml), "content_hash": hex.EncodeToString(value.ContentHash),
		"manifest_hash": hex.EncodeToString(value.ManifestHash), "created_at": value.CreatedAt.Time, "slot_bindings": bindingsToMaps(bindings), "demos": demosToMaps(demos)})
	if value.CreatedBy.Valid {
		createdBy := value.CreatedBy.UUID
		result.CreatedBy = &createdBy
	}
	return result, nil
}

func (handler CardHandler) toInteractiveCard(ctx context.Context, value db.InteractiveCard) (cardapi.InteractiveCard, error) {
	result := cardapi.InteractiveCard{Id: value.ID, Source: cardapi.InteractiveCardSource(value.Source), Slug: value.Slug, Name: value.Name,
		Description: value.Description, Lifecycle: cardapi.CardLifecycle(value.Lifecycle), Enabled: value.Enabled, Availability: cardapi.CardAvailability(value.Availability),
		LatestRevision: int(value.LatestRevision), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.EnterpriseID.Valid {
		id := value.EnterpriseID.UUID
		result.EnterpriseId = &id
	}
	if value.CreatedBy.Valid {
		id := value.CreatedBy.UUID
		result.CreatedBy = &id
	}
	if value.ActiveVersionID.Valid {
		version, err := handler.Service.Store.Queries.GetCardVersionByID(ctx, value.ActiveVersionID.UUID)
		if err != nil || version.CardID != value.ID {
			return cardapi.InteractiveCard{}, cardservice.ErrPresentationInvalid
		}
		revision := int(version.Revision)
		result.ActiveRevision = &revision
	}
	return result, nil
}

func toCardVersionSummary(value db.CardVersion) cardapi.CardVersionSummary {
	result := cardapi.CardVersionSummary{CardId: value.CardID, Revision: int(value.Revision), Status: cardapi.CardVersionStatus(value.Status),
		ContentHash: hex.EncodeToString(value.ContentHash), ManifestHash: hex.EncodeToString(value.ManifestHash), CreatedAt: value.CreatedAt.Time}
	if value.CreatedBy.Valid {
		id := value.CreatedBy.UUID
		result.CreatedBy = &id
	}
	return result
}

func toValidationRun(value db.CardValidationRun, version db.CardVersion, nonce string) cardapi.CardValidationRun {
	var issues []cardapi.CardValidationIssue
	_ = json.Unmarshal(value.Issues, &issues)
	required := make([]cardapi.DemoScenario, len(value.RequiredScenarios))
	for index, scenario := range value.RequiredScenarios {
		required[index] = cardapi.DemoScenario(scenario)
	}
	passed := make([]cardapi.DemoScenario, len(value.PassedScenarios))
	for index, scenario := range value.PassedScenarios {
		passed[index] = cardapi.DemoScenario(scenario)
	}
	return cardapi.CardValidationRun{Id: value.ID, CardId: version.CardID, Revision: int(version.Revision), RuntimeVersion: value.RuntimeVersion,
		ContentHash: hex.EncodeToString(value.ContentHash), Nonce: nonce, Status: cardapi.CardValidationRunStatus(value.Status), RequiredScenarios: required,
		PassedScenarios: passed, Issues: issues, ExpiresAt: value.ExpiresAt.Time, CreatedAt: value.CreatedAt.Time}
}

func toCardPresentation(value cardservice.PresentationResult, colorScheme string) cardapi.CardPresentation {
	renderPlan := map[string]any{"schema_version": "argus.render_plan/v1", "card_id": value.Card.ID.String(), "card_revision": value.Version.Revision,
		"card_instance_id": value.Instance.ID.String(), "data_bindings": []any{}, "query_binding_ids": value.QueryBindings, "action_binding_ids": value.ActionBindings,
		"locale": value.Locale, "color_scheme": colorScheme}
	return fromJSON[cardapi.CardPresentation](map[string]any{"presentation_id": value.Presentation.ID, "card_instance": map[string]any{"id": value.Instance.ID,
		"card_id": value.Card.ID, "card_revision": value.Version.Revision, "conversation_id": value.Instance.ConversationID, "run_id": value.Instance.RunID.UUID,
		"status": value.Instance.Status, "created_at": value.Instance.CreatedAt.Time}, "manifest": json.RawMessage(value.Version.Manifest),
		"entrypoint_html": string(value.Version.EntrypointHtml), "render_plan": renderPlan, "initial_data": value.InitialData, "partial": value.Partial,
		"locale_fallback": value.LocaleFallback, "expires_at": value.Presentation.ExpiresAt.Time})
}

func bindingInput(value cardapi.SlotBinding) cardservice.Binding {
	semantic := ""
	if value.SemanticType != nil {
		semantic = *value.SemanticType
	}
	return cardservice.Binding{SlotName: value.SlotName, SlotKind: value.SlotKind.(string), Mode: value.Mode.(string), ToolID: value.ToolId,
		OutputSchemaVersion: value.OutputSchemaVersion, SchemaHash: value.SchemaHash, FieldPath: value.Path, ValueType: value.ValueType.(string), SemanticType: semantic}
}

func bindingsToMaps(values []db.CardSlotBinding) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item := map[string]any{"slot_name": value.SlotName, "slot_kind": value.SlotKind, "mode": value.Mode, "tool_id": value.ToolID,
			"output_schema_version": value.OutputSchemaVersion, "schema_hash": hex.EncodeToString(value.SchemaHash), "path": value.FieldPath, "value_type": value.ValueType}
		if value.SemanticType.Valid {
			item["semantic_type"] = value.SemanticType.String
		}
		result = append(result, item)
	}
	return result
}

func demosToMaps(values []db.CardDemoScenario) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"scenario": value.Scenario, "data": json.RawMessage(value.Data)})
	}
	return result
}

func fromJSON[T any](value any) T {
	encoded, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(encoded, &result)
	return result
}

func pointer[T any](value T) *T { return &value }

func cardError(ctx context.Context, err error) cardapi.ApiError {
	code, key := "INTERNAL_ERROR", "errors.internal"
	switch {
	case errors.Is(err, cardservice.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		code, key = "CARD_NOT_FOUND", "errors.card.not_found"
	case errors.Is(err, cardservice.ErrReadOnly):
		code, key = "CARD_SOURCE_READ_ONLY", "errors.card.source_read_only"
	case errors.Is(err, cardservice.ErrVersionConflict):
		code, key = "CARD_VERSION_CONFLICT", "errors.card.version_conflict"
	case errors.Is(err, cardservice.ErrStaticValidation):
		code, key = "CARD_STATIC_VALIDATION_FAILED", "errors.card.static_validation_failed"
	case errors.Is(err, cardservice.ErrRuntimeValidation):
		code, key = "CARD_RUNTIME_VALIDATION_FAILED", "errors.card.runtime_validation_failed"
	case errors.Is(err, cardservice.ErrBindingExpired):
		code, key = "CARD_BINDING_EXPIRED", "errors.card.binding_expired"
	case errors.Is(err, cardservice.ErrBindingConsumed):
		code, key = "CARD_BINDING_CONSUMED", "errors.card.binding_consumed"
	case errors.Is(err, actionservice.ErrBindingConsumed):
		code, key = "CARD_BINDING_CONSUMED", "errors.card.binding_consumed"
	case errors.Is(err, actionservice.ErrBindingExpired):
		code, key = "CARD_BINDING_EXPIRED", "errors.card.binding_expired"
	case errors.Is(err, actionservice.ErrInvalidated):
		code, key = "CARD_ACTION_INVALIDATED", "errors.card.action_invalidated"
	case errors.Is(err, cardservice.ErrPresentationInvalid):
		code, key = "CARD_PRESENTATION_INVALIDATED", "errors.card.presentation_invalidated"
	case errors.Is(err, cardservice.ErrBindingInvalid):
		code, key = "CARD_BINDING_INVALID", "errors.card.binding_invalid"
	case errors.Is(err, cardservice.ErrDependencyUnavailable):
		code, key = "CARD_DEPENDENCY_UNAVAILABLE", "errors.card.dependency_unavailable"
	case errors.Is(err, postgres.ErrIdempotencyConflict):
		code, key = "IDEMPOTENCY_CONFLICT", "errors.common.idempotency_conflict"
	case errors.Is(err, postgres.ErrIdempotencyExpired):
		code, key = "IDEMPOTENCY_RESULT_EXPIRED", "errors.common.idempotency_result_expired"
	}
	return cardapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx)}
}

func cardStatus(err error) int {
	switch {
	case errors.Is(err, cardservice.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	case errors.Is(err, cardservice.ErrReadOnly), errors.Is(err, cardservice.ErrPresentationInvalid):
		return http.StatusForbidden
	case errors.Is(err, cardservice.ErrBindingExpired):
		return http.StatusGone
	case errors.Is(err, cardservice.ErrVersionConflict), errors.Is(err, cardservice.ErrBindingConsumed),
		errors.Is(err, actionservice.ErrBindingConsumed), errors.Is(err, actionservice.ErrInvalidated):
		return http.StatusConflict
	case errors.Is(err, actionservice.ErrBindingExpired):
		return http.StatusGone
	case errors.Is(err, postgres.ErrIdempotencyConflict), errors.Is(err, postgres.ErrIdempotencyExpired):
		return http.StatusConflict
	case errors.Is(err, cardservice.ErrDependencyUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, cardservice.ErrStaticValidation), errors.Is(err, cardservice.ErrRuntimeValidation), errors.Is(err, cardservice.ErrBindingInvalid):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
