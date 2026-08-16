-- name: UpsertPermission :exec
INSERT INTO permissions (id, description, registry_version) VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET description = EXCLUDED.description, registry_version = EXCLUDED.registry_version;

-- name: ListPermissions :many
SELECT id FROM permissions ORDER BY id;

-- name: CreateRole :one
INSERT INTO roles (id, enterprise_id, identity_key, name, description, builtin)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetRole :one
SELECT * FROM roles WHERE id = $1 AND enterprise_id = $2;

-- name: GetBuiltinRole :one
SELECT * FROM roles WHERE enterprise_id = $1 AND identity_key = $2 AND builtin = true;

-- name: ListRoles :many
SELECT * FROM roles WHERE enterprise_id = $1 ORDER BY builtin DESC, created_at, id;

-- name: AddRolePermission :exec
INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: DeleteRolePermissions :exec
DELETE FROM role_permissions WHERE role_id = $1;

-- name: ListRolePermissions :many
SELECT permission_id FROM role_permissions WHERE role_id = $1 ORDER BY permission_id;

-- name: UpdateRole :one
UPDATE roles SET
  name = COALESCE(sqlc.narg(name), name),
  description = COALESCE(sqlc.narg(description), description),
  status = COALESCE(sqlc.narg(status), status),
  version = version + 1,
  updated_at = now()
WHERE id = sqlc.arg(id) AND enterprise_id = sqlc.arg(enterprise_id)
  AND version = sqlc.arg(expected_version) AND builtin = false
RETURNING *;

-- name: CreateDataScope :one
INSERT INTO data_scopes (
  id, enterprise_id, name, description, resource_types, explicit_resource_ids,
  label_selector, selector_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetDataScope :one
SELECT * FROM data_scopes WHERE id = $1 AND enterprise_id = $2;

-- name: ListDataScopes :many
SELECT * FROM data_scopes WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: UpdateDataScope :one
UPDATE data_scopes SET
  name = $3, description = $4, resource_types = $5,
  explicit_resource_ids = $6, label_selector = $7, selector_hash = $8,
  status = COALESCE(sqlc.narg(status), status), version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: CreateRoleBinding :one
INSERT INTO role_bindings (id, enterprise_id, subject_type, subject_id, role_id, valid_from, valid_until)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetRoleBinding :one
SELECT * FROM role_bindings WHERE id = $1 AND enterprise_id = $2;

-- name: ListRoleBindings :many
SELECT * FROM role_bindings WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: ReplaceRoleBindingDataScopes :exec
DELETE FROM role_binding_data_scopes WHERE role_binding_id = $1;

-- name: AddRoleBindingDataScope :exec
INSERT INTO role_binding_data_scopes (role_binding_id, data_scope_id, enterprise_id)
VALUES ($1, $2, $3);

-- name: ListRoleBindingDataScopes :many
SELECT data_scope_id FROM role_binding_data_scopes WHERE role_binding_id = $1 ORDER BY data_scope_id;

-- name: UpdateRoleBinding :one
UPDATE role_bindings SET
  valid_from = CASE WHEN sqlc.arg(set_valid_from)::boolean THEN sqlc.narg(valid_from) ELSE valid_from END,
  valid_until = CASE WHEN sqlc.arg(set_valid_until)::boolean THEN sqlc.narg(valid_until) ELSE valid_until END,
  status = COALESCE(sqlc.narg(status), status),
  version = version + 1, updated_at = now()
WHERE id = sqlc.arg(id) AND enterprise_id = sqlc.arg(enterprise_id) AND version = sqlc.arg(expected_version)
RETURNING *;

