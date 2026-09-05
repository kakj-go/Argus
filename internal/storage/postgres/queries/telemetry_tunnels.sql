-- PlanV4 反向隧道权威状态。认领/租约/fence 模式与 telemetry_collector_operations
-- 同构:SKIP LOCKED 批量认领,fence 随认领递增,epoch 在每次重建连接时递增。

-- name: CreateTelemetryTunnel :one
INSERT INTO telemetry_tunnels (
  id, enterprise_id, host_id, collector_id, connector_id, credential_id,
  credential_version, target_address, target_port, target_username,
  pinned_host_key, initiator, transport, loopback_port, forward_target, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'desired')
ON CONFLICT (collector_id) DO UPDATE SET
  connector_id = EXCLUDED.connector_id,
  credential_id = EXCLUDED.credential_id,
  credential_version = EXCLUDED.credential_version,
  target_address = EXCLUDED.target_address,
  target_port = EXCLUDED.target_port,
  target_username = EXCLUDED.target_username,
  pinned_host_key = EXCLUDED.pinned_host_key,
  initiator = EXCLUDED.initiator,
  transport = EXCLUDED.transport,
  loopback_port = EXCLUDED.loopback_port,
  forward_target = EXCLUDED.forward_target,
  status = 'desired',
  epoch = telemetry_tunnels.epoch + 1,
  lease_owner = '',
  owner_connection_epoch = 0,
  lease_expires_at = NULL,
  last_drop_reason = '',
  reconnect_attempt = 0,
  next_claim_at = NULL,
  updated_at = now()
RETURNING *;

-- name: GetTelemetryTunnel :one
SELECT * FROM telemetry_tunnels WHERE id = $1 AND enterprise_id = $2;

-- name: GetTelemetryTunnelByCollector :one
SELECT * FROM telemetry_tunnels WHERE collector_id = $1;

