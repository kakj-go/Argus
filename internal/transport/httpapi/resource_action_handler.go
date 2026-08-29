package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	actionservice "github.com/kakj-go/Argus/internal/action"
	actionapi "github.com/kakj-go/Argus/internal/gen/openapi/actionapi"
	connectionapi "github.com/kakj-go/Argus/internal/gen/openapi/connectionapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
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
	Cursor   pagination.Signer
}

func (handler ResourceActionHandler) ListPendingActions(ctx context.Context, request actionapi.ListPendingActionsRequestObject) (actionapi.ListPendingActionsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "pending_action.read")
	if apiError != nil {
		return actionapi.ListPendingActionsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	var items []db.PendingAction
	var err error
	if request.Params.Scope != nil {
		switch *request.Params.Scope {
		case actionapi.Created:
			items, err = handler.Service.Actions.ListCreated(ctx, p.EnterpriseIDValue(), p.ActorID())
		case actionapi.Mine:
			items, err = handler.Service.Actions.ListMine(ctx, p.EnterpriseIDValue(), p.ActorID())
		default:
			items, err = handler.Service.Actions.List(ctx, p.EnterpriseIDValue())
		}
	} else {
		items, err = handler.Service.Actions.List(ctx, p.EnterpriseIDValue())
	}
	if err == nil {
		if request.Params.Status != nil && len(*request.Params.Status) > 0 {
			allowed := make(map[string]struct{}, len(*request.Params.Status))
			for _, value := range *request.Params.Status {
				allowed[string(value)] = struct{}{}
			}
			filtered := items[:0]
			for _, item := range items {
				if _, ok := allowed[item.Status]; ok {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
		if request.Params.Risk != nil && len(*request.Params.Risk) > 0 {
			allowed := make(map[string]struct{}, len(*request.Params.Risk))
			for _, value := range *request.Params.Risk {
				allowed[string(value)] = struct{}{}
			}
			filtered := items[:0]
			for _, item := range items {
				if _, ok := allowed[item.Risk]; ok {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
		if request.Params.Query != nil && strings.TrimSpace(*request.Params.Query) != "" {
			needle := strings.ToLower(strings.TrimSpace(*request.Params.Query))
			filtered := items[:0]
			for _, item := range items {
				if strings.Contains(strings.ToLower(item.Title), needle) || strings.Contains(strings.ToLower(item.Summary), needle) {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
	}
	if err != nil {
		return actionapi.ListPendingActionsdefaultJSONResponse{Body: actionError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	items, err = handler.filterVisiblePendingActions(ctx, p, items)
	if err != nil {
		return actionapi.ListPendingActionsdefaultJSONResponse{Body: actionError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	binding := enterpriseCursorBinding(p, map[string]any{
		"scope":  request.Params.Scope,
		"status": request.Params.Status,
		"risk":   request.Params.Risk,
		"query":  request.Params.Query,
	}, "created_at_desc")
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), binding, func(value db.PendingAction) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		code, key, status := paginationError(err)
		return actionapi.ListPendingActionsdefaultJSONResponse{Body: actionapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, StatusCode: status}, nil
	}
	converted := make([]actionapi.PendingActionPublicSchema, 0, len(items))
	for _, item := range items {
		converted = append(converted, convertPending[actionapi.PendingActionPublicSchema](item))
	}
	return actionapi.ListPendingActions200JSONResponse{Items: converted, Page: actionapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyActionPage().Partial}}, nil
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
	visible, visibilityErr := handler.pendingActionVisible(ctx, p, value)
	if visibilityErr != nil {
		return actionapi.GetPendingActiondefaultJSONResponse{Body: actionError(ctx, visibilityErr), StatusCode: http.StatusInternalServerError}, nil
	}
	if !visible {
		return actionapi.GetPendingActiondefaultJSONResponse{Body: actionapi.ApiError{Code: "RESOURCE_NOT_FOUND", MessageKey: "errors.common.resource_not_found", RequestId: requestID(ctx)}, StatusCode: http.StatusNotFound}, nil
	}
	return actionapi.GetPendingAction200JSONResponse(convertPending[actionapi.PendingActionPublicSchema](value)), nil
}

func (handler ResourceActionHandler) filterVisiblePendingActions(ctx context.Context, principal identity.Principal, items []db.PendingAction) ([]db.PendingAction, error) {
	visible := make([]db.PendingAction, 0, len(items))
	for _, item := range items {
		ok, err := handler.pendingActionVisible(ctx, principal, item)
		if err != nil {
			return nil, err
		}
		if ok {
			visible = append(visible, item)
		}
	}
	return visible, nil
}

func (handler ResourceActionHandler) pendingActionVisible(ctx context.Context, principal identity.Principal, item db.PendingAction) (bool, error) {
	if slices.Contains(principal.Permissions, "*") ||
		(item.CreatorSubjectType == "user" && item.CreatorSubjectID.String() == principal.ActorID()) {
		return true, nil
	}
	if !item.ResourceID.Valid || item.ResourceType == "" {
		return false, nil
	}
	switch item.ResourceType {
	case "host":
		if _, err := handler.Service.Store.Queries.GetHost(ctx, db.GetHostParams{ID: item.ResourceID.UUID, EnterpriseID: principal.EnterpriseIDValue()}); err != nil {
			return false, nil
		}
	case "kubernetes_cluster":
		if _, err := handler.Service.Store.Queries.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: item.ResourceID.UUID, EnterpriseID: principal.EnterpriseIDValue()}); err != nil {
			return false, nil
		}
	default:
		return false, nil
	}
	return handler.Service.Access.CanAccess(principal.AuthorizedResourceIDs, item.ResourceID.UUID), nil
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
