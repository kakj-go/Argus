-- +goose Up

-- 主机实时探活状态:与 hosts.connection_status(接入时人工验证的静态语义)分离,
-- 由 worker 周期探测(TCP + SSH 握手,不使用凭据)仅在状态变迁时写入。
CREATE TABLE IF NOT EXISTS host_probe_states (
    host_id uuid PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    enterprise_id uuid NOT NULL,
    -- online | offline | key_changed(SSH 主机键与 pinned 值不一致)
    status text NOT NULL CHECK (status IN ('online','offline','key_changed')),
    last_checked_at timestamptz NOT NULL DEFAULT now(),
    latency_ms integer NOT NULL DEFAULT 0,
    fingerprint text NOT NULL DEFAULT '',
    consecutive_failures integer NOT NULL DEFAULT 0,
    error text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS host_probe_states_enterprise_idx ON host_probe_states (enterprise_id);
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS last_probe_claim_at timestamptz;
-- 探活认领:按"最久未检"优先,保证大批量时平滑降级而不是每轮扫一半。
CREATE INDEX IF NOT EXISTS host_probe_claim_idx
    ON hosts (last_probe_claim_at ASC NULLS FIRST)
    WHERE status <> 'deleted' AND connection_mode IN ('direct_ssh','direct_winrm');

-- +goose Down
DROP INDEX IF EXISTS host_probe_claim_idx;
ALTER TABLE hosts DROP COLUMN IF EXISTS last_probe_claim_at;
DROP INDEX IF EXISTS host_probe_states_enterprise_idx;
DROP TABLE IF EXISTS host_probe_states;
