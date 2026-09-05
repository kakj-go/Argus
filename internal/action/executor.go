package action

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/kakj-go/Argus/internal/conversation"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
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
			if resourceNameConflict(err) {
				// A preview check gives an early user-facing error; the unique index
				// remains the race-proof authority. A uniqueness conflict is permanent
				// for this immutable action plan and must not spend the retry budget.
				return runtime.Error{ErrorCode: "RESOURCE_NAME_CONFLICT", Cause: err, Permanent: true}
			}
			return err
		}
		if result.ConnectorCommandID.Valid {
			_, err = q.MarkExecutionResultUnknown(ctx, db.MarkExecutionResultUnknownParams{
				ID: execution.ID, EnterpriseID: execution.EnterpriseID, ConnectorCommandID: result.ConnectorCommandID,
			})
			return err
		}
		if result.TelemetryOperationID.Valid {
			_, err = q.MarkExecutionTelemetryResultUnknown(ctx, db.MarkExecutionTelemetryResultUnknownParams{
				ID: execution.ID, EnterpriseID: execution.EnterpriseID, TelemetryCollectorOperationID: result.TelemetryOperationID,
			})
			return err
		}
		if result.ConnectorInstallOperationID.Valid {
			_, err = q.MarkExecutionConnectorInstallResultUnknown(ctx, db.MarkExecutionConnectorInstallResultUnknownParams{
				ID: execution.ID, EnterpriseID: execution.EnterpriseID,
				ConnectorInstallOperationID: result.ConnectorInstallOperationID,
			})
			return err
		}
		if result.OneTimeCommand != nil {
			if action.CreatorSubjectType != "user" {
				return runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: ErrInvalidated}
			}
			creator, err := q.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{
				ID: action.CreatorSubjectID, EnterpriseID: action.EnterpriseID,
			})
			if err != nil || creator.Status != "active" {
				return runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: ErrInvalidated}
			}
			if err := storeOneTimeResult(ctx, q, executor.OneTimeResultKey, execution, creator.AuthorizationVersion,
				*result.OneTimeCommand, result.OneTimeResultKind); err != nil {
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
		if execution.RunID.Valid {
			return enqueueVerify(ctx, q, execution, finished)
		}
		return nil
	})
}

// HandleExhausted converges the public action state after the runtime queue has
// spent its retry budget. A failed deterministic commit is rolled back, so the
// execution normally remains pending and the action remains ready here. The
// transition below is a new transaction and prevents clients from waiting on a
// state that no worker will process again.
func (executor Executor) HandleExhausted(ctx context.Context, task runtime.Task, cause error) error {
	if executor.Store == nil {
		return nil
	}
	var payload ExecutionTask
	if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.ExecutionID == uuid.Nil || payload.EnterpriseID == uuid.Nil {
		return nil
	}
	errorCode := executionFailureCode(cause)
	return executor.Store.InTx(ctx, func(q *db.Queries) error {
		execution, err := q.GetExecution(ctx, db.GetExecutionParams{ID: payload.ExecutionID, EnterpriseID: payload.EnterpriseID})
		if err != nil {
			return err
		}
		if execution.Status == "succeeded" || execution.Status == "failed" || execution.Status == "cancelled" {
			return nil
		}
		action, err := q.GetPendingActionByIDForUpdate(ctx, db.GetPendingActionByIDForUpdateParams{
			ID: execution.PendingActionID, EnterpriseID: execution.EnterpriseID,
		})
		if err != nil {
			return err
		}
		code := pgtype.Text{String: errorCode, Valid: true}
		if errorCode == "ACTION_INVALIDATED" && action.Status == "ready" {
			action, err = q.InvalidatePendingActionM4(ctx, db.InvalidatePendingActionM4Params{
				ID: action.ID, EnterpriseID: action.EnterpriseID, ErrorCode: code,
			})
			if err != nil {
				return err
			}
		} else {
			if action.Status == "ready" {
				action, err = q.MarkPendingActionExecutingM4(ctx, db.MarkPendingActionExecutingM4Params{
					ID: action.ID, EnterpriseID: action.EnterpriseID,
				})
				if err != nil {
					return err
				}
			}
			if action.Status == "executing" {
				action, err = q.FinishPendingAction(ctx, db.FinishPendingActionParams{
					ID: action.ID, EnterpriseID: action.EnterpriseID, Status: "failed",
					ResultSummary: "Action execution attempts exhausted", ErrorCode: code,
				})
				if err != nil {
					return err
				}
			}
		}
		if action.ErrorCode.Valid {
			code = action.ErrorCode
		}
		execution, err = q.FinishExecution(ctx, db.FinishExecutionParams{
			ID: execution.ID, EnterpriseID: execution.EnterpriseID, Status: "failed", ErrorCode: code,
		})
		if err != nil {
			return err
		}
		if execution.RunID.Valid {
			return enqueueVerify(ctx, q, execution, action)
		}
		return nil
	})
}

