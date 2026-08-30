-- name: ClaimHostProbeBatch :many
-- 认领一批待探测主机:仅直连模式(堡垒机路径地址不可直达),最久未检优先;
-- 认领即打时间戳,SKIP LOCKED 保证多副本互不重复。
WITH claimed AS (
    SELECT id FROM hosts
    WHERE status <> 'deleted'
      AND connection_mode IN ('direct_ssh','direct_winrm')
      AND (last_probe_claim_at IS NULL OR last_probe_claim_at < now() - interval '45 seconds')
    ORDER BY last_probe_claim_at ASC NULLS FIRST, id
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE hosts AS host SET last_probe_claim_at = now(), updated_at = updated_at
FROM claimed WHERE host.id = claimed.id
RETURNING host.id, host.enterprise_id, host.address, host.port, host.platform,
          host.pinned_host_key, host.architecture;

-- name: UpsertHostProbeState :one
INSERT INTO host_probe_states (
    host_id, enterprise_id, status, last_checked_at, latency_ms, fingerprint,
    consecutive_failures, error, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
ON CONFLICT (host_id) DO UPDATE SET
    status = EXCLUDED.status,
    last_checked_at = EXCLUDED.last_checked_at,
    latency_ms = EXCLUDED.latency_ms,
    fingerprint = EXCLUDED.fingerprint,
    consecutive_failures = EXCLUDED.consecutive_failures,
    error = EXCLUDED.error,
    updated_at = now()
RETURNING *;

-- name: ListHostProbeStatesByHosts :many
SELECT * FROM host_probe_states WHERE host_id = ANY($1::uuid[]);

-- name: GetHostProbeState :one
SELECT * FROM host_probe_states WHERE host_id = $1;
