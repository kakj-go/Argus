package action

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrUnavailable        = errors.New("action unavailable")
	ErrInvalidated        = errors.New("action invalidated")
	ErrApprovalIneligible = errors.New("approval actor is not eligible")
	ErrApprovalRequired   = errors.New("approval policy is required")
)

const (
	defaultBindingTTL  = 15 * time.Minute
	defaultApprovalTTL = 24 * time.Hour
)

// ResourceExecutor is deliberately an internal-only interface. The immutable
// plan and commit token never cross an HTTP, SSE, model, or MCP boundary.
type ResourceExecutor interface {
	RevalidatePendingAction(context.Context, *db.Queries, db.PendingAction, json.RawMessage) ([]byte, error)
	CommitPendingAction(context.Context, *db.Queries, db.PendingAction, json.RawMessage) (resource.ActionCommitResult, error)
}

type Service struct {
	Store            *postgres.Store
	Idempotency      postgres.Idempotency
	Resources        ResourceExecutor
	BindingTTL       time.Duration
	OneTimeResultKey []byte
}

type Confirmation struct {
	PendingAction   db.PendingAction    `json:"pending_action"`
	ApprovalRequest *db.ApprovalRequest `json:"approval_request,omitempty"`
	Execution       *db.Execution       `json:"execution,omitempty"`
}

type Decision struct {
	Request   db.ApprovalRequest
	Action    db.PendingAction
	Execution *db.Execution
}

func (service Service) StartAutomationApproval(ctx context.Context, enterpriseID uuid.UUID, actionRef string) (Confirmation, error) {
	var result Confirmation
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		action, plan, err := service.lockAndRevalidate(ctx, q, enterpriseID, actionRef, "awaiting_approval", 0)
		if err != nil || action.CreatorSubjectType != "service_account" {
			return ErrInvalidated
		}
		account, err := q.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: action.CreatorSubjectID, EnterpriseID: enterpriseID})
		if err != nil || account.Status != "active" || account.AuthorizationVersion != action.AuthorizationVersion {
			return ErrInvalidated
		}
		policies, err := q.ListMatchingApprovalPolicies(ctx, approvalPolicyQuery(enterpriseID, action, plan))
		if err != nil {
			return err
		}
		policies, err = matchingApprovalPolicies(ctx, q, action, plan, policies)
		if err != nil {
			return err
		}
		if len(policies) == 0 {
			_, _ = q.InvalidatePendingActionM4(ctx, db.InvalidatePendingActionM4Params{ID: action.ID, EnterpriseID: enterpriseID,
				ErrorCode: pgtype.Text{String: "APPROVAL_POLICY_REQUIRED", Valid: true}})
			return ErrApprovalRequired
		}
		_ = plan
		approval, err := service.createApprovalRequest(ctx, q, action, policies)
		if err != nil {
			return err
		}
		result = Confirmation{PendingAction: action, ApprovalRequest: &approval}
		return service.audit(ctx, q, action.CreatorSubjectID.String(), enterpriseID, "automation.pending_action", action, "awaiting_approval")
	})
	return result, err
}

