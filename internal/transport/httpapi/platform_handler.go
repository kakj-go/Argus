package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	platformapi "github.com/kakj-go/Argus/internal/gen/openapi/platform"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/platform"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type PlatformHandler struct {
	Auth       SetupHandler
	Enterprise platform.EnterpriseService
	Cursor     pagination.Signer
}

func (handler PlatformHandler) ListEnterprises(ctx context.Context, request platformapi.ListEnterprisesRequestObject) (platformapi.ListEnterprisesResponseObject, error) {
	principal, response := handler.platformPrincipal(ctx, false, "")
	if response != nil {
		return platformapi.ListEnterprisesdefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	items, err := handler.Enterprise.ListEnterprises(ctx)
	if err != nil {
		return platformapi.ListEnterprisesdefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	binding := pagination.Binding{Audience: "platform", SubjectType: "platform_user", SubjectID: principal.ActorID(), FilterHash: pagination.HashFilter(nil), Sort: "created_at_desc"}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), binding, func(value db.Enterprise) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		code, key, status := paginationError(err)
		return platformapi.ListEnterprisesdefaultJSONResponse{Body: platformapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, StatusCode: status}, nil
	}
	converted := make([]platformapi.Enterprise, 0, len(items))
	for _, item := range items {
		converted = append(converted, toPlatformEnterprise(item))
	}
	return platformapi.ListEnterprises200JSONResponse{Items: converted, Page: platformapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyPlatformPage().Partial}}, nil
}

func (handler PlatformHandler) GetEnterprise(ctx context.Context, request platformapi.GetEnterpriseRequestObject) (platformapi.GetEnterpriseResponseObject, error) {
	if _, response := handler.platformPrincipal(ctx, false, ""); response != nil {
		return platformapi.GetEnterprisedefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Enterprise.GetEnterprise(ctx, uuid.UUID(request.Id))
	if err != nil {
		return platformapi.GetEnterprisedefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusNotFound}, nil
	}
	return platformapi.GetEnterprise200JSONResponse(toPlatformEnterprise(value)), nil
}

