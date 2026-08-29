package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	remoteaccessapi "github.com/kakj-go/Argus/internal/gen/openapi/remoteaccessapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/remoteaccess"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type RemoteAccessHandler struct {
	Identity     EnterpriseIdentityHandler
	Service      remoteaccess.Service
	Cursor       pagination.Signer
	WebsocketURL string
}

type remoteAccessRequestError struct {
	apiError remoteaccessapi.ApiError
}

func (err remoteAccessRequestError) Error() string {
	return err.apiError.Code
}

func (handler RemoteAccessHandler) ListRemoteAccessGrants(ctx context.Context, request remoteaccessapi.ListRemoteAccessGrantsRequestObject) (remoteaccessapi.ListRemoteAccessGrantsResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "remote_access.grant.read")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessGrantsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListGrants(ctx, actor.EnterpriseID)
	if err != nil {
		return remoteaccessapi.ListRemoteAccessGrantsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "created_at_desc"), func(value db.RemoteAccessGrant) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := remoteAccessPaginationError(ctx, err)
		return remoteaccessapi.ListRemoteAccessGrantsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	result := make([]remoteaccessapi.RemoteAccessGrant, 0, len(items))
	for _, item := range items {
		result = append(result, toRemoteAccessGrant(item))
	}
	return remoteaccessapi.ListRemoteAccessGrants200JSONResponse{Items: result, Page: remoteaccessapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyRemoteAccessPage().Partial}}, nil
}

