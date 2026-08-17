-- +goose Up

INSERT INTO permissions (id, description, registry_version) VALUES
    ('conversation.read', 'Read conversations and immutable events', 3),
    ('conversation.use', 'Create messages and run the Model Agent', 3),
    ('model.read', 'Read enabled AI model metadata and own availability', 3),
    ('model.manage', 'Manage AI models and pricing', 3),
    ('model.quota.manage', 'Manage department and user model quotas', 3),
    ('model.usage.read', 'Read governed model usage', 3),
    ('approval_policy.read', 'Read approval policies', 3),
    ('approval_policy.manage', 'Manage approval policies', 3),
    ('approval.read', 'Read approval requests', 3),
    ('approval.decide', 'Approve or reject eligible requests', 3),
    ('execution.read', 'Read deterministic execution state', 3),
    ('automation.read', 'Read automations and runs', 3),
    ('automation.manage', 'Manage service-account automations', 3)
ON CONFLICT (id) DO UPDATE SET description = EXCLUDED.description, registry_version = EXCLUDED.registry_version;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id FROM roles CROSS JOIN permissions
WHERE roles.identity_key = 'enterprise_admin' AND permissions.registry_version = 3
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permission_id FROM roles CROSS JOIN unnest(ARRAY[
    'conversation.read','conversation.use','model.read','approval_policy.read','approval.read',
    'approval.decide','execution.read','automation.read'
]) AS permission_id
WHERE roles.identity_key IN ('resource_admin','resource_operator')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permission_id FROM roles CROSS JOIN unnest(ARRAY[
    'approval_policy.read','approval.read','approval.decide','execution.read'
]) AS permission_id
WHERE roles.identity_key = 'resource_approver'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permission_id FROM roles CROSS JOIN unnest(ARRAY[
    'conversation.read','conversation.use','model.read','execution.read'
]) AS permission_id
WHERE roles.identity_key = 'resource_viewer'
ON CONFLICT DO NOTHING;

UPDATE pending_actions
SET status = 'invalidated', error_code = 'ACTION_INVALIDATED', updated_at = now()
WHERE status IN ('prepared','awaiting_confirmation','executing');
ALTER TABLE pending_actions DROP CONSTRAINT pending_actions_status_check;
ALTER TABLE pending_actions RENAME COLUMN creator_user_id TO creator_subject_id;
ALTER TABLE pending_actions ADD COLUMN creator_subject_type text NOT NULL DEFAULT 'user'
    CHECK (creator_subject_type IN ('user','service_account'));
ALTER TABLE pending_actions ADD COLUMN run_id uuid;
ALTER TABLE pending_actions ADD COLUMN confirmation_required boolean NOT NULL DEFAULT true;
ALTER TABLE pending_actions ADD COLUMN policy_snapshot_hash bytea;
ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_policy_hash_check
    CHECK (policy_snapshot_hash IS NULL OR octet_length(policy_snapshot_hash) = 32);
ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_status_check CHECK (status IN (
    'prepared','awaiting_confirmation','awaiting_approval','ready','executing','succeeded','failed',
    'result_unknown','cancelled','expired','rejected','invalidated'
));

