package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	actionapi "github.com/kakj-go/Argus/internal/gen/openapi/actionapi"
	connectionapi "github.com/kakj-go/Argus/internal/gen/openapi/connectionapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/resource"
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
	return identity.Principal{}, &connectionapi.ApiError{Code: value.Code, MessageKey: value.MessageKey, RequestId: value.RequestId, Retryable: value.Retryable}
}

type ResourceActionHandler struct {
	Identity EnterpriseIdentityHandler
	Service  resource.Service
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
	confirmation, err := handler.Service.Confirm(ctx, resourceSubject(p), p.EnterpriseIDValue(), request.ActionRef, request.Params.IdempotencyKey)
	if err != nil {
		return actionapi.ConfirmPendingActiondefaultJSONResponse{Body: actionError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	value := confirmation.PendingAction
	result := actionapi.ConfirmPendingActionResult{PendingAction: convertPending[actionapi.PendingActionPublicSchema](value)}
	if value.ResultResourceID.Valid && value.ResultResourceType.Valid && value.ResultResourceVersion.Valid {
		result.ResourceRef = &actionapi.ResourceRef{ResourceId: value.ResultResourceID.UUID.String(), ResourceType: actionapi.ResourceRefResourceType(value.ResultResourceType.String), Version: value.ResultResourceVersion.Int64}
	}
	if confirmation.Enrollment != nil {
		command := confirmation.Enrollment.InstallCommand
		result.Enrollment = &actionapi.EnrollmentResult{EnrollmentId: openapi_types.UUID(confirmation.Enrollment.EnrollmentID), ExpiresAt: confirmation.Enrollment.ExpiresAt, InstallCommand: &command}
	}
	return actionapi.ConfirmPendingAction200JSONResponse(result), nil
}

func (handler ResourceActionHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *actionapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &actionapi.ApiError{Code: value.Code, MessageKey: value.MessageKey, RequestId: value.RequestId, Retryable: value.Retryable}
}

func emptyActionPage() actionapi.CursorPage {
	return actionapi.CursorPage{NextCursor: nil, HasMore: false, Partial: actionapi.PartialMetadata{Partial: false, Reasons: []actionapi.PartialMetadataReasons{}}}
}

func actionError(ctx context.Context, err error) actionapi.ApiError {
	base := hostError(ctx, err)
	if errors.Is(err, resource.ErrActionUnavailable) {
		base.Code, base.MessageKey = "ACTION_STATE_CONFLICT", "errors.actions.state_conflict"
	}
	return actionapi.ApiError{Code: base.Code, MessageKey: base.MessageKey, RequestId: base.RequestId, Retryable: base.Retryable}
}

func connectionError(ctx context.Context, err error) connectionapi.ApiError {
	base := hostError(ctx, err)
	return connectionapi.ApiError{Code: base.Code, MessageKey: base.MessageKey, RequestId: base.RequestId, Retryable: base.Retryable}
}

func actionResourceRef(id uuid.UUID, resourceType string, version int64) *actionapi.ResourceRef {
	return &actionapi.ResourceRef{ResourceId: id.String(), ResourceType: actionapi.ResourceRefResourceType(resourceType), Version: version}
}

func connectionUUID(value uuid.UUID) openapi_types.UUID { return openapi_types.UUID(value) }
