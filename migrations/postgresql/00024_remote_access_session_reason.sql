-- +goose Up

-- 会话行直接保存建立时的事由（来自 AccessRequest），会话列表/详情展示使用，
-- 与 decision/profile 快照同属会话自带的审计事实，避免跨表联查。
ALTER TABLE remote_access_sessions
    ADD COLUMN reason text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE remote_access_sessions
    DROP COLUMN reason;
