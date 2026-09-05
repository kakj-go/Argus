package resource

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/installinstruction"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrActionInvalidated = errors.New("pending action invalidated")
	ErrActionUnavailable = errors.New("pending action unavailable")
)

type PendingActionService struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
	Key         []byte
	TTL         time.Duration
}

type PrepareActionInput struct {
	ActionType, Title, Summary, Risk string
	ResourceType                     string
	ResourceID                       uuid.NullUUID
	ExpectedResourceVersion          pgtype.Int8
	AuthorizationVersion             int64
	Preview, Diff                    any
	ImmutablePlan                    any
	ResourceScopeSnapshot            any
	CommitHandler                    string
	RunID                            uuid.NullUUID
}

type ActionSubject struct {
	ID                   string
	Type                 string
	AuthorizationVersion int64
	ConfirmationRequired bool
}

type ActionCommitResult struct {
	ResourceType         string
	ResourceID           uuid.UUID
	ResourceVersion      int64
	Summary              string
	ErrorCode            string
	ConnectorCommandID   uuid.NullUUID
	TelemetryOperationID uuid.NullUUID
	OneTimeCommand       *OneTimeCommandResult
	// ConnectorInstallOperationID is the durable operation reference used by
	// direct Connector installation. The action remains result_unknown until
	// that operation reaches its complete success condition.
	ConnectorInstallOperationID uuid.NullUUID
	OneTimeResultKind           string
}

type EnrollmentResult struct {
	EnrollmentID    uuid.UUID                `json:"enrollment_id"`
	InstructionSets []installinstruction.Set `json:"instruction_sets"`
	ExpiresAt       time.Time                `json:"expires_at"`
}

type OneTimeCommandResult struct {
	InstructionSets []installinstruction.Set
	ExpiresAt       time.Time
}

type ActionConfirmation struct {
	PendingAction db.PendingAction `json:"pending_action"`
}

type ActionCommitFunc func(context.Context, *db.Queries, db.PendingAction, json.RawMessage) (ActionCommitResult, error)
type ActionRevalidateFunc func(context.Context, *db.Queries, db.PendingAction, json.RawMessage) ([]byte, error)

// ExecuteReady consumes the private token and applies the immutable plan. It
// must only be called by the internal Action Executor after confirmation and
// all approval requirements have transitioned the action to ready.
func (service PendingActionService) ExecuteReady(ctx context.Context, q *db.Queries, action db.PendingAction, revalidate ActionRevalidateFunc, commit ActionCommitFunc) (ActionCommitResult, error) {
	if action.Status != "ready" || time.Now().UTC().After(action.ExpiresAt.Time) {
		return ActionCommitResult{}, ErrActionUnavailable
	}
	plan, err := q.GetPendingActionPlan(ctx, db.GetPendingActionPlanParams{ActionRef: action.ActionRef, EnterpriseID: action.EnterpriseID})
	if err != nil || plan.AuthorizationVersion != action.AuthorizationVersion {
		return ActionCommitResult{}, ErrActionInvalidated
	}
	canonicalPlan, err := CanonicalJSON(plan.ImmutablePlan)
	if err != nil {
		return ActionCommitResult{}, ErrActionInvalidated
	}
	planHash := sha256.Sum256(canonicalPlan)
	if !subtleEqual(planHash[:], plan.PlanHash) {
		return ActionCommitResult{}, ErrActionInvalidated
	}
	token, err := q.GetPendingActionTokenForUpdate(ctx, db.GetPendingActionTokenForUpdateParams{ActionRef: action.ActionRef, EnterpriseID: action.EnterpriseID})
	if err != nil || token.Status != "active" || time.Now().UTC().After(token.ExpiresAt.Time) {
		return ActionCommitResult{}, ErrActionUnavailable
	}
	plaintext, err := (actionCipher{key: service.Key}).decrypt(token.Nonce, token.Ciphertext, actionAAD(action.EnterpriseID.String(), action.ID.String()))
	if err != nil || !verifyActionToken(string(plaintext), token.TokenHash) {
		clear(plaintext)
		return ActionCommitResult{}, ErrActionInvalidated
	}
	clear(plaintext)
	if revalidate != nil {
		impactHash, err := revalidate(ctx, q, action, canonicalPlan)
		if err != nil || !subtleEqual(impactHash, action.ImpactHash) {
			return ActionCommitResult{}, ErrActionInvalidated
		}
	}
	rows, err := q.ConsumePendingActionToken(ctx, db.ConsumePendingActionTokenParams{PendingActionID: action.ID, EnterpriseID: action.EnterpriseID})
	if err != nil || rows != 1 {
		return ActionCommitResult{}, ErrActionUnavailable
	}
	if _, err := q.MarkPendingActionExecutingM4(ctx, db.MarkPendingActionExecutingM4Params{ID: action.ID, EnterpriseID: action.EnterpriseID}); err != nil {
		return ActionCommitResult{}, ErrActionUnavailable
	}
	return commit(ctx, q, action, canonicalPlan)
}

