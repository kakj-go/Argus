-- +goose Up

-- Task 02 is a direct cutover. Development runtime rows may contain legacy
-- policy snapshots, so clear them before removing the compatibility schema.
TRUNCATE TABLE remote_access_requests CASCADE;

ALTER TABLE remote_access_requirement_snapshots
    DROP CONSTRAINT IF EXISTS remote_access_requirement_source_ck,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_workflow_source_ck,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_snapshots_policy_id_fkey,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_snapshots_request_id_policy_id_key,
    DROP COLUMN IF EXISTS policy_id,
    DROP COLUMN IF EXISTS policy_version;
ALTER TABLE remote_access_requirement_snapshots
    ADD CONSTRAINT remote_access_requirement_workflow_source_ck
        CHECK (workflow_id IS NOT NULL AND workflow_version IS NOT NULL);

ALTER TABLE remote_access_leases
    DROP COLUMN IF EXISTS policy_snapshot_hash;

DROP TABLE IF EXISTS remote_access_policies;

DELETE FROM role_permissions
WHERE permission_id IN ('remote_access.policy.read', 'remote_access.policy.manage');
DELETE FROM permissions
WHERE id IN ('remote_access.policy.read', 'remote_access.policy.manage');

-- +goose Down
-- Intentionally irreversible: RemoteAccessPolicy and development runtime data
-- are removed by the PlanV3 Task 02 direct cutover.
