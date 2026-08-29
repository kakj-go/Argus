-- +goose Up

INSERT INTO permissions (id, description, registry_version) VALUES
    ('remote_access.rule.read', 'Read remote access rules', 7),
    ('remote_access.rule.manage', 'Manage remote access rules', 7),
    ('remote_access.workflow.read', 'Read remote access approval workflows', 7),
    ('remote_access.workflow.manage', 'Manage remote access approval workflows', 7),
    ('remote_access.session_profile.read', 'Read remote access session profiles', 7),
    ('remote_access.session_profile.manage', 'Manage remote access session profiles', 7),
    ('remote_access.governance.references.read', 'Read remote access governance references', 7)
ON CONFLICT (id) DO UPDATE SET description = EXCLUDED.description, registry_version = EXCLUDED.registry_version;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id
FROM roles CROSS JOIN permissions
WHERE roles.identity_key = 'enterprise_admin' AND permissions.registry_version = 7
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id
FROM roles CROSS JOIN permissions
WHERE roles.identity_key = 'resource_admin' AND permissions.registry_version = 7
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id
FROM roles CROSS JOIN permissions
WHERE roles.identity_key IN ('resource_operator', 'resource_viewer', 'resource_approver')
  AND permissions.id IN ('remote_access.rule.read', 'remote_access.workflow.read', 'remote_access.session_profile.read')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT roles.id, permissions.id
FROM roles CROSS JOIN permissions
WHERE roles.identity_key IN ('resource_operator', 'resource_approver')
  AND permissions.id = 'remote_access.governance.references.read'
ON CONFLICT DO NOTHING;

CREATE TABLE remote_access_approval_workflows (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2048),
    approver_role_ids uuid[] NOT NULL CHECK (cardinality(approver_role_ids) > 0 AND cardinality(approver_role_ids) <= 64),
    minimum_approvals integer NOT NULL CHECK (minimum_approvals BETWEEN 1 AND 16),
    separation_of_duties boolean NOT NULL DEFAULT true,
    approval_timeout_seconds integer NOT NULL CHECK (approval_timeout_seconds BETWEEN 60 AND 604800),
    timeout_effect text NOT NULL CHECK (timeout_effect IN ('reject','expire')),
    escalation_role_ids uuid[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','enabled','disabled','archived')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id),
    CHECK (minimum_approvals <= cardinality(approver_role_ids))
);

CREATE TABLE remote_access_session_profiles (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2048),
    max_session_seconds integer NOT NULL CHECK (max_session_seconds BETWEEN 60 AND 86400),
    idle_timeout_seconds integer NOT NULL CHECK (idle_timeout_seconds BETWEEN 60 AND 86400),
    recording_mode text NOT NULL CHECK (recording_mode IN ('required','optional','disabled')),
    command_audit_mode text NOT NULL CHECK (command_audit_mode IN ('required','optional','disabled')),
    clipboard_mode text NOT NULL CHECK (clipboard_mode IN ('enabled','disabled')),
    file_upload_mode text NOT NULL CHECK (file_upload_mode IN ('enabled','disabled')),
    file_download_mode text NOT NULL CHECK (file_download_mode IN ('enabled','disabled')),
    port_forward_mode text NOT NULL CHECK (port_forward_mode IN ('enabled','disabled')),
    session_share_mode text NOT NULL CHECK (session_share_mode IN ('enabled','disabled')),
    retention_days integer NOT NULL CHECK (retention_days BETWEEN 1 AND 3650),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','enabled','disabled','archived')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id),
    CHECK (idle_timeout_seconds <= max_session_seconds)
);

