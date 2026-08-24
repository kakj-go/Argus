package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	enterpriseapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseidentity"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type EnterpriseIdentityHandler struct {
	Auth    SetupHandler
	Service identity.EnterpriseService
	Machine identity.MachineService
	Cursor  pagination.Signer
}

func (handler EnterpriseIdentityHandler) ListEnterpriseUsers(ctx context.Context, request enterpriseapi.ListEnterpriseUsersRequestObject) (enterpriseapi.ListEnterpriseUsersResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, false, "", "identity.read")
	if apiError != nil {
		return enterpriseapi.ListEnterpriseUsersdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	users, err := handler.Service.ListUsers(principal.Context(ctx), principal.EnterpriseIDValue())
	if err != nil {
		return enterpriseapi.ListEnterpriseUsersdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	users, next, hasMore, err := paginate(handler.Cursor, users, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "created_at_asc"), func(value db.EnterpriseUser) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := enterpriseIdentityPaginationError(ctx, err)
		return enterpriseapi.ListEnterpriseUsersdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	items := make([]enterpriseapi.EnterpriseUser, 0, len(users))
	for _, user := range users {
		items = append(items, toEnterpriseIdentityUser(user))
	}
	return enterpriseapi.ListEnterpriseUsers200JSONResponse{Items: items, Page: enterpriseapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyEnterpriseIdentityPage().Partial}}, nil
}

func (handler EnterpriseIdentityHandler) GetEnterpriseUser(ctx context.Context, request enterpriseapi.GetEnterpriseUserRequestObject) (enterpriseapi.GetEnterpriseUserResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, false, "", "identity.read")
	if apiError != nil {
		return enterpriseapi.GetEnterpriseUserdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	user, err := handler.Service.GetUser(principal.Context(ctx), principal.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return enterpriseapi.GetEnterpriseUserdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusNotFound}, nil
	}
	return enterpriseapi.GetEnterpriseUser200JSONResponse(toEnterpriseIdentityUser(user)), nil
}

