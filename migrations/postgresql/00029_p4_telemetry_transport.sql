-- +goose Up

-- PlanV4: 遥测路由新增 transport 维度(与 kind 正交)。kind 决定身份与逻辑上游,
-- transport 只决定字节物理路径:direct 直连、executor_tunnel(Direct Executor
-- 发起的 SSH 反向隧道,场景②)、bastion_tunnel(堡垒机 Connector 发起,场景③)。
ALTER TABLE telemetry_routes
    ADD COLUMN IF NOT EXISTS transport text NOT NULL,
    ADD COLUMN IF NOT EXISTS loopback_port integer;

ALTER TABLE telemetry_routes DROP CONSTRAINT IF EXISTS telemetry_routes_transport_check;
ALTER TABLE telemetry_routes ADD CONSTRAINT telemetry_routes_transport_check
    CHECK (transport IN ('direct','executor_tunnel','bastion_tunnel'));

ALTER TABLE telemetry_routes ADD CONSTRAINT telemetry_routes_loopback_check CHECK (
    (transport = 'direct' AND loopback_port IS NULL) OR
    -- 紧邻端口保留给 telemetry identity HTTP 转发。
    (transport IN ('executor_tunnel','bastion_tunnel') AND loopback_port BETWEEN 1 AND 65534)
);
ALTER TABLE telemetry_routes ADD CONSTRAINT telemetry_routes_kind_transport_check CHECK (
    kind <> 'kubernetes_gateway' OR transport = 'direct'
);

-- 隧道权威状态:PostgreSQL 为事实来源,发起侧(Direct Executor / Connector)按
-- desired 行认领建连,断线/重建只更新本表。认领列与 telemetry_collector_operations
-- 的 lease/fence 模式同构,epoch 在每次重建时递增用于 fencing 旧连接。
CREATE TABLE telemetry_tunnels (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    host_id uuid NOT NULL,
    collector_id uuid NOT NULL,
    connector_id uuid,
    credential_id uuid NOT NULL,
    credential_version bigint NOT NULL CHECK (credential_version > 0),
    target_address text NOT NULL CHECK (char_length(target_address) BETWEEN 1 AND 512),
    target_port integer NOT NULL CHECK (target_port BETWEEN 1 AND 65535),
    target_username text NOT NULL CHECK (char_length(target_username) BETWEEN 1 AND 256),
    pinned_host_key text NOT NULL CHECK (char_length(pinned_host_key) BETWEEN 1 AND 512),
    initiator text NOT NULL CHECK (initiator IN ('direct_executor','connector')),
    transport text NOT NULL CHECK (transport IN ('executor_tunnel','bastion_tunnel')),
    -- loopback_port+1 固定承载 Collector enrollment/rotation HTTP。
    loopback_port integer NOT NULL CHECK (loopback_port BETWEEN 1 AND 65534),
    forward_target text NOT NULL CHECK (char_length(forward_target) BETWEEN 1 AND 256),
    status text NOT NULL DEFAULT 'desired'
        CHECK (status IN ('desired','establishing','established','degraded','down','removed')),
    epoch bigint NOT NULL DEFAULT 1 CHECK (epoch > 0),
    lease_owner text NOT NULL DEFAULT '',
    owner_connection_epoch bigint NOT NULL DEFAULT 0 CHECK (owner_connection_epoch >= 0),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_expires_at timestamptz,
    last_claim_at timestamptz,
    last_established_at timestamptz,
    last_heartbeat_at timestamptz,
    last_drop_reason text NOT NULL DEFAULT '',
    reconnect_attempt integer NOT NULL DEFAULT 0 CHECK (reconnect_attempt BETWEEN 0 AND 30),
    next_claim_at timestamptz,
    bytes_relayed bigint NOT NULL DEFAULT 0 CHECK (bytes_relayed >= 0),
    throttled_events bigint NOT NULL DEFAULT 0 CHECK (throttled_events >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (collector_id, enterprise_id) REFERENCES collector_instances(id, enterprise_id),
    FOREIGN KEY (connector_id, enterprise_id) REFERENCES connectors(id, enterprise_id),
    FOREIGN KEY (credential_id, enterprise_id) REFERENCES credentials(id, enterprise_id),
    UNIQUE (collector_id),
    CHECK (initiator <> 'direct_executor' OR transport = 'executor_tunnel'),
    CHECK (initiator <> 'connector' OR (transport = 'bastion_tunnel' AND connector_id IS NOT NULL)),
    CHECK (initiator <> 'direct_executor' OR connector_id IS NULL)
);
-- 隧道认领:最久未认领优先;removed 行不参与。
CREATE INDEX telemetry_tunnels_claim_idx ON telemetry_tunnels (last_claim_at ASC NULLS FIRST)
    WHERE status IN ('desired','establishing','degraded','down');
CREATE INDEX telemetry_tunnels_enterprise_idx ON telemetry_tunnels (enterprise_id, host_id);

-- self_enrolled 主机的安装不经过执行器,记录为 bootstrap 执行。
ALTER TABLE telemetry_collector_operations DROP CONSTRAINT IF EXISTS telemetry_collector_operations_executor_kind_check;
ALTER TABLE telemetry_collector_operations ADD CONSTRAINT telemetry_collector_operations_executor_kind_check
    CHECK (executor_kind IN ('direct','bootstrap'));

-- +goose Down
ALTER TABLE telemetry_collector_operations DROP CONSTRAINT IF EXISTS telemetry_collector_operations_executor_kind_check;
ALTER TABLE telemetry_collector_operations ADD CONSTRAINT telemetry_collector_operations_executor_kind_check
    CHECK (executor_kind IN ('direct'));
DROP INDEX IF EXISTS telemetry_tunnels_enterprise_idx;
DROP INDEX IF EXISTS telemetry_tunnels_claim_idx;
DROP TABLE IF EXISTS telemetry_tunnels;
ALTER TABLE telemetry_routes DROP CONSTRAINT IF EXISTS telemetry_routes_kind_transport_check;
ALTER TABLE telemetry_routes DROP CONSTRAINT IF EXISTS telemetry_routes_loopback_check;
ALTER TABLE telemetry_routes DROP CONSTRAINT IF EXISTS telemetry_routes_transport_check;
ALTER TABLE telemetry_routes DROP COLUMN IF EXISTS loopback_port;
ALTER TABLE telemetry_routes DROP COLUMN IF EXISTS transport;