-- name: ClaimTelemetryTunnelBatch :many
-- Direct Executor 只认领由它发起的隧道；Connector 隧道由活跃控制会话认领。
WITH claimed AS (
  SELECT id FROM telemetry_tunnels
  WHERE initiator = 'direct_executor' AND status IN ('desired','establishing','degraded','down')
    AND (lease_expires_at IS NULL OR lease_expires_at < now())
    AND (next_claim_at IS NULL OR next_claim_at <= now())
  ORDER BY last_claim_at ASC NULLS FIRST, id
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
UPDATE telemetry_tunnels AS tunnel SET
  status = 'establishing', lease_owner = $2, fence = fence + 1,
  epoch = epoch + 1, owner_connection_epoch = 0,
  lease_expires_at = now() + interval '90 seconds', last_claim_at = now(), updated_at = now()
FROM claimed WHERE tunnel.id = claimed.id
RETURNING tunnel.*;

-- name: HeartbeatTelemetryTunnel :execrows
UPDATE telemetry_tunnels SET
  last_heartbeat_at = now(),
  lease_expires_at = now() + interval '90 seconds',
  bytes_relayed = bytes_relayed + sqlc.arg('bytes_relayed'),
  throttled_events = throttled_events + sqlc.arg('throttled_events'),
  updated_at = now()
WHERE id = sqlc.arg('id') AND fence = sqlc.arg('fence') AND lease_owner = sqlc.arg('lease_owner')
  AND status IN ('established','establishing');

-- name: MarkTelemetryTunnelEstablished :one
UPDATE telemetry_tunnels SET
  status = 'established', last_established_at = now(), last_heartbeat_at = now(),
  lease_expires_at = now() + interval '90 seconds', last_drop_reason = '',
  reconnect_attempt = 0, next_claim_at = NULL, updated_at = now()
WHERE id = $1 AND fence = $2 AND lease_owner = $3
RETURNING *;

-- name: MarkTelemetryTunnelDropped :execrows
-- fence 守卫:只有当前持租约的副本能写断开状态,防止旧副本覆盖新副本。
UPDATE telemetry_tunnels SET
  status = $4, last_drop_reason = $5, epoch = epoch + 1,
  reconnect_attempt = LEAST(reconnect_attempt + 1, 30),
  next_claim_at = now() + make_interval(secs => CASE LEAST(reconnect_attempt + 1, 6)
    WHEN 1 THEN 1 WHEN 2 THEN 2 WHEN 3 THEN 4 WHEN 4 THEN 8 WHEN 5 THEN 16 ELSE 30 END),
  lease_owner = '', owner_connection_epoch = 0, lease_expires_at = NULL, updated_at = now()
WHERE id = $1 AND fence = $2 AND lease_owner = $3;

-- name: MarkTelemetryTunnelRemoved :execrows
UPDATE telemetry_tunnels SET
  status = 'removed', last_drop_reason = $4, epoch = epoch + 1,
  lease_owner = '', owner_connection_epoch = 0, lease_expires_at = NULL, next_claim_at = NULL, updated_at = now()
WHERE collector_id = $1 AND enterprise_id = $2 AND status <> 'removed'
  AND ($3::text = '' OR lease_owner = $3 OR lease_owner = '');

-- name: RecoverExpiredTelemetryTunnels :execrows
-- 租约过期的 establishing/established 行回退为 down,等待重新认领。
UPDATE telemetry_tunnels SET
  status = 'down', last_drop_reason = 'lease_expired',
  lease_owner = '', owner_connection_epoch = 0, lease_expires_at = NULL, epoch = epoch + 1,
  reconnect_attempt = LEAST(reconnect_attempt + 1, 30), next_claim_at = now() + interval '1 second', updated_at = now()
WHERE status IN ('establishing','established') AND lease_expires_at < now();

-- name: ClaimConnectorTelemetryTunnels :many
WITH claimed AS (
  SELECT candidate.id FROM telemetry_tunnels AS candidate
  JOIN credentials AS credential ON credential.id = candidate.credential_id
    AND credential.enterprise_id = candidate.enterprise_id
  WHERE candidate.initiator = 'connector'
    AND candidate.connector_id = sqlc.arg('connector_id')
    AND candidate.enterprise_id = sqlc.arg('enterprise_id')
    AND candidate.status <> 'removed'
    AND credential.status = 'active' AND credential.version = candidate.credential_version
    AND (candidate.status IN ('desired','degraded','down')
      OR candidate.owner_connection_epoch < sqlc.arg('connection_epoch'))
    AND (candidate.next_claim_at IS NULL OR candidate.next_claim_at <= now()
      OR candidate.owner_connection_epoch < sqlc.arg('connection_epoch'))
  ORDER BY candidate.last_claim_at ASC NULLS FIRST, candidate.id
  LIMIT sqlc.arg('limit') FOR UPDATE SKIP LOCKED
)
UPDATE telemetry_tunnels AS tunnel SET
  status = 'establishing', lease_owner = sqlc.arg('lease_owner'),
  owner_connection_epoch = sqlc.arg('connection_epoch'),
  fence = fence + 1, epoch = epoch + 1,
  lease_expires_at = now() + interval '90 seconds', last_claim_at = now(), updated_at = now()
FROM claimed WHERE tunnel.id = claimed.id RETURNING tunnel.*;

-- name: ListOwnedConnectorTelemetryTunnels :many
SELECT tunnel.* FROM telemetry_tunnels AS tunnel
JOIN credentials AS credential ON credential.id = tunnel.credential_id AND credential.enterprise_id = tunnel.enterprise_id
WHERE tunnel.connector_id = $1 AND tunnel.enterprise_id = $2 AND tunnel.lease_owner = $3
  AND tunnel.owner_connection_epoch = $4 AND tunnel.status IN ('establishing','established')
  AND tunnel.lease_expires_at > now() AND credential.status = 'active'
  AND credential.version = tunnel.credential_version
ORDER BY tunnel.id;

-- name: HeartbeatConnectorTelemetryTunnel :execrows
UPDATE telemetry_tunnels SET
  status = sqlc.arg('status'), last_heartbeat_at = now(), lease_expires_at = now() + interval '90 seconds',
  bytes_relayed = bytes_relayed + sqlc.arg('bytes_relayed'),
  throttled_events = throttled_events + sqlc.arg('throttled_events'),
  last_drop_reason = sqlc.arg('last_drop_reason'),
  reconnect_attempt = CASE WHEN sqlc.arg('status') = 'established' THEN 0 ELSE reconnect_attempt END,
  next_claim_at = CASE WHEN sqlc.arg('status') = 'established' THEN NULL ELSE next_claim_at END,
  last_established_at = CASE WHEN sqlc.arg('status') = 'established' THEN COALESCE(last_established_at, now()) ELSE last_established_at END,
  updated_at = now()
WHERE id = sqlc.arg('id') AND enterprise_id = sqlc.arg('enterprise_id')
  AND connector_id = sqlc.arg('connector_id') AND epoch = sqlc.arg('epoch') AND fence = sqlc.arg('fence')
  AND lease_owner = sqlc.arg('lease_owner') AND owner_connection_epoch > 0
  AND status IN ('establishing','established');

-- name: DropOwnedConnectorTelemetryTunnels :execrows
UPDATE telemetry_tunnels SET status = 'down', last_drop_reason = $4, epoch = epoch + 1,
  lease_owner = '', owner_connection_epoch = 0, lease_expires_at = NULL,
  reconnect_attempt = LEAST(reconnect_attempt + 1, 30), next_claim_at = now() + interval '1 second', updated_at = now()
WHERE connector_id = $1 AND enterprise_id = $2 AND lease_owner = $3
  AND status IN ('establishing','established');

-- name: FenceInvalidConnectorTelemetryTunnels :execrows
UPDATE telemetry_tunnels AS tunnel SET
  status = 'down', last_drop_reason = 'credential_revoked', epoch = epoch + 1,
  lease_owner = '', owner_connection_epoch = 0, lease_expires_at = NULL,
  next_claim_at = NULL, updated_at = now()
WHERE tunnel.connector_id = $1 AND tunnel.enterprise_id = $2 AND tunnel.status <> 'removed'
  AND NOT EXISTS (
    SELECT 1 FROM credentials AS credential
    WHERE credential.id = tunnel.credential_id AND credential.enterprise_id = tunnel.enterprise_id
      AND credential.status = 'active' AND credential.version = tunnel.credential_version
  );

-- name: ListTelemetryTunnelsByHosts :many
SELECT * FROM telemetry_tunnels WHERE host_id = ANY($1::uuid[]);

-- name: CountActiveTunnelsByEnterprise :one
SELECT count(*)::bigint FROM telemetry_tunnels
WHERE enterprise_id = $1 AND status <> 'removed';

-- name: MarkOverdueTelemetryTunnelQuota :execrows
WITH candidate AS (
  SELECT id FROM telemetry_tunnels
  WHERE initiator = 'direct_executor' AND status IN ('desired','down')
    AND lease_owner = '' AND created_at < now() - interval '30 seconds'
    AND (next_claim_at IS NULL OR next_claim_at <= now())
  ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE telemetry_tunnels AS tunnel SET status = 'down', last_drop_reason = 'tunnel_quota_exceeded',
  reconnect_attempt = LEAST(reconnect_attempt + 1, 30), next_claim_at = now() + interval '30 seconds', updated_at = now()
FROM candidate WHERE tunnel.id = candidate.id;
