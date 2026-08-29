package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	machineapi "github.com/kakj-go/Argus/internal/gen/openapi/machine"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type MachineHandler struct {
	Identity EnterpriseIdentityHandler
	Service  identity.MachineService
	Cursor   pagination.Signer
}

func (handler MachineHandler) ListServiceAccounts(ctx context.Context, request machineapi.ListServiceAccountsRequestObject) (machineapi.ListServiceAccountsResponseObject, error) {
	p, e := handler.auth(ctx, false, "", "service_account.read")
	if e != nil {
		return machineapi.ListServiceAccountsdefaultJSONResponse{Body: *e, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListServiceAccounts(p.Context(ctx), p.EnterpriseIDValue())
	if err != nil {
		return machineapi.ListServiceAccountsdefaultJSONResponse{Body: machineError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(p, nil, "created_at_asc"), func(value identity.ServiceAccountRecord) pageKey {
		return pageKey{Time: value.Account.CreatedAt.Time, ID: value.Account.ID.String()}
	})
	if err != nil {
		body, status := machinePaginationError(ctx, err)
		return machineapi.ListServiceAccountsdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	converted := make([]machineapi.ServiceAccount, 0, len(items))
	for _, v := range items {
		converted = append(converted, toMachineServiceAccount(v))
	}
	return machineapi.ListServiceAccounts200JSONResponse{Items: converted, Page: machineapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyMachinePage().Partial}}, nil
}

func (handler MachineHandler) CreateServiceAccount(ctx context.Context, request machineapi.CreateServiceAccountRequestObject) (machineapi.CreateServiceAccountResponseObject, error) {
	p, e := handler.auth(ctx, true, request.Params.XCSRFToken, "service_account.manage")
	if e != nil {
		return machineapi.CreateServiceAccountdefaultJSONResponse{Body: *e, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return machineapi.CreateServiceAccountdefaultJSONResponse{Body: machineError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	description := ""
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	result, err := handler.Service.CreateServiceAccount(p.Context(ctx), p.ActorID(), p.EnterpriseIDValue(), identity.ServiceAccountInput{Name: request.Body.Name, Description: description, AllowedToolIDs: request.Body.AllowedToolIds}, request.Params.IdempotencyKey)
	if err != nil {
		return machineapi.CreateServiceAccountdefaultJSONResponse{Body: machineError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return machineapi.CreateServiceAccount201JSONResponse(toMachineServiceAccount(result)), nil
}

func (handler MachineHandler) UpdateServiceAccount(ctx context.Context, request machineapi.UpdateServiceAccountRequestObject) (machineapi.UpdateServiceAccountResponseObject, error) {
	p, e := handler.auth(ctx, true, request.Params.XCSRFToken, "service_account.manage")
	if e != nil {
		return machineapi.UpdateServiceAccountdefaultJSONResponse{Body: *e, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return machineapi.UpdateServiceAccountdefaultJSONResponse{Body: machineError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	description := ""
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	var tools []string
	if request.Body.AllowedToolIds != nil {
		tools = append([]string(nil), (*request.Body.AllowedToolIds)...)
	}
	var status *string
	if request.Body.Status != nil {
		v := string(*request.Body.Status)
		status = &v
	}
	result, err := handler.Service.UpdateServiceAccount(p.Context(ctx), p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), identity.ServiceAccountInput{Description: description, AllowedToolIDs: tools, Status: status, ExpectedVersion: request.Body.ExpectedVersion})
	if err != nil {
		return machineapi.UpdateServiceAccountdefaultJSONResponse{Body: machineError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return machineapi.UpdateServiceAccount200JSONResponse(toMachineServiceAccount(result)), nil
}

func (handler MachineHandler) ListApiKeys(ctx context.Context, request machineapi.ListApiKeysRequestObject) (machineapi.ListApiKeysResponseObject, error) {
	p, e := handler.auth(ctx, false, "", "service_account.read")
	if e != nil {
		return machineapi.ListApiKeysdefaultJSONResponse{Body: *e, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListAPIKeys(p.Context(ctx), p.EnterpriseIDValue(), uuid.UUID(request.ServiceAccountId))
	if err != nil {
		return machineapi.ListApiKeysdefaultJSONResponse{Body: machineError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	items, next, hasMore, err := paginate(handler.Cursor, items, cursorValue(request.Params.Cursor), listLimit(request.Params.Limit), enterpriseCursorBinding(p, map[string]any{"service_account_id": request.ServiceAccountId.String()}, "created_at_asc"), func(value db.ApiKey) pageKey {
		return pageKey{Time: value.CreatedAt.Time, ID: value.ID.String()}
	})
	if err != nil {
		body, status := machinePaginationError(ctx, err)
		return machineapi.ListApiKeysdefaultJSONResponse{Body: body, StatusCode: status}, nil
	}
	converted := make([]machineapi.ApiKey, 0, len(items))
	for _, v := range items {
		converted = append(converted, toMachineAPIKey(v))
	}
	return machineapi.ListApiKeys200JSONResponse{Items: converted, Page: machineapi.CursorPage{NextCursor: next, HasMore: hasMore, Partial: emptyMachinePage().Partial}}, nil
}

func (handler MachineHandler) CreateApiKey(ctx context.Context, request machineapi.CreateApiKeyRequestObject) (machineapi.CreateApiKeyResponseObject, error) {
	p, e := handler.auth(ctx, true, request.Params.XCSRFToken, "service_account.manage")
	if e != nil {
		return machineapi.CreateApiKeydefaultJSONResponse{Body: *e, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return machineapi.CreateApiKeydefaultJSONResponse{Body: machineError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	created, err := handler.Service.CreateAPIKey(p.Context(ctx), p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.ServiceAccountId), request.Body.Name, request.Body.ExpiresAt, request.Params.IdempotencyKey)
	if err != nil {
		return machineapi.CreateApiKeydefaultJSONResponse{Body: machineError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	secret := created.Secret
	return machineapi.CreateApiKey201JSONResponse{ApiKey: toMachineAPIKey(created.Key), Secret: &secret}, nil
}

func (handler MachineHandler) RotateApiKey(ctx context.Context, request machineapi.RotateApiKeyRequestObject) (machineapi.RotateApiKeyResponseObject, error) {
	p, e := handler.auth(ctx, true, request.Params.XCSRFToken, "service_account.manage")
	if e != nil {
		return machineapi.RotateApiKeydefaultJSONResponse{Body: *e, StatusCode: http.StatusForbidden}, nil
	}
	created, err := handler.Service.RotateAPIKey(p.Context(ctx), p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Params.ExpectedVersion, request.Params.IdempotencyKey)
	if err != nil {
		return machineapi.RotateApiKeydefaultJSONResponse{Body: machineError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	secret := created.Secret
	return machineapi.RotateApiKey200JSONResponse{ApiKey: toMachineAPIKey(created.Key), Secret: &secret}, nil
}

func (handler MachineHandler) RevokeApiKey(ctx context.Context, request machineapi.RevokeApiKeyRequestObject) (machineapi.RevokeApiKeyResponseObject, error) {
	p, e := handler.auth(ctx, true, request.Params.XCSRFToken, "service_account.manage")
	if e != nil {
		return machineapi.RevokeApiKeydefaultJSONResponse{Body: *e, StatusCode: http.StatusForbidden}, nil
	}
	if err := handler.Service.RevokeAPIKey(p.Context(ctx), p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Params.ExpectedVersion); err != nil {
		return machineapi.RevokeApiKeydefaultJSONResponse{Body: machineError(ctx, err), StatusCode: http.StatusConflict}, nil
	}
	return machineapi.RevokeApiKey204Response{}, nil
}

func (handler MachineHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *machineapi.ApiError) {
	p, e := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if e == nil {
		return p, nil
	}
	v := machineapi.ApiError{Code: e.Code, MessageKey: e.MessageKey, RequestId: e.RequestId, Retryable: e.Retryable}
	return identity.Principal{}, &v
}
func toMachineServiceAccount(value identity.ServiceAccountRecord) machineapi.ServiceAccount {
	tools := append([]string(nil), value.Account.AllowedToolIds...)
	result := machineapi.ServiceAccount{Id: value.Account.ID.String(), EnterpriseId: value.Account.EnterpriseID.String(), Name: value.Account.Name, AllowedToolIds: &tools, Status: machineapi.ServiceAccountStatus(value.Account.Status), AuthorizationVersion: value.Account.AuthorizationVersion, Version: value.Account.Version, CreatedAt: value.Account.CreatedAt.Time, UpdatedAt: value.Account.UpdatedAt.Time}
	if value.Account.Description != "" {
		result.Description = &value.Account.Description
	}
	return result
}
func toMachineAPIKey(value db.ApiKey) machineapi.ApiKey {
	result := machineapi.ApiKey{Id: value.ID.String(), EnterpriseId: value.EnterpriseID.String(), ServiceAccountId: value.ServiceAccountID.String(), Name: value.Name, Prefix: value.Prefix, Status: machineapi.ApiKeyStatus(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.Time}
	if value.ExpiresAt.Valid {
		result.ExpiresAt = &value.ExpiresAt.Time
	}
	if value.LastUsedAt.Valid {
		result.LastUsedAt = &value.LastUsedAt.Time
	}
	return result
}
func emptyMachinePage() machineapi.CursorPage {
	return machineapi.CursorPage{NextCursor: nil, HasMore: false, Partial: machineapi.PartialMetadata{Partial: false, Reasons: []machineapi.PartialMetadataReasons{}}}
}
func machineError(ctx context.Context, err error) machineapi.ApiError {
	base := setupErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	if errors.Is(err, postgres.ErrIdempotencyConflict) {
		base.Code, base.MessageKey = "IDEMPOTENCY_CONFLICT", "errors.common.idempotency_conflict"
	}
	if errors.Is(err, postgres.ErrIdempotencyExpired) {
		base.Code, base.MessageKey = "IDEMPOTENCY_RESULT_EXPIRED", "errors.common.idempotency_result_expired"
	}
	return machineapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]machineapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func machinePaginationError(ctx context.Context, err error) (machineapi.ApiError, int) {
	code, key, status := paginationError(err)
	logMappedError(ctx, code, err)
	return machineapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx), Retryable: retryablePointer(code == "CURSOR_EXPIRED")}, status
}