CREATE TABLE remote_access_rules (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2048),
    priority integer NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 10000),
    subject_selector jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(subject_selector) = 'object'),
    host_selector jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(host_selector) = 'object'),
    managed_account_selector jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(managed_account_selector) = 'object'),
    protocols text[] NOT NULL CHECK (cardinality(protocols) > 0 AND protocols <@ ARRAY['ssh','winrs']::text[]),
    actions text[] NOT NULL DEFAULT ARRAY['terminal']::text[] CHECK (actions = ARRAY['terminal']::text[]),
    source_cidrs text[] NOT NULL DEFAULT '{}' CHECK (cardinality(source_cidrs) <= 64),
    time_windows jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(time_windows) = 'array'),
    effects text[] NOT NULL CHECK (cardinality(effects) > 0 AND effects <@ ARRAY['allow','deny','require_mfa','require_approval','notify']::text[]),
    approval_workflow_id uuid,
    session_profile_id uuid,
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','enabled','disabled','archived')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, enterprise_id),
    FOREIGN KEY (approval_workflow_id, enterprise_id) REFERENCES remote_access_approval_workflows(id, enterprise_id),
    FOREIGN KEY (session_profile_id, enterprise_id) REFERENCES remote_access_session_profiles(id, enterprise_id),
    CHECK (NOT ('deny' = ANY(effects)) OR cardinality(effects) = 1),
    CHECK (NOT ('require_approval' = ANY(effects)) OR approval_workflow_id IS NOT NULL),
    CHECK (NOT ('deny' = ANY(effects)) OR (cardinality(effects) = 1 AND approval_workflow_id IS NULL AND session_profile_id IS NULL))
);
CREATE UNIQUE INDEX remote_access_workflows_name_unique ON remote_access_approval_workflows (enterprise_id, lower(name));
CREATE UNIQUE INDEX remote_access_profiles_name_unique ON remote_access_session_profiles (enterprise_id, lower(name));
CREATE UNIQUE INDEX remote_access_rules_name_unique ON remote_access_rules (enterprise_id, lower(name));
CREATE INDEX remote_access_rules_match ON remote_access_rules (enterprise_id, status, priority, id);
CREATE INDEX remote_access_rules_host_selector_gin ON remote_access_rules USING gin (host_selector jsonb_path_ops);

ALTER TABLE remote_access_requirement_snapshots
    ADD COLUMN rule_id uuid,
    ADD COLUMN rule_version bigint,
    ADD COLUMN workflow_id uuid,
    ADD COLUMN workflow_version bigint,
    ADD COLUMN session_profile_id uuid,
    ADD COLUMN session_profile_version bigint;
ALTER TABLE remote_access_requirement_snapshots
    ALTER COLUMN policy_id DROP NOT NULL,
    ALTER COLUMN policy_version DROP NOT NULL;
ALTER TABLE remote_access_requirement_snapshots
    ADD CONSTRAINT remote_access_requirement_rule_fk FOREIGN KEY (rule_id) REFERENCES remote_access_rules(id),
    ADD CONSTRAINT remote_access_requirement_workflow_fk FOREIGN KEY (workflow_id) REFERENCES remote_access_approval_workflows(id),
    ADD CONSTRAINT remote_access_requirement_profile_fk FOREIGN KEY (session_profile_id) REFERENCES remote_access_session_profiles(id),
    ADD CONSTRAINT remote_access_requirement_rule_version_ck CHECK (rule_version IS NULL OR rule_version > 0),
    ADD CONSTRAINT remote_access_requirement_workflow_version_ck CHECK (workflow_version IS NULL OR workflow_version > 0),
    ADD CONSTRAINT remote_access_requirement_profile_version_ck CHECK (session_profile_version IS NULL OR session_profile_version > 0);
ALTER TABLE remote_access_requirement_snapshots
    ADD CONSTRAINT remote_access_requirement_source_ck CHECK (policy_id IS NOT NULL OR rule_id IS NOT NULL);

-- Development-stage conversion: every legacy policy becomes one rule, workflow and profile.
WITH migrated_policies AS (
    SELECT policy.*,
        COALESCE(
            (SELECT array_agg(DISTINCT role_id ORDER BY role_id)
             FROM unnest(policy.approver_role_ids) AS role_id
             JOIN roles role ON role.id = role_id
                 AND role.enterprise_id = policy.enterprise_id
                 AND role.status = 'active'),
            (SELECT ARRAY[role.id] FROM roles role
             WHERE role.enterprise_id = policy.enterprise_id
               AND role.identity_key = 'enterprise_admin'
               AND role.status = 'active'
             ORDER BY role.id LIMIT 1)
        )::uuid[] AS role_ids
    FROM remote_access_policies policy
)
INSERT INTO remote_access_approval_workflows (id, enterprise_id, name, description, approver_role_ids, minimum_approvals,
    separation_of_duties, approval_timeout_seconds, timeout_effect, status, created_by)
SELECT md5(policy.id::text || ':workflow')::uuid, policy.enterprise_id,
    left(policy.name, 100) || ' workflow ' || substr(md5(policy.id::text), 1, 16),
    'Migrated from RemoteAccessPolicy ' || policy.id,
    policy.role_ids,
    GREATEST(1, LEAST(policy.minimum_approvals, cardinality(policy.role_ids))),
    policy.separation_of_duties, 3600, 'expire', CASE WHEN policy.enabled THEN 'enabled' ELSE 'disabled' END, policy.created_by
