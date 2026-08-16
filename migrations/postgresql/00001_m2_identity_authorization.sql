-- +goose Up
CREATE TABLE platform_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    state text NOT NULL CHECK (state IN ('uninitialized', 'initializing', 'initialized')),
    initialized_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO platform_state (singleton, state) VALUES (true, 'uninitialized');

CREATE TABLE platform_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    platform_name text NOT NULL CHECK (char_length(platform_name) BETWEEN 1 AND 128),
    default_locale text NOT NULL CHECK (default_locale IN ('zh-CN', 'en-US')),
    timezone text NOT NULL,
    external_url text NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform_users (
    id uuid PRIMARY KEY,
    username text NOT NULL CHECK (char_length(username) BETWEEN 3 AND 128),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 128),
    email text,
    role text NOT NULL DEFAULT 'platform_super_admin' CHECK (role = 'platform_super_admin'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    mfa_enabled boolean NOT NULL DEFAULT false,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX platform_users_username_unique ON platform_users (lower(username));

CREATE TABLE enterprises (
    id uuid PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    code text NOT NULL CHECK (code ~ '^[a-z][a-z0-9-]{1,62}$'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'disabled')),
    timezone text NOT NULL,
    default_locale text NOT NULL DEFAULT 'zh-CN' CHECK (default_locale IN ('zh-CN', 'en-US')),
    remark text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (code),
    UNIQUE (id, status)
);

