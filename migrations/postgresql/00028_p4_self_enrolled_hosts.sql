-- +goose Up

-- PlanV4 场景⑤「只出不进」主机。项目尚未发布，本迁移直接定义最终结构，
-- 不为早期开发数据库保留兼容列或回填分支。
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_connection_mode_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_connection_mode_check
    CHECK (connection_mode IN ('connector_local','via_bastion','direct_ssh','direct_winrm','self_enrolled'));

ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_address_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_address_check CHECK (char_length(address) BETWEEN 0 AND 512);
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_port_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_port_check CHECK (port BETWEEN 0 AND 65535);
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_check CHECK (
    (connection_mode IN ('connector_local','via_bastion') AND bastion_scope_id IS NOT NULL) OR
    (connection_mode IN ('direct_ssh','direct_winrm') AND bastion_scope_id IS NULL) OR
    (connection_mode = 'self_enrolled' AND bastion_scope_id IS NULL AND connector_id IS NULL)
);
ALTER TABLE hosts ADD CONSTRAINT hosts_mode_fields_check CHECK (
    connection_mode = 'self_enrolled' OR (char_length(address) >= 1 AND port >= 1)
);

-- 安装/重新收敛令牌。一个 Host 可以有多条历史记录，但同一时刻只能有
-- 一个 active 令牌。首次交换结果以 AEAD 密文缓存，使同设备网络重试返回
-- 同一份 Collector enrollment 物料，不会再次签发令牌。
CREATE TABLE host_enrollment_tokens (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    preallocated_host_id uuid NOT NULL,
    collector_id uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    frozen_plan jsonb NOT NULL CHECK (jsonb_typeof(frozen_plan) = 'object'),
    frozen_plan_hash bytea NOT NULL CHECK (octet_length(frozen_plan_hash) = 32),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','consumed','revoked','expired')),
    remaining_uses integer NOT NULL DEFAULT 1 CHECK (remaining_uses IN (0,1)),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    consumed_device_hash bytea CHECK (consumed_device_hash IS NULL OR octet_length(consumed_device_hash) = 32),
    reported_hostname text NOT NULL DEFAULT '' CHECK (char_length(reported_hostname) <= 253),
    reported_address text NOT NULL DEFAULT '' CHECK (char_length(reported_address) <= 512),
    reported_architecture text NOT NULL DEFAULT '' CHECK (reported_architecture IN ('','amd64','arm64')),
    exchange_key_version integer CHECK (exchange_key_version IS NULL OR exchange_key_version > 0),
    exchange_nonce bytea,
    exchange_ciphertext bytea,
    exchange_expires_at timestamptz,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (preallocated_host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (collector_id, enterprise_id) REFERENCES collector_instances(id, enterprise_id),
    CHECK (expires_at > created_at),
    CHECK ((exchange_key_version IS NULL) = (exchange_nonce IS NULL)),
    CHECK ((exchange_key_version IS NULL) = (exchange_ciphertext IS NULL)),
    CHECK ((exchange_key_version IS NULL) = (exchange_expires_at IS NULL))
);
CREATE UNIQUE INDEX host_one_active_enrollment
    ON host_enrollment_tokens (enterprise_id, preallocated_host_id)
    WHERE status = 'active';
CREATE INDEX host_enrollment_tokens_enterprise_idx
    ON host_enrollment_tokens (enterprise_id, preallocated_host_id, created_at DESC);

-- Bind the short-lived Collector enrollment credential to the exact bootstrap
-- exchange that issued it. This prevents a late enrollment from activating a
-- Host with reports from a newer rotate operation.
ALTER TABLE telemetry_enrollment_tokens
    ADD COLUMN host_enrollment_token_id uuid UNIQUE REFERENCES host_enrollment_tokens(id);

-- 卸载是独立危险动作，不复用 enrollment token。交换只把资源置为
-- uninstalling；脚本完成本机清理并提交 completion token 后才收敛为
-- uninstalled。
CREATE TABLE host_uninstall_tokens (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    host_id uuid NOT NULL,
    collector_id uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    completion_token_hash bytea CHECK (completion_token_hash IS NULL OR octet_length(completion_token_hash) = 32),
    frozen_plan jsonb NOT NULL CHECK (jsonb_typeof(frozen_plan) = 'object'),
    frozen_plan_hash bytea NOT NULL CHECK (octet_length(frozen_plan_hash) = 32),
    status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','consumed','completed','revoked','expired')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    completed_at timestamptz,
    consumed_device_hash bytea CHECK (consumed_device_hash IS NULL OR octet_length(consumed_device_hash) = 32),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (collector_id, enterprise_id) REFERENCES collector_instances(id, enterprise_id),
    CHECK (expires_at > created_at)
);
CREATE UNIQUE INDEX host_one_active_uninstall
    ON host_uninstall_tokens (enterprise_id, host_id)
    WHERE status = 'active';
CREATE INDEX host_uninstall_tokens_enterprise_idx
    ON host_uninstall_tokens (enterprise_id, host_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS host_uninstall_tokens_enterprise_idx;
DROP INDEX IF EXISTS host_one_active_uninstall;
DROP TABLE IF EXISTS host_uninstall_tokens;
ALTER TABLE telemetry_enrollment_tokens DROP COLUMN IF EXISTS host_enrollment_token_id;
DROP INDEX IF EXISTS host_enrollment_tokens_enterprise_idx;
DROP INDEX IF EXISTS host_one_active_enrollment;
DROP TABLE IF EXISTS host_enrollment_tokens;
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_mode_fields_check;
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_check CHECK (
    (connection_mode IN ('connector_local','via_bastion') AND bastion_scope_id IS NOT NULL) OR
    (connection_mode IN ('direct_ssh','direct_winrm') AND bastion_scope_id IS NULL)
);
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_port_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_port_check CHECK (port BETWEEN 1 AND 65535);
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_address_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_address_check CHECK (char_length(address) BETWEEN 1 AND 512);
ALTER TABLE hosts DROP CONSTRAINT IF EXISTS hosts_connection_mode_check;
ALTER TABLE hosts ADD CONSTRAINT hosts_connection_mode_check
    CHECK (connection_mode IN ('connector_local','via_bastion','direct_ssh','direct_winrm'));
