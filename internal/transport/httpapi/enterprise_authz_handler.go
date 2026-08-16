package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/authorization"
	authzapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseauthz"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type EnterpriseAuthorizationHandler struct {
	Identity EnterpriseIdentityHandler
	Service  authorization.Service
	Cursor   pagination.Signer
}

func (handler EnterpriseAuthorizationHandler) ListPermissions(ctx context.Context, request authzapi.ListPermissionsRequestObject) (authzapi.ListPermissionsResponseObject, error) {
	principal, apiError := handler.auth(ctx, false, "", "role.read")
	if apiError != nil {
		return authzapi.ListPermissionsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.Permissions(ctx)
	if err != nil {
		return authzapi.ListPermissionsdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "permission_asc"), func(value string) pageKey {
		return pageKey{Time: time.Unix(0, 0).UTC(), ID: value}
	})
	if err != nil {
		body, status := authzPaginationError(ctx, err)
		return authzapi.ListPermissionsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	permissions := make([]authzapi.Permission, len(items))
	copy(permissions, items)
	return authzapi.ListPermissions200JSONResponse{Items: permissions, Page: authzapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyAuthzPage().Partial}}, nil
}

func (handler EnterpriseAuthorizationHandler) ListRoles(ctx context.Context, request authzapi.ListRolesRequestObject) (authzapi.ListRolesResponseObject, error) {
	principal, apiError := handler.auth(ctx, false, "", "role.read")
	if apiError != nil {
		return authzapi.ListRolesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListRoles(principal.Context(ctx), principal.EnterpriseIDValue())
	if err != nil {
		return authzapi.ListRolesdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "builtin_desc_created_at_asc"), func(value authorization.RoleRecord) pageKey {
		return pageKey{Time: value.Role.CreatedAt.Time, ID: value.Role.ID.String()}
	})
	if err != nil {
		body, status := authzPaginationError(ctx, err)
		return authzapi.ListRolesdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	converted := make([]authzapi.Role, 0, len(items))
	for _, item := range items {
		converted = append(converted, toAuthzRole(item))
	}
	return authzapi.ListRoles200JSONResponse{Items: converted, Page: authzapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyAuthzPage().Partial}}, nil
}

func (handler EnterpriseAuthorizationHandler) CreateRole(ctx context.Context, request authzapi.CreateRoleRequestObject) (authzapi.CreateRoleResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "role.manage")
	if apiError != nil {
		return authzapi.CreateRoledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.CreateRoledefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	description := ""
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	permissions := make([]string, len(request.Body.Permissions))
	copy(permissions, request.Body.Permissions)
	result, err := handler.Service.CreateRole(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), authorization.RoleInput{Name: request.Body.Name, Description: description, Permissions: permissions}, request.Params.IdempotencyKey)
	if err != nil {
		return authzapi.CreateRoledefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.CreateRole201JSONResponse(toAuthzRole(result)), nil
}

func (handler EnterpriseAuthorizationHandler) UpdateRole(ctx context.Context, request authzapi.UpdateRoleRequestObject) (authzapi.UpdateRoleResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "role.manage")
	if apiError != nil {
		return authzapi.UpdateRoledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.UpdateRoledefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	name, description := "", ""
	if request.Body.Name != nil {
		name = *request.Body.Name
	}
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	var permissions []string
	if request.Body.Permissions != nil {
		permissions = append([]string(nil), (*request.Body.Permissions)...)
	}
	var status *string
	if request.Body.Status != nil {
		value := string(*request.Body.Status)
		status = &value
	}
	result, err := handler.Service.UpdateRole(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), authorization.RoleInput{Name: name, Description: description, Permissions: permissions, Status: status, ExpectedVersion: request.Body.ExpectedVersion})
	if err != nil {
		return authzapi.UpdateRoledefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.UpdateRole200JSONResponse(toAuthzRole(result)), nil
}

func (handler EnterpriseAuthorizationHandler) DisableRole(ctx context.Context, request authzapi.DisableRoleRequestObject) (authzapi.DisableRoleResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "role.manage")
	if apiError != nil {
		return authzapi.DisableRoledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	status := "disabled"
	_, err := handler.Service.UpdateRole(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), authorization.RoleInput{Status: &status, ExpectedVersion: request.Params.ExpectedVersion})
	if err != nil {
		return authzapi.DisableRoledefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.DisableRole204Response{}, nil
}

