-- name: CreateRemoteAccessRequest :one
INSERT INTO remote_access_requests (id,enterprise_id,requester_id,grant_id,host_id,managed_account_id,protocol,action,reason,status,
    authorization_version,expires_at,decision_outcome,decision_reason_codes,decision_snapshot,decision_snapshot_hash,
    matched_grant_snapshots,matched_rule_snapshots,decision_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING *;

-- name: GetRemoteAccessRequest :one
SELECT * FROM remote_access_requests WHERE id=$1 AND enterprise_id=$2;

-- name: GetRemoteAccessRequestForUpdate :one
SELECT * FROM remote_access_requests WHERE id=$1 AND enterprise_id=$2 FOR UPDATE;

-- name: GetRemoteAccessRequestForReconcile :one
SELECT * FROM remote_access_requests WHERE id=$1 FOR UPDATE;

-- name: CanReadRemoteAccessRequestAsApprover :one
SELECT EXISTS (
  SELECT 1
  FROM remote_access_requests request
  WHERE request.id=sqlc.arg(request_id) AND request.enterprise_id=sqlc.arg(enterprise_id)
    AND (
      EXISTS (
        SELECT 1 FROM remote_access_decisions decision
        WHERE decision.request_id=request.id AND decision.decided_by=sqlc.arg(approver_id)
      )
      OR EXISTS (
        SELECT 1
        FROM remote_access_requirement_snapshots requirement
        JOIN enterprise_users approver ON approver.id=sqlc.arg(approver_id)
          AND approver.enterprise_id=request.enterprise_id AND approver.status='active'
        JOIN role_bindings binding ON binding.enterprise_id=request.enterprise_id
          AND binding.status='active' AND binding.role_id=ANY(requirement.approver_role_ids)
          AND ((binding.subject_type='user' AND binding.subject_id=sqlc.arg(approver_id))
            OR (binding.subject_type='department' AND binding.subject_id=approver.department_id))
          AND (binding.valid_from IS NULL OR binding.valid_from<=now())
          AND (binding.valid_until IS NULL OR binding.valid_until>now())
        WHERE requirement.request_id=request.id
          AND (requirement.separation_of_duties=false OR request.requester_id<>sqlc.arg(approver_id))
      )
    )
);

-- name: ListRemoteAccessRequests :many
SELECT request.*
FROM remote_access_requests request
WHERE request.enterprise_id=sqlc.arg(enterprise_id)
  AND CASE sqlc.arg(scope)::text
    WHEN 'mine' THEN request.requester_id=sqlc.arg(actor_id)
    WHEN 'approver' THEN request.status='awaiting_approval' AND EXISTS (
      SELECT 1
      FROM remote_access_requirement_snapshots requirement
      JOIN role_bindings binding ON binding.enterprise_id=request.enterprise_id
        AND binding.status='active'
        AND binding.role_id=ANY(requirement.approver_role_ids)
        AND ((binding.subject_type='user' AND binding.subject_id=sqlc.arg(actor_id))
          OR (binding.subject_type='department' AND binding.subject_id=sqlc.arg(department_id)))
        AND (binding.valid_from IS NULL OR binding.valid_from<=now())
        AND (binding.valid_until IS NULL OR binding.valid_until>now())
      WHERE requirement.request_id=request.id
        AND requirement.status='pending'
        AND (requirement.separation_of_duties=false OR request.requester_id<>sqlc.arg(actor_id))
        AND NOT EXISTS (
          SELECT 1 FROM remote_access_decisions decision
          WHERE decision.requirement_id=requirement.id AND decision.decided_by=sqlc.arg(actor_id)
        )
    )
    WHEN 'processed' THEN EXISTS (
      SELECT 1 FROM remote_access_decisions decision
      WHERE decision.request_id=request.id AND decision.decided_by=sqlc.arg(actor_id)
    )
    ELSE false
  END
  AND (sqlc.narg(status)::text IS NULL OR request.status=sqlc.narg(status)::text)
  AND (sqlc.narg(created_by)::uuid IS NULL OR request.requester_id=sqlc.narg(created_by)::uuid)
  AND (sqlc.narg(host_id)::uuid IS NULL OR request.host_id=sqlc.narg(host_id)::uuid)
  AND (sqlc.narg(protocol)::text IS NULL OR request.protocol=sqlc.narg(protocol)::text)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR request.created_at>=sqlc.narg(created_from)::timestamptz)
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR request.created_at<sqlc.narg(created_to)::timestamptz)
ORDER BY request.created_at DESC,request.id DESC;

