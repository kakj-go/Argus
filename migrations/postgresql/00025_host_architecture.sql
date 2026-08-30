-- +goose Up

-- Collector 产物按目标架构分发:连接测试时通过 uname -m 探测并记录,
-- NULL 视为 amd64(存量数据与服务器的多数派架构)。
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS architecture text;

-- +goose Down
ALTER TABLE hosts DROP COLUMN IF EXISTS architecture;