func (handler EnterpriseAuthorizationHandler) ListDataScopes(ctx context.Context, request authzapi.ListDataScopesRequestObject) (authzapi.ListDataScopesResponseObject, error) {
	principal, apiError := handler.auth(ctx, false, "", "data_scope.read")
	if apiError != nil {
		return authzapi.ListDataScopesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListScopes(principal.Context(ctx), principal.EnterpriseIDValue())
	if err != nil {
		return authzapi.ListDataScopesdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "created_at_asc"), func(value db.DataScope) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := authzPaginationError(ctx, err)
		return authzapi.ListDataScopesdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	converted := make([]authzapi.DataScope, 0, len(items))
	for _, item := range items {
		converted = append(converted, toAuthzScope(item))
	}
	return authzapi.ListDataScopes200JSONResponse{Items: converted, Page: authzapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyAuthzPage().Partial}}, nil
}

func (handler EnterpriseAuthorizationHandler) CreateDataScope(ctx context.Context, request authzapi.CreateDataScopeRequestObject) (authzapi.CreateDataScopeResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "data_scope.manage")
	if apiError != nil {
		return authzapi.CreateDataScopedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.CreateDataScopedefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input, err := scopeCreateInput(*request.Body)
	if err != nil {
		return authzapi.CreateDataScopedefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusUnprocessableEntity}, nil
	}
	value, err := handler.Service.CreateScope(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	if err != nil {
		return authzapi.CreateDataScopedefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusUnprocessableEntity}, nil
	}
	return authzapi.CreateDataScope201JSONResponse(toAuthzScope(value)), nil
}

func (handler EnterpriseAuthorizationHandler) UpdateDataScope(ctx context.Context, request authzapi.UpdateDataScopeRequestObject) (authzapi.UpdateDataScopeResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "data_scope.manage")
	if apiError != nil {
		return authzapi.UpdateDataScopedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.UpdateDataScopedefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	raw, _ := json.Marshal(request.Body.LabelSelector)
	if request.Body.LabelSelector == nil {
		raw = nil
	}
	resourceTypes := make([]string, len(request.Body.ResourceTypes))
	for i, v := range request.Body.ResourceTypes {
		resourceTypes[i] = string(v)
	}
	description := ""
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	var status *string
	if request.Body.Status != nil {
		v := string(*request.Body.Status)
		status = &v
	}
	value, err := handler.Service.UpdateScope(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), authorization.ScopeInput{Name: request.Body.Name, Description: description, ResourceTypes: resourceTypes, ExplicitResourceIDs: request.Body.ExplicitResourceIds, LabelSelector: raw, Status: status, ExpectedVersion: request.Body.ExpectedVersion})
	if err != nil {
		return authzapi.UpdateDataScopedefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.UpdateDataScope200JSONResponse(toAuthzScope(value)), nil
}