CREATE TABLE departments (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '',
    is_default boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX departments_name_unique ON departments (enterprise_id, lower(name));
CREATE UNIQUE INDEX departments_one_default_per_enterprise ON departments (enterprise_id) WHERE is_default;

CREATE TABLE enterprise_users (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    department_id uuid NOT NULL,
    username text NOT NULL CHECK (char_length(username) BETWEEN 3 AND 128),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 128),
    email text,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    mfa_enabled boolean NOT NULL DEFAULT false,
    authorization_version bigint NOT NULL DEFAULT 1 CHECK (authorization_version > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (department_id, enterprise_id) REFERENCES departments(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX enterprise_users_username_unique ON enterprise_users (lower(username));

CREATE TABLE password_credentials (
    id uuid PRIMARY KEY,
    audience text NOT NULL CHECK (audience IN ('platform', 'enterprise')),
    subject_id uuid NOT NULL,
    encoded_hash text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    temporary boolean NOT NULL DEFAULT false,
    expires_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (audience, subject_id),
    CHECK ((temporary AND expires_at IS NOT NULL) OR (NOT temporary))
);

CREATE TABLE permissions (
    id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9_.]+$'),
    description text NOT NULL DEFAULT '',
    registry_version integer NOT NULL CHECK (registry_version > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    identity_key text,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '',
    builtin boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (enterprise_id, identity_key),
    UNIQUE (id, enterprise_id),
    CHECK ((builtin AND identity_key IS NOT NULL) OR (NOT builtin AND identity_key IS NULL))
);
CREATE UNIQUE INDEX roles_name_unique ON roles (enterprise_id, lower(name));

CREATE TABLE role_permissions (
    role_id uuid NOT NULL REFERENCES roles(id),
    permission_id text NOT NULL REFERENCES permissions(id),
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE data_scopes (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '',
    resource_types text[] NOT NULL CHECK (cardinality(resource_types) > 0),
    explicit_resource_ids text[] NOT NULL DEFAULT '{}',
    label_selector jsonb,
    selector_hash bytea NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX data_scopes_name_unique ON data_scopes (enterprise_id, lower(name));

CREATE TABLE service_accounts (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '',
    allowed_tool_ids text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    authorization_version bigint NOT NULL DEFAULT 1 CHECK (authorization_version > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX service_accounts_name_unique ON service_accounts (enterprise_id, lower(name));

CREATE TABLE service_account_data_scopes (
    service_account_id uuid NOT NULL,
    data_scope_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    PRIMARY KEY (service_account_id, data_scope_id),
    FOREIGN KEY (service_account_id, enterprise_id) REFERENCES service_accounts(id, enterprise_id),
    FOREIGN KEY (data_scope_id, enterprise_id) REFERENCES data_scopes(id, enterprise_id)
);

CREATE TABLE role_bindings (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'department', 'service_account')),
    subject_id uuid NOT NULL,
    role_id uuid NOT NULL,
    valid_from timestamptz,
    valid_until timestamptz,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (role_id, enterprise_id) REFERENCES roles(id, enterprise_id),
    UNIQUE (id, enterprise_id),
    CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until > valid_from)
);

CREATE TABLE role_binding_data_scopes (
    role_binding_id uuid NOT NULL,
    data_scope_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    PRIMARY KEY (role_binding_id, data_scope_id),
    FOREIGN KEY (role_binding_id, enterprise_id) REFERENCES role_bindings(id, enterprise_id),
    FOREIGN KEY (data_scope_id, enterprise_id) REFERENCES data_scopes(id, enterprise_id)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_role_binding_subject() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.subject_type = 'user' AND NOT EXISTS (SELECT 1 FROM enterprise_users WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'role binding user must belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.subject_type = 'department' AND NOT EXISTS (SELECT 1 FROM departments WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'role binding department must belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.subject_type = 'service_account' AND NOT EXISTS (SELECT 1 FROM service_accounts WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'role binding service account must belong to enterprise' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER role_binding_subject_enterprise
AFTER INSERT OR UPDATE ON role_bindings DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION validate_role_binding_subject();

CREATE TABLE authorization_versions (
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'department', 'service_account')),
    subject_id uuid NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (enterprise_id, subject_type, subject_id)
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_hash bytea NOT NULL CHECK (octet_length(csrf_hash) = 32),
    audience text NOT NULL CHECK (audience IN ('platform', 'enterprise')),
    user_id uuid NOT NULL,
    enterprise_id uuid REFERENCES enterprises(id),
    department_id uuid,
    authorization_version bigint,
    locale text NOT NULL CHECK (locale IN ('zh-CN', 'en-US')),
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
      (audience = 'platform' AND enterprise_id IS NULL AND department_id IS NULL AND authorization_version IS NULL)
      OR
      (audience = 'enterprise' AND enterprise_id IS NOT NULL AND department_id IS NOT NULL AND authorization_version IS NOT NULL)
    )
);
CREATE INDEX sessions_active_token ON sessions (token_hash) WHERE revoked_at IS NULL;
CREATE INDEX sessions_subject ON sessions (audience, user_id) WHERE revoked_at IS NULL;

CREATE TABLE temporary_credentials (
    id uuid PRIMARY KEY,
    audience text NOT NULL CHECK (audience IN ('platform', 'enterprise')),
    user_id uuid NOT NULL,
    challenge_hash bytea CHECK (challenge_hash IS NULL OR octet_length(challenge_hash) = 32),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'consumed', 'expired', 'revoked')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX temporary_credentials_subject ON temporary_credentials (audience, user_id, status);

CREATE TABLE api_keys (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL,
    service_account_id uuid NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    prefix text NOT NULL UNIQUE CHECK (char_length(prefix) BETWEEN 6 AND 32),
    secret_hash bytea NOT NULL CHECK (octet_length(secret_hash) = 32),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'expired')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    expires_at timestamptz,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (service_account_id, enterprise_id) REFERENCES service_accounts(id, enterprise_id)
);

CREATE TABLE audit_chain_heads (
    chain_key text PRIMARY KEY,
    domain text NOT NULL CHECK (domain IN ('platform', 'enterprise')),
    enterprise_id uuid,
    last_event_id uuid,
    last_hash bytea NOT NULL CHECK (octet_length(last_hash) = 32),
    version bigint NOT NULL DEFAULT 1,
    CHECK ((domain = 'platform' AND enterprise_id IS NULL) OR (domain = 'enterprise' AND enterprise_id IS NOT NULL))
);

CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    domain text NOT NULL CHECK (domain IN ('platform', 'enterprise')),
    enterprise_id uuid,
    actor_type text NOT NULL CHECK (actor_type IN ('platform_user', 'enterprise_user', 'service_account', 'system')),
    actor_id text NOT NULL,
    action text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_.]+$'),
    resource_type text,
    resource_id text,
    result text NOT NULL CHECK (result IN ('success', 'failure', 'denied')),
    details jsonb NOT NULL DEFAULT '{}',
    previous_hash bytea NOT NULL CHECK (octet_length(previous_hash) = 32),
    event_hash bytea NOT NULL UNIQUE CHECK (octet_length(event_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((domain = 'platform' AND enterprise_id IS NULL) OR (domain = 'enterprise' AND enterprise_id IS NOT NULL))
);
CREATE INDEX audit_events_platform_cursor ON audit_events (created_at DESC, id DESC) WHERE domain = 'platform';
CREATE INDEX audit_events_enterprise_cursor ON audit_events (enterprise_id, created_at DESC, id DESC) WHERE domain = 'enterprise';

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY,
    topic text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    claimed_at timestamptz,
    published_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_pending ON outbox_events (available_at, created_at) WHERE published_at IS NULL;

CREATE TABLE idempotency_records (
    audience text NOT NULL CHECK (audience IN ('setup', 'platform', 'enterprise', 'api_key')),
    subject_id text NOT NULL,
    operation text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    response_status integer,
    response_nonce bytea,
    response_ciphertext bytea,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (audience, subject_id, operation, idempotency_key),
    CHECK ((response_ciphertext IS NULL AND response_nonce IS NULL AND response_status IS NULL) OR
           (response_ciphertext IS NOT NULL AND response_nonce IS NOT NULL AND response_status IS NOT NULL))
);

REVOKE UPDATE, DELETE ON audit_events FROM PUBLIC;

-- +goose Down
DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS audit_chain_heads;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS temporary_credentials;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS authorization_versions;
DROP TRIGGER IF EXISTS role_binding_subject_enterprise ON role_bindings;
DROP FUNCTION IF EXISTS validate_role_binding_subject();
DROP TABLE IF EXISTS role_binding_data_scopes;
DROP TABLE IF EXISTS role_bindings;
DROP TABLE IF EXISTS service_account_data_scopes;
DROP TABLE IF EXISTS service_accounts;
DROP TABLE IF EXISTS data_scopes;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS password_credentials;
DROP TABLE IF EXISTS enterprise_users;
DROP TABLE IF EXISTS departments;
DROP TABLE IF EXISTS enterprises;
DROP TABLE IF EXISTS platform_users;
DROP TABLE IF EXISTS platform_settings;
DROP TABLE IF EXISTS platform_state;
