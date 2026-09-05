-- PlanV4 self-enrolled Host 安装/重新收敛与独立卸载令牌。

-- name: CreateHostEnrollmentToken :one
INSERT INTO host_enrollment_tokens (
  id, enterprise_id, preallocated_host_id, collector_id, token_hash, frozen_plan, frozen_plan_hash,
  status, remaining_uses, expires_at, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,'active',1,$8,$9)
RETURNING *;

-- name: GetHostEnrollmentTokenByHash :one
SELECT * FROM host_enrollment_tokens WHERE token_hash = $1;

-- name: GetHostEnrollmentTokenByHashForUpdate :one
SELECT * FROM host_enrollment_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: GetHostEnrollmentTokenForUpdate :one
SELECT * FROM host_enrollment_tokens WHERE id = $1 FOR UPDATE;

-- name: GetActiveHostEnrollmentTokenByHost :one
SELECT * FROM host_enrollment_tokens
WHERE enterprise_id = $1 AND preallocated_host_id = $2 AND status = 'active'
ORDER BY created_at DESC LIMIT 1;

-- name: ConsumeHostEnrollmentToken :execrows
UPDATE host_enrollment_tokens SET
  status = 'consumed', remaining_uses = 0, consumed_at = now(),
  consumed_device_hash = $2, reported_hostname = $3, reported_address = $4,
  reported_architecture = $5, updated_at = now()
WHERE id = $1 AND status = 'active' AND remaining_uses = 1 AND expires_at > now();

-- name: StoreHostEnrollmentExchange :one
UPDATE host_enrollment_tokens SET
  exchange_key_version = $2, exchange_nonce = $3, exchange_ciphertext = $4,
  exchange_expires_at = $5, updated_at = now()
WHERE id = $1 AND status = 'consumed' AND exchange_ciphertext IS NULL
RETURNING *;

-- name: RevokeActiveHostEnrollmentTokens :execrows
UPDATE host_enrollment_tokens SET status = 'revoked', updated_at = now()
WHERE enterprise_id = $1 AND preallocated_host_id = $2 AND status = 'active';

-- name: ExpireHostEnrollmentTokens :execrows
UPDATE host_enrollment_tokens SET status = 'expired', updated_at = now()
WHERE status = 'active' AND expires_at <= now();

-- name: ListHostEnrollmentTokensByHost :many
SELECT * FROM host_enrollment_tokens
WHERE enterprise_id = $1 AND preallocated_host_id = $2
ORDER BY created_at DESC, id;

-- name: GetLatestConsumedHostEnrollmentByCollector :one
SELECT * FROM host_enrollment_tokens
WHERE collector_id = $1 AND status = 'consumed'
ORDER BY consumed_at DESC, id DESC LIMIT 1;

-- name: CreateHostUninstallToken :one
INSERT INTO host_uninstall_tokens (
  id, enterprise_id, host_id, collector_id, token_hash, frozen_plan,
  frozen_plan_hash, status, expires_at, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,'active',$8,$9)
RETURNING *;

-- name: GetHostUninstallTokenByHash :one
SELECT * FROM host_uninstall_tokens WHERE token_hash = $1;

-- name: GetHostUninstallTokenByHashForUpdate :one
SELECT * FROM host_uninstall_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: GetHostUninstallTokenForUpdate :one
SELECT * FROM host_uninstall_tokens WHERE id = $1 FOR UPDATE;

-- name: ConsumeHostUninstallToken :one
UPDATE host_uninstall_tokens SET
  status = 'consumed', consumed_at = now(), consumed_device_hash = $2,
  completion_token_hash = $3, updated_at = now()
WHERE id = $1 AND status = 'active' AND expires_at > now()
RETURNING *;

-- name: CompleteHostUninstallToken :one
UPDATE host_uninstall_tokens SET status = 'completed', completed_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'consumed'
  AND completion_token_hash = $3
RETURNING *;

-- name: RevokeActiveHostUninstallTokens :execrows
UPDATE host_uninstall_tokens SET status = 'revoked', updated_at = now()
WHERE enterprise_id = $1 AND host_id = $2 AND status = 'active';

-- name: ExpireHostUninstallTokens :execrows
UPDATE host_uninstall_tokens SET status = 'expired', updated_at = now()
WHERE status = 'active' AND expires_at <= now();

-- name: ListHostUninstallTokensByHost :many
SELECT * FROM host_uninstall_tokens
WHERE enterprise_id = $1 AND host_id = $2 ORDER BY created_at DESC, id;
