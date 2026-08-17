// Package model owns governed model revisions, compatibility checks, credentials, quotas, and usage.
package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/integration/modelprovider"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrEndpointNotAllowed    = modelprovider.ErrEndpointNotAllowed
	ErrCompatibilityFailed   = errors.New("model compatibility failed")
	ErrQuotaExceeded         = errors.New("model quota exceeded")
	ErrCredentialUnavailable = errors.New("model credential unavailable")
)

type Input struct {
	Name            string
	BaseURL         string
	ProviderModelID string
	Protocol        string
	ContextWindow   int32
	MaxOutput       int32
	InputPrice      float64
	OutputPrice     float64
	APIKey          string
	Status          string
	ExpectedVersion int64
}

type CompatibilityCheck struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}

type CompatibilityResult struct {
	Compatible bool                 `json:"compatible"`
	Checks     []CompatibilityCheck `json:"checks"`
}

type Tester interface {
	Test(context.Context, Input) CompatibilityResult
}

type Service struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
	Keyring     secret.Keyring
	Tester      Tester
}

func (service Service) List(ctx context.Context, enterpriseID uuid.UUID, limit int32) ([]db.AiModel, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return service.Store.Queries.ListAIModels(ctx, db.ListAIModelsParams{EnterpriseID: enterpriseID, Limit: limit})
}

func (service Service) Get(ctx context.Context, enterpriseID, modelID uuid.UUID) (db.AiModel, error) {
	return service.Store.Queries.GetAIModel(ctx, db.GetAIModelParams{ID: modelID, EnterpriseID: enterpriseID})
}

func (service Service) TestAndCreate(ctx context.Context, actorID string, enterpriseID uuid.UUID, input Input, idempotencyKey string) (db.AiModel, CompatibilityResult, error) {
	result := service.test(ctx, input)
	if !result.Compatible {
		return db.AiModel{}, result, ErrCompatibilityFailed
	}
	modelValue, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "model.create", idempotencyKey, publicInput(input), 201,
		func(q *db.Queries) (db.AiModel, error) {
			modelID, revisionID := newID(), newID()
			capabilities := capabilitiesJSON(input)
			created, err := q.CreateAIModel(ctx, db.CreateAIModelParams{ID: modelID, EnterpriseID: enterpriseID, Name: input.Name,
				BaseUrl: input.BaseURL, ModelID: input.ProviderModelID, ApiProtocol: input.Protocol, ContextWindowTokens: input.ContextWindow,
				MaxOutputTokens: input.MaxOutput, InputPricePerMillion: numeric(input.InputPrice), OutputPricePerMillion: numeric(input.OutputPrice), Capabilities: capabilities})
			if err != nil {
				return db.AiModel{}, err
			}
			revision, err := q.CreateAIModelRevision(ctx, db.CreateAIModelRevisionParams{ID: revisionID, ModelID: modelID, EnterpriseID: enterpriseID,
				Revision: 1, BaseUrl: input.BaseURL, ProviderModelID: input.ProviderModelID, ApiProtocol: input.Protocol,
				ContextWindowTokens: input.ContextWindow, MaxOutputTokens: input.MaxOutput, InputPricePerMillion: numeric(input.InputPrice),
				OutputPricePerMillion: numeric(input.OutputPrice), Capabilities: capabilities})
			if err != nil {
				return db.AiModel{}, err
			}
			if err := service.storeCredential(ctx, q, enterpriseID, revision, input.APIKey); err != nil {
				return db.AiModel{}, err
			}
			checks, _ := json.Marshal(result.Checks)
			_, err = q.CreateModelCompatibilityResult(ctx, db.CreateModelCompatibilityResultParams{ID: newID(), ModelRevisionID: revision.ID,
				EnterpriseID: enterpriseID, Compatible: true, Checks: checks})
			return created, err
		})
	return modelValue, result, err
}

