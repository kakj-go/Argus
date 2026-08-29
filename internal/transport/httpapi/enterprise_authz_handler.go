package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/authorization"
	authzapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseauthz"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
)

type EnterpriseAuthorizationHandler struct {
	Identity EnterpriseIdentityHandler
	Service  authorization.Service
	Cursor   pagination.Signer
}

func (handler EnterpriseAuthorizationHandler) ListDataAuthorizationResources(ctx context.Context, request authzapi.ListDataAuthorizationResourcesRequestObject) (authzapi.ListDataAuthorizationResourcesResponseObject, error) {
	principal, apiError := handler.auth(ctx, false, "", "data_authorization.read")
	if apiError != nil {
		return authzapi.ListDataAuthorizationResourcesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	subjectType := string(request.SubjectType)
	resourceType := string(request.Params.ResourceType)
	if !validDataAuthorizationSubjectType(subjectType) || !validDataAuthorizationResourceType(resourceType) {
		return authzapi.ListDataAuthorizationResourcesdefaultJSONResponse{Body: authzError(ctx, errors.New("invalid data authorization target")), StatusCode: http.StatusBadRequest}, nil
	}
	items, err := handler.Service.ListGrantResources(principal.Context(ctx), principal.EnterpriseIDValue(), uuid.UUID(request.SubjectId), subjectType, resourceType)
	if err != nil {
		return authzapi.ListDataAuthorizationResourcesdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	filter := map[string]any{"subject_type": subjectType, "subject_id": request.SubjectId.String(), "resource_type": resourceType}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(principal, filter, "resource_id_asc"), func(value authorization.GrantResource) pageKey {
		return pageKey{ID: value.ResourceID.String()}
	})
	if err != nil {
		body, status := authzPaginationError(ctx, err)
		return authzapi.ListDataAuthorizationResourcesdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	version, err := handler.Service.CurrentAuthorizationVersion(principal.Context(ctx), principal.EnterpriseIDValue(), uuid.UUID(request.SubjectId), subjectType)
	if err != nil {
		return authzapi.ListDataAuthorizationResourcesdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	affected, err := handler.Service.AffectedMemberCount(principal.Context(ctx), principal.EnterpriseIDValue(), uuid.UUID(request.SubjectId), subjectType)
	if err != nil {
		return authzapi.ListDataAuthorizationResourcesdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	converted := make([]authzapi.DataAuthorizationResource, 0, len(items))
	for _, item := range items {
		converted = append(converted, authzapi.DataAuthorizationResource{
			ResourceType: authzapi.DataAuthorizationResourceType(item.ResourceType),
			ResourceId:   authzapi.ResourceId(item.ResourceID),
			Name:         item.Name, Direct: item.Direct, Inherited: item.Inherited, Sources: item.Sources,
		})
	}
	return authzapi.ListDataAuthorizationResources200JSONResponse{Items: converted, Page: authzapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyAuthzPage().Partial}, AuthorizationVersion: version, AffectedMemberCount: affected}, nil
}

func (handler EnterpriseAuthorizationHandler) UpdateDataAuthorization(ctx context.Context, request authzapi.UpdateDataAuthorizationRequestObject) (authzapi.UpdateDataAuthorizationResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, string(request.Params.XCSRFToken), "data_authorization.manage")
	if apiError != nil {
		return authzapi.UpdateDataAuthorizationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.UpdateDataAuthorizationdefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	subjectType := string(request.SubjectType)
	resourceType := string(request.Body.ResourceType)
	if !validDataAuthorizationSubjectType(subjectType) || !validDataAuthorizationResourceType(resourceType) {
		return authzapi.UpdateDataAuthorizationdefaultJSONResponse{Body: authzError(ctx, errors.New("invalid data authorization target")), StatusCode: http.StatusBadRequest}, nil
	}
	if request.Body.ExpectedVersion < 1 || len(request.Body.ResourceIds) == 0 {
		return authzapi.UpdateDataAuthorizationdefaultJSONResponse{Body: authzError(ctx, errors.New("expected_version and resource_ids are required")), StatusCode: http.StatusBadRequest}, nil
	}
	ids := make([]uuid.UUID, 0, len(request.Body.ResourceIds))
	for _, id := range request.Body.ResourceIds {
		ids = append(ids, uuid.UUID(id))
	}
	err := handler.Service.UpdateGrantBatch(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), authorization.GrantBatchInput{
		SubjectType: subjectType, SubjectID: uuid.UUID(request.SubjectId), ResourceType: resourceType, ResourceIDs: ids, Remove: request.Body.Remove, ExpectedVersion: request.Body.ExpectedVersion,
	})
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, authorization.ErrInvalidResource) {
			status = http.StatusUnprocessableEntity
		}
		return authzapi.UpdateDataAuthorizationdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: status}, nil
	}
	return authzapi.UpdateDataAuthorization204Response{}, nil
}

