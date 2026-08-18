-- +goose Up
ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_actor_type_check
    CHECK (actor_type IN ('platform_user', 'enterprise_user', 'service_account', 'connector', 'direct_executor', 'remote_access_gateway', 'system'));

CREATE TABLE remote_access_grants (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    subject_type text NOT NULL CHECK (subject_type IN ('user','department')),
    subject_id uuid NOT NULL,
    host_ids uuid[] NOT NULL DEFAULT '{}',
    host_selector jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(host_selector) = 'object'),
    host_selector_hash bytea NOT NULL CHECK (octet_length(host_selector_hash) = 32),
    managed_account_ids uuid[] NOT NULL CHECK (cardinality(managed_account_ids) > 0),
    protocols text[] NOT NULL CHECK (cardinality(protocols) > 0 AND protocols <@ ARRAY['ssh','winrs']::text[]),
    actions text[] NOT NULL DEFAULT ARRAY['terminal']::text[] CHECK (actions = ARRAY['terminal']::text[]),
    valid_from timestamptz NOT NULL,
    valid_until timestamptz NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id),
    CHECK (valid_until > valid_from),
    CHECK (cardinality(host_ids) > 0 OR host_selector <> '{}'::jsonb)
);
CREATE INDEX remote_access_grants_subject ON remote_access_grants (enterprise_id, subject_type, subject_id, enabled, valid_until);
CREATE INDEX remote_access_grants_selector_gin ON remote_access_grants USING gin (host_selector jsonb_path_ops);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_remote_access_grant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.subject_type = 'user' AND NOT EXISTS (SELECT 1 FROM enterprise_users WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'remote access grant user must belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.subject_type = 'department' AND NOT EXISTS (SELECT 1 FROM departments WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'remote access grant department must belong to enterprise' USING ERRCODE = '23503';
    END IF;
    IF EXISTS (SELECT 1 FROM unnest(NEW.host_ids) id WHERE NOT EXISTS (SELECT 1 FROM hosts WHERE hosts.id = id AND hosts.enterprise_id = NEW.enterprise_id AND hosts.status = 'active')) THEN
        RAISE EXCEPTION 'remote access grant host must belong to enterprise' USING ERRCODE = '23503';
    END IF;
    IF EXISTS (SELECT 1 FROM unnest(NEW.managed_account_ids) id WHERE NOT EXISTS (SELECT 1 FROM managed_accounts WHERE managed_accounts.id = id AND managed_accounts.enterprise_id = NEW.enterprise_id AND managed_accounts.status = 'active')) THEN
        RAISE EXCEPTION 'remote access grant account must belong to enterprise' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER remote_access_grant_enterprise
AFTER INSERT OR UPDATE ON remote_access_grants DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION validate_remote_access_grant();

CREATE TABLE remote_access_policies (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    enabled boolean NOT NULL DEFAULT true,
    priority integer NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 10000),
    protocols text[] NOT NULL CHECK (cardinality(protocols) > 0 AND protocols <@ ARRAY['ssh','winrs']::text[]),
    host_selector jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(host_selector) = 'object'),
    host_selector_hash bytea NOT NULL CHECK (octet_length(host_selector_hash) = 32),
    approver_role_ids uuid[] NOT NULL DEFAULT '{}',
    minimum_approvals integer NOT NULL DEFAULT 1 CHECK (minimum_approvals BETWEEN 1 AND 16),
    separation_of_duties boolean NOT NULL DEFAULT true,
    require_mfa boolean NOT NULL DEFAULT false,
    max_session_seconds integer NOT NULL DEFAULT 3600 CHECK (max_session_seconds BETWEEN 60 AND 3600),
    idle_timeout_seconds integer NOT NULL DEFAULT 900 CHECK (idle_timeout_seconds BETWEEN 60 AND 900),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX remote_access_policies_name_unique ON remote_access_policies (enterprise_id, lower(name));

