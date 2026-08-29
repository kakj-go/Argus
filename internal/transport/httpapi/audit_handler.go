package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/kakj-go/Argus/internal/audit"
	auditapi "github.com/kakj-go/Argus/internal/gen/openapi/audit"
	enterpriseapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseidentity"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type AuditHandler struct {
	Auth       SetupHandler
	Enterprise EnterpriseIdentityHandler
	Store      *postgres.Store
	Cursor     pagination.Signer
}

type auditActorPresentation struct {
	displayName string
	username    string
}

type auditPresentationResolver struct {
	queries   *db.Queries
	actors    map[string]auditActorPresentation
	resources map[string]string
}

func (handler AuditHandler) ListAuditEvents(ctx context.Context, request auditapi.ListAuditEventsRequestObject) (auditapi.ListAuditEventsResponseObject, error) {
	audience := string(request.Audience)
	var principal identity.Principal
	var err error
	if audience == "enterprise" {
		var apiError *enterpriseapi.ApiError
		principal, apiError = handler.Enterprise.enterprisePrincipal(ctx, false, "", "audit.read")
		if apiError != nil {
			return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditapi.ApiError{Code: apiError.Code, Message: apiError.Message, MessageKey: apiError.MessageKey,
				Params: copyErrorParams[map[string]auditapi.ApiError_Params_AdditionalProperties](apiError.Params), RequestId: apiError.RequestId,
				Retryable: apiError.Retryable, TraceId: apiError.TraceId}, StatusCode: http.StatusForbidden}, nil
		}
	} else {
		principal, err = handler.Auth.authenticate(ctx, audience, false, "")
		if err != nil {
			return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditError(ctx, err), StatusCode: authStatus(err)}, nil
		}
	}
	var rows []db.AuditEvent
	var binding pagination.Binding
	if audience == "platform" {
		if principal.PlatformUser == nil {
			return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditError(ctx, errors.New("authorization denied")), StatusCode: http.StatusForbidden}, nil
		}
		rows, err = handler.Store.Queries.ListAllPlatformAuditEvents(ctx)
		binding = pagination.Binding{Audience: "platform", SubjectType: "platform_user", SubjectID: principal.ActorID(), FilterHash: pagination.HashFilter(map[string]any{"action": request.Params.Action}), Sort: "created_at_desc"}
	} else if audience == "enterprise" {
		if _, ok := principal.EnterpriseID(); !ok || (!slices.Contains(principal.Permissions, "*") && !slices.Contains(principal.Permissions, "audit.read")) {
			return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditError(ctx, errors.New("authorization denied")), StatusCode: http.StatusForbidden}, nil
		}
		rows, err = handler.Store.Queries.ListAllEnterpriseAuditEvents(ctx, uuid.NullUUID{UUID: principal.EnterpriseIDValue(), Valid: true})
		binding = enterpriseCursorBinding(principal, map[string]any{"action": request.Params.Action}, "created_at_desc")
	} else {
		return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditError(ctx, errors.New("invalid audience")), StatusCode: http.StatusBadRequest}, nil
	}
	if err != nil {
		return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	filtered := make([]db.AuditEvent, 0, len(rows))
	for _, row := range rows {
		if request.Params.Action != nil && row.Action != *request.Params.Action {
			continue
		}
		filtered = append(filtered, row)
	}
	filtered, next, hasMore, err := paginate(handler.Cursor, filtered, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), binding, func(value db.AuditEvent) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		code, key, status := paginationError(err)
		return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, StatusCode: status}, nil
	}
	items := make([]auditapi.AuditEvent, 0, len(filtered))
	resolver := auditPresentationResolver{
		queries:   handler.Store.Queries,
		actors:    make(map[string]auditActorPresentation),
		resources: make(map[string]string),
	}
	for _, row := range filtered {
		items = append(items, resolver.toAuditEvent(ctx, row))
	}
	return auditapi.ListAuditEvents200JSONResponse{Items: items, Page: auditapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: auditapi.PartialMetadata{Partial: false, Reasons: []auditapi.PartialMetadataReasons{}}}}, nil
}

func (resolver *auditPresentationResolver) toAuditEvent(ctx context.Context, row db.AuditEvent) auditapi.AuditEvent {
	return applyAuditPresentation(
		toAuditEvent(row),
		resolver.resolveActor(ctx, row),
		resolver.resolveResource(ctx, row),
	)
}

func applyAuditPresentation(result auditapi.AuditEvent, actor auditActorPresentation, resourceName string) auditapi.AuditEvent {
	if actor.displayName != "" {
		result.ActorDisplayName = &actor.displayName
	}
	if actor.username != "" {
		result.ActorUsername = &actor.username
	}
	if resourceName != "" {
		result.ResourceDisplayName = &resourceName
	}
	return result
}

