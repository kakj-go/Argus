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

-- name: ListUserAuthorizedResourceIDs :many
SELECT DISTINCT dag.resource_id
FROM data_authorization_grants dag
LEFT JOIN enterprise_users eu
  ON dag.subject_type = 'department'
 AND eu.department_id = dag.subject_id
 AND eu.enterprise_id = dag.enterprise_id
 AND eu.id = sqlc.arg(user_id)
LEFT JOIN role_bindings rb
  ON dag.subject_type = 'role'
 AND rb.role_id = dag.subject_id
 AND rb.enterprise_id = dag.enterprise_id
 AND rb.subject_type IN ('user', 'department')
 AND rb.status = 'active'
LEFT JOIN enterprise_users role_eu
  ON rb.subject_type = 'department'
 AND role_eu.department_id = rb.subject_id
 AND role_eu.enterprise_id = rb.enterprise_id
 AND role_eu.id = sqlc.arg(user_id)
WHERE dag.enterprise_id = sqlc.arg(enterprise_id)
  AND dag.resource_type = sqlc.arg(resource_type)
  AND dag.status = 'active'
  AND (
    (dag.subject_type = 'user' AND dag.subject_id = sqlc.arg(user_id))
    OR (dag.subject_type = 'department' AND eu.id IS NOT NULL)
    OR (dag.subject_type = 'role' AND (
      EXISTS (SELECT 1 FROM role_bindings rbu WHERE rbu.enterprise_id = dag.enterprise_id AND rbu.subject_type = 'user' AND rbu.subject_id = sqlc.arg(user_id) AND rbu.role_id = dag.subject_id AND rbu.status = 'active')
      OR role_eu.id IS NOT NULL
    ))
  )
ORDER BY dag.resource_id;

-- name: ListServiceAccountAuthorizedResourceIDs :many
SELECT DISTINCT dag.resource_id
FROM data_authorization_grants dag
LEFT JOIN role_bindings rb
  ON dag.subject_type = 'role'
 AND rb.role_id = dag.subject_id
 AND rb.enterprise_id = dag.enterprise_id
 AND rb.subject_type = 'service_account'
 AND rb.subject_id = sqlc.arg(service_account_id)
 AND rb.status = 'active'
WHERE dag.enterprise_id = sqlc.arg(enterprise_id)
  AND dag.resource_type = sqlc.arg(resource_type)
  AND dag.status = 'active'
  AND (
    (dag.subject_type = 'service_account' AND dag.subject_id = sqlc.arg(service_account_id))
    OR rb.id IS NOT NULL
  )
ORDER BY dag.resource_id;

-- name: ListDepartmentAuthorizedResourceIDs :many
SELECT DISTINCT dag.resource_id
FROM data_authorization_grants dag
JOIN role_bindings rb
  ON dag.subject_type = 'role'
 AND rb.role_id = dag.subject_id
 AND rb.enterprise_id = dag.enterprise_id
 AND rb.subject_type = 'department'
 AND rb.subject_id = sqlc.arg(department_id)
 AND rb.status = 'active'
WHERE dag.enterprise_id = sqlc.arg(enterprise_id)
  AND dag.resource_type = sqlc.arg(resource_type)
  AND dag.status = 'active'
ORDER BY dag.resource_id;

-- name: ListDataAuthorizationGrants :many
SELECT * FROM data_authorization_grants
WHERE enterprise_id = sqlc.arg(enterprise_id)
  AND subject_type = sqlc.arg(subject_type)
  AND subject_id = sqlc.arg(subject_id)
  AND resource_type = sqlc.arg(resource_type)
  AND status = 'active'
ORDER BY resource_id;

-- name: GetAuthorizationVersion :one
SELECT COALESCE((SELECT version FROM authorization_versions
WHERE enterprise_id = sqlc.arg(enterprise_id)
  AND subject_type = sqlc.arg(subject_type)
  AND subject_id = sqlc.arg(subject_id)), 1)::bigint AS version;

