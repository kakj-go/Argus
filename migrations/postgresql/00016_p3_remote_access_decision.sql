-- +goose Up

-- Task 02 runtime snapshot contract. The legacy policy table remains present
-- until the generated compatibility API is removed in the same release.
ALTER TABLE remote_access_requests
    DROP CONSTRAINT IF EXISTS remote_access_requests_status_check;
ALTER TABLE remote_access_requests
    ADD CONSTRAINT remote_access_requests_status_check CHECK (status IN ('requested','awaiting_mfa','awaiting_approval','authorized','rejected','expired','invalidated'));
ALTER TABLE remote_access_requests
    ADD COLUMN IF NOT EXISTS decision_outcome text,
    ADD COLUMN IF NOT EXISTS decision_reason_codes jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS decision_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS decision_snapshot_hash bytea,
    ADD COLUMN IF NOT EXISTS matched_grant_snapshots jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS matched_rule_snapshots jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS decision_at timestamptz;
ALTER TABLE remote_access_requests
    ADD CONSTRAINT remote_access_request_snapshot_hash_ck CHECK (decision_snapshot_hash IS NULL OR octet_length(decision_snapshot_hash) = 32),
    ADD CONSTRAINT remote_access_request_decision_snapshot_ck CHECK (jsonb_typeof(decision_snapshot) = 'object');

ALTER TABLE remote_access_requirement_snapshots
    ADD COLUMN IF NOT EXISTS approval_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS deadline_at timestamptz,
    ADD COLUMN IF NOT EXISTS escalated_at timestamptz,
    ADD COLUMN IF NOT EXISTS timeout_effect text NOT NULL DEFAULT 'expire' CHECK (timeout_effect IN ('reject','expire')),
    ADD COLUMN IF NOT EXISTS escalation_role_ids uuid[] NOT NULL DEFAULT '{}';
ALTER TABLE remote_access_requirement_snapshots
    DROP CONSTRAINT IF EXISTS remote_access_requirement_snapshots_max_session_seconds_check,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_snapshots_idle_timeout_seconds_check;
ALTER TABLE remote_access_requirement_snapshots
    ADD CONSTRAINT remote_access_requirement_snapshots_max_session_seconds_check CHECK (max_session_seconds BETWEEN 60 AND 86400),
    ADD CONSTRAINT remote_access_requirement_snapshots_idle_timeout_seconds_check CHECK (idle_timeout_seconds BETWEEN 60 AND max_session_seconds);
CREATE UNIQUE INDEX IF NOT EXISTS remote_access_requirement_workflow_unique
    ON remote_access_requirement_snapshots (request_id, workflow_id) WHERE workflow_id IS NOT NULL;

ALTER TABLE remote_access_leases
    ADD COLUMN IF NOT EXISTS decision_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS session_profile_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS decision_snapshot_hash bytea;
ALTER TABLE remote_access_leases
    ADD CONSTRAINT remote_access_lease_snapshot_hash_ck CHECK (decision_snapshot_hash IS NULL OR octet_length(decision_snapshot_hash) = 32),
    ADD CONSTRAINT remote_access_lease_decision_snapshot_ck CHECK (jsonb_typeof(decision_snapshot) = 'object'),
    ADD CONSTRAINT remote_access_lease_profile_snapshot_ck CHECK (jsonb_typeof(session_profile_snapshot) = 'object');

ALTER TABLE remote_access_sessions
    ADD COLUMN IF NOT EXISTS decision_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS session_profile_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS decision_snapshot_hash bytea,
    ADD COLUMN IF NOT EXISTS recording_mode text NOT NULL DEFAULT 'required' CHECK (recording_mode IN ('required','optional','disabled')),
    ADD COLUMN IF NOT EXISTS command_audit_mode text NOT NULL DEFAULT 'required' CHECK (command_audit_mode IN ('required','optional','disabled')),
    ADD COLUMN IF NOT EXISTS clipboard_mode text NOT NULL DEFAULT 'disabled' CHECK (clipboard_mode IN ('enabled','disabled')),
    ADD COLUMN IF NOT EXISTS file_upload_mode text NOT NULL DEFAULT 'disabled' CHECK (file_upload_mode IN ('enabled','disabled')),
    ADD COLUMN IF NOT EXISTS file_download_mode text NOT NULL DEFAULT 'disabled' CHECK (file_download_mode IN ('enabled','disabled')),
    ADD COLUMN IF NOT EXISTS port_forward_mode text NOT NULL DEFAULT 'disabled' CHECK (port_forward_mode IN ('enabled','disabled')),
    ADD COLUMN IF NOT EXISTS session_share_mode text NOT NULL DEFAULT 'disabled' CHECK (session_share_mode IN ('enabled','disabled')),
    ADD COLUMN IF NOT EXISTS retention_days integer NOT NULL DEFAULT 90 CHECK (retention_days BETWEEN 1 AND 3650);
