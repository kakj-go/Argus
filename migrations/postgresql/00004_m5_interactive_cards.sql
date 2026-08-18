-- +goose Up

INSERT INTO permissions (id, description, registry_version) VALUES
    ('interactive_card.read', 'Read the interactive Card catalog', 4),
    ('interactive_card.create', 'Create enterprise Card drafts through Chat', 4),
    ('interactive_card.update', 'Create Card configuration revisions', 4),
    ('interactive_card.publish', 'Validate, activate, disable, and roll back Cards', 4),
    ('interactive_card.deprecate', 'Deprecate enterprise Cards', 4)
ON CONFLICT (id) DO UPDATE SET description = EXCLUDED.description, registry_version = EXCLUDED.registry_version;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id FROM roles CROSS JOIN permissions
WHERE roles.identity_key = 'enterprise_admin' AND permissions.registry_version = 4
ON CONFLICT DO NOTHING;

CREATE TABLE interactive_cards (
    id uuid PRIMARY KEY,
    enterprise_id uuid REFERENCES enterprises(id),
    source text NOT NULL CHECK (source IN ('system','enterprise')),
    slug text NOT NULL CHECK (slug ~ '^[a-z][a-z0-9-]{0,62}$'),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2048),
    lifecycle text NOT NULL DEFAULT 'draft' CHECK (lifecycle IN ('draft','active','deprecated')),
    enabled boolean NOT NULL DEFAULT false,
    availability text NOT NULL DEFAULT 'disabled' CHECK (availability IN ('available','disabled','dependency_pending','invalidated')),
    active_version_id uuid,
    latest_revision integer NOT NULL DEFAULT 1 CHECK (latest_revision > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((source = 'system' AND enterprise_id IS NULL AND created_by IS NULL) OR
           (source = 'enterprise' AND enterprise_id IS NOT NULL AND created_by IS NOT NULL)),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX interactive_cards_system_slug_unique ON interactive_cards (slug) WHERE source = 'system';
CREATE UNIQUE INDEX interactive_cards_enterprise_slug_unique ON interactive_cards (enterprise_id, slug) WHERE source = 'enterprise';
CREATE INDEX interactive_cards_catalog ON interactive_cards (enterprise_id, source, enabled, availability, updated_at DESC);

CREATE TABLE card_versions (
    id uuid PRIMARY KEY,
    card_id uuid NOT NULL REFERENCES interactive_cards(id),
    revision integer NOT NULL CHECK (revision > 0),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','validating','validated','active','retired')),
    manifest jsonb NOT NULL,
    entrypoint_html bytea NOT NULL CHECK (octet_length(entrypoint_html) <= 524288),
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    manifest_hash bytea NOT NULL CHECK (octet_length(manifest_hash) = 32),
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (card_id, revision),
    UNIQUE (id, card_id)
);
ALTER TABLE interactive_cards ADD CONSTRAINT interactive_cards_active_version_fk
    FOREIGN KEY (active_version_id, id) REFERENCES card_versions(id, card_id);

CREATE TABLE card_slot_bindings (
    id uuid PRIMARY KEY,
    card_version_id uuid NOT NULL REFERENCES card_versions(id) ON DELETE CASCADE,
    slot_name text NOT NULL CHECK (slot_name ~ '^[a-z][a-z0-9_]*$'),
    slot_kind text NOT NULL CHECK (slot_kind IN ('data','query','action')),
    mode text NOT NULL CHECK (mode IN ('strict','preferred')),
    tool_id text NOT NULL CHECK (tool_id ~ '^[a-z][a-z0-9_.-]+$'),
    output_schema_version text NOT NULL,
    schema_hash bytea NOT NULL CHECK (octet_length(schema_hash) = 32),
    field_path text NOT NULL,
    value_type text NOT NULL CHECK (value_type IN ('string','number','boolean','array','object')),
    semantic_type text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (card_version_id, slot_name)
);

CREATE TABLE card_demo_scenarios (
    id uuid PRIMARY KEY,
    card_version_id uuid NOT NULL REFERENCES card_versions(id) ON DELETE CASCADE,
    scenario text NOT NULL CHECK (scenario IN ('default','empty','error','large','light','dark','zh-CN','en-US')),
    data jsonb NOT NULL,
    byte_size integer NOT NULL CHECK (byte_size >= 0 AND byte_size <= 262144),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (card_version_id, scenario)
);

CREATE TABLE card_validation_runs (
    id uuid PRIMARY KEY,
    card_version_id uuid NOT NULL REFERENCES card_versions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    actor_user_id uuid NOT NULL,
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    runtime_version text NOT NULL CHECK (char_length(runtime_version) BETWEEN 1 AND 128),
    nonce_hash bytea NOT NULL CHECK (octet_length(nonce_hash) = 32),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','passed','failed','expired')),
    required_scenarios text[] NOT NULL,
    passed_scenarios text[] NOT NULL DEFAULT '{}',
    issues jsonb NOT NULL DEFAULT '[]',
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (actor_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);

CREATE TABLE card_instances (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    conversation_id uuid NOT NULL,
    run_id uuid,
    card_id uuid NOT NULL REFERENCES interactive_cards(id),
    card_version_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    presentation_kind text NOT NULL CHECK (presentation_kind IN ('table','detail','pending_action','metric','generic')),
    render_spec jsonb NOT NULL,
    render_spec_hash bytea NOT NULL CHECK (octet_length(render_spec_hash) = 32),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','invalidated')),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, enterprise_id) REFERENCES conversations(id, enterprise_id),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    FOREIGN KEY (actor_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    FOREIGN KEY (card_version_id, card_id) REFERENCES card_versions(id, card_id),
    UNIQUE (id, enterprise_id)
);
CREATE INDEX card_instances_conversation ON card_instances (conversation_id, created_at, id);

CREATE TABLE card_data_sources (
    id uuid PRIMARY KEY,
    card_instance_id uuid NOT NULL REFERENCES card_instances(id) ON DELETE CASCADE,
    slot_name text NOT NULL,
    tool_call_id uuid NOT NULL REFERENCES tool_calls(id),
    result_ref text NOT NULL REFERENCES artifacts(result_ref),
    field_path text NOT NULL,
    output_schema_version text NOT NULL,
    source_hash bytea NOT NULL CHECK (octet_length(source_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (card_instance_id, slot_name)
);

CREATE TABLE card_query_binding_specs (
    id uuid PRIMARY KEY,
    card_instance_id uuid NOT NULL REFERENCES card_instances(id) ON DELETE CASCADE,
    slot_name text NOT NULL,
    tool_id text NOT NULL CHECK (tool_id ~ '^[a-z][a-z0-9_.-]+$'),
    fixed_input jsonb NOT NULL,
    input_hash bytea NOT NULL CHECK (octet_length(input_hash) = 32),
    output_schema_version text NOT NULL,
    schema_hash bytea NOT NULL CHECK (octet_length(schema_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (card_instance_id, slot_name)
);

CREATE TABLE card_presentations (
    id uuid PRIMARY KEY,
    card_instance_id uuid NOT NULL REFERENCES card_instances(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    viewer_user_id uuid NOT NULL,
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    locale text NOT NULL CHECK (locale IN ('zh-CN','en-US')),
    color_scheme text NOT NULL CHECK (color_scheme IN ('light','dark')),
    locale_fallback boolean NOT NULL DEFAULT false,
    initial_data jsonb NOT NULL,
    partial boolean NOT NULL DEFAULT false,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (viewer_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);

CREATE TABLE card_query_bindings (
    id uuid PRIMARY KEY,
    binding_ref text NOT NULL UNIQUE,
    presentation_id uuid NOT NULL REFERENCES card_presentations(id) ON DELETE CASCADE,
    binding_spec_id uuid NOT NULL REFERENCES card_query_binding_specs(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    viewer_user_id uuid NOT NULL,
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','invalidated')),
    expires_at timestamptz NOT NULL,
    last_invoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (viewer_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE INDEX card_query_bindings_expiry ON card_query_bindings (expires_at) WHERE status = 'active';

ALTER TABLE action_bindings
    ADD COLUMN card_instance_id uuid REFERENCES card_instances(id),
    ADD COLUMN conversation_id uuid REFERENCES conversations(id),
    ADD COLUMN authorization_version bigint,
    ADD COLUMN binding_source text NOT NULL DEFAULT 'text_fallback' CHECK (binding_source IN ('text_fallback','card'));
ALTER TABLE action_bindings ADD CONSTRAINT action_bindings_authorization_version_check
    CHECK (authorization_version IS NULL OR authorization_version > 0);

ALTER TABLE conversation_events DROP CONSTRAINT conversation_events_event_type_check;
ALTER TABLE conversation_events ADD CONSTRAINT conversation_events_event_type_check CHECK (event_type IN (
    'user_message','assistant_message','model_usage','tool_call_requested','tool_call_started','tool_call_result',
    'pending_action_created','user_confirmation','approval_update','execution_update','card_draft_created',
    'card_instance_created','card_presentation_invalidated','card_action_result','run_state_changed','context_compacted','agent_delta'
));

-- +goose Down

ALTER TABLE conversation_events DROP CONSTRAINT IF EXISTS conversation_events_event_type_check;
ALTER TABLE conversation_events ADD CONSTRAINT conversation_events_event_type_check CHECK (event_type IN (
    'user_message','assistant_message','model_usage','tool_call_requested','tool_call_started','tool_call_result',
    'pending_action_created','user_confirmation','approval_update','execution_update','card_action_result',
    'run_state_changed','context_compacted','agent_delta'
));
ALTER TABLE action_bindings DROP CONSTRAINT IF EXISTS action_bindings_authorization_version_check;
ALTER TABLE action_bindings DROP COLUMN IF EXISTS binding_source;
ALTER TABLE action_bindings DROP COLUMN IF EXISTS authorization_version;
ALTER TABLE action_bindings DROP COLUMN IF EXISTS conversation_id;
ALTER TABLE action_bindings DROP COLUMN IF EXISTS card_instance_id;
DROP TABLE IF EXISTS card_query_bindings;
DROP TABLE IF EXISTS card_presentations;
DROP TABLE IF EXISTS card_query_binding_specs;
DROP TABLE IF EXISTS card_data_sources;
DROP TABLE IF EXISTS card_instances;
DROP TABLE IF EXISTS card_validation_runs;
DROP TABLE IF EXISTS card_demo_scenarios;
DROP TABLE IF EXISTS card_slot_bindings;
ALTER TABLE interactive_cards DROP CONSTRAINT IF EXISTS interactive_cards_active_version_fk;
DROP TABLE IF EXISTS card_versions;
DROP TABLE IF EXISTS interactive_cards;
DELETE FROM role_permissions WHERE permission_id IN (
    'interactive_card.read','interactive_card.create','interactive_card.update','interactive_card.publish','interactive_card.deprecate'
);
DELETE FROM permissions WHERE registry_version = 4;