CREATE TABLE remote_access_requests (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    requester_id uuid NOT NULL,
    grant_id uuid NOT NULL,
    host_id uuid NOT NULL,
    managed_account_id uuid NOT NULL,
    protocol text NOT NULL CHECK (protocol IN ('ssh','winrs')),
    action text NOT NULL DEFAULT 'terminal' CHECK (action = 'terminal'),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 2048),
    status text NOT NULL CHECK (status IN ('requested','awaiting_approval','authorized','rejected','expired','invalidated')),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (requester_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    FOREIGN KEY (grant_id, enterprise_id) REFERENCES remote_access_grants(id, enterprise_id),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (managed_account_id, enterprise_id) REFERENCES managed_accounts(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE INDEX remote_access_requests_requester ON remote_access_requests (enterprise_id, requester_id, created_at DESC, id DESC);

CREATE TABLE remote_access_requirement_snapshots (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL REFERENCES remote_access_requests(id),
    policy_id uuid NOT NULL REFERENCES remote_access_policies(id),
    policy_version bigint NOT NULL CHECK (policy_version > 0),
    approver_role_ids uuid[] NOT NULL DEFAULT '{}',
    minimum_approvals integer NOT NULL CHECK (minimum_approvals BETWEEN 1 AND 16),
    separation_of_duties boolean NOT NULL,
    require_mfa boolean NOT NULL,
    max_session_seconds integer NOT NULL CHECK (max_session_seconds BETWEEN 60 AND 3600),
    idle_timeout_seconds integer NOT NULL CHECK (idle_timeout_seconds BETWEEN 60 AND 900),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','satisfied','rejected','invalidated')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (request_id, policy_id)
);

CREATE TABLE remote_access_decisions (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL REFERENCES remote_access_requests(id),
    requirement_id uuid NOT NULL REFERENCES remote_access_requirement_snapshots(id),
    decision text NOT NULL CHECK (decision IN ('approve','reject')),
    comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 2048),
    decided_by uuid NOT NULL REFERENCES enterprise_users(id),
    decided_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (requirement_id, decided_by)
);

CREATE TABLE remote_access_leases (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE REFERENCES remote_access_requests(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    user_id uuid NOT NULL,
    grant_id uuid NOT NULL,
    host_id uuid NOT NULL,
    managed_account_id uuid NOT NULL,
    protocol text NOT NULL CHECK (protocol IN ('ssh','winrs')),
    action text NOT NULL DEFAULT 'terminal' CHECK (action = 'terminal'),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    policy_snapshot_hash bytea NOT NULL CHECK (octet_length(policy_snapshot_hash) = 32),
    issued_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason text,
    FOREIGN KEY (user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    FOREIGN KEY (grant_id, enterprise_id) REFERENCES remote_access_grants(id, enterprise_id),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (managed_account_id, enterprise_id) REFERENCES managed_accounts(id, enterprise_id),
    UNIQUE (id, enterprise_id),
    CHECK (expires_at <= issued_at + interval '15 minutes')
);
CREATE INDEX remote_access_leases_active ON remote_access_leases (enterprise_id, user_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE remote_access_sessions (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    user_id uuid NOT NULL,
    http_session_id uuid NOT NULL REFERENCES sessions(id),
    lease_id uuid NOT NULL,
    host_id uuid NOT NULL,
    managed_account_id uuid NOT NULL,
    protocol text NOT NULL CHECK (protocol IN ('ssh','winrs')),
    connection_mode text NOT NULL CHECK (connection_mode IN ('via_bastion','connector_local','direct_ssh','direct_winrm')),
    connector_id uuid,
    connector_epoch bigint,
    status text NOT NULL CHECK (status IN ('requested','awaiting_approval','authorized','connecting','active','terminating','terminated','failed','expired','connection_lost','invalidated')),
    session_fence bigint NOT NULL DEFAULT 1 CHECK (session_fence > 0),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    idle_timeout_seconds integer NOT NULL CHECK (idle_timeout_seconds BETWEEN 60 AND 900),
    max_duration_seconds integer NOT NULL CHECK (max_duration_seconds BETWEEN 60 AND 3600),
    connect_before timestamptz NOT NULL,
    connected_at timestamptz,
    terminated_at timestamptz,
    termination_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    FOREIGN KEY (lease_id, enterprise_id) REFERENCES remote_access_leases(id, enterprise_id),
    FOREIGN KEY (host_id, enterprise_id) REFERENCES hosts(id, enterprise_id),
    FOREIGN KEY (managed_account_id, enterprise_id) REFERENCES managed_accounts(id, enterprise_id),
    FOREIGN KEY (connector_id, enterprise_id) REFERENCES connectors(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE INDEX remote_access_sessions_capacity ON remote_access_sessions (enterprise_id, user_id, host_id) WHERE status IN ('connecting','active','terminating');

CREATE TABLE remote_access_tickets (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES remote_access_sessions(id),
    ticket_hash bytea NOT NULL UNIQUE CHECK (octet_length(ticket_hash) = 32),
    http_session_id uuid NOT NULL REFERENCES sessions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    user_id uuid NOT NULL,
    host_id uuid NOT NULL,
    managed_account_id uuid NOT NULL,
    protocol text NOT NULL CHECK (protocol IN ('ssh','winrs')),
    lease_id uuid NOT NULL REFERENCES remote_access_leases(id),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    session_fence bigint NOT NULL CHECK (session_fence > 0),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    CHECK (expires_at <= created_at + interval '60 seconds')
);
CREATE INDEX remote_access_tickets_session ON remote_access_tickets (session_id, created_at DESC);

CREATE TABLE remote_access_recordings (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    session_id uuid NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'recording' CHECK (status IN ('recording','available','incomplete','failed','expired')),
    format text NOT NULL DEFAULT 'asciicast_v2' CHECK (format = 'asciicast_v2'),
    key_provider text NOT NULL,
    key_id text NOT NULL,
    key_version integer NOT NULL CHECK (key_version > 0),
    wrapped_dek bytea NOT NULL,
    chunk_count integer NOT NULL DEFAULT 0 CHECK (chunk_count >= 0),
    event_count bigint NOT NULL DEFAULT 0 CHECK (event_count >= 0),
    size_bytes bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    duration_ms bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    final_hash bytea CHECK (final_hash IS NULL OR octet_length(final_hash) = 32),
    retention_until timestamptz NOT NULL DEFAULT (now() + interval '90 days'),
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    FOREIGN KEY (session_id, enterprise_id) REFERENCES remote_access_sessions(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);

CREATE TABLE remote_access_recording_chunks (
    recording_id uuid NOT NULL REFERENCES remote_access_recordings(id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    object_key text NOT NULL UNIQUE,
    nonce bytea NOT NULL CHECK (octet_length(nonce) = 12),
    ciphertext_size bigint NOT NULL CHECK (ciphertext_size > 0),
    event_count integer NOT NULL CHECK (event_count > 0),
    started_at timestamptz NOT NULL,
    ended_at timestamptz NOT NULL,
    previous_hash bytea NOT NULL CHECK (octet_length(previous_hash) = 32),
    chunk_hash bytea NOT NULL CHECK (octet_length(chunk_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (recording_id, sequence),
    CHECK (ended_at >= started_at)
);

CREATE TABLE remote_access_command_events (
    id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES remote_access_sessions(id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL CHECK (event_type IN ('input','output','resize','marker','state')),
    command_hash bytea CHECK (command_hash IS NULL OR octet_length(command_hash) = 32),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (session_id, sequence)
);

CREATE TABLE remote_access_routes (
    session_id uuid PRIMARY KEY REFERENCES remote_access_sessions(id),
    gateway_instance text NOT NULL,
    connector_id uuid,
    connector_epoch bigint,
    session_fence bigint NOT NULL CHECK (session_fence > 0),
    lease_expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_actor_type_check
    CHECK (actor_type IN ('platform_user', 'enterprise_user', 'service_account', 'connector', 'direct_executor', 'system'));

DROP TABLE IF EXISTS remote_access_routes;
DROP TABLE IF EXISTS remote_access_command_events;
DROP TABLE IF EXISTS remote_access_recording_chunks;
DROP TABLE IF EXISTS remote_access_recordings;
DROP TABLE IF EXISTS remote_access_tickets;
DROP TABLE IF EXISTS remote_access_sessions;
DROP TABLE IF EXISTS remote_access_leases;
DROP TABLE IF EXISTS remote_access_decisions;
DROP TABLE IF EXISTS remote_access_requirement_snapshots;
DROP TABLE IF EXISTS remote_access_requests;
DROP TABLE IF EXISTS remote_access_policies;
DROP TRIGGER IF EXISTS remote_access_grant_enterprise ON remote_access_grants;
DROP FUNCTION IF EXISTS validate_remote_access_grant();
DROP TABLE IF EXISTS remote_access_grants;