func (resolver *auditPresentationResolver) resolveActor(ctx context.Context, row db.AuditEvent) auditActorPresentation {
	key := row.ActorType + ":" + row.ActorID
	if cached, ok := resolver.actors[key]; ok {
		return cached
	}
	resolved := auditActorPresentation{}
	id, err := uuid.Parse(row.ActorID)
	if err == nil {
		switch row.ActorType {
		case "platform_user":
			if value, queryErr := resolver.queries.GetPlatformUser(ctx, id); queryErr == nil {
				resolved = auditActorPresentation{displayName: value.DisplayName, username: value.Username}
			}
		case "enterprise_user":
			if value, queryErr := resolver.queries.GetEnterpriseUserByID(ctx, id); queryErr == nil {
				resolved = auditActorPresentation{displayName: value.DisplayName, username: value.Username}
			}
		case "service_account":
			if row.EnterpriseID.Valid {
				if value, queryErr := resolver.queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: id, EnterpriseID: row.EnterpriseID.UUID}); queryErr == nil {
					resolved.displayName = value.Name
				}
			}
		case "connector":
			if row.EnterpriseID.Valid {
				if value, queryErr := resolver.queries.GetConnector(ctx, db.GetConnectorParams{ID: id, EnterpriseID: row.EnterpriseID.UUID}); queryErr == nil {
					resolved.displayName = value.Name
				}
			}
		}
	}
	resolver.actors[key] = resolved
	return resolved
}

func (resolver *auditPresentationResolver) resolveResource(ctx context.Context, row db.AuditEvent) string {
	if !row.ResourceType.Valid || !row.ResourceID.Valid {
		return ""
	}
	key := row.ResourceType.String + ":" + row.ResourceID.String
	if cached, ok := resolver.resources[key]; ok {
		return cached
	}
	resolved := resolver.resolveResourceUncached(ctx, row)
	resolver.resources[key] = resolved
	return resolved
}

func (resolver *auditPresentationResolver) resolveResourceUncached(ctx context.Context, row db.AuditEvent) string {
	id, err := uuid.Parse(row.ResourceID.String)
	if err != nil {
		return ""
	}
	if row.ResourceType.String == "enterprise" {
		if value, queryErr := resolver.queries.GetEnterprise(ctx, id); queryErr == nil {
			return value.Name
		}
		return ""
	}
	if !row.EnterpriseID.Valid {
		return ""
	}
	enterpriseID := row.EnterpriseID.UUID
	switch row.ResourceType.String {
	case "enterprise_user", "enterprise_admin", "user":
		if value, queryErr := resolver.queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.DisplayName
		}
	case "department":
		if value, queryErr := resolver.queries.GetDepartment(ctx, db.GetDepartmentParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "role":
		if value, queryErr := resolver.queries.GetRole(ctx, db.GetRoleParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "role_binding":
		if binding, queryErr := resolver.queries.GetRoleBinding(ctx, db.GetRoleBindingParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			if role, roleErr := resolver.queries.GetRole(ctx, db.GetRoleParams{ID: binding.RoleID, EnterpriseID: enterpriseID}); roleErr == nil {
				return role.Name
			}
		}
	case "service_account":
		if value, queryErr := resolver.queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "secret":
		if value, queryErr := resolver.queries.GetSecret(ctx, db.GetSecretParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "credential":
		if value, queryErr := resolver.queries.GetCredential(ctx, db.GetCredentialParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "managed_account":
		if value, queryErr := resolver.queries.GetManagedAccount(ctx, db.GetManagedAccountParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Username
		}
	case "host":
		if value, queryErr := resolver.queries.GetHost(ctx, db.GetHostParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "kubernetes_cluster":
		if value, queryErr := resolver.queries.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "ai_model":
		if value, queryErr := resolver.queries.GetAIModel(ctx, db.GetAIModelParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "connector":
		if value, queryErr := resolver.queries.GetConnector(ctx, db.GetConnectorParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "bastion_scope":
		if value, queryErr := resolver.queries.GetBastionScope(ctx, db.GetBastionScopeParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "approval_policy":
		if value, queryErr := resolver.queries.GetApprovalPolicy(ctx, db.GetApprovalPolicyParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Name
		}
	case "pending_action":
		if value, queryErr := resolver.queries.GetPendingActionByID(ctx, db.GetPendingActionByIDParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.Title
		}
	case "execution":
		if value, queryErr := resolver.queries.GetExecution(ctx, db.GetExecutionParams{ID: id, EnterpriseID: enterpriseID}); queryErr == nil {
			return value.ExecutionRef
		}
	}
	return ""
}

func toAuditEvent(row db.AuditEvent) auditapi.AuditEvent {
	details := map[string]any{}
	_ = json.Unmarshal(row.Details, &details)
	result := auditapi.AuditEvent{Id: openapi_types.UUID(row.ID), Domain: auditapi.AuditEventDomain(row.Domain), ActorType: auditapi.AuditEventActorType(row.ActorType),
		ActorId: row.ActorID, Action: row.Action, Result: auditapi.AuditEventResult(row.Result), Details: details,
		PreviousHash: audit.HashHex(row.PreviousHash), EventHash: audit.HashHex(row.EventHash), CreatedAt: row.CreatedAt.Time}
	if row.EnterpriseID.Valid {
		value := openapi_types.UUID(row.EnterpriseID.UUID)
		result.EnterpriseId = &value
	}
	if row.ResourceType.Valid {
		result.ResourceType = &row.ResourceType.String
	}
	if row.ResourceID.Valid {
		result.ResourceId = &row.ResourceID.String
	}
	return result
}

func auditError(ctx context.Context, err error) auditapi.ApiError {
	base := setupErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	if err != nil && err.Error() == "authorization denied" {
		base.Code, base.MessageKey = "AUTHORIZATION_DENIED", "errors.auth.authorization_denied"
	}
	return auditapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]auditapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}
