package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	actionservice "github.com/kakj-go/Argus/internal/action"
	actionapi "github.com/kakj-go/Argus/internal/gen/openapi/actionapi"
	connectionapi "github.com/kakj-go/Argus/internal/gen/openapi/connectionapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type ConnectionHandler struct {
	Identity EnterpriseIdentityHandler
	Service  resource.Service
}

func (handler ConnectionHandler) GetConnectionTest(ctx context.Context, request connectionapi.GetConnectionTestRequestObject) (connectionapi.GetConnectionTestResponseObject, error) {
	p, apiError := handler.auth(ctx)
	if apiError != nil {
		return connectionapi.GetConnectionTestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetConnectionTest(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return connectionapi.GetConnectionTestdefaultJSONResponse{Body: connectionError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	encoded, _ := jsonMarshal(toHostConnectionTest(value))
	var result connectionapi.ConnectionTest
	_ = jsonUnmarshal(encoded, &result)
	return connectionapi.GetConnectionTest200JSONResponse(result), nil
}

func (handler ConnectionHandler) auth(ctx context.Context) (identity.Principal, *connectionapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, false, "", "host.read")
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &connectionapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]connectionapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

type ResourceActionHandler struct {
	Identity EnterpriseIdentityHandler
	Service  resource.Service
	Workflow actionservice.Service
}

func (handler ResourceActionHandler) ListPendingActions(ctx context.Context, _ actionapi.ListPendingActionsRequestObject) (actionapi.ListPendingActionsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "pending_action.read")
	if apiError != nil {
		return actionapi.ListPendingActionsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.Actions.List(ctx, p.EnterpriseIDValue())
	if err != nil {
		return actionapi.ListPendingActionsdefaultJSONResponse{Body: actionError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	converted := make([]actionapi.PendingActionPublicSchema, 0, len(items))
	for _, item := range items {
		converted = append(converted, convertPending[actionapi.PendingActionPublicSchema](item))
	}
	return actionapi.ListPendingActions200JSONResponse{Items: converted, Page: emptyActionPage()}, nil
}

func (handler ResourceActionHandler) GetPendingAction(ctx context.Context, request actionapi.GetPendingActionRequestObject) (actionapi.GetPendingActionResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "pending_action.read")
	if apiError != nil {
		return actionapi.GetPendingActiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Actions.Get(ctx, p.EnterpriseIDValue(), request.ActionRef)
	if err != nil {
		return actionapi.GetPendingActiondefaultJSONResponse{Body: actionError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return actionapi.GetPendingAction200JSONResponse(convertPending[actionapi.PendingActionPublicSchema](value)), nil
}

func (handler ResourceActionHandler) CancelPendingAction(ctx context.Context, request actionapi.CancelPendingActionRequestObject) (actionapi.CancelPendingActionResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "pending_action.confirm")
	if apiError != nil {
		return actionapi.CancelPendingActiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Actions.Cancel(ctx, p.ActorID(), p.EnterpriseIDValue(), request.ActionRef, request.Params.IdempotencyKey)
	if err != nil {
		return actionapi.CancelPendingActiondefaultJSONResponse{Body: actionError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return actionapi.CancelPendingAction200JSONResponse(convertPending[actionapi.PendingActionPublicSchema](value)), nil
}

func (handler ResourceActionHandler) ConfirmPendingAction(ctx context.Context, request actionapi.ConfirmPendingActionRequestObject) (actionapi.ConfirmPendingActionResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "pending_action.confirm")
	if apiError != nil {
		return actionapi.ConfirmPendingActiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	requestID := request.Params.IdempotencyKey
	if current, ok := RequestFromContext(ctx); ok {
		requestID = current.RequestID
	}
	stepUp := handler.Identity.Auth.Identity.RequireStepUp(p) == nil
	confirmation, err := handler.Workflow.Confirm(ctx, p.ActorID(), requestID, p.EnterpriseIDValue(), p.AuthorizationVersion(), stepUp, request.ActionRef, request.Params.IdempotencyKey)
	if err != nil {
		return actionapi.ConfirmPendingActiondefaultJSONResponse{Body: actionError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	result := actionapi.PendingActionCommandResult{PendingAction: convertPending[actionapi.PendingActionPublicSchema](confirmation.PendingAction)}
	if confirmation.Execution != nil {
		value := toActionExecution(*confirmation.Execution, confirmation.PendingAction)
		result.Execution = &value
	}
	if confirmation.ApprovalRequest != nil {
		view, viewErr := handler.Workflow.GetApprovalView(ctx, p.EnterpriseIDValue(), confirmation.ApprovalRequest.ID)
		if viewErr != nil {
			return actionapi.ConfirmPendingActiondefaultJSONResponse{Body: actionError(ctx, viewErr), StatusCode: http.StatusInternalServerError}, nil
		}
		value := toActionApprovalView(view)
		result.ApprovalRequest = &value
	}
	return actionapi.ConfirmPendingAction200JSONResponse(result), nil
}

func toActionExecution(value db.Execution, pending db.PendingAction) actionapi.Execution {
	available := false
	result := actionapi.Execution{ExecutionId: value.ID.String(), ActionRef: pending.ActionRef, Status: value.Status,
		OneTimeResultAvailable: &available, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.ResultRef.Valid {
		result.ResultRef = &value.ResultRef.String
	}
	if value.ErrorCode.Valid {
		result.ErrorCode = &value.ErrorCode.String
	}
	return result
}

func toActionApprovalView(value actionservice.ApprovalView) actionapi.ApprovalRequestView {
	requirements := make([]actionapi.ApprovalRequirement, 0, len(value.Requirements))
	for _, item := range value.Requirements {
		requirements = append(requirements, actionapi.ApprovalRequirement{PolicyId: item.PolicyID, PolicyVersion: item.PolicyVersion,
			MinimumApprovers: int(item.MinimumApprovers), ApprovedCount: int(item.ApprovedCount), SeparationOfDuty: item.SeparationOfDuty,
			Status: actionapi.ApprovalRequirementStatus(item.Status)})
	}
	decisions := make([]actionapi.ApprovalDecision, 0, len(value.Decisions))
	for _, item := range value.Decisions {
		reason := item.Reason
		decisions = append(decisions, actionapi.ApprovalDecision{DecisionId: item.ID.String(), ActorUserId: item.ActorUserID.String(),
			Decision: item.Decision, Reason: &reason, DecidedAt: item.DecidedAt.Time})
	}
	return actionapi.ApprovalRequestView{ApprovalRequestId: value.Request.ID, ActionRef: value.Action.ActionRef,
		Status: actionapi.ApprovalRequestViewStatus(value.Request.Status), Requirements: requirements, Decisions: decisions,
		ExpiresAt: value.Request.ExpiresAt.Time, CreatedAt: value.Request.CreatedAt.Time, UpdatedAt: value.Request.UpdatedAt.Time}
}

func (handler ResourceActionHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *actionapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &actionapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]actionapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func emptyActionPage() actionapi.CursorPage {
	return actionapi.CursorPage{NextCursor: nil, HasMore: false, Partial: actionapi.PartialMetadata{Partial: false, Reasons: []actionapi.PartialMetadataReasons{}}}
}

func actionError(ctx context.Context, err error) actionapi.ApiError {
	base := hostErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	if errors.Is(err, resource.ErrActionUnavailable) {
		base.Code, base.MessageKey = "ACTION_STATE_CONFLICT", "errors.actions.state_conflict"
	} else if errors.Is(err, actionservice.ErrStepUpRequired) {
		base.Code, base.MessageKey = "STEP_UP_REQUIRED", "errors.identity.step_up_required"
	}
	return actionapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]actionapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func connectionError(ctx context.Context, err error) connectionapi.ApiError {
	base := hostErrorBase(ctx, err)
	logMappedError(ctx, base.Code, err)
	return connectionapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]connectionapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}
