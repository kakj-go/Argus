package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/automation"
	automationapi "github.com/kakj-go/Argus/internal/gen/openapi/automationapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type AutomationHandler struct {
	Identity EnterpriseIdentityHandler
	Service  automation.Service
}

func (handler AutomationHandler) ListAutomations(ctx context.Context, _ automationapi.ListAutomationsRequestObject) (automationapi.ListAutomationsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "automation.read")
	if apiError != nil {
		return automationapi.ListAutomationsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.List(ctx, p.EnterpriseIDValue())
	if err != nil {
		return automationapi.ListAutomationsdefaultJSONResponse{Body: automationError(ctx, err), StatusCode: automationStatus(err)}, nil
	}
	result := make([]automationapi.Automation, 0, len(items))
	for _, item := range items {
		result = append(result, toAutomation(item))
	}
	return automationapi.ListAutomations200JSONResponse(result), nil
}

func (handler AutomationHandler) CreateAutomation(ctx context.Context, request automationapi.CreateAutomationRequestObject) (automationapi.CreateAutomationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "automation.manage")
	if apiError != nil {
		return automationapi.CreateAutomationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Create(ctx, p.ActorID(), p.EnterpriseIDValue(), automationInput(*request.Body), request.Params.IdempotencyKey)
	if err != nil {
		return automationapi.CreateAutomationdefaultJSONResponse{Body: automationError(ctx, err), StatusCode: automationStatus(err)}, nil
	}
	return automationapi.CreateAutomation201JSONResponse(toAutomation(value)), nil
}

func (handler AutomationHandler) GetAutomation(ctx context.Context, request automationapi.GetAutomationRequestObject) (automationapi.GetAutomationResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "automation.read")
	if apiError != nil {
		return automationapi.GetAutomationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Get(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return automationapi.GetAutomationdefaultJSONResponse{Body: automationError(ctx, err), StatusCode: automationStatus(err)}, nil
	}
	return automationapi.GetAutomation200JSONResponse(toAutomation(value)), nil
}

func (handler AutomationHandler) UpdateAutomation(ctx context.Context, request automationapi.UpdateAutomationRequestObject) (automationapi.UpdateAutomationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "automation.manage")
	if apiError != nil {
		return automationapi.UpdateAutomationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Update(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id), automationInput(*request.Body))
	if err != nil {
		return automationapi.UpdateAutomationdefaultJSONResponse{Body: automationError(ctx, err), StatusCode: automationStatus(err)}, nil
	}
	return automationapi.UpdateAutomation200JSONResponse(toAutomation(value)), nil
}

func (handler AutomationHandler) ChangeAutomationState(ctx context.Context, request automationapi.ChangeAutomationStateRequestObject) (automationapi.ChangeAutomationStateResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "automation.manage")
	if apiError != nil {
		return automationapi.ChangeAutomationStatedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	status := "disabled"
	if request.StateAction == "enable" {
		status = "enabled"
	}
	value, err := handler.Service.ChangeState(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id), status, request.Params.ExpectedVersion)
	if err != nil {
		return automationapi.ChangeAutomationStatedefaultJSONResponse{Body: automationError(ctx, err), StatusCode: automationStatus(err)}, nil
	}
	return automationapi.ChangeAutomationState200JSONResponse(toAutomation(value)), nil
}

func (handler AutomationHandler) ListAutomationRuns(ctx context.Context, request automationapi.ListAutomationRunsRequestObject) (automationapi.ListAutomationRunsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "automation.read")
	if apiError != nil {
		return automationapi.ListAutomationRunsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListRuns(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id), 100)
	if err != nil {
		return automationapi.ListAutomationRunsdefaultJSONResponse{Body: automationError(ctx, err), StatusCode: automationStatus(err)}, nil
	}
	result := make([]automationapi.AutomationRun, 0, len(items))
	for _, item := range items {
		value := toAutomationRun(item)
		if item.PendingActionID.Valid {
			action, actionErr := handler.Service.Store.Queries.GetPendingActionByID(ctx, db.GetPendingActionByIDParams{ID: item.PendingActionID.UUID, EnterpriseID: p.EnterpriseIDValue()})
			if actionErr != nil {
				return automationapi.ListAutomationRunsdefaultJSONResponse{Body: automationError(ctx, actionErr), StatusCode: automationStatus(actionErr)}, nil
			}
			value.PendingActionRef = &action.ActionRef
		}
		result = append(result, value)
	}
	return automationapi.ListAutomationRuns200JSONResponse(result), nil
}

func (handler AutomationHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *automationapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &automationapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]automationapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func automationInput(value automationapi.AutomationWrite) automation.Input {
	var input map[string]any
	raw, _ := json.Marshal(value.ToolInput)
	_ = json.Unmarshal(raw, &input)
	version := int64(0)
	if value.ExpectedVersion != nil {
		version = *value.ExpectedVersion
	}
	return automation.Input{Name: value.Name, ServiceAccountID: uuid.UUID(value.ServiceAccountId), ToolID: value.ToolId,
		ToolInput: input, Cron: value.Cron, Timezone: value.Timezone, ExpectedVersion: version}
}

func toAutomation(value db.Automation) automationapi.Automation {
	var input automationapi.PublicJsonObject
	_ = json.Unmarshal(value.ToolInput, &input)
	return automationapi.Automation{Id: value.ID, Name: value.Name, ServiceAccountId: value.ServiceAccountID, ToolId: value.ToolID,
		ToolInput: input, Cron: value.Cron, Timezone: value.Timezone, Status: automationapi.AutomationStatus(value.Status),
		NextRunAt: value.NextRunAt.Time, Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
}

func toAutomationRun(value db.AutomationRun) automationapi.AutomationRun {
	result := automationapi.AutomationRun{Id: value.ID, AutomationId: value.AutomationID, AutomationRevision: int(value.AutomationRevision), ScheduledFor: value.ScheduledFor.Time,
		Status: automationapi.AutomationRunStatus(value.Status), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.ResultRef.Valid {
		result.ResultRef = &value.ResultRef.String
	}
	if value.ErrorCode.Valid {
		result.ErrorCode = &value.ErrorCode.String
	}
	return result
}

func automationError(ctx context.Context, err error) automationapi.ApiError {
	code := "CLIENT_OPERATION_UNAVAILABLE"
	key := "errors.client.operation_unavailable"
	defer func() { logMappedError(ctx, code, err) }()
	if errors.Is(err, automation.ErrVersionConflict) {
		code, key = "VERSION_CONFLICT", "errors.common.version_conflict"
	}
	return automationapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx)}
}

func automationStatus(err error) int {
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound
	}
	if errors.Is(err, automation.ErrVersionConflict) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}
