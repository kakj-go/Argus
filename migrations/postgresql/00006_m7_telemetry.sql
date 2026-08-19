-- +goose Up

INSERT INTO permissions (id, description, registry_version) VALUES
    ('telemetry.collector.read', 'Read Collector catalog and status', 6),
    ('telemetry.collector.manage', 'Manage Collector lifecycle and routes', 6),
    ('telemetry.query.metrics', 'Query authorized metrics', 6),
    ('telemetry.query.logs', 'Query authorized logs', 6),
    ('telemetry.query.traces', 'Query authorized traces', 6),
    ('telemetry.sensitive_fields.read', 'Read governed sensitive telemetry fields', 6),
    ('telemetry.usage.read', 'Read telemetry usage and retention', 6)
ON CONFLICT (id) DO UPDATE SET description = EXCLUDED.description, registry_version = EXCLUDED.registry_version;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id FROM roles CROSS JOIN permissions
WHERE roles.identity_key = 'enterprise_admin' AND permissions.registry_version = 6
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id FROM roles CROSS JOIN permissions
WHERE roles.identity_key IN ('resource_admin','resource_operator')
  AND permissions.id IN ('telemetry.collector.read','telemetry.query.metrics','telemetry.query.logs','telemetry.query.traces','telemetry.usage.read')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id FROM roles CROSS JOIN permissions
WHERE roles.identity_key IN ('resource_viewer','resource_approver')
  AND permissions.id IN ('telemetry.collector.read','telemetry.query.metrics','telemetry.query.logs','telemetry.query.traces')
ON CONFLICT DO NOTHING;

CREATE TABLE collector_distribution_versions (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    version text NOT NULL CHECK (char_length(version) BETWEEN 1 AND 64),
    collector_version text NOT NULL CHECK (char_length(collector_version) BETWEEN 1 AND 64),
    config_schema_version text NOT NULL CHECK (char_length(config_schema_version) BETWEEN 1 AND 64),
    support_status text NOT NULL CHECK (support_status IN ('supported','validation_pending','retired')),
    components text[] NOT NULL DEFAULT '{}',
    artifact_manifest jsonb NOT NULL CHECK (jsonb_typeof(artifact_manifest) = 'array'),
    catalog_revision integer NOT NULL CHECK (catalog_revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);

CREATE TABLE collection_profiles (
    id uuid PRIMARY KEY,
    profile_key text NOT NULL CHECK (profile_key ~ '^[a-z][a-z0-9-]{0,62}$'),
    version text NOT NULL CHECK (char_length(version) BETWEEN 1 AND 64),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1024),
    signals text[] NOT NULL CHECK (cardinality(signals) > 0 AND signals <@ ARRAY['metrics','logs','traces']::text[]),
    required_components text[] NOT NULL DEFAULT '{}',
    supported_platforms text[] NOT NULL CHECK (cardinality(supported_platforms) > 0 AND supported_platforms <@ ARRAY['linux_arm64','windows_amd64']::text[]),
    claim_types text[] NOT NULL DEFAULT '{}',
    config_schema_version text NOT NULL CHECK (char_length(config_schema_version) BETWEEN 1 AND 64),
    support_status text NOT NULL CHECK (support_status IN ('supported','validation_pending','retired')),
    catalog_revision integer NOT NULL CHECK (catalog_revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (profile_key, version)
);

CREATE TABLE collector_instances (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    resource_type text NOT NULL CHECK (resource_type IN ('host','kubernetes_cluster')),
    resource_id uuid NOT NULL,
    distribution_version_id uuid NOT NULL REFERENCES collector_distribution_versions(id),
    platform text NOT NULL CHECK (platform IN ('linux_arm64','windows_amd64')),
    role text NOT NULL CHECK (role IN ('direct','leaf','edge_gateway','daemonset','kubernetes_gateway')),
    status text NOT NULL DEFAULT 'pending_install' CHECK (status IN ('pending_install','installing','converged','degraded','backlog','result_unknown','uninstalling','uninstalled')),
    desired_revision bigint NOT NULL DEFAULT 0 CHECK (desired_revision >= 0),
    effective_revision bigint NOT NULL DEFAULT 0 CHECK (effective_revision >= 0),
    authorization_version bigint NOT NULL DEFAULT 1 CHECK (authorization_version > 0),
    last_seen_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id),
    UNIQUE (enterprise_id, resource_type, resource_id)
);
CREATE INDEX collector_instances_inventory ON collector_instances (enterprise_id, resource_type, status, updated_at DESC);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_collector_resource() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.resource_type = 'host' AND NOT EXISTS (
        SELECT 1 FROM hosts WHERE id = NEW.resource_id AND enterprise_id = NEW.enterprise_id
    ) THEN
        RAISE EXCEPTION 'collector Host must belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.resource_type = 'kubernetes_cluster' AND NOT EXISTS (
        SELECT 1 FROM kubernetes_clusters WHERE id = NEW.resource_id AND enterprise_id = NEW.enterprise_id
    ) THEN
        RAISE EXCEPTION 'collector Kubernetes cluster must belong to enterprise' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER collector_resource_enterprise
