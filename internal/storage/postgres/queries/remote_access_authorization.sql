-- name: CreateRemoteAccessRequest :one
INSERT INTO remote_access_requests (id,enterprise_id,requester_id,grant_id,host_id,managed_account_id,protocol,action,reason,status,authorization_version,expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: GetRemoteAccessRequest :one
SELECT * FROM remote_access_requests WHERE id=$1 AND enterprise_id=$2;

-- name: GetRemoteAccessRequestForUpdate :one
SELECT * FROM remote_access_requests WHERE id=$1 AND enterprise_id=$2 FOR UPDATE;

-- name: ListRemoteAccessRequests :many
SELECT * FROM remote_access_requests WHERE enterprise_id=$1 AND (requester_id=$2 OR $3::boolean) ORDER BY created_at DESC,id DESC LIMIT $4;

-- name: CreateRemoteAccessRequirement :one
INSERT INTO remote_access_requirement_snapshots (id,request_id,policy_id,policy_version,approver_role_ids,minimum_approvals,
    separation_of_duties,require_mfa,max_session_seconds,idle_timeout_seconds)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: ListRemoteAccessRequirements :many
SELECT * FROM remote_access_requirement_snapshots WHERE request_id=$1 ORDER BY policy_id;

-- name: CreateRemoteAccessDecision :one
INSERT INTO remote_access_decisions (id,request_id,requirement_id,decision,comment,decided_by)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: ListRemoteAccessDecisions :many
SELECT * FROM remote_access_decisions WHERE request_id=$1 ORDER BY decided_at,id;

-- name: CountRemoteAccessApprovals :one
SELECT count(*)::integer FROM remote_access_decisions decision
JOIN remote_access_requirement_snapshots requirement ON requirement.id=decision.requirement_id
JOIN remote_access_requests request ON request.id=decision.request_id
JOIN role_bindings binding ON binding.enterprise_id=request.enterprise_id AND binding.subject_type='user'
  AND binding.subject_id=decision.decided_by AND binding.status='active' AND binding.role_id=ANY(requirement.approver_role_ids)
WHERE decision.requirement_id=$1 AND decision.decision='approve'
  AND (requirement.separation_of_duties=false OR decision.decided_by<>request.requester_id);

-- name: IsRemoteAccessApproverEligible :one
SELECT EXISTS (
  SELECT 1 FROM remote_access_requirement_snapshots requirement
  JOIN remote_access_requests request ON request.id=requirement.request_id
  JOIN role_bindings binding ON binding.enterprise_id=request.enterprise_id AND binding.subject_type='user'
    AND binding.subject_id=sqlc.arg(approver_id) AND binding.status='active' AND binding.role_id=ANY(requirement.approver_role_ids)
  WHERE requirement.id=sqlc.arg(requirement_id) AND request.id=sqlc.arg(request_id)
    AND (requirement.separation_of_duties=false OR sqlc.arg(approver_id)<>request.requester_id)
    AND (binding.valid_from IS NULL OR binding.valid_from<=now())
    AND (binding.valid_until IS NULL OR binding.valid_until>now())
);

-- name: UpdateRemoteAccessRequirementStatus :one
UPDATE remote_access_requirement_snapshots SET status=$2 WHERE id=$1 AND status='pending' RETURNING *;

-- name: UpdateRemoteAccessRequestStatus :one
UPDATE remote_access_requests SET status=$3,updated_at=now() WHERE id=$1 AND enterprise_id=$2 AND status=$4 RETURNING *;

-- name: CreateRemoteAccessLease :one
INSERT INTO remote_access_leases (id,request_id,enterprise_id,user_id,grant_id,host_id,managed_account_id,protocol,action,
    authorization_version,policy_snapshot_hash,expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: ListRemoteAccessLeases :many
SELECT * FROM remote_access_leases WHERE enterprise_id=$1 AND (user_id=$2 OR $3::boolean) ORDER BY issued_at DESC,id DESC LIMIT $4;

-- name: RevokeRemoteAccessLease :one
UPDATE remote_access_leases SET revoked_at=now(),revoke_reason=$3 WHERE id=$1 AND enterprise_id=$2 AND revoked_at IS NULL RETURNING *;

-- name: RevokeRemoteAccessLeasesByGrant :exec
UPDATE remote_access_leases SET revoked_at=now(),revoke_reason=sqlc.arg(reason)
WHERE grant_id=sqlc.arg(grant_id) AND enterprise_id=sqlc.arg(enterprise_id) AND revoked_at IS NULL;

-- name: RevokeRemoteAccessLeasesByEnterprise :exec
UPDATE remote_access_leases SET revoked_at=now(),revoke_reason=sqlc.arg(reason)
WHERE enterprise_id=sqlc.arg(enterprise_id) AND revoked_at IS NULL;

-- name: InvalidateRemoteAccessRequestsByGrant :exec
UPDATE remote_access_requests SET status='invalidated',updated_at=now()
WHERE grant_id=sqlc.arg(grant_id) AND enterprise_id=sqlc.arg(enterprise_id) AND status IN ('requested','awaiting_approval','authorized');

-- name: InvalidateRemoteAccessRequestsByEnterprise :exec
UPDATE remote_access_requests SET status='invalidated',updated_at=now()
WHERE enterprise_id=$1 AND status IN ('requested','awaiting_approval','authorized');

-- name: GetRemoteAccessLeaseForSession :one
SELECT lease.*, host.connection_mode, bastion.active_connector_id AS connector_id, account.username, account.credential_id
FROM remote_access_leases lease JOIN hosts host ON host.id=lease.host_id AND host.enterprise_id=lease.enterprise_id
JOIN managed_accounts account ON account.id=lease.managed_account_id AND account.enterprise_id=lease.enterprise_id
LEFT JOIN bastion_scopes bastion ON bastion.id=host.bastion_scope_id AND bastion.enterprise_id=host.enterprise_id AND bastion.status='active'
WHERE lease.id=$1 AND lease.enterprise_id=$2 AND lease.user_id=$3 AND lease.revoked_at IS NULL AND lease.expires_at>now()
  AND host.status='active' AND account.status='active';

-- name: CountRemoteAccessCapacity :one
SELECT count(*) FILTER (WHERE user_id=$2)::integer AS user_active,
       count(*) FILTER (WHERE host_id=$3)::integer AS host_active,
       count(*)::integer AS enterprise_active
FROM remote_access_sessions WHERE enterprise_id=$1 AND status IN ('connecting','active','terminating');