func (handler EnterpriseIdentityHandler) CreateEnterpriseUser(ctx context.Context, request enterpriseapi.CreateEnterpriseUserRequestObject) (enterpriseapi.CreateEnterpriseUserResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, true, request.Params.XCSRFToken, "identity.manage")
	if apiError != nil {
		return enterpriseapi.CreateEnterpriseUserdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return enterpriseapi.CreateEnterpriseUserdefaultJSONResponse{Body: enterpriseIdentityError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	email := ""
	if request.Body.Email != nil {
		email = string(*request.Body.Email)
	}
	var roleIDs []uuid.UUID
	if request.Body.RoleIds != nil {
		roleIDs = make([]uuid.UUID, len(*request.Body.RoleIds))
		for index, roleID := range *request.Body.RoleIds {
			roleIDs[index] = uuid.UUID(roleID)
		}
	}
	created, err := handler.Service.CreateUser(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(),
		request.Body.Username, request.Body.DisplayName, email, uuid.UUID(request.Body.DepartmentId), roleIDs, request.Params.IdempotencyKey)
	if err != nil {
		return enterpriseapi.CreateEnterpriseUserdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	password := created.TemporaryPassword
	return enterpriseapi.CreateEnterpriseUser201JSONResponse{User: toEnterpriseIdentityUser(created.User), TemporaryPassword: &password, ExpiresAt: created.ExpiresAt}, nil
}

func (handler EnterpriseIdentityHandler) UpdateEnterpriseUser(ctx context.Context, request enterpriseapi.UpdateEnterpriseUserRequestObject) (enterpriseapi.UpdateEnterpriseUserResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, true, request.Params.XCSRFToken, "identity.manage")
	if apiError != nil {
		return enterpriseapi.UpdateEnterpriseUserdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return enterpriseapi.UpdateEnterpriseUserdefaultJSONResponse{Body: enterpriseIdentityError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	var email *string
	setEmail := request.Body.Email != nil
	if request.Body.Email != nil {
		value := string(*request.Body.Email)
		email = &value
	}
	var departmentID *uuid.UUID
	if request.Body.DepartmentId != nil {
		value := uuid.UUID(*request.Body.DepartmentId)
		departmentID = &value
	}
	var status *string
	if request.Body.Status != nil {
		value := string(*request.Body.Status)
		status = &value
	}
	user, err := handler.Service.UpdateUser(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), identity.EnterpriseUserUpdate{
		DisplayName: request.Body.DisplayName, Email: email, SetEmail: setEmail, DepartmentID: departmentID, Status: status, ExpectedVersion: request.Body.ExpectedVersion,
	})
	if err != nil {
		return enterpriseapi.UpdateEnterpriseUserdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return enterpriseapi.UpdateEnterpriseUser200JSONResponse(toEnterpriseIdentityUser(user)), nil
}

func (handler EnterpriseIdentityHandler) ListDepartments(ctx context.Context, request enterpriseapi.ListDepartmentsRequestObject) (enterpriseapi.ListDepartmentsResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, false, "", "department.read")
	if apiError != nil {
		return enterpriseapi.ListDepartmentsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	departments, err := handler.Service.ListDepartments(principal.Context(ctx), principal.EnterpriseIDValue())
	if err != nil {
		return enterpriseapi.ListDepartmentsdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	departments, next, hasMore, err := paginate(handler.Cursor, departments, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "created_at_asc"), func(value db.Department) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := enterpriseIdentityPaginationError(ctx, err)
		return enterpriseapi.ListDepartmentsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	items := make([]enterpriseapi.Department, 0, len(departments))
	for _, department := range departments {
		items = append(items, toEnterpriseDepartment(department))
	}
	return enterpriseapi.ListDepartments200JSONResponse{Items: items, Page: enterpriseapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyEnterpriseIdentityPage().Partial}}, nil
}

func (handler EnterpriseIdentityHandler) CreateDepartment(ctx context.Context, request enterpriseapi.CreateDepartmentRequestObject) (enterpriseapi.CreateDepartmentResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, true, request.Params.XCSRFToken, "department.manage")
	if apiError != nil {
		return enterpriseapi.CreateDepartmentdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return enterpriseapi.CreateDepartmentdefaultJSONResponse{Body: enterpriseIdentityError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	description := ""
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	department, err := handler.Service.CreateDepartment(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), request.Body.Name, description, request.Params.IdempotencyKey)
	if err != nil {
		return enterpriseapi.CreateDepartmentdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return enterpriseapi.CreateDepartment201JSONResponse(toEnterpriseDepartment(department)), nil
}

func (handler EnterpriseIdentityHandler) UpdateDepartment(ctx context.Context, request enterpriseapi.UpdateDepartmentRequestObject) (enterpriseapi.UpdateDepartmentResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, true, request.Params.XCSRFToken, "department.manage")
	if apiError != nil {
		return enterpriseapi.UpdateDepartmentdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return enterpriseapi.UpdateDepartmentdefaultJSONResponse{Body: enterpriseIdentityError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	var status *string
	if request.Body.Status != nil {
		value := string(*request.Body.Status)
		status = &value
	}
	department, err := handler.Service.UpdateDepartment(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), identity.DepartmentUpdate{
		Name: request.Body.Name, Description: request.Body.Description, Status: status, ExpectedVersion: request.Body.ExpectedVersion,
	})
	if err != nil {
		return enterpriseapi.UpdateDepartmentdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return enterpriseapi.UpdateDepartment200JSONResponse(toEnterpriseDepartment(department)), nil
}

func (handler EnterpriseIdentityHandler) DisableDepartment(ctx context.Context, request enterpriseapi.DisableDepartmentRequestObject) (enterpriseapi.DisableDepartmentResponseObject, error) {
	principal, apiError := handler.enterprisePrincipal(ctx, true, request.Params.XCSRFToken, "department.manage")
	if apiError != nil {
		return enterpriseapi.DisableDepartmentdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	status := "disabled"
	_, err := handler.Service.UpdateDepartment(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), identity.DepartmentUpdate{Status: &status, ExpectedVersion: request.Params.ExpectedVersion})
	if err != nil {
		return enterpriseapi.DisableDepartmentdefaultJSONResponse{Body: enterpriseIdentityError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return enterpriseapi.DisableDepartment204Response{}, nil
}

func (handler EnterpriseIdentityHandler) enterprisePrincipal(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *enterpriseapi.ApiError) {
	metadata, ok := RequestFromContext(ctx)
	if !ok {
		value := enterpriseIdentityError(ctx, identity.ErrSessionInvalid)
		return identity.Principal{}, &value
	}
	var principal identity.Principal
	var err error
	authorization := metadata.Request.Header.Get("Authorization")
	if authorization != "" {
		if metadata.Request.Header.Get("Cookie") != "" || metadata.Request.Header.Get("X-CSRF-Token") != "" || csrf != "" {
			err = identity.ErrAudienceMismatch
		} else if !strings.HasPrefix(authorization, "Bearer ") {
			err = identity.ErrInvalidCredentials
		} else {
			principal, err = handler.Machine.AuthenticateAPIKey(ctx, strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")))
		}
	} else {
		principal, err = handler.Auth.authenticate(ctx, "enterprise", mutation, csrf)
	}
	_, hasEnterprise := principal.EnterpriseID()
	if err == nil && hasEnterprise && (slices.Contains(principal.Permissions, "*") || slices.Contains(principal.Permissions, permission)) {
		return principal, nil
	}
	if err == nil {
		err = errors.New("authorization denied")
	}
	value := enterpriseIdentityError(ctx, err)
	return identity.Principal{}, &value
}

func toEnterpriseIdentityUser(user db.EnterpriseUser) enterpriseapi.EnterpriseUser {
	result := enterpriseapi.EnterpriseUser{Id: user.ID.String(), EnterpriseId: user.EnterpriseID.String(), DepartmentId: user.DepartmentID.String(), Username: user.Username,
		DisplayName: user.DisplayName, Status: enterpriseapi.EnterpriseUserStatus(user.Status), MfaEnabled: user.MfaEnabled, AuthorizationVersion: user.AuthorizationVersion,
		Version: user.Version, CreatedAt: user.CreatedAt.Time, UpdatedAt: user.UpdatedAt.Time}
	if user.Email.Valid {
		email := openapi_types.Email(user.Email.String)
		result.Email = &email
	}
	if user.LastLoginAt.Valid {
		result.LastLoginAt = &user.LastLoginAt.Time
	}
	return result
}

func toEnterpriseDepartment(value db.Department) enterpriseapi.Department {
	result := enterpriseapi.Department{Id: value.ID.String(), EnterpriseId: value.EnterpriseID.String(), Name: value.Name, IsDefault: value.IsDefault,
		Status: enterpriseapi.DepartmentStatus(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.Description != "" {
		result.Description = &value.Description
	}
	return result
}

func emptyEnterpriseIdentityPage() enterpriseapi.CursorPage {
	return enterpriseapi.CursorPage{NextCursor: nil, HasMore: false, Partial: enterpriseapi.PartialMetadata{Partial: false, Reasons: []enterpriseapi.PartialMetadataReasons{}}}
}

func enterpriseIdentityError(ctx context.Context, err error) enterpriseapi.ApiError {
	base := setupErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	if err != nil && err.Error() == "authorization denied" {
		base.Code, base.MessageKey = "AUTHORIZATION_DENIED", "errors.auth.authorization_denied"
	}
	if errors.Is(err, identity.ErrDefaultDepartment) {
		base.Code, base.MessageKey = "DEFAULT_DEPARTMENT_IMMUTABLE", "errors.department.default_immutable"
	}
	if errors.Is(err, identity.ErrDepartmentNotEmpty) {
		base.Code, base.MessageKey = "DEPARTMENT_NOT_EMPTY", "errors.department.not_empty"
	}
	return enterpriseapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]enterpriseapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func enterpriseIdentityPaginationError(ctx context.Context, err error) (enterpriseapi.ApiError, int) {
	code, key, status := paginationError(err)
	logMappedError(ctx, code, err)
	return enterpriseapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, status
}