func (service Service) Confirm(ctx context.Context, actorID, requestID string, enterpriseID uuid.UUID, authorizationVersion int64, actionRef, idempotencyKey string) (Confirmation, error) {
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return Confirmation{}, ErrUnavailable
	}
	if requestID == "" {
		requestID = idempotencyKey
	}
	request := struct {
		ActionRef            string `json:"action_ref"`
		AuthorizationVersion int64  `json:"authorization_version"`
	}{actionRef, authorizationVersion}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "pending_action.confirm", idempotencyKey, request, 200,
		func(q *db.Queries) (Confirmation, error) {
			action, plan, err := service.lockAndRevalidate(ctx, q, enterpriseID, actionRef, "awaiting_confirmation", authorizationVersion)
			if err != nil || action.CreatorSubjectType != "user" || action.CreatorSubjectID != actor {
				return Confirmation{}, ErrInvalidated
			}
			if _, err := q.CreateUserConfirmation(ctx, db.CreateUserConfirmationParams{ID: newID(), PendingActionID: action.ID,
				EnterpriseID: enterpriseID, ActorUserID: actor, AuthorizationVersion: authorizationVersion}); err != nil {
				return Confirmation{}, err
			}
			bindingRef, err := randomRef("bind_")
			if err != nil {
				return Confirmation{}, err
			}
			ttl := service.BindingTTL
			if ttl <= 0 {
				ttl = defaultBindingTTL
			}
			if _, err := q.CreateActionBinding(ctx, db.CreateActionBindingParams{ID: newID(), BindingRef: bindingRef,
				PendingActionID: action.ID, EnterpriseID: enterpriseID, ActorUserID: uuid.NullUUID{UUID: actor, Valid: true},
				Action: "confirm", RequestID: requestID, ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(ttl), Valid: true}}); err != nil {
				return Confirmation{}, err
			}
			policies, err := q.ListMatchingApprovalPolicies(ctx, approvalPolicyQuery(enterpriseID, action, plan))
			if err != nil {
				return Confirmation{}, err
			}
			policies, err = matchingApprovalPolicies(ctx, q, action, plan, policies)
			if err != nil {
				return Confirmation{}, err
			}
			if len(policies) == 0 {
				ready, err := q.MarkPendingActionReady(ctx, db.MarkPendingActionReadyParams{ID: action.ID, EnterpriseID: enterpriseID})
				if err != nil {
					return Confirmation{}, err
				}
				execution, err := service.createExecutionTask(ctx, q, ready, "confirm:"+idempotencyKey)
				if err != nil {
					return Confirmation{}, err
				}
				_ = plan // The plan was verified without exposing it to the response.
				return Confirmation{PendingAction: ready, Execution: &execution}, service.audit(ctx, q, actorID, enterpriseID, "pending_action.confirm", action, "ready")
			}

			requirementHash := hashPolicies(policies)
			action, err = q.SetPendingActionPolicySnapshot(ctx, db.SetPendingActionPolicySnapshotParams{ID: action.ID, EnterpriseID: enterpriseID, PolicySnapshotHash: requirementHash})
			if err != nil {
				return Confirmation{}, err
			}
			expiresAt := time.Now().UTC().Add(defaultApprovalTTL)
			for _, policy := range policies {
				candidate := time.Now().UTC().Add(time.Duration(policy.ExpiresAfterSeconds) * time.Second)
				if candidate.Before(expiresAt) {
					expiresAt = candidate
				}
			}
			approval, err := q.CreateApprovalRequest(ctx, db.CreateApprovalRequestParams{ID: newID(), PendingActionID: action.ID,
				EnterpriseID: enterpriseID, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
			if err != nil {
				return Confirmation{}, err
			}
			for _, policy := range policies {
				if _, err := q.CreateApprovalRequirementSnapshot(ctx, db.CreateApprovalRequirementSnapshotParams{ID: newID(), ApprovalRequestID: approval.ID,
					EnterpriseID: enterpriseID, PolicyID: policy.ID, PolicyVersion: policy.Version, MinimumApprovers: policy.MinimumApprovers,
					SeparationOfDuty: policy.SeparationOfDuty, ApproverRoleIds: policy.ApproverRoleIds, PolicyHash: hashPolicy(policy)}); err != nil {
					return Confirmation{}, err
				}
			}
			action, err = q.MarkPendingActionAwaitingApproval(ctx, db.MarkPendingActionAwaitingApprovalParams{ID: action.ID, EnterpriseID: enterpriseID})
			if err != nil {
				return Confirmation{}, err
			}
			return Confirmation{PendingAction: action, ApprovalRequest: &approval}, service.audit(ctx, q, actorID, enterpriseID, "pending_action.confirm", action, "awaiting_approval")
		})
}