func validDataAuthorizationSubjectType(value string) bool {
	switch value {
	case "user", "department", "role", "service_account":
		return true
	default:
		return false
	}
}

func validDataAuthorizationResourceType(value string) bool {
	return value == "host" || value == "kubernetes_cluster"
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
	result, err := handler.Service.CreateBinding(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), authorization.BindingInput{SubjectType: string(request.Body.SubjectType), SubjectID: uuid.UUID(request.Body.SubjectId), RoleID: uuid.UUID(request.Body.RoleId), ValidFrom: request.Body.ValidFrom, ValidUntil: request.Body.ValidUntil}, request.Params.IdempotencyKey)
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
	var status *string
	if request.Body.Status != nil {
		v := string(*request.Body.Status)
		status = &v
	}
	result, err := handler.Service.UpdateBinding(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), authorization.BindingInput{ValidFrom: request.Body.ValidFrom, ValidUntil: request.Body.ValidUntil, SetValidFrom: true, SetValidUntil: true, Status: status, ExpectedVersion: request.Body.ExpectedVersion})
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

func (handler EnterpriseAuthorizationHandler) GetUserRoleAssignments(ctx context.Context, request authzapi.GetUserRoleAssignmentsRequestObject) (authzapi.GetUserRoleAssignmentsResponseObject, error) {
	principal, apiError := handler.auth(ctx, false, "", "role.read")
	if apiError != nil {
		return authzapi.GetUserRoleAssignmentsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	result, err := handler.Service.GetUserRoleAssignments(principal.Context(ctx), principal.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return authzapi.GetUserRoleAssignmentsdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: http.StatusNotFound}, nil
	}
	return authzapi.GetUserRoleAssignments200JSONResponse(toUserRoleAssignments(result)), nil
}

