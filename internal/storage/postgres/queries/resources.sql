-- name: CreateHost :one
INSERT INTO hosts (id, enterprise_id, name, hostname, address, port, platform, architecture, connection_mode, bastion_scope_id, environment, labels, labels_hash, connection_status, pinned_host_key)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING *;

-- name: GetHost :one
SELECT * FROM hosts WHERE id = $1 AND enterprise_id = $2 AND status <> 'deleted';

-- name: ListHosts :many
SELECT * FROM hosts WHERE enterprise_id = $1 AND status <> 'deleted' ORDER BY created_at, id;

-- name: UpdateHost :one
UPDATE hosts SET name = COALESCE(sqlc.narg('name'), name), environment = COALESCE(sqlc.narg('environment'), environment),
 hostname = COALESCE(sqlc.narg('hostname'), hostname), address = COALESCE(sqlc.narg('address'), address),
 port = COALESCE(sqlc.narg('port'), port), connection_mode = COALESCE(sqlc.narg('connection_mode'), connection_mode),
 bastion_scope_id = CASE WHEN sqlc.arg('set_bastion_scope')::boolean THEN sqlc.narg('bastion_scope_id') ELSE bastion_scope_id END,
 connection_status = COALESCE(sqlc.narg('connection_status'), connection_status),
 pinned_host_key = COALESCE(sqlc.narg('pinned_host_key'), pinned_host_key),
 architecture = COALESCE(sqlc.narg('architecture'), architecture),
 labels = COALESCE(sqlc.narg('labels'), labels), labels_hash = COALESCE(sqlc.narg('labels_hash'), labels_hash),
 labels_version = CASE WHEN sqlc.narg('labels')::jsonb IS NULL THEN labels_version ELSE labels_version + 1 END,
 resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND resource_version = $3 AND status <> 'deleted' RETURNING *;

-- name: DeleteHost :one
UPDATE hosts SET status = 'deleted', deleted_at = now(), resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND resource_version = $3 AND connection_mode <> 'connector_local' RETURNING *;

-- name: DeleteBastionRootHost :one
UPDATE hosts SET status = 'deleted', connection_status = 'offline', deleted_at = now(), resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND bastion_scope_id = $3 AND connection_mode = 'connector_local' AND status <> 'deleted'
RETURNING *;

-- name: CreateKubernetesCluster :one
INSERT INTO kubernetes_clusters (id, enterprise_id, name, api_server, connection_mode, bastion_scope_id, credential_id, default_namespace, environment, labels, labels_hash, connection_status)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: GetKubernetesCluster :one
SELECT * FROM kubernetes_clusters WHERE id = $1 AND enterprise_id = $2 AND status <> 'deleted';

-- name: ListKubernetesClusters :many
SELECT * FROM kubernetes_clusters WHERE enterprise_id = $1 AND status <> 'deleted' ORDER BY created_at, id;

-- name: UpdateKubernetesCluster :one
UPDATE kubernetes_clusters SET name = COALESCE(sqlc.narg('name'), name), environment = COALESCE(sqlc.narg('environment'), environment),
 api_server = COALESCE(sqlc.narg('api_server'), api_server), connection_mode = COALESCE(sqlc.narg('connection_mode'), connection_mode),
 bastion_scope_id = CASE WHEN sqlc.arg('set_bastion_scope')::boolean THEN sqlc.narg('bastion_scope_id') ELSE bastion_scope_id END,
 credential_id = CASE WHEN sqlc.arg('set_credential')::boolean THEN sqlc.narg('credential_id') ELSE credential_id END,
 default_namespace = COALESCE(sqlc.narg('default_namespace'), default_namespace),
 connection_status = COALESCE(sqlc.narg('connection_status'), connection_status),
 labels = COALESCE(sqlc.narg('labels'), labels), labels_hash = COALESCE(sqlc.narg('labels_hash'), labels_hash),
 labels_version = CASE WHEN sqlc.narg('labels')::jsonb IS NULL THEN labels_version ELSE labels_version + 1 END,
 resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND resource_version = $3 AND status <> 'deleted' RETURNING *;

-- name: DeleteKubernetesCluster :one
UPDATE kubernetes_clusters SET status = 'deleted', deleted_at = now(), resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND resource_version = $3 RETURNING *;

