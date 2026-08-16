-- name: CreateIdempotencyRecord :execrows
INSERT INTO idempotency_records (audience, subject_id, operation, idempotency_key, request_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING;

-- name: GetIdempotencyRecord :one
SELECT * FROM idempotency_records
WHERE audience = $1 AND subject_id = $2 AND operation = $3 AND idempotency_key = $4;

-- name: CompleteIdempotencyRecord :execrows
UPDATE idempotency_records SET response_status = $5, response_nonce = $6, response_ciphertext = $7
WHERE audience = $1 AND subject_id = $2 AND operation = $3 AND idempotency_key = $4
  AND response_ciphertext IS NULL AND expires_at > now();

-- name: GetEffectiveRoleBindings :many
SELECT rb.* FROM role_bindings rb
WHERE rb.enterprise_id = $1 AND rb.status = 'active'
  AND (rb.valid_from IS NULL OR rb.valid_from <= now())
  AND (rb.valid_until IS NULL OR rb.valid_until > now())
  AND ((rb.subject_type = 'user' AND rb.subject_id = sqlc.arg(user_id))
    OR (rb.subject_type = 'department' AND rb.subject_id = sqlc.arg(department_id)));