func (service PendingActionService) Prepare(ctx context.Context, actorID string, enterpriseID uuid.UUID, input PrepareActionInput, idempotencyKey string) (db.PendingAction, error) {
	return service.PrepareForSubject(ctx, ActionSubject{ID: actorID, Type: "user", AuthorizationVersion: input.AuthorizationVersion, ConfirmationRequired: true}, enterpriseID, input, idempotencyKey)
}

func (service PendingActionService) PrepareForSubject(ctx context.Context, subject ActionSubject, enterpriseID uuid.UUID, input PrepareActionInput, idempotencyKey string) (db.PendingAction, error) {
	if service.TTL <= 0 {
		service.TTL = 15 * time.Minute
	}
	if subject.Type != "user" && subject.Type != "service_account" {
		return db.PendingAction{}, ErrActionUnavailable
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", subject.ID, input.ActionType+".preview", idempotencyKey, input, 201, func(q *db.Queries) (db.PendingAction, error) {
		actorUUID, err := uuid.Parse(subject.ID)
		if err != nil {
			return db.PendingAction{}, ErrActionUnavailable
		}
		planJSON, err := json.Marshal(input.ImmutablePlan)
		if err != nil || len(planJSON) == 0 || string(planJSON) == "null" {
			return db.PendingAction{}, ErrActionUnavailable
		}
		planJSON, err = CanonicalJSON(planJSON)
		if err != nil {
			return db.PendingAction{}, ErrActionUnavailable
		}
		previewJSON, err := json.Marshal(input.Preview)
		if err != nil {
			return db.PendingAction{}, err
		}
		diffJSON, err := json.Marshal(input.Diff)
		if err != nil {
			return db.PendingAction{}, err
		}
		snapshotJSON, err := json.Marshal(input.ResourceScopeSnapshot)
		if err != nil {
			return db.PendingAction{}, err
		}
		planHash := sha256.Sum256(planJSON)
		impactHash := sha256.Sum256(snapshotJSON)
		actionID := newResourceID()
		actionRef, err := randomReference("act_", 18)
		if err != nil {
			return db.PendingAction{}, err
		}
		expiresAt := time.Now().UTC().Add(service.TTL)
		status := "awaiting_approval"
		if subject.ConfirmationRequired {
			status = "awaiting_confirmation"
		}
		action, err := q.CreatePendingAction(ctx, db.CreatePendingActionParams{ID: actionID, ActionRef: actionRef, EnterpriseID: enterpriseID,
			CreatorSubjectID: actorUUID, CreatorSubjectType: subject.Type, AuthorizationVersion: subject.AuthorizationVersion, ActionType: input.ActionType, Title: input.Title,
			Summary: input.Summary, Risk: input.Risk, Preview: previewJSON, Diff: diffJSON, ResourceType: input.ResourceType,
			ResourceID: input.ResourceID, ExpectedResourceVersion: input.ExpectedResourceVersion, ImpactHash: impactHash[:],
			Status: status, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}, RunID: input.RunID})
		if err != nil {
			return db.PendingAction{}, err
		}
		previewCallID, err := randomReference("preview_", 16)
		if err != nil {
			return db.PendingAction{}, err
		}
		if _, err := q.CreatePendingActionPlan(ctx, db.CreatePendingActionPlanParams{ID: newResourceID(), PendingActionID: action.ID,
			EnterpriseID: enterpriseID, PreviewCallID: previewCallID, CommitTool: input.CommitHandler, AuthorizationVersion: subject.AuthorizationVersion,
			PlanSchemaVersion: "argus.resource_plan/v1", PlanHash: planHash[:], ImmutablePlan: planJSON, ResourceScopeSnapshot: snapshotJSON}); err != nil {
			return db.PendingAction{}, err
		}
		token, tokenHash, err := newActionToken()
		if err != nil {
			return db.PendingAction{}, err
		}
		nonce, ciphertext, err := (actionCipher{key: service.Key}).encrypt([]byte(token), actionAAD(enterpriseID.String(), action.ID.String()))
		if err != nil {
			return db.PendingAction{}, err
		}
		if _, err := q.CreatePendingActionToken(ctx, db.CreatePendingActionTokenParams{ID: newResourceID(), PendingActionID: action.ID,
			EnterpriseID: enterpriseID, TokenHash: tokenHash, KeyVersion: 1, Nonce: nonce, Ciphertext: ciphertext,
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}}); err != nil {
			return db.PendingAction{}, err
		}
		clear([]byte(token))
		if err := appendResourceAudit(ctx, q, subject.ID, enterpriseID, input.ActionType+".preview", input.ResourceType, input.ResourceID.UUID,
			map[string]any{"summary": "resource action prepared", "authorization_version": subject.AuthorizationVersion, "subject_type": subject.Type}); err != nil {
			return db.PendingAction{}, err
		}
		return action, nil
	})
}