AFTER INSERT OR UPDATE ON collector_instances DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION validate_collector_resource();

CREATE TABLE collector_config_revisions (
    id uuid PRIMARY KEY,
    collector_id uuid NOT NULL REFERENCES collector_instances(id),
    revision bigint NOT NULL CHECK (revision > 0),
    profile_ids uuid[] NOT NULL CHECK (cardinality(profile_ids) > 0),
    rendered_config jsonb NOT NULL CHECK (jsonb_typeof(rendered_config) = 'object'),
    config_hash bytea NOT NULL CHECK (octet_length(config_hash) = 32),
    status text NOT NULL DEFAULT 'prepared' CHECK (status IN ('prepared','applying','effective','failed','rolled_back','superseded')),
    failure_code text,
    rollback_revision bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    applied_at timestamptz,
    UNIQUE (collector_id, revision)
);

CREATE TABLE telemetry_routes (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    collector_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('direct_argus','bastion_gateway','kubernetes_gateway')),
    gateway_collector_id uuid,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','testing','active','degraded','invalidated')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    last_tested_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (collector_id, enterprise_id) REFERENCES collector_instances(id, enterprise_id),
    FOREIGN KEY (gateway_collector_id, enterprise_id) REFERENCES collector_instances(id, enterprise_id),
    UNIQUE (collector_id)
);

CREATE TABLE telemetry_collector_operations (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    collector_id uuid NOT NULL,
    pending_action_id uuid NOT NULL UNIQUE REFERENCES pending_actions(id),
    operation text NOT NULL CHECK (operation IN ('install','configure','upgrade','repair','uninstall')),
    executor_kind text NOT NULL CHECK (executor_kind IN ('direct')),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed','result_unknown','expired')),
    plan jsonb NOT NULL CHECK (jsonb_typeof(plan) = 'object'),
    plan_hash bytea NOT NULL CHECK (octet_length(plan_hash) = 32),
    lease_owner text,
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_expires_at timestamptz,
    result_hash bytea,
    error_code text,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    FOREIGN KEY (collector_id, enterprise_id) REFERENCES collector_instances(id, enterprise_id),
    CHECK (result_hash IS NULL OR octet_length(result_hash) = 32)
);
CREATE INDEX telemetry_collector_operations_queue ON telemetry_collector_operations (status, created_at)
WHERE status IN ('queued','running','result_unknown');

ALTER TABLE executions ADD COLUMN telemetry_collector_operation_id uuid REFERENCES telemetry_collector_operations(id);
ALTER TABLE executions ADD CONSTRAINT execution_external_operation CHECK (
    connector_command_id IS NULL OR telemetry_collector_operation_id IS NULL
);

CREATE TABLE telemetry_route_tests (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    route_id uuid NOT NULL REFERENCES telemetry_routes(id),
    status text NOT NULL CHECK (status IN ('queued','running','succeeded','failed','expired')),
    result_code text,
    result_hash bytea,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CHECK (result_hash IS NULL OR octet_length(result_hash) = 32)
);

CREATE TABLE collection_claims (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    physical_resource_ref text NOT NULL CHECK (char_length(physical_resource_ref) BETWEEN 1 AND 256),
    collector_id uuid NOT NULL,
    profile_id uuid REFERENCES collection_profiles(id),
    claim_type text NOT NULL CHECK (char_length(claim_type) BETWEEN 1 AND 128),
    signal text NOT NULL CHECK (signal IN ('metrics','logs','traces')),
    selector jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(selector) = 'object'),
    selector_hash bytea NOT NULL CHECK (octet_length(selector_hash) = 32),
    ownership text NOT NULL CHECK (ownership IN ('primary','supplemental','migration')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','released','conflict','expired')),
    primary_claim_id uuid,
    rollback_plan jsonb,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (collector_id, enterprise_id) REFERENCES collector_instances(id, enterprise_id),
	UNIQUE (id, enterprise_id),
	FOREIGN KEY (primary_claim_id, enterprise_id) REFERENCES collection_claims(id, enterprise_id),
    CHECK (ownership <> 'migration' OR (primary_claim_id IS NOT NULL AND expires_at IS NOT NULL AND rollback_plan IS NOT NULL))
);
CREATE UNIQUE INDEX collection_claims_active_primary ON collection_claims (enterprise_id, physical_resource_ref, claim_type, selector_hash)
WHERE ownership = 'primary' AND status = 'active';
CREATE INDEX collection_claims_resource ON collection_claims (enterprise_id, physical_resource_ref, status);
CREATE UNIQUE INDEX collection_claims_active_migration_per_collector ON collection_claims (
	enterprise_id, physical_resource_ref, claim_type, selector_hash, collector_id
) WHERE ownership = 'migration' AND status = 'active';

