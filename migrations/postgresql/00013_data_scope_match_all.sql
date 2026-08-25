-- +goose Up
ALTER TABLE data_scopes
    ADD COLUMN match_all boolean NOT NULL DEFAULT false;

-- The bootstrap scope created before match_all represented the initial
-- enterprise administrator's resource range. Make that intent explicit.
UPDATE data_scopes
SET match_all = true,
    name = 'Default resource scope',
    description = 'Matches all host and Kubernetes resources'
WHERE name = 'Default empty scope'
  AND resource_types @> ARRAY['host', 'kubernetes_cluster']::text[]
  AND cardinality(explicit_resource_ids) = 0
  AND label_selector IS NULL;

-- +goose Down
UPDATE data_scopes
SET name = 'Default empty scope',
    description = 'Matches no resources'
WHERE match_all = true
  AND name = 'Default resource scope';
ALTER TABLE data_scopes DROP COLUMN match_all;