func (service Service) Decide(ctx context.Context, actorID string, enterpriseID, requestID uuid.UUID, decision, reason, idempotencyKey string) (Decision, error) {
	actor, err := uuid.Parse(actorID)
	if err != nil || (decision != "approved" && decision != "rejected") {
		return Decision{}, ErrApprovalIneligible
	}
	request := struct {
		RequestID uuid.UUID `json:"request_id"`
		Decision  string    `json:"decision"`
		Reason    string    `json:"reason"`
	}{requestID, decision, reason}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "approval.decide", idempotencyKey, request, 200,
		func(q *db.Queries) (Decision, error) {
			approval, err := q.GetApprovalRequestForUpdate(ctx, db.GetApprovalRequestForUpdateParams{ID: requestID, EnterpriseID: enterpriseID})
			if err != nil || approval.Status != "pending" || time.Now().UTC().After(approval.ExpiresAt.Time) {
				return Decision{}, ErrUnavailable
			}
			action, err := q.GetPendingActionByIDForUpdate(ctx, db.GetPendingActionByIDForUpdateParams{ID: approval.PendingActionID, EnterpriseID: enterpriseID})
			if err != nil || action.Status != "awaiting_approval" {
				return Decision{}, ErrInvalidated
			}
			requirements, err := q.ListApprovalRequirements(ctx, db.ListApprovalRequirementsParams{ApprovalRequestID: approval.ID, EnterpriseID: enterpriseID})
			if err != nil || len(requirements) == 0 || !eligibleForAny(ctx, q, approval.ID, enterpriseID, actor, action.CreatorSubjectID, requirements) {
				return Decision{}, ErrApprovalIneligible
			}
			if _, err := q.CreateApprovalDecision(ctx, db.CreateApprovalDecisionParams{ID: newID(), ApprovalRequestID: approval.ID,
				EnterpriseID: enterpriseID, ActorUserID: actor, Decision: decision, Reason: reason}); err != nil {
				return Decision{}, err
			}
			if decision == "rejected" {
				approval, err = q.UpdateApprovalRequestStatus(ctx, db.UpdateApprovalRequestStatusParams{ID: approval.ID, EnterpriseID: enterpriseID, Status: "rejected"})
				if err != nil {
					return Decision{}, err
				}
				action, err = q.RejectPendingAction(ctx, db.RejectPendingActionParams{ID: action.ID, EnterpriseID: enterpriseID})
				if err != nil {
					return Decision{}, err
				}
				if err := q.FinishAutomationRunByPendingAction(ctx, db.FinishAutomationRunByPendingActionParams{PendingActionID: uuid.NullUUID{UUID: action.ID, Valid: true},
					EnterpriseID: enterpriseID, Status: "failed", ErrorCode: pgtype.Text{String: "APPROVAL_REJECTED", Valid: true}}); err != nil {
					return Decision{}, err
				}
				return Decision{Request: approval, Action: action}, service.audit(ctx, q, actorID, enterpriseID, "approval.reject", action, "rejected")
			}

			allApproved := true
			for _, requirement := range requirements {
				count, err := q.CountEligibleApprovalDecisions(ctx, db.CountEligibleApprovalDecisionsParams{ApprovalRequestID: approval.ID,
					EnterpriseID: enterpriseID, Column3: requirement.ApproverRoleIds, Column4: requirement.SeparationOfDuty, Column5: action.CreatorSubjectID})
				if err != nil {
					return Decision{}, err
				}
				status := "pending"
				if count >= requirement.MinimumApprovers {
					status = "approved"
				} else {
					allApproved = false
				}
				if _, err := q.UpdateApprovalRequirementStatus(ctx, db.UpdateApprovalRequirementStatusParams{ID: requirement.ID,
					EnterpriseID: enterpriseID, ApprovedCount: count, Status: status}); err != nil {
					return Decision{}, err
				}
			}
			if !allApproved {
				return Decision{Request: approval, Action: action}, service.audit(ctx, q, actorID, enterpriseID, "approval.approve", action, "pending")
			}
			approval, err = q.UpdateApprovalRequestStatus(ctx, db.UpdateApprovalRequestStatusParams{ID: approval.ID, EnterpriseID: enterpriseID, Status: "approved"})
			if err != nil {
				return Decision{}, err
			}
			action, err = q.MarkPendingActionReady(ctx, db.MarkPendingActionReadyParams{ID: action.ID, EnterpriseID: enterpriseID})
			if err != nil {
				return Decision{}, err
			}
			execution, err := service.createExecutionTask(ctx, q, action, "approval:"+approval.ID.String())
			if err != nil {
				return Decision{}, err
			}
			return Decision{Request: approval, Action: action, Execution: &execution}, service.audit(ctx, q, actorID, enterpriseID, "approval.approve", action, "approved")
		})
}

