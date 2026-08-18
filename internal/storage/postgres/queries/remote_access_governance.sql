-- name: ListRemoteAccessGrants :many
SELECT * FROM remote_access_grants WHERE enterprise_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: GetRemoteAccessGrant :one
SELECT * FROM remote_access_grants WHERE id = $1 AND enterprise_id = $2;

-- name: CreateRemoteAccessGrant :one
INSERT INTO remote_access_grants (id, enterprise_id, subject_type, subject_id, host_ids, host_selector, host_selector_hash,
    managed_account_ids, protocols, actions, valid_from, valid_until, enabled, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;

-- name: UpdateRemoteAccessGrant :one
UPDATE remote_access_grants SET subject_type=$3, subject_id=$4, host_ids=$5, host_selector=$6, host_selector_hash=$7,
    managed_account_ids=$8, protocols=$9, actions=$10, valid_from=$11, valid_until=$12, enabled=$13,
    version=version+1, updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND version=$14 RETURNING *;

-- name: DisableRemoteAccessGrant :one
UPDATE remote_access_grants SET enabled=false, version=version+1, updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND enabled=true RETURNING *;

-- name: ListCandidateRemoteAccessGrants :many
SELECT access_grant.* FROM remote_access_grants access_grant
JOIN enterprise_users actor ON actor.id=sqlc.arg(actor_id) AND actor.enterprise_id=access_grant.enterprise_id
WHERE access_grant.enterprise_id=sqlc.arg(enterprise_id) AND access_grant.enabled=true AND now() >= access_grant.valid_from AND now() < access_grant.valid_until
  AND ((access_grant.subject_type='user' AND access_grant.subject_id=actor.id) OR (access_grant.subject_type='department' AND access_grant.subject_id=actor.department_id))
ORDER BY access_grant.created_at, access_grant.id;

-- name: ListRemoteAccessPolicies :many
SELECT * FROM remote_access_policies WHERE enterprise_id=$1 ORDER BY priority, id;

-- name: GetRemoteAccessPolicy :one
SELECT * FROM remote_access_policies WHERE id=$1 AND enterprise_id=$2;

-- name: CreateRemoteAccessPolicy :one
INSERT INTO remote_access_policies (id,enterprise_id,name,enabled,priority,protocols,host_selector,host_selector_hash,
    approver_role_ids,minimum_approvals,separation_of_duties,require_mfa,max_session_seconds,idle_timeout_seconds,created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING *;

-- name: UpdateRemoteAccessPolicy :one
UPDATE remote_access_policies SET name=$3,enabled=$4,priority=$5,protocols=$6,host_selector=$7,host_selector_hash=$8,
    approver_role_ids=$9,minimum_approvals=$10,separation_of_duties=$11,require_mfa=$12,max_session_seconds=$13,
    idle_timeout_seconds=$14,version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND version=$15 RETURNING *;

-- name: DisableRemoteAccessPolicy :one
UPDATE remote_access_policies SET enabled=false,version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND enabled=true RETURNING *;
