-- +goose Up

-- Repair the development-era state where a pending Scope could be soft
-- deleted without deleting its connector_local root Host. Deleted rows do not
-- participate in either live-name partial unique index.
UPDATE hosts AS host
SET status = 'deleted',
    connection_status = 'offline',
    deleted_at = COALESCE(host.deleted_at, now()),
    resource_version = host.resource_version + 1,
    updated_at = now()
FROM bastion_scopes AS scope
WHERE host.enterprise_id = scope.enterprise_id
  AND host.bastion_scope_id = scope.id
  AND host.connection_mode = 'connector_local'
  AND host.status <> 'deleted'
  AND scope.status = 'deleted';

-- Backfill the ownership link for Scopes created before the creation
-- transaction started attaching its preallocated root Host immediately.
UPDATE bastion_scopes AS scope
SET connector_host_id = root.id,
    updated_at = now()
FROM hosts AS root
WHERE scope.enterprise_id = root.enterprise_id
  AND scope.id = root.bastion_scope_id
  AND scope.status <> 'deleted'
  AND scope.connector_host_id IS NULL
  AND root.connection_mode = 'connector_local'
  AND root.status <> 'deleted';

-- A live Scope owns one and only one live connector_local root. This index
-- protects the relationship while keeping deleted tombstones for audit.
CREATE UNIQUE INDEX hosts_bastion_root_live_unique
    ON hosts (enterprise_id, bastion_scope_id)
    WHERE connection_mode = 'connector_local'
      AND bastion_scope_id IS NOT NULL
      AND status <> 'deleted';

-- +goose Down
DROP INDEX IF EXISTS hosts_bastion_root_live_unique;
