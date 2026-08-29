-- name: ListRemoteAccessGrants :many
SELECT * FROM remote_access_grants WHERE enterprise_id = $1 ORDER BY created_at DESC, id DESC;

-- name: GetRemoteAccessGrant :one
SELECT * FROM remote_access_grants WHERE id = $1 AND enterprise_id = $2;

-- name: CreateRemoteAccessGrant :one
INSERT INTO remote_access_grants (id, enterprise_id, subject_type, subject_id, host_ids,
    managed_account_ids, protocols, actions, valid_from, valid_until, status, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: UpdateRemoteAccessGrant :one
UPDATE remote_access_grants SET subject_type=$3, subject_id=$4, host_ids=$5,
    managed_account_ids=$6, protocols=$7, actions=$8, valid_from=$9, valid_until=$10,
    version=version+1, updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND version=$11 RETURNING *;

-- name: TransitionRemoteAccessGrant :one
UPDATE remote_access_grants SET status=$3, version=version+1, updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND status=$4 AND version=$5 RETURNING *;

-- name: CountRemoteAccessGrantReferences :one
SELECT 0::integer AS rules,
       (SELECT count(*)::integer FROM remote_access_requests request WHERE request.grant_id=$1)::integer AS requests,
       (SELECT count(*)::integer FROM remote_access_leases lease WHERE lease.grant_id=$1)::integer AS leases,
       (SELECT count(*)::integer FROM remote_access_sessions session
         JOIN remote_access_leases lease ON lease.id=session.lease_id WHERE lease.grant_id=$1)::integer AS sessions;

-- name: ListCandidateRemoteAccessGrants :many
SELECT access_grant.* FROM remote_access_grants access_grant
JOIN enterprise_users actor ON actor.id=sqlc.arg(actor_id) AND actor.enterprise_id=access_grant.enterprise_id
WHERE access_grant.enterprise_id=sqlc.arg(enterprise_id) AND access_grant.status='enabled' AND now() >= access_grant.valid_from AND now() < access_grant.valid_until
  AND ((access_grant.subject_type='user' AND access_grant.subject_id=actor.id) OR (access_grant.subject_type='department' AND access_grant.subject_id=actor.department_id))
ORDER BY access_grant.created_at, access_grant.id;

-- name: ListRemoteAccessRules :many
SELECT * FROM remote_access_rules WHERE enterprise_id=$1 ORDER BY priority, id;

-- name: GetRemoteAccessRule :one
SELECT * FROM remote_access_rules WHERE id=$1 AND enterprise_id=$2;

-- name: CreateRemoteAccessRule :one
INSERT INTO remote_access_rules (id,enterprise_id,name,description,priority,
    protocols,actions,source_cidrs,time_windows,effects,approval_workflow_id,session_profile_id,status,created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;

-- name: UpdateRemoteAccessRule :one
UPDATE remote_access_rules SET name=$3,description=$4,priority=$5,
    protocols=$6,actions=$7,source_cidrs=$8,time_windows=$9,effects=$10,
    approval_workflow_id=$11,session_profile_id=$12,version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND version=$13 RETURNING *;

-- name: TransitionRemoteAccessRule :one
UPDATE remote_access_rules SET status=$3,version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND status=$4 AND version=$5 RETURNING *;

-- name: CountRemoteAccessRuleReferences :one
SELECT 0::integer AS rules,
       (SELECT count(*)::integer FROM remote_access_requests request
         WHERE request.decision_snapshot->'matched_rule_snapshots' @> jsonb_build_array(jsonb_build_object('id', $1::uuid::text)))::integer AS requests,
       (SELECT count(*)::integer FROM remote_access_leases lease
         WHERE lease.decision_snapshot->'matched_rule_snapshots' @> jsonb_build_array(jsonb_build_object('id', $1::uuid::text)))::integer AS leases,
       (SELECT count(*)::integer FROM remote_access_sessions session
         WHERE session.decision_snapshot->'matched_rule_snapshots' @> jsonb_build_array(jsonb_build_object('id', $1::uuid::text)))::integer AS sessions;

-- name: ListRemoteAccessApprovalWorkflows :many
SELECT * FROM remote_access_approval_workflows WHERE enterprise_id=$1 ORDER BY created_at DESC, id DESC;

-- name: GetRemoteAccessApprovalWorkflow :one
SELECT * FROM remote_access_approval_workflows WHERE id=$1 AND enterprise_id=$2;

-- name: CreateRemoteAccessApprovalWorkflow :one
INSERT INTO remote_access_approval_workflows (id,enterprise_id,name,description,approver_role_ids,minimum_approvals,
    separation_of_duties,approval_timeout_seconds,escalation_after_seconds,timeout_effect,escalation_role_ids,status,created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: UpdateRemoteAccessApprovalWorkflow :one
UPDATE remote_access_approval_workflows SET name=$3,description=$4,approver_role_ids=$5,minimum_approvals=$6,
    separation_of_duties=$7,approval_timeout_seconds=$8,escalation_after_seconds=$9,timeout_effect=$10,escalation_role_ids=$11,
    version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND version=$12 RETURNING *;

-- name: TransitionRemoteAccessApprovalWorkflow :one
UPDATE remote_access_approval_workflows SET status=$3,version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND status=$4 AND version=$5 RETURNING *;

-- name: CountRemoteAccessApprovalWorkflowReferences :one
SELECT (SELECT count(*)::integer FROM remote_access_rules WHERE approval_workflow_id=$1)::integer AS rules,
       (SELECT count(*)::integer FROM remote_access_requests request
         WHERE request.decision_snapshot->'approval_requirements' @> jsonb_build_array(jsonb_build_object('workflow_id', $1::uuid::text)))::integer AS requests,
       (SELECT count(*)::integer FROM remote_access_leases lease
         WHERE lease.decision_snapshot->'approval_requirements' @> jsonb_build_array(jsonb_build_object('workflow_id', $1::uuid::text)))::integer AS leases,
       (SELECT count(*)::integer FROM remote_access_sessions session
         WHERE session.decision_snapshot->'approval_requirements' @> jsonb_build_array(jsonb_build_object('workflow_id', $1::uuid::text)))::integer AS sessions;

-- name: ListRemoteAccessSessionProfiles :many
SELECT * FROM remote_access_session_profiles WHERE enterprise_id=$1 ORDER BY created_at DESC, id DESC;

-- name: GetRemoteAccessSessionProfile :one
SELECT * FROM remote_access_session_profiles WHERE id=$1 AND enterprise_id=$2;

-- name: CreateRemoteAccessSessionProfile :one
INSERT INTO remote_access_session_profiles (id,enterprise_id,name,description,max_session_seconds,idle_timeout_seconds,
    recording_mode,command_audit_mode,clipboard_mode,file_upload_mode,file_download_mode,port_forward_mode,session_share_mode,
    retention_days,status,created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING *;

-- name: UpdateRemoteAccessSessionProfile :one
UPDATE remote_access_session_profiles SET name=$3,description=$4,max_session_seconds=$5,idle_timeout_seconds=$6,
    recording_mode=$7,command_audit_mode=$8,clipboard_mode=$9,file_upload_mode=$10,file_download_mode=$11,
    port_forward_mode=$12,session_share_mode=$13,retention_days=$14,version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND version=$15 RETURNING *;

-- name: TransitionRemoteAccessSessionProfile :one
UPDATE remote_access_session_profiles SET status=$3,version=version+1,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND status=$4 AND version=$5 RETURNING *;

-- name: CountRemoteAccessSessionProfileReferences :one
SELECT (SELECT count(*)::integer FROM remote_access_rules profile_rule WHERE profile_rule.session_profile_id=$1)::integer AS rules,
       (SELECT count(*)::integer FROM remote_access_requests request
         WHERE request.decision_snapshot->'session_profile'->'source_profiles' @> jsonb_build_array(jsonb_build_object('id', $1::uuid::text)))::integer AS requests,
       (SELECT count(*)::integer FROM remote_access_leases lease
         WHERE lease.decision_snapshot->'session_profile'->'source_profiles' @> jsonb_build_array(jsonb_build_object('id', $1::uuid::text)))::integer AS leases,
       (SELECT count(*)::integer FROM remote_access_sessions session
         WHERE session.decision_snapshot->'session_profile'->'source_profiles' @> jsonb_build_array(jsonb_build_object('id', $1::uuid::text)))::integer AS sessions;
