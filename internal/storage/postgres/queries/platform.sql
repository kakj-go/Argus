-- name: GetPlatformState :one
SELECT state, initialized_at, updated_at FROM platform_state WHERE singleton = true;

-- name: LockPlatformState :one
SELECT state, initialized_at, updated_at FROM platform_state WHERE singleton = true FOR UPDATE;

-- name: MarkPlatformInitializing :execrows
UPDATE platform_state SET state = 'initializing', updated_at = now()
WHERE singleton = true AND state = 'uninitialized';

-- name: MarkPlatformInitialized :exec
UPDATE platform_state SET state = 'initialized', initialized_at = now(), updated_at = now()
WHERE singleton = true AND state = 'initializing';

-- name: CreatePlatformSettings :one
INSERT INTO platform_settings (platform_name, default_locale, timezone, external_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetPlatformSettings :one
SELECT * FROM platform_settings WHERE singleton = true;

-- name: CreatePlatformUser :one
INSERT INTO platform_users (id, username, display_name, email)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetPlatformUserByUsername :one
SELECT * FROM platform_users WHERE lower(username) = lower($1);

-- name: GetPlatformUser :one
SELECT * FROM platform_users WHERE id = $1;

-- name: CreateEnterprise :one
INSERT INTO enterprises (id, name, code, timezone, default_locale, remark)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetEnterprise :one
SELECT * FROM enterprises WHERE id = $1;

-- name: ListEnterprises :many
SELECT * FROM enterprises
WHERE ($1::timestamptz IS NULL OR (created_at, id) < ($1, $2))
ORDER BY created_at DESC, id DESC LIMIT $3;

-- name: ListAllEnterprises :many
SELECT * FROM enterprises ORDER BY created_at DESC, id DESC;

-- name: UpdateEnterprise :one
UPDATE enterprises SET
  name = COALESCE(sqlc.narg(name), name),
  timezone = COALESCE(sqlc.narg(timezone), timezone),
  default_locale = COALESCE(sqlc.narg(default_locale), default_locale),
  remark = COALESCE(sqlc.narg(remark), remark),
  version = version + 1,
  updated_at = now()
WHERE id = sqlc.arg(id) AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: ChangeEnterpriseStatus :one
UPDATE enterprises SET status = $2, version = version + 1, updated_at = now()
WHERE id = $1 AND version = $3
RETURNING *;

