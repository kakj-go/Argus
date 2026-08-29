-- name: CreateRemoteAccessSession :one
INSERT INTO remote_access_sessions (id,enterprise_id,user_id,http_session_id,lease_id,host_id,managed_account_id,protocol,
    connection_mode,connector_id,status,authorization_version,idle_timeout_seconds,max_duration_seconds,connect_before,
    decision_snapshot,session_profile_snapshot,decision_snapshot_hash,recording_mode,command_audit_mode,clipboard_mode,
    file_upload_mode,file_download_mode,port_forward_mode,session_share_mode,retention_days,reason)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'authorized',$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,sqlc.arg(reason)) RETURNING *;

-- name: GetRemoteAccessSession :one
SELECT * FROM remote_access_sessions WHERE id=$1 AND enterprise_id=$2;

-- name: ListRemoteAccessSessions :many
SELECT * FROM remote_access_sessions session
WHERE session.enterprise_id=sqlc.arg(enterprise_id)
  AND (session.user_id=sqlc.arg(actor_id) OR sqlc.arg(all_sessions)::boolean)
  AND CASE sqlc.arg(scope)::text
    WHEN 'active' THEN session.status IN ('authorized','connecting','active','terminating')
    WHEN 'history' THEN session.status NOT IN ('authorized','connecting','active','terminating')
    WHEN 'all' THEN true
    ELSE false
  END
  AND (sqlc.narg(status)::text IS NULL OR session.status=sqlc.narg(status)::text)
  AND (sqlc.narg(user_id)::uuid IS NULL OR session.user_id=sqlc.narg(user_id)::uuid)
  AND (sqlc.narg(host_id)::uuid IS NULL OR session.host_id=sqlc.narg(host_id)::uuid)
  AND (sqlc.narg(managed_account_id)::uuid IS NULL OR session.managed_account_id=sqlc.narg(managed_account_id)::uuid)
  AND (sqlc.narg(protocol)::text IS NULL OR session.protocol=sqlc.narg(protocol)::text)
  AND (sqlc.narg(connection_mode)::text IS NULL OR session.connection_mode=sqlc.narg(connection_mode)::text)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR session.created_at>=sqlc.narg(created_from)::timestamptz)
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR session.created_at<sqlc.narg(created_to)::timestamptz)
ORDER BY session.created_at DESC,session.id DESC;

-- name: TerminateRemoteAccessSession :one
UPDATE remote_access_sessions SET status='terminating',session_fence=session_fence+1,termination_reason=$3,updated_at=now()
WHERE id=$1 AND enterprise_id=$2 AND status IN ('authorized','connecting','active') RETURNING *;

-- name: TerminateRemoteAccessSessionsByLease :many
UPDATE remote_access_sessions SET status='terminating',session_fence=session_fence+1,termination_reason=sqlc.arg(reason),updated_at=now()
WHERE lease_id=sqlc.arg(lease_id) AND enterprise_id=sqlc.arg(enterprise_id) AND status IN ('authorized','connecting','active') RETURNING *;

-- name: TerminateRemoteAccessSessionsByGrant :many
UPDATE remote_access_sessions session SET status='terminating',session_fence=session_fence+1,termination_reason=sqlc.arg(reason),updated_at=now()
FROM remote_access_leases lease
WHERE session.lease_id=lease.id AND lease.grant_id=sqlc.arg(grant_id) AND session.enterprise_id=sqlc.arg(enterprise_id)
  AND session.status IN ('authorized','connecting','active') RETURNING session.*;

-- name: TerminateRemoteAccessSessionsByEnterprise :many
UPDATE remote_access_sessions SET status='terminating',session_fence=session_fence+1,termination_reason=sqlc.arg(reason),updated_at=now()
WHERE enterprise_id=sqlc.arg(enterprise_id) AND status IN ('authorized','connecting','active') RETURNING *;

