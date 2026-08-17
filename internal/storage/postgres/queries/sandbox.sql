-- name: CreateSandboxBackend :one
INSERT INTO sandbox_backends (id, name, endpoint, credential_provider, credential_key_id, credential_key_version,
    credential_wrapped_dek, credential_wrap_nonce, credential_nonce, credential_ciphertext, credential_value_hash, status)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: ListSandboxBackends :many
SELECT * FROM sandbox_backends ORDER BY name, id;

-- name: GetSandboxBackend :one
SELECT * FROM sandbox_backends WHERE id = $1;

-- name: UpdateSandboxBackend :one
UPDATE sandbox_backends SET name = $2, endpoint = $3,
    credential_provider = COALESCE(sqlc.narg('credential_provider'), credential_provider),
    credential_key_id = COALESCE(sqlc.narg('credential_key_id'), credential_key_id),
    credential_key_version = COALESCE(sqlc.narg('credential_key_version'), credential_key_version),
    credential_wrapped_dek = COALESCE(sqlc.narg('credential_wrapped_dek'), credential_wrapped_dek),
    credential_wrap_nonce = COALESCE(sqlc.narg('credential_wrap_nonce'), credential_wrap_nonce),
    credential_nonce = COALESCE(sqlc.narg('credential_nonce'), credential_nonce),
    credential_ciphertext = COALESCE(sqlc.narg('credential_ciphertext'), credential_ciphertext),
    credential_value_hash = COALESCE(sqlc.narg('credential_value_hash'), credential_value_hash),
    status = $4, health_status = 'unknown', version = version + 1, updated_at = now()
WHERE id = $1 AND version = $5 RETURNING *;

-- name: SetSandboxBackendHealth :one
UPDATE sandbox_backends SET health_status = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: CreateSandboxImage :one
INSERT INTO sandbox_images (id, backend_id, name, image_ref, digest, status)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: ListSandboxImages :many
SELECT * FROM sandbox_images ORDER BY name, id;

-- name: GetSandboxImage :one
SELECT * FROM sandbox_images WHERE id = $1;

-- name: UpdateSandboxImage :one
UPDATE sandbox_images SET backend_id = $2, name = $3, image_ref = $4, digest = $5, status = $6,
    version = version + 1, updated_at = now()
WHERE id = $1 AND version = $7 RETURNING *;

-- name: CreateSandboxProfile :one
INSERT INTO sandbox_profiles (id, name, backend_id, image_id, task_kinds, cpu_millis, memory_mib, timeout_seconds, network_mode, status)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: ListSandboxProfiles :many
SELECT * FROM sandbox_profiles ORDER BY name, id;

-- name: GetSandboxProfile :one
SELECT * FROM sandbox_profiles WHERE id = $1;

-- name: SelectSandboxProfile :one
SELECT profile.* FROM sandbox_profiles profile
JOIN sandbox_backends backend ON backend.id = profile.backend_id AND backend.status = 'enabled'
JOIN sandbox_images image ON image.id = profile.image_id AND image.status = 'enabled'
WHERE profile.status = 'enabled' AND $1::text = ANY(profile.task_kinds)
ORDER BY profile.name, profile.id LIMIT 1;

-- name: UpdateSandboxProfile :one
UPDATE sandbox_profiles SET name = $2, backend_id = $3, image_id = $4, task_kinds = $5,
    cpu_millis = $6, memory_mib = $7, timeout_seconds = $8, network_mode = $9, status = $10,
    revision = revision + 1, version = version + 1, updated_at = now()
WHERE id = $1 AND version = $11 RETURNING *;

-- name: GetSandboxQuota :one
SELECT * FROM sandbox_quotas WHERE enterprise_id = $1;

-- name: GetSandboxQuotaForUpdate :one
SELECT * FROM sandbox_quotas WHERE enterprise_id = $1 FOR UPDATE;