-- name: CreateRemoteAccessRequirement :one
INSERT INTO remote_access_requirement_snapshots (id,request_id,approver_role_ids,minimum_approvals,
    separation_of_duties,require_mfa,max_session_seconds,idle_timeout_seconds,rule_id,rule_version,workflow_id,workflow_version,
    session_profile_id,session_profile_version,approval_snapshot,deadline_at,escalation_at,timeout_effect,escalation_role_ids)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING *;

-- name: ListRemoteAccessRequirements :many
SELECT * FROM remote_access_requirement_snapshots WHERE request_id=$1 ORDER BY workflow_id, id;

-- name: CreateRemoteAccessDecision :one
INSERT INTO remote_access_decisions (id,request_id,requirement_id,decision,comment,decided_by)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: ListRemoteAccessDecisions :many
SELECT * FROM remote_access_decisions WHERE request_id=$1 ORDER BY decided_at,id;

-- name: CountRemoteAccessApprovals :one
SELECT count(*)::integer FROM remote_access_decisions decision
JOIN remote_access_requirement_snapshots requirement ON requirement.id=decision.requirement_id
JOIN remote_access_requests request ON request.id=decision.request_id
JOIN enterprise_users approver ON approver.id=decision.decided_by AND approver.enterprise_id=request.enterprise_id
JOIN role_bindings binding ON binding.enterprise_id=request.enterprise_id
  AND binding.status='active' AND binding.role_id=ANY(requirement.approver_role_ids)
  AND ((binding.subject_type='user' AND binding.subject_id=decision.decided_by)
    OR (binding.subject_type='department' AND binding.subject_id=approver.department_id))
WHERE decision.requirement_id=$1 AND decision.decision='approve'
  AND (requirement.separation_of_duties=false OR decision.decided_by<>request.requester_id)
  AND (binding.valid_from IS NULL OR binding.valid_from<=decision.decided_at)
  AND (binding.valid_until IS NULL OR binding.valid_until>decision.decided_at);

-- name: IsRemoteAccessApproverEligible :one
SELECT EXISTS (
  SELECT 1 FROM remote_access_requirement_snapshots requirement
  JOIN remote_access_requests request ON request.id=requirement.request_id
  JOIN enterprise_users approver ON approver.id=sqlc.arg(approver_id) AND approver.enterprise_id=request.enterprise_id AND approver.status='active'
  JOIN role_bindings binding ON binding.enterprise_id=request.enterprise_id
    AND binding.status='active' AND binding.role_id=ANY(requirement.approver_role_ids)
    AND ((binding.subject_type='user' AND binding.subject_id=sqlc.arg(approver_id))
      OR (binding.subject_type='department' AND binding.subject_id=approver.department_id))
  WHERE requirement.id=sqlc.arg(requirement_id) AND request.id=sqlc.arg(request_id)
    AND (requirement.separation_of_duties=false OR sqlc.arg(approver_id)<>request.requester_id)
    AND (binding.valid_from IS NULL OR binding.valid_from<=now())
    AND (binding.valid_until IS NULL OR binding.valid_until>now())
);

-- name: UpdateRemoteAccessRequirementStatus :one
UPDATE remote_access_requirement_snapshots SET status=$2 WHERE id=$1 AND status='pending' RETURNING *;

-- name: UpdateRemoteAccessRequestStatus :one
UPDATE remote_access_requests SET status=$3,updated_at=now() WHERE id=$1 AND enterprise_id=$2 AND status=$4 RETURNING *;

-- name: ResumeRemoteAccessRequest :one
UPDATE remote_access_requests SET status=$3,decision_outcome=$4,decision_reason_codes=$5,decision_snapshot=$6,
    decision_snapshot_hash=$7,matched_grant_snapshots=$8,matched_rule_snapshots=$9,expires_at=$10,decision_at=now(),updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND status='awaiting_mfa' RETURNING *;