-- name: TerminateRemoteAccessSessionsByRequest :many
UPDATE remote_access_sessions session SET status='terminating',session_fence=session_fence+1,termination_reason=sqlc.arg(reason),updated_at=now()
FROM remote_access_leases lease
WHERE session.lease_id=lease.id AND lease.request_id=sqlc.arg(request_id)
  AND session.status IN ('authorized','connecting','active') RETURNING session.*;

-- name: CreateRemoteAccessTicket :one
INSERT INTO remote_access_tickets (id,session_id,ticket_hash,http_session_id,enterprise_id,user_id,host_id,managed_account_id,
    protocol,lease_id,authorization_version,session_fence,expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: GetCurrentRemoteAccessAuthorizationVersion :one
SELECT actor.authorization_version FROM enterprise_users actor
JOIN enterprises enterprise ON enterprise.id=actor.enterprise_id AND enterprise.status='active'
WHERE actor.id=sqlc.arg(user_id) AND actor.enterprise_id=sqlc.arg(enterprise_id) AND actor.status='active';

-- name: ConsumeRemoteAccessTicket :one
UPDATE remote_access_tickets ticket SET consumed_at=now()
WHERE ticket_hash=$1 AND consumed_at IS NULL AND expires_at>$2 AND session_id=$3 AND http_session_id=$4 AND enterprise_id=$5
  AND user_id=$6 AND host_id=$7 AND managed_account_id=$8 AND protocol=$9 AND lease_id=$10
  AND authorization_version=$11 AND session_fence=$12
RETURNING *;

-- name: ConsumeRemoteAccessTicketForGateway :one
-- status='authorized' 是首次接入；status='active' 允许浏览器断开后重接
-- （会话驻留期间由 Gateway 内存注册表持有 PTY 后端）。
UPDATE remote_access_tickets ticket SET consumed_at=now()
FROM remote_access_sessions session, remote_access_leases lease, remote_access_grants access_grant,
     enterprise_users actor, enterprises enterprise
WHERE ticket.ticket_hash=sqlc.arg(ticket_hash) AND ticket.session_id=sqlc.arg(session_id)
  AND ticket.consumed_at IS NULL AND ticket.expires_at>sqlc.arg(now)
  AND session.id=ticket.session_id AND session.status IN ('authorized','active') AND (
    session.status='authorized' AND session.connect_before>sqlc.arg(now)
    OR session.status='active'
  )
  AND session.session_fence=ticket.session_fence AND session.authorization_version=ticket.authorization_version
  AND lease.id=session.lease_id AND lease.revoked_at IS NULL AND lease.expires_at>sqlc.arg(now)
  AND access_grant.id=lease.grant_id AND access_grant.status='enabled' AND access_grant.valid_until>sqlc.arg(now)
  AND actor.id=ticket.user_id AND actor.enterprise_id=ticket.enterprise_id AND actor.status='active'
  AND actor.authorization_version=ticket.authorization_version
  AND enterprise.id=ticket.enterprise_id AND enterprise.status='active'
RETURNING ticket.*;

-- name: GetRemoteAccessSessionTarget :one
SELECT session.*, host.address,host.hostname,host.port,host.pinned_host_key,account.username,account.credential_id,
       connector.connection_epoch,lease.expires_at AS lease_expires_at
FROM remote_access_sessions session
JOIN remote_access_leases lease ON lease.id=session.lease_id AND lease.enterprise_id=session.enterprise_id
JOIN hosts host ON host.id=session.host_id AND host.enterprise_id=session.enterprise_id AND host.status='active'
JOIN managed_accounts account ON account.id=session.managed_account_id AND account.enterprise_id=session.enterprise_id AND account.status='active'
LEFT JOIN connectors connector ON connector.id=session.connector_id AND connector.enterprise_id=session.enterprise_id AND connector.status='online'
WHERE session.id=$1;

-- name: MarkRemoteAccessSessionConnecting :one
UPDATE remote_access_sessions SET status='connecting',updated_at=now()
WHERE id=$1 AND session_fence=$2 AND status='authorized' RETURNING *;

-- name: MarkRemoteAccessSessionActive :one
UPDATE remote_access_sessions SET status='active',connected_at=COALESCE(connected_at,now()),updated_at=now()
WHERE id=$1 AND session_fence=$2 AND status='connecting' RETURNING *;

-- name: FinishRemoteAccessSession :one
UPDATE remote_access_sessions SET status=$3,termination_reason=$4,terminated_at=now(),updated_at=now()
WHERE id=$1 AND session_fence=$2 AND status IN ('authorized','connecting','active','terminating') RETURNING *;

-- name: CreateRemoteAccessRecording :one
INSERT INTO remote_access_recordings (id,enterprise_id,session_id,key_provider,key_id,key_version,wrapped_dek)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: GetRemoteAccessRecording :one
SELECT * FROM remote_access_recordings WHERE id=$1 AND enterprise_id=$2 AND retention_until>now();

-- name: ListRemoteAccessRecordings :many
SELECT recording.* FROM remote_access_recordings recording
JOIN remote_access_sessions session ON session.id=recording.session_id AND session.enterprise_id=recording.enterprise_id
WHERE recording.enterprise_id=sqlc.arg(enterprise_id) AND recording.retention_until>now()
  AND (sqlc.narg(status)::text IS NULL OR recording.status=sqlc.narg(status)::text)
  AND (sqlc.narg(session_id)::uuid IS NULL OR recording.session_id=sqlc.narg(session_id)::uuid)
  AND (sqlc.narg(user_id)::uuid IS NULL OR session.user_id=sqlc.narg(user_id)::uuid)
  AND (sqlc.narg(host_id)::uuid IS NULL OR session.host_id=sqlc.narg(host_id)::uuid)
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR recording.created_at>=sqlc.narg(created_from)::timestamptz)
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR recording.created_at<sqlc.narg(created_to)::timestamptz)
ORDER BY recording.created_at DESC,recording.id DESC;