func (handler RemoteAccessHandler) CreateRemoteAccessGrant(ctx context.Context, request remoteaccessapi.CreateRemoteAccessGrantRequestObject) (remoteaccessapi.CreateRemoteAccessGrantResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.grant.manage")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessGrantdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.CreateGrant(ctx, actor, grantInput(request.Body.SubjectType, request.Body.SubjectId, request.Body.HostIds,
		request.Body.ManagedAccountIds, request.Body.Protocols, request.Body.Actions, request.Body.ValidFrom, request.Body.ValidUntil, string(request.Body.Status), 0), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.CreateRemoteAccessGrant201JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) GetRemoteAccessGrant(ctx context.Context, request remoteaccessapi.GetRemoteAccessGrantRequestObject) (remoteaccessapi.GetRemoteAccessGrantResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.grant.read")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessGrantdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetGrant(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessGrant200JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) UpdateRemoteAccessGrant(ctx context.Context, request remoteaccessapi.UpdateRemoteAccessGrantRequestObject) (remoteaccessapi.UpdateRemoteAccessGrantResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.grant.manage")
	if apiError != nil {
		return remoteaccessapi.UpdateRemoteAccessGrantdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.UpdateRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.UpdateGrant(ctx, actor, uuid.UUID(request.Id), grantInput(request.Body.SubjectType, request.Body.SubjectId, request.Body.HostIds,
		request.Body.ManagedAccountIds, request.Body.Protocols, request.Body.Actions, request.Body.ValidFrom, request.Body.ValidUntil, "", request.Body.ExpectedVersion), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.UpdateRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.UpdateRemoteAccessGrant200JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) DisableRemoteAccessGrant(ctx context.Context, request remoteaccessapi.DisableRemoteAccessGrantRequestObject) (remoteaccessapi.DisableRemoteAccessGrantResponseObject, error) {
	value, err := handler.transitionGrant(ctx, request.Id, request.Params.XCSRFToken, int64(request.Params.ExpectedVersion), request.Params.IdempotencyKey, remoteaccess.GovernanceDisabled)
	if err != nil {
		return remoteaccessapi.DisableRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.DisableRemoteAccessGrant200JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) EnableRemoteAccessGrant(ctx context.Context, request remoteaccessapi.EnableRemoteAccessGrantRequestObject) (remoteaccessapi.EnableRemoteAccessGrantResponseObject, error) {
	value, err := handler.transitionGrant(ctx, request.Id, request.Params.XCSRFToken, int64(request.Params.ExpectedVersion), request.Params.IdempotencyKey, remoteaccess.GovernanceEnabled)
	if err != nil {
		return remoteaccessapi.EnableRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.EnableRemoteAccessGrant200JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) RestoreRemoteAccessGrant(ctx context.Context, request remoteaccessapi.RestoreRemoteAccessGrantRequestObject) (remoteaccessapi.RestoreRemoteAccessGrantResponseObject, error) {
	value, err := handler.transitionGrant(ctx, request.Id, request.Params.XCSRFToken, int64(request.Params.ExpectedVersion), request.Params.IdempotencyKey, remoteaccess.GovernanceDraft)
	if err != nil {
		return remoteaccessapi.RestoreRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.RestoreRemoteAccessGrant200JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) ArchiveRemoteAccessGrant(ctx context.Context, request remoteaccessapi.ArchiveRemoteAccessGrantRequestObject) (remoteaccessapi.ArchiveRemoteAccessGrantResponseObject, error) {
	value, err := handler.transitionGrant(ctx, request.Id, request.Params.XCSRFToken, int64(request.Params.ExpectedVersion), request.Params.IdempotencyKey, remoteaccess.GovernanceArchived)
	if err != nil {
		return remoteaccessapi.ArchiveRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.ArchiveRemoteAccessGrant200JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) transitionGrant(ctx context.Context, id openapi_types.UUID, csrf string, expected int64, key, to string) (db.RemoteAccessGrant, error) {
	actor, _, apiError := handler.actor(ctx, true, csrf, "remote_access.grant.manage")
	if apiError != nil {
		return db.RemoteAccessGrant{}, remoteAccessRequestError{apiError: *apiError}
	}
	current, err := handler.Service.GetGrant(ctx, actor.EnterpriseID, uuid.UUID(id))
	if err != nil {
		return db.RemoteAccessGrant{}, err
	}
	return handler.Service.TransitionGrant(ctx, actor, uuid.UUID(id), current.Status, to, expected, key)
}

func (handler RemoteAccessHandler) GetRemoteAccessGrantReferences(ctx context.Context, request remoteaccessapi.GetRemoteAccessGrantReferencesRequestObject) (remoteaccessapi.GetRemoteAccessGrantReferencesResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.governance.references.read")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessGrantReferencesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GrantReferences(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessGrantReferencesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessGrantReferences200JSONResponse{Rules: int(value.Rules), Requests: int(value.Requests), Leases: int(value.Leases), Sessions: int(value.Sessions)}, nil
}

func (handler RemoteAccessHandler) ListRemoteAccessRules(ctx context.Context, request remoteaccessapi.ListRemoteAccessRulesRequestObject) (remoteaccessapi.ListRemoteAccessRulesResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "remote_access.rule.read")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessRulesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListRules(ctx, actor.EnterpriseID)
	if err != nil {
		return remoteaccessapi.ListRemoteAccessRulesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "priority_id"), func(value db.RemoteAccessRule) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := remoteAccessPaginationError(ctx, err)
		return remoteaccessapi.ListRemoteAccessRulesdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	result := make([]remoteaccessapi.RemoteAccessRule, 0, len(items))
	for _, item := range items {
		result = append(result, toRemoteAccessRule(item))
	}
	return remoteaccessapi.ListRemoteAccessRules200JSONResponse{Items: result, Page: remoteaccessapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyRemoteAccessPage().Partial}}, nil
}
func (handler RemoteAccessHandler) CreateRemoteAccessRule(ctx context.Context, request remoteaccessapi.CreateRemoteAccessRuleRequestObject) (remoteaccessapi.CreateRemoteAccessRuleResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.rule.manage")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessRuledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	v, err := handler.Service.CreateRule(ctx, actor, ruleInput(*request.Body, 0), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.CreateRemoteAccessRule201JSONResponse(toRemoteAccessRule(v)), nil
}
func (handler RemoteAccessHandler) SimulateRemoteAccessRule(ctx context.Context, request remoteaccessapi.SimulateRemoteAccessRuleRequestObject) (remoteaccessapi.SimulateRemoteAccessRuleResponseObject, error) {
	actor, _, apiError := handler.actorAny(ctx, true, request.Params.XCSRFToken, "remote_access.rule.read", "remote_access.governance.references.read")
	if apiError != nil {
		return remoteaccessapi.SimulateRemoteAccessRuledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.SimulateRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	var sourceIP netip.Addr
	if request.Body.SourceIp != nil && strings.TrimSpace(*request.Body.SourceIp) != "" {
		parsed, err := netip.ParseAddr(strings.TrimSpace(*request.Body.SourceIp))
		if err != nil {
			return remoteaccessapi.SimulateRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
		}
		sourceIP = parsed.Unmap()
	}
	var evaluationTime time.Time
	if request.Body.EvaluationTime != nil {
		evaluationTime = request.Body.EvaluationTime.UTC()
	}
	decision, err := handler.Service.Simulate(ctx, actor, remoteaccess.SimulationInput{
		HostID: uuid.UUID(request.Body.HostId), ManagedAccountID: uuid.UUID(request.Body.ManagedAccountId),
		Protocol: string(request.Body.Protocol), Action: string(request.Body.Action), SourceIP: sourceIP,
		At: evaluationTime, StepUpAuthenticated: request.Body.StepUpAuthenticated,
	})
	if err != nil {
		return remoteaccessapi.SimulateRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.SimulateRemoteAccessRule200JSONResponse(toRemoteAccessRuleSimulation(decision)), nil
}
func (handler RemoteAccessHandler) GetRemoteAccessRule(ctx context.Context, request remoteaccessapi.GetRemoteAccessRuleRequestObject) (remoteaccessapi.GetRemoteAccessRuleResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.rule.read")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessRuledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	v, err := handler.Service.GetRule(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessRule200JSONResponse(toRemoteAccessRule(v)), nil
}
func (handler RemoteAccessHandler) UpdateRemoteAccessRule(ctx context.Context, request remoteaccessapi.UpdateRemoteAccessRuleRequestObject) (remoteaccessapi.UpdateRemoteAccessRuleResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.rule.manage")
	if apiError != nil {
		return remoteaccessapi.UpdateRemoteAccessRuledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.UpdateRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	v, err := handler.Service.UpdateRule(ctx, actor, uuid.UUID(request.Id), ruleInputUpdate(*request.Body), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.UpdateRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.UpdateRemoteAccessRule200JSONResponse(toRemoteAccessRule(v)), nil
}
func (handler RemoteAccessHandler) transitionRule(ctx context.Context, id openapi_types.UUID, params remoteaccessapi.DisableRemoteAccessRuleParams, to string) (remoteaccessapi.RemoteAccessRule, error) {
	actor, _, apiError := handler.actor(ctx, true, params.XCSRFToken, "remote_access.rule.manage")
	if apiError != nil {
		return remoteaccessapi.RemoteAccessRule{}, remoteAccessRequestError{apiError: *apiError}
	}
	current, err := handler.Service.GetRule(ctx, actor.EnterpriseID, uuid.UUID(id))
	if err != nil {
		return remoteaccessapi.RemoteAccessRule{}, err
	}
	value, err := handler.Service.TransitionRule(ctx, actor, uuid.UUID(id), current.Status, to, int64(params.ExpectedVersion), params.IdempotencyKey)
	return toRemoteAccessRule(value), err
}
func (handler RemoteAccessHandler) DisableRemoteAccessRule(ctx context.Context, request remoteaccessapi.DisableRemoteAccessRuleRequestObject) (remoteaccessapi.DisableRemoteAccessRuleResponseObject, error) {
	v, err := handler.transitionRule(ctx, request.Id, request.Params, remoteaccess.GovernanceDisabled)
	if err != nil {
		return remoteaccessapi.DisableRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.DisableRemoteAccessRule200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) EnableRemoteAccessRule(ctx context.Context, request remoteaccessapi.EnableRemoteAccessRuleRequestObject) (remoteaccessapi.EnableRemoteAccessRuleResponseObject, error) {
	p := remoteaccessapi.DisableRemoteAccessRuleParams{ExpectedVersion: request.Params.ExpectedVersion, XCSRFToken: request.Params.XCSRFToken, IdempotencyKey: request.Params.IdempotencyKey}
	v, err := handler.transitionRule(ctx, request.Id, p, remoteaccess.GovernanceEnabled)
	if err != nil {
		return remoteaccessapi.EnableRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.EnableRemoteAccessRule200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) ArchiveRemoteAccessRule(ctx context.Context, request remoteaccessapi.ArchiveRemoteAccessRuleRequestObject) (remoteaccessapi.ArchiveRemoteAccessRuleResponseObject, error) {
	p := remoteaccessapi.DisableRemoteAccessRuleParams{ExpectedVersion: request.Params.ExpectedVersion, XCSRFToken: request.Params.XCSRFToken, IdempotencyKey: request.Params.IdempotencyKey}
	v, err := handler.transitionRule(ctx, request.Id, p, remoteaccess.GovernanceArchived)
	if err != nil {
		return remoteaccessapi.ArchiveRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.ArchiveRemoteAccessRule200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) RestoreRemoteAccessRule(ctx context.Context, request remoteaccessapi.RestoreRemoteAccessRuleRequestObject) (remoteaccessapi.RestoreRemoteAccessRuleResponseObject, error) {
	p := remoteaccessapi.DisableRemoteAccessRuleParams{ExpectedVersion: request.Params.ExpectedVersion, XCSRFToken: request.Params.XCSRFToken, IdempotencyKey: request.Params.IdempotencyKey}
	v, err := handler.transitionRule(ctx, request.Id, p, remoteaccess.GovernanceDraft)
	if err != nil {
		return remoteaccessapi.RestoreRemoteAccessRuledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.RestoreRemoteAccessRule200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) GetRemoteAccessRuleReferences(ctx context.Context, request remoteaccessapi.GetRemoteAccessRuleReferencesRequestObject) (remoteaccessapi.GetRemoteAccessRuleReferencesResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.governance.references.read")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessRuleReferencesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	v, err := handler.Service.RuleReferences(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessRuleReferencesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessRuleReferences200JSONResponse{Rules: int(v.Rules), Requests: int(v.Requests), Leases: int(v.Leases), Sessions: int(v.Sessions)}, nil
}

func (handler RemoteAccessHandler) ListApprovalWorkflows(ctx context.Context, request remoteaccessapi.ListApprovalWorkflowsRequestObject) (remoteaccessapi.ListApprovalWorkflowsResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "remote_access.workflow.read")
	if apiError != nil {
		return remoteaccessapi.ListApprovalWorkflowsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListWorkflows(ctx, actor.EnterpriseID)
	if err != nil {
		return remoteaccessapi.ListApprovalWorkflowsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "created_at_desc"), func(value db.RemoteAccessApprovalWorkflow) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := remoteAccessPaginationError(ctx, err)
		return remoteaccessapi.ListApprovalWorkflowsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	result := make([]remoteaccessapi.ApprovalWorkflow, 0, len(items))
	for _, item := range items {
		result = append(result, toApprovalWorkflow(item))
	}
	return remoteaccessapi.ListApprovalWorkflows200JSONResponse{Items: result, Page: remoteaccessapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyRemoteAccessPage().Partial}}, nil
}
func (handler RemoteAccessHandler) CreateApprovalWorkflow(ctx context.Context, request remoteaccessapi.CreateApprovalWorkflowRequestObject) (remoteaccessapi.CreateApprovalWorkflowResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.workflow.manage")
	if apiError != nil {
		return remoteaccessapi.CreateApprovalWorkflowdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	v, err := handler.Service.CreateWorkflow(ctx, actor, workflowInput(*request.Body, 0), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.CreateApprovalWorkflow201JSONResponse(toApprovalWorkflow(v)), nil
}
func (handler RemoteAccessHandler) GetApprovalWorkflow(ctx context.Context, request remoteaccessapi.GetApprovalWorkflowRequestObject) (remoteaccessapi.GetApprovalWorkflowResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.workflow.read")
	if apiError != nil {
		return remoteaccessapi.GetApprovalWorkflowdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	v, err := handler.Service.GetWorkflow(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetApprovalWorkflow200JSONResponse(toApprovalWorkflow(v)), nil
}
func (handler RemoteAccessHandler) UpdateApprovalWorkflow(ctx context.Context, request remoteaccessapi.UpdateApprovalWorkflowRequestObject) (remoteaccessapi.UpdateApprovalWorkflowResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.workflow.manage")
	if apiError != nil {
		return remoteaccessapi.UpdateApprovalWorkflowdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.UpdateApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	v, err := handler.Service.UpdateWorkflow(ctx, actor, uuid.UUID(request.Id), workflowInputUpdate(*request.Body), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.UpdateApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.UpdateApprovalWorkflow200JSONResponse(toApprovalWorkflow(v)), nil
}
func (handler RemoteAccessHandler) transitionWorkflow(ctx context.Context, id openapi_types.UUID, csrf string, expectedVersion remoteaccessapi.ExpectedVersion, key, to string) (remoteaccessapi.ApprovalWorkflow, error) {
	actor, _, apiError := handler.actor(ctx, true, csrf, "remote_access.workflow.manage")
	if apiError != nil {
		return remoteaccessapi.ApprovalWorkflow{}, remoteAccessRequestError{apiError: *apiError}
	}
	current, err := handler.Service.GetWorkflow(ctx, actor.EnterpriseID, uuid.UUID(id))
	if err != nil {
		return remoteaccessapi.ApprovalWorkflow{}, err
	}
	value, err := handler.Service.TransitionWorkflow(ctx, actor, uuid.UUID(id), current.Status, to, int64(expectedVersion), key)
	return toApprovalWorkflow(value), err
}
func (handler RemoteAccessHandler) workflowTransitionResponse(ctx context.Context, id openapi_types.UUID, csrf string, expectedVersion remoteaccessapi.ExpectedVersion, key, to string) (remoteaccessapi.ApprovalWorkflow, error) {
	return handler.transitionWorkflow(ctx, id, csrf, expectedVersion, key, to)
}
func (handler RemoteAccessHandler) DisableApprovalWorkflow(ctx context.Context, request remoteaccessapi.DisableApprovalWorkflowRequestObject) (remoteaccessapi.DisableApprovalWorkflowResponseObject, error) {
	v, err := handler.transitionWorkflow(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceDisabled)
	if err != nil {
		return remoteaccessapi.DisableApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.DisableApprovalWorkflow200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) EnableApprovalWorkflow(ctx context.Context, request remoteaccessapi.EnableApprovalWorkflowRequestObject) (remoteaccessapi.EnableApprovalWorkflowResponseObject, error) {
	v, err := handler.workflowTransitionResponse(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceEnabled)
	if err != nil {
		return remoteaccessapi.EnableApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.EnableApprovalWorkflow200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) ArchiveApprovalWorkflow(ctx context.Context, request remoteaccessapi.ArchiveApprovalWorkflowRequestObject) (remoteaccessapi.ArchiveApprovalWorkflowResponseObject, error) {
	v, err := handler.workflowTransitionResponse(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceArchived)
	if err != nil {
		return remoteaccessapi.ArchiveApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.ArchiveApprovalWorkflow200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) RestoreApprovalWorkflow(ctx context.Context, request remoteaccessapi.RestoreApprovalWorkflowRequestObject) (remoteaccessapi.RestoreApprovalWorkflowResponseObject, error) {
	v, err := handler.workflowTransitionResponse(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceDraft)
	if err != nil {
		return remoteaccessapi.RestoreApprovalWorkflowdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.RestoreApprovalWorkflow200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) GetApprovalWorkflowReferences(ctx context.Context, request remoteaccessapi.GetApprovalWorkflowReferencesRequestObject) (remoteaccessapi.GetApprovalWorkflowReferencesResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.governance.references.read")
	if apiError != nil {
		return remoteaccessapi.GetApprovalWorkflowReferencesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	v, err := handler.Service.WorkflowReferences(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetApprovalWorkflowReferencesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetApprovalWorkflowReferences200JSONResponse{Rules: int(v.Rules), Requests: int(v.Requests), Leases: int(v.Leases), Sessions: int(v.Sessions)}, nil
}

func (handler RemoteAccessHandler) ListSessionProfiles(ctx context.Context, request remoteaccessapi.ListSessionProfilesRequestObject) (remoteaccessapi.ListSessionProfilesResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "remote_access.session_profile.read")
	if apiError != nil {
		return remoteaccessapi.ListSessionProfilesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListSessionProfiles(ctx, actor.EnterpriseID)
	if err != nil {
		return remoteaccessapi.ListSessionProfilesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "created_at_desc"), func(value db.RemoteAccessSessionProfile) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := remoteAccessPaginationError(ctx, err)
		return remoteaccessapi.ListSessionProfilesdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	result := make([]remoteaccessapi.SessionProfile, 0, len(items))
	for _, item := range items {
		result = append(result, toSessionProfile(item))
	}
	return remoteaccessapi.ListSessionProfiles200JSONResponse{Items: result, Page: remoteaccessapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyRemoteAccessPage().Partial}}, nil
}
func (handler RemoteAccessHandler) CreateSessionProfile(ctx context.Context, request remoteaccessapi.CreateSessionProfileRequestObject) (remoteaccessapi.CreateSessionProfileResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.session_profile.manage")
	if apiError != nil {
		return remoteaccessapi.CreateSessionProfiledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	v, err := handler.Service.CreateSessionProfile(ctx, actor, remoteProfileInput(*request.Body, 0), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.CreateSessionProfile201JSONResponse(toSessionProfile(v)), nil
}
func (handler RemoteAccessHandler) GetSessionProfile(ctx context.Context, request remoteaccessapi.GetSessionProfileRequestObject) (remoteaccessapi.GetSessionProfileResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.session_profile.read")
	if apiError != nil {
		return remoteaccessapi.GetSessionProfiledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	v, err := handler.Service.GetSessionProfile(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetSessionProfile200JSONResponse(toSessionProfile(v)), nil
}
func (handler RemoteAccessHandler) UpdateSessionProfile(ctx context.Context, request remoteaccessapi.UpdateSessionProfileRequestObject) (remoteaccessapi.UpdateSessionProfileResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.session_profile.manage")
	if apiError != nil {
		return remoteaccessapi.UpdateSessionProfiledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.UpdateSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	v, err := handler.Service.UpdateSessionProfile(ctx, actor, uuid.UUID(request.Id), remoteProfileInputUpdate(*request.Body), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.UpdateSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.UpdateSessionProfile200JSONResponse(toSessionProfile(v)), nil
}
func (handler RemoteAccessHandler) transitionProfile(ctx context.Context, id openapi_types.UUID, csrf string, expectedVersion remoteaccessapi.ExpectedVersion, key, to string) (remoteaccessapi.SessionProfile, error) {
	actor, _, apiError := handler.actor(ctx, true, csrf, "remote_access.session_profile.manage")
	if apiError != nil {
		return remoteaccessapi.SessionProfile{}, remoteAccessRequestError{apiError: *apiError}
	}
	current, err := handler.Service.GetSessionProfile(ctx, actor.EnterpriseID, uuid.UUID(id))
	if err != nil {
		return remoteaccessapi.SessionProfile{}, err
	}
	value, err := handler.Service.TransitionSessionProfile(ctx, actor, uuid.UUID(id), current.Status, to, int64(expectedVersion), key)
	return toSessionProfile(value), err
}
func (handler RemoteAccessHandler) DisableSessionProfile(ctx context.Context, request remoteaccessapi.DisableSessionProfileRequestObject) (remoteaccessapi.DisableSessionProfileResponseObject, error) {
	v, err := handler.transitionProfile(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceDisabled)
	if err != nil {
		return remoteaccessapi.DisableSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.DisableSessionProfile200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) EnableSessionProfile(ctx context.Context, request remoteaccessapi.EnableSessionProfileRequestObject) (remoteaccessapi.EnableSessionProfileResponseObject, error) {
	v, err := handler.transitionProfile(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceEnabled)
	if err != nil {
		return remoteaccessapi.EnableSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.EnableSessionProfile200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) ArchiveSessionProfile(ctx context.Context, request remoteaccessapi.ArchiveSessionProfileRequestObject) (remoteaccessapi.ArchiveSessionProfileResponseObject, error) {
	v, err := handler.transitionProfile(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceArchived)
	if err != nil {
		return remoteaccessapi.ArchiveSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.ArchiveSessionProfile200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) RestoreSessionProfile(ctx context.Context, request remoteaccessapi.RestoreSessionProfileRequestObject) (remoteaccessapi.RestoreSessionProfileResponseObject, error) {
	v, err := handler.transitionProfile(ctx, request.Id, request.Params.XCSRFToken, request.Params.ExpectedVersion, request.Params.IdempotencyKey, remoteaccess.GovernanceDraft)
	if err != nil {
		return remoteaccessapi.RestoreSessionProfiledefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.RestoreSessionProfile200JSONResponse(v), nil
}
func (handler RemoteAccessHandler) GetSessionProfileReferences(ctx context.Context, request remoteaccessapi.GetSessionProfileReferencesRequestObject) (remoteaccessapi.GetSessionProfileReferencesResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.governance.references.read")
	if apiError != nil {
		return remoteaccessapi.GetSessionProfileReferencesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	v, err := handler.Service.SessionProfileReferences(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetSessionProfileReferencesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetSessionProfileReferences200JSONResponse{Rules: int(v.Rules), Requests: int(v.Requests), Leases: int(v.Leases), Sessions: int(v.Sessions)}, nil
}

func (handler RemoteAccessHandler) ListRemoteAccessRequests(ctx context.Context, request remoteaccessapi.ListRemoteAccessRequestsRequestObject) (remoteaccessapi.ListRemoteAccessRequestsResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.request", "remote_access.session.approve")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessRequestsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	scope := "mine"
	if request.Params.Scope != nil {
		scope = string(*request.Params.Scope)
	}
	if scope != "mine" && !hasPermission(principal, "remote_access.session.approve") {
		body := remoteAccessError(ctx, remoteaccess.ErrApprovalNotEligible)
		body.Code, body.MessageKey = "AUTHORIZATION_DENIED", "errors.auth.authorization_denied"
		return remoteaccessapi.ListRemoteAccessRequestsdefaultJSONResponse{Body: body, StatusCode: http.StatusForbidden}, nil
	}
	filter := remoteaccess.RequestListFilter{Scope: scope, CreatedFrom: request.Params.CreatedFrom, CreatedTo: request.Params.CreatedTo}
	if request.Params.Status != nil {
		filter.Status = string(*request.Params.Status)
	}
	if request.Params.Protocol != nil {
		filter.Protocol = string(*request.Params.Protocol)
	}
	if request.Params.CreatedBy != nil {
		filter.CreatedBy = uuid.UUID(*request.Params.CreatedBy)
	}
	if request.Params.HostId != nil {
		filter.HostID = uuid.UUID(*request.Params.HostId)
	}
	items, err := handler.Service.ListRequests(ctx, actor, filter)
	if err != nil {
		return remoteaccessapi.ListRemoteAccessRequestsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit),
		enterpriseCursorBinding(principal, filter, "created_at_desc"), func(value remoteaccess.RequestView) pageKey {
			return pageKey{Time: value.Request.CreatedAt.Time, ID: value.Request.ID.String()}
		})
	if err != nil {
		body, status := remoteAccessPaginationError(ctx, err)
		return remoteaccessapi.ListRemoteAccessRequestsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	result := make([]remoteaccessapi.AccessRequest, 0, len(items))
	for _, item := range items {
		result = append(result, toAccessRequest(item))
	}
	return remoteaccessapi.ListRemoteAccessRequests200JSONResponse{Items: result, Page: remoteaccessapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyRemoteAccessPage().Partial}}, nil
}

func (handler RemoteAccessHandler) CreateRemoteAccessRequest(ctx context.Context, request remoteaccessapi.CreateRemoteAccessRequestRequestObject) (remoteaccessapi.CreateRemoteAccessRequestResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.request")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessRequestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateRemoteAccessRequestdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.CreateRequest(ctx, actor, remoteaccess.RequestInput{HostID: uuid.UUID(request.Body.HostId), ManagedAccountID: uuid.UUID(request.Body.ManagedAccountId),
		Protocol: string(request.Body.Protocol), Action: string(request.Body.Action), Reason: request.Body.Reason}, request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateRemoteAccessRequestdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.CreateRemoteAccessRequest201JSONResponse(toAccessRequest(value)), nil
}

func (handler RemoteAccessHandler) GetRemoteAccessRequest(ctx context.Context, request remoteaccessapi.GetRemoteAccessRequestRequestObject) (remoteaccessapi.GetRemoteAccessRequestResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.request", "remote_access.session.approve")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessRequestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetRequest(ctx, actor, uuid.UUID(request.Id), hasPermission(principal, "remote_access.session.terminate"))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessRequestdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessRequest200JSONResponse(toAccessRequest(value)), nil
}

func (handler RemoteAccessHandler) ResumeRemoteAccessRequest(ctx context.Context, request remoteaccessapi.ResumeRemoteAccessRequestRequestObject) (remoteaccessapi.ResumeRemoteAccessRequestResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.request")
	if apiError != nil {
		return remoteaccessapi.ResumeRemoteAccessRequestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.ResumeRequest(ctx, actor, uuid.UUID(request.Id), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.ResumeRemoteAccessRequestdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.ResumeRemoteAccessRequest200JSONResponse(toAccessRequest(value)), nil
}

func (handler RemoteAccessHandler) DecideRemoteAccessRequest(ctx context.Context, request remoteaccessapi.DecideRemoteAccessRequestRequestObject) (remoteaccessapi.DecideRemoteAccessRequestResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.session.approve")
	if apiError != nil {
		return remoteaccessapi.DecideRemoteAccessRequestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.DecideRemoteAccessRequestdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	comment := ""
	if request.Body.Comment != nil {
		comment = *request.Body.Comment
	}
	value, err := handler.Service.DecideRequest(ctx, actor, uuid.UUID(request.Id), remoteaccess.DecisionInput{RequirementID: uuid.UUID(request.Body.RequirementId), Decision: string(request.Body.Decision), Comment: comment}, request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.DecideRemoteAccessRequestdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.DecideRemoteAccessRequest200JSONResponse(toAccessRequest(value)), nil
}

func (handler RemoteAccessHandler) ListRemoteAccessLeases(ctx context.Context, request remoteaccessapi.ListRemoteAccessLeasesRequestObject) (remoteaccessapi.ListRemoteAccessLeasesResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.session.create", "remote_access.session.terminate")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessLeasesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListLeases(ctx, actor, hasPermission(principal, "remote_access.session.terminate"), requestLimit(request.Params.Limit))
	if err != nil {
		return remoteaccessapi.ListRemoteAccessLeasesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	result := make([]remoteaccessapi.AccessLease, 0, len(items))
	for _, item := range items {
		result = append(result, toAccessLease(item))
	}
	return remoteaccessapi.ListRemoteAccessLeases200JSONResponse{Items: result, Page: emptyRemoteAccessPage()}, nil
}

func (handler RemoteAccessHandler) RevokeRemoteAccessLease(ctx context.Context, request remoteaccessapi.RevokeRemoteAccessLeaseRequestObject) (remoteaccessapi.RevokeRemoteAccessLeaseResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.session.terminate")
	if apiError != nil {
		return remoteaccessapi.RevokeRemoteAccessLeasedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.RevokeLease(ctx, actor, uuid.UUID(request.Id), "operator_revoked", request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.RevokeRemoteAccessLeasedefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.RevokeRemoteAccessLease200JSONResponse(toAccessLease(value)), nil
}

func (handler RemoteAccessHandler) ListRemoteAccessSessions(ctx context.Context, request remoteaccessapi.ListRemoteAccessSessionsRequestObject) (remoteaccessapi.ListRemoteAccessSessionsResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.session.create", "remote_access.session.terminate")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessSessionsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	filter := remoteaccess.SessionListFilter{Scope: "all", CreatedFrom: request.Params.CreatedFrom, CreatedTo: request.Params.CreatedTo}
	if request.Params.Scope != nil {
		filter.Scope = string(*request.Params.Scope)
	}
	if request.Params.Status != nil {
		filter.Status = string(*request.Params.Status)
	}
	if request.Params.Protocol != nil {
		filter.Protocol = string(*request.Params.Protocol)
	}
	if request.Params.ConnectionMode != nil {
		filter.ConnectionMode = string(*request.Params.ConnectionMode)
	}
	if request.Params.UserId != nil {
		filter.UserID = uuid.UUID(*request.Params.UserId)
	}
	if request.Params.HostId != nil {
		filter.HostID = uuid.UUID(*request.Params.HostId)
	}
	if request.Params.ManagedAccountId != nil {
		filter.ManagedAccountID = uuid.UUID(*request.Params.ManagedAccountId)
	}
	items, err := handler.Service.ListSessions(ctx, actor, hasPermission(principal, "remote_access.session.terminate"), filter)
	if err != nil {
		return remoteaccessapi.ListRemoteAccessSessionsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit),
		enterpriseCursorBinding(principal, filter, "created_at_desc"), func(value remoteaccess.SessionView) pageKey {
			return pageKey{Time: value.Session.CreatedAt.Time, ID: value.Session.ID.String()}
		})
	if err != nil {
		body, status := remoteAccessPaginationError(ctx, err)
		return remoteaccessapi.ListRemoteAccessSessionsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	result := make([]remoteaccessapi.RemoteAccessSession, 0, len(items))
	for _, item := range items {
		result = append(result, toRemoteAccessSession(item))
	}
	return remoteaccessapi.ListRemoteAccessSessions200JSONResponse{Items: result, Page: remoteaccessapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyRemoteAccessPage().Partial}}, nil
}

func (handler RemoteAccessHandler) CreateRemoteAccessSession(ctx context.Context, request remoteaccessapi.CreateRemoteAccessSessionRequestObject) (remoteaccessapi.CreateRemoteAccessSessionResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.session.create")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessSessiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateRemoteAccessSessiondefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.CreateSession(ctx, actor, uuid.UUID(request.Body.LeaseId), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateRemoteAccessSessiondefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.CreateRemoteAccessSession201JSONResponse(toRemoteAccessSession(value)), nil
}

func (handler RemoteAccessHandler) GetRemoteAccessSession(ctx context.Context, request remoteaccessapi.GetRemoteAccessSessionRequestObject) (remoteaccessapi.GetRemoteAccessSessionResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.session.create", "remote_access.session.terminate")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessSessiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetSession(ctx, actor, uuid.UUID(request.Id), hasPermission(principal, "remote_access.session.terminate"))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessSessiondefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessSession200JSONResponse(toRemoteAccessSession(value)), nil
}

func (handler RemoteAccessHandler) TerminateRemoteAccessSession(ctx context.Context, request remoteaccessapi.TerminateRemoteAccessSessionRequestObject) (remoteaccessapi.TerminateRemoteAccessSessionResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, true, request.Params.XCSRFToken, "remote_access.session.create", "remote_access.session.terminate")
	if apiError != nil {
		return remoteaccessapi.TerminateRemoteAccessSessiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.TerminateRemoteAccessSessiondefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.TerminateSession(ctx, actor, uuid.UUID(request.Id), request.Body.Reason, hasPermission(principal, "remote_access.session.terminate"), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.TerminateRemoteAccessSessiondefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.TerminateRemoteAccessSession200JSONResponse(toRemoteAccessSession(value)), nil
}

func (handler RemoteAccessHandler) CreateRemoteAccessSessionTicket(ctx context.Context, request remoteaccessapi.CreateRemoteAccessSessionTicketRequestObject) (remoteaccessapi.CreateRemoteAccessSessionTicketResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.session.create")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessSessionTicketdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.IssueTicket(ctx, actor, uuid.UUID(request.Id), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateRemoteAccessSessionTicketdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	ticket := value.Ticket
	websocketBase := handler.WebsocketURL
	if metadata, ok := RequestFromContext(ctx); ok && metadata.Request != nil && isLoopbackHost(metadata.Request.Host) {
		scheme := "ws"
		if strings.EqualFold(metadata.Request.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "wss"
		}
		websocketBase = scheme + "://" + metadata.Request.Host
	}
	return remoteaccessapi.CreateRemoteAccessSessionTicket201JSONResponse{SessionId: openapi_types.UUID(value.SessionID), Ticket: &ticket,
		ExpiresAt: value.ExpiresAt, ProtocolVersion: remoteaccessapi.ArgusRemoteAccessv1, WebsocketUrl: strings.TrimRight(websocketBase, "/") + "/v1/sessions/" + value.SessionID.String()}, nil
}

func isLoopbackHost(raw string) bool {
	host, _, err := net.SplitHostPort(raw)
	if err != nil {
		host = raw
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (handler RemoteAccessHandler) GetRemoteAccessRecording(ctx context.Context, request remoteaccessapi.GetRemoteAccessRecordingRequestObject) (remoteaccessapi.GetRemoteAccessRecordingResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.session.create", "remote_access.recording.read")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessRecordingdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetRecording(ctx, actor, uuid.UUID(request.Id), hasPermission(principal, "remote_access.recording.read"))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessRecordingdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessRecording200JSONResponse(toRemoteAccessRecording(value)), nil
}

func (handler RemoteAccessHandler) ListRemoteAccessRecordings(ctx context.Context, request remoteaccessapi.ListRemoteAccessRecordingsRequestObject) (remoteaccessapi.ListRemoteAccessRecordingsResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "remote_access.recording.read")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessRecordingsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	filter := remoteaccess.RecordingListFilter{CreatedFrom: request.Params.CreatedFrom, CreatedTo: request.Params.CreatedTo}
	if request.Params.Status != nil {
		filter.Status = string(*request.Params.Status)
	}
	if request.Params.SessionId != nil {
		filter.SessionID = uuid.UUID(*request.Params.SessionId)
	}
	if request.Params.UserId != nil {
		filter.UserID = uuid.UUID(*request.Params.UserId)
	}
	if request.Params.HostId != nil {
		filter.HostID = uuid.UUID(*request.Params.HostId)
	}
	items, err := handler.Service.ListRecordings(ctx, actor, filter)
	if err != nil {
		return remoteaccessapi.ListRemoteAccessRecordingsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit),
		enterpriseCursorBinding(principal, filter, "created_at_desc"), func(value db.RemoteAccessRecording) pageKey {
			return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
		})
	if err != nil {
		body, status := remoteAccessPaginationError(ctx, err)
		return remoteaccessapi.ListRemoteAccessRecordingsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	result := make([]remoteaccessapi.RemoteAccessRecording, 0, len(items))
	for _, item := range items {
		result = append(result, toRemoteAccessRecording(item))
	}
	return remoteaccessapi.ListRemoteAccessRecordings200JSONResponse{Items: result, Page: remoteaccessapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyRemoteAccessPage().Partial}}, nil
}

func (handler RemoteAccessHandler) ListRemoteAccessRecordingEvents(ctx context.Context, request remoteaccessapi.ListRemoteAccessRecordingEventsRequestObject) (remoteaccessapi.ListRemoteAccessRecordingEventsResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.session.create", "remote_access.recording.read")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessRecordingEventsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	after := int64(0)
	if request.Params.Cursor != nil {
		parsed, err := strconv.ParseInt(*request.Params.Cursor, 10, 64)
		if err != nil || parsed < 0 {
			return remoteaccessapi.ListRemoteAccessRecordingEventsdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
		}
		after = parsed
	}
	page, err := handler.Service.ReadRecordingEvents(ctx, actor, uuid.UUID(request.Id), after, hasPermission(principal, "remote_access.recording.read"))
	if err != nil {
		return remoteaccessapi.ListRemoteAccessRecordingEventsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.ListRemoteAccessRecordingEvents200JSONResponse(toRecordingEventPage(page)), nil
}

// recordingEventPageEventsItem aliases the generated anonymous struct so the
// events slice can be pre-allocated; a nil slice would serialize as JSON null
// and violate the RecordingEventPage contract (events must be an array).
type recordingEventPageEventsItem = struct {
	Data interface{}                                  `json:"data"`
	Time float32                                      `json:"time"`
	Type remoteaccessapi.RecordingEventPageEventsType `json:"type"`
}

func toRecordingEventPage(page remoteaccess.RecordingEventPage) remoteaccessapi.RecordingEventPage {
	result := remoteaccessapi.RecordingEventPage{Recording: toRemoteAccessRecording(page.Recording), NextCursor: strconv.FormatInt(page.Next, 10), Complete: page.Complete, Events: make([]recordingEventPageEventsItem, 0, len(page.Events))}
	for _, event := range page.Events {
		result.Events = append(result.Events, recordingEventPageEventsItem{Data: event.Data, Time: float32(event.Time), Type: remoteaccessapi.RecordingEventPageEventsType(event.Type)})
	}
	return result
}

func (handler RemoteAccessHandler) actor(ctx context.Context, mutation bool, csrf, permission string) (remoteaccess.Actor, identity.Principal, *remoteaccessapi.ApiError) {
	return handler.actorAny(ctx, mutation, csrf, permission)
}

func (handler RemoteAccessHandler) actorAny(ctx context.Context, mutation bool, csrf string, permissions ...string) (remoteaccess.Actor, identity.Principal, *remoteaccessapi.ApiError) {
	var last *remoteaccessapi.ApiError
	for _, permission := range permissions {
		principal, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
		if value != nil {
			converted := remoteaccessapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
				Params: copyErrorParams[map[string]remoteaccessapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
				Retryable: value.Retryable, TraceId: value.TraceId}
			last = &converted
			continue
		}
		if remoteAccessAdminPermission(permission) && !principal.EnterpriseAdmin {
			denied := remoteAccessError(ctx, remoteaccess.ErrScopeDenied)
			denied.Code, denied.MessageKey = "AUTHORIZATION_DENIED", "errors.auth.authorization_denied"
			last = &denied
			continue
		}
		if principal.EnterpriseUser == nil || principal.Session.ID == uuid.Nil {
			denied := remoteAccessError(ctx, errors.New("human enterprise session required"))
			denied.Code, denied.MessageKey = "AUTHORIZATION_DENIED", "errors.auth.authorization_denied"
			return remoteaccess.Actor{}, identity.Principal{}, &denied
		}
		metadata, _ := RequestFromContext(ctx)
		sourceIP, _ := netip.ParseAddr(metadata.ClientIP)
		return remoteaccess.Actor{EnterpriseID: principal.EnterpriseUser.EnterpriseID, UserID: principal.EnterpriseUser.ID,
			DepartmentID: principal.EnterpriseUser.DepartmentID, HTTPSessionID: principal.Session.ID,
			AuthorizationVersion: principal.EnterpriseUser.AuthorizationVersion, AuthorizedResourceIDs: slices.Clone(principal.AuthorizedResourceIDs),
			StepUpAuthenticated: handler.Identity.Auth.Identity.RequireStepUp(principal) == nil, SourceIP: sourceIP}, principal, nil
	}
	if last == nil {
		value := remoteAccessError(ctx, identity.ErrSessionInvalid)
		last = &value
	}
	return remoteaccess.Actor{}, identity.Principal{}, last
}

func remoteAccessAdminPermission(permission string) bool {
	switch permission {
	case "remote_access.grant.read", "remote_access.grant.manage",
		"remote_access.rule.read", "remote_access.rule.manage",
		"remote_access.workflow.read", "remote_access.workflow.manage",
		"remote_access.session_profile.read", "remote_access.session_profile.manage",
		"remote_access.governance.references.read", "remote_access.session.terminate",
		"remote_access.recording.read":
		return true
	default:
		return false
	}
}

func grantInput(subjectType remoteaccessapi.RemoteAccessSubjectType, subjectID openapi_types.UUID, hostIDs []openapi_types.UUID,
	accountIDs []openapi_types.UUID, protocols []remoteaccessapi.RemoteAccessProtocol, actions []remoteaccessapi.RemoteAccessAction,
	validFrom, validUntil time.Time, status string, expected int64) remoteaccess.GrantInput {
	return remoteaccess.GrantInput{SubjectType: string(subjectType), SubjectID: uuid.UUID(subjectID), HostIDs: fromOpenAPIUUIDs(hostIDs),
		ManagedAccountIDs: fromOpenAPIUUIDs(accountIDs), Protocols: fromRemoteProtocols(protocols),
		Actions: fromRemoteActions(actions), ValidFrom: validFrom, ValidUntil: validUntil, Status: status, ExpectedVersion: expected}
}

func ruleInput(value remoteaccessapi.RemoteAccessRuleWrite, expected int64) remoteaccess.RuleInput {
	var workflow, profile uuid.UUID
	if value.ApprovalWorkflowId != nil {
		workflow = uuid.UUID(*value.ApprovalWorkflowId)
	}
	if value.SessionProfileId != nil {
		profile = uuid.UUID(*value.SessionProfileId)
	}
	return remoteaccess.RuleInput{Name: value.Name, Description: value.Description, Priority: int32(value.Priority), Protocols: fromRemoteProtocols(value.Protocols), Actions: fromRemoteActions(value.Actions), SourceCIDRs: value.SourceCidrs, Effects: fromRuleEffects(value.Effects), TimeWindows: marshalTimeWindows(value.TimeWindows), ApprovalWorkflowID: workflow, SessionProfileID: profile, Status: string(value.Status), ExpectedVersion: expected}
}
func ruleInputUpdate(value remoteaccessapi.RemoteAccessRuleUpdate) remoteaccess.RuleInput {
	return remoteaccess.RuleInput{Name: value.Name, Description: value.Description, Priority: int32(value.Priority), Protocols: fromRemoteProtocols(value.Protocols), Actions: fromRemoteActions(value.Actions), SourceCIDRs: value.SourceCidrs, Effects: fromRuleEffects(value.Effects), TimeWindows: marshalTimeWindows(value.TimeWindows), ApprovalWorkflowID: uuidFromPtr(value.ApprovalWorkflowId), SessionProfileID: uuidFromPtr(value.SessionProfileId), ExpectedVersion: value.ExpectedVersion}
}

func workflowInput(value remoteaccessapi.ApprovalWorkflowWrite, expected int64) remoteaccess.WorkflowInput {
	return remoteaccess.WorkflowInput{Name: value.Name, Description: value.Description, ApproverRoleIDs: fromOpenAPIUUIDs(value.ApproverRoleIds), MinimumApprovals: int32(value.MinimumApprovals), SeparationOfDuties: value.SeparationOfDuties, ApprovalTimeoutSeconds: int32(value.ApprovalTimeoutSeconds), EscalationAfterSeconds: int32(value.EscalationAfterSeconds), TimeoutEffect: string(value.TimeoutEffect), EscalationRoleIDs: fromOpenAPIUUIDs(value.EscalationRoleIds), Status: string(value.Status), ExpectedVersion: expected}
}
func workflowInputUpdate(value remoteaccessapi.ApprovalWorkflowUpdate) remoteaccess.WorkflowInput {
	return remoteaccess.WorkflowInput{Name: value.Name, Description: value.Description, ApproverRoleIDs: fromOpenAPIUUIDs(value.ApproverRoleIds), MinimumApprovals: int32(value.MinimumApprovals), SeparationOfDuties: value.SeparationOfDuties, ApprovalTimeoutSeconds: int32(value.ApprovalTimeoutSeconds), EscalationAfterSeconds: int32(value.EscalationAfterSeconds), TimeoutEffect: string(value.TimeoutEffect), EscalationRoleIDs: fromOpenAPIUUIDs(value.EscalationRoleIds), ExpectedVersion: value.ExpectedVersion}
}

func remoteProfileInput(value remoteaccessapi.SessionProfileWrite, expected int64) remoteaccess.SessionProfileInput {
	return remoteaccess.SessionProfileInput{Name: value.Name, Description: value.Description, MaxSessionSeconds: int32(value.MaxSessionSeconds), IdleTimeoutSeconds: int32(value.IdleTimeoutSeconds), RecordingMode: string(value.RecordingMode), CommandAuditMode: string(value.CommandAuditMode), ClipboardMode: string(value.ClipboardMode), FileUploadMode: string(value.FileUploadMode), FileDownloadMode: string(value.FileDownloadMode), PortForwardMode: string(value.PortForwardMode), SessionShareMode: string(value.SessionShareMode), RetentionDays: int32(value.RetentionDays), Status: string(value.Status), ExpectedVersion: expected}
}
func remoteProfileInputUpdate(value remoteaccessapi.SessionProfileUpdate) remoteaccess.SessionProfileInput {
	return remoteaccess.SessionProfileInput{Name: value.Name, Description: value.Description, MaxSessionSeconds: int32(value.MaxSessionSeconds), IdleTimeoutSeconds: int32(value.IdleTimeoutSeconds), RecordingMode: string(value.RecordingMode), CommandAuditMode: string(value.CommandAuditMode), ClipboardMode: string(value.ClipboardMode), FileUploadMode: string(value.FileUploadMode), FileDownloadMode: string(value.FileDownloadMode), PortForwardMode: string(value.PortForwardMode), SessionShareMode: string(value.SessionShareMode), RetentionDays: int32(value.RetentionDays), ExpectedVersion: value.ExpectedVersion}
}

func fromRuleEffects(values []remoteaccessapi.RemoteAccessRuleEffect) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = string(values[i])
	}
	return result
}
func marshalTimeWindows(values []remoteaccessapi.RemoteAccessTimeWindow) []byte {
	raw, _ := json.Marshal(values)
	return raw
}
func uuidFromPtr(value *openapi_types.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return uuid.UUID(*value)
}

func hasPermission(principal identity.Principal, permission string) bool {
	return slices.Contains(principal.Permissions, "*") || slices.Contains(principal.Permissions, permission)
}

func requestLimit(value *remoteaccessapi.Limit) int32 {
	if value == nil {
		return 50
	}
	return int32(*value)
}

func remoteAccessPaginationError(ctx context.Context, err error) (remoteaccessapi.ApiError, int) {
	code, key, status := paginationError(err)
	logMappedError(ctx, code, err)
	return remoteaccessapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, status
}

func emptyRemoteAccessPage() remoteaccessapi.CursorPage {
	return remoteaccessapi.CursorPage{NextCursor: nil, HasMore: false, Partial: remoteaccessapi.PartialMetadata{Partial: false, Reasons: []remoteaccessapi.PartialMetadataReasons{}}}
}

func remoteAccessError(ctx context.Context, err error) remoteaccessapi.ApiError {
	var requestError remoteAccessRequestError
	if errors.As(err, &requestError) {
		logMappedError(ctx, requestError.apiError.Code, err)
		return requestError.apiError
	}
	base := setupErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	switch {
	case errors.Is(err, remoteaccess.ErrGrantRequired):
		base.Code, base.MessageKey = "REMOTE_ACCESS_GRANT_REQUIRED", "errors.remote_access.grant_required"
	case errors.Is(err, remoteaccess.ErrScopeDenied):
		base.Code, base.MessageKey = "REMOTE_ACCESS_SCOPE_DENIED", "errors.remote_access.scope_denied"
	case errors.Is(err, remoteaccess.ErrApprovalRequired):
		base.Code, base.MessageKey = "REMOTE_ACCESS_APPROVAL_REQUIRED", "errors.remote_access.approval_required"
	case errors.Is(err, remoteaccess.ErrApprovalNotEligible):
		base.Code, base.MessageKey = "REMOTE_ACCESS_APPROVAL_NOT_ELIGIBLE", "errors.remote_access.approval_not_eligible"
	case errors.Is(err, remoteaccess.ErrLeaseExpired):
		base.Code, base.MessageKey = "REMOTE_ACCESS_LEASE_EXPIRED", "errors.remote_access.lease_expired"
	case errors.Is(err, remoteaccess.ErrCapacityExceeded):
		base.Code, base.MessageKey = "REMOTE_ACCESS_CAPACITY_EXCEEDED", "errors.remote_access.capacity_exceeded"
	case errors.Is(err, remoteaccess.ErrRecordingUnavailable):
		base.Code, base.MessageKey = "REMOTE_ACCESS_RECORDING_UNAVAILABLE", "errors.remote_access.recording_unavailable"
	case errors.Is(err, remoteaccess.ErrAuthorizationStale):
		base.Code, base.MessageKey = "AUTHORIZATION_VERSION_STALE", "errors.auth.authorization_version_stale"
	case errors.Is(err, remoteaccess.ErrVersionConflict):
		base.Code, base.MessageKey = "VERSION_CONFLICT", "errors.common.version_conflict"
	case errors.Is(err, remoteaccess.ErrInvalidTransition):
		base.Code, base.MessageKey = "REMOTE_ACCESS_INVALID_STATE_TRANSITION", "errors.remote_access.invalid_state_transition"
	case errors.Is(err, remoteaccess.ErrInvalidGovernance):
		base.Code, base.MessageKey = "REMOTE_ACCESS_INVALID_GOVERNANCE", "errors.remote_access.invalid_governance"
	case errors.Is(err, remoteaccess.ErrInvalidRequest):
		base.Code, base.MessageKey = "INVALID_ARGUMENT", "errors.common.invalid_argument"
	case errors.Is(err, pgx.ErrNoRows):
		base.Code, base.MessageKey = "RESOURCE_NOT_FOUND", "errors.common.resource_not_found"
	case errors.Is(err, postgres.ErrIdempotencyConflict):
		base.Code, base.MessageKey = "IDEMPOTENCY_CONFLICT", "errors.common.idempotency_conflict"
	case errors.Is(err, postgres.ErrIdempotencyExpired):
		base.Code, base.MessageKey = "IDEMPOTENCY_EXPIRED", "errors.common.idempotency_expired"
	}
	return remoteaccessapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]remoteaccessapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func remoteAccessStatus(err error) int {
	var requestError remoteAccessRequestError
	if errors.As(err, &requestError) {
		return remoteAccessAuthStatus(requestError.apiError)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	case errors.Is(err, remoteaccess.ErrInvalidRequest):
		return http.StatusBadRequest
	case errors.Is(err, remoteaccess.ErrInvalidTransition):
		return http.StatusConflict
	case errors.Is(err, remoteaccess.ErrInvalidGovernance):
		return http.StatusUnprocessableEntity
	case errors.Is(err, remoteaccess.ErrScopeDenied), errors.Is(err, remoteaccess.ErrGrantRequired), errors.Is(err, remoteaccess.ErrApprovalNotEligible):
		return http.StatusForbidden
	case errors.Is(err, remoteaccess.ErrRecordingUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusConflict
	}
}

func remoteAccessAuthStatus(apiError remoteaccessapi.ApiError) int {
	switch apiError.Code {
	case "AUTHORIZATION_VERSION_STALE":
		return http.StatusConflict
	case "SESSION_EXPIRED", "SESSION_REVOKED", "SESSION_INVALID":
		return http.StatusUnauthorized
	default:
		return http.StatusForbidden
	}
}

func toRemoteAccessGrant(value db.RemoteAccessGrant) remoteaccessapi.RemoteAccessGrant {
	result := remoteaccessapi.RemoteAccessGrant{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID), SubjectType: remoteaccessapi.RemoteAccessSubjectType(value.SubjectType),
		SubjectId: openapi_types.UUID(value.SubjectID), HostIds: toOpenAPIUUIDs(value.HostIds), ManagedAccountIds: toOpenAPIUUIDs(value.ManagedAccountIds),
		Protocols: toRemoteProtocols(value.Protocols), Actions: toRemoteActions(value.Actions), ValidFrom: value.ValidFrom.Time, ValidUntil: value.ValidUntil.Time,
		Status: remoteaccessapi.RemoteAccessGovernanceStatus(value.Status), Version: value.Version, CreatedBy: pointerOpenAPIUUID(value.CreatedBy), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	return result
}

func toRemoteAccessRule(value db.RemoteAccessRule) remoteaccessapi.RemoteAccessRule {
	result := remoteaccessapi.RemoteAccessRule{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID), Name: value.Name, Description: value.Description, Priority: int(value.Priority), Protocols: toRemoteProtocols(value.Protocols), Actions: toRemoteActions(value.Actions), SourceCidrs: slices.Clone(value.SourceCidrs), Effects: fromDBRuleEffects(value.Effects), Status: remoteaccessapi.RemoteAccessGovernanceStatus(value.Status), Version: value.Version, CreatedBy: openapi_types.UUID(value.CreatedBy), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.ApprovalWorkflowID.Valid {
		id := openapi_types.UUID(value.ApprovalWorkflowID.UUID)
		result.ApprovalWorkflowId = &id
	}
	if value.SessionProfileID.Valid {
		id := openapi_types.UUID(value.SessionProfileID.UUID)
		result.SessionProfileId = &id
	}
	if len(value.TimeWindows) > 0 {
		_ = json.Unmarshal(value.TimeWindows, &result.TimeWindows)
	}
	return result
}
func toApprovalWorkflow(value db.RemoteAccessApprovalWorkflow) remoteaccessapi.ApprovalWorkflow {
	return remoteaccessapi.ApprovalWorkflow{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID), Name: value.Name, Description: value.Description, ApproverRoleIds: toOpenAPIUUIDs(value.ApproverRoleIds), MinimumApprovals: int(value.MinimumApprovals), SeparationOfDuties: value.SeparationOfDuties, ApprovalTimeoutSeconds: int(value.ApprovalTimeoutSeconds), EscalationAfterSeconds: int(value.EscalationAfterSeconds), TimeoutEffect: remoteaccessapi.ApprovalWorkflowTimeoutEffect(value.TimeoutEffect), EscalationRoleIds: toOpenAPIUUIDs(value.EscalationRoleIds), Status: remoteaccessapi.RemoteAccessGovernanceStatus(value.Status), Version: value.Version, CreatedBy: openapi_types.UUID(value.CreatedBy), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
}
func toSessionProfile(value db.RemoteAccessSessionProfile) remoteaccessapi.SessionProfile {
	return remoteaccessapi.SessionProfile{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID), Name: value.Name, Description: value.Description, MaxSessionSeconds: int(value.MaxSessionSeconds), IdleTimeoutSeconds: int(value.IdleTimeoutSeconds), RecordingMode: remoteaccessapi.SessionProfileRecordingMode(value.RecordingMode), CommandAuditMode: remoteaccessapi.SessionProfileCommandAuditMode(value.CommandAuditMode), ClipboardMode: remoteaccessapi.SessionProfileClipboardMode(value.ClipboardMode), FileUploadMode: remoteaccessapi.SessionProfileFileUploadMode(value.FileUploadMode), FileDownloadMode: remoteaccessapi.SessionProfileFileDownloadMode(value.FileDownloadMode), PortForwardMode: remoteaccessapi.SessionProfilePortForwardMode(value.PortForwardMode), SessionShareMode: remoteaccessapi.SessionProfileSessionShareMode(value.SessionShareMode), RetentionDays: int(value.RetentionDays), Status: remoteaccessapi.RemoteAccessGovernanceStatus(value.Status), Version: value.Version, CreatedBy: openapi_types.UUID(value.CreatedBy), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
}
func fromDBRuleEffects(values []string) []remoteaccessapi.RemoteAccessRuleEffect {
	result := make([]remoteaccessapi.RemoteAccessRuleEffect, len(values))
	for i := range values {
		result[i] = remoteaccessapi.RemoteAccessRuleEffect(values[i])
	}
	return result
}

func toAccessRequest(value remoteaccess.RequestView) remoteaccessapi.AccessRequest {
	request := value.Request
	result := remoteaccessapi.AccessRequest{Id: openapi_types.UUID(request.ID), EnterpriseId: pointerOpenAPIUUID(request.EnterpriseID), RequesterId: pointerOpenAPIUUID(request.RequesterID),
		GrantId: openapi_types.UUID(request.GrantID), HostId: openapi_types.UUID(request.HostID), ManagedAccountId: openapi_types.UUID(request.ManagedAccountID),
		Protocol: remoteaccessapi.RemoteAccessProtocol(request.Protocol), Action: remoteaccessapi.RemoteAccessAction(request.Action), Reason: request.Reason,
		Status: remoteaccessapi.RemoteAccessRequestStatus(request.Status), AuthorizationVersion: request.AuthorizationVersion, ExpiresAt: request.ExpiresAt.Time,
		CreatedAt: request.CreatedAt.Time, UpdatedAt: request.UpdatedAt.Time, Requirements: []remoteaccessapi.RemoteAccessRequirement{}, Decisions: []remoteaccessapi.RemoteAccessDecision{}}
	if request.DecisionOutcome.Valid {
		outcome := remoteaccessapi.AccessRequestDecisionOutcome(request.DecisionOutcome.String)
		result.DecisionOutcome = &outcome
	}
	if request.DecisionAt.Valid {
		result.DecisionAt = &request.DecisionAt.Time
	}
	if len(request.DecisionSnapshotHash) == 32 {
		hash := hex.EncodeToString(request.DecisionSnapshotHash)
		result.DecisionSnapshotHash = &hash
	}
	result.DecisionReasonCodes = decodeStringArray(request.DecisionReasonCodes)
	result.DecisionSnapshot = decodeObject(request.DecisionSnapshot)
	result.MatchedGrantSnapshots = decodeObjectArray(request.MatchedGrantSnapshots)
	result.MatchedRuleSnapshots = decodeObjectArray(request.MatchedRuleSnapshots)
	for _, item := range value.Requirements {
		requirement := item.Requirement
		mapped := remoteaccessapi.RemoteAccessRequirement{Id: openapi_types.UUID(requirement.ID),
			MinimumApprovals: int(requirement.MinimumApprovals), SeparationOfDuties: requirement.SeparationOfDuties,
			RequireMfa: requirement.RequireMfa, Status: remoteaccessapi.RemoteAccessRequirementStatus(requirement.Status), ApprovedCount: int(item.ApprovedCount)}
		if requirement.RuleID.Valid {
			id := openapi_types.UUID(requirement.RuleID.UUID)
			mapped.RuleId = &id
		}
		if requirement.RuleVersion.Valid {
			value := requirement.RuleVersion.Int64
			mapped.RuleVersion = &value
		}
		if requirement.WorkflowID.Valid {
			id := openapi_types.UUID(requirement.WorkflowID.UUID)
			mapped.WorkflowId = &id
		}
		if requirement.WorkflowVersion.Valid {
			value := requirement.WorkflowVersion.Int64
			mapped.WorkflowVersion = &value
		}
		if requirement.SessionProfileID.Valid {
			id := openapi_types.UUID(requirement.SessionProfileID.UUID)
			mapped.SessionProfileId = &id
		}
		if requirement.SessionProfileVersion.Valid {
			value := requirement.SessionProfileVersion.Int64
			mapped.SessionProfileVersion = &value
		}
		mapped.ApprovalSnapshot = decodeObject(requirement.ApprovalSnapshot)
		if requirement.DeadlineAt.Valid {
			mapped.DeadlineAt = &requirement.DeadlineAt.Time
		}
		if requirement.EscalationAt.Valid {
			mapped.EscalationAt = &requirement.EscalationAt.Time
		}
		if requirement.EscalatedAt.Valid {
			mapped.EscalatedAt = &requirement.EscalatedAt.Time
		}
		timeoutEffect := remoteaccessapi.RemoteAccessRequirementTimeoutEffect(requirement.TimeoutEffect)
		mapped.TimeoutEffect = &timeoutEffect
		escalationRoles := toOpenAPIUUIDs(requirement.EscalationRoleIds)
		mapped.EscalationRoleIds = &escalationRoles
		result.Requirements = append(result.Requirements, mapped)
	}
	for _, decision := range value.Decisions {
		comment := decision.Comment
		result.Decisions = append(result.Decisions, remoteaccessapi.RemoteAccessDecision{Id: openapi_types.UUID(decision.ID), RequestId: openapi_types.UUID(decision.RequestID),
			RequirementId: openapi_types.UUID(decision.RequirementID), Decision: remoteaccessapi.RemoteAccessDecisionDecision(decision.Decision), Comment: &comment,
			DecidedBy: openapi_types.UUID(decision.DecidedBy), DecidedAt: decision.DecidedAt.Time})
	}
	return result
}

func toAccessLease(value db.RemoteAccessLease) remoteaccessapi.AccessLease {
	result := remoteaccessapi.AccessLease{Id: openapi_types.UUID(value.ID), RequestId: openapi_types.UUID(value.RequestID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID),
		UserId: pointerOpenAPIUUID(value.UserID), GrantId: openapi_types.UUID(value.GrantID), HostId: openapi_types.UUID(value.HostID),
		ManagedAccountId: openapi_types.UUID(value.ManagedAccountID), Protocol: remoteaccessapi.RemoteAccessProtocol(value.Protocol), Action: remoteaccessapi.RemoteAccessAction(value.Action),
		AuthorizationVersion: value.AuthorizationVersion, IssuedAt: value.IssuedAt.Time, ExpiresAt: value.ExpiresAt.Time, Revoked: value.RevokedAt.Valid}
	if value.RevokedAt.Valid {
		result.RevokedAt = &value.RevokedAt.Time
	}
	if len(value.DecisionSnapshotHash) == 32 {
		hash := hex.EncodeToString(value.DecisionSnapshotHash)
		result.DecisionSnapshotHash = &hash
	}
	return result
}

func toRemoteAccessSession(value remoteaccess.SessionView) remoteaccessapi.RemoteAccessSession {
	session := value.Session
	result := remoteaccessapi.RemoteAccessSession{Id: openapi_types.UUID(session.ID), EnterpriseId: pointerOpenAPIUUID(session.EnterpriseID), UserId: pointerOpenAPIUUID(session.UserID),
		LeaseId: openapi_types.UUID(session.LeaseID), HostId: openapi_types.UUID(session.HostID), ManagedAccountId: openapi_types.UUID(session.ManagedAccountID),
		Protocol: remoteaccessapi.RemoteAccessProtocol(session.Protocol), ConnectionMode: remoteaccessapi.RemoteAccessSessionConnectionMode(session.ConnectionMode),
		Status: remoteaccessapi.RemoteAccessSessionStatus(session.Status), IdleTimeoutSeconds: int(session.IdleTimeoutSeconds),
		MaxDurationSeconds: int(session.MaxDurationSeconds), ConnectBefore: session.ConnectBefore.Time, CreatedAt: session.CreatedAt.Time, UpdatedAt: session.UpdatedAt.Time}
	if value.RecordingID != uuid.Nil {
		recordingID := openapi_types.UUID(value.RecordingID)
		result.RecordingId = &recordingID
	}
	fence := session.SessionFence
	result.SessionFence = &fence
	authorizationVersion := session.AuthorizationVersion
	result.AuthorizationVersion = &authorizationVersion
	reason := session.Reason
	result.Reason = &reason
	if session.ConnectorID.Valid {
		id := openapi_types.UUID(session.ConnectorID.UUID)
		result.ConnectorId = &id
	}
	if session.ConnectorEpoch.Valid {
		epoch := session.ConnectorEpoch.Int64
		result.ConnectorEpoch = &epoch
	}
	if session.GatewayInstance.Valid {
		result.GatewayInstance = &session.GatewayInstance.String
	}
	if session.ConnectedAt.Valid {
		result.ConnectedAt = &session.ConnectedAt.Time
	}
	if session.TerminatedAt.Valid {
		result.TerminatedAt = &session.TerminatedAt.Time
	}
	if session.TerminationReason.Valid {
		result.TerminationReason = &session.TerminationReason.String
	}
	if len(session.DecisionSnapshotHash) == 32 {
		hash := hex.EncodeToString(session.DecisionSnapshotHash)
		result.DecisionSnapshotHash = &hash
	}
	result.DecisionSnapshot = decodeObject(session.DecisionSnapshot)
	result.SessionProfileSnapshot = decodeObject(session.SessionProfileSnapshot)
	recordingMode := remoteaccessapi.RemoteAccessSessionRecordingMode(session.RecordingMode)
	commandAuditMode := remoteaccessapi.RemoteAccessSessionCommandAuditMode(session.CommandAuditMode)
	clipboardMode := remoteaccessapi.RemoteAccessSessionClipboardMode(session.ClipboardMode)
	fileUploadMode := remoteaccessapi.RemoteAccessSessionFileUploadMode(session.FileUploadMode)
	fileDownloadMode := remoteaccessapi.RemoteAccessSessionFileDownloadMode(session.FileDownloadMode)
	portForwardMode := remoteaccessapi.RemoteAccessSessionPortForwardMode(session.PortForwardMode)
	sessionShareMode := remoteaccessapi.RemoteAccessSessionSessionShareMode(session.SessionShareMode)
	retentionDays := int(session.RetentionDays)
	result.RecordingMode, result.CommandAuditMode, result.ClipboardMode = &recordingMode, &commandAuditMode, &clipboardMode
	result.FileUploadMode, result.FileDownloadMode, result.PortForwardMode = &fileUploadMode, &fileDownloadMode, &portForwardMode
	result.SessionShareMode, result.RetentionDays = &sessionShareMode, &retentionDays
	return result
}

func toRemoteAccessRuleSimulation(value remoteaccess.AccessDecision) remoteaccessapi.RemoteAccessRuleSimulationResult {
	requirements := make([]remoteaccessapi.RemoteAccessApprovalRequirementSimulation, 0, len(value.ApprovalRequirements))
	for _, requirement := range value.ApprovalRequirements {
		requirements = append(requirements, remoteaccessapi.RemoteAccessApprovalRequirementSimulation{
			WorkflowId: openapi_types.UUID(requirement.WorkflowID), WorkflowVersion: requirement.WorkflowVersion,
			ApproverRoleIds: toOpenAPIUUIDs(requirement.ApproverRoleIDs), MinimumApprovals: requirement.MinimumApprovals,
			SeparationOfDuties: requirement.SeparationOfDuties, ApprovalTimeoutSeconds: int(requirement.ApprovalTimeout / time.Second), EscalationAfterSeconds: int(requirement.EscalationAfter / time.Second),
			TimeoutEffect:     remoteaccessapi.RemoteAccessApprovalRequirementSimulationTimeoutEffect(requirement.TimeoutEffect),
			EscalationRoleIds: toOpenAPIUUIDs(requirement.EscalationRoleIDs), SourceRuleIds: toOpenAPIUUIDs(requirement.SourceRuleIDs),
		})
	}
	profile := value.SessionProfile
	return remoteaccessapi.RemoteAccessRuleSimulationResult{
		Outcome:     remoteaccessapi.RemoteAccessRuleSimulationResultOutcome(value.Outcome),
		ReasonCodes: slices.Clone(value.ReasonCodes), Explanation: slices.Clone(value.Explanation),
		MatchedGrants: toRemoteAccessObjectVersions(value.MatchedGrantSnapshots), MatchedRules: toRemoteAccessObjectVersions(value.MatchedRuleSnapshots),
		ApprovalRequirements: requirements, SnapshotHash: hex.EncodeToString(value.SnapshotHash[:]),
		SessionProfile: remoteaccessapi.RemoteAccessSessionProfileSnapshot{
			SourceProfiles: toRemoteAccessObjectVersions(profile.SourceProfiles), MaxSessionSeconds: profile.MaxSessionSeconds,
			IdleTimeoutSeconds: profile.IdleTimeoutSeconds, RecordingMode: remoteaccessapi.RemoteAccessSessionProfileSnapshotRecordingMode(profile.RecordingMode),
			CommandAuditMode: remoteaccessapi.RemoteAccessSessionProfileSnapshotCommandAuditMode(profile.CommandAuditMode),
			ClipboardMode:    remoteaccessapi.RemoteAccessSessionProfileSnapshotClipboardMode(profile.ClipboardMode),
			FileUploadMode:   remoteaccessapi.RemoteAccessSessionProfileSnapshotFileUploadMode(profile.FileUploadMode),
			FileDownloadMode: remoteaccessapi.RemoteAccessSessionProfileSnapshotFileDownloadMode(profile.FileDownloadMode),
			PortForwardMode:  remoteaccessapi.RemoteAccessSessionProfileSnapshotPortForwardMode(profile.PortForwardMode),
			SessionShareMode: remoteaccessapi.RemoteAccessSessionProfileSnapshotSessionShareMode(profile.SessionShareMode), RetentionDays: profile.RetentionDays,
		},
	}
}

func toRemoteAccessObjectVersions(values []remoteaccess.ObjectVersionSnapshot) []remoteaccessapi.RemoteAccessObjectVersion {
	result := make([]remoteaccessapi.RemoteAccessObjectVersion, len(values))
	for i, value := range values {
		result[i] = remoteaccessapi.RemoteAccessObjectVersion{Id: openapi_types.UUID(value.ID), Version: value.Version}
	}
	return result
}

func decodeStringArray(raw []byte) *[]string {
	var value []string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func decodeObject(raw []byte) *map[string]interface{} {
	var value map[string]interface{}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func decodeObjectArray(raw []byte) *[]map[string]interface{} {
	var value []map[string]interface{}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func toRemoteAccessRecording(value db.RemoteAccessRecording) remoteaccessapi.RemoteAccessRecording {
	result := remoteaccessapi.RemoteAccessRecording{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID), SessionId: openapi_types.UUID(value.SessionID),
		Status: remoteaccessapi.RemoteAccessRecordingStatus(value.Status), Format: remoteaccessapi.RemoteAccessRecordingFormat(value.Format), Encrypted: true,
		ChunkCount: int(value.ChunkCount), EventCount: int(value.EventCount), SizeBytes: value.SizeBytes, RetentionUntil: value.RetentionUntil.Time, CreatedAt: value.CreatedAt.Time}
	if value.DurationMs > 0 {
		result.DurationMs = &value.DurationMs
	}
	if len(value.FinalHash) > 0 {
		hash := hex.EncodeToString(value.FinalHash)
		result.FinalHash = &hash
	}
	if value.CompletedAt.Valid {
		result.CompletedAt = &value.CompletedAt.Time
	}
	return result
}

func pointerOpenAPIUUID(value uuid.UUID) *openapi_types.UUID {
	converted := openapi_types.UUID(value)
	return &converted
}

func toOpenAPIUUIDs(values []uuid.UUID) []openapi_types.UUID {
	result := make([]openapi_types.UUID, len(values))
	for i := range values {
		result[i] = openapi_types.UUID(values[i])
	}
	return result
}

func fromOpenAPIUUIDs(values []openapi_types.UUID) []uuid.UUID {
	result := make([]uuid.UUID, len(values))
	for i := range values {
		result[i] = uuid.UUID(values[i])
	}
	return result
}

func toRemoteProtocols(values []string) []remoteaccessapi.RemoteAccessProtocol {
	result := make([]remoteaccessapi.RemoteAccessProtocol, len(values))
	for i := range values {
		result[i] = remoteaccessapi.RemoteAccessProtocol(values[i])
	}
	return result
}

func toRemoteActions(values []string) []remoteaccessapi.RemoteAccessAction {
	result := make([]remoteaccessapi.RemoteAccessAction, len(values))
	for i := range values {
		result[i] = remoteaccessapi.RemoteAccessAction(values[i])
	}
	return result
}

func fromRemoteProtocols(values []remoteaccessapi.RemoteAccessProtocol) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = string(values[i])
	}
	return result
}

func fromRemoteActions(values []remoteaccessapi.RemoteAccessAction) []string {
	result := make([]string, len(values))
	for i := range values {
		result[i] = string(values[i])
	}
	return result
}