func (handler EnterpriseAuthorizationHandler) ReplaceUserRoleAssignments(ctx context.Context, request authzapi.ReplaceUserRoleAssignmentsRequestObject) (authzapi.ReplaceUserRoleAssignmentsResponseObject, error) {
	principal, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "role.manage")
	if apiError != nil {
		return authzapi.ReplaceUserRoleAssignmentsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if !hasAllPermissions(principal, "identity.manage", "role.manage") {
		return authzapi.ReplaceUserRoleAssignmentsdefaultJSONResponse{Body: authzError(ctx, errors.New("authorization denied")), StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return authzapi.ReplaceUserRoleAssignmentsdefaultJSONResponse{Body: authzError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	roleIDs := make([]uuid.UUID, 0, len(request.Body.RoleIds))
	for _, roleID := range request.Body.RoleIds {
		roleIDs = append(roleIDs, uuid.UUID(roleID))
	}
	result, err := handler.Service.ReplaceUserRoleAssignments(principal.Context(ctx), principal.ActorID(), principal.EnterpriseIDValue(), uuid.UUID(request.Id), authorization.UserRoleAssignmentsUpdate{
		DepartmentID: uuid.UUID(request.Body.DepartmentId), RoleIDs: roleIDs, ExpectedUserVersion: request.Body.ExpectedUserVersion, ExpectedAuthorizationVersion: request.Body.ExpectedAuthorizationVersion,
	}, request.Params.IdempotencyKey)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, authorization.ErrInvalidRoleAssignment) {
			status = http.StatusUnprocessableEntity
		}
		return authzapi.ReplaceUserRoleAssignmentsdefaultJSONResponse{Body: authzError(ctx, err), StatusCode: status}, nil
	}
	return authzapi.ReplaceUserRoleAssignments200JSONResponse(toUserRoleAssignments(result)), nil
}

func (handler EnterpriseAuthorizationHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *authzapi.ApiError) {
	p, e := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if e == nil {
		return p, nil
	}
	v := authzapi.ApiError{Code: e.Code, MessageKey: e.MessageKey, RequestId: e.RequestId, Retryable: e.Retryable}
	return identity.Principal{}, &v
}

func hasAllPermissions(principal identity.Principal, permissions ...string) bool {
	if slices.Contains(principal.Permissions, "*") {
		return true
	}
	for _, permission := range permissions {
		if !slices.Contains(principal.Permissions, permission) {
			return false
		}
	}
	return true
}

func toAuthzRole(value authorization.RoleRecord) authzapi.Role {
	permissions := make([]authzapi.Permission, len(value.Permissions))
	copy(permissions, value.Permissions)
	r := authzapi.Role{Id: value.Role.ID.String(), EnterpriseId: value.Role.EnterpriseID.String(), Name: value.Role.Name, Builtin: value.Role.Builtin, Permissions: permissions, Status: authzapi.RoleStatus(value.Role.Status), Version: value.Role.Version, CreatedAt: value.Role.CreatedAt.Time, UpdatedAt: value.Role.UpdatedAt.Time}
	if value.Role.IdentityKey.Valid {
		r.BuiltinKey = &value.Role.IdentityKey.String
	}
	if value.Role.Description != "" {
		r.Description = &value.Role.Description
	}
	return r
}
func toAuthzBinding(value authorization.BindingRecord) authzapi.RoleBinding {
	r := authzapi.RoleBinding{Id: value.Binding.ID.String(), EnterpriseId: value.Binding.EnterpriseID.String(), SubjectType: authzapi.RoleBindingSubjectType(value.Binding.SubjectType), SubjectId: value.Binding.SubjectID.String(), RoleId: value.Binding.RoleID.String(), Status: authzapi.RoleBindingStatus(value.Binding.Status), Version: value.Binding.Version, CreatedAt: value.Binding.CreatedAt.Time, UpdatedAt: value.Binding.UpdatedAt.Time}
	if value.Binding.ValidFrom.Valid {
		r.ValidFrom = &value.Binding.ValidFrom.Time
	}
	if value.Binding.ValidUntil.Valid {
		r.ValidUntil = &value.Binding.ValidUntil.Time
	}
	return r
}

func toUserRoleAssignments(value authorization.UserRoleAssignments) authzapi.UserRoleAssignments {
	direct := make([]uuid.UUID, len(value.DirectRoleIDs))
	copy(direct, value.DirectRoleIDs)
	effective := make([]uuid.UUID, len(value.EffectiveRoleIDs))
	copy(effective, value.EffectiveRoleIDs)
	inherited := make([]authzapi.InheritedRoleAssignment, 0, len(value.InheritedRoles))
	for _, item := range value.InheritedRoles {
		inherited = append(inherited, authzapi.InheritedRoleAssignment{RoleId: item.RoleID, SourceId: item.SourceID, SourceName: item.SourceName, SourceType: authzapi.InheritedRoleAssignmentSourceTypeDepartment})
	}
	return authzapi.UserRoleAssignments{DirectRoleIds: direct, InheritedRoles: inherited, EffectiveRoleIds: effective, AuthorizationVersion: value.AuthorizationVersion}
}
func emptyAuthzPage() authzapi.CursorPage {
	return authzapi.CursorPage{NextCursor: nil, HasMore: false, Partial: authzapi.PartialMetadata{Partial: false, Reasons: []authzapi.PartialMetadataReasons{}}}
}
func authzError(ctx context.Context, err error) authzapi.ApiError {
	base := setupErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	if errors.Is(err, authorization.ErrBuiltinRoleImmutable) {
		base.Code, base.MessageKey = "BUILTIN_ROLE_IMMUTABLE", "errors.role.builtin_immutable"
	} else if errors.Is(err, authorization.ErrAuthorizationConflict) {
		base.Code, base.MessageKey = "AUTHORIZATION_VERSION_STALE", "errors.auth.authorization_version_stale"
	} else if errors.Is(err, authorization.ErrLastIAMAdminRequired) {
		base.Code, base.MessageKey = "LAST_IAM_ADMIN_REQUIRED", "errors.role.last_iam_admin_required"
	} else if errors.Is(err, authorization.ErrInvalidRoleAssignment) {
		base.Code, base.MessageKey = "INVALID_ARGUMENT", "errors.common.invalid_argument"
	}
	return authzapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]authzapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func authzPaginationError(ctx context.Context, err error) (authzapi.ApiError, int) {
	code, key, status := paginationError(err)
	logMappedError(ctx, code, err)
	return authzapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, status
}
