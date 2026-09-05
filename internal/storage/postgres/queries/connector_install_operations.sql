-- PlanV4 Connector 发行、B/C 安装 operation 与模式 C 长期控制隧道。

-- name: CreateConnectorReleaseVersion :one
INSERT INTO connector_release_versions (id, version, status, manifest, manifest_hash)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (version) DO UPDATE SET
  status = EXCLUDED.status, updated_at = now()
WHERE connector_release_versions.manifest_hash = EXCLUDED.manifest_hash
RETURNING *;

-- name: GetActiveConnectorReleaseVersion :one
SELECT * FROM connector_release_versions WHERE status = 'active'
ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: GetConnectorReleaseVersion :one
SELECT * FROM connector_release_versions WHERE id = $1;

-- name: CreateConnectorInstallOperation :one
INSERT INTO connector_install_operations (
  id, enterprise_id, connector_id, bastion_scope_id, host_id, pending_action_id,
  retry_of, release_version_id, connection_test_id, install_mode, plan, plan_hash,
  expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING *;

-- name: CreateConnectorInstallOperationEvent :one
INSERT INTO connector_install_operation_events (
  id, operation_id, enterprise_id, sequence, stage, status, error_code
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: ListConnectorInstallOperationEvents :many
SELECT * FROM connector_install_operation_events
WHERE operation_id = $1 AND enterprise_id = $2 ORDER BY sequence;

-- name: GetConnectorInstallOperation :one
SELECT * FROM connector_install_operations WHERE id = $1 AND enterprise_id = $2;

-- name: GetLatestConnectorInstallOperation :one
SELECT * FROM connector_install_operations
WHERE connector_id = $1 AND enterprise_id = $2
ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: GetLatestConnectorInstallOperationByScope :one
SELECT * FROM connector_install_operations
WHERE bastion_scope_id = $1 AND enterprise_id = $2
ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: ClaimConnectorInstallOperations :many
WITH claimed AS (
  SELECT id FROM connector_install_operations
  WHERE status = 'queued' AND attempts < 3 AND expires_at > now()
  ORDER BY created_at, id LIMIT $1 FOR UPDATE SKIP LOCKED
)
UPDATE connector_install_operations AS operation SET
  status = 'running', stage = 'ssh_connecting', lease_owner = $2,
  fence = fence + 1, lease_expires_at = now() + interval '90 seconds',
  attempts = attempts + 1, updated_at = now()
FROM claimed WHERE operation.id = claimed.id RETURNING operation.*;

-- name: AdvanceConnectorInstallOperation :one
UPDATE connector_install_operations SET
  stage = $4, lease_expires_at = now() + interval '90 seconds', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3
  AND lease_owner <> '' AND status = 'running'
RETURNING *;

-- name: RenewConnectorInstallOperationLease :execrows
UPDATE connector_install_operations SET lease_expires_at = now() + interval '90 seconds', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3 AND lease_owner = $4 AND status = 'running';

-- name: RequeueConnectorInstallOperation :one
UPDATE connector_install_operations SET
  status = 'queued', stage = 'queued', error_code = $4,
  lease_owner = '', lease_expires_at = NULL, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3
  AND status = 'running' AND attempts < 3 AND expires_at > now()
RETURNING *;

-- name: MarkConnectorInstallOnline :one
UPDATE connector_install_operations SET connector_online_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status IN ('running','result_unknown')
RETURNING *;

-- name: FinishConnectorInstallOperation :one
UPDATE connector_install_operations SET
  status = $4, stage = CASE WHEN $4 = 'succeeded' THEN 'completed' ELSE stage END,
  error_code = $5, lease_owner = '', lease_expires_at = NULL,
  completed_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3
RETURNING *;

-- name: RecoverConnectorInstallOperations :execrows
UPDATE connector_install_operations SET
  status = CASE
    WHEN expires_at <= now() THEN 'expired'
    WHEN attempts < 3 THEN 'queued'
    ELSE 'result_unknown'
  END,
  lease_owner = '', lease_expires_at = NULL,
  error_code = CASE WHEN attempts >= 3 THEN 'CONNECTOR_INSTALL_RESULT_UNKNOWN' ELSE error_code END,
  updated_at = now()
WHERE status = 'running' AND (lease_expires_at IS NULL OR lease_expires_at < now());

-- name: ExpireConnectorInstallOperations :execrows
UPDATE connector_install_operations SET status = 'expired', error_code = 'CONNECTOR_INSTALL_EXPIRED', updated_at = now()
WHERE status = 'queued' AND expires_at <= now();

-- name: CreateConnectorInstallOperationSecret :one
INSERT INTO connector_install_operation_secrets (
  operation_id, enterprise_id, key_version, nonce, ciphertext, expires_at
) VALUES ($1,$2,$3,$4,$5,$6)
RETURNING *;

-- name: GetConnectorInstallOperationSecret :one
SELECT * FROM connector_install_operation_secrets
WHERE operation_id = $1 AND enterprise_id = $2 AND expires_at > now();

-- name: ConsumeConnectorInstallOperationSecret :execrows
UPDATE connector_install_operation_secrets SET consumed_at = now()
WHERE operation_id = $1 AND enterprise_id = $2 AND consumed_at IS NULL;

-- name: DeleteConnectorInstallOperationSecret :execrows
DELETE FROM connector_install_operation_secrets WHERE operation_id = $1 AND enterprise_id = $2;

-- name: CreateConnectorControlTunnel :one
INSERT INTO connector_control_tunnels (
  id, enterprise_id, connector_id, bastion_scope_id, host_id,
  credential_id, credential_version, target_address, target_port,
  target_username, pinned_host_key, enroll_forward_target,
  gateway_forward_target, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'desired')
ON CONFLICT (connector_id) DO UPDATE SET
  credential_id = EXCLUDED.credential_id,
  credential_version = EXCLUDED.credential_version,
  target_address = EXCLUDED.target_address,
  target_port = EXCLUDED.target_port,
  target_username = EXCLUDED.target_username,
  pinned_host_key = EXCLUDED.pinned_host_key,
  enroll_forward_target = EXCLUDED.enroll_forward_target,
  gateway_forward_target = EXCLUDED.gateway_forward_target,
  status = 'desired', epoch = connector_control_tunnels.epoch + 1,
  lease_owner = '', lease_expires_at = NULL, last_drop_reason = '',
  reconnect_attempt = 0, next_claim_at = NULL, updated_at = now()
RETURNING *;

-- name: GetConnectorControlTunnel :one
SELECT * FROM connector_control_tunnels WHERE id = $1 AND enterprise_id = $2;

-- name: GetConnectorControlTunnelByConnector :one
SELECT * FROM connector_control_tunnels WHERE connector_id = $1 AND enterprise_id = $2;

-- name: ClaimConnectorControlTunnels :many
WITH claimed AS (
  SELECT id FROM connector_control_tunnels
  WHERE status IN ('desired','degraded','down')
    AND (lease_expires_at IS NULL OR lease_expires_at < now())
    AND (next_claim_at IS NULL OR next_claim_at <= now())
  ORDER BY last_claim_at ASC NULLS FIRST, id
  LIMIT $1 FOR UPDATE SKIP LOCKED
)
UPDATE connector_control_tunnels AS tunnel SET
  status = 'establishing', lease_owner = $2, fence = fence + 1,
  epoch = epoch + 1, lease_expires_at = now() + interval '90 seconds',
  last_claim_at = now(), updated_at = now()
FROM claimed WHERE tunnel.id = claimed.id RETURNING tunnel.*;

-- name: MarkConnectorControlTunnelEstablished :one
UPDATE connector_control_tunnels SET
  status = 'established', last_established_at = now(), last_heartbeat_at = now(),
  lease_expires_at = now() + interval '90 seconds', last_drop_reason = '',
  reconnect_attempt = 0, next_claim_at = NULL, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3 AND lease_owner = $4
RETURNING *;

-- name: HeartbeatConnectorControlTunnel :execrows
UPDATE connector_control_tunnels SET
  last_heartbeat_at = now(), lease_expires_at = now() + interval '90 seconds',
  bytes_relayed = bytes_relayed + $5, throttled_events = throttled_events + $6, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3 AND lease_owner = $4
  AND status = 'established';

-- name: MarkConnectorControlTunnelDropped :execrows
UPDATE connector_control_tunnels SET
  status = $5, last_drop_reason = $6, lease_owner = '', lease_expires_at = NULL,
  reconnect_attempt = LEAST(reconnect_attempt + 1, 30),
  next_claim_at = now() + make_interval(secs => CASE LEAST(reconnect_attempt + 1, 6)
    WHEN 1 THEN 1 WHEN 2 THEN 2 WHEN 3 THEN 4 WHEN 4 THEN 8 WHEN 5 THEN 16 ELSE 30 END),
  updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3 AND lease_owner = $4;

-- name: RecoverExpiredConnectorControlTunnels :execrows
UPDATE connector_control_tunnels SET
  status = 'down', last_drop_reason = 'lease_expired', lease_owner = '',
  lease_expires_at = NULL, reconnect_attempt = LEAST(reconnect_attempt + 1, 30),
  next_claim_at = now() + interval '1 second', updated_at = now()
WHERE status IN ('establishing','established') AND lease_expires_at < now();

-- name: MarkConnectorControlTunnelRemoved :execrows
UPDATE connector_control_tunnels SET
  status = 'removed', last_drop_reason = $3, epoch = epoch + 1,
  lease_owner = '', lease_expires_at = NULL, next_claim_at = NULL, updated_at = now()
WHERE connector_id = $1 AND enterprise_id = $2 AND status <> 'removed';

-- name: MarkConnectorControlTunnelsRemovedByScope :execrows
UPDATE connector_control_tunnels SET
  status = 'removed', last_drop_reason = $3, epoch = epoch + 1,
  lease_owner = '', lease_expires_at = NULL, next_claim_at = NULL, updated_at = now()
WHERE bastion_scope_id = $1 AND enterprise_id = $2 AND status <> 'removed';

-- name: RevokeConnectorControlTunnelLeases :execrows
UPDATE credential_leases AS lease SET status = 'revoked'
WHERE lease.enterprise_id = $2 AND lease.status = 'active'
  AND lease.operation_ref IN (
    SELECT 'connector_control_tunnel:' || tunnel.id::text
    FROM connector_control_tunnels AS tunnel
    WHERE tunnel.connector_id = $1 AND tunnel.enterprise_id = $2
  );

-- name: RevokeConnectorControlTunnelLeasesByScope :execrows
UPDATE credential_leases AS lease SET status = 'revoked'
WHERE lease.enterprise_id = $2 AND lease.status = 'active'
  AND lease.operation_ref IN (
    SELECT 'connector_control_tunnel:' || tunnel.id::text
    FROM connector_control_tunnels AS tunnel
    WHERE tunnel.bastion_scope_id = $1 AND tunnel.enterprise_id = $2
  );

-- name: CountOwnedConnectorControlTunnels :one
SELECT count(*)::bigint FROM connector_control_tunnels
WHERE lease_owner = $1 AND status IN ('establishing','established');

-- name: MarkOverdueConnectorControlTunnelQuota :execrows
WITH candidate AS (
  SELECT id FROM connector_control_tunnels
  WHERE status IN ('desired','down') AND lease_owner = ''
    AND created_at < now() - interval '30 seconds'
    AND (next_claim_at IS NULL OR next_claim_at <= now())
  ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE connector_control_tunnels AS tunnel SET status = 'down', last_drop_reason = 'tunnel_quota_exceeded',
  reconnect_attempt = LEAST(reconnect_attempt + 1, 30), next_claim_at = now() + interval '30 seconds', updated_at = now()
FROM candidate WHERE tunnel.id = candidate.id;
