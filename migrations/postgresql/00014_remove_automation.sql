-- +goose Up

DROP TABLE IF EXISTS automation_runs;
DROP TABLE IF EXISTS automation_revisions;
DROP TABLE IF EXISTS automations;

ALTER TABLE runtime_tasks DROP CONSTRAINT IF EXISTS runtime_tasks_queue_check;
ALTER TABLE runtime_tasks ADD CONSTRAINT runtime_tasks_queue_check
  CHECK (queue IN ('agent','action','compaction','sandbox'));

DELETE FROM role_permissions WHERE permission_id IN ('automation.read', 'automation.manage');
DELETE FROM permissions WHERE id IN ('automation.read', 'automation.manage');

-- +goose Down
-- Intentionally irreversible: the Automation domain is removed from the product.