-- name: CreateRemoteAccessLease :one
INSERT INTO remote_access_leases (id,request_id,enterprise_id,user_id,grant_id,host_id,managed_account_id,protocol,action,
    authorization_version,expires_at,decision_snapshot,session_profile_snapshot,decision_snapshot_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;

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

-- name: RevokeRemoteAccessLeasesByRequest :exec
UPDATE remote_access_leases SET revoked_at=now(),revoke_reason=sqlc.arg(reason)
WHERE request_id=sqlc.arg(request_id) AND revoked_at IS NULL;

-- name: InvalidateRemoteAccessRequestsByGrant :exec
UPDATE remote_access_requests SET status='invalidated',updated_at=now()
WHERE grant_id=sqlc.arg(grant_id) AND enterprise_id=sqlc.arg(enterprise_id) AND status IN ('requested','awaiting_mfa','awaiting_approval','authorized');

-- name: InvalidateRemoteAccessRequestsByEnterprise :exec
UPDATE remote_access_requests SET status='invalidated',updated_at=now()
WHERE enterprise_id=$1 AND status IN ('requested','awaiting_mfa','awaiting_approval','authorized');

-- name: BumpRemoteAccessGovernanceUsersAuthorizationVersion :many
WITH affected AS (
  SELECT DISTINCT request.requester_id
  FROM remote_access_requests request
  WHERE request.enterprise_id=sqlc.arg(enterprise_id)
    AND (
      request.status IN ('requested','awaiting_mfa','awaiting_approval','authorized')
      OR EXISTS (SELECT 1 FROM remote_access_leases lease WHERE lease.request_id=request.id AND lease.revoked_at IS NULL AND lease.expires_at>now())
      OR EXISTS (SELECT 1 FROM remote_access_sessions session JOIN remote_access_leases lease ON lease.id=session.lease_id
                  WHERE lease.request_id=request.id AND session.status IN ('requested','authorized','connecting','active','connection_lost'))
    )
    AND CASE sqlc.arg(source_type)::text
      WHEN 'grant' THEN request.grant_id=sqlc.arg(source_id)::uuid
      WHEN 'host' THEN request.host_id=sqlc.arg(source_id)::uuid
      WHEN 'managed_account' THEN request.managed_account_id=sqlc.arg(source_id)::uuid
      WHEN 'rule' THEN request.decision_snapshot->'matched_rule_snapshots' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
      WHEN 'workflow' THEN request.decision_snapshot->'approval_requirements' @> jsonb_build_array(jsonb_build_object('workflow_id', sqlc.arg(source_id)::uuid::text))
      WHEN 'session_profile' THEN request.decision_snapshot->'session_profile'->'source_profiles' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
      ELSE false
    END
)
UPDATE enterprise_users actor SET authorization_version=authorization_version+1,updated_at=now()
FROM affected WHERE actor.id=affected.requester_id AND actor.enterprise_id=sqlc.arg(enterprise_id)
RETURNING actor.id;

-- name: InvalidateRemoteAccessRequestsByGovernanceSource :exec
UPDATE remote_access_requests request SET status='invalidated',updated_at=now()
WHERE request.enterprise_id=sqlc.arg(enterprise_id)
  AND request.status IN ('requested','awaiting_mfa','awaiting_approval','authorized')
  AND CASE sqlc.arg(source_type)::text
    WHEN 'grant' THEN request.grant_id=sqlc.arg(source_id)::uuid
    WHEN 'host' THEN request.host_id=sqlc.arg(source_id)::uuid
    WHEN 'managed_account' THEN request.managed_account_id=sqlc.arg(source_id)::uuid
    WHEN 'rule' THEN request.decision_snapshot->'matched_rule_snapshots' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
    WHEN 'workflow' THEN request.decision_snapshot->'approval_requirements' @> jsonb_build_array(jsonb_build_object('workflow_id', sqlc.arg(source_id)::uuid::text))
    WHEN 'session_profile' THEN request.decision_snapshot->'session_profile'->'source_profiles' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
    ELSE false
  END;

-- name: RevokeRemoteAccessLeasesByGovernanceSource :exec
UPDATE remote_access_leases lease SET revoked_at=now(),revoke_reason=sqlc.arg(reason)
WHERE lease.enterprise_id=sqlc.arg(enterprise_id) AND lease.revoked_at IS NULL
  AND CASE sqlc.arg(source_type)::text
    WHEN 'grant' THEN lease.grant_id=sqlc.arg(source_id)::uuid
    WHEN 'host' THEN lease.host_id=sqlc.arg(source_id)::uuid
    WHEN 'managed_account' THEN lease.managed_account_id=sqlc.arg(source_id)::uuid
    WHEN 'rule' THEN lease.decision_snapshot->'matched_rule_snapshots' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
    WHEN 'workflow' THEN lease.decision_snapshot->'approval_requirements' @> jsonb_build_array(jsonb_build_object('workflow_id', sqlc.arg(source_id)::uuid::text))
    WHEN 'session_profile' THEN lease.decision_snapshot->'session_profile'->'source_profiles' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
    ELSE false
  END;

-- name: TerminateRemoteAccessSessionsByGovernanceSource :many
UPDATE remote_access_sessions session SET status='invalidated',terminated_at=now(),termination_reason=sqlc.arg(reason),
    session_fence=session_fence+1,updated_at=now()
WHERE session.enterprise_id=sqlc.arg(enterprise_id)
  AND session.status IN ('requested','authorized','connecting','active','connection_lost')
  AND CASE sqlc.arg(source_type)::text
    WHEN 'grant' THEN EXISTS (SELECT 1 FROM remote_access_leases lease WHERE lease.id=session.lease_id AND lease.grant_id=sqlc.arg(source_id)::uuid)
    WHEN 'host' THEN session.host_id=sqlc.arg(source_id)::uuid
    WHEN 'managed_account' THEN session.managed_account_id=sqlc.arg(source_id)::uuid
    WHEN 'rule' THEN session.decision_snapshot->'matched_rule_snapshots' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
    WHEN 'workflow' THEN session.decision_snapshot->'approval_requirements' @> jsonb_build_array(jsonb_build_object('workflow_id', sqlc.arg(source_id)::uuid::text))
    WHEN 'session_profile' THEN session.decision_snapshot->'session_profile'->'source_profiles' @> jsonb_build_array(jsonb_build_object('id', sqlc.arg(source_id)::uuid::text))
    ELSE false
  END
RETURNING *;

-- name: InvalidateRemoteAccessRequestsByUsers :exec
UPDATE remote_access_requests SET status='invalidated',updated_at=now()
WHERE enterprise_id=sqlc.arg(enterprise_id) AND requester_id=ANY(sqlc.arg(user_ids)::uuid[])
  AND status IN ('requested','awaiting_mfa','awaiting_approval','authorized');

-- name: RevokeRemoteAccessLeasesByUsers :exec
UPDATE remote_access_leases SET revoked_at=now(),revoke_reason=sqlc.arg(reason)
WHERE enterprise_id=sqlc.arg(enterprise_id) AND user_id=ANY(sqlc.arg(user_ids)::uuid[])
  AND revoked_at IS NULL;

-- name: TerminateRemoteAccessSessionsByUsers :many
UPDATE remote_access_sessions SET status='invalidated',terminated_at=now(),termination_reason=sqlc.arg(reason),
    session_fence=session_fence+1,updated_at=now()
WHERE enterprise_id=sqlc.arg(enterprise_id) AND user_id=ANY(sqlc.arg(user_ids)::uuid[])
  AND status IN ('requested','authorized','connecting','active','connection_lost')
RETURNING *;

-- name: ExpirePendingRemoteAccessRequests :many
WITH candidates AS (
  SELECT request.id
  FROM remote_access_requests request
  WHERE request.status IN ('requested','awaiting_mfa') AND request.expires_at<=now()
  ORDER BY request.expires_at, request.id
  FOR UPDATE OF request SKIP LOCKED
  LIMIT $1
)
UPDATE remote_access_requests request SET status='expired',updated_at=now()
FROM candidates WHERE request.id=candidates.id
RETURNING request.*;

-- name: ExpireRemoteAccessRequirements :many
WITH candidates AS (
  SELECT requirement.id
  FROM remote_access_requirement_snapshots requirement
  JOIN remote_access_requests request ON request.id=requirement.request_id
  WHERE requirement.status='pending' AND requirement.deadline_at<=now()
    AND request.status='awaiting_approval'
  ORDER BY requirement.deadline_at, requirement.id
  FOR UPDATE OF requirement SKIP LOCKED
  LIMIT $1
)
UPDATE remote_access_requirement_snapshots requirement SET status='invalidated'
FROM candidates WHERE requirement.id=candidates.id
RETURNING requirement.*;

-- name: EscalateRemoteAccessRequirements :many
WITH candidates AS (
  SELECT requirement.id
  FROM remote_access_requirement_snapshots requirement
  JOIN remote_access_requests request ON request.id=requirement.request_id
  WHERE requirement.status='pending' AND requirement.escalated_at IS NULL
    AND cardinality(requirement.escalation_role_ids)>0
    AND requirement.escalation_at<=now()
    AND request.status='awaiting_approval'
  ORDER BY requirement.deadline_at, requirement.id
  FOR UPDATE OF requirement SKIP LOCKED
  LIMIT $1
)
UPDATE remote_access_requirement_snapshots requirement SET escalated_at=now()
FROM candidates WHERE requirement.id=candidates.id
RETURNING requirement.*;

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