CREATE TABLE ai_models (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    base_url text NOT NULL CHECK (char_length(base_url) BETWEEN 1 AND 2048),
    model_id text NOT NULL CHECK (char_length(model_id) BETWEEN 1 AND 256),
    api_protocol text NOT NULL CHECK (api_protocol IN ('chat_completions','responses')),
    context_window_tokens integer NOT NULL CHECK (context_window_tokens >= 8192),
    max_output_tokens integer NOT NULL CHECK (max_output_tokens > 0 AND max_output_tokens < context_window_tokens),
    input_price_per_million numeric(20,8) NOT NULL CHECK (input_price_per_million >= 0),
    output_price_per_million numeric(20,8) NOT NULL CHECK (output_price_per_million >= 0),
    capabilities jsonb NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled','disabled')),
    health_status text NOT NULL DEFAULT 'unknown' CHECK (health_status IN ('unknown','healthy','unhealthy')),
    revision integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    last_tested_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX ai_models_name_unique ON ai_models (enterprise_id, lower(name));

CREATE TABLE ai_model_revisions (
    id uuid PRIMARY KEY,
    model_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    base_url text NOT NULL,
    provider_model_id text NOT NULL,
    api_protocol text NOT NULL CHECK (api_protocol IN ('chat_completions','responses')),
    context_window_tokens integer NOT NULL,
    max_output_tokens integer NOT NULL,
    input_price_per_million numeric(20,8) NOT NULL,
    output_price_per_million numeric(20,8) NOT NULL,
    capabilities jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (model_id, enterprise_id) REFERENCES ai_models(id, enterprise_id),
    UNIQUE (model_id, revision),
    UNIQUE (id, enterprise_id)
);

CREATE TABLE ai_model_credentials (
    id uuid PRIMARY KEY,
    model_revision_id uuid NOT NULL UNIQUE REFERENCES ai_model_revisions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    key_id text NOT NULL,
    key_version integer NOT NULL CHECK (key_version > 0),
    wrapped_dek bytea NOT NULL,
    wrap_nonce bytea NOT NULL,
    nonce bytea NOT NULL,
    ciphertext bytea NOT NULL,
    value_hash bytea NOT NULL CHECK (octet_length(value_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE model_compatibility_results (
    id uuid PRIMARY KEY,
    model_revision_id uuid NOT NULL REFERENCES ai_model_revisions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    compatible boolean NOT NULL,
    checks jsonb NOT NULL,
    error_code text,
    tested_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE conversations (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    owner_user_id uuid NOT NULL,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 256),
    selected_model_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (selected_model_id, enterprise_id) REFERENCES ai_models(id, enterprise_id),
    FOREIGN KEY (owner_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE INDEX conversations_owner_cursor ON conversations (enterprise_id, owner_user_id, updated_at DESC, id DESC);

CREATE TABLE runs (
    id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    model_id uuid NOT NULL,
    model_revision integer NOT NULL CHECK (model_revision > 0),
    locale text NOT NULL CHECK (locale IN ('zh-CN','en-US')),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','waiting_input','waiting_approval','waiting_system','succeeded','failed','cancelled','timed_out')),
    current_step_id uuid,
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    checkpoint jsonb NOT NULL DEFAULT '{}',
    stop_reason text,
    error_code text,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, enterprise_id) REFERENCES conversations(id, enterprise_id),
    FOREIGN KEY (model_id, enterprise_id) REFERENCES ai_models(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX runs_one_active_per_conversation ON runs (conversation_id)
WHERE status IN ('pending','running','waiting_input','waiting_approval','waiting_system');

ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_run_fk FOREIGN KEY (run_id) REFERENCES runs(id);

CREATE TABLE conversation_events (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    run_id uuid,
    step_id uuid,
    sequence bigint NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL CHECK (event_type IN (
        'user_message','assistant_message','model_usage','tool_call_requested','tool_call_started',
        'tool_call_result','pending_action_created','user_confirmation','approval_update',
        'execution_update','card_action_result','run_state_changed','context_compacted','agent_delta'
    )),
    actor_type text NOT NULL CHECK (actor_type IN ('user','model','service','worker','system')),
    actor_id text,
    payload jsonb NOT NULL,
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    artifact_ref text,
    data_classification text NOT NULL DEFAULT 'internal' CHECK (data_classification IN ('public','internal','sensitive')),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, enterprise_id) REFERENCES conversations(id, enterprise_id),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    UNIQUE (conversation_id, sequence),
    UNIQUE (id, enterprise_id)
);
CREATE INDEX conversation_events_cursor ON conversation_events (conversation_id, sequence);

CREATE TABLE run_steps (
    id uuid PRIMARY KEY,
    run_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    sequence integer NOT NULL CHECK (sequence > 0),
    step_type text NOT NULL CHECK (step_type IN ('model_call','tool_call','pending_action','execution','context_compaction','verification')),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','leased','running','waiting_input','waiting_approval','succeeded','failed','cancelled','timed_out')),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    UNIQUE (run_id, sequence),
    UNIQUE (id, enterprise_id)
);
ALTER TABLE runs ADD CONSTRAINT runs_current_step_fk FOREIGN KEY (current_step_id) REFERENCES run_steps(id);

CREATE TABLE runtime_tasks (
    id uuid PRIMARY KEY,
    enterprise_id uuid,
    queue text NOT NULL CHECK (queue IN ('agent','action','compaction','automation','sandbox')),
    run_id uuid,
    step_id uuid,
    payload jsonb NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','leased','running','succeeded','failed','cancelled','timed_out')),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    lease_owner text,
    lease_until timestamptz,
    fence_token bigint NOT NULL DEFAULT 0 CHECK (fence_token >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    last_error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    FOREIGN KEY (step_id, enterprise_id) REFERENCES run_steps(id, enterprise_id),
    CHECK ((run_id IS NULL AND step_id IS NULL) OR enterprise_id IS NOT NULL)
);
CREATE INDEX runtime_tasks_claim ON runtime_tasks (queue, available_at, created_at)
WHERE status = 'pending' OR (status IN ('leased','running') AND lease_until IS NOT NULL);

CREATE TABLE artifacts (
    id uuid PRIMARY KEY,
    result_ref text NOT NULL UNIQUE,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    conversation_id uuid,
    run_id uuid,
    content_type text NOT NULL,
    data_classification text NOT NULL CHECK (data_classification IN ('public','internal','sensitive')),
    content bytea NOT NULL CHECK (octet_length(content) <= 4194304),
    content_hash bytea NOT NULL CHECK (octet_length(content_hash) = 32),
    byte_size integer NOT NULL CHECK (byte_size >= 0 AND byte_size <= 4194304),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, enterprise_id) REFERENCES conversations(id, enterprise_id),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id)
);

CREATE TABLE tool_calls (
    id uuid PRIMARY KEY,
    call_id text NOT NULL UNIQUE,
    enterprise_id uuid NOT NULL,
    run_id uuid NOT NULL,
    step_id uuid NOT NULL,
    tool_id text NOT NULL CHECK (tool_id ~ '^[a-z][a-z0-9_.-]+$'),
    input jsonb NOT NULL,
    input_hash bytea NOT NULL CHECK (octet_length(input_hash) = 32),
    status text NOT NULL CHECK (status IN ('requested','running','succeeded','failed','cancelled')),
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    FOREIGN KEY (step_id, enterprise_id) REFERENCES run_steps(id, enterprise_id)
);

CREATE TABLE tool_results (
    id uuid PRIMARY KEY,
    tool_call_id uuid NOT NULL UNIQUE REFERENCES tool_calls(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    artifact_id uuid NOT NULL REFERENCES artifacts(id),
    projection jsonb NOT NULL,
    projection_hash bytea NOT NULL CHECK (octet_length(projection_hash) = 32),
    projection_bytes integer NOT NULL CHECK (projection_bytes >= 0 AND projection_bytes <= 65536),
    partial boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE context_snapshots (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    run_id uuid NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    source_from_sequence bigint NOT NULL CHECK (source_from_sequence > 0),
    source_through_sequence bigint NOT NULL CHECK (source_through_sequence >= source_from_sequence),
    first_kept_sequence bigint NOT NULL CHECK (first_kept_sequence > source_through_sequence),
    typed_checkpoint jsonb NOT NULL,
    narrative_summary text NOT NULL,
    compaction_model_id uuid NOT NULL,
    compaction_model_revision integer NOT NULL CHECK (compaction_model_revision > 0),
    prompt_version text NOT NULL,
    estimated_tokens_before integer NOT NULL CHECK (estimated_tokens_before >= 0),
    actual_tokens_after integer NOT NULL CHECK (actual_tokens_after >= 0),
    source_hash bytea NOT NULL CHECK (octet_length(source_hash) = 32),
    snapshot_hash bytea NOT NULL CHECK (octet_length(snapshot_hash) = 32),
    status text NOT NULL CHECK (status IN ('active','superseded','failed')),
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, enterprise_id) REFERENCES conversations(id, enterprise_id),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    UNIQUE (run_id, revision),
    UNIQUE (run_id, source_hash)
);
CREATE UNIQUE INDEX context_snapshots_one_active ON context_snapshots (run_id) WHERE status = 'active';

CREATE TABLE model_calls (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL,
    run_id uuid NOT NULL,
    step_id uuid NOT NULL,
    model_id uuid NOT NULL,
    model_revision integer NOT NULL,
    call_kind text NOT NULL CHECK (call_kind IN ('inference','compaction')),
    projection_hash bytea NOT NULL CHECK (octet_length(projection_hash) = 32),
    input_tokens bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    input_price_snapshot numeric(20,8) NOT NULL,
    output_price_snapshot numeric(20,8) NOT NULL,
    amount numeric(20,8) NOT NULL DEFAULT 0 CHECK (amount >= 0),
    latency_ms bigint NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    stop_reason text,
    status text NOT NULL CHECK (status IN ('reserved','running','succeeded','failed','cancelled')),
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    FOREIGN KEY (step_id, enterprise_id) REFERENCES run_steps(id, enterprise_id),
    FOREIGN KEY (model_id, enterprise_id) REFERENCES ai_models(id, enterprise_id)
);

CREATE TABLE model_quotas (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    model_id uuid NOT NULL,
    subject_type text NOT NULL CHECK (subject_type IN ('department','user')),
    subject_id uuid NOT NULL,
    monthly_amount numeric(20,8) NOT NULL CHECK (monthly_amount >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (model_id, enterprise_id) REFERENCES ai_models(id, enterprise_id),
    UNIQUE (enterprise_id, model_id, subject_type, subject_id)
);

CREATE TABLE model_quota_reservations (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    model_call_id uuid NOT NULL UNIQUE REFERENCES model_calls(id),
    model_id uuid NOT NULL,
    department_id uuid NOT NULL,
    user_id uuid NOT NULL,
    month date NOT NULL,
    reserved_amount numeric(20,8) NOT NULL CHECK (reserved_amount >= 0),
    settled_amount numeric(20,8),
    status text NOT NULL CHECK (status IN ('active','settled','released')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (model_id, enterprise_id) REFERENCES ai_models(id, enterprise_id)
);

CREATE TABLE approval_policies (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    enabled boolean NOT NULL DEFAULT true,
    tool_ids text[] NOT NULL DEFAULT '{}',
    risks text[] NOT NULL CHECK (cardinality(risks) > 0),
    resource_types text[] NOT NULL DEFAULT '{}',
    label_selector jsonb,
    minimum_approvers integer NOT NULL CHECK (minimum_approvers BETWEEN 1 AND 10),
    separation_of_duty boolean NOT NULL DEFAULT true,
    approver_role_ids uuid[] NOT NULL CHECK (cardinality(approver_role_ids) > 0),
    expires_after_seconds integer NOT NULL DEFAULT 86400 CHECK (expires_after_seconds BETWEEN 60 AND 604800),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX approval_policies_name_unique ON approval_policies (enterprise_id, lower(name));

CREATE TABLE user_confirmations (
    id uuid PRIMARY KEY,
    pending_action_id uuid NOT NULL UNIQUE REFERENCES pending_actions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    actor_user_id uuid NOT NULL,
    authorization_version bigint NOT NULL,
    confirmed_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (actor_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id)
);

CREATE TABLE approval_requests (
    id uuid PRIMARY KEY,
    pending_action_id uuid NOT NULL UNIQUE REFERENCES pending_actions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','expired','invalidated')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE approval_requirement_snapshots (
    id uuid PRIMARY KEY,
    approval_request_id uuid NOT NULL REFERENCES approval_requests(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    policy_id uuid NOT NULL,
    policy_version bigint NOT NULL,
    minimum_approvers integer NOT NULL CHECK (minimum_approvers BETWEEN 1 AND 10),
    separation_of_duty boolean NOT NULL,
    approver_role_ids uuid[] NOT NULL,
    approved_count integer NOT NULL DEFAULT 0 CHECK (approved_count >= 0),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','invalidated')),
    policy_hash bytea NOT NULL CHECK (octet_length(policy_hash) = 32),
    UNIQUE (approval_request_id, policy_id)
);

CREATE TABLE approval_decisions (
    id uuid PRIMARY KEY,
    approval_request_id uuid NOT NULL REFERENCES approval_requests(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    actor_user_id uuid NOT NULL,
    decision text NOT NULL CHECK (decision IN ('approved','rejected')),
    reason text NOT NULL DEFAULT '',
    decided_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (actor_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    UNIQUE (approval_request_id, actor_user_id)
);

CREATE TABLE action_bindings (
    id uuid PRIMARY KEY,
    binding_ref text NOT NULL UNIQUE,
    pending_action_id uuid NOT NULL REFERENCES pending_actions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    actor_user_id uuid,
    action text NOT NULL CHECK (action IN ('confirm','cancel','approve','reject')),
    request_id text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','consumed','cancelled','expired','invalidated')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id),
    UNIQUE (pending_action_id, request_id)
);

CREATE TABLE executions (
    id uuid PRIMARY KEY,
    execution_ref text NOT NULL UNIQUE,
    pending_action_id uuid NOT NULL UNIQUE REFERENCES pending_actions(id),
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    run_id uuid,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed','result_unknown','cancelled')),
    idempotency_key text NOT NULL,
    result_ref text,
    connector_command_id uuid,
    error_code text,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (run_id, enterprise_id) REFERENCES runs(id, enterprise_id),
    FOREIGN KEY (connector_command_id) REFERENCES connector_commands(id),
    UNIQUE (enterprise_id, idempotency_key),
    UNIQUE (id, enterprise_id)
);

CREATE TABLE execution_one_time_results (
    id uuid PRIMARY KEY,
    execution_id uuid NOT NULL,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    result_kind text NOT NULL CHECK (result_kind IN ('connector_enrollment')),
    key_version integer NOT NULL CHECK (key_version > 0),
    nonce bytea NOT NULL,
    ciphertext bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_by_user_id uuid,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (execution_id, enterprise_id) REFERENCES executions(id, enterprise_id),
    FOREIGN KEY (consumed_by_user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id),
    UNIQUE (execution_id),
    CHECK ((consumed_by_user_id IS NULL) = (consumed_at IS NULL))
);

CREATE TABLE automations (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    service_account_id uuid NOT NULL,
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    tool_id text NOT NULL CHECK (tool_id ~ '^[a-z][a-z0-9_.-]+$'),
    tool_input jsonb NOT NULL,
    cron text NOT NULL CHECK (char_length(cron) BETWEEN 9 AND 128),
    timezone text NOT NULL CHECK (char_length(timezone) BETWEEN 1 AND 128),
    status text NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled','disabled')),
    next_run_at timestamptz NOT NULL,
    revision integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (service_account_id, enterprise_id) REFERENCES service_accounts(id, enterprise_id),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX automations_name_unique ON automations (enterprise_id, lower(name));
CREATE INDEX automations_due ON automations (next_run_at, id) WHERE status = 'enabled';

CREATE TABLE automation_revisions (
    automation_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    service_account_id uuid NOT NULL,
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    tool_id text NOT NULL CHECK (tool_id ~ '^[a-z][a-z0-9_.-]+$'),
    tool_input jsonb NOT NULL,
    cron text NOT NULL CHECK (char_length(cron) BETWEEN 9 AND 128),
    timezone text NOT NULL CHECK (char_length(timezone) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (automation_id, revision),
    FOREIGN KEY (automation_id, enterprise_id) REFERENCES automations(id, enterprise_id),
    FOREIGN KEY (service_account_id, enterprise_id) REFERENCES service_accounts(id, enterprise_id),
    UNIQUE (automation_id, enterprise_id, revision)
);

CREATE TABLE automation_runs (
    id uuid PRIMARY KEY,
    automation_id uuid NOT NULL,
    enterprise_id uuid NOT NULL,
    automation_revision integer NOT NULL CHECK (automation_revision > 0),
    scheduled_for timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','waiting_approval','succeeded','failed','skipped','cancelled')),
    task_id uuid,
    pending_action_id uuid,
    result_ref text,
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (automation_id, enterprise_id) REFERENCES automations(id, enterprise_id),
    FOREIGN KEY (automation_id, enterprise_id, automation_revision) REFERENCES automation_revisions(automation_id, enterprise_id, revision),
    FOREIGN KEY (task_id) REFERENCES runtime_tasks(id),
    FOREIGN KEY (pending_action_id) REFERENCES pending_actions(id),
    UNIQUE (automation_id, scheduled_for),
    UNIQUE (id, enterprise_id)
);
CREATE UNIQUE INDEX automation_runs_one_active ON automation_runs (automation_id)
WHERE status IN ('pending','running','waiting_approval');

CREATE TABLE sandbox_backends (
    id uuid PRIMARY KEY,
    name text NOT NULL UNIQUE CHECK (char_length(name) BETWEEN 1 AND 128),
    endpoint text NOT NULL CHECK (char_length(endpoint) BETWEEN 1 AND 2048),
    credential_provider text,
    credential_key_id text,
    credential_key_version integer,
    credential_wrapped_dek bytea,
    credential_wrap_nonce bytea,
    credential_nonce bytea,
    credential_ciphertext bytea,
    credential_value_hash bytea,
    status text NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled','disabled')),
    health_status text NOT NULL DEFAULT 'unknown' CHECK (health_status IN ('unknown','healthy','unhealthy')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
    ,CHECK ((credential_ciphertext IS NULL) = (credential_key_version IS NULL))
);

CREATE TABLE sandbox_images (
    id uuid PRIMARY KEY,
    backend_id uuid NOT NULL REFERENCES sandbox_backends(id),
    name text NOT NULL,
    image_ref text NOT NULL,
    digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    status text NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled','disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (backend_id, name),
    UNIQUE (backend_id, digest)
);

CREATE TABLE sandbox_profiles (
    id uuid PRIMARY KEY,
    name text NOT NULL UNIQUE,
    backend_id uuid NOT NULL REFERENCES sandbox_backends(id),
    image_id uuid NOT NULL REFERENCES sandbox_images(id),
    task_kinds text[] NOT NULL CHECK (cardinality(task_kinds) > 0),
    cpu_millis integer NOT NULL CHECK (cpu_millis BETWEEN 100 AND 16000),
    memory_mib integer NOT NULL CHECK (memory_mib BETWEEN 128 AND 65536),
    timeout_seconds integer NOT NULL CHECK (timeout_seconds BETWEEN 10 AND 3600),
    network_mode text NOT NULL CHECK (network_mode IN ('none','restricted')),
    status text NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled','disabled')),
    revision integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sandbox_quotas (
    enterprise_id uuid PRIMARY KEY REFERENCES enterprises(id),
    max_concurrent_sessions integer NOT NULL CHECK (max_concurrent_sessions BETWEEN 0 AND 10000),
    monthly_session_seconds bigint NOT NULL CHECK (monthly_session_seconds >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sandbox_sessions (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    task_id uuid NOT NULL UNIQUE REFERENCES runtime_tasks(id),
    profile_id uuid NOT NULL REFERENCES sandbox_profiles(id),
    profile_revision integer NOT NULL CHECK (profile_revision > 0),
    upstream_session_id text NOT NULL UNIQUE,
    status text NOT NULL CHECK (status IN ('creating','running','terminating','terminated','failed','unknown')),
    expires_at timestamptz NOT NULL,
    started_at timestamptz,
    terminated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sandbox_sessions_active ON sandbox_sessions (enterprise_id, expires_at)
WHERE status IN ('creating','running','terminating','unknown');

CREATE TABLE sandbox_usage (
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    month date NOT NULL,
    session_count bigint NOT NULL DEFAULT 0 CHECK (session_count >= 0),
    session_seconds bigint NOT NULL DEFAULT 0 CHECK (session_seconds >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (enterprise_id, month)
);

-- +goose Down

DROP TABLE IF EXISTS sandbox_usage;
DROP TABLE IF EXISTS sandbox_sessions;
DROP TABLE IF EXISTS sandbox_quotas;
DROP TABLE IF EXISTS sandbox_profiles;
DROP TABLE IF EXISTS sandbox_images;
DROP TABLE IF EXISTS sandbox_backends;
DROP TABLE IF EXISTS automation_runs;
DROP TABLE IF EXISTS automation_revisions;
DROP TABLE IF EXISTS automations;
DROP TABLE IF EXISTS execution_one_time_results;
DROP TABLE IF EXISTS executions;
DROP TABLE IF EXISTS action_bindings;
DROP TABLE IF EXISTS approval_decisions;
DROP TABLE IF EXISTS approval_requirement_snapshots;
DROP TABLE IF EXISTS approval_requests;
DROP TABLE IF EXISTS user_confirmations;
DROP TABLE IF EXISTS approval_policies;
DROP TABLE IF EXISTS model_quota_reservations;
DROP TABLE IF EXISTS model_quotas;
DROP TABLE IF EXISTS model_calls;
DROP TABLE IF EXISTS context_snapshots;
DROP TABLE IF EXISTS tool_results;
DROP TABLE IF EXISTS tool_calls;
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS runtime_tasks;
ALTER TABLE runs DROP CONSTRAINT IF EXISTS runs_current_step_fk;
DROP TABLE IF EXISTS run_steps;
ALTER TABLE pending_actions DROP CONSTRAINT IF EXISTS pending_actions_run_fk;
DROP TABLE IF EXISTS conversation_events;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS model_compatibility_results;
DROP TABLE IF EXISTS ai_model_credentials;
DROP TABLE IF EXISTS ai_model_revisions;
DROP TABLE IF EXISTS ai_models;
ALTER TABLE pending_actions DROP CONSTRAINT IF EXISTS pending_actions_status_check;
ALTER TABLE pending_actions DROP CONSTRAINT IF EXISTS pending_actions_policy_hash_check;
ALTER TABLE pending_actions DROP COLUMN IF EXISTS policy_snapshot_hash;
ALTER TABLE pending_actions DROP COLUMN IF EXISTS confirmation_required;
ALTER TABLE pending_actions DROP COLUMN IF EXISTS run_id;
ALTER TABLE pending_actions DROP COLUMN IF EXISTS creator_subject_type;
ALTER TABLE pending_actions RENAME COLUMN creator_subject_id TO creator_user_id;
UPDATE pending_actions SET status = 'expired' WHERE status NOT IN ('prepared','awaiting_confirmation','executing','succeeded','failed','cancelled','expired','invalidated');
ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_status_check CHECK (status IN ('prepared','awaiting_confirmation','executing','succeeded','failed','cancelled','expired','invalidated'));
DELETE FROM role_permissions WHERE permission_id IN (
    'conversation.read','conversation.use','model.read','model.manage','model.quota.manage','model.usage.read',
    'approval_policy.read','approval_policy.manage','approval.read','approval.decide','execution.read','automation.read','automation.manage'
);
DELETE FROM permissions WHERE registry_version = 3;