FROM migrated_policies policy;
INSERT INTO remote_access_session_profiles (id, enterprise_id, name, description, max_session_seconds, idle_timeout_seconds,
    recording_mode, command_audit_mode, clipboard_mode, file_upload_mode, file_download_mode, port_forward_mode,
    session_share_mode, retention_days, status, created_by)
SELECT md5(policy.id::text || ':profile')::uuid, policy.enterprise_id,
    left(policy.name, 100) || ' profile ' || substr(md5(policy.id::text), 1, 16),
    'Migrated from RemoteAccessPolicy ' || policy.id, policy.max_session_seconds,
    LEAST(policy.idle_timeout_seconds, policy.max_session_seconds),
    'required', 'required', 'disabled', 'disabled', 'disabled', 'disabled', 'disabled', 90,
    CASE WHEN policy.enabled THEN 'enabled' ELSE 'disabled' END, policy.created_by
FROM remote_access_policies policy;
INSERT INTO remote_access_rules (id, enterprise_id, name, description, priority, protocols, host_selector, effects,
    approval_workflow_id, session_profile_id, status, created_by)
SELECT md5(policy.id::text || ':rule')::uuid, policy.enterprise_id,
    left(policy.name, 100) || ' rule ' || substr(md5(policy.id::text), 1, 16),
    'Migrated from RemoteAccessPolicy ' || policy.id, policy.priority, policy.protocols, policy.host_selector,
    CASE WHEN policy.require_mfa THEN ARRAY['require_mfa','require_approval']::text[] ELSE ARRAY['require_approval']::text[] END,
    md5(policy.id::text || ':workflow')::uuid, md5(policy.id::text || ':profile')::uuid,
    CASE WHEN policy.enabled THEN 'enabled' ELSE 'disabled' END, policy.created_by
FROM remote_access_policies policy;

-- Backfill governance references for already-created M6 requirement snapshots.
-- Snapshots whose legacy policy predates the projection remain policy-only and
-- continue to be readable through the compatibility path.
UPDATE remote_access_requirement_snapshots snapshot
SET rule_id = rule.id,
    rule_version = rule.version,
    workflow_id = workflow.id,
    workflow_version = workflow.version,
    session_profile_id = profile.id,
    session_profile_version = profile.version
FROM remote_access_policies policy
JOIN remote_access_rules rule
  ON rule.id = md5(policy.id::text || ':rule')::uuid
 AND rule.enterprise_id = policy.enterprise_id
JOIN remote_access_approval_workflows workflow
  ON workflow.id = md5(policy.id::text || ':workflow')::uuid
 AND workflow.enterprise_id = policy.enterprise_id
JOIN remote_access_session_profiles profile
  ON profile.id = md5(policy.id::text || ':profile')::uuid
 AND profile.enterprise_id = policy.enterprise_id
WHERE snapshot.policy_id = policy.id
  AND snapshot.rule_id IS NULL;

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (
    'remote_access.rule.read', 'remote_access.rule.manage',
    'remote_access.workflow.read', 'remote_access.workflow.manage',
    'remote_access.session_profile.read', 'remote_access.session_profile.manage',
    'remote_access.governance.references.read'
);
DELETE FROM permissions
WHERE id IN (
    'remote_access.rule.read', 'remote_access.rule.manage',
    'remote_access.workflow.read', 'remote_access.workflow.manage',
    'remote_access.session_profile.read', 'remote_access.session_profile.manage',
    'remote_access.governance.references.read'
);
ALTER TABLE remote_access_requirement_snapshots
    DROP CONSTRAINT IF EXISTS remote_access_requirement_rule_fk,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_workflow_fk,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_profile_fk,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_rule_version_ck,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_workflow_version_ck,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_profile_version_ck,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_source_ck,
    DROP COLUMN IF EXISTS rule_id,
    DROP COLUMN IF EXISTS rule_version,
    DROP COLUMN IF EXISTS workflow_id,
    DROP COLUMN IF EXISTS workflow_version,
    DROP COLUMN IF EXISTS session_profile_id,
    DROP COLUMN IF EXISTS session_profile_version;
ALTER TABLE remote_access_requirement_snapshots
    ALTER COLUMN policy_id SET NOT NULL,
    ALTER COLUMN policy_version SET NOT NULL;
DROP TABLE IF EXISTS remote_access_rules;
DROP TABLE IF EXISTS remote_access_session_profiles;
DROP TABLE IF EXISTS remote_access_approval_workflows;
