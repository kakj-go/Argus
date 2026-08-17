-- +goose Up

ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_actor_type_check
    CHECK (actor_type IN ('platform_user', 'enterprise_user', 'service_account', 'connector', 'direct_executor', 'system'));
INSERT INTO permissions (id, description, registry_version) VALUES
    ('host.read', 'Read hosts', 2),
    ('host.manage', 'Manage hosts', 2),
    ('host.test', 'Test host connections', 2),
    ('kubernetes.read', 'Read Kubernetes clusters and resources', 2),
    ('kubernetes.manage', 'Manage Kubernetes clusters', 2),
    ('kubernetes.logs', 'Read bounded Kubernetes Pod logs', 2),
    ('secret.read', 'Read Secret metadata', 2),
    ('secret.manage', 'Create and rotate Secrets', 2),
    ('credential.read', 'Read Credential metadata', 2),
    ('credential.manage', 'Manage Credentials', 2),
    ('credential.use', 'Use Credentials through the broker', 2),
    ('managed_account.read', 'Read managed accounts', 2),
    ('managed_account.manage', 'Manage managed accounts', 2),
    ('bastion_scope.read', 'Read Bastion Scopes', 2),
    ('bastion_scope.manage', 'Manage Bastion Scopes', 2),
    ('connector.read', 'Read Connector diagnostics', 2),
    ('connector.manage', 'Manage Connector lifecycle', 2),
    ('pending_action.read', 'Read resource Pending Actions', 2),
    ('pending_action.confirm', 'Confirm resource Pending Actions', 2)
ON CONFLICT (id) DO UPDATE SET description = EXCLUDED.description, registry_version = EXCLUDED.registry_version;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id
FROM roles
CROSS JOIN permissions
WHERE roles.identity_key = 'enterprise_admin' AND permissions.registry_version = 2
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permission_id
FROM roles
CROSS JOIN unnest(ARRAY[
    'host.read','host.manage','host.test','kubernetes.read','kubernetes.manage','kubernetes.logs',
    'secret.read','secret.manage','credential.read','credential.manage','credential.use',
    'managed_account.read','managed_account.manage','bastion_scope.read','bastion_scope.manage',
    'connector.read','connector.manage','pending_action.read','pending_action.confirm'
]) AS permission_id
WHERE roles.identity_key = 'resource_admin'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permission_id
FROM roles
CROSS JOIN unnest(ARRAY[
    'host.read','host.test','kubernetes.read','kubernetes.logs','secret.read','credential.read',
    'credential.use','managed_account.read','bastion_scope.read','connector.read','pending_action.read'
]) AS permission_id
WHERE roles.identity_key = 'resource_operator'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permission_id
FROM roles
CROSS JOIN unnest(ARRAY[
    'host.read','kubernetes.read','secret.read','credential.read','managed_account.read',
    'bastion_scope.read','connector.read','pending_action.read'
]) AS permission_id
WHERE roles.identity_key IN ('resource_viewer', 'resource_approver')
ON CONFLICT DO NOTHING;