func (handler PlatformHandler) CreateEnterprise(ctx context.Context, request platformapi.CreateEnterpriseRequestObject) (platformapi.CreateEnterpriseResponseObject, error) {
	principal, response := handler.platformPrincipal(ctx, true, request.Params.XCSRFToken)
	if response != nil {
		return platformapi.CreateEnterprisedefaultJSONResponse{Body: *response, StatusCode: authStatus(identity.ErrSessionInvalid)}, nil
	}
	if request.Body == nil {
		return platformapi.CreateEnterprisedefaultJSONResponse{Body: platformError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	locale := "zh-CN"
	if request.Body.DefaultLocale != nil {
		locale = string(*request.Body.DefaultLocale)
	}
	remark := ""
	if request.Body.Remark != nil {
		remark = *request.Body.Remark
	}
	value, err := handler.Enterprise.CreateEnterprise(ctx, principal.PlatformUser.ID.String(), platform.CreateEnterpriseInput{
		Name: request.Body.Name, Code: request.Body.Code, Timezone: request.Body.Timezone, DefaultLocale: locale, Remark: remark,
	}, request.Params.IdempotencyKey)
	if err != nil {
		return platformapi.CreateEnterprisedefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return platformapi.CreateEnterprise201JSONResponse(toPlatformEnterprise(value)), nil
}

func (handler PlatformHandler) UpdateEnterprise(ctx context.Context, request platformapi.UpdateEnterpriseRequestObject) (platformapi.UpdateEnterpriseResponseObject, error) {
	if _, response := handler.platformPrincipal(ctx, true, request.Params.XCSRFToken); response != nil {
		return platformapi.UpdateEnterprisedefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	if request.Body == nil {
		return platformapi.UpdateEnterprisedefaultJSONResponse{Body: platformError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	var locale *string
	if request.Body.DefaultLocale != nil {
		value := string(*request.Body.DefaultLocale)
		locale = &value
	}
	value, err := handler.Enterprise.UpdateEnterprise(ctx, uuid.UUID(request.Id), platform.UpdateEnterpriseInput{
		Name: request.Body.Name, Timezone: request.Body.Timezone, DefaultLocale: locale, Remark: request.Body.Remark, ExpectedVersion: request.Body.ExpectedVersion,
	})
	if err != nil {
		return platformapi.UpdateEnterprisedefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return platformapi.UpdateEnterprise200JSONResponse(toPlatformEnterprise(value)), nil
}

func (handler PlatformHandler) ChangeEnterpriseState(ctx context.Context, request platformapi.ChangeEnterpriseStateRequestObject) (platformapi.ChangeEnterpriseStateResponseObject, error) {
	principal, response := handler.platformPrincipal(ctx, true, request.Params.XCSRFToken)
	if response != nil {
		return platformapi.ChangeEnterpriseStatedefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	status := request.StateAction
	if status == "suspend" {
		status = "suspended"
	}
	if status == "activate" {
		status = "active"
	}
	if status == "disable" {
		status = "disabled"
	}
	value, err := handler.Enterprise.ChangeStatus(ctx, principal.PlatformUser.ID.String(), uuid.UUID(request.Id), status, request.Params.ExpectedVersion)
	if err != nil {
		return platformapi.ChangeEnterpriseStatedefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return platformapi.ChangeEnterpriseState200JSONResponse(toPlatformEnterprise(value)), nil
}

func (handler PlatformHandler) ListEnterpriseAdmins(ctx context.Context, request platformapi.ListEnterpriseAdminsRequestObject) (platformapi.ListEnterpriseAdminsResponseObject, error) {
	principal, response := handler.platformPrincipal(ctx, false, "")
	if response != nil {
		return platformapi.ListEnterpriseAdminsdefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	enterpriseID := uuid.NullUUID{}
	if request.Params.EnterpriseId != nil {
		enterpriseID = uuid.NullUUID{UUID: uuid.UUID(*request.Params.EnterpriseId), Valid: true}
	}
	users, err := handler.Enterprise.ListAdmins(ctx, enterpriseID)
	if err != nil {
		return platformapi.ListEnterpriseAdminsdefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	enterpriseFilter := ""
	if enterpriseID.Valid {
		enterpriseFilter = enterpriseID.UUID.String()
	}
	binding := pagination.Binding{Audience: "platform", SubjectType: "platform_user", SubjectID: principal.ActorID(), FilterHash: pagination.HashFilter(map[string]any{"enterprise_id": enterpriseFilter}), Sort: "created_at_asc"}
	users, next, hasMore, err := paginate(handler.Cursor, users, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), binding, func(value db.EnterpriseUser) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		code, key, status := paginationError(err)
		return platformapi.ListEnterpriseAdminsdefaultJSONResponse{Body: platformapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, StatusCode: status}, nil
	}
	items := make([]platformapi.EnterpriseUser, 0, len(users))
	for _, user := range users {
		items = append(items, toPlatformEnterpriseUser(user))
	}
	return platformapi.ListEnterpriseAdmins200JSONResponse{Items: items, Page: platformapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyPlatformPage().Partial}}, nil
}

func (handler PlatformHandler) CreateEnterpriseAdmin(ctx context.Context, request platformapi.CreateEnterpriseAdminRequestObject) (platformapi.CreateEnterpriseAdminResponseObject, error) {
	principal, response := handler.platformPrincipal(ctx, true, request.Params.XCSRFToken)
	if response != nil {
		return platformapi.CreateEnterpriseAdmindefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	if request.Body == nil {
		return platformapi.CreateEnterpriseAdmindefaultJSONResponse{Body: platformError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	email := ""
	if request.Body.Email != nil {
		email = string(*request.Body.Email)
	}
	created, err := handler.Enterprise.CreateAdmin(ctx, principal.PlatformUser.ID.String(), uuid.UUID(request.Body.EnterpriseId), request.Body.Username, request.Body.DisplayName, email, request.Params.IdempotencyKey)
	if err != nil {
		return platformapi.CreateEnterpriseAdmindefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return platformapi.CreateEnterpriseAdmin201JSONResponse(toCreatedCredential(created)), nil
}

func (handler PlatformHandler) ResetEnterpriseAdminPassword(ctx context.Context, request platformapi.ResetEnterpriseAdminPasswordRequestObject) (platformapi.ResetEnterpriseAdminPasswordResponseObject, error) {
	principal, response := handler.platformPrincipal(ctx, true, request.Params.XCSRFToken)
	if response != nil {
		return platformapi.ResetEnterpriseAdminPassworddefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	created, err := handler.Enterprise.ResetAdminPassword(ctx, principal.PlatformUser.ID.String(), uuid.UUID(request.Id), request.Params.IdempotencyKey)
	if err != nil {
		return platformapi.ResetEnterpriseAdminPassworddefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusNotFound}, nil
	}
	return platformapi.ResetEnterpriseAdminPassword200JSONResponse(toCreatedCredential(created)), nil
}

func (handler PlatformHandler) DisableEnterpriseAdmin(ctx context.Context, request platformapi.DisableEnterpriseAdminRequestObject) (platformapi.DisableEnterpriseAdminResponseObject, error) {
	principal, response := handler.platformPrincipal(ctx, true, request.Params.XCSRFToken)
	if response != nil {
		return platformapi.DisableEnterpriseAdmindefaultJSONResponse{Body: *response, StatusCode: http.StatusUnauthorized}, nil
	}
	user, err := handler.Enterprise.DisableAdmin(ctx, principal.PlatformUser.ID.String(), uuid.UUID(request.Id), request.Params.ExpectedVersion)
	if err != nil {
		return platformapi.DisableEnterpriseAdmindefaultJSONResponse{Body: platformError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return platformapi.DisableEnterpriseAdmin200JSONResponse(toPlatformEnterpriseUser(user)), nil
}

func (handler PlatformHandler) platformPrincipal(ctx context.Context, mutation bool, csrf string) (identity.Principal, *platformapi.ApiError) {
	principal, err := handler.Auth.authenticate(ctx, "platform", mutation, csrf)
	if err == nil && principal.PlatformUser != nil {
		return principal, nil
	}
	value := platformError(ctx, err)
	return identity.Principal{}, &value
}

func toPlatformEnterprise(value db.Enterprise) platformapi.Enterprise {
	result := platformapi.Enterprise{Id: value.ID.String(), Name: value.Name, Code: value.Code, Status: platformapi.EnterpriseStatus(value.Status),
		Timezone: value.Timezone, DefaultLocale: platformapi.EnterpriseDefaultLocale(value.DefaultLocale), Version: value.Version,
		CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.Remark != "" {
		result.Remark = &value.Remark
	}
	return result
}

func toPlatformEnterpriseUser(user db.EnterpriseUser) platformapi.EnterpriseUser {
	result := platformapi.EnterpriseUser{Id: user.ID.String(), EnterpriseId: user.EnterpriseID.String(), DepartmentId: user.DepartmentID.String(),
		Username: user.Username, DisplayName: user.DisplayName, Status: platformapi.EnterpriseUserStatus(user.Status), MfaEnabled: user.MfaEnabled,
		AuthorizationVersion: user.AuthorizationVersion, Version: user.Version, CreatedAt: user.CreatedAt.Time, UpdatedAt: user.UpdatedAt.Time}
	if user.Email.Valid {
		email := openapi_types.Email(user.Email.String)
		result.Email = &email
	}
	if user.LastLoginAt.Valid {
		result.LastLoginAt = &user.LastLoginAt.Time
	}
	return result
}

func toCreatedCredential(value platform.CreatedCredential) platformapi.CreatedUserCredential {
	password := value.TemporaryPassword
	return platformapi.CreatedUserCredential{User: toPlatformEnterpriseUser(value.User), TemporaryPassword: &password, ExpiresAt: value.ExpiresAt}
}

func emptyPlatformPage() platformapi.CursorPage {
	return platformapi.CursorPage{NextCursor: nil, HasMore: false, Partial: platformapi.PartialMetadata{Partial: false, Reasons: []platformapi.PartialMetadataReasons{}}}
}

func platformError(ctx context.Context, err error) platformapi.ApiError {
	base := setupError(ctx, err)
	return platformapi.ApiError{Code: base.Code, MessageKey: base.MessageKey, RequestId: base.RequestId, Retryable: base.Retryable}
}