-- name: UpsertSandboxQuota :one
INSERT INTO sandbox_quotas (enterprise_id, max_concurrent_sessions, monthly_session_seconds)
VALUES ($1,$2,$3)
ON CONFLICT (enterprise_id) DO UPDATE SET max_concurrent_sessions = EXCLUDED.max_concurrent_sessions,
    monthly_session_seconds = EXCLUDED.monthly_session_seconds, version = sandbox_quotas.version + 1, updated_at = now()
WHERE sandbox_quotas.version = $4 OR $4 = 0
RETURNING *;

-- name: CountActiveSandboxSessions :one
SELECT count(*)::integer FROM sandbox_sessions
WHERE enterprise_id = $1 AND status IN ('creating','running','terminating','unknown');

-- name: GetSandboxMonthlyCommittedSeconds :one
SELECT (
    COALESCE((
        SELECT usage.session_seconds FROM sandbox_usage usage
        WHERE usage.enterprise_id = sqlc.arg('enterprise_id') AND usage.month = sqlc.arg('month')
    ), 0)
    + COALESCE((
        SELECT sum(GREATEST(0, extract(epoch FROM (session.expires_at - COALESCE(session.started_at, session.created_at)))))::bigint
        FROM sandbox_sessions session
        WHERE session.enterprise_id = sqlc.arg('enterprise_id')
          AND session.created_at >= sqlc.arg('month')::date
          AND session.status IN ('creating','running','terminating','unknown')
    ), 0)
)::bigint AS committed_seconds;

-- name: CreateSandboxSession :one
INSERT INTO sandbox_sessions (id, enterprise_id, task_id, profile_id, profile_revision, upstream_session_id, status, expires_at, started_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetSandboxSession :one
SELECT * FROM sandbox_sessions WHERE id = $1;

-- name: GetSandboxSessionByTask :one
SELECT * FROM sandbox_sessions WHERE task_id = $1;

-- name: GetSandboxSessionForUpdate :one
SELECT * FROM sandbox_sessions WHERE id = $1 FOR UPDATE;

-- name: ListSandboxSessions :many
SELECT * FROM sandbox_sessions ORDER BY created_at DESC, id DESC LIMIT $1;

-- name: ListExpiredSandboxSessions :many
SELECT * FROM sandbox_sessions WHERE expires_at <= $1 AND status IN ('creating','running','terminating','unknown')
ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT $2;

-- name: ListSandboxSessionsForReconcile :many
SELECT * FROM sandbox_sessions
WHERE status IN ('creating','terminating','unknown')
   OR (status = 'running' AND expires_at <= $1)
ORDER BY updated_at, id LIMIT $2;

-- name: UpdateSandboxSessionStatus :one
UPDATE sandbox_sessions SET status = $2,
    started_at = COALESCE(sqlc.narg('started_at'), started_at),
    terminated_at = COALESCE(sqlc.narg('terminated_at'), terminated_at), updated_at = now()
WHERE id = $1 RETURNING *;

-- name: UpdateSandboxSessionExpiry :one
UPDATE sandbox_sessions SET status = $2, expires_at = $3,
    started_at = COALESCE(sqlc.narg('started_at'), started_at), updated_at = now()
WHERE id = $1 RETURNING *;

-- name: ListSandboxUsage :many
SELECT * FROM sandbox_usage ORDER BY month DESC, enterprise_id LIMIT $1;

-- name: GetCurrentSandboxUsage :one
SELECT * FROM sandbox_usage WHERE enterprise_id = $1 AND month = $2;

-- name: AddSandboxUsage :one
INSERT INTO sandbox_usage (enterprise_id, month, session_count, session_seconds)
VALUES ($1,$2,$3,$4)
ON CONFLICT (enterprise_id, month) DO UPDATE SET
    session_count = sandbox_usage.session_count + EXCLUDED.session_count,
    session_seconds = sandbox_usage.session_seconds + EXCLUDED.session_seconds,
    updated_at = now()
RETURNING *;