func (service Service) lockAndRevalidate(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, actionRef, status string, authorizationVersion int64) (db.PendingAction, db.PendingActionPlan, error) {
	action, err := q.GetPendingActionForUpdate(ctx, db.GetPendingActionForUpdateParams{ActionRef: actionRef, EnterpriseID: enterpriseID})
	if err != nil || action.Status != status || time.Now().UTC().After(action.ExpiresAt.Time) || (authorizationVersion > 0 && action.AuthorizationVersion != authorizationVersion) {
		return db.PendingAction{}, db.PendingActionPlan{}, ErrInvalidated
	}
	plan, err := q.GetPendingActionPlan(ctx, db.GetPendingActionPlanParams{ActionRef: actionRef, EnterpriseID: enterpriseID})
	if err != nil || (authorizationVersion > 0 && plan.AuthorizationVersion != authorizationVersion) || !validPlan(plan) {
		return db.PendingAction{}, db.PendingActionPlan{}, ErrInvalidated
	}
	if service.Resources == nil {
		return db.PendingAction{}, db.PendingActionPlan{}, ErrUnavailable
	}
	impactHash, err := service.Resources.RevalidatePendingAction(ctx, q, action, plan.ImmutablePlan)
	if err != nil || !equalHash(impactHash, action.ImpactHash) {
		return db.PendingAction{}, db.PendingActionPlan{}, ErrInvalidated
	}
	return action, plan, nil
}

