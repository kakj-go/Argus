package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	remoteaccessapi "github.com/kakj-go/Argus/internal/gen/openapi/remoteaccessapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/remoteaccess"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type RemoteAccessHandler struct {
	Identity     EnterpriseIdentityHandler
	Service      remoteaccess.Service
	WebsocketURL string
}

func (handler RemoteAccessHandler) ListRemoteAccessGrants(ctx context.Context, request remoteaccessapi.ListRemoteAccessGrantsRequestObject) (remoteaccessapi.ListRemoteAccessGrantsResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.grant.read")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessGrantsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListGrants(ctx, actor.EnterpriseID, requestLimit(request.Params.Limit))
	if err != nil {
		return remoteaccessapi.ListRemoteAccessGrantsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	result := make([]remoteaccessapi.RemoteAccessGrant, 0, len(items))
	for _, item := range items {
		result = append(result, toRemoteAccessGrant(item))
	}
	return remoteaccessapi.ListRemoteAccessGrants200JSONResponse{Items: result, Page: emptyRemoteAccessPage()}, nil
}

func (handler RemoteAccessHandler) CreateRemoteAccessGrant(ctx context.Context, request remoteaccessapi.CreateRemoteAccessGrantRequestObject) (remoteaccessapi.CreateRemoteAccessGrantResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.grant.manage")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessGrantdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.CreateGrant(ctx, actor, grantInput(request.Body.SubjectType, request.Body.SubjectId, request.Body.HostIds, request.Body.HostSelector,
		request.Body.ManagedAccountIds, request.Body.Protocols, request.Body.Actions, request.Body.ValidFrom, request.Body.ValidUntil, request.Body.Enabled, 0), request.Params.IdempotencyKey)
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
	value, err := handler.Service.UpdateGrant(ctx, actor, uuid.UUID(request.Id), grantInput(request.Body.SubjectType, request.Body.SubjectId, request.Body.HostIds, request.Body.HostSelector,
		request.Body.ManagedAccountIds, request.Body.Protocols, request.Body.Actions, request.Body.ValidFrom, request.Body.ValidUntil, request.Body.Enabled, request.Body.ExpectedVersion))
	if err != nil {
		return remoteaccessapi.UpdateRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.UpdateRemoteAccessGrant200JSONResponse(toRemoteAccessGrant(value)), nil
}

