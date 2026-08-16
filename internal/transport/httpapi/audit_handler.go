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

func (handler AuditHandler) ListAuditEvents(ctx context.Context, request auditapi.ListAuditEventsRequestObject) (auditapi.ListAuditEventsResponseObject, error) {
	audience := string(request.Audience)
	var principal identity.Principal
	var err error
	if audience == "enterprise" {
		var apiError *enterpriseapi.ApiError
		principal, apiError = handler.Enterprise.enterprisePrincipal(ctx, false, "", "audit.read")
		if apiError != nil {
			return auditapi.ListAuditEventsdefaultJSONResponse{Body: auditapi.ApiError{Code: apiError.Code, MessageKey: apiError.MessageKey, RequestId: apiError.RequestId, Retryable: apiError.Retryable}, StatusCode: http.StatusForbidden}, nil
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
	for _, row := range filtered {
		items = append(items, toAuditEvent(row))
	}
	return auditapi.ListAuditEvents200JSONResponse{Items: items, Page: auditapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: auditapi.PartialMetadata{Partial: false, Reasons: []auditapi.PartialMetadataReasons{}}}}, nil
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
	base := setupError(ctx, err)
	if err != nil && err.Error() == "authorization denied" {
		base.Code, base.MessageKey = "AUTHORIZATION_DENIED", "errors.auth.authorization_denied"
	}
	return auditapi.ApiError{Code: base.Code, MessageKey: base.MessageKey, RequestId: base.RequestId, Retryable: base.Retryable}
}
