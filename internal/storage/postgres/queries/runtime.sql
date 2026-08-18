-- name: CreateConversation :one
INSERT INTO conversations (id, enterprise_id, owner_user_id, title, selected_model_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetConversation :one
SELECT * FROM conversations WHERE id = $1 AND enterprise_id = $2 AND owner_user_id = $3;

-- name: ListConversations :many
SELECT * FROM conversations
WHERE enterprise_id = $1 AND owner_user_id = $2
ORDER BY updated_at DESC, id DESC LIMIT $3;

-- name: UpdateConversation :one
UPDATE conversations SET
    title = COALESCE(sqlc.narg('title'), title),
    selected_model_id = COALESCE(sqlc.narg('selected_model_id'), selected_model_id),
    status = COALESCE(sqlc.narg('status'), status),
    version = version + 1, updated_at = now()
WHERE id = sqlc.arg('id') AND enterprise_id = sqlc.arg('enterprise_id')
  AND owner_user_id = sqlc.arg('owner_user_id') AND version = sqlc.arg('expected_version')
RETURNING *;

-- name: CreateRun :one
INSERT INTO runs (id, conversation_id, enterprise_id, actor_user_id, model_id, model_revision, locale, authorization_version, checkpoint)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1 AND enterprise_id = $2;

-- name: GetActiveRunForConversation :one
SELECT * FROM runs
WHERE conversation_id = $1 AND enterprise_id = $2
  AND status IN ('pending','running','waiting_input','waiting_approval','waiting_system')
ORDER BY created_at DESC LIMIT 1;

-- name: UpdateRunStatus :one
UPDATE runs SET status = $3, stop_reason = $4, error_code = $5, version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $6
RETURNING *;

-- name: SetRunCurrentStep :one
UPDATE runs SET status = $3, current_step_id = $4, version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $5
RETURNING *;

-- name: CreateRunStep :one
INSERT INTO run_steps (id, run_id, enterprise_id, sequence, step_type, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: NextRunStepSequence :one
SELECT COALESCE(MAX(sequence), 0)::integer + 1 FROM run_steps WHERE run_id = $1 AND enterprise_id = $2;

-- name: FinishRunStep :one
UPDATE run_steps SET status = $3, version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status IN ('pending','leased','running','waiting_input','waiting_approval')
RETURNING *;

-- name: ListRunConversationEvents :many
SELECT * FROM conversation_events WHERE run_id = $1 AND enterprise_id = $2 ORDER BY sequence;

-- name: NextConversationSequence :one
SELECT COALESCE(MAX(sequence), 0)::bigint + 1 AS sequence
FROM conversation_events WHERE conversation_id = $1;

-- name: CreateConversationEvent :one
INSERT INTO conversation_events (id, enterprise_id, conversation_id, run_id, step_id, sequence, event_type, actor_type, actor_id, payload, content_hash, artifact_ref, data_classification)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: LockConversation :one
SELECT * FROM conversations WHERE id = $1 AND enterprise_id = $2 AND owner_user_id = $3 FOR UPDATE;

-- name: ListConversationEvents :many
SELECT * FROM conversation_events
WHERE conversation_id = $1 AND enterprise_id = $2 AND sequence > $3
ORDER BY sequence LIMIT $4;

-- name: CreateRuntimeTask :one
INSERT INTO runtime_tasks (id, enterprise_id, queue, run_id, step_id, payload, max_attempts, available_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ClaimRuntimeTask :one
WITH candidate AS (
    SELECT runtime_tasks.id FROM runtime_tasks
    WHERE runtime_tasks.queue = $1
      AND runtime_tasks.attempt < runtime_tasks.max_attempts
      AND runtime_tasks.available_at <= now()
      AND (runtime_tasks.status = 'pending' OR (runtime_tasks.status IN ('leased','running') AND runtime_tasks.lease_until < now()))
    ORDER BY runtime_tasks.available_at, runtime_tasks.created_at
    FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE runtime_tasks AS task SET
    status = 'leased', lease_owner = $2, lease_until = now() + $3::interval,
    fence_token = fence_token + 1, attempt = attempt + 1, updated_at = now()
FROM candidate WHERE task.id = candidate.id
RETURNING task.*;

-- name: StartRuntimeTask :one
UPDATE runtime_tasks SET status = 'running', updated_at = now()
WHERE id = $1 AND lease_owner = $2 AND fence_token = $3 AND status = 'leased' AND lease_until > now()
RETURNING *;

-- name: RenewRuntimeTaskLease :one
UPDATE runtime_tasks SET lease_until = now() + $4::interval, updated_at = now()
WHERE id = $1 AND lease_owner = $2 AND fence_token = $3 AND status IN ('leased','running')
RETURNING *;

-- name: FinishRuntimeTask :one
UPDATE runtime_tasks SET status = $4, last_error_code = $5, lease_owner = NULL, lease_until = NULL, updated_at = now()
WHERE id = $1 AND lease_owner = $2 AND fence_token = $3 AND status IN ('leased','running')
RETURNING *;

-- name: RequeueRuntimeTask :one
UPDATE runtime_tasks SET status = 'pending', last_error_code = $4, available_at = $5,
    lease_owner = NULL, lease_until = NULL, updated_at = now()
WHERE id = $1 AND lease_owner = $2 AND fence_token = $3
RETURNING *;

-- name: CreateArtifact :one
INSERT INTO artifacts (id, result_ref, enterprise_id, conversation_id, run_id, content_type, data_classification, content, content_hash, byte_size)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetArtifactByRef :one
SELECT * FROM artifacts WHERE result_ref = $1 AND enterprise_id = $2;

-- name: GetToolResultByArtifact :one
SELECT result.*, call.call_id, call.tool_id FROM tool_results result
JOIN tool_calls call ON call.id = result.tool_call_id
WHERE result.artifact_id = $1 AND result.enterprise_id = $2;

-- name: CreateToolCall :one
INSERT INTO tool_calls (id, call_id, enterprise_id, run_id, step_id, tool_id, input, input_hash, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: FinishToolCall :one
UPDATE tool_calls SET status = $2, error_code = $3, updated_at = now() WHERE id = $1 RETURNING *;

-- name: CreateToolResult :one
INSERT INTO tool_results (id, tool_call_id, enterprise_id, artifact_id, projection, projection_hash, projection_bytes, partial)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateContextSnapshot :one
INSERT INTO context_snapshots (id, enterprise_id, conversation_id, run_id, revision, source_from_sequence, source_through_sequence,
    first_kept_sequence, typed_checkpoint, narrative_summary, compaction_model_id, compaction_model_revision, prompt_version,
    estimated_tokens_before, actual_tokens_after, source_hash, snapshot_hash, status, error_code)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
RETURNING *;

-- name: GetActiveContextSnapshot :one
SELECT * FROM context_snapshots WHERE run_id = $1 AND enterprise_id = $2 AND status = 'active';

-- name: GetContextSnapshotBySourceHash :one
SELECT * FROM context_snapshots
WHERE run_id = $1 AND enterprise_id = $2 AND source_hash = $3;

-- name: NextContextSnapshotRevision :one
SELECT COALESCE(MAX(revision), 0)::integer + 1 FROM context_snapshots WHERE run_id = $1 AND enterprise_id = $2;

-- name: SupersedeContextSnapshots :exec
UPDATE context_snapshots SET status = 'superseded'
WHERE run_id = $1 AND enterprise_id = $2 AND status = 'active';