func (service Service) createApprovalRequest(ctx context.Context, q *db.Queries, action db.PendingAction, policies []db.ApprovalPolicy) (db.ApprovalRequest, error) {
	requirementHash := hashPolicies(policies)
	if _, err := q.SetPendingActionPolicySnapshot(ctx, db.SetPendingActionPolicySnapshotParams{ID: action.ID, EnterpriseID: action.EnterpriseID, PolicySnapshotHash: requirementHash}); err != nil {
		return db.ApprovalRequest{}, err
	}
	expiresAt := time.Now().UTC().Add(defaultApprovalTTL)
	for _, policy := range policies {
		candidate := time.Now().UTC().Add(time.Duration(policy.ExpiresAfterSeconds) * time.Second)
		if candidate.Before(expiresAt) {
			expiresAt = candidate
		}
	}
	approval, err := q.CreateApprovalRequest(ctx, db.CreateApprovalRequestParams{ID: newID(), PendingActionID: action.ID,
		EnterpriseID: action.EnterpriseID, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
	if err != nil {
		return db.ApprovalRequest{}, err
	}
	for _, policy := range policies {
		if _, err := q.CreateApprovalRequirementSnapshot(ctx, db.CreateApprovalRequirementSnapshotParams{ID: newID(), ApprovalRequestID: approval.ID,
			EnterpriseID: action.EnterpriseID, PolicyID: policy.ID, PolicyVersion: policy.Version, MinimumApprovers: policy.MinimumApprovers,
			SeparationOfDuty: policy.SeparationOfDuty, ApproverRoleIds: policy.ApproverRoleIds, PolicyHash: hashPolicy(policy)}); err != nil {
			return db.ApprovalRequest{}, err
		}
	}
	return approval, nil
}

func (service Service) createExecutionTask(ctx context.Context, q *db.Queries, action db.PendingAction, idempotencyKey string) (db.Execution, error) {
	executionRef, err := randomRef("exec_")
	if err != nil {
		return db.Execution{}, err
	}
	execution, err := q.CreateExecution(ctx, db.CreateExecutionParams{ID: newID(), ExecutionRef: executionRef,
		PendingActionID: action.ID, EnterpriseID: action.EnterpriseID, RunID: action.RunID, IdempotencyKey: idempotencyKey})
	if err != nil {
		return db.Execution{}, err
	}
	if err := q.MarkAutomationRunExecuting(ctx, db.MarkAutomationRunExecutingParams{PendingActionID: uuid.NullUUID{UUID: action.ID, Valid: true}, EnterpriseID: action.EnterpriseID}); err != nil {
		return db.Execution{}, err
	}
	payload, _ := json.Marshal(ExecutionTask{ExecutionID: execution.ID, EnterpriseID: action.EnterpriseID})
	_, err = q.CreateRuntimeTask(ctx, db.CreateRuntimeTaskParams{ID: newID(), EnterpriseID: uuid.NullUUID{UUID: action.EnterpriseID, Valid: true},
		Queue: "action", RunID: action.RunID, Payload: payload, MaxAttempts: 5, AvailableAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}})
	return execution, err
}

func eligibleForAny(ctx context.Context, q *db.Queries, requestID, enterpriseID, actor, creator uuid.UUID, requirements []db.ApprovalRequirementSnapshot) bool {
	_ = requestID
	for _, requirement := range requirements {
		if requirement.SeparationOfDuty && actor == creator {
			continue
		}
		eligible, err := q.IsApprovalActorEligible(ctx, db.IsApprovalActorEligibleParams{EnterpriseID: enterpriseID,
			SubjectID: actor, Column3: requirement.ApproverRoleIds})
		if err == nil && eligible {
			return true
		}
	}
	return false
}

func validPlan(plan db.PendingActionPlan) bool {
	canonical, err := canonicalJSON(plan.ImmutablePlan)
	if err != nil {
		return false
	}
	hash := sha256.Sum256(canonical)
	return equalHash(hash[:], plan.PlanHash)
}

func hashPolicies(policies []db.ApprovalPolicy) []byte {
	hashes := make([]string, 0, len(policies))
	for _, policy := range policies {
		hashes = append(hashes, fmt.Sprintf("%x", hashPolicy(policy)))
	}
	sort.Strings(hashes)
	value, _ := json.Marshal(hashes)
	hash := sha256.Sum256(value)
	return hash[:]
}

func hashPolicy(policy db.ApprovalPolicy) []byte {
	value, _ := json.Marshal(struct {
		ID         uuid.UUID   `json:"id"`
		Version    int64       `json:"version"`
		Minimum    int32       `json:"minimum"`
		Separation bool        `json:"separation"`
		Roles      []uuid.UUID `json:"roles"`
	}{policy.ID, policy.Version, policy.MinimumApprovers, policy.SeparationOfDuty, policy.ApproverRoleIds})
	hash := sha256.Sum256(value)
	return hash[:]
}

func (service Service) audit(ctx context.Context, q *db.Queries, actorID string, enterpriseID uuid.UUID, actionName string, action db.PendingAction, result string) error {
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true},
		ActorType: "enterprise_user", ActorID: actorID, Action: actionName, ResourceType: "pending_action", ResourceID: action.ID.String(),
		Result: "success", Details: map[string]any{"status": result, "action_ref": action.ActionRef}})
	return err
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}

func randomRef(prefix string) (string, error) {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
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

func equalHash(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var diff byte
	for i := range left {
		diff |= left[i] ^ right[i]
	}
	return diff == 0
}