-- name: GetRemoteAccessRecordingBySession :one
SELECT * FROM remote_access_recordings WHERE session_id=$1 AND enterprise_id=$2 AND retention_until>now();

-- name: GetRemoteAccessRecordingForGateway :one
SELECT * FROM remote_access_recordings WHERE session_id=$1 AND status='recording' FOR UPDATE;

-- name: CreateRemoteAccessRecordingChunk :one
INSERT INTO remote_access_recording_chunks (recording_id,sequence,object_key,nonce,ciphertext_size,event_count,started_at,ended_at,previous_hash,chunk_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: AdvanceRemoteAccessRecording :one
UPDATE remote_access_recordings SET chunk_count=$2,event_count=event_count+$3,size_bytes=size_bytes+$4,
    duration_ms=GREATEST(duration_ms,$5),final_hash=$6
WHERE id=$1 AND status='recording' AND chunk_count=$2-1 RETURNING *;

-- name: FinishRemoteAccessRecording :one
UPDATE remote_access_recordings SET status=$2,completed_at=now()
WHERE id=$1 AND status='recording' RETURNING *;

-- name: ExpireRemoteAccessRecordings :many
WITH expired AS (
  SELECT id
  FROM remote_access_recordings
  WHERE retention_until <= now() AND status <> 'expired'
  ORDER BY retention_until, id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE remote_access_recordings recording
SET status='expired', completed_at=COALESCE(recording.completed_at, now())
FROM expired
WHERE recording.id = expired.id
RETURNING recording.*;

-- name: ConvergeInvalidRemoteAccessSessions :many
WITH candidates AS (
  SELECT session.id
  FROM remote_access_sessions session
  JOIN remote_access_leases lease ON lease.id=session.lease_id
  JOIN remote_access_grants access_grant ON access_grant.id=lease.grant_id
  LEFT JOIN enterprise_users actor ON actor.id=session.user_id AND actor.enterprise_id=session.enterprise_id
  LEFT JOIN enterprises enterprise ON enterprise.id=session.enterprise_id
  LEFT JOIN hosts host ON host.id=session.host_id AND host.enterprise_id=session.enterprise_id
  LEFT JOIN managed_accounts account ON account.id=session.managed_account_id AND account.enterprise_id=session.enterprise_id
  WHERE session.status IN ('authorized','connecting','active')
    AND (
      lease.revoked_at IS NOT NULL OR lease.expires_at<=now()
      OR access_grant.status<>'enabled' OR access_grant.valid_until<=now()
      OR actor.id IS NULL OR actor.status<>'active' OR actor.authorization_version<>session.authorization_version
      OR enterprise.id IS NULL OR enterprise.status<>'active'
      OR host.id IS NULL OR host.status<>'active'
      OR account.id IS NULL OR account.status<>'active'
    )
  ORDER BY session.updated_at,session.id
  FOR UPDATE OF session SKIP LOCKED
  LIMIT $1
)
UPDATE remote_access_sessions session
SET status='terminating',session_fence=session_fence+1,termination_reason='authorization_invalidated',updated_at=now()
FROM candidates
WHERE session.id=candidates.id
RETURNING session.*;

-- name: ConvergeStuckTerminatingRemoteAccessSessions :many
WITH candidates AS (
  SELECT id
  FROM remote_access_sessions
  WHERE status='terminating' AND updated_at < now() - interval '2 minutes'
  ORDER BY updated_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE remote_access_sessions session
SET status='failed', terminated_at=COALESCE(terminated_at, now()),
    termination_reason=COALESCE(NULLIF(termination_reason, ''), 'REMOTE_ACCESS_TERMINATION_TIMEOUT'), updated_at=now()
FROM candidates
WHERE session.id=candidates.id
RETURNING session.*;

-- name: LockOptionalSessionsMissingRecording :many
SELECT session.*
FROM remote_access_sessions session
WHERE session.status IN ('authorized','connecting','active')
  AND session.recording_mode = 'optional'
  AND NOT EXISTS (
    SELECT 1 FROM remote_access_recordings recording WHERE recording.session_id = session.id
  )
ORDER BY session.created_at, session.id
FOR UPDATE OF session SKIP LOCKED
LIMIT $1;

-- name: CreateRemoteAccessCommandEvent :one
INSERT INTO remote_access_command_events (id,session_id,sequence,event_type,command_hash,occurred_at)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: ListRemoteAccessRecordingChunks :many
SELECT * FROM remote_access_recording_chunks WHERE recording_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3;

-- name: UpsertRemoteAccessRoute :one
INSERT INTO remote_access_routes (session_id,gateway_instance,connector_id,connector_epoch,session_fence,lease_expires_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,now())
ON CONFLICT (session_id) DO UPDATE SET gateway_instance=EXCLUDED.gateway_instance,connector_id=EXCLUDED.connector_id,
  connector_epoch=EXCLUDED.connector_epoch,session_fence=EXCLUDED.session_fence,lease_expires_at=EXCLUDED.lease_expires_at,updated_at=now()
WHERE remote_access_routes.session_fence <= EXCLUDED.session_fence
RETURNING *;

-- name: SetRemoteAccessSessionRouteMetadata :exec
UPDATE remote_access_sessions
SET gateway_instance=$2,connector_epoch=$3,updated_at=now()
WHERE id=$1 AND session_fence=$4 AND status IN ('authorized','connecting','active');

-- name: GetRemoteAccessRoute :one
SELECT * FROM remote_access_routes WHERE session_id=$1 AND lease_expires_at>now();

-- name: DeleteRemoteAccessRoute :execrows
DELETE FROM remote_access_routes WHERE session_id=$1 AND session_fence=$2;