func executionFailureCode(cause error) string {
	type coded interface{ Code() string }
	var value coded
	if errors.As(cause, &value) {
		switch value.Code() {
		case "ACTION_INVALIDATED", "RESOURCE_NAME_CONFLICT":
			return value.Code()
		}
	}
	return "ACTION_EXECUTION_FAILED"
}

func resourceNameConflict(cause error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(cause, &postgresError) || postgresError.Code != "23505" {
		return false
	}
	switch postgresError.ConstraintName {
	case "bastion_scopes_name_unique", "hosts_name_unique":
		return true
	default:
		return false
	}
}

func (executor Executor) reconcileExecution(ctx context.Context, q *db.Queries, execution db.Execution) error {
	if execution.ConnectorInstallOperationID.Valid {
		operation, err := q.GetConnectorInstallOperation(ctx, db.GetConnectorInstallOperationParams{
			ID: execution.ConnectorInstallOperationID.UUID, EnterpriseID: execution.EnterpriseID,
		})
		if err != nil {
			return err
		}
		status, errorCode, terminal, err := reconciledConnectorInstallOutcome(ctx, q, operation)
		if err != nil || !terminal {
			return err
		}
		scope, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: operation.BastionScopeID, EnterpriseID: operation.EnterpriseID})
		if err != nil {
			return err
		}
		return executor.finishReconciledExecution(ctx, q, execution, status, errorCode,
			"Connector install operation "+operation.ID.String()+" reconciled", "bastion_scope", scope.ID, scope.ResourceVersion)
	}
	if execution.TelemetryCollectorOperationID.Valid {
		operation, err := q.GetTelemetryCollectorOperation(ctx, db.GetTelemetryCollectorOperationParams{
			ID: execution.TelemetryCollectorOperationID.UUID, EnterpriseID: execution.EnterpriseID,
		})
		if err != nil {
			return err
		}
		status, errorCode, terminal := reconciledTelemetryOutcome(operation)
		if !terminal {
			return nil
		}
		return executor.finishReconciledExecution(ctx, q, execution, status, errorCode,
			"Collector operation "+operation.ID.String()+" reconciled", "", uuid.Nil, 0)
	}
	if !execution.ConnectorCommandID.Valid {
		return nil
	}
	command, err := q.GetConnectorCommandByID(ctx, execution.ConnectorCommandID.UUID)
	if err != nil {
		return err
	}
	status, errorCode, terminal, err := reconciledConnectorCommandOutcome(ctx, q, command)
	if err != nil {
		return err
	}
	if !terminal {
		return nil
	}
	return executor.finishReconciledExecution(ctx, q, execution, status, errorCode,
		"Connector command "+command.CommandID+" reconciled", "", uuid.Nil, 0)
}

func reconciledConnectorCommandOutcome(ctx context.Context, q *db.Queries, command db.ConnectorCommand) (string, pgtype.Text, bool, error) {
	status, errorCode, terminal := reconciledCommandOutcome(command)
	if !terminal || status != "succeeded" || command.CommandType != "collector_management" {
		return status, errorCode, terminal, nil
	}
	var request connectorv1.CollectorManagementCommand
	if protojson.Unmarshal(command.Payload, &request) != nil {
		return "failed", pgtype.Text{String: "COLLECTOR_COMMAND_INVALID", Valid: true}, true, nil
	}
	transport := request.GetTransport()
	if request.GetOperation() == "uninstall" || (transport != "executor_tunnel" && transport != "bastion_tunnel") {
		return status, errorCode, terminal, nil
	}
	collectorID, parseErr := uuid.Parse(request.GetCollectorId())
	if parseErr != nil {
		return "failed", pgtype.Text{String: "COLLECTOR_COMMAND_INVALID", Valid: true}, true, nil
	}
	tunnel, err := q.GetTelemetryTunnelByCollector(ctx, collectorID)
	if err != nil {
		return "", pgtype.Text{}, false, err
	}
	if tunnel.EnterpriseID != command.EnterpriseID || tunnel.Transport != transport {
		return "failed", pgtype.Text{String: "COLLECTOR_ROUTE_INVALID", Valid: true}, true, nil
	}
	if tunnel.Status == "established" {
		if _, err = q.MarkTelemetryRouteActive(ctx, db.MarkTelemetryRouteActiveParams{
			CollectorID: collectorID, EnterpriseID: command.EnterpriseID}); err != nil {
			return "", pgtype.Text{}, false, err
		}
		if _, err = q.FinalizeCollectorClaimMigrations(ctx, db.FinalizeCollectorClaimMigrationsParams{
			EnterpriseID: command.EnterpriseID, CollectorID: collectorID}); err != nil {
			return "", pgtype.Text{}, false, err
		}
		return "succeeded", pgtype.Text{}, true, nil
	}
	if code, failed := terminalTelemetryTunnelFailure(tunnel); failed {
		_, _ = q.ApplyCollectorOperationFailure(ctx, db.ApplyCollectorOperationFailureParams{
			ID: collectorID, EnterpriseID: command.EnterpriseID, Status: "degraded"})
		_, _ = q.RollbackCollectorClaimMigrations(ctx, db.RollbackCollectorClaimMigrationsParams{
			EnterpriseID: command.EnterpriseID, CollectorID: collectorID})
		_, _ = q.MarkTelemetryRouteDegraded(ctx, db.MarkTelemetryRouteDegradedParams{
			CollectorID: collectorID, EnterpriseID: command.EnterpriseID})
		return "failed", pgtype.Text{String: code, Valid: true}, true, nil
	}
	return "", pgtype.Text{}, false, nil
}

