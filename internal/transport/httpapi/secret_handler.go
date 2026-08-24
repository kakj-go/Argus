package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	secretapi "github.com/kakj-go/Argus/internal/gen/openapi/secretapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type SecretHandler struct {
	Identity EnterpriseIdentityHandler
	Service  secret.Service
}

func (handler SecretHandler) ListSecrets(ctx context.Context, _ secretapi.ListSecretsRequestObject) (secretapi.ListSecretsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "secret.read")
	if apiError != nil {
		return secretapi.ListSecretsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.List(ctx, p.EnterpriseIDValue())
	if err != nil {
		return secretapi.ListSecretsdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	converted := make([]secretapi.Secret, 0, len(items))
	for _, item := range items {
		converted = append(converted, toSecret(item))
	}
	return secretapi.ListSecrets200JSONResponse{Items: converted, Page: emptySecretPage()}, nil
}

func (handler SecretHandler) CreateSecret(ctx context.Context, request secretapi.CreateSecretRequestObject) (secretapi.CreateSecretResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "secret.manage")
	if apiError != nil {
		return secretapi.CreateSecretdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil || request.Body.Value == nil {
		return secretapi.CreateSecretdefaultJSONResponse{Body: secretError(ctx, secret.ErrSecretValueRequired), StatusCode: http.StatusBadRequest}, nil
	}
	description := ""
	if request.Body.Description != nil {
		description = *request.Body.Description
	}
	created, err := handler.Service.Create(ctx, p.ActorID(), p.EnterpriseIDValue(), secret.SecretInput{Name: request.Body.Name, Type: string(request.Body.Type),
		Description: description, Value: *request.Body.Value}, request.Params.IdempotencyKey)
	if err != nil {
		return secretapi.CreateSecretdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.CreateSecret201JSONResponse(toSecret(created)), nil
}

func (handler SecretHandler) GetSecret(ctx context.Context, request secretapi.GetSecretRequestObject) (secretapi.GetSecretResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "secret.read")
	if apiError != nil {
		return secretapi.GetSecretdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Get(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return secretapi.GetSecretdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.GetSecret200JSONResponse(toSecret(value)), nil
}

func (handler SecretHandler) UpdateSecret(ctx context.Context, request secretapi.UpdateSecretRequestObject) (secretapi.UpdateSecretResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "secret.manage")
	if apiError != nil {
		return secretapi.UpdateSecretdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return secretapi.UpdateSecretdefaultJSONResponse{Body: secretError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := secret.SecretInput{ExpectedVersion: request.Body.ExpectedVersion}
	if request.Body.Name != nil {
		input.Name = *request.Body.Name
	}
	if request.Body.Description != nil {
		input.Description = *request.Body.Description
	}
	if request.Body.Status != nil {
		status := string(*request.Body.Status)
		input.Status = &status
	}
	value, err := handler.Service.Update(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), input)
	if err != nil {
		return secretapi.UpdateSecretdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.UpdateSecret200JSONResponse(toSecret(value)), nil
}

func (handler SecretHandler) DeleteSecret(ctx context.Context, request secretapi.DeleteSecretRequestObject) (secretapi.DeleteSecretResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "secret.manage")
	if apiError != nil {
		return secretapi.DeleteSecretdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if err := handler.Service.Disable(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Params.ExpectedVersion); err != nil {
		return secretapi.DeleteSecretdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.DeleteSecret204Response{}, nil
}

func (handler SecretHandler) RotateSecret(ctx context.Context, request secretapi.RotateSecretRequestObject) (secretapi.RotateSecretResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "secret.manage")
	if apiError != nil {
		return secretapi.RotateSecretdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil || request.Body.Value == nil {
		return secretapi.RotateSecretdefaultJSONResponse{Body: secretError(ctx, secret.ErrSecretValueRequired), StatusCode: http.StatusBadRequest}, nil
	}
	value, err := handler.Service.Rotate(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), secret.SecretInput{Value: *request.Body.Value,
		ExpectedVersion: request.Body.ExpectedVersion}, request.Params.IdempotencyKey)
	if err != nil {
		return secretapi.RotateSecretdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.RotateSecret200JSONResponse(toSecret(value)), nil
}

func (handler SecretHandler) ListCredentials(ctx context.Context, _ secretapi.ListCredentialsRequestObject) (secretapi.ListCredentialsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "credential.read")
	if apiError != nil {
		return secretapi.ListCredentialsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListCredentials(ctx, p.EnterpriseIDValue())
	if err != nil {
		return secretapi.ListCredentialsdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	converted := make([]secretapi.Credential, 0, len(items))
	for _, item := range items {
		converted = append(converted, toCredential(item))
	}
	return secretapi.ListCredentials200JSONResponse{Items: converted, Page: emptySecretPage()}, nil
}

func (handler SecretHandler) CreateCredential(ctx context.Context, request secretapi.CreateCredentialRequestObject) (secretapi.CreateCredentialResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "credential.manage")
	if apiError != nil {
		return secretapi.CreateCredentialdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return secretapi.CreateCredentialdefaultJSONResponse{Body: secretError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	username := ""
	if request.Body.Username != nil {
		username = *request.Body.Username
	}
	value, err := handler.Service.CreateCredential(ctx, p.ActorID(), p.EnterpriseIDValue(), secret.CredentialInput{Name: request.Body.Name,
		Protocol: string(request.Body.Protocol), Username: username, SecretID: uuid.UUID(request.Body.SecretId)}, request.Params.IdempotencyKey)
	if err != nil {
		return secretapi.CreateCredentialdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.CreateCredential201JSONResponse(toCredential(value)), nil
}

func (handler SecretHandler) UpdateCredential(ctx context.Context, request secretapi.UpdateCredentialRequestObject) (secretapi.UpdateCredentialResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "credential.manage")
	if apiError != nil {
		return secretapi.UpdateCredentialdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return secretapi.UpdateCredentialdefaultJSONResponse{Body: secretError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := secret.CredentialInput{ExpectedVersion: request.Body.ExpectedVersion}
	if request.Body.Name != nil {
		input.Name = *request.Body.Name
	}
	if request.Body.Username != nil {
		input.Username = *request.Body.Username
	}
	if request.Body.SecretId != nil {
		input.SecretID = uuid.UUID(*request.Body.SecretId)
	}
	if request.Body.Status != nil {
		status := string(*request.Body.Status)
		input.Status = &status
	}
	value, err := handler.Service.UpdateCredential(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), input)
	if err != nil {
		return secretapi.UpdateCredentialdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.UpdateCredential200JSONResponse(toCredential(value)), nil
}

func (handler SecretHandler) ListManagedAccounts(ctx context.Context, _ secretapi.ListManagedAccountsRequestObject) (secretapi.ListManagedAccountsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "managed_account.read")
	if apiError != nil {
		return secretapi.ListManagedAccountsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListManagedAccounts(ctx, p.EnterpriseIDValue())
	if err != nil {
		return secretapi.ListManagedAccountsdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	converted := make([]secretapi.ManagedAccount, 0, len(items))
	for _, item := range items {
		converted = append(converted, toManagedAccount(item))
	}
	return secretapi.ListManagedAccounts200JSONResponse{Items: converted, Page: emptySecretPage()}, nil
}

func (handler SecretHandler) CreateManagedAccount(ctx context.Context, request secretapi.CreateManagedAccountRequestObject) (secretapi.CreateManagedAccountResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "managed_account.manage")
	if apiError != nil {
		return secretapi.CreateManagedAccountdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return secretapi.CreateManagedAccountdefaultJSONResponse{Body: secretError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	protocols := make([]string, len(request.Body.AllowedProtocols))
	for i, value := range request.Body.AllowedProtocols {
		protocols[i] = string(value)
	}
	value, err := handler.Service.CreateManagedAccount(ctx, p.ActorID(), p.EnterpriseIDValue(), secret.ManagedAccountInput{HostID: uuid.UUID(request.Body.HostId),
		Username: request.Body.Username, PrivilegeLevel: string(request.Body.PrivilegeLevel), CredentialID: uuid.UUID(request.Body.CredentialId),
		AllowedProtocols: protocols}, request.Params.IdempotencyKey)
	if err != nil {
		return secretapi.CreateManagedAccountdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.CreateManagedAccount201JSONResponse(toManagedAccount(value)), nil
}

func (handler SecretHandler) UpdateManagedAccount(ctx context.Context, request secretapi.UpdateManagedAccountRequestObject) (secretapi.UpdateManagedAccountResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "managed_account.manage")
	if apiError != nil {
		return secretapi.UpdateManagedAccountdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return secretapi.UpdateManagedAccountdefaultJSONResponse{Body: secretError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := secret.ManagedAccountInput{ExpectedVersion: request.Body.ExpectedVersion}
	if request.Body.Username != nil {
		input.Username = *request.Body.Username
	}
	if request.Body.PrivilegeLevel != nil {
		input.PrivilegeLevel = string(*request.Body.PrivilegeLevel)
	}
	if request.Body.CredentialId != nil {
		input.CredentialID = uuid.UUID(*request.Body.CredentialId)
	}
	if request.Body.AllowedProtocols != nil {
		for _, value := range *request.Body.AllowedProtocols {
			input.AllowedProtocols = append(input.AllowedProtocols, string(value))
		}
	}
	if request.Body.Status != nil {
		status := string(*request.Body.Status)
		input.Status = &status
	}
	value, err := handler.Service.UpdateManagedAccount(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), input)
	if err != nil {
		return secretapi.UpdateManagedAccountdefaultJSONResponse{Body: secretError(ctx, err), StatusCode: secretStatus(err)}, nil
	}
	return secretapi.UpdateManagedAccount200JSONResponse(toManagedAccount(value)), nil
}

func (handler SecretHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *secretapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &secretapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]secretapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func toSecret(value secret.SecretRecord) secretapi.Secret {
	item := value.Secret
	result := secretapi.Secret{Id: openapi_types.UUID(item.ID), EnterpriseId: pointerUUID(item.EnterpriseID), Name: item.Name, Type: secretapi.SecretType(item.Type),
		Status: secretapi.SecretStatus(item.Status), CurrentVersion: int(item.CurrentVersion), ReferenceCount: int(value.ReferenceCount), Version: item.Version,
		CreatedBy: openapi_types.UUID(item.CreatedBy), CreatedAt: item.CreatedAt.Time, UpdatedAt: item.UpdatedAt.Time}
	if item.Description != "" {
		result.Description = &item.Description
	}
	if item.LastAccessedAt.Valid {
		result.LastAccessedAt = &item.LastAccessedAt.Time
	}
	return result
}

func toCredential(value db.Credential) secretapi.Credential {
	result := secretapi.Credential{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerUUID(value.EnterpriseID), Name: value.Name,
		Protocol: secretapi.CredentialProtocol(value.Protocol), SecretId: openapi_types.UUID(value.SecretID), Status: secretapi.CredentialStatus(value.Status),
		Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.Username != "" {
		result.Username = &value.Username
	}
	return result
}

func toManagedAccount(value db.ManagedAccount) secretapi.ManagedAccount {
	protocols := make([]secretapi.ManagedAccountAllowedProtocols, len(value.AllowedProtocols))
	for i, item := range value.AllowedProtocols {
		protocols[i] = secretapi.ManagedAccountAllowedProtocols(item)
	}
	return secretapi.ManagedAccount{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerUUID(value.EnterpriseID), HostId: openapi_types.UUID(value.HostID),
		Username: value.Username, PrivilegeLevel: secretapi.ManagedAccountPrivilegeLevel(value.PrivilegeLevel), CredentialId: openapi_types.UUID(value.CredentialID),
		AllowedProtocols: protocols, Status: secretapi.ManagedAccountStatus(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
}

func pointerUUID(value uuid.UUID) *openapi_types.UUID {
	converted := openapi_types.UUID(value)
	return &converted
}

func emptySecretPage() secretapi.CursorPage {
	return secretapi.CursorPage{NextCursor: nil, HasMore: false, Partial: secretapi.PartialMetadata{Partial: false, Reasons: []secretapi.PartialMetadataReasons{}}}
}

func secretError(ctx context.Context, err error) secretapi.ApiError {
	base := setupErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	switch {
	case errors.Is(err, secret.ErrVersionConflict):
		base.Code, base.MessageKey = "VERSION_CONFLICT", "errors.common.version_conflict"
	case errors.Is(err, secret.ErrSecretReferenced):
		base.Code, base.MessageKey = "SECRET_REFERENCED", "errors.secret.referenced"
	case errors.Is(err, secret.ErrSecretValueRequired):
		base.Code, base.MessageKey = "INVALID_ARGUMENT", "errors.common.invalid_argument"
	case errors.Is(err, secret.ErrCredentialUnavailable):
		base.Code, base.MessageKey = "CREDENTIAL_UNAVAILABLE", "errors.credential.unavailable"
	case errors.Is(err, postgres.ErrIdempotencyConflict):
		base.Code, base.MessageKey = "IDEMPOTENCY_CONFLICT", "errors.common.idempotency_conflict"
	}
	return secretapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]secretapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func secretStatus(err error) int {
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound
	}
	if errors.Is(err, secret.ErrSecretValueRequired) {
		return http.StatusBadRequest
	}
	return http.StatusConflict
}
