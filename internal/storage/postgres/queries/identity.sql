-- name: CreateDepartment :one
INSERT INTO departments (id, enterprise_id, name, description, is_default)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetDepartment :one
SELECT * FROM departments WHERE id = $1 AND enterprise_id = $2;

-- name: ListDepartments :many
SELECT * FROM departments WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: GetDefaultDepartment :one
SELECT * FROM departments WHERE enterprise_id = $1 AND is_default = true;

-- name: UpdateDepartment :one
UPDATE departments SET
  name = COALESCE(sqlc.narg(name), name),
  description = COALESCE(sqlc.narg(description), description),
  status = COALESCE(sqlc.narg(status), status),
  version = version + 1,
  updated_at = now()
WHERE id = sqlc.arg(id) AND enterprise_id = sqlc.arg(enterprise_id) AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: CountDepartmentUsers :one
SELECT count(*) FROM enterprise_users WHERE enterprise_id = $1 AND department_id = $2 AND status = 'active';

-- name: BumpDepartmentUsersAuthorizationVersion :many
UPDATE enterprise_users SET authorization_version = authorization_version + 1, updated_at = now()
WHERE enterprise_id = $1 AND department_id = $2
RETURNING id;

-- name: BumpEnterpriseUsersAuthorizationVersion :many
UPDATE enterprise_users SET authorization_version = authorization_version + 1, updated_at = now()
WHERE enterprise_id = $1 RETURNING id;

-- name: BumpEnterpriseServiceAccountsAuthorizationVersion :many
UPDATE service_accounts SET authorization_version = authorization_version + 1, updated_at = now()
WHERE enterprise_id = $1 RETURNING id;

-- name: CreateEnterpriseUser :one
INSERT INTO enterprise_users (id, enterprise_id, department_id, username, display_name, email)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetEnterpriseUser :one
SELECT * FROM enterprise_users WHERE id = $1 AND enterprise_id = $2;

-- name: GetEnterpriseUserByID :one
SELECT * FROM enterprise_users WHERE id = $1;

-- name: GetEnterpriseUserByUsername :one
SELECT eu.*, e.status AS enterprise_status, d.status AS department_status
FROM enterprise_users eu
JOIN enterprises e ON e.id = eu.enterprise_id
JOIN departments d ON d.id = eu.department_id
WHERE lower(eu.username) = lower($1);

-- name: ListEnterpriseUsers :many
SELECT * FROM enterprise_users WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: UpdateEnterpriseUser :one
UPDATE enterprise_users SET
  display_name = COALESCE(sqlc.narg(display_name), display_name),
  email = CASE WHEN sqlc.arg(set_email)::boolean THEN sqlc.narg(email) ELSE email END,
  department_id = COALESCE(sqlc.narg(department_id), department_id),
  status = COALESCE(sqlc.narg(status), status),
  authorization_version = authorization_version + 1,
  version = version + 1,
  updated_at = now()
WHERE id = sqlc.arg(id) AND enterprise_id = sqlc.arg(enterprise_id) AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: CreatePasswordCredential :one
INSERT INTO password_credentials (id, audience, subject_id, encoded_hash, temporary, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (audience, subject_id) DO UPDATE SET
  encoded_hash = EXCLUDED.encoded_hash,
  temporary = EXCLUDED.temporary,
  expires_at = EXCLUDED.expires_at,
  status = 'active',
  version = password_credentials.version + 1,
  updated_at = now()
RETURNING *;

-- name: GetPasswordCredential :one
SELECT * FROM password_credentials WHERE audience = $1 AND subject_id = $2 AND status = 'active';

-- name: CreateTemporaryCredential :one
INSERT INTO temporary_credentials (id, audience, user_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: SetTemporaryCredentialChallenge :one
UPDATE temporary_credentials SET challenge_hash = $2
WHERE id = $1 AND status = 'active' AND expires_at > now()
RETURNING *;

-- name: GetTemporaryCredentialByChallenge :one
SELECT * FROM temporary_credentials
WHERE challenge_hash = $1 AND status = 'active' AND expires_at > now();

-- name: GetActiveTemporaryCredential :one
SELECT * FROM temporary_credentials
WHERE audience = $1 AND user_id = $2 AND status = 'active' AND expires_at > now()
ORDER BY created_at DESC LIMIT 1;

-- name: ConsumeTemporaryCredential :execrows
UPDATE temporary_credentials SET status = 'consumed', consumed_at = now()
WHERE id = $1 AND status = 'active';

-- name: CreateSession :one
INSERT INTO sessions (
  id, token_hash, csrf_hash, audience, user_id, enterprise_id, department_id,
  authorization_version, locale, idle_expires_at, absolute_expires_at, last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: TouchSession :one
UPDATE sessions SET last_seen_at = $2, idle_expires_at = $3
WHERE id = $1 AND revoked_at IS NULL AND absolute_expires_at > $2
RETURNING *;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = now(), revoke_reason = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSubjectSessions :exec
UPDATE sessions SET revoked_at = now(), revoke_reason = $3
WHERE audience = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeEnterpriseSessions :exec
UPDATE sessions SET revoked_at = now(), revoke_reason = $2
WHERE enterprise_id = $1 AND revoked_at IS NULL;

-- name: UpdatePasswordCredential :one
UPDATE password_credentials SET encoded_hash = $3, temporary = false, expires_at = NULL,
  version = version + 1, updated_at = now()
WHERE audience = $1 AND subject_id = $2 AND version = $4 AND status = 'active'
RETURNING *;

-- name: MarkPlatformLogin :exec
UPDATE platform_users SET updated_at = now() WHERE id = $1;

-- name: MarkEnterpriseLogin :exec
UPDATE enterprise_users SET last_login_at = now(), updated_at = now() WHERE id = $1;

-- name: BumpUserAuthorizationVersion :one
UPDATE enterprise_users SET authorization_version = authorization_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 RETURNING authorization_version;

-- name: BumpServiceAccountAuthorizationVersion :one
UPDATE service_accounts SET authorization_version = authorization_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 RETURNING authorization_version;

-- name: BumpAuthorizationVersionRecord :exec
UPDATE authorization_versions SET version = version + 1, updated_at = now()
WHERE enterprise_id = $1 AND subject_type = $2 AND subject_id = $3;

-- name: InitializeAuthorizationVersion :exec
INSERT INTO authorization_versions (enterprise_id, subject_type, subject_id, version)
VALUES ($1, $2, $3, 1) ON CONFLICT DO NOTHING;

-- name: InsertOutboxEvent :exec
INSERT INTO outbox_events (id, topic, aggregate_type, aggregate_id, payload)
VALUES ($1, $2, $3, $4, $5);

-- name: ClaimOutboxEvents :many
UPDATE outbox_events SET claimed_at = now(), attempts = attempts + 1
WHERE id IN (
  SELECT id FROM outbox_events
  WHERE published_at IS NULL AND available_at <= now() AND (claimed_at IS NULL OR claimed_at < now() - interval '5 minutes')
  ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT $1
)
RETURNING *;

-- name: MarkOutboxPublished :exec
UPDATE outbox_events SET published_at = now(), last_error = NULL WHERE id = $1;

-- name: RetryOutboxEvent :exec
UPDATE outbox_events SET claimed_at = NULL, available_at = $2, last_error = $3 WHERE id = $1;