func (service PendingActionService) List(ctx context.Context, enterpriseID uuid.UUID) ([]db.PendingAction, error) {
	return service.Store.Queries.ListPendingActions(ctx, enterpriseID)
}

func (service PendingActionService) ListCreated(ctx context.Context, enterpriseID uuid.UUID, actorID string) ([]db.PendingAction, error) {
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return nil, err
	}
	return service.Store.Queries.ListPendingActionsByCreator(ctx, db.ListPendingActionsByCreatorParams{
		EnterpriseID:     enterpriseID,
		CreatorSubjectID: actor,
	})
}

func (service PendingActionService) ListMine(ctx context.Context, enterpriseID uuid.UUID, actorID string) ([]db.PendingAction, error) {
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return nil, err
	}
	return service.Store.Queries.ListPendingActionsForApprover(ctx, db.ListPendingActionsForApproverParams{
		EnterpriseID: enterpriseID,
		SubjectID:    actor,
	})
}

func (service PendingActionService) Get(ctx context.Context, enterpriseID uuid.UUID, actionRef string) (db.PendingAction, error) {
	return service.Store.Queries.GetPendingAction(ctx, db.GetPendingActionParams{ActionRef: actionRef, EnterpriseID: enterpriseID})
}

func (service PendingActionService) Cancel(ctx context.Context, actorID string, enterpriseID uuid.UUID, actionRef, idempotencyKey string) (db.PendingAction, error) {
	actorUUID, err := uuid.Parse(actorID)
	if err != nil {
		return db.PendingAction{}, ErrActionUnavailable
	}
	request := struct {
		ActionRef string `json:"action_ref"`
	}{ActionRef: actionRef}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "pending_action.cancel", idempotencyKey, request, 200, func(q *db.Queries) (db.PendingAction, error) {
		result, err := q.CancelPendingAction(ctx, db.CancelPendingActionParams{ActionRef: actionRef, EnterpriseID: enterpriseID, CreatorSubjectID: actorUUID})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.PendingAction{}, ErrActionUnavailable
		}
		if err != nil {
			return db.PendingAction{}, err
		}
		if err := appendResourceAudit(ctx, q, actorID, enterpriseID, "pending_action.cancel", result.ResourceType, result.ResourceID.UUID, map[string]any{"status": "cancelled"}); err != nil {
			return db.PendingAction{}, err
		}
		return result, nil
	})
}