-- name: CreateConnectionTest :one
INSERT INTO connection_tests (id, enterprise_id, target_type, resource_id, path, connector_id, connection_epoch, credential_id, credential_version, request_plan, request_hash, expires_at, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: GetConnectionTest :one
SELECT * FROM connection_tests WHERE id = $1 AND enterprise_id = $2;

-- name: ClaimDirectConnectionTests :many
WITH claimed AS (
  SELECT id FROM connection_tests
  WHERE path = 'direct' AND status = 'queued' AND expires_at > now()
  ORDER BY created_at
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
UPDATE connection_tests AS test SET status = 'running', updated_at = now()
FROM claimed WHERE test.id = claimed.id RETURNING test.*;

-- name: ClaimDirectConnectionTest :one
UPDATE connection_tests SET status = 'running', updated_at = now()
WHERE id = $1 AND path = 'direct' AND status = 'queued' AND expires_at > now()
RETURNING *;

-- name: MarkConnectorConnectionTestRunning :one
UPDATE connection_tests SET status = 'running', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND path = 'connector' AND status = 'queued' AND expires_at > now()
RETURNING *;

-- name: ExpireQueuedConnectionTests :execrows
UPDATE connection_tests SET status = 'expired', updated_at = now()
WHERE status IN ('queued','running') AND expires_at <= now();

-- name: CompleteConnectionTest :one
UPDATE connection_tests SET status = $3, result = $4, error_code = $5, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status IN ('queued','running','result_unknown') RETURNING *;

-- name: ExpireConnectionTestsByCredential :exec
UPDATE connection_tests AS test SET status = 'expired', updated_at = now()
WHERE test.enterprise_id = $1 AND test.credential_id IN (SELECT credential.id FROM credentials AS credential WHERE credential.secret_id = $2 AND credential.enterprise_id = $1)
AND test.status IN ('queued','running','succeeded','result_unknown');

-- name: CreatePendingAction :one
INSERT INTO pending_actions (id, action_ref, enterprise_id, creator_subject_id, creator_subject_type, authorization_version, action_type, title, summary, risk, preview, diff, status, resource_type, resource_id, expected_resource_version, impact_hash, expires_at, run_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING *;

-- name: CreatePendingActionPlan :one
INSERT INTO pending_action_plans (id, pending_action_id, enterprise_id, preview_call_id, commit_tool, authorization_version, plan_schema_version, plan_hash, immutable_plan, resource_scope_snapshot)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: CreatePendingActionToken :one
INSERT INTO pending_action_tokens (id, pending_action_id, enterprise_id, token_hash, key_version, nonce, ciphertext, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;

-- name: GetPendingAction :one
SELECT * FROM pending_actions WHERE action_ref = $1 AND enterprise_id = $2;

-- name: GetPendingActionForUpdate :one
SELECT * FROM pending_actions WHERE action_ref = $1 AND enterprise_id = $2 FOR UPDATE;

-- name: GetPendingActionPlan :one
SELECT p.* FROM pending_action_plans p JOIN pending_actions a ON a.id = p.pending_action_id
WHERE a.action_ref = $1 AND p.enterprise_id = $2;

-- name: GetPendingActionTokenForUpdate :one
SELECT t.* FROM pending_action_tokens t JOIN pending_actions a ON a.id = t.pending_action_id
WHERE a.action_ref = $1 AND t.enterprise_id = $2 FOR UPDATE;

-- name: ListPendingActions :many
SELECT * FROM pending_actions WHERE enterprise_id = $1 ORDER BY created_at DESC, id DESC;

-- name: ListPendingActionsByCreator :many
SELECT * FROM pending_actions
WHERE enterprise_id = $1 AND creator_subject_type = 'user' AND creator_subject_id = $2
ORDER BY created_at DESC, id DESC;

-- name: ListPendingActionsForApprover :many
SELECT DISTINCT action.* FROM pending_actions action
JOIN approval_requests request ON request.pending_action_id = action.id
JOIN approval_requirement_snapshots requirement ON requirement.approval_request_id = request.id
JOIN role_bindings binding ON binding.enterprise_id = action.enterprise_id
  AND binding.subject_type = 'user' AND binding.subject_id = $2
  AND binding.status = 'active' AND binding.role_id = ANY(requirement.approver_role_ids)
WHERE action.enterprise_id = $1
  AND action.status = 'awaiting_approval'
  AND request.status = 'pending'
  AND requirement.status = 'pending'
  AND (binding.valid_from IS NULL OR binding.valid_from <= now())
  AND (binding.valid_until IS NULL OR binding.valid_until > now())
  AND (requirement.separation_of_duty = false OR action.creator_subject_id <> $2)
  AND request.expires_at > now()
ORDER BY action.created_at DESC, action.id DESC;

-- name: MarkPendingActionExecuting :one
UPDATE pending_actions SET status = 'executing', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'awaiting_confirmation' AND expires_at > now() RETURNING *;

-- name: ConsumePendingActionToken :execrows
UPDATE pending_action_tokens SET status = 'consumed', consumed_at = now(), updated_at = now()
WHERE pending_action_id = $1 AND enterprise_id = $2 AND status = 'active' AND expires_at > now();

-- name: FinishPendingAction :one
UPDATE pending_actions SET status = $3, result_resource_type = $4, result_resource_id = $5,
 result_resource_version = $6, result_summary = $7, error_code = $8, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'executing' RETURNING *;

-- name: CancelPendingAction :one
UPDATE pending_actions SET status = 'cancelled', updated_at = now()
WHERE action_ref = $1 AND enterprise_id = $2 AND creator_subject_type = 'user' AND creator_subject_id = $3
  AND status IN ('awaiting_confirmation','awaiting_approval','ready') RETURNING *;
