-- name: CreateAutomation :one
INSERT INTO automations (id, enterprise_id, name, service_account_id, authorization_version, tool_id, tool_input, cron, timezone, next_run_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: ListAutomations :many
SELECT * FROM automations WHERE enterprise_id = $1 ORDER BY name, id;

-- name: GetAutomation :one
SELECT * FROM automations WHERE id = $1 AND enterprise_id = $2;

-- name: UpdateAutomation :one
UPDATE automations SET name = $3, service_account_id = $4, authorization_version = $5, tool_id = $6, tool_input = $7,
    cron = $8, timezone = $9, next_run_at = $10, revision = revision + 1,
    version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $11
RETURNING *;

-- name: SetAutomationStatus :one
UPDATE automations SET status = $3, version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $4
RETURNING *;

-- name: ClaimDueAutomations :many
SELECT * FROM automations WHERE status = 'enabled' AND next_run_at <= $1
ORDER BY next_run_at, id FOR UPDATE SKIP LOCKED LIMIT $2;

-- name: GetAutomationForUpdate :one
SELECT * FROM automations WHERE id = $1 AND enterprise_id = $2 FOR UPDATE;

-- name: CreateAutomationRevision :one
INSERT INTO automation_revisions (
    automation_id, enterprise_id, revision, service_account_id, authorization_version,
    tool_id, tool_input, cron, timezone
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: GetAutomationRevision :one
SELECT * FROM automation_revisions
WHERE automation_id = $1 AND enterprise_id = $2 AND revision = $3;

-- name: AdvanceAutomation :one
UPDATE automations SET next_run_at = $3, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 RETURNING *;

-- name: CreateAutomationRun :one
INSERT INTO automation_runs (id, automation_id, enterprise_id, automation_revision, scheduled_for, status)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (automation_id, scheduled_for) DO UPDATE SET updated_at = automation_runs.updated_at
RETURNING *;

-- name: UpdateAutomationRun :one
UPDATE automation_runs SET status = $3, task_id = $4, pending_action_id = $5, result_ref = $6,
    error_code = $7, updated_at = now() WHERE id = $1 AND enterprise_id = $2 RETURNING *;

-- name: MarkAutomationRunExecuting :exec
UPDATE automation_runs SET status = 'running', updated_at = now()
WHERE pending_action_id = $1 AND enterprise_id = $2 AND status = 'waiting_approval';

-- name: FinishAutomationRunByPendingAction :exec
UPDATE automation_runs SET status = $3, error_code = $4, updated_at = now()
WHERE pending_action_id = $1 AND enterprise_id = $2 AND status IN ('waiting_approval','running');

-- name: GetAutomationRun :one
SELECT * FROM automation_runs WHERE id = $1 AND enterprise_id = $2;

-- name: GetActiveAutomationRun :one
SELECT * FROM automation_runs WHERE automation_id = $1 AND enterprise_id = $2
  AND status IN ('pending','running','waiting_approval') ORDER BY scheduled_for DESC LIMIT 1;

-- name: ListAutomationRuns :many
SELECT * FROM automation_runs WHERE automation_id = $1 AND enterprise_id = $2 ORDER BY scheduled_for DESC LIMIT $3;