func (service Service) Update(ctx context.Context, actorID string, enterpriseID, modelID uuid.UUID, patch Input, idempotencyKey string) (db.AiModel, CompatibilityResult, error) {
	current, err := service.Get(ctx, enterpriseID, modelID)
	if err != nil {
		return db.AiModel{}, CompatibilityResult{}, err
	}
	latest, err := service.Store.Queries.GetLatestAIModelRevision(ctx, db.GetLatestAIModelRevisionParams{ModelID: modelID, EnterpriseID: enterpriseID})
	if err != nil {
		return db.AiModel{}, CompatibilityResult{}, err
	}
	merged := mergeInput(current, patch)
	if merged.APIKey == "" {
		credential, err := service.LeaseCredential(ctx, enterpriseID, latest.ID)
		if err != nil {
			return db.AiModel{}, CompatibilityResult{}, err
		}
		merged.APIKey = string(credential)
		clear(credential)
	}
	result := service.test(ctx, merged)
	if !result.Compatible {
		return db.AiModel{}, result, ErrCompatibilityFailed
	}
	updated, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "model.update", idempotencyKey, publicInput(merged), 200,
		func(q *db.Queries) (db.AiModel, error) {
			revisionNumber := current.Revision + 1
			capabilities := capabilitiesJSON(merged)
			revision, err := q.CreateAIModelRevision(ctx, db.CreateAIModelRevisionParams{ID: newID(), ModelID: modelID, EnterpriseID: enterpriseID,
				Revision: revisionNumber, BaseUrl: merged.BaseURL, ProviderModelID: merged.ProviderModelID, ApiProtocol: merged.Protocol,
				ContextWindowTokens: merged.ContextWindow, MaxOutputTokens: merged.MaxOutput, InputPricePerMillion: numeric(merged.InputPrice),
				OutputPricePerMillion: numeric(merged.OutputPrice), Capabilities: capabilities})
			if err != nil {
				return db.AiModel{}, err
			}
			if err := service.storeCredential(ctx, q, enterpriseID, revision, merged.APIKey); err != nil {
				return db.AiModel{}, err
			}
			checks, _ := json.Marshal(result.Checks)
			if _, err := q.CreateModelCompatibilityResult(ctx, db.CreateModelCompatibilityResultParams{ID: newID(), ModelRevisionID: revision.ID,
				EnterpriseID: enterpriseID, Compatible: true, Checks: checks}); err != nil {
				return db.AiModel{}, err
			}
			status := merged.Status
			if status == "" {
				status = current.Status
			}
			return q.UpdateAIModel(ctx, db.UpdateAIModelParams{ID: modelID, EnterpriseID: enterpriseID, Name: merged.Name,
				BaseUrl: merged.BaseURL, ModelID: merged.ProviderModelID, ApiProtocol: merged.Protocol, ContextWindowTokens: merged.ContextWindow,
				MaxOutputTokens: merged.MaxOutput, InputPricePerMillion: numeric(merged.InputPrice), OutputPricePerMillion: numeric(merged.OutputPrice),
				Capabilities: capabilities, Status: status, HealthStatus: "healthy", Revision: revisionNumber, Version: patch.ExpectedVersion})
		})
	return updated, result, err
}

func (service Service) TestExisting(ctx context.Context, enterpriseID, modelID uuid.UUID) (CompatibilityResult, error) {
	current, err := service.Get(ctx, enterpriseID, modelID)
	if err != nil {
		return CompatibilityResult{}, err
	}
	revision, err := service.Store.Queries.GetLatestAIModelRevision(ctx, db.GetLatestAIModelRevisionParams{ModelID: modelID, EnterpriseID: enterpriseID})
	if err != nil {
		return CompatibilityResult{}, err
	}
	credential, err := service.LeaseCredential(ctx, enterpriseID, revision.ID)
	if err != nil {
		return CompatibilityResult{}, err
	}
	defer clear(credential)
	input := mergeInput(current, Input{APIKey: string(credential)})
	result := service.test(ctx, input)
	checks, _ := json.Marshal(result.Checks)
	_, err = service.Store.Queries.CreateModelCompatibilityResult(ctx, db.CreateModelCompatibilityResultParams{ID: newID(), ModelRevisionID: revision.ID,
		EnterpriseID: enterpriseID, Compatible: result.Compatible, Checks: checks, ErrorCode: pgtype.Text{String: compatibilityCode(result), Valid: !result.Compatible}})
	return result, err
}