func (service PendingActionService) Confirm(ctx context.Context, actorID string, enterpriseID uuid.UUID, authorizationVersion int64, actionRef, idempotencyKey string, revalidate ActionRevalidateFunc, commit ActionCommitFunc) (ActionConfirmation, error) {
	actorUUID, err := uuid.Parse(actorID)
	if err != nil {
		return ActionConfirmation{}, ErrActionUnavailable
	}
	request := struct {
		ActionRef            string `json:"action_ref"`
		AuthorizationVersion int64  `json:"authorization_version"`
	}{ActionRef: actionRef, AuthorizationVersion: authorizationVersion}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "pending_action.confirm", idempotencyKey, request, 200, func(q *db.Queries) (ActionConfirmation, error) {
		action, err := q.GetPendingActionForUpdate(ctx, db.GetPendingActionForUpdateParams{ActionRef: actionRef, EnterpriseID: enterpriseID})
		if err != nil || action.Status != "awaiting_confirmation" || time.Now().UTC().After(action.ExpiresAt.Time) || action.CreatorSubjectType != "user" || action.CreatorSubjectID != actorUUID {
			return ActionConfirmation{}, ErrActionUnavailable
		}
		if action.AuthorizationVersion != authorizationVersion {
			return ActionConfirmation{}, ErrActionInvalidated
		}
		plan, err := q.GetPendingActionPlan(ctx, db.GetPendingActionPlanParams{ActionRef: actionRef, EnterpriseID: enterpriseID})
		if err != nil || plan.AuthorizationVersion != authorizationVersion {
			return ActionConfirmation{}, ErrActionInvalidated
		}
		canonicalPlan, err := CanonicalJSON(plan.ImmutablePlan)
		if err != nil {
			return ActionConfirmation{}, ErrActionInvalidated
		}
		planHash := sha256.Sum256(canonicalPlan)
		if subtleEqual(planHash[:], plan.PlanHash) == false {
			return ActionConfirmation{}, ErrActionInvalidated
		}
		token, err := q.GetPendingActionTokenForUpdate(ctx, db.GetPendingActionTokenForUpdateParams{ActionRef: actionRef, EnterpriseID: enterpriseID})
		if err != nil || token.Status != "active" || time.Now().UTC().After(token.ExpiresAt.Time) {
			return ActionConfirmation{}, ErrActionUnavailable
		}
		plaintext, err := (actionCipher{key: service.Key}).decrypt(token.Nonce, token.Ciphertext, actionAAD(enterpriseID.String(), action.ID.String()))
		if err != nil || !verifyActionToken(string(plaintext), token.TokenHash) {
			clear(plaintext)
			return ActionConfirmation{}, ErrActionInvalidated
		}
		clear(plaintext)
		if revalidate != nil {
			impactHash, err := revalidate(ctx, q, action, canonicalPlan)
			if err != nil || !subtleEqual(impactHash, action.ImpactHash) {
				return ActionConfirmation{}, ErrActionInvalidated
			}
		}
		rows, err := q.ConsumePendingActionToken(ctx, db.ConsumePendingActionTokenParams{PendingActionID: action.ID, EnterpriseID: enterpriseID})
		if err != nil || rows != 1 {
			return ActionConfirmation{}, ErrActionUnavailable
		}
		if _, err := q.MarkPendingActionExecuting(ctx, db.MarkPendingActionExecutingParams{ID: action.ID, EnterpriseID: enterpriseID}); err != nil {
			return ActionConfirmation{}, ErrActionUnavailable
		}
		commitResult, err := commit(ctx, q, action, canonicalPlan)
		if err != nil {
			return ActionConfirmation{}, err
		}
		status := "succeeded"
		if commitResult.ErrorCode != "" {
			status = "failed"
		}
		result, err := q.FinishPendingAction(ctx, db.FinishPendingActionParams{ID: action.ID, EnterpriseID: enterpriseID, Status: status,
			ResultResourceType:    pgtype.Text{String: commitResult.ResourceType, Valid: commitResult.ResourceType != ""},
			ResultResourceID:      uuid.NullUUID{UUID: commitResult.ResourceID, Valid: commitResult.ResourceID != uuid.Nil},
			ResultResourceVersion: pgtype.Int8{Int64: commitResult.ResourceVersion, Valid: commitResult.ResourceVersion > 0}, ResultSummary: commitResult.Summary,
			ErrorCode: pgtype.Text{String: commitResult.ErrorCode, Valid: commitResult.ErrorCode != ""}})
		if err != nil {
			return ActionConfirmation{}, err
		}
		if err := appendResourceAudit(ctx, q, actorID, enterpriseID, "pending_action.confirm", action.ResourceType, action.ResourceID.UUID, map[string]any{"status": status}); err != nil {
			return ActionConfirmation{}, err
		}
		return ActionConfirmation{PendingAction: result}, nil
	})
}

// CanonicalJSON preserves JSON number precision while normalizing object order
// and insignificant whitespace for hashes stored alongside PostgreSQL jsonb.
func CanonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("multiple JSON values are not allowed")
	}
	return json.Marshal(value)
}

func randomReference(prefix string, bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func subtleEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var difference byte
	for i := range a {
		difference |= a[i] ^ b[i]
	}
	return difference == 0
}

func newResourceID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}

func appendResourceAudit(ctx context.Context, q *db.Queries, actorID string, enterpriseID uuid.UUID, action, resourceType string, resourceID uuid.UUID, details map[string]any) error {
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: "enterprise_user",
		ActorID: actorID, Action: action, ResourceType: resourceType, ResourceID: resourceID.String(), Result: "success", Details: details})
	return err
}
