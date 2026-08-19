-- name: ListCollectorDistributionVersions :many
SELECT * FROM collector_distribution_versions ORDER BY catalog_revision DESC, name, version;

-- name: ListCollectionProfiles :many
SELECT * FROM collection_profiles ORDER BY profile_key, version;

-- name: UpsertCollectorDistributionVersion :one
INSERT INTO collector_distribution_versions (
    id, name, version, collector_version, config_schema_version, support_status, components, artifact_manifest, catalog_revision
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (name, version) DO UPDATE SET
    collector_version = EXCLUDED.collector_version,
    config_schema_version = EXCLUDED.config_schema_version,
    support_status = EXCLUDED.support_status,
    components = EXCLUDED.components,
    artifact_manifest = EXCLUDED.artifact_manifest,
    catalog_revision = EXCLUDED.catalog_revision
RETURNING *;

-- name: UpsertCollectionProfile :one
INSERT INTO collection_profiles (
    id, profile_key, version, name, description, signals, required_components, supported_platforms, claim_types,
    config_schema_version, support_status, catalog_revision
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (profile_key, version) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    signals = EXCLUDED.signals,
    required_components = EXCLUDED.required_components,
    supported_platforms = EXCLUDED.supported_platforms,
    claim_types = EXCLUDED.claim_types,
    config_schema_version = EXCLUDED.config_schema_version,
    support_status = EXCLUDED.support_status,
    catalog_revision = EXCLUDED.catalog_revision
RETURNING *;