-- name: ListUserIDsForRole :many
SELECT DISTINCT eu.id
FROM enterprise_users eu
JOIN role_bindings rb ON rb.enterprise_id = eu.enterprise_id
 AND rb.subject_type = 'user' AND rb.subject_id = eu.id
 AND rb.role_id = sqlc.arg(role_id) AND rb.status = 'active'
WHERE eu.enterprise_id = sqlc.arg(enterprise_id);

-- name: ListUserIDsForDepartmentRole :many
SELECT DISTINCT eu.id
FROM enterprise_users eu
JOIN role_bindings rb ON rb.enterprise_id = eu.enterprise_id
 AND rb.subject_type = 'department' AND rb.subject_id = eu.department_id
 AND rb.role_id = sqlc.arg(role_id) AND rb.status = 'active'
WHERE eu.enterprise_id = sqlc.arg(enterprise_id) AND eu.status = 'active';

-- name: ListServiceAccountIDsForRole :many
SELECT DISTINCT sa.id
FROM service_accounts sa
JOIN role_bindings rb ON rb.enterprise_id = sa.enterprise_id
 AND rb.subject_type = 'service_account' AND rb.subject_id = sa.id
 AND rb.role_id = sqlc.arg(role_id) AND rb.status = 'active'
WHERE sa.enterprise_id = sqlc.arg(enterprise_id) AND sa.status = 'active';

-- name: CountRoleMembers :one
WITH members AS (
  SELECT eu.id AS member_id
  FROM enterprise_users eu
  JOIN role_bindings rb ON rb.enterprise_id = eu.enterprise_id
    AND rb.subject_type = 'user' AND rb.subject_id = eu.id
    AND rb.role_id = sqlc.arg(role_id) AND rb.status = 'active'
  WHERE eu.enterprise_id = sqlc.arg(enterprise_id) AND eu.status = 'active'
  UNION
  SELECT eu.id
  FROM enterprise_users eu
  JOIN role_bindings rb ON rb.enterprise_id = eu.enterprise_id
    AND rb.subject_type = 'department' AND rb.subject_id = eu.department_id
    AND rb.role_id = sqlc.arg(role_id) AND rb.status = 'active'
  WHERE eu.enterprise_id = sqlc.arg(enterprise_id) AND eu.status = 'active'
), machine_members AS (
  SELECT sa.id AS member_id
  FROM service_accounts sa
  JOIN role_bindings rb ON rb.enterprise_id = sa.enterprise_id
    AND rb.subject_type = 'service_account' AND rb.subject_id = sa.id
    AND rb.role_id = sqlc.arg(role_id) AND rb.status = 'active'
  WHERE sa.enterprise_id = sqlc.arg(enterprise_id) AND sa.status = 'active'
)
SELECT ((SELECT count(*) FROM members) + (SELECT count(*) FROM machine_members))::bigint;

