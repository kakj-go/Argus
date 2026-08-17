package action

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/conversation"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type ExecutionTask struct {
	ExecutionID  uuid.UUID `json:"execution_id"`
	EnterpriseID uuid.UUID `json:"enterprise_id"`
}

type DeterministicExecutor interface {
	ExecutePendingAction(context.Context, *db.Queries, db.PendingAction) (resource.ActionCommitResult, error)
}

type Executor struct {
	Store            *postgres.Store
	Resources        DeterministicExecutor
	OneTimeResultKey []byte
}

func (executor Executor) Handle(ctx context.Context, task runtime.Task) error {
	if executor.Store == nil || executor.Resources == nil {
		return runtime.Error{ErrorCode: "ACTION_EXECUTOR_UNAVAILABLE", Cause: ErrUnavailable}
	}
	var payload ExecutionTask
	if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.ExecutionID == uuid.Nil || payload.EnterpriseID == uuid.Nil {
		return runtime.Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: ErrUnavailable}
	}
	return executor.Store.InTx(ctx, func(q *db.Queries) error {
		execution, err := q.GetExecution(ctx, db.GetExecutionParams{ID: payload.ExecutionID, EnterpriseID: payload.EnterpriseID})
		if err != nil {
			return err
		}
		if execution.Status == "succeeded" || execution.Status == "failed" || execution.Status == "cancelled" {
			return nil
		}
		if execution.Status == "result_unknown" {
			return executor.reconcileExecution(ctx, q, execution)
		}
		if execution.Status == "pending" {
			execution, err = q.ClaimExecution(ctx, db.ClaimExecutionParams{ID: execution.ID, EnterpriseID: execution.EnterpriseID})
			if err != nil {
				return err
			}
		}
		action, err := q.GetPendingActionByIDForUpdate(ctx, db.GetPendingActionByIDForUpdateParams{ID: execution.PendingActionID, EnterpriseID: execution.EnterpriseID})
		if err != nil {
			return err
		}
		if action.Status == "succeeded" || action.Status == "failed" {
			status := action.Status
			_, err = q.FinishExecution(ctx, db.FinishExecutionParams{ID: execution.ID, EnterpriseID: execution.EnterpriseID,
				Status: status, ResultRef: pgtype.Text{}, ErrorCode: action.ErrorCode})
			return err
		}
		if action.Status != "ready" || !authorizationCurrent(ctx, q, action) || !approvalCurrent(ctx, q, action) {
			_, _ = q.InvalidatePendingActionM4(ctx, db.InvalidatePendingActionM4Params{ID: action.ID, EnterpriseID: action.EnterpriseID,
				ErrorCode: pgtype.Text{String: "ACTION_INVALIDATED", Valid: true}})
			_, err = q.FinishExecution(ctx, db.FinishExecutionParams{ID: execution.ID, EnterpriseID: execution.EnterpriseID,
				Status: "failed", ErrorCode: pgtype.Text{String: "ACTION_INVALIDATED", Valid: true}})
			return err
		}
		result, err := executor.Resources.ExecutePendingAction(ctx, q, action)
		if err != nil {
			if errors.Is(err, resource.ErrActionInvalidated) || errors.Is(err, resource.ErrActionUnavailable) {
				return runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: err}
			}
			return err
		}
		if result.ConnectorCommandID.Valid {
			_, err = q.MarkExecutionResultUnknown(ctx, db.MarkExecutionResultUnknownParams{
				ID: execution.ID, EnterpriseID: execution.EnterpriseID, ConnectorCommandID: result.ConnectorCommandID,
			})
			return err
		}
		if result.Enrollment != nil {
			if action.CreatorSubjectType != "user" {
				return runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: ErrInvalidated}
			}
			creator, err := q.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{
				ID: action.CreatorSubjectID, EnterpriseID: action.EnterpriseID,
			})
			if err != nil || creator.Status != "active" {
				return runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: ErrInvalidated}
			}
			if err := storeOneTimeResult(ctx, q, executor.OneTimeResultKey, execution, creator.AuthorizationVersion, *result.Enrollment); err != nil {
				return err
			}
		}
		status := "succeeded"
		if result.ErrorCode != "" {
			status = "failed"
		}
		finished, err := q.FinishPendingAction(ctx, db.FinishPendingActionParams{ID: action.ID, EnterpriseID: action.EnterpriseID, Status: status,
			ResultResourceType:    pgtype.Text{String: result.ResourceType, Valid: result.ResourceType != ""},
			ResultResourceID:      uuid.NullUUID{UUID: result.ResourceID, Valid: result.ResourceID != uuid.Nil},
			ResultResourceVersion: pgtype.Int8{Int64: result.ResourceVersion, Valid: result.ResourceVersion > 0},
			ResultSummary:         result.Summary, ErrorCode: pgtype.Text{String: result.ErrorCode, Valid: result.ErrorCode != ""}})
		if err != nil {
			return err
		}
		_, err = q.FinishExecution(ctx, db.FinishExecutionParams{ID: execution.ID, EnterpriseID: execution.EnterpriseID,
			Status: status, ErrorCode: finished.ErrorCode})
		if err != nil {
			return err
		}
		if err = finishAutomationRun(ctx, q, finished); err != nil {
			return err
		}
		if execution.RunID.Valid {
			return enqueueVerify(ctx, q, execution, finished)
		}
		return nil
	})
}