CREATE TABLE kubernetes_node_host_bindings (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    kubernetes_cluster_id uuid NOT NULL,
    node_uid text NOT NULL CHECK (char_length(node_uid) BETWEEN 1 AND 256),
    node_name text NOT NULL CHECK (char_length(node_name) BETWEEN 1 AND 253),
    host_id uuid,
    matched_by text NOT NULL CHECK (matched_by IN ('system_uuid','provider_id','machine_id','collector_host_id','ip','manual')),
    evidence jsonb NOT NULL CHECK (jsonb_typeof(evidence) = 'object'),
    evidence_hash bytea NOT NULL CHECK (octet_length(evidence_hash) = 32),
    confidence integer NOT NULL CHECK (confidence BETWEEN 0 AND 100),
    status text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','verified','rejected','stale')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (kubernetes_cluster_id, enterprise_id) REFERENCES kubernetes_clusters(id, enterprise_id),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    UNIQUE (enterprise_id, kubernetes_cluster_id, node_uid)
);

CREATE TABLE telemetry_enrollment_tokens (
    id uuid PRIMARY KEY,
    collector_id uuid NOT NULL REFERENCES collector_instances(id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    CHECK (expires_at <= created_at + interval '10 minutes')
);

CREATE TABLE telemetry_certificates (
    id uuid PRIMARY KEY,
    collector_id uuid NOT NULL REFERENCES collector_instances(id),
    serial_number text NOT NULL UNIQUE,
    uri_san text NOT NULL,
    csr_hash bytea NOT NULL CHECK (octet_length(csr_hash) = 32),
    certificate_hash bytea NOT NULL CHECK (octet_length(certificate_hash) = 32),
    certificate_request_name text NOT NULL,
    issuer_generation integer NOT NULL CHECK (issuer_generation > 0),
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (not_after <= not_before + interval '24 hours')
);
CREATE INDEX telemetry_certificates_active ON telemetry_certificates (collector_id, not_after) WHERE revoked_at IS NULL;

CREATE TABLE telemetry_retention_policies (
    enterprise_id uuid PRIMARY KEY REFERENCES enterprises(id),
    metrics_days integer NOT NULL DEFAULT 30 CHECK (metrics_days BETWEEN 1 AND 3650),
    logs_days integer NOT NULL DEFAULT 14 CHECK (logs_days BETWEEN 1 AND 3650),
    traces_days integer NOT NULL DEFAULT 7 CHECK (traces_days BETWEEN 1 AND 3650),
    max_rows integer NOT NULL DEFAULT 50000 CHECK (max_rows BETWEEN 1 AND 100000),
    max_scan_bytes bigint NOT NULL DEFAULT 268435456 CHECK (max_scan_bytes BETWEEN 1048576 AND 1073741824),
    max_execution_ms integer NOT NULL DEFAULT 10000 CHECK (max_execution_ms BETWEEN 100 AND 30000),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE telemetry_usage_daily (
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    usage_date date NOT NULL,
    ingested_bytes bigint NOT NULL DEFAULT 0 CHECK (ingested_bytes >= 0),
    metric_points bigint NOT NULL DEFAULT 0 CHECK (metric_points >= 0),
    log_records bigint NOT NULL DEFAULT 0 CHECK (log_records >= 0),
    spans bigint NOT NULL DEFAULT 0 CHECK (spans >= 0),
    estimated_storage_bytes bigint NOT NULL DEFAULT 0 CHECK (estimated_storage_bytes >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (enterprise_id, usage_date)
);

CREATE TABLE telemetry_dlq_records (
    id uuid PRIMARY KEY,
    signal text NOT NULL CHECK (signal IN ('metrics','logs','traces')),
    topic text NOT NULL,
    partition integer NOT NULL CHECK (partition >= 0),
    source_offset bigint NOT NULL CHECK (source_offset >= 0),
    dlq_topic text NOT NULL,
    dlq_partition integer NOT NULL CHECK (dlq_partition >= 0),
    dlq_offset bigint NOT NULL CHECK (dlq_offset >= 0),
    record_hash bytea NOT NULL CHECK (octet_length(record_hash) = 32),
    error_code text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','replaying','replayed','discarded','failed')),
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    replayed_at timestamptz,
    UNIQUE (topic, partition, source_offset, record_hash)
);

-- +goose Down
ALTER TABLE executions DROP CONSTRAINT IF EXISTS execution_external_operation;
ALTER TABLE executions DROP COLUMN IF EXISTS telemetry_collector_operation_id;
DROP TABLE IF EXISTS telemetry_collector_operations;
DROP TABLE IF EXISTS telemetry_dlq_records;
DROP TABLE IF EXISTS telemetry_usage_daily;
DROP TABLE IF EXISTS telemetry_retention_policies;
DROP TABLE IF EXISTS telemetry_certificates;
DROP TABLE IF EXISTS telemetry_enrollment_tokens;
DROP TABLE IF EXISTS kubernetes_node_host_bindings;
DROP TABLE IF EXISTS collection_claims;
DROP TABLE IF EXISTS telemetry_route_tests;
DROP TABLE IF EXISTS telemetry_routes;
DROP TABLE IF EXISTS collector_config_revisions;
DROP TRIGGER IF EXISTS collector_resource_enterprise ON collector_instances;
DROP FUNCTION IF EXISTS validate_collector_resource();
DROP TABLE IF EXISTS collector_instances;
DROP TABLE IF EXISTS collection_profiles;
DROP TABLE IF EXISTS collector_distribution_versions;
