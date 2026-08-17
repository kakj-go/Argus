-- name: CreateSecret :one
INSERT INTO secrets (id, enterprise_id, name, type, description, created_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: CreateSecretVersion :one
INSERT INTO secret_versions (id, secret_id, enterprise_id, version, provider, key_id, key_version, wrapped_dek, wrap_nonce, nonce, ciphertext, value_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING *;

-- name: GetSecret :one
SELECT s.*, (SELECT count(*) FROM credentials c WHERE c.secret_id = s.id AND c.status = 'active')::bigint AS reference_count
FROM secrets s WHERE s.id = $1 AND s.enterprise_id = $2;

-- name: ListSecrets :many
SELECT s.*, (SELECT count(*) FROM credentials c WHERE c.secret_id = s.id AND c.status = 'active')::bigint AS reference_count
FROM secrets s WHERE s.enterprise_id = $1 ORDER BY s.created_at, s.id;

-- name: GetCurrentSecretVersion :one
SELECT v.* FROM secret_versions v JOIN secrets s ON s.id = v.secret_id
WHERE v.secret_id = $1 AND v.enterprise_id = $2 AND v.version = s.current_version;

-- name: GetSecretVersionByID :one
SELECT * FROM secret_versions WHERE id = $1 AND enterprise_id = $2;

-- name: UpdateSecretMetadata :one
UPDATE secrets SET
  name = COALESCE(sqlc.narg('name'), name),
  description = COALESCE(sqlc.narg('description'), description),
  status = COALESCE(sqlc.narg('status'), status),
  version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $3 RETURNING *;

-- name: AdvanceSecretVersion :one
UPDATE secrets SET current_version = current_version + 1, version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $3 RETURNING *;

-- name: DisableSecret :execrows
UPDATE secrets AS secret SET status = 'disabled', version = secret.version + 1, updated_at = now()
WHERE secret.id = $1 AND secret.enterprise_id = $2 AND secret.version = $3 AND secret.status = 'active'
  AND NOT EXISTS (SELECT 1 FROM credentials AS credential WHERE credential.secret_id = secret.id AND credential.status = 'active');

-- name: CreateCredential :one
INSERT INTO credentials (id, enterprise_id, name, protocol, username, secret_id)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: ListCredentials :many
SELECT * FROM credentials WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: GetCredential :one
SELECT * FROM credentials WHERE id = $1 AND enterprise_id = $2;

-- name: UpdateCredential :one
UPDATE credentials SET
  name = COALESCE(sqlc.narg('name'), name), username = COALESCE(sqlc.narg('username'), username),
  secret_id = COALESCE(sqlc.narg('secret_id'), secret_id), status = COALESCE(sqlc.narg('status'), status),
  version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $3 RETURNING *;

-- name: CreateManagedAccount :one
INSERT INTO managed_accounts (id, enterprise_id, host_id, username, privilege_level, credential_id, allowed_protocols)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: ListManagedAccounts :many
SELECT * FROM managed_accounts WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: GetManagedAccount :one
SELECT * FROM managed_accounts WHERE id = $1 AND enterprise_id = $2;

-- name: UpdateManagedAccount :one
UPDATE managed_accounts SET
  username = COALESCE(sqlc.narg('username'), username), privilege_level = COALESCE(sqlc.narg('privilege_level'), privilege_level),
  credential_id = COALESCE(sqlc.narg('credential_id'), credential_id), allowed_protocols = COALESCE(sqlc.narg('allowed_protocols'), allowed_protocols),
  status = COALESCE(sqlc.narg('status'), status), version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $3 RETURNING *;

-- name: RevokeCredentialLeasesBySecret :exec
UPDATE credential_leases AS lease SET status = 'revoked' WHERE lease.enterprise_id = $1 AND lease.status = 'active'
AND lease.credential_id IN (SELECT credential.id FROM credentials AS credential WHERE credential.secret_id = $2 AND credential.enterprise_id = $1);

-- name: MarkSecretAccessed :exec
UPDATE secrets SET last_accessed_at = now(), updated_at = now() WHERE id = $1 AND enterprise_id = $2;

-- name: CreateCredentialLease :one
INSERT INTO credential_leases (id, enterprise_id, credential_id, secret_version_id, operation_ref, target_resource_type, target_resource_id, recipient_type, recipient_id, protocol, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: ConsumeCredentialLease :execrows
UPDATE credential_leases SET status = 'consumed', consumed_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'active' AND expires_at > now();

-- name: GetCredentialLease :one
SELECT * FROM credential_leases WHERE id = $1 AND enterprise_id = $2;

-- name: RevokeCredentialLease :exec
UPDATE credential_leases SET status = 'revoked'
WHERE id = $1 AND enterprise_id = $2 AND status = 'active';
