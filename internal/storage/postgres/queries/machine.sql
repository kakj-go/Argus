-- name: CreateServiceAccount :one
INSERT INTO service_accounts (id, enterprise_id, name, description, allowed_tool_ids)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListServiceAccounts :many
SELECT * FROM service_accounts WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: GetServiceAccount :one
SELECT * FROM service_accounts WHERE id = $1 AND enterprise_id = $2;

-- name: UpdateServiceAccount :one
UPDATE service_accounts SET
  description = COALESCE(sqlc.narg(description), description),
  allowed_tool_ids = COALESCE(sqlc.narg(allowed_tool_ids), allowed_tool_ids),
  status = COALESCE(sqlc.narg(status), status),
  authorization_version = authorization_version + 1,
  version = version + 1, updated_at = now()
WHERE id = sqlc.arg(id) AND enterprise_id = sqlc.arg(enterprise_id) AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: CreateApiKey :one
INSERT INTO api_keys (id, enterprise_id, service_account_id, name, prefix, secret_hash, authorization_version, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetApiKeyByPrefix :one
SELECT ak.*, sa.status AS service_account_status, sa.authorization_version AS service_account_authorization_version
FROM api_keys ak
JOIN service_accounts sa ON sa.id = ak.service_account_id AND sa.enterprise_id = ak.enterprise_id
JOIN enterprises e ON e.id = ak.enterprise_id
WHERE ak.prefix = $1 AND e.status = 'active';

-- name: MarkApiKeyUsed :exec
UPDATE api_keys SET last_used_at = now() WHERE id = $1;

-- name: ListApiKeys :many
SELECT * FROM api_keys WHERE enterprise_id = $1 AND service_account_id = $2 ORDER BY created_at, id;

-- name: GetApiKey :one
SELECT * FROM api_keys WHERE id = $1 AND enterprise_id = $2;

-- name: RevokeApiKey :one
UPDATE api_keys SET status = 'revoked', revoked_at = now(), version = version + 1
WHERE id = $1 AND enterprise_id = $2 AND version = $3 AND status = 'active'
RETURNING *;