func (handler RemoteAccessHandler) DisableRemoteAccessGrant(ctx context.Context, request remoteaccessapi.DisableRemoteAccessGrantRequestObject) (remoteaccessapi.DisableRemoteAccessGrantResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.grant.manage")
	if apiError != nil {
		return remoteaccessapi.DisableRemoteAccessGrantdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if err := handler.Service.DisableGrant(ctx, actor, uuid.UUID(request.Id)); err != nil {
		return remoteaccessapi.DisableRemoteAccessGrantdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.DisableRemoteAccessGrant204Response{}, nil
}

func (handler RemoteAccessHandler) ListRemoteAccessPolicies(ctx context.Context, _ remoteaccessapi.ListRemoteAccessPoliciesRequestObject) (remoteaccessapi.ListRemoteAccessPoliciesResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.policy.read")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessPoliciesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListPolicies(ctx, actor.EnterpriseID)
	if err != nil {
		return remoteaccessapi.ListRemoteAccessPoliciesdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	result := make([]remoteaccessapi.RemoteAccessPolicy, 0, len(items))
	for _, item := range items {
		result = append(result, toRemoteAccessPolicy(item))
	}
	return remoteaccessapi.ListRemoteAccessPolicies200JSONResponse{Items: result, Page: emptyRemoteAccessPage()}, nil
}

func (handler RemoteAccessHandler) CreateRemoteAccessPolicy(ctx context.Context, request remoteaccessapi.CreateRemoteAccessPolicyRequestObject) (remoteaccessapi.CreateRemoteAccessPolicyResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.policy.manage")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessPolicydefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateRemoteAccessPolicydefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.CreatePolicy(ctx, actor, policyInput(request.Body.Name, request.Body.Enabled, request.Body.Priority, request.Body.Protocols, request.Body.HostSelector,
		request.Body.ApproverRoleIds, request.Body.MinimumApprovals, request.Body.SeparationOfDuties, request.Body.RequireMfa, request.Body.MaxSessionSeconds, request.Body.IdleTimeoutSeconds, 0), request.Params.IdempotencyKey)
	if err != nil {
		return remoteaccessapi.CreateRemoteAccessPolicydefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.CreateRemoteAccessPolicy201JSONResponse(toRemoteAccessPolicy(value)), nil
}

func (handler RemoteAccessHandler) GetRemoteAccessPolicy(ctx context.Context, request remoteaccessapi.GetRemoteAccessPolicyRequestObject) (remoteaccessapi.GetRemoteAccessPolicyResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "remote_access.policy.read")
	if apiError != nil {
		return remoteaccessapi.GetRemoteAccessPolicydefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetPolicy(ctx, actor.EnterpriseID, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessPolicydefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessPolicy200JSONResponse(toRemoteAccessPolicy(value)), nil
}

func (handler RemoteAccessHandler) UpdateRemoteAccessPolicy(ctx context.Context, request remoteaccessapi.UpdateRemoteAccessPolicyRequestObject) (remoteaccessapi.UpdateRemoteAccessPolicyResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.policy.manage")
	if apiError != nil {
		return remoteaccessapi.UpdateRemoteAccessPolicydefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.UpdateRemoteAccessPolicydefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.UpdatePolicy(ctx, actor, uuid.UUID(request.Id), policyInput(request.Body.Name, request.Body.Enabled, request.Body.Priority, request.Body.Protocols, request.Body.HostSelector,
		request.Body.ApproverRoleIds, request.Body.MinimumApprovals, request.Body.SeparationOfDuties, request.Body.RequireMfa, request.Body.MaxSessionSeconds, request.Body.IdleTimeoutSeconds, request.Body.ExpectedVersion))
	if err != nil {
		return remoteaccessapi.UpdateRemoteAccessPolicydefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.UpdateRemoteAccessPolicy200JSONResponse(toRemoteAccessPolicy(value)), nil
}

func (handler RemoteAccessHandler) DisableRemoteAccessPolicy(ctx context.Context, request remoteaccessapi.DisableRemoteAccessPolicyRequestObject) (remoteaccessapi.DisableRemoteAccessPolicyResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.policy.manage")
	if apiError != nil {
		return remoteaccessapi.DisableRemoteAccessPolicydefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if err := handler.Service.DisablePolicy(ctx, actor, uuid.UUID(request.Id)); err != nil {
		return remoteaccessapi.DisableRemoteAccessPolicydefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.DisableRemoteAccessPolicy204Response{}, nil
}

func (handler RemoteAccessHandler) ListRemoteAccessRequests(ctx context.Context, request remoteaccessapi.ListRemoteAccessRequestsRequestObject) (remoteaccessapi.ListRemoteAccessRequestsResponseObject, error) {
	actor, principal, apiError := handler.actorAny(ctx, false, "", "remote_access.request", "remote_access.session.approve")
	if apiError != nil {
		return remoteaccessapi.ListRemoteAccessRequestsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListRequests(ctx, actor, hasPermission(principal, "remote_access.session.approve"), requestLimit(request.Params.Limit))
	if err != nil {
		return remoteaccessapi.ListRemoteAccessRequestsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	result := make([]remoteaccessapi.AccessRequest, 0, len(items))
	for _, item := range items {
		result = append(result, toAccessRequest(item))
	}
	return remoteaccessapi.ListRemoteAccessRequests200JSONResponse{Items: result, Page: emptyRemoteAccessPage()}, nil
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
	value, err := handler.Service.GetRequest(ctx, actor, uuid.UUID(request.Id), hasPermission(principal, "remote_access.session.approve"))
	if err != nil {
		return remoteaccessapi.GetRemoteAccessRequestdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	return remoteaccessapi.GetRemoteAccessRequest200JSONResponse(toAccessRequest(value)), nil
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
	value, err := handler.Service.RevokeLease(ctx, actor, uuid.UUID(request.Id), "operator_revoked")
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
	items, err := handler.Service.ListSessions(ctx, actor, hasPermission(principal, "remote_access.session.terminate"), requestLimit(request.Params.Limit))
	if err != nil {
		return remoteaccessapi.ListRemoteAccessSessionsdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	result := make([]remoteaccessapi.RemoteAccessSession, 0, len(items))
	for _, item := range items {
		result = append(result, toRemoteAccessSession(item))
	}
	return remoteaccessapi.ListRemoteAccessSessions200JSONResponse{Items: result, Page: emptyRemoteAccessPage()}, nil
}

func (handler RemoteAccessHandler) CreateRemoteAccessSession(ctx context.Context, request remoteaccessapi.CreateRemoteAccessSessionRequestObject) (remoteaccessapi.CreateRemoteAccessSessionResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "remote_access.session.create")
	if apiError != nil {
		return remoteaccessapi.CreateRemoteAccessSessiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return remoteaccessapi.CreateRemoteAccessSessiondefaultJSONResponse{Body: remoteAccessError(ctx, remoteaccess.ErrInvalidRequest), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.CreateSession(ctx, actor, uuid.UUID(request.Body.LeaseId))
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
	value, err := handler.Service.TerminateSession(ctx, actor, uuid.UUID(request.Id), request.Body.Reason, hasPermission(principal, "remote_access.session.terminate"))
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
	value, err := handler.Service.IssueTicket(ctx, actor, uuid.UUID(request.Id))
	if err != nil {
		return remoteaccessapi.CreateRemoteAccessSessionTicketdefaultJSONResponse{Body: remoteAccessError(ctx, err), StatusCode: remoteAccessStatus(err)}, nil
	}
	ticket := value.Ticket
	return remoteaccessapi.CreateRemoteAccessSessionTicket201JSONResponse{SessionId: openapi_types.UUID(value.SessionID), Ticket: &ticket,
		ExpiresAt: value.ExpiresAt, ProtocolVersion: remoteaccessapi.ArgusRemoteAccessv1, WebsocketUrl: strings.TrimRight(handler.WebsocketURL, "/") + "/v1/sessions/" + value.SessionID.String()}, nil
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
	result := remoteaccessapi.RecordingEventPage{Recording: toRemoteAccessRecording(page.Recording), NextCursor: strconv.FormatInt(page.Next, 10), Complete: page.Complete}
	for _, event := range page.Events {
		result.Events = append(result.Events, struct {
			Data interface{}                                  `json:"data"`
			Time float32                                      `json:"time"`
			Type remoteaccessapi.RecordingEventPageEventsType `json:"type"`
		}{Data: event.Data, Time: float32(event.Time), Type: remoteaccessapi.RecordingEventPageEventsType(event.Type)})
	}
	return remoteaccessapi.ListRemoteAccessRecordingEvents200JSONResponse(result), nil
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
		if principal.EnterpriseUser == nil || principal.Session.ID == uuid.Nil {
			denied := remoteAccessError(ctx, errors.New("human enterprise session required"))
			denied.Code, denied.MessageKey = "AUTHORIZATION_DENIED", "errors.auth.authorization_denied"
			return remoteaccess.Actor{}, identity.Principal{}, &denied
		}
		return remoteaccess.Actor{EnterpriseID: principal.EnterpriseUser.EnterpriseID, UserID: principal.EnterpriseUser.ID,
			DepartmentID: principal.EnterpriseUser.DepartmentID, HTTPSessionID: principal.Session.ID,
			AuthorizationVersion: principal.EnterpriseUser.AuthorizationVersion, DataScopeIDs: slices.Clone(principal.DataScopeIDs),
			StepUpAuthenticated: handler.Identity.Auth.Identity.RequireStepUp(principal) == nil}, principal, nil
	}
	if last == nil {
		value := remoteAccessError(ctx, identity.ErrSessionInvalid)
		last = &value
	}
	return remoteaccess.Actor{}, identity.Principal{}, last
}

func grantInput(subjectType remoteaccessapi.RemoteAccessSubjectType, subjectID openapi_types.UUID, hostIDs []openapi_types.UUID, selector *remoteaccessapi.LabelSelector,
	accountIDs []openapi_types.UUID, protocols []remoteaccessapi.RemoteAccessProtocol, actions []remoteaccessapi.RemoteAccessAction,
	validFrom, validUntil time.Time, enabled bool, expected int64) remoteaccess.GrantInput {
	return remoteaccess.GrantInput{SubjectType: string(subjectType), SubjectID: uuid.UUID(subjectID), HostIDs: fromOpenAPIUUIDs(hostIDs),
		HostSelector: marshalRemoteSelector(selector), ManagedAccountIDs: fromOpenAPIUUIDs(accountIDs), Protocols: fromRemoteProtocols(protocols),
		Actions: fromRemoteActions(actions), ValidFrom: validFrom, ValidUntil: validUntil, Enabled: enabled, ExpectedVersion: expected}
}

func policyInput(name string, enabled bool, priority int, protocols []remoteaccessapi.RemoteAccessProtocol, selector *remoteaccessapi.LabelSelector,
	roleIDs *[]openapi_types.UUID, minimum int, separation, requireMFA bool, maxSeconds, idleSeconds int, expected int64) remoteaccess.PolicyInput {
	var roles []uuid.UUID
	if roleIDs != nil {
		roles = fromOpenAPIUUIDs(*roleIDs)
	}
	return remoteaccess.PolicyInput{Name: name, Enabled: enabled, Priority: int32(priority), Protocols: fromRemoteProtocols(protocols),
		HostSelector: marshalRemoteSelector(selector), ApproverRoleIDs: roles, MinimumApprovals: int32(minimum), SeparationOfDuties: separation,
		RequireMFA: requireMFA, MaxSessionSeconds: int32(maxSeconds), IdleTimeoutSeconds: int32(idleSeconds), ExpectedVersion: expected}
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

func emptyRemoteAccessPage() remoteaccessapi.CursorPage {
	return remoteaccessapi.CursorPage{NextCursor: nil, HasMore: false, Partial: remoteaccessapi.PartialMetadata{Partial: false, Reasons: []remoteaccessapi.PartialMetadataReasons{}}}
}

func remoteAccessError(ctx context.Context, err error) remoteaccessapi.ApiError {
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
	case errors.Is(err, remoteaccess.ErrMFARequired):
		base.Code, base.MessageKey = "REMOTE_ACCESS_MFA_REQUIRED", "errors.remote_access.mfa_required"
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
	case errors.Is(err, remoteaccess.ErrInvalidRequest):
		base.Code, base.MessageKey = "INVALID_ARGUMENT", "errors.common.invalid_argument"
	case errors.Is(err, postgres.ErrIdempotencyConflict):
		base.Code, base.MessageKey = "IDEMPOTENCY_CONFLICT", "errors.common.idempotency_conflict"
	}
	return remoteaccessapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]remoteaccessapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func remoteAccessStatus(err error) int {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	case errors.Is(err, remoteaccess.ErrInvalidRequest):
		return http.StatusBadRequest
	case errors.Is(err, remoteaccess.ErrScopeDenied), errors.Is(err, remoteaccess.ErrGrantRequired), errors.Is(err, remoteaccess.ErrApprovalNotEligible):
		return http.StatusForbidden
	case errors.Is(err, remoteaccess.ErrRecordingUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusConflict
	}
}

func toRemoteAccessGrant(value db.RemoteAccessGrant) remoteaccessapi.RemoteAccessGrant {
	result := remoteaccessapi.RemoteAccessGrant{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID), SubjectType: remoteaccessapi.RemoteAccessSubjectType(value.SubjectType),
		SubjectId: openapi_types.UUID(value.SubjectID), HostIds: toOpenAPIUUIDs(value.HostIds), ManagedAccountIds: toOpenAPIUUIDs(value.ManagedAccountIds),
		Protocols: toRemoteProtocols(value.Protocols), Actions: toRemoteActions(value.Actions), ValidFrom: value.ValidFrom.Time, ValidUntil: value.ValidUntil.Time,
		Enabled: value.Enabled, Version: value.Version, CreatedBy: pointerOpenAPIUUID(value.CreatedBy), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if selector := decodeRemoteSelector(value.HostSelector); selector != nil {
		result.HostSelector = selector
	}
	return result
}

func toRemoteAccessPolicy(value db.RemoteAccessPolicy) remoteaccessapi.RemoteAccessPolicy {
	roles := toOpenAPIUUIDs(value.ApproverRoleIds)
	result := remoteaccessapi.RemoteAccessPolicy{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerOpenAPIUUID(value.EnterpriseID), Name: value.Name, Enabled: value.Enabled,
		Priority: int(value.Priority), Protocols: toRemoteProtocols(value.Protocols), ApproverRoleIds: &roles, MinimumApprovals: int(value.MinimumApprovals),
		SeparationOfDuties: value.SeparationOfDuties, RequireMfa: value.RequireMfa, MaxSessionSeconds: int(value.MaxSessionSeconds),
		IdleTimeoutSeconds: int(value.IdleTimeoutSeconds), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	result.HostSelector = decodeRemoteSelector(value.HostSelector)
	return result
}

func toAccessRequest(value remoteaccess.RequestView) remoteaccessapi.AccessRequest {
	request := value.Request
	result := remoteaccessapi.AccessRequest{Id: openapi_types.UUID(request.ID), EnterpriseId: pointerOpenAPIUUID(request.EnterpriseID), RequesterId: pointerOpenAPIUUID(request.RequesterID),
		GrantId: openapi_types.UUID(request.GrantID), HostId: openapi_types.UUID(request.HostID), ManagedAccountId: openapi_types.UUID(request.ManagedAccountID),
		Protocol: remoteaccessapi.RemoteAccessProtocol(request.Protocol), Action: remoteaccessapi.RemoteAccessAction(request.Action), Reason: request.Reason,
		Status: remoteaccessapi.RemoteAccessRequestStatus(request.Status), AuthorizationVersion: request.AuthorizationVersion, ExpiresAt: request.ExpiresAt.Time,
		CreatedAt: request.CreatedAt.Time, UpdatedAt: request.UpdatedAt.Time, Requirements: []remoteaccessapi.RemoteAccessRequirement{}, Decisions: []remoteaccessapi.RemoteAccessDecision{}}
	for _, item := range value.Requirements {
		requirement := item.Requirement
		result.Requirements = append(result.Requirements, remoteaccessapi.RemoteAccessRequirement{Id: openapi_types.UUID(requirement.ID), PolicyId: openapi_types.UUID(requirement.PolicyID),
			PolicyVersion: requirement.PolicyVersion, MinimumApprovals: int(requirement.MinimumApprovals), SeparationOfDuties: requirement.SeparationOfDuties,
			RequireMfa: requirement.RequireMfa, Status: remoteaccessapi.RemoteAccessRequirementStatus(requirement.Status), ApprovedCount: int(item.ApprovedCount)})
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
	return result
}

func toRemoteAccessSession(value remoteaccess.SessionView) remoteaccessapi.RemoteAccessSession {
	session := value.Session
	result := remoteaccessapi.RemoteAccessSession{Id: openapi_types.UUID(session.ID), EnterpriseId: pointerOpenAPIUUID(session.EnterpriseID), UserId: pointerOpenAPIUUID(session.UserID),
		LeaseId: openapi_types.UUID(session.LeaseID), HostId: openapi_types.UUID(session.HostID), ManagedAccountId: openapi_types.UUID(session.ManagedAccountID),
		Protocol: remoteaccessapi.RemoteAccessProtocol(session.Protocol), ConnectionMode: remoteaccessapi.RemoteAccessSessionConnectionMode(session.ConnectionMode),
		Status: remoteaccessapi.RemoteAccessSessionStatus(session.Status), RecordingId: openapi_types.UUID(value.RecordingID), IdleTimeoutSeconds: int(session.IdleTimeoutSeconds),
		MaxDurationSeconds: int(session.MaxDurationSeconds), ConnectBefore: session.ConnectBefore.Time, CreatedAt: session.CreatedAt.Time, UpdatedAt: session.UpdatedAt.Time}
	fence := session.SessionFence
	result.SessionFence = &fence
	if session.ConnectedAt.Valid {
		result.ConnectedAt = &session.ConnectedAt.Time
	}
	if session.TerminatedAt.Valid {
		result.TerminatedAt = &session.TerminatedAt.Time
	}
	if session.TerminationReason.Valid {
		result.TerminationReason = &session.TerminationReason.String
	}
	return result
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

func decodeRemoteSelector(raw []byte) *remoteaccessapi.LabelSelector {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return nil
	}
	var selector remoteaccessapi.LabelSelector
	if json.Unmarshal(raw, &selector) != nil {
		return nil
	}
	return &selector
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

func marshalRemoteSelector(value *remoteaccessapi.LabelSelector) json.RawMessage {
	if value == nil {
		return nil
	}
	raw, _ := json.Marshal(value)
	return raw
}