-- name: AddDataAuthorizationGrant :one
INSERT INTO data_authorization_grants (id, enterprise_id, subject_type, subject_id, resource_type, resource_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (enterprise_id, subject_type, subject_id, resource_type, resource_id)
DO UPDATE SET status = 'active', version = data_authorization_grants.version + 1, updated_at = now()
RETURNING *;

-- name: DisableDataAuthorizationGrant :exec
UPDATE data_authorization_grants
SET status = 'disabled', version = version + 1, updated_at = now()
WHERE enterprise_id = sqlc.arg(enterprise_id)
  AND subject_type = sqlc.arg(subject_type)
  AND subject_id = sqlc.arg(subject_id)
  AND resource_type = sqlc.arg(resource_type)
  AND resource_id = sqlc.arg(resource_id)
  AND status = 'active';

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

-- name: CreateRoleBinding :one
INSERT INTO role_bindings (id, enterprise_id, subject_type, subject_id, role_id, valid_from, valid_until)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetRoleBinding :one
SELECT * FROM role_bindings WHERE id = $1 AND enterprise_id = $2;

-- name: ListRoleBindings :many
SELECT * FROM role_bindings WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: ListEffectiveRoleBindingsForSubject :many
SELECT binding.*
FROM role_bindings binding
JOIN roles role ON role.id = binding.role_id AND role.enterprise_id = binding.enterprise_id
WHERE binding.enterprise_id = sqlc.arg(enterprise_id)
  AND binding.subject_type = sqlc.arg(subject_type)
  AND binding.subject_id = sqlc.arg(subject_id)
  AND binding.status = 'active'
  AND role.status = 'active'
  AND (binding.valid_from IS NULL OR binding.valid_from <= now())
  AND (binding.valid_until IS NULL OR binding.valid_until > now())
ORDER BY binding.created_at, binding.id;

-- name: UpsertPermanentUserRoleBinding :one
INSERT INTO role_bindings (id, enterprise_id, subject_type, subject_id, role_id)
VALUES (sqlc.arg(id), sqlc.arg(enterprise_id), 'user', sqlc.arg(user_id), sqlc.arg(role_id))
ON CONFLICT (enterprise_id, subject_type, subject_id, role_id)
DO UPDATE SET status = 'active', valid_from = NULL, valid_until = NULL,
  version = role_bindings.version + 1, updated_at = now()
RETURNING *;

-- name: DisableUserRoleBindingsExcept :exec
UPDATE role_bindings
SET status = 'disabled', version = version + 1, updated_at = now()
WHERE enterprise_id = sqlc.arg(enterprise_id)
  AND subject_type = 'user'
  AND subject_id = sqlc.arg(user_id)
  AND status = 'active'
  AND NOT (role_id = ANY(sqlc.arg(role_ids)::uuid[]));

-- name: CountEnterpriseIAMManagers :one
SELECT count(*)::bigint
FROM enterprise_users enterprise_user
WHERE enterprise_user.enterprise_id = sqlc.arg(enterprise_id)
  AND enterprise_user.status = 'active'
  AND EXISTS (
    SELECT 1
    FROM role_bindings binding
    JOIN roles role ON role.id = binding.role_id AND role.enterprise_id = binding.enterprise_id AND role.status = 'active'
    WHERE binding.enterprise_id = enterprise_user.enterprise_id
      AND binding.status = 'active'
      AND (binding.valid_from IS NULL OR binding.valid_from <= now())
      AND (binding.valid_until IS NULL OR binding.valid_until > now())
      AND ((binding.subject_type = 'user' AND binding.subject_id = enterprise_user.id)
        OR (binding.subject_type = 'department' AND binding.subject_id = enterprise_user.department_id))
      AND EXISTS (SELECT 1 FROM role_permissions permission WHERE permission.role_id = role.id AND permission.permission_id = 'identity.manage')
  )
  AND EXISTS (
    SELECT 1
    FROM role_bindings binding
    JOIN roles role ON role.id = binding.role_id AND role.enterprise_id = binding.enterprise_id AND role.status = 'active'
    WHERE binding.enterprise_id = enterprise_user.enterprise_id
      AND binding.status = 'active'
      AND (binding.valid_from IS NULL OR binding.valid_from <= now())
      AND (binding.valid_until IS NULL OR binding.valid_until > now())
      AND ((binding.subject_type = 'user' AND binding.subject_id = enterprise_user.id)
        OR (binding.subject_type = 'department' AND binding.subject_id = enterprise_user.department_id))
      AND EXISTS (SELECT 1 FROM role_permissions permission WHERE permission.role_id = role.id AND permission.permission_id = 'role.manage')
  );

-- name: UpdateRoleBinding :one
UPDATE role_bindings SET
  valid_from = CASE WHEN sqlc.arg(set_valid_from)::boolean THEN sqlc.narg(valid_from) ELSE valid_from END,
  valid_until = CASE WHEN sqlc.arg(set_valid_until)::boolean THEN sqlc.narg(valid_until) ELSE valid_until END,
  status = COALESCE(sqlc.narg(status), status),
  version = version + 1, updated_at = now()
WHERE id = sqlc.arg(id) AND enterprise_id = sqlc.arg(enterprise_id) AND version = sqlc.arg(expected_version)
RETURNING *;