func (handler EnterpriseAuthorizationHandler) DisableDataScope(ctx context.Context, request authzapi.DisableDataScopeRequestObject) (authzapi.DisableDataScopeResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "data_scope.manage")
	if apiError != nil {
		return authzapi.DisableDataScopedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	current, err := handler.Service.ListScopes(principal.Context(ctx), principal.EnterpriseIDValue())
	if err != nil {
		return authzapi.DisableDataScopedefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	for _, scope := range current {
		if scope.ID == uuid.UUID(request.Id) {
			status := "disabled"
			_, err = handler.Service.UpdateScope(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), scope.ID, authorization.ScopeInput{Name: scope.Name, Description: scope.Description, ResourceTypes: scope.ResourceTypes, ExplicitResourceIDs: scope.ExplicitResourceIds, LabelSelector: scope.LabelSelector, Status: &status, ExpectedVersion: request.Params.ExpectedVersion})
			break
		}
	}
	if err != nil {
		return authzapi.DisableDataScopedefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.DisableDataScope204Response{}, nil
}

func (handler EnterpriseAuthorizationHandler) ListRoleBindings(ctx context.Context, request authzapi.ListRoleBindingsRequestObject) (authzapi.ListRoleBindingsResponseObject, error) {
	principal, apiError := handler.auth(ctx, false, "", "role.read")
	if apiError != nil {
		return authzapi.ListRoleBindingsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListBindings(principal.Context(ctx), principal.EnterpriseIDValue())
	if err != nil {
		return authzapi.ListRoleBindingsdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, nil, "created_at_asc"), func(value authorization.BindingRecord) pageKey {
		return pageKey{Time: value.Binding.CreatedAt.Time, ID: value.Binding.ID.String()}
	})
	if err != nil {
		body, status := authzPaginationError(ctx, err)
		return authzapi.ListRoleBindingsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	converted := make([]authzapi.RoleBinding, 0, len(items))
	for _, item := range items {
		converted = append(converted, toAuthzBinding(item))
	}
	return authzapi.ListRoleBindings200JSONResponse{Items: converted, Page: authzapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyAuthzPage().Partial}}, nil
}

func (handler EnterpriseAuthorizationHandler) CreateRoleBinding(ctx context.Context, request authzapi.CreateRoleBindingRequestObject) (authzapi.CreateRoleBindingResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "role.manage")
	if apiError != nil {
		return authzapi.CreateRoleBindingdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.CreateRoleBindingdefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	scopes := uuidSlice(request.Body.DataScopeIds)
	result, err := handler.Service.CreateBinding(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), authorization.BindingInput{SubjectType: string(request.Body.SubjectType), SubjectID: uuid.UUID(request.Body.SubjectId), RoleID: uuid.UUID(request.Body.RoleId), DataScopeIDs: scopes, ValidFrom: request.Body.ValidFrom, ValidUntil: request.Body.ValidUntil}, request.Params.IdempotencyKey)
	if err != nil {
		return authzapi.CreateRoleBindingdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.CreateRoleBinding201JSONResponse(toAuthzBinding(result)), nil
}

func (handler EnterpriseAuthorizationHandler) UpdateRoleBinding(ctx context.Context, request authzapi.UpdateRoleBindingRequestObject) (authzapi.UpdateRoleBindingResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "role.manage")
	if apiError != nil {
		return authzapi.UpdateRoleBindingdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.UpdateRoleBindingdefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	var scopes []uuid.UUID
	if request.Body.DataScopeIds != nil {
		scopes = uuidSlice(*request.Body.DataScopeIds)
	}
	var status *string
	if request.Body.Status != nil {
		v := string(*request.Body.Status)
		status = &v
	}
	result, err := handler.Service.UpdateBinding(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), authorization.BindingInput{DataScopeIDs: scopes, ValidFrom: request.Body.ValidFrom, ValidUntil: request.Body.ValidUntil, SetValidFrom: true, SetValidUntil: true, Status: status, ExpectedVersion: request.Body.ExpectedVersion})
	if err != nil {
		return authzapi.UpdateRoleBindingdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.UpdateRoleBinding200JSONResponse(toAuthzBinding(result)), nil
}

func (handler EnterpriseAuthorizationHandler) DisableRoleBinding(ctx context.Context, request authzapi.DisableRoleBindingRequestObject) (authzapi.DisableRoleBindingResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "role.manage")
	if apiError != nil {
		return authzapi.DisableRoleBindingdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	status := "disabled"
	_, err := handler.Service.UpdateBinding(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), authorization.BindingInput{Status: &status, ExpectedVersion: request.Params.ExpectedVersion})
	if err != nil {
		return authzapi.DisableRoleBindingdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return authzapi.DisableRoleBinding204Response{}, nil
}

func (handler EnterpriseAuthorizationHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *authzapi.ApiError) {
	p, e := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if e == nil {
		return p, nil
	}
	v := authzapi.ApiError{Code: e.Code, MessageKey: e.MessageKey, RequestId: e.RequestId, Retryable: e.Retryable}
	return identity.Principal{}, &v
}

func toAuthzRole(value authorization.RoleRecord) authzapi.Role {
	permissions := make([]authzapi.Permission, len(value.Permissions))
	copy(permissions, value.Permissions)
	r := authzapi.Role{Id: value.Role.ID.String(), EnterpriseId: value.Role.EnterpriseID.String(), Name: value.Role.Name, Builtin: value.Role.Builtin, Permissions: permissions, Status: authzapi.RoleStatus(value.Role.Status), Version: value.Role.Version, CreatedAt: value.Role.CreatedAt.Time, UpdatedAt: value.Role.UpdatedAt.Time}
	if value.Role.Description != "" {
		r.Description = &value.Role.Description
	}
	return r
}
func toAuthzScope(value db.DataScope) authzapi.DataScope {
	types := make([]authzapi.DataScopeResourceTypes, len(value.ResourceTypes))
	for i, v := range value.ResourceTypes {
		types[i] = authzapi.DataScopeResourceTypes(v)
	}
	r := authzapi.DataScope{Id: value.ID.String(), EnterpriseId: value.EnterpriseID.String(), Name: value.Name, ResourceTypes: types, ExplicitResourceIds: value.ExplicitResourceIds, Status: authzapi.DataScopeStatus(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.Description != "" {
		r.Description = &value.Description
	}
	if len(value.LabelSelector) > 0 {
		var selector authzapi.LabelSelector
		if json.Unmarshal(value.LabelSelector, &selector) == nil {
			r.LabelSelector = &selector
		}
	}
	return r
}
func toAuthzBinding(value authorization.BindingRecord) authzapi.RoleBinding {
	ids := make([]string, len(value.DataScopeIDs))
	for i, v := range value.DataScopeIDs {
		ids[i] = v.String()
	}
	r := authzapi.RoleBinding{Id: value.Binding.ID.String(), EnterpriseId: value.Binding.EnterpriseID.String(), SubjectType: authzapi.RoleBindingSubjectType(value.Binding.SubjectType), SubjectId: value.Binding.SubjectID.String(), RoleId: value.Binding.RoleID.String(), DataScopeIds: ids, Status: authzapi.RoleBindingStatus(value.Binding.Status), Version: value.Binding.Version, CreatedAt: value.Binding.CreatedAt.Time, UpdatedAt: value.Binding.UpdatedAt.Time}
	if value.Binding.ValidFrom.Valid {
		r.ValidFrom = &value.Binding.ValidFrom.Time
	}
	if value.Binding.ValidUntil.Valid {
		r.ValidUntil = &value.Binding.ValidUntil.Time
	}
	return r
}
func scopeCreateInput(body authzapi.DataScopeCreate) (authorization.ScopeInput, error) {
	raw, _ := json.Marshal(body.LabelSelector)
	if body.LabelSelector == nil {
		raw = nil
	}
	types := make([]string, len(body.ResourceTypes))
	for i, v := range body.ResourceTypes {
		types[i] = string(v)
	}
	description := ""
	if body.Description != nil {
		description = *body.Description
	}
	return authorization.ScopeInput{Name: body.Name, Description: description, ResourceTypes: types, ExplicitResourceIDs: body.ExplicitResourceIds, LabelSelector: raw}, nil
}
func uuidSlice(values []uuid.UUID) []uuid.UUID { return append([]uuid.UUID(nil), values...) }
func emptyAuthzPage() authzapi.CursorPage {
	return authzapi.CursorPage{NextCursor: nil, HasMore: false, Partial: authzapi.PartialMetadata{Partial: false, Reasons: []authzapi.PartialMetadataReasons{}}}
}
func authzError(ctx context.Context, err error) authzapi.ApiError {
	base := setupError(ctx, err)
	if errors.Is(err, authorization.ErrBuiltinRoleImmutable) {
		base.Code, base.MessageKey = "BUILTIN_ROLE_IMMUTABLE", "errors.role.builtin_immutable"
	}
	return authzapi.ApiError{Code: base.Code, MessageKey: base.MessageKey, RequestId: base.RequestId, Retryable: base.Retryable}
}

func authzPaginationError(ctx context.Context, err error) (authzapi.ApiError, int) {
	code, key, status := paginationError(err)
	return authzapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, status
}
