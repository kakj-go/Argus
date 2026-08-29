-- name: ListMatchingApprovalPolicies :many
SELECT * FROM approval_policies
WHERE enterprise_id = $1 AND enabled = true
  AND ($2::text = ANY(risks))
  AND (cardinality(tool_ids) = 0 OR $3::text = ANY(tool_ids))
  AND (cardinality(resource_types) = 0 OR $4::text = ANY(resource_types))
ORDER BY id;

-- name: CreateApprovalPolicy :one
INSERT INTO approval_policies (id, enterprise_id, name, enabled, tool_ids, risks, resource_types,
    minimum_approvers, separation_of_duty, approver_role_ids, expires_after_seconds)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: ListApprovalPolicies :many
SELECT * FROM approval_policies WHERE enterprise_id = $1 ORDER BY name, id;

-- name: GetApprovalPolicy :one
SELECT * FROM approval_policies WHERE id = $1 AND enterprise_id = $2;

-- name: UpdateApprovalPolicy :one
UPDATE approval_policies SET
    name = $3, enabled = $4, tool_ids = $5, risks = $6, resource_types = $7,
    minimum_approvers = $8, separation_of_duty = $9,
    approver_role_ids = $10, expires_after_seconds = $11,
    version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $12
RETURNING *;

-- name: CreateUserConfirmation :one
INSERT INTO user_confirmations (id, pending_action_id, enterprise_id, actor_user_id, authorization_version)
VALUES ($1,$2,$3,$4,$5) RETURNING *;

