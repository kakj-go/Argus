-- name: InitializeAuditChain :exec
INSERT INTO audit_chain_heads (chain_key, domain, enterprise_id, last_hash)
VALUES ($1, $2, $3, $4) ON CONFLICT (chain_key) DO NOTHING;

-- name: LockAuditChain :one
SELECT * FROM audit_chain_heads WHERE chain_key = $1 FOR UPDATE;

-- name: InsertAuditEvent :one
INSERT INTO audit_events (
  id, domain, enterprise_id, actor_type, actor_id, action, resource_type,
  resource_id, result, details, previous_hash, event_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: AdvanceAuditChain :exec
UPDATE audit_chain_heads SET last_event_id = $2, last_hash = $3, version = version + 1
WHERE chain_key = $1;

-- name: ListPlatformAuditEvents :many
SELECT * FROM audit_events WHERE domain = 'platform' ORDER BY created_at DESC, id DESC LIMIT $1;

-- name: ListAllPlatformAuditEvents :many
SELECT * FROM audit_events WHERE domain = 'platform' ORDER BY created_at DESC, id DESC;

-- name: ListEnterpriseAuditEvents :many
SELECT * FROM audit_events WHERE domain = 'enterprise' AND enterprise_id = $1
ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: ListAllEnterpriseAuditEvents :many
SELECT * FROM audit_events WHERE domain = 'enterprise' AND enterprise_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListEnterpriseAdmins :many
SELECT DISTINCT eu.*
FROM enterprise_users eu
JOIN role_bindings rb ON rb.enterprise_id = eu.enterprise_id AND rb.subject_type = 'user' AND rb.subject_id = eu.id AND rb.status = 'active'
JOIN roles r ON r.id = rb.role_id AND r.identity_key = 'enterprise_admin'
WHERE (sqlc.narg(enterprise_id)::uuid IS NULL OR eu.enterprise_id = sqlc.narg(enterprise_id))
ORDER BY eu.created_at, eu.id;

-- name: DisableEnterpriseUser :one
UPDATE enterprise_users SET status = 'disabled', authorization_version = authorization_version + 1,
  version = version + 1, updated_at = now()
WHERE id = $1 AND version = $2
RETURNING *;

-- name: ListEffectiveUserPermissions :many
SELECT DISTINCT rp.permission_id
FROM role_bindings rb
JOIN role_permissions rp ON rp.role_id = rb.role_id
WHERE rb.enterprise_id = $1
  AND rb.status = 'active'
  AND (rb.valid_from IS NULL OR rb.valid_from <= now())
  AND (rb.valid_until IS NULL OR rb.valid_until > now())
  AND ((rb.subject_type = 'user' AND rb.subject_id = sqlc.arg(user_id))
    OR (rb.subject_type = 'department' AND rb.subject_id = sqlc.arg(department_id)))
ORDER BY rp.permission_id;

-- name: ListEffectiveServiceAccountPermissions :many
SELECT DISTINCT rp.permission_id
FROM role_bindings rb
JOIN role_permissions rp ON rp.role_id = rb.role_id
WHERE rb.enterprise_id = $1
  AND rb.status = 'active'
  AND (rb.valid_from IS NULL OR rb.valid_from <= now())
  AND (rb.valid_until IS NULL OR rb.valid_until > now())
  AND rb.subject_type = 'service_account'
  AND rb.subject_id = sqlc.arg(service_account_id)
ORDER BY rp.permission_id;