CREATE TABLE secrets (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    type text NOT NULL CHECK (type IN ('ssh_password','ssh_private_key','winrm_password','kubeconfig','api_token','basic_auth')),
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    current_version integer NOT NULL DEFAULT 1 CHECK (current_version > 0),
    last_accessed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX secrets_name_unique ON secrets (enterprise_id, lower(name));

CREATE TABLE secret_versions (
    id uuid PRIMARY KEY,
    secret_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    provider text NOT NULL DEFAULT 'local' CHECK (provider IN ('local','vault','openbao','aws_kms','gcp_kms','azure_key_vault')),
    key_id text NOT NULL,
    key_version integer NOT NULL CHECK (key_version > 0),
    wrapped_dek bytea NOT NULL,
    wrap_nonce bytea NOT NULL,
    nonce bytea NOT NULL,
    ciphertext bytea NOT NULL,
    value_hash bytea NOT NULL CHECK (octet_length(value_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (secret_id, enterprise_id) REFERENCES secrets(id, enterprise_id),
    UNIQUE (secret_id, version),
    UNIQUE (id, enterprise_id)
);

CREATE TABLE credentials (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    protocol text NOT NULL CHECK (protocol IN ('ssh','winrm','kubernetes','http')),
    username text NOT NULL DEFAULT '',
    secret_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (secret_id, enterprise_id) REFERENCES secrets(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX credentials_name_unique ON credentials (enterprise_id, lower(name));

CREATE TABLE bastion_scopes (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    environment text NOT NULL CHECK (environment IN ('development','staging','production')),
    labels jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(labels) = 'object' AND octet_length(labels::text) <= 4096),
    labels_hash bytea NOT NULL CHECK (octet_length(labels_hash) = 32),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','suspected_offline','offline','uninstalling','uninstalled','deleted')),
    connector_host_id uuid,
    active_connector_id uuid,
    fencing_generation bigint NOT NULL DEFAULT 1 CHECK (fencing_generation > 0),
    resource_version bigint NOT NULL DEFAULT 1 CHECK (resource_version > 0),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX bastion_scopes_name_unique ON bastion_scopes (enterprise_id, lower(name)) WHERE status <> 'deleted';
CREATE INDEX bastion_scopes_labels_gin ON bastion_scopes USING gin (labels jsonb_path_ops);

CREATE TABLE hosts (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    hostname text NOT NULL DEFAULT '',
    address text NOT NULL CHECK (char_length(address) BETWEEN 1 AND 512),
    port integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    platform text NOT NULL CHECK (platform IN ('linux','windows')),
    connection_mode text NOT NULL CHECK (connection_mode IN ('connector_local','via_bastion','direct_ssh','direct_winrm')),
    bastion_scope_id uuid,
    connector_id uuid,
    environment text NOT NULL CHECK (environment IN ('development','staging','production')),
    labels jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(labels) = 'object' AND octet_length(labels::text) <= 4096),
    labels_hash bytea NOT NULL CHECK (octet_length(labels_hash) = 32),
    labels_version bigint NOT NULL DEFAULT 1 CHECK (labels_version > 0),
    resource_version bigint NOT NULL DEFAULT 1 CHECK (resource_version > 0),
    connection_status text NOT NULL DEFAULT 'unknown' CHECK (connection_status IN ('online','offline','onboarding','degraded','unknown')),
    pinned_host_key text NOT NULL DEFAULT '',
    last_seen_at timestamptz,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','deleted')),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (bastion_scope_id, enterprise_id) REFERENCES bastion_scopes(id, enterprise_id),
    UNIQUE (id, enterprise_id),
    CHECK ((connection_mode IN ('connector_local','via_bastion') AND bastion_scope_id IS NOT NULL) OR
           (connection_mode IN ('direct_ssh','direct_winrm') AND bastion_scope_id IS NULL))
);
CREATE UNIQUE INDEX hosts_name_unique ON hosts (enterprise_id, lower(name)) WHERE status <> 'deleted';
CREATE INDEX hosts_scope_index ON hosts (enterprise_id, bastion_scope_id, created_at, id) WHERE status <> 'deleted';
CREATE INDEX hosts_labels_gin ON hosts USING gin (labels jsonb_path_ops);

CREATE TABLE managed_accounts (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    host_id uuid NOT NULL,
    username text NOT NULL CHECK (char_length(username) BETWEEN 1 AND 256),
    privilege_level text NOT NULL CHECK (privilege_level IN ('standard','sudo','administrator')),
    credential_id uuid NOT NULL,
    allowed_protocols text[] NOT NULL CHECK (cardinality(allowed_protocols) > 0),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (credential_id, enterprise_id) REFERENCES credentials(id, enterprise_id),
    UNIQUE (id, enterprise_id),
    UNIQUE (host_id, username)
);

CREATE TABLE kubernetes_clusters (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    api_server text NOT NULL CHECK (char_length(api_server) BETWEEN 1 AND 2048),
    connection_mode text NOT NULL CHECK (connection_mode IN ('via_bastion','direct','in_cluster')),
    bastion_scope_id uuid,
    connector_id uuid,
    credential_id uuid,
    default_namespace text NOT NULL DEFAULT '',
    environment text NOT NULL CHECK (environment IN ('development','staging','production')),
    labels jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(labels) = 'object' AND octet_length(labels::text) <= 4096),
    labels_hash bytea NOT NULL CHECK (octet_length(labels_hash) = 32),
    labels_version bigint NOT NULL DEFAULT 1 CHECK (labels_version > 0),
    resource_version bigint NOT NULL DEFAULT 1 CHECK (resource_version > 0),
    connection_status text NOT NULL DEFAULT 'disconnected' CHECK (connection_status IN ('pending_connector','connected','degraded','disconnected')),
    kubernetes_version text NOT NULL DEFAULT '',
    node_count integer NOT NULL DEFAULT 0 CHECK (node_count >= 0),
    ready_node_count integer NOT NULL DEFAULT 0 CHECK (ready_node_count >= 0),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','deleted')),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (bastion_scope_id, enterprise_id) REFERENCES bastion_scopes(id, enterprise_id),
    FOREIGN KEY (credential_id, enterprise_id) REFERENCES credentials(id, enterprise_id),
    UNIQUE (id, enterprise_id),
    CHECK ((connection_mode = 'via_bastion' AND bastion_scope_id IS NOT NULL AND credential_id IS NOT NULL) OR
           (connection_mode = 'direct' AND bastion_scope_id IS NULL AND credential_id IS NOT NULL) OR
           (connection_mode = 'in_cluster' AND bastion_scope_id IS NULL AND credential_id IS NULL))
);
CREATE UNIQUE INDEX kubernetes_clusters_name_unique ON kubernetes_clusters (enterprise_id, lower(name)) WHERE status <> 'deleted';
CREATE INDEX kubernetes_clusters_labels_gin ON kubernetes_clusters USING gin (labels jsonb_path_ops);

CREATE TABLE connectors (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    role text NOT NULL CHECK (role IN ('bastion','kubernetes')),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    host_id uuid,
    bastion_scope_id uuid,
    kubernetes_cluster_id uuid,
    instance_id text NOT NULL,
    device_fingerprint_hash bytea NOT NULL CHECK (octet_length(device_fingerprint_hash) = 32),
    public_key_hash bytea NOT NULL CHECK (octet_length(public_key_hash) = 32),
    software_version text NOT NULL DEFAULT '',
    capabilities text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','online','suspected_offline','offline','draining','uninstalled','revoked')),
    connection_epoch bigint NOT NULL DEFAULT 0 CHECK (connection_epoch >= 0),
    certificate_expires_at timestamptz NOT NULL,
    certificate_rotation_requested_at timestamptz,
    connected_at timestamptz,
    last_heartbeat_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (bastion_scope_id, enterprise_id) REFERENCES bastion_scopes(id, enterprise_id),
    FOREIGN KEY (kubernetes_cluster_id, enterprise_id) REFERENCES kubernetes_clusters(id, enterprise_id),
    UNIQUE (id, enterprise_id),
    UNIQUE (enterprise_id, instance_id),
    CHECK ((role = 'bastion' AND host_id IS NOT NULL AND bastion_scope_id IS NOT NULL AND kubernetes_cluster_id IS NULL) OR
           (role = 'kubernetes' AND host_id IS NULL AND bastion_scope_id IS NULL AND kubernetes_cluster_id IS NOT NULL))
);
ALTER TABLE bastion_scopes ADD CONSTRAINT bastion_scope_connector_host_fk
    FOREIGN KEY (connector_host_id, enterprise_id) REFERENCES hosts(id, enterprise_id);
ALTER TABLE bastion_scopes ADD CONSTRAINT bastion_scope_active_connector_fk
    FOREIGN KEY (active_connector_id, enterprise_id) REFERENCES connectors(id, enterprise_id);

CREATE TABLE connector_certificates (
    id uuid PRIMARY KEY,
    connector_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    serial_number text NOT NULL UNIQUE,
    issuer_generation integer NOT NULL CHECK (issuer_generation > 0),
    certificate_request_name text NOT NULL,
    certificate_pem text NOT NULL,
    ca_bundle_pem text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','overlap','revoked','expired')),
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (connector_id, enterprise_id) REFERENCES connectors(id, enterprise_id)
);
CREATE INDEX connector_certificates_active ON connector_certificates (connector_id, status, not_after);

CREATE TABLE connector_enrollment_tokens (
    id uuid PRIMARY KEY,
    preallocated_connector_id uuid NOT NULL UNIQUE,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    role text NOT NULL CHECK (role IN ('bastion','kubernetes')),
    purpose text NOT NULL CHECK (purpose IN ('initial_registration','connector_replacement','kubernetes_registration')),
    bastion_scope_id uuid,
    kubernetes_cluster_id uuid,
    preallocated_host_id uuid,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    policy jsonb NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','consumed','revoked','expired')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    consumed_device_hash bytea,
    registered_connector_id uuid,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (bastion_scope_id, enterprise_id) REFERENCES bastion_scopes(id, enterprise_id),
    FOREIGN KEY (kubernetes_cluster_id, enterprise_id) REFERENCES kubernetes_clusters(id, enterprise_id),
    CHECK ((role = 'bastion' AND bastion_scope_id IS NOT NULL AND preallocated_host_id IS NOT NULL AND kubernetes_cluster_id IS NULL) OR
           (role = 'kubernetes' AND bastion_scope_id IS NULL AND preallocated_host_id IS NULL AND kubernetes_cluster_id IS NOT NULL))
);
CREATE UNIQUE INDEX connector_one_active_enrollment ON connector_enrollment_tokens
    (enterprise_id, role, COALESCE(bastion_scope_id, kubernetes_cluster_id)) WHERE status = 'active';

CREATE TABLE connector_sessions (
    connector_id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL,
    gateway_instance_id text NOT NULL,
    connection_epoch bigint NOT NULL CHECK (connection_epoch > 0),
    capabilities text[] NOT NULL DEFAULT '{}',
    connected_at timestamptz NOT NULL,
    last_heartbeat_at timestamptz NOT NULL,
    draining boolean NOT NULL DEFAULT false,
    FOREIGN KEY (connector_id, enterprise_id) REFERENCES connectors(id, enterprise_id)
);

CREATE TABLE connection_tests (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    target_type text NOT NULL CHECK (target_type IN ('host','kubernetes_cluster')),
    resource_id uuid,
    path text NOT NULL CHECK (path IN ('connector','direct','in_cluster')),
    connector_id uuid,
    connection_epoch bigint,
    credential_id uuid,
    credential_version bigint,
    request_plan jsonb NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed','result_unknown','expired')),
    result jsonb NOT NULL DEFAULT '{}',
    error_code text,
    expires_at timestamptz NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (credential_id, enterprise_id) REFERENCES credentials(id, enterprise_id),
    FOREIGN KEY (connector_id, enterprise_id) REFERENCES connectors(id, enterprise_id)
);
CREATE INDEX connection_tests_expiry ON connection_tests (enterprise_id, expires_at);

CREATE TABLE pending_actions (
    id uuid PRIMARY KEY,
    action_ref text NOT NULL UNIQUE,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    creator_user_id uuid NOT NULL,
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    action_type text NOT NULL CHECK (action_type ~ '^[a-z][a-z0-9_.]+$'),
    title text NOT NULL,
    summary text NOT NULL,
    risk text NOT NULL CHECK (risk IN ('write','dangerous','critical')),
    preview jsonb NOT NULL,
    diff jsonb NOT NULL DEFAULT '[]',
    status text NOT NULL CHECK (status IN ('prepared','awaiting_confirmation','executing','succeeded','failed','cancelled','expired','invalidated')),
    resource_type text NOT NULL,
    resource_id uuid,
    expected_resource_version bigint,
    impact_hash bytea NOT NULL CHECK (octet_length(impact_hash) = 32),
    result_resource_type text,
    result_resource_id uuid,
    result_resource_version bigint,
    result_summary text NOT NULL DEFAULT '',
    error_code text,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pending_actions_enterprise_cursor ON pending_actions (enterprise_id, created_at DESC, id DESC);

CREATE TABLE pending_action_plans (
    id uuid PRIMARY KEY,
    pending_action_id uuid NOT NULL UNIQUE REFERENCES pending_actions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    preview_call_id text NOT NULL,
    commit_tool text NOT NULL CHECK (commit_tool ~ '^[a-z][a-z0-9_.]+[.]commit$'),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    plan_schema_version text NOT NULL,
    plan_hash bytea NOT NULL CHECK (octet_length(plan_hash) = 32),
    immutable_plan jsonb NOT NULL,
    resource_scope_snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE pending_action_tokens (
    id uuid PRIMARY KEY,
    pending_action_id uuid NOT NULL UNIQUE REFERENCES pending_actions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    key_version integer NOT NULL CHECK (key_version > 0),
    nonce bytea NOT NULL,
    ciphertext bytea NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','consumed','revoked','expired')),
    consumed_at timestamptz,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE connector_commands (
    id uuid PRIMARY KEY,
    command_id text NOT NULL UNIQUE,
    enterprise_id uuid NOT NULL,
    connector_id uuid NOT NULL,
    connection_epoch bigint NOT NULL CHECK (connection_epoch > 0),
    operation_ref text NOT NULL,
    credential_lease_id uuid,
    command_type text NOT NULL CHECK (command_type IN ('host_connection_probe','kubernetes_connection_probe','kubernetes_resource_query','kubernetes_pod_logs','connector_uninstall')),
    payload_schema_version text NOT NULL,
    payload jsonb NOT NULL,
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash) = 32),
    idempotency_key text NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','dispatched','acknowledged','running','succeeded','failed','timed_out','delivery_unknown','result_unknown','expired')),
    result jsonb NOT NULL DEFAULT '{}',
    result_hash bytea,
    error_code text,
    expires_at timestamptz NOT NULL,
    acknowledged_at timestamptz,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (connector_id, enterprise_id) REFERENCES connectors(id, enterprise_id),
    UNIQUE (connector_id, idempotency_key)
);
CREATE INDEX connector_commands_dispatch ON connector_commands (connector_id, status, created_at);

CREATE TABLE credential_leases (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    credential_id uuid NOT NULL,
    secret_version_id uuid NOT NULL,
    operation_ref text NOT NULL,
    target_resource_type text NOT NULL,
    target_resource_id uuid NOT NULL,
    recipient_type text NOT NULL CHECK (recipient_type IN ('connector','direct_executor')),
    recipient_id text NOT NULL,
    protocol text NOT NULL CHECK (protocol IN ('ssh','winrm','kubernetes','http')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','consumed','expired','revoked')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (credential_id, enterprise_id) REFERENCES credentials(id, enterprise_id),
    FOREIGN KEY (secret_version_id, enterprise_id) REFERENCES secret_versions(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE INDEX credential_leases_active ON credential_leases (enterprise_id, recipient_type, recipient_id, status, expires_at);
ALTER TABLE connector_commands ADD CONSTRAINT connector_command_credential_lease_fk
    FOREIGN KEY (credential_lease_id, enterprise_id) REFERENCES credential_leases(id, enterprise_id);

-- +goose Down

ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_actor_type_check
    CHECK (actor_type IN ('platform_user', 'enterprise_user', 'service_account', 'system'));
DROP TABLE IF EXISTS credential_leases;
DROP TABLE IF EXISTS connector_commands;
DROP TABLE IF EXISTS pending_action_tokens;
DROP TABLE IF EXISTS pending_action_plans;
DROP TABLE IF EXISTS pending_actions;
DROP TABLE IF EXISTS connection_tests;
DROP TABLE IF EXISTS connector_sessions;
DROP TABLE IF EXISTS connector_enrollment_tokens;
DROP TABLE IF EXISTS connector_certificates;
ALTER TABLE bastion_scopes DROP CONSTRAINT IF EXISTS bastion_scope_active_connector_fk;
ALTER TABLE bastion_scopes DROP CONSTRAINT IF EXISTS bastion_scope_connector_host_fk;
DROP TABLE IF EXISTS connectors;
DROP TABLE IF EXISTS kubernetes_clusters;
DROP TABLE IF EXISTS managed_accounts;
DROP TABLE IF EXISTS hosts;
DROP TABLE IF EXISTS bastion_scopes;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS secret_versions;
DROP TABLE IF EXISTS secrets;
DELETE FROM role_permissions WHERE permission_id IN (
    'host.read','host.manage','host.test','kubernetes.read','kubernetes.manage','kubernetes.logs',
    'secret.read','secret.manage','credential.read','credential.manage','credential.use',
    'managed_account.read','managed_account.manage','bastion_scope.read','bastion_scope.manage',
    'connector.read','connector.manage','pending_action.read','pending_action.confirm'
);
DELETE FROM permissions WHERE registry_version = 2;