-- name: CreateActionBinding :one
INSERT INTO action_bindings (id, binding_ref, pending_action_id, enterprise_id, actor_user_id, action, request_id, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;

-- name: ConsumeActionBinding :one
UPDATE action_bindings SET status = 'consumed', consumed_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'pending' AND expires_at > now()
RETURNING *;

-- name: CreateApprovalRequest :one
INSERT INTO approval_requests (id, pending_action_id, enterprise_id, expires_at)
VALUES ($1,$2,$3,$4) RETURNING *;

-- name: CreateApprovalRequirementSnapshot :one
INSERT INTO approval_requirement_snapshots (id, approval_request_id, enterprise_id, policy_id, policy_version,
    minimum_approvers, separation_of_duty, approver_role_ids, policy_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: ListApprovalRequirements :many
SELECT * FROM approval_requirement_snapshots WHERE approval_request_id = $1 AND enterprise_id = $2 ORDER BY policy_id;

-- name: CreateApprovalDecision :one
INSERT INTO approval_decisions (id, approval_request_id, enterprise_id, actor_user_id, decision, reason)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: ListApprovalDecisions :many
SELECT * FROM approval_decisions WHERE approval_request_id = $1 AND enterprise_id = $2 ORDER BY decided_at, id;

-- name: CountEligibleApprovalDecisions :one
SELECT count(*)::integer FROM approval_decisions decision
JOIN role_bindings binding ON binding.enterprise_id = decision.enterprise_id
    AND binding.subject_type = 'user' AND binding.subject_id = decision.actor_user_id AND binding.status = 'active'
WHERE decision.approval_request_id = $1 AND decision.enterprise_id = $2 AND decision.decision = 'approved'
  AND binding.role_id = ANY($3::uuid[])
  AND ($4::boolean = false OR decision.actor_user_id <> $5::uuid);

-- name: IsApprovalActorEligible :one
SELECT EXISTS (
    SELECT 1 FROM role_bindings binding
    WHERE binding.enterprise_id = $1 AND binding.subject_type = 'user'
      AND binding.subject_id = $2 AND binding.status = 'active'
      AND binding.role_id = ANY($3::uuid[])
)::boolean;

-- name: UpdateApprovalRequirementStatus :one
UPDATE approval_requirement_snapshots SET approved_count = $3, status = $4
WHERE id = $1 AND enterprise_id = $2 RETURNING *;

-- name: UpdateApprovalRequestStatus :one
UPDATE approval_requests SET status = $3, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'pending' RETURNING *;

-- name: GetApprovalRequest :one
SELECT * FROM approval_requests WHERE id = $1 AND enterprise_id = $2;

-- name: GetApprovalRequestForUpdate :one
SELECT * FROM approval_requests WHERE id = $1 AND enterprise_id = $2 FOR UPDATE;

-- name: GetPendingActionByIDForUpdate :one
SELECT * FROM pending_actions WHERE id = $1 AND enterprise_id = $2 FOR UPDATE;

-- name: GetPendingActionByID :one
SELECT * FROM pending_actions WHERE id = $1 AND enterprise_id = $2;

-- name: SetPendingActionPolicySnapshot :one
UPDATE pending_actions SET policy_snapshot_hash = $3, updated_at = now()
WHERE id = $1 AND enterprise_id = $2
RETURNING *;

-- name: RejectPendingAction :one
UPDATE pending_actions SET status = 'rejected', error_code = 'APPROVAL_REJECTED', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'awaiting_approval'
RETURNING *;

-- name: InvalidatePendingActionM4 :one
UPDATE pending_actions SET status = 'invalidated', error_code = $3, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status IN ('awaiting_confirmation','awaiting_approval','ready')
RETURNING *;

-- name: ListApprovalRequests :many
SELECT * FROM approval_requests WHERE enterprise_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: GetApprovalRequestByAction :one
SELECT request.* FROM approval_requests request
JOIN pending_actions action ON action.id = request.pending_action_id
WHERE action.action_ref = $1 AND request.enterprise_id = $2;

-- name: CreateExecution :one
INSERT INTO executions (id, execution_ref, pending_action_id, enterprise_id, run_id, idempotency_key)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: GetExecution :one
SELECT * FROM executions WHERE id = $1 AND enterprise_id = $2;

-- name: GetExecutionByAction :one
SELECT execution.* FROM executions execution JOIN pending_actions action ON action.id = execution.pending_action_id
WHERE action.action_ref = $1 AND execution.enterprise_id = $2;

-- name: CreateExecutionOneTimeResult :one
INSERT INTO execution_one_time_results (id, execution_id, enterprise_id, authorization_version, result_kind, key_version, nonce, ciphertext, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetExecutionOneTimeResultForUpdate :one
SELECT * FROM execution_one_time_results WHERE execution_id = $1 AND enterprise_id = $2 FOR UPDATE;

-- name: HasExecutionOneTimeResult :one
SELECT EXISTS (
    SELECT 1 FROM execution_one_time_results
    WHERE execution_id = $1 AND enterprise_id = $2 AND consumed_at IS NULL AND expires_at > now()
);

-- name: ConsumeExecutionOneTimeResult :one
UPDATE execution_one_time_results SET consumed_by_user_id = $3, consumed_at = now()
WHERE execution_id = $1 AND enterprise_id = $2 AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ListExecutions :many
SELECT * FROM executions WHERE enterprise_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: ClaimExecution :one
UPDATE executions SET status = 'running', started_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'pending' RETURNING *;

-- name: FinishExecution :one
UPDATE executions SET status = $3, result_ref = $4, error_code = $5, completed_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status IN ('pending','running','result_unknown') RETURNING *;

-- name: MarkExecutionResultUnknown :one
UPDATE executions SET status = 'result_unknown', connector_command_id = $3,
    error_code = 'EXECUTION_RESULT_UNKNOWN', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'running' RETURNING *;

-- name: MarkExecutionTelemetryResultUnknown :one
UPDATE executions SET status = 'result_unknown', telemetry_collector_operation_id = $3,
    error_code = 'EXECUTION_RESULT_UNKNOWN', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'running' RETURNING *;

-- name: ListUncertainExecutions :many
SELECT * FROM executions WHERE status = 'result_unknown'
ORDER BY updated_at, id LIMIT $1;

-- name: MarkPendingActionAwaitingApproval :one
UPDATE pending_actions SET status = 'awaiting_approval', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'awaiting_confirmation' RETURNING *;

-- name: MarkPendingActionReady :one
UPDATE pending_actions SET status = 'ready', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status IN ('awaiting_confirmation','awaiting_approval') RETURNING *;

-- name: MarkPendingActionExecutingM4 :one
UPDATE pending_actions SET status = 'executing', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'ready' RETURNING *;
