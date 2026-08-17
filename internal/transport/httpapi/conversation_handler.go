package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/conversation"
	conversationapi "github.com/kakj-go/Argus/internal/gen/openapi/conversationapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type ConversationHandler struct {
	Identity EnterpriseIdentityHandler
	Service  conversation.Service
}

func (handler ConversationHandler) ListConversations(ctx context.Context, request conversationapi.ListConversationsRequestObject) (conversationapi.ListConversationsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "conversation.read")
	if apiError != nil {
		return conversationapi.ListConversationsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	limit := int32(100)
	if request.Params.Limit != nil {
		limit = int32(*request.Params.Limit)
	}
	items, err := handler.Service.List(ctx, p.EnterpriseIDValue(), uuid.MustParse(p.ActorID()), limit)
	if err != nil {
		return conversationapi.ListConversationsdefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	result := make([]conversationapi.Conversation, 0, len(items))
	for _, item := range items {
		result = append(result, toConversation(item))
	}
	return conversationapi.ListConversations200JSONResponse{Items: result, Page: emptyConversationPage()}, nil
}

func (handler ConversationHandler) CreateConversation(ctx context.Context, request conversationapi.CreateConversationRequestObject) (conversationapi.CreateConversationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "conversation.use")
	if apiError != nil {
		return conversationapi.CreateConversationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	title := ""
	if request.Body.Title != nil {
		title = *request.Body.Title
	}
	value, err := handler.Service.Create(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.MustParse(p.ActorID()), uuid.UUID(request.Body.SelectedModelId), title, request.Params.IdempotencyKey)
	if err != nil {
		return conversationapi.CreateConversationdefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	return conversationapi.CreateConversation201JSONResponse(toConversation(value)), nil
}

func (handler ConversationHandler) GetConversation(ctx context.Context, request conversationapi.GetConversationRequestObject) (conversationapi.GetConversationResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "conversation.read")
	if apiError != nil {
		return conversationapi.GetConversationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Get(ctx, p.EnterpriseIDValue(), uuid.MustParse(p.ActorID()), uuid.UUID(request.ConversationId))
	if err != nil {
		return conversationapi.GetConversationdefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	return conversationapi.GetConversation200JSONResponse(toConversation(value)), nil
}

func (handler ConversationHandler) UpdateConversation(ctx context.Context, request conversationapi.UpdateConversationRequestObject) (conversationapi.UpdateConversationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "conversation.use")
	if apiError != nil {
		return conversationapi.UpdateConversationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	var status *string
	if request.Body.Status != nil {
		value := string(*request.Body.Status)
		status = &value
	}
	var modelID *uuid.UUID
	if request.Body.SelectedModelId != nil {
		value := uuid.UUID(*request.Body.SelectedModelId)
		modelID = &value
	}
	value, err := handler.Service.Update(ctx, p.EnterpriseIDValue(), uuid.MustParse(p.ActorID()), uuid.UUID(request.ConversationId), request.Body.ExpectedVersion, request.Body.Title, status, modelID)
	if err != nil {
		return conversationapi.UpdateConversationdefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	return conversationapi.UpdateConversation200JSONResponse(toConversation(value)), nil
}

func (handler ConversationHandler) CreateConversationMessage(ctx context.Context, request conversationapi.CreateConversationMessageRequestObject) (conversationapi.CreateConversationMessageResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "conversation.use")
	if apiError != nil {
		return conversationapi.CreateConversationMessagedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	accepted, err := handler.Service.AddMessage(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.MustParse(p.ActorID()), uuid.UUID(request.ConversationId),
		p.AuthorizationVersion(), LocaleFromContext(ctx), request.Body.Content, request.Params.IdempotencyKey)
	if err != nil {
		return conversationapi.CreateConversationMessagedefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	return conversationapi.CreateConversationMessage202JSONResponse{Event: toConversationEvent(accepted.Event), Run: toConversationRun(accepted.Run)}, nil
}

func (handler ConversationHandler) ListConversationEvents(ctx context.Context, request conversationapi.ListConversationEventsRequestObject) (conversationapi.ListConversationEventsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "conversation.read")
	if apiError != nil {
		return conversationapi.ListConversationEventsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	after := int64(0)
	if request.Params.Cursor != nil {
		after = conversation.ParseSequence(*request.Params.Cursor)
	}
	limit := int32(200)
	if request.Params.Limit != nil {
		limit = int32(*request.Params.Limit)
	}
	items, err := handler.Service.ListEvents(ctx, p.EnterpriseIDValue(), uuid.MustParse(p.ActorID()), uuid.UUID(request.ConversationId), after, limit)
	if err != nil {
		return conversationapi.ListConversationEventsdefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	result := make([]conversationapi.ConversationEvent, 0, len(items))
	for _, item := range items {
		result = append(result, toConversationEvent(item))
	}
	return conversationapi.ListConversationEvents200JSONResponse{Items: result, Page: emptyConversationPage()}, nil
}

func (handler ConversationHandler) StreamConversationEvents(ctx context.Context, request conversationapi.StreamConversationEventsRequestObject) (conversationapi.StreamConversationEventsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "conversation.read")
	if apiError != nil {
		return conversationapi.StreamConversationEvents409JSONResponse(*apiError), nil
	}
	conversationID, err := uuid.Parse(request.ConversationId)
	if err != nil {
		return conversationapi.StreamConversationEvents409JSONResponse(conversationError(ctx, err)), nil
	}
	after := int64(0)
	if request.Params.LastEventID != nil {
		after = conversation.ParseSequence(*request.Params.LastEventID)
	}
	reader, writer := io.Pipe()
	go handler.writeEventStream(ctx, writer, p, conversationID, after)
	return conversationapi.StreamConversationEvents200TexteventStreamResponse{Body: reader}, nil
}

func (handler ConversationHandler) GetRun(ctx context.Context, request conversationapi.GetRunRequestObject) (conversationapi.GetRunResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "conversation.read")
	if apiError != nil {
		return conversationapi.GetRundefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetRun(ctx, p.EnterpriseIDValue(), uuid.UUID(request.RunId))
	if err != nil {
		return conversationapi.GetRundefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	return conversationapi.GetRun200JSONResponse(toConversationRun(value)), nil
}

func (handler ConversationHandler) CancelRun(ctx context.Context, request conversationapi.CancelRunRequestObject) (conversationapi.CancelRunResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "conversation.use")
	if apiError != nil {
		return conversationapi.CancelRundefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.CancelRun(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.RunId), request.Params.IdempotencyKey)
	if err != nil {
		return conversationapi.CancelRundefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	return conversationapi.CancelRun200JSONResponse{Run: toConversationRun(value)}, nil
}

func (handler ConversationHandler) CompactRun(ctx context.Context, request conversationapi.CompactRunRequestObject) (conversationapi.CompactRunResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "conversation.use")
	if apiError != nil {
		return conversationapi.CompactRundefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.RequestCompaction(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.RunId), request.Params.IdempotencyKey)
	if err != nil {
		return conversationapi.CompactRundefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	return conversationapi.CompactRun202JSONResponse{Run: toConversationRun(value)}, nil
}

func (handler ConversationHandler) GetToolResult(ctx context.Context, request conversationapi.GetToolResultRequestObject) (conversationapi.GetToolResultResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "conversation.read")
	if apiError != nil {
		return conversationapi.GetToolResultdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetToolResult(ctx, p.EnterpriseIDValue(), uuid.MustParse(p.ActorID()), request.ResultRef)
	if err != nil {
		return conversationapi.GetToolResultdefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: conversationStatus(err)}, nil
	}
	var projection conversationapi.ToolResultProjection
	if err := json.Unmarshal(value.Result.Projection, &projection); err != nil {
		return conversationapi.GetToolResultdefaultJSONResponse{Body: conversationError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	return conversationapi.GetToolResult200JSONResponse{ResultRef: value.Artifact.ResultRef, ToolCallId: value.Result.CallID,
		ToolId: value.Result.ToolID, ByteSize: int(value.Artifact.ByteSize), ContentHash: value.ContentHash, Partial: value.Result.Partial, Projection: projection}, nil
}

func (handler ConversationHandler) writeEventStream(ctx context.Context, writer *io.PipeWriter, principal identity.Principal, conversationID uuid.UUID, after int64) {
	defer writer.Close()
	poll := time.NewTicker(500 * time.Millisecond)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		items, err := handler.Service.ListEvents(ctx, principal.EnterpriseIDValue(), uuid.MustParse(principal.ActorID()), conversationID, after, 200)
		if err != nil {
			return
		}
		for _, item := range items {
			envelope, ok := toAgentStreamEnvelope(item, principal.AuthorizationVersion())
			if !ok {
				after = item.Sequence
				continue
			}
			payload, _ := json.Marshal(envelope)
			if _, err := fmt.Fprintf(writer, "id: %d\nevent: agent_event\ndata: %s\n\n", item.Sequence, payload); err != nil {
				return
			}
			after = item.Sequence
			if terminal, _ := envelope["terminal"].(bool); terminal {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
				return
			}
		case <-poll.C:
		}
	}
}

func toAgentStreamEnvelope(value db.ConversationEvent, authorizationVersion int64) (map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal(value.Payload, &payload); err != nil {
		return nil, false
	}
	eventType := ""
	agentPayload := map[string]any{}
	terminal := false
	closeReason := ""
	switch value.EventType {
	case "run_state_changed":
		if projected, _ := payload["agent_event_type"].(string); projected == "message_delta" {
			eventType = projected
			agentPayload["delta"] = payload["delta"]
		} else if status, _ := payload["status"].(string); status != "" {
			terminal = status == "succeeded" || status == "failed" || status == "cancelled"
			if status == "succeeded" || status == "cancelled" {
				eventType = "run_completed"
			} else if status == "failed" {
				eventType = "run_failed"
			} else {
				return nil, false
			}
			agentPayload["stop_reason"] = payload["stop_reason"]
			if code, ok := payload["error_code"]; ok {
				agentPayload["error_code"] = code
			}
			if terminal {
				closeReason = "normal"
			}
		}
	case "assistant_message":
		eventType = "message_completed"
		agentPayload["message"] = map[string]any{
			"message_id":      value.ID.String(),
			"conversation_id": value.ConversationID.String(),
			"role":            "assistant",
			"content":         payload["content"],
			"created_at":      value.OccurredAt.Time,
		}
	case "tool_call_result":
		eventType = "tool_call_completed"
		agentPayload = payload
		agentPayload["status"] = "succeeded"
	case "pending_action_created":
		eventType = "pending_action_created"
		agentPayload = payload
	case "context_compacted":
		eventType = "context_compaction_completed"
		agentPayload = payload
	default:
		return nil, false
	}
	if eventType == "" || !value.RunID.Valid {
		return nil, false
	}
	agentEvent := map[string]any{
		"schema_version": "argus.agent_event/v1",
		"event_id":       value.ID.String(),
		"sequence":       value.Sequence,
		"run_id":         value.RunID.UUID.String(),
		"event_type":     eventType,
		"occurred_at":    value.OccurredAt.Time,
		"payload":        agentPayload,
	}
	if value.StepID.Valid {
		agentEvent["step_id"] = value.StepID.UUID.String()
	}
	resumeCursor := fmt.Sprintf("%d", value.Sequence)
	envelope := map[string]any{
		"schema_version":        "argus.stream_event/v1",
		"event_id":              value.ID.String(),
		"sequence":              value.Sequence,
		"event_type":            "agent_event",
		"occurred_at":           value.OccurredAt.Time,
		"authorization_version": authorizationVersion,
		"resume_cursor":         resumeCursor,
		"terminal":              terminal,
		"data":                  agentEvent,
	}
	if closeReason != "" {
		envelope["close_reason"] = closeReason
	}
	return envelope, true
}

func (handler ConversationHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *conversationapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &conversationapi.ApiError{Code: value.Code, MessageKey: value.MessageKey, RequestId: value.RequestId, Retryable: value.Retryable}
}

func toConversation(value db.Conversation) conversationapi.Conversation {
	return conversationapi.Conversation{Id: value.ID, Title: value.Title, SelectedModelId: value.SelectedModelID,
		Status: conversationapi.ConversationStatus(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
}

func toConversationRun(value db.Run) conversationapi.Run {
	result := conversationapi.Run{RunId: value.ID.String(), ConversationId: value.ConversationID.String(), EnterpriseId: value.EnterpriseID.String(),
		ModelId: value.ModelID.String(), ModelRevision: int(value.ModelRevision), Locale: value.Locale, Status: value.Status,
		AuthorizationVersion: int(value.AuthorizationVersion), Version: int(value.Version), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.CurrentStepID.Valid {
		step := value.CurrentStepID.UUID.String()
		result.CurrentStepId = &step
	}
	if value.StopReason.Valid {
		result.StopReason = &value.StopReason.String
	}
	if value.ErrorCode.Valid {
		result.ErrorCode = &value.ErrorCode.String
	}
	return result
}

func toConversationEvent(value db.ConversationEvent) conversationapi.ConversationEvent {
	shape := map[string]any{"schema_version": "argus.conversation_event/v1", "event_id": value.ID.String(), "sequence": value.Sequence,
		"enterprise_id": value.EnterpriseID.String(), "conversation_id": value.ConversationID.String(), "event_type": value.EventType,
		"occurred_at": value.OccurredAt.Time, "content_hash": hex.EncodeToString(value.ContentHash), "payload": json.RawMessage(value.Payload),
		"data_classification": value.DataClassification, "actor_type": value.ActorType}
	if value.RunID.Valid {
		shape["run_id"] = value.RunID.UUID.String()
	}
	if value.StepID.Valid {
		shape["step_id"] = value.StepID.UUID.String()
	}
	if value.ActorID.Valid {
		shape["actor_id"] = value.ActorID.String
	}
	if value.ArtifactRef.Valid {
		shape["artifact_ref"] = value.ArtifactRef.String
	}
	encoded, _ := json.Marshal(shape)
	var result conversationapi.ConversationEvent
	_ = json.Unmarshal(encoded, &result)
	return result
}

func conversationError(ctx context.Context, err error) conversationapi.ApiError {
	code, key := "INTERNAL_ERROR", "errors.internal"
	switch {
	case errors.Is(err, conversation.ErrRunAlreadyActive):
		code, key = "RUN_ALREADY_ACTIVE", "errors.run.already_active"
	case errors.Is(err, conversation.ErrModelUnavailable):
		code, key = "MODEL_COMPATIBILITY_FAILED", "errors.model.unavailable"
	case errors.Is(err, conversation.ErrConversationClosed), errors.Is(err, conversation.ErrRunNotCancellable):
		code, key = "RUN_STATE_CONFLICT", "errors.run.state_conflict"
	case errors.Is(err, pgx.ErrNoRows):
		code, key = "NOT_FOUND", "errors.not_found"
	}
	requestID := "server-generated-request"
	if current, ok := RequestFromContext(ctx); ok {
		requestID = current.RequestID
	}
	return conversationapi.ApiError{Code: code, MessageKey: key, RequestId: requestID}
}

func conversationStatus(err error) int {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	case errors.Is(err, conversation.ErrRunAlreadyActive), errors.Is(err, conversation.ErrConversationClosed), errors.Is(err, conversation.ErrRunNotCancellable), errors.Is(err, conversation.ErrModelUnavailable):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func emptyConversationPage() conversationapi.CursorPage {
	return conversationapi.CursorPage{HasMore: false, Partial: conversationapi.PartialMetadata{Partial: false, Reasons: []conversationapi.PartialMetadataReasons{}}}
}