ALTER TABLE remote_access_sessions
    ADD CONSTRAINT remote_access_session_snapshot_hash_ck CHECK (decision_snapshot_hash IS NULL OR octet_length(decision_snapshot_hash) = 32),
    ADD CONSTRAINT remote_access_session_decision_snapshot_ck CHECK (jsonb_typeof(decision_snapshot) = 'object'),
    ADD CONSTRAINT remote_access_session_profile_snapshot_ck CHECK (jsonb_typeof(session_profile_snapshot) = 'object');

CREATE INDEX IF NOT EXISTS remote_access_requests_expiry ON remote_access_requests (enterprise_id, status, expires_at);
CREATE INDEX IF NOT EXISTS remote_access_requirements_deadline ON remote_access_requirement_snapshots (deadline_at, status);

-- +goose Down
DROP INDEX IF EXISTS remote_access_requirements_deadline;
DROP INDEX IF EXISTS remote_access_requirement_workflow_unique;
DROP INDEX IF EXISTS remote_access_requests_expiry;
ALTER TABLE remote_access_sessions
    DROP CONSTRAINT IF EXISTS remote_access_session_snapshot_hash_ck,
    DROP CONSTRAINT IF EXISTS remote_access_session_decision_snapshot_ck,
    DROP CONSTRAINT IF EXISTS remote_access_session_profile_snapshot_ck,
    DROP COLUMN IF EXISTS decision_snapshot,
    DROP COLUMN IF EXISTS session_profile_snapshot,
    DROP COLUMN IF EXISTS decision_snapshot_hash,
    DROP COLUMN IF EXISTS recording_mode,
    DROP COLUMN IF EXISTS command_audit_mode,
    DROP COLUMN IF EXISTS clipboard_mode,
    DROP COLUMN IF EXISTS file_upload_mode,
    DROP COLUMN IF EXISTS file_download_mode,
    DROP COLUMN IF EXISTS port_forward_mode,
    DROP COLUMN IF EXISTS session_share_mode,
    DROP COLUMN IF EXISTS retention_days;
ALTER TABLE remote_access_leases
    DROP CONSTRAINT IF EXISTS remote_access_lease_snapshot_hash_ck,
    DROP CONSTRAINT IF EXISTS remote_access_lease_decision_snapshot_ck,
    DROP CONSTRAINT IF EXISTS remote_access_lease_profile_snapshot_ck,
    DROP COLUMN IF EXISTS decision_snapshot,
    DROP COLUMN IF EXISTS session_profile_snapshot,
    DROP COLUMN IF EXISTS decision_snapshot_hash;
ALTER TABLE remote_access_requirement_snapshots
    DROP CONSTRAINT IF EXISTS remote_access_requirement_snapshots_max_session_seconds_check,
    DROP CONSTRAINT IF EXISTS remote_access_requirement_snapshots_idle_timeout_seconds_check;
ALTER TABLE remote_access_requirement_snapshots
    ADD CONSTRAINT remote_access_requirement_snapshots_max_session_seconds_check CHECK (max_session_seconds BETWEEN 60 AND 3600),
    ADD CONSTRAINT remote_access_requirement_snapshots_idle_timeout_seconds_check CHECK (idle_timeout_seconds BETWEEN 60 AND 900);
ALTER TABLE remote_access_requirement_snapshots
    DROP COLUMN IF EXISTS approval_snapshot,
    DROP COLUMN IF EXISTS deadline_at,
    DROP COLUMN IF EXISTS escalated_at,
    DROP COLUMN IF EXISTS timeout_effect,
    DROP COLUMN IF EXISTS escalation_role_ids;
ALTER TABLE remote_access_requests
    DROP CONSTRAINT IF EXISTS remote_access_request_snapshot_hash_ck,
    DROP CONSTRAINT IF EXISTS remote_access_request_decision_snapshot_ck,
    DROP COLUMN IF EXISTS decision_outcome,
    DROP COLUMN IF EXISTS decision_reason_codes,
    DROP COLUMN IF EXISTS decision_snapshot,
    DROP COLUMN IF EXISTS decision_snapshot_hash,
    DROP COLUMN IF EXISTS matched_grant_snapshots,
    DROP COLUMN IF EXISTS matched_rule_snapshots,
    DROP COLUMN IF EXISTS decision_at;
ALTER TABLE remote_access_requests
    DROP CONSTRAINT IF EXISTS remote_access_requests_status_check;
ALTER TABLE remote_access_requests
    ADD CONSTRAINT remote_access_requests_status_check CHECK (status IN ('requested','awaiting_approval','authorized','rejected','expired','invalidated'));