func (service Service) LeaseCredential(ctx context.Context, enterpriseID, revisionID uuid.UUID) ([]byte, error) {
	value, err := service.Store.Queries.GetAIModelCredential(ctx, db.GetAIModelCredentialParams{ModelRevisionID: revisionID, EnterpriseID: enterpriseID})
	if err != nil {
		return nil, ErrCredentialUnavailable
	}
	plaintext, err := service.Keyring.Decrypt(secret.Envelope{Provider: "local", KeyID: value.KeyID, KeyVersion: int(value.KeyVersion),
		WrappedDEK: value.WrappedDek, WrapNonce: value.WrapNonce, Nonce: value.Nonce, Ciphertext: value.Ciphertext, ValueHash: value.ValueHash}, modelAAD(enterpriseID, revisionID))
	if err != nil {
		return nil, ErrCredentialUnavailable
	}
	return plaintext, nil
}

func (service Service) ListQuotas(ctx context.Context, enterpriseID uuid.UUID) ([]db.ModelQuota, error) {
	return service.Store.Queries.ListModelQuotas(ctx, enterpriseID)
}

func (service Service) UpsertQuota(ctx context.Context, actorID string, enterpriseID, modelID uuid.UUID, subjectType string, subjectID uuid.UUID, amount float64, idempotencyKey string) (db.ModelQuota, error) {
	input := map[string]any{"model_id": modelID, "subject_type": subjectType, "subject_id": subjectID, "monthly_amount": amount}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "model_quota.upsert", idempotencyKey, input, 200,
		func(q *db.Queries) (db.ModelQuota, error) {
			return q.UpsertModelQuota(ctx, db.UpsertModelQuotaParams{ID: newID(), EnterpriseID: enterpriseID, ModelID: modelID,
				SubjectType: subjectType, SubjectID: subjectID, MonthlyAmount: numeric(amount)})
		})
}

func (service Service) ListUsage(ctx context.Context, enterpriseID uuid.UUID, month time.Time, modelID uuid.NullUUID) ([]db.ListModelUsageRow, error) {
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	return service.Store.Queries.ListModelUsage(ctx, db.ListModelUsageParams{EnterpriseID: enterpriseID,
		CompletedAt: pgtype.Timestamptz{Time: start, Valid: true}, CompletedAt_2: pgtype.Timestamptz{Time: start.AddDate(0, 1, 0), Valid: true}, ModelID: modelID})
}

func (service Service) ReserveQuota(ctx context.Context, call db.ModelCall, departmentID, userID uuid.UUID, amount pgtype.Numeric) (db.ModelQuotaReservation, error) {
	var reservation db.ModelQuotaReservation
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		quotas, err := q.GetApplicableModelQuotasForUpdate(ctx, db.GetApplicableModelQuotasForUpdateParams{EnterpriseID: call.EnterpriseID,
			ModelID: call.ModelID, SubjectID: departmentID, SubjectID_2: userID})
		if err != nil {
			return err
		}
		month := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
		used, err := q.SumQuotaReservationsBySubject(ctx, db.SumQuotaReservationsBySubjectParams{EnterpriseID: call.EnterpriseID,
			ModelID: call.ModelID, Month: pgtype.Date{Time: month, Valid: true}, DepartmentID: departmentID, UserID: userID})
		if err != nil {
			return err
		}
		requested := numericFloat(amount)
		for _, quota := range quotas {
			current := numericFloat(used.UserAmount)
			if quota.SubjectType == "department" {
				current = numericFloat(used.DepartmentAmount)
			}
			if current+requested > numericFloat(quota.MonthlyAmount) {
				return ErrQuotaExceeded
			}
		}
		reservation, err = q.CreateQuotaReservation(ctx, db.CreateQuotaReservationParams{ID: newID(), EnterpriseID: call.EnterpriseID,
			ModelCallID: call.ID, ModelID: call.ModelID, DepartmentID: departmentID, UserID: userID, Month: pgtype.Date{Time: month, Valid: true},
			ReservedAmount: amount, ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(15 * time.Minute), Valid: true}})
		return err
	})
	return reservation, err
}

