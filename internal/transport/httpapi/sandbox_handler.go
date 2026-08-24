package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sandboxapi "github.com/kakj-go/Argus/internal/gen/openapi/sandboxapi"
	"github.com/kakj-go/Argus/internal/sandbox"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type SandboxHandler struct {
	Auth    SetupHandler
	Service sandbox.Service
}

func (handler SandboxHandler) ListSandboxBackends(ctx context.Context, _ sandboxapi.ListSandboxBackendsRequestObject) (sandboxapi.ListSandboxBackendsResponseObject, error) {
	if apiError := handler.auth(ctx, false, ""); apiError != nil {
		return sandboxapi.ListSandboxBackendsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	items, err := handler.Service.ListBackends(ctx)
	if err != nil {
		return sandboxapi.ListSandboxBackendsdefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	result := make([]sandboxapi.SandboxBackend, 0, len(items))
	for _, item := range items {
		result = append(result, toSandboxBackend(item))
	}
	return sandboxapi.ListSandboxBackends200JSONResponse(result), nil
}

func (handler SandboxHandler) CreateSandboxBackend(ctx context.Context, request sandboxapi.CreateSandboxBackendRequestObject) (sandboxapi.CreateSandboxBackendResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.CreateSandboxBackenddefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.CreateBackend(ctx, backendInput(*request.Body))
	if err != nil {
		return sandboxapi.CreateSandboxBackenddefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.CreateSandboxBackend201JSONResponse(toSandboxBackend(value)), nil
}

func (handler SandboxHandler) UpdateSandboxBackend(ctx context.Context, request sandboxapi.UpdateSandboxBackendRequestObject) (sandboxapi.UpdateSandboxBackendResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.UpdateSandboxBackenddefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.UpdateBackend(ctx, uuid.UUID(request.Id), backendInput(*request.Body))
	if err != nil {
		return sandboxapi.UpdateSandboxBackenddefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.UpdateSandboxBackend200JSONResponse(toSandboxBackend(value)), nil
}

func (handler SandboxHandler) TestSandboxBackend(ctx context.Context, request sandboxapi.TestSandboxBackendRequestObject) (sandboxapi.TestSandboxBackendResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.TestSandboxBackenddefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.TestBackend(ctx, uuid.UUID(request.Id))
	if err != nil {
		return sandboxapi.TestSandboxBackenddefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.TestSandboxBackend200JSONResponse(toSandboxBackend(value)), nil
}

func (handler SandboxHandler) ListSandboxImages(ctx context.Context, _ sandboxapi.ListSandboxImagesRequestObject) (sandboxapi.ListSandboxImagesResponseObject, error) {
	if apiError := handler.auth(ctx, false, ""); apiError != nil {
		return sandboxapi.ListSandboxImagesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	items, err := handler.Service.ListImages(ctx)
	if err != nil {
		return sandboxapi.ListSandboxImagesdefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	result := make([]sandboxapi.SandboxImage, 0, len(items))
	for _, item := range items {
		result = append(result, toSandboxImage(item))
	}
	return sandboxapi.ListSandboxImages200JSONResponse(result), nil
}

func (handler SandboxHandler) CreateSandboxImage(ctx context.Context, request sandboxapi.CreateSandboxImageRequestObject) (sandboxapi.CreateSandboxImageResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.CreateSandboxImagedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.CreateImage(ctx, imageInput(*request.Body))
	if err != nil {
		return sandboxapi.CreateSandboxImagedefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.CreateSandboxImage201JSONResponse(toSandboxImage(value)), nil
}

func (handler SandboxHandler) UpdateSandboxImage(ctx context.Context, request sandboxapi.UpdateSandboxImageRequestObject) (sandboxapi.UpdateSandboxImageResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.UpdateSandboxImagedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.UpdateImage(ctx, uuid.UUID(request.Id), imageInput(*request.Body))
	if err != nil {
		return sandboxapi.UpdateSandboxImagedefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.UpdateSandboxImage200JSONResponse(toSandboxImage(value)), nil
}

func (handler SandboxHandler) ListSandboxProfiles(ctx context.Context, _ sandboxapi.ListSandboxProfilesRequestObject) (sandboxapi.ListSandboxProfilesResponseObject, error) {
	if apiError := handler.auth(ctx, false, ""); apiError != nil {
		return sandboxapi.ListSandboxProfilesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	items, err := handler.Service.ListProfiles(ctx)
	if err != nil {
		return sandboxapi.ListSandboxProfilesdefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	result := make([]sandboxapi.SandboxProfile, 0, len(items))
	for _, item := range items {
		result = append(result, toSandboxProfile(item))
	}
	return sandboxapi.ListSandboxProfiles200JSONResponse(result), nil
}

func (handler SandboxHandler) CreateSandboxProfile(ctx context.Context, request sandboxapi.CreateSandboxProfileRequestObject) (sandboxapi.CreateSandboxProfileResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.CreateSandboxProfiledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.CreateProfile(ctx, profileInput(*request.Body))
	if err != nil {
		return sandboxapi.CreateSandboxProfiledefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.CreateSandboxProfile201JSONResponse(toSandboxProfile(value)), nil
}

func (handler SandboxHandler) UpdateSandboxProfile(ctx context.Context, request sandboxapi.UpdateSandboxProfileRequestObject) (sandboxapi.UpdateSandboxProfileResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.UpdateSandboxProfiledefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.UpdateProfile(ctx, uuid.UUID(request.Id), profileInput(*request.Body))
	if err != nil {
		return sandboxapi.UpdateSandboxProfiledefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.UpdateSandboxProfile200JSONResponse(toSandboxProfile(value)), nil
}

func (handler SandboxHandler) GetSandboxQuota(ctx context.Context, request sandboxapi.GetSandboxQuotaRequestObject) (sandboxapi.GetSandboxQuotaResponseObject, error) {
	if apiError := handler.auth(ctx, false, ""); apiError != nil {
		return sandboxapi.GetSandboxQuotadefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.GetQuota(ctx, uuid.UUID(request.EnterpriseId))
	if err != nil {
		return sandboxapi.GetSandboxQuotadefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.GetSandboxQuota200JSONResponse(toSandboxQuota(value)), nil
}

func (handler SandboxHandler) UpdateSandboxQuota(ctx context.Context, request sandboxapi.UpdateSandboxQuotaRequestObject) (sandboxapi.UpdateSandboxQuotaResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.UpdateSandboxQuotadefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.UpdateQuota(ctx, uuid.UUID(request.EnterpriseId), int32(request.Body.MaxConcurrentSessions), request.Body.MonthlySessionSeconds, request.Body.ExpectedVersion)
	if err != nil {
		return sandboxapi.UpdateSandboxQuotadefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.UpdateSandboxQuota200JSONResponse(toSandboxQuota(value)), nil
}

func (handler SandboxHandler) ListSandboxSessions(ctx context.Context, _ sandboxapi.ListSandboxSessionsRequestObject) (sandboxapi.ListSandboxSessionsResponseObject, error) {
	if apiError := handler.auth(ctx, false, ""); apiError != nil {
		return sandboxapi.ListSandboxSessionsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	items, err := handler.Service.ListSessions(ctx)
	if err != nil {
		return sandboxapi.ListSandboxSessionsdefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	result := make([]sandboxapi.SandboxSession, 0, len(items))
	for _, item := range items {
		result = append(result, toSandboxSession(item))
	}
	return sandboxapi.ListSandboxSessions200JSONResponse(result), nil
}

func (handler SandboxHandler) TerminateSandboxSession(ctx context.Context, request sandboxapi.TerminateSandboxSessionRequestObject) (sandboxapi.TerminateSandboxSessionResponseObject, error) {
	if apiError := handler.auth(ctx, true, request.Params.XCSRFToken); apiError != nil {
		return sandboxapi.TerminateSandboxSessiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	value, err := handler.Service.Terminate(ctx, uuid.UUID(request.Id))
	if err != nil {
		return sandboxapi.TerminateSandboxSessiondefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	return sandboxapi.TerminateSandboxSession200JSONResponse(toSandboxSession(value)), nil
}

func (handler SandboxHandler) ListSandboxUsage(ctx context.Context, _ sandboxapi.ListSandboxUsageRequestObject) (sandboxapi.ListSandboxUsageResponseObject, error) {
	if apiError := handler.auth(ctx, false, ""); apiError != nil {
		return sandboxapi.ListSandboxUsagedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusUnauthorized}, nil
	}
	items, err := handler.Service.ListUsage(ctx)
	if err != nil {
		return sandboxapi.ListSandboxUsagedefaultJSONResponse{Body: sandboxError(ctx, err), StatusCode: sandboxStatus(err)}, nil
	}
	result := make([]sandboxapi.SandboxUsage, 0, len(items))
	for _, item := range items {
		result = append(result, toSandboxUsage(item))
	}
	return sandboxapi.ListSandboxUsage200JSONResponse(result), nil
}

func (handler SandboxHandler) auth(ctx context.Context, mutation bool, csrf string) *sandboxapi.ApiError {
	principal, err := handler.Auth.authenticate(ctx, "platform", mutation, csrf)
	if err == nil && principal.PlatformUser != nil {
		return nil
	}
	value := platformError(ctx, err)
	return &sandboxapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]sandboxapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func backendInput(value sandboxapi.SandboxBackendWrite) sandbox.BackendInput {
	key := ""
	if value.ApiKey != nil {
		key = *value.ApiKey
	}
	return sandbox.BackendInput{Name: value.Name, Endpoint: value.Endpoint, APIKey: key, Status: string(value.Status), ExpectedVersion: value.ExpectedVersion}
}
func imageInput(value sandboxapi.SandboxImageWrite) sandbox.ImageInput {
	return sandbox.ImageInput{BackendID: uuid.UUID(value.BackendId), Name: value.Name, ImageRef: value.ImageRef, Digest: value.Digest, Status: string(value.Status), ExpectedVersion: value.ExpectedVersion}
}
func profileInput(value sandboxapi.SandboxProfileWrite) sandbox.ProfileInput {
	kinds := make([]string, len(value.TaskKinds))
	for i, kind := range value.TaskKinds {
		kinds[i] = string(kind)
	}
	return sandbox.ProfileInput{Name: value.Name, BackendID: uuid.UUID(value.BackendId), ImageID: uuid.UUID(value.ImageId), TaskKinds: kinds, CPUMillis: int32(value.CpuMillis), MemoryMiB: int32(value.MemoryMib), TimeoutSeconds: int32(value.TimeoutSeconds), NetworkMode: string(value.NetworkMode), Status: string(value.Status), ExpectedVersion: value.ExpectedVersion}
}

func toSandboxBackend(v db.SandboxBackend) sandboxapi.SandboxBackend {
	return sandboxapi.SandboxBackend{Id: v.ID, Name: v.Name, Endpoint: v.Endpoint, Status: sandboxapi.SandboxBackendStatus(v.Status), HealthStatus: sandboxapi.SandboxBackendHealthStatus(v.HealthStatus), Version: v.Version, CreatedAt: v.CreatedAt.Time, UpdatedAt: v.UpdatedAt.Time}
}
func toSandboxImage(v db.SandboxImage) sandboxapi.SandboxImage {
	return sandboxapi.SandboxImage{Id: v.ID, BackendId: v.BackendID, Name: v.Name, ImageRef: v.ImageRef, Digest: v.Digest, Status: sandboxapi.SandboxImageStatus(v.Status), Version: v.Version, CreatedAt: v.CreatedAt.Time, UpdatedAt: v.UpdatedAt.Time}
}
func toSandboxProfile(v db.SandboxProfile) sandboxapi.SandboxProfile {
	kinds := make([]sandboxapi.SandboxProfileTaskKinds, len(v.TaskKinds))
	for i, kind := range v.TaskKinds {
		kinds[i] = sandboxapi.SandboxProfileTaskKinds(kind)
	}
	return sandboxapi.SandboxProfile{Id: v.ID, Name: v.Name, BackendId: v.BackendID, ImageId: v.ImageID, TaskKinds: kinds, CpuMillis: int(v.CpuMillis), MemoryMib: int(v.MemoryMib), TimeoutSeconds: int(v.TimeoutSeconds), NetworkMode: sandboxapi.SandboxProfileNetworkMode(v.NetworkMode), Status: sandboxapi.SandboxProfileStatus(v.Status), Revision: int(v.Revision), Version: v.Version, CreatedAt: v.CreatedAt.Time, UpdatedAt: v.UpdatedAt.Time}
}
func toSandboxQuota(v db.SandboxQuota) sandboxapi.SandboxQuota {
	return sandboxapi.SandboxQuota{EnterpriseId: v.EnterpriseID, MaxConcurrentSessions: int(v.MaxConcurrentSessions), MonthlySessionSeconds: v.MonthlySessionSeconds, Version: v.Version, CreatedAt: v.CreatedAt.Time, UpdatedAt: v.UpdatedAt.Time}
}
func toSandboxSession(v db.SandboxSession) sandboxapi.SandboxSession {
	return sandboxapi.SandboxSession{Id: v.ID, EnterpriseId: v.EnterpriseID, TaskId: v.TaskID, ProfileId: v.ProfileID, ProfileRevision: int(v.ProfileRevision), UpstreamSessionId: v.UpstreamSessionID, Status: sandboxapi.SandboxSessionStatus(v.Status), ExpiresAt: v.ExpiresAt.Time, CreatedAt: v.CreatedAt.Time, UpdatedAt: v.UpdatedAt.Time}
}
func toSandboxUsage(v db.SandboxUsage) sandboxapi.SandboxUsage {
	month := ""
	if v.Month.Valid {
		month = v.Month.Time.Format("2006-01")
	}
	return sandboxapi.SandboxUsage{EnterpriseId: v.EnterpriseID, Month: month, SessionCount: v.SessionCount, SessionSeconds: v.SessionSeconds}
}

func sandboxError(ctx context.Context, err error) sandboxapi.ApiError {
	code, key := "SANDBOX_PROFILE_UNAVAILABLE", "errors.sandbox.profile_unavailable"
	defer func() { logMappedError(ctx, code, err) }()
	if errors.Is(err, sandbox.ErrQuotaExceeded) {
		code, key = "SANDBOX_QUOTA_EXCEEDED", "errors.sandbox.quota_exceeded"
	}
	if errors.Is(err, sandbox.ErrVersionConflict) {
		code, key = "VERSION_CONFLICT", "errors.common.version_conflict"
	}
	return sandboxapi.ApiError{Code: code, MessageKey: key, RequestId: requestID(ctx)}
}
func sandboxStatus(err error) int {
	if errors.Is(err, pgx.ErrNoRows) {
		return http.StatusNotFound
	}
	if errors.Is(err, sandbox.ErrVersionConflict) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}
