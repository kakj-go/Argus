-- name: CreateAIModel :one
INSERT INTO ai_models (id, enterprise_id, name, base_url, model_id, api_protocol, context_window_tokens, max_output_tokens,
    input_price_per_million, output_price_per_million, capabilities, health_status, last_tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'healthy',now()) RETURNING *;

-- name: CreateAIModelRevision :one
INSERT INTO ai_model_revisions (id, model_id, enterprise_id, revision, base_url, provider_model_id, api_protocol,
    context_window_tokens, max_output_tokens, input_price_per_million, output_price_per_million, capabilities)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: CreateAIModelCredential :one
INSERT INTO ai_model_credentials (id, model_revision_id, enterprise_id, provider, key_id, key_version, wrapped_dek, wrap_nonce, nonce, ciphertext, value_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: CreateModelCompatibilityResult :one
INSERT INTO model_compatibility_results (id, model_revision_id, enterprise_id, compatible, checks, error_code)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: ListAIModels :many
SELECT * FROM ai_models WHERE enterprise_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: GetAIModel :one
SELECT * FROM ai_models WHERE id = $1 AND enterprise_id = $2;

-- name: UpdateAIModel :one
UPDATE ai_models SET name = $3, base_url = $4, model_id = $5, api_protocol = $6,
    context_window_tokens = $7, max_output_tokens = $8,
    input_price_per_million = $9, output_price_per_million = $10,
    capabilities = $11, status = $12, health_status = $13,
    revision = $14, version = version + 1, last_tested_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $15
RETURNING *;

-- name: GetEnabledAIModelRevision :one
SELECT revision.* FROM ai_model_revisions revision
JOIN ai_models model ON model.id = revision.model_id AND model.enterprise_id = revision.enterprise_id
WHERE revision.model_id = $1 AND revision.enterprise_id = $2 AND revision.revision = $3
  AND model.status = 'enabled' AND model.health_status = 'healthy';

-- name: GetAIModelCredential :one
SELECT * FROM ai_model_credentials WHERE model_revision_id = $1 AND enterprise_id = $2;

-- name: GetLatestAIModelRevision :one
SELECT * FROM ai_model_revisions WHERE model_id = $1 AND enterprise_id = $2 ORDER BY revision DESC LIMIT 1;

-- name: UpsertModelQuota :one
INSERT INTO model_quotas (id, enterprise_id, model_id, subject_type, subject_id, monthly_amount)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (enterprise_id, model_id, subject_type, subject_id) DO UPDATE SET
    monthly_amount = EXCLUDED.monthly_amount, version = model_quotas.version + 1, updated_at = now()
RETURNING *;

-- name: ListModelQuotas :many
SELECT * FROM model_quotas WHERE enterprise_id = $1 ORDER BY model_id, subject_type, subject_id;

-- name: ListModelUsage :many
SELECT model_id,
    date_trunc('month', completed_at)::date AS month,
    count(*)::bigint AS request_count,
    COALESCE(sum(input_tokens), 0)::bigint AS input_tokens,
    COALESCE(sum(output_tokens), 0)::bigint AS output_tokens,
    COALESCE(sum(amount), 0)::numeric(20,8) AS amount,
    count(*) FILTER (WHERE call_kind = 'compaction')::bigint AS compaction_count
FROM model_calls
WHERE enterprise_id = $1 AND completed_at >= $2 AND completed_at < $3
  AND (sqlc.narg('model_id')::uuid IS NULL OR model_id = sqlc.narg('model_id'))
GROUP BY model_id, date_trunc('month', completed_at)::date
ORDER BY month DESC, model_id;

-- name: CreateQuotaReservation :one
INSERT INTO model_quota_reservations (id, enterprise_id, model_call_id, model_id, department_id, user_id, month, reserved_amount, status, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9) RETURNING *;

-- name: SumActiveAndSettledQuota :one
SELECT COALESCE(sum(CASE WHEN status = 'settled' THEN settled_amount ELSE reserved_amount END), 0)::numeric(20,8)
FROM model_quota_reservations
WHERE enterprise_id = $1 AND model_id = $2 AND month = $3 AND status IN ('active','settled')
  AND (department_id = $4 OR user_id = $5);

-- name: GetApplicableModelQuotasForUpdate :many
SELECT * FROM model_quotas
WHERE enterprise_id = $1 AND model_id = $2
  AND ((subject_type = 'department' AND subject_id = $3) OR (subject_type = 'user' AND subject_id = $4))
ORDER BY subject_type FOR UPDATE;

-- name: SumQuotaReservationsBySubject :one
SELECT
  COALESCE(sum(CASE WHEN status = 'settled' THEN settled_amount ELSE reserved_amount END) FILTER (WHERE department_id = $4), 0)::numeric(20,8) AS department_amount,
  COALESCE(sum(CASE WHEN status = 'settled' THEN settled_amount ELSE reserved_amount END) FILTER (WHERE user_id = $5), 0)::numeric(20,8) AS user_amount
FROM model_quota_reservations
WHERE enterprise_id = $1 AND model_id = $2 AND month = $3 AND status IN ('active','settled');

-- name: SettleQuotaReservation :one
UPDATE model_quota_reservations SET settled_amount = $3, status = 'settled'
WHERE id = $1 AND enterprise_id = $2 AND status = 'active' RETURNING *;

-- name: CreateModelCall :one
INSERT INTO model_calls (id, enterprise_id, run_id, step_id, model_id, model_revision, call_kind, projection_hash,
    input_price_snapshot, output_price_snapshot, status)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'reserved') RETURNING *;

-- name: FinishModelCall :one
UPDATE model_calls SET input_tokens = $3, output_tokens = $4, amount = $5, latency_ms = $6,
    stop_reason = $7, status = $8, error_code = $9, completed_at = now()
WHERE id = $1 AND enterprise_id = $2 RETURNING *;