func (service Service) SettleQuota(ctx context.Context, reservation db.ModelQuotaReservation, amount pgtype.Numeric) error {
	_, err := service.Store.Queries.SettleQuotaReservation(ctx, db.SettleQuotaReservationParams{ID: reservation.ID, EnterpriseID: reservation.EnterpriseID, SettledAmount: amount})
	return err
}

func (service Service) test(ctx context.Context, input Input) CompatibilityResult {
	tester := service.Tester
	if tester == nil {
		tester = ProviderTester{}
	}
	return tester.Test(ctx, input)
}

func (service Service) storeCredential(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, revision db.AiModelRevision, value string) error {
	if value == "" {
		return ErrCredentialUnavailable
	}
	envelope, err := service.Keyring.Encrypt([]byte(value), modelAAD(enterpriseID, revision.ID))
	if err != nil {
		return err
	}
	_, err = q.CreateAIModelCredential(ctx, db.CreateAIModelCredentialParams{ID: newID(), ModelRevisionID: revision.ID,
		EnterpriseID: enterpriseID, KeyID: envelope.KeyID, KeyVersion: int32(envelope.KeyVersion), WrappedDek: envelope.WrappedDEK,
		WrapNonce: envelope.WrapNonce, Nonce: envelope.Nonce, Ciphertext: envelope.Ciphertext, ValueHash: envelope.ValueHash})
	return err
}

func modelAAD(enterpriseID, revisionID uuid.UUID) []byte {
	return []byte("argus.model_credential/v1\x00" + enterpriseID.String() + "\x00" + revisionID.String())
}

func mergeInput(current db.AiModel, patch Input) Input {
	result := Input{Name: current.Name, BaseURL: current.BaseUrl, ProviderModelID: current.ModelID, Protocol: current.ApiProtocol,
		ContextWindow: current.ContextWindowTokens, MaxOutput: current.MaxOutputTokens, InputPrice: numericFloat(current.InputPricePerMillion),
		OutputPrice: numericFloat(current.OutputPricePerMillion), Status: current.Status, ExpectedVersion: patch.ExpectedVersion, APIKey: patch.APIKey}
	if patch.Name != "" {
		result.Name = patch.Name
	}
	if patch.BaseURL != "" {
		result.BaseURL = patch.BaseURL
	}
	if patch.ProviderModelID != "" {
		result.ProviderModelID = patch.ProviderModelID
	}
	if patch.Protocol != "" {
		result.Protocol = patch.Protocol
	}
	if patch.ContextWindow > 0 {
		result.ContextWindow = patch.ContextWindow
	}
	if patch.MaxOutput > 0 {
		result.MaxOutput = patch.MaxOutput
	}
	if patch.InputPrice >= 0 && (patch.InputPrice != 0 || numericFloat(current.InputPricePerMillion) == 0) {
		result.InputPrice = patch.InputPrice
	}
	if patch.OutputPrice >= 0 && (patch.OutputPrice != 0 || numericFloat(current.OutputPricePerMillion) == 0) {
		result.OutputPrice = patch.OutputPrice
	}
	if patch.Status != "" {
		result.Status = patch.Status
	}
	return result
}

func publicInput(input Input) any { value := input; value.APIKey = ""; return value }
func capabilitiesJSON(input Input) []byte {
	value, _ := json.Marshal(map[string]any{"supports_streaming": true, "supports_tool_calling": true,
		"supports_structured_output": true, "provider_compaction_capability": false, "context_window_tokens": input.ContextWindow, "max_output_tokens": input.MaxOutput})
	return value
}

func numeric(value float64) pgtype.Numeric {
	var result pgtype.Numeric
	_ = result.Scan(fmt.Sprintf("%.8f", value))
	return result
}
func numericFloat(value pgtype.Numeric) float64 {
	result, err := value.Float64Value()
	if err != nil || !result.Valid {
		return 0
	}
	return result.Float64
}
func compatibilityCode(value CompatibilityResult) string {
	if value.Compatible {
		return ""
	}
	return "MODEL_COMPATIBILITY_FAILED"
}
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}