func terminalTelemetryTunnelFailure(tunnel db.TelemetryTunnel) (string, bool) {
	switch tunnel.LastDropReason {
	case "loopback_port_conflict":
		return "COLLECTOR_LOOPBACK_PORT_CONFLICT", true
	case "tunnel_forward_target_unconfigured":
		return "TUNNEL_FORWARD_TARGET_UNCONFIGURED", true
	case "tunnel_quota_exceeded":
		return "TUNNEL_QUOTA_EXCEEDED", true
	case "credential_revoked", "credential_unavailable":
		return "CREDENTIAL_UNAVAILABLE", true
	case "host_key_changed":
		return "COLLECTOR_TARGET_HOST_KEY_CHANGED", true
	}
	// down/degraded 是长期 desired 隧道的可恢复状态。监督器会持续按上限
	// 30 秒退避重建，因此不能用任意重试次数把动作提前终结；动作自身的
	// expires_at 负责界定结果未知。removed 才是明确的终态事实。
	if tunnel.Status == "removed" {
		return "COLLECTOR_MANAGEMENT_FAILED", true
	}
	return "", false
}

func (executor Executor) finishReconciledExecution(ctx context.Context, q *db.Queries, execution db.Execution, status string, errorCode pgtype.Text, summary, resourceType string, resourceID uuid.UUID, resourceVersion int64) error {
	action, err := q.GetPendingActionByIDForUpdate(ctx, db.GetPendingActionByIDForUpdateParams{
		ID: execution.PendingActionID, EnterpriseID: execution.EnterpriseID,
	})
	if err != nil {
		return err
	}
	finished, err := q.FinishPendingAction(ctx, db.FinishPendingActionParams{
		ID: action.ID, EnterpriseID: action.EnterpriseID, Status: status,
		ResultSummary: summary, ErrorCode: errorCode,
		ResultResourceType:    pgtype.Text{String: resourceType, Valid: resourceType != ""},
		ResultResourceID:      uuid.NullUUID{UUID: resourceID, Valid: resourceID != uuid.Nil},
		ResultResourceVersion: pgtype.Int8{Int64: resourceVersion, Valid: resourceVersion > 0},
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
	if execution.RunID.Valid {
		return enqueueVerify(ctx, q, execution, finished)
	}
	return nil
}

func reconciledConnectorInstallOutcome(ctx context.Context, q *db.Queries, operation db.ConnectorInstallOperation) (string, pgtype.Text, bool, error) {
	switch operation.Status {
	case "succeeded":
		if operation.Stage != "completed" || !operation.ConnectorOnlineAt.Valid {
			return "", pgtype.Text{}, false, nil
		}
		connector, err := q.GetConnector(ctx, db.GetConnectorParams{ID: operation.ConnectorID, EnterpriseID: operation.EnterpriseID})
		if err != nil {
			return "", pgtype.Text{}, false, err
		}
		if connector.Status != "online" {
			return "", pgtype.Text{}, false, nil
		}
		if operation.InstallMode == "direct_install_tunnel" {
			tunnel, err := q.GetConnectorControlTunnelByConnector(ctx, db.GetConnectorControlTunnelByConnectorParams{
				ConnectorID: operation.ConnectorID, EnterpriseID: operation.EnterpriseID,
			})
			if err != nil {
				return "", pgtype.Text{}, false, err
			}
			if tunnel.Status != "established" || tunnel.Epoch < 1 {
				return "", pgtype.Text{}, false, nil
			}
		}
		return "succeeded", pgtype.Text{}, true, nil
	case "failed", "expired", "cancelled":
		code := operation.ErrorCode
		if !code.Valid {
			code = pgtype.Text{String: "CONNECTOR_INSTALL_RESULT_UNKNOWN", Valid: true}
		}
		return "failed", code, true, nil
	default:
		return "", pgtype.Text{}, false, nil
	}
}

func reconciledTelemetryOutcome(operation db.TelemetryCollectorOperation) (string, pgtype.Text, bool) {
	switch operation.Status {
	case "succeeded":
		return "succeeded", pgtype.Text{}, true
	case "failed", "expired":
		code := operation.ErrorCode
		if !code.Valid {
			code = pgtype.Text{String: "EXECUTION_RESULT_UNKNOWN", Valid: true}
		}
		return "failed", code, true
	default:
		return "", pgtype.Text{}, false
	}
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