func (executor Executor) reconcileExecution(ctx context.Context, q *db.Queries, execution db.Execution) error {
	if !execution.ConnectorCommandID.Valid {
		return nil
	}
	command, err := q.GetConnectorCommandByID(ctx, execution.ConnectorCommandID.UUID)
	if err != nil {
		return err
	}
	status, errorCode, terminal := reconciledCommandOutcome(command)
	if !terminal {
		return nil
	}
	action, err := q.GetPendingActionByIDForUpdate(ctx, db.GetPendingActionByIDForUpdateParams{
		ID: execution.PendingActionID, EnterpriseID: execution.EnterpriseID,
	})
	if err != nil {
		return err
	}
	finished, err := q.FinishPendingAction(ctx, db.FinishPendingActionParams{
		ID: action.ID, EnterpriseID: action.EnterpriseID, Status: status,
		ResultSummary: "Connector command " + command.CommandID + " reconciled", ErrorCode: errorCode,
	})
	if err != nil {
		return err
	}
	execution, err = q.FinishExecution(ctx, db.FinishExecutionParams{
		ID: execution.ID, EnterpriseID: execution.EnterpriseID, Status: status, ErrorCode: errorCode,
	})
	if err != nil {
		return err
	}
	if err = finishAutomationRun(ctx, q, finished); err != nil {
		return err
	}
	if execution.RunID.Valid {
		return enqueueVerify(ctx, q, execution, finished)
	}
	return nil
}

func finishAutomationRun(ctx context.Context, q *db.Queries, action db.PendingAction) error {
	status := action.Status
	if status != "succeeded" && status != "failed" {
		return nil
	}
	return q.FinishAutomationRunByPendingAction(ctx, db.FinishAutomationRunByPendingActionParams{PendingActionID: uuid.NullUUID{UUID: action.ID, Valid: true},
		EnterpriseID: action.EnterpriseID, Status: status, ErrorCode: action.ErrorCode})
}

func reconciledCommandOutcome(command db.ConnectorCommand) (string, pgtype.Text, bool) {
	switch command.Status {
	case "succeeded":
		return "succeeded", pgtype.Text{}, true
	case "failed", "timed_out", "expired":
		code := command.ErrorCode
		if !code.Valid {
			code = pgtype.Text{String: "EXECUTION_RESULT_UNKNOWN", Valid: true}
		}
		return "failed", code, true
	default:
		return "", pgtype.Text{}, false
	}
}

type Reconciler struct {
	Executor Executor
	Poll     time.Duration
	Logger   *slog.Logger
}

