-- +goose Up

-- 创建方式是 onboarding 事实，稳态 Host 仍保持 connector_local。
ALTER TABLE bastion_scopes ADD COLUMN onboarding_mode text NOT NULL
    CHECK (onboarding_mode IN ('command','direct_install','direct_install_tunnel'));

-- instance_id 标识 Connector 所在机器，会在 replacement 后保持稳定。只允许
-- 同一企业中存在一个未退役的实例；被 replacement fencing 或完成卸载的历史
-- Connector 继续保留审计记录，但不得阻塞新 Connector enrollment。
ALTER TABLE connectors DROP CONSTRAINT connectors_enterprise_id_instance_id_key;
CREATE UNIQUE INDEX connectors_enterprise_instance_live_unique
    ON connectors (enterprise_id, instance_id)
    WHERE status NOT IN ('revoked','uninstalled');

-- Connector 发行目录。manifest 包含 linux_amd64/linux_arm64 的 URI、SHA256、
-- Ed25519 签名、key id 与字节数；Pending Action 只引用不可变版本。
CREATE TABLE connector_release_versions (
    id uuid PRIMARY KEY,
    version text NOT NULL UNIQUE CHECK (char_length(version) BETWEEN 1 AND 128),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
    manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    manifest_hash bytea NOT NULL CHECK (octet_length(manifest_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- connection_tests.id is globally unique, but tenant-scoped foreign keys use
-- the composite identity consistently with the rest of the resource model.
-- PostgreSQL requires an explicit matching unique key for that relationship.
ALTER TABLE connection_tests
    ADD CONSTRAINT connection_tests_id_enterprise_key UNIQUE (id, enterprise_id);

-- B/C 平台代安装是短期 operation。stage 是可恢复的当前阶段，事件表保存
-- 对用户公开的时间线；敏感 enrollment token 存放在独立 AEAD envelope。
CREATE TABLE connector_install_operations (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    connector_id uuid NOT NULL,
    bastion_scope_id uuid NOT NULL,
    host_id uuid NOT NULL,
    pending_action_id uuid NOT NULL UNIQUE REFERENCES pending_actions(id),
    retry_of uuid REFERENCES connector_install_operations(id),
    release_version_id uuid NOT NULL REFERENCES connector_release_versions(id),
    connection_test_id uuid NOT NULL,
    install_mode text NOT NULL CHECK (install_mode IN ('direct_install','direct_install_tunnel')),
    status text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued','running','succeeded','failed','result_unknown','expired')),
    stage text NOT NULL DEFAULT 'queued' CHECK (stage IN (
        'queued','ssh_connecting','artifact_verifying','artifact_transferring',
        'service_installing','control_tunnel_establishing','enrolling',
        'waiting_connector_online','completed'
    )),
    plan jsonb NOT NULL CHECK (jsonb_typeof(plan) = 'object'),
    plan_hash bytea NOT NULL CHECK (octet_length(plan_hash) = 32),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    lease_owner text NOT NULL DEFAULT '',
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_expires_at timestamptz,
    error_code text,
    connector_online_at timestamptz,
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- connector_id is preallocated by the enrollment token; the Connector row
    -- does not exist until enrollment succeeds.
    FOREIGN KEY (bastion_scope_id, enterprise_id) REFERENCES bastion_scopes(id, enterprise_id),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (connection_test_id, enterprise_id) REFERENCES connection_tests(id, enterprise_id)
);
CREATE INDEX connector_install_operations_claim_idx
    ON connector_install_operations (created_at, id) WHERE status = 'queued';
CREATE INDEX connector_install_operations_scope_idx
    ON connector_install_operations (enterprise_id, bastion_scope_id, created_at DESC);

CREATE TABLE connector_install_operation_events (
    id uuid PRIMARY KEY,
    operation_id uuid NOT NULL REFERENCES connector_install_operations(id) ON DELETE CASCADE,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    stage text NOT NULL CHECK (stage IN (
        'queued','ssh_connecting','artifact_verifying','artifact_transferring',
        'service_installing','control_tunnel_establishing','enrolling',
        'waiting_connector_online','completed'
    )),
    status text NOT NULL CHECK (status IN ('started','succeeded','failed','retrying')),
    error_code text,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (operation_id, sequence)
);

CREATE TABLE connector_install_operation_secrets (
    operation_id uuid PRIMARY KEY REFERENCES connector_install_operations(id) ON DELETE CASCADE,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    key_version integer NOT NULL CHECK (key_version > 0),
    nonce bytea NOT NULL,
    ciphertext bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 模式 C 的长期控制隧道。它与安装 operation 生命周期分离；PostgreSQL 是
-- desired 和当前所有权的唯一事实来源。
CREATE TABLE connector_control_tunnels (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    connector_id uuid NOT NULL,
    bastion_scope_id uuid NOT NULL,
    host_id uuid NOT NULL,
    credential_id uuid NOT NULL,
    credential_version bigint NOT NULL CHECK (credential_version > 0),
    target_address text NOT NULL CHECK (char_length(target_address) BETWEEN 1 AND 512),
    target_port integer NOT NULL CHECK (target_port BETWEEN 1 AND 65535),
    target_username text NOT NULL CHECK (char_length(target_username) BETWEEN 1 AND 256),
    pinned_host_key text NOT NULL CHECK (char_length(pinned_host_key) BETWEEN 1 AND 512),
    enroll_forward_target text NOT NULL CHECK (char_length(enroll_forward_target) BETWEEN 1 AND 256),
    gateway_forward_target text NOT NULL CHECK (char_length(gateway_forward_target) BETWEEN 1 AND 256),
    status text NOT NULL DEFAULT 'desired'
        CHECK (status IN ('desired','establishing','established','degraded','down','removed')),
    epoch bigint NOT NULL DEFAULT 0 CHECK (epoch >= 0),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_owner text NOT NULL DEFAULT '',
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
    -- connector_id remains a preallocated identity while the desired tunnel is
    -- established before Connector enrollment.
    FOREIGN KEY (bastion_scope_id, enterprise_id) REFERENCES bastion_scopes(id, enterprise_id),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (credential_id, enterprise_id) REFERENCES credentials(id, enterprise_id),
    UNIQUE (connector_id)
);
CREATE INDEX connector_control_tunnels_claim_idx
    ON connector_control_tunnels (last_claim_at ASC NULLS FIRST, id)
    WHERE status IN ('desired','establishing','degraded','down');
CREATE INDEX connector_control_tunnels_scope_idx
    ON connector_control_tunnels (enterprise_id, bastion_scope_id);

ALTER TABLE executions ADD COLUMN connector_install_operation_id uuid
    REFERENCES connector_install_operations(id);

-- The lifecycle reconciler scans every tenant. The older recipient lookup
-- index starts with enterprise_id and cannot serve this global expiry scan.
CREATE INDEX credential_leases_expiry_idx ON credential_leases (expires_at)
    WHERE status = 'active';

-- +goose Down
DROP INDEX IF EXISTS credential_leases_expiry_idx;
ALTER TABLE executions DROP COLUMN IF EXISTS connector_install_operation_id;
DROP INDEX IF EXISTS connector_control_tunnels_scope_idx;
DROP INDEX IF EXISTS connector_control_tunnels_claim_idx;
DROP TABLE IF EXISTS connector_control_tunnels;
DROP TABLE IF EXISTS connector_install_operation_secrets;
DROP TABLE IF EXISTS connector_install_operation_events;
DROP INDEX IF EXISTS connector_install_operations_scope_idx;
DROP INDEX IF EXISTS connector_install_operations_claim_idx;
DROP TABLE IF EXISTS connector_install_operations;
DROP TABLE IF EXISTS connector_release_versions;
ALTER TABLE connection_tests DROP CONSTRAINT IF EXISTS connection_tests_id_enterprise_key;
DROP INDEX IF EXISTS connectors_enterprise_instance_live_unique;
ALTER TABLE connectors ADD CONSTRAINT connectors_enterprise_id_instance_id_key
    UNIQUE (enterprise_id, instance_id);
ALTER TABLE bastion_scopes DROP COLUMN IF EXISTS onboarding_mode;
