package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	modelapi "github.com/kakj-go/Argus/internal/gen/openapi/modelapi"
	"github.com/kakj-go/Argus/internal/identity"
	modelservice "github.com/kakj-go/Argus/internal/model"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type ModelHandler struct {
	Identity EnterpriseIdentityHandler
	Service  modelservice.Service
}

func (handler ModelHandler) ListAIModels(ctx context.Context, request modelapi.ListAIModelsRequestObject) (modelapi.ListAIModelsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "model.read")
	if apiError != nil {
		return modelapi.ListAIModelsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	limit := int32(100)
	if request.Params.Limit != nil {
		limit = int32(*request.Params.Limit)
	}
	items, err := handler.Service.List(ctx, p.EnterpriseIDValue(), limit)
	if err != nil {
		return modelapi.ListAIModelsdefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	result := make([]modelapi.AIModel, 0, len(items))
	for _, item := range items {
		result = append(result, toAIModel(item))
	}
	return modelapi.ListAIModels200JSONResponse{Items: result, Page: emptyModelPage()}, nil
}

func (handler ModelHandler) TestAndCreateAIModel(ctx context.Context, request modelapi.TestAndCreateAIModelRequestObject) (modelapi.TestAndCreateAIModelResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "model.manage")
	if apiError != nil {
		return modelapi.TestAndCreateAIModeldefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	input := modelCreateInput(*request.Body)
	value, result, err := handler.Service.TestAndCreate(ctx, p.ActorID(), p.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	response := toModelTestResult(result)
	if err != nil {
		return modelapi.TestAndCreateAIModeldefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	model := toAIModel(value)
	response.Model = &model
	return modelapi.TestAndCreateAIModel201JSONResponse(response), nil
}

func (handler ModelHandler) GetAIModel(ctx context.Context, request modelapi.GetAIModelRequestObject) (modelapi.GetAIModelResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "model.read")
	if apiError != nil {
		return modelapi.GetAIModeldefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.Get(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return modelapi.GetAIModeldefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	return modelapi.GetAIModel200JSONResponse(toAIModel(value)), nil
}

func (handler ModelHandler) UpdateAIModel(ctx context.Context, request modelapi.UpdateAIModelRequestObject) (modelapi.UpdateAIModelResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "model.manage")
	if apiError != nil {
		return modelapi.UpdateAIModeldefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	input := modelUpdateInput(*request.Body)
	value, _, err := handler.Service.Update(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), input, request.Params.IdempotencyKey)
	if err != nil {
		return modelapi.UpdateAIModeldefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	return modelapi.UpdateAIModel200JSONResponse(toAIModel(value)), nil
}

func (handler ModelHandler) TestAIModel(ctx context.Context, request modelapi.TestAIModelRequestObject) (modelapi.TestAIModelResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "model.manage")
	if apiError != nil {
		return modelapi.TestAIModeldefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	result, err := handler.Service.TestExisting(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return modelapi.TestAIModeldefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	return modelapi.TestAIModel200JSONResponse(toModelTestResult(result)), nil
}

func (handler ModelHandler) ListModelQuotas(ctx context.Context, _ modelapi.ListModelQuotasRequestObject) (modelapi.ListModelQuotasResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "model.quota.manage")
	if apiError != nil {
		return modelapi.ListModelQuotasdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListQuotas(ctx, p.EnterpriseIDValue())
	if err != nil {
		return modelapi.ListModelQuotasdefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	result := make([]modelapi.ModelQuota, 0, len(items))
	for _, item := range items {
		result = append(result, toModelQuota(item))
	}
	return modelapi.ListModelQuotas200JSONResponse(result), nil
}

func (handler ModelHandler) UpsertModelQuota(ctx context.Context, request modelapi.UpsertModelQuotaRequestObject) (modelapi.UpsertModelQuotaResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "model.quota.manage")
	if apiError != nil {
		return modelapi.UpsertModelQuotadefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.UpsertQuota(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Body.ModelId), string(request.Body.SubjectType), uuid.UUID(request.Body.SubjectId), float64(request.Body.MonthlyAmount), request.Params.IdempotencyKey)
	if err != nil {
		return modelapi.UpsertModelQuotadefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	return modelapi.UpsertModelQuota200JSONResponse(toModelQuota(value)), nil
}

func (handler ModelHandler) ListModelUsage(ctx context.Context, request modelapi.ListModelUsageRequestObject) (modelapi.ListModelUsageResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "model.usage.read")
	if apiError != nil {
		return modelapi.ListModelUsagedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	month := time.Now().UTC()
	if request.Params.Month != nil {
		parsed, err := time.Parse("2006-01", *request.Params.Month)
		if err != nil {
			return modelapi.ListModelUsagedefaultJSONResponse{Body: modelError(ctx, err), StatusCode: http.StatusBadRequest}, nil
		}
		month = parsed
	}
	modelID := uuid.NullUUID{}
	if request.Params.ModelId != nil {
		modelID = uuid.NullUUID{UUID: uuid.UUID(*request.Params.ModelId), Valid: true}
	}
	items, err := handler.Service.ListUsage(ctx, p.EnterpriseIDValue(), month, modelID)
	if err != nil {
		return modelapi.ListModelUsagedefaultJSONResponse{Body: modelError(ctx, err), StatusCode: modelStatus(err)}, nil
	}
	result := make([]modelapi.ModelUsage, 0, len(items))
	for _, item := range items {
		result = append(result, modelapi.ModelUsage{ModelId: item.ModelID, Month: item.Month.Time.Format("2006-01"), RequestCount: item.RequestCount, InputTokens: item.InputTokens, OutputTokens: item.OutputTokens, Amount: float32(numericValue(item.Amount)), CompactionCount: item.CompactionCount})
	}
	return modelapi.ListModelUsage200JSONResponse(result), nil
}

func (handler ModelHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *modelapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &modelapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]modelapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func modelCreateInput(value modelapi.AIModelTestCreate) modelservice.Input {
	key := ""
	if value.ApiKey != nil {
		key = *value.ApiKey
	}
	return modelservice.Input{Name: value.Name, BaseURL: value.BaseUrl, ProviderModelID: value.ModelId, Protocol: string(value.ApiProtocol), ContextWindow: int32(value.ContextWindowTokens), MaxOutput: int32(value.MaxOutputTokens), InputPrice: float64(value.InputPricePerMillion), OutputPrice: float64(value.OutputPricePerMillion), APIKey: key, Status: "enabled"}
}
func modelUpdateInput(value modelapi.AIModelUpdate) modelservice.Input {
	result := modelservice.Input{ExpectedVersion: value.ExpectedVersion, InputPrice: -1, OutputPrice: -1}
	if value.Name != nil {
		result.Name = *value.Name
	}
	if value.BaseUrl != nil {
		result.BaseURL = *value.BaseUrl
	}
	if value.ModelId != nil {
		result.ProviderModelID = *value.ModelId
	}
	if value.ApiProtocol != nil {
		result.Protocol = string(*value.ApiProtocol)
	}
	if value.ContextWindowTokens != nil {
		result.ContextWindow = int32(*value.ContextWindowTokens)
	}
	if value.MaxOutputTokens != nil {
		result.MaxOutput = int32(*value.MaxOutputTokens)
	}
	if value.InputPricePerMillion != nil {
		result.InputPrice = float64(*value.InputPricePerMillion)
	}
	if value.OutputPricePerMillion != nil {
		result.OutputPrice = float64(*value.OutputPricePerMillion)
	}
	if value.ApiKey != nil {
		result.APIKey = *value.ApiKey
	}
	if value.Status != nil {
		result.Status = string(*value.Status)
	}
	return result
}
func toAIModel(value db.AiModel) modelapi.AIModel {
	result := modelapi.AIModel{Id: value.ID, Name: value.Name, BaseUrl: value.BaseUrl, ModelId: value.ModelID, ApiProtocol: modelapi.AIModelProtocol(value.ApiProtocol), ContextWindowTokens: int(value.ContextWindowTokens), MaxOutputTokens: int(value.MaxOutputTokens), InputPricePerMillion: float32(numericValue(value.InputPricePerMillion)), OutputPricePerMillion: float32(numericValue(value.OutputPricePerMillion)), Status: modelapi.AIModelStatus(value.Status), HealthStatus: modelapi.AIModelHealthStatus(value.HealthStatus), Revision: int(value.Revision), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.LastTestedAt.Valid {
		result.LastTestedAt = &value.LastTestedAt.Time
	}
	var capabilities modelapi.ModelCapabilities
	if json.Unmarshal(value.Capabilities, &capabilities) == nil {
		result.Capabilities = &capabilities
	}
	return result
}
func toModelTestResult(value modelservice.CompatibilityResult) modelapi.AIModelTestResult {
	result := modelapi.AIModelTestResult{Compatible: value.Compatible}
	for _, item := range value.Checks {
		check := struct {
			ErrorCode *string                                `json:"error_code,omitempty"`
			Name      modelapi.AIModelTestResultChecksName   `json:"name"`
			Status    modelapi.AIModelTestResultChecksStatus `json:"status"`
		}{Name: modelapi.AIModelTestResultChecksName(item.Name), Status: modelapi.AIModelTestResultChecksStatus(item.Status)}
		if item.ErrorCode != "" {
			check.ErrorCode = &item.ErrorCode
		}
		result.Checks = append(result.Checks, check)
	}
	return result
}
func toModelQuota(value db.ModelQuota) modelapi.ModelQuota {
	return modelapi.ModelQuota{Id: value.ID, ModelId: value.ModelID, SubjectType: modelapi.ModelQuotaSubjectType(value.SubjectType), SubjectId: value.SubjectID, MonthlyAmount: float32(numericValue(value.MonthlyAmount)), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
}
func numericValue(value pgtype.Numeric) float64 {
	result, err := value.Float64Value()
	if err != nil || !result.Valid {
		return 0
	}
	return result.Float64
}
func modelError(ctx context.Context, err error) modelapi.ApiError {
	code, key := "INTERNAL_ERROR", "errors.common.internal"
	defer func() { logMappedError(ctx, code, err) }()
	switch {
	case errors.Is(err, modelservice.ErrEndpointNotAllowed):
		code, key = "MODEL_ENDPOINT_NOT_ALLOWED", "errors.model.endpoint_not_allowed"
	case errors.Is(err, modelservice.ErrCompatibilityFailed):
		code, key = "MODEL_COMPATIBILITY_FAILED", "errors.model.compatibility_failed"
	case errors.Is(err, modelservice.ErrQuotaExceeded):
		code, key = "MODEL_QUOTA_EXCEEDED", "errors.model.quota_exceeded"
	case errors.Is(err, pgx.ErrNoRows):
		code, key = "RESOURCE_NOT_FOUND", "errors.common.resource_not_found"
	}
	requestID := "server-generated-request"
	if current, ok := RequestFromContext(ctx); ok {
		requestID = current.RequestID
	}
	return modelapi.ApiError{Code: code, MessageKey: key, RequestId: requestID}
}
func modelStatus(err error) int {
	switch {
	case errors.Is(err, modelservice.ErrEndpointNotAllowed):
		return http.StatusBadRequest
	case errors.Is(err, modelservice.ErrCompatibilityFailed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, modelservice.ErrQuotaExceeded):
		return http.StatusTooManyRequests
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
func emptyModelPage() modelapi.CursorPage {
	return modelapi.CursorPage{HasMore: false, Partial: modelapi.PartialMetadata{Partial: false, Reasons: []modelapi.PartialMetadataReasons{}}}
}