func (reconciler Reconciler) Run(ctx context.Context) error {
	if reconciler.Poll <= 0 {
		reconciler.Poll = 2 * time.Second
	}
	if reconciler.Logger == nil {
		reconciler.Logger = slog.Default()
	}
	ticker := time.NewTicker(reconciler.Poll)
	defer ticker.Stop()
	for {
		items, err := reconciler.Executor.Store.Queries.ListUncertainExecutions(ctx, 100)
		if err != nil {
			reconciler.Logger.Error("execution reconciliation scan failed", "error", err)
		} else {
			for _, item := range items {
				err := reconciler.Executor.Store.InTx(ctx, func(q *db.Queries) error {
					current, err := q.GetExecution(ctx, db.GetExecutionParams{ID: item.ID, EnterpriseID: item.EnterpriseID})
					if err != nil || current.Status != "result_unknown" {
						return err
					}
					return reconciler.Executor.reconcileExecution(ctx, q, current)
				})
				if err != nil {
					reconciler.Logger.Warn("execution reconciliation failed", "execution_id", item.ID, "error", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func approvalCurrent(ctx context.Context, q *db.Queries, action db.PendingAction) bool {
	if len(action.PolicySnapshotHash) == 0 {
		return action.CreatorSubjectType == "user"
	}
	request, err := q.GetApprovalRequestByAction(ctx, db.GetApprovalRequestByActionParams{ActionRef: action.ActionRef, EnterpriseID: action.EnterpriseID})
	if err != nil || request.Status != "approved" || request.ExpiresAt.Time.Before(time.Now().UTC()) {
		return false
	}
	requirements, err := q.ListApprovalRequirements(ctx, db.ListApprovalRequirementsParams{ApprovalRequestID: request.ID, EnterpriseID: action.EnterpriseID})
	if err != nil || len(requirements) == 0 {
		return false
	}
	policies := make([]db.ApprovalPolicy, 0, len(requirements))
	plan, err := q.GetPendingActionPlan(ctx, db.GetPendingActionPlanParams{ActionRef: action.ActionRef, EnterpriseID: action.EnterpriseID})
	if err != nil {
		return false
	}
	for _, requirement := range requirements {
		if requirement.Status != "approved" {
			return false
		}
		policy, err := q.GetApprovalPolicy(ctx, db.GetApprovalPolicyParams{ID: requirement.PolicyID, EnterpriseID: action.EnterpriseID})
		if err != nil || !policy.Enabled || policy.Version != requirement.PolicyVersion || !equalHash(hashPolicy(policy), requirement.PolicyHash) {
			return false
		}
		policies = append(policies, policy)
	}
	policies, err = matchingApprovalPolicies(ctx, q, action, plan, policies)
	if err != nil || len(policies) != len(requirements) {
		return false
	}
	return equalHash(hashPolicies(policies), action.PolicySnapshotHash)
}

func authorizationCurrent(ctx context.Context, q *db.Queries, action db.PendingAction) bool {
	switch action.CreatorSubjectType {
	case "user":
		user, err := q.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: action.CreatorSubjectID, EnterpriseID: action.EnterpriseID})
		return err == nil && user.Status == "active" && user.AuthorizationVersion == action.AuthorizationVersion
	case "service_account":
		account, err := q.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: action.CreatorSubjectID, EnterpriseID: action.EnterpriseID})
		return err == nil && account.Status == "active" && account.AuthorizationVersion == action.AuthorizationVersion
	default:
		return false
	}
}

func enqueueVerify(ctx context.Context, q *db.Queries, execution db.Execution, action db.PendingAction) error {
	payload, _ := json.Marshal(conversation.AgentTask{RunID: execution.RunID.UUID, EnterpriseID: execution.EnterpriseID,
		Reason: "execution_verify", ExecutionRef: execution.ExecutionRef, ActionRef: action.ActionRef})
	_, err := q.CreateRuntimeTask(ctx, db.CreateRuntimeTaskParams{ID: newID(), EnterpriseID: uuid.NullUUID{UUID: execution.EnterpriseID, Valid: true},
		Queue: "agent", RunID: execution.RunID, Payload: payload, MaxAttempts: 5, AvailableAt: pgtype.Timestamptz{Time: action.UpdatedAt.Time, Valid: true}})
	return err
}
