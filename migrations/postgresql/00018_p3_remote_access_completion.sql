-- +goose Up

ALTER TABLE remote_access_approval_workflows
    ADD COLUMN escalation_after_seconds integer;

UPDATE remote_access_approval_workflows
SET escalation_after_seconds = GREATEST(30, approval_timeout_seconds / 2);

ALTER TABLE remote_access_approval_workflows
    ALTER COLUMN escalation_after_seconds SET NOT NULL,
    ALTER COLUMN escalation_after_seconds SET DEFAULT 30,
    ADD CONSTRAINT remote_access_workflow_escalation_window_ck
        CHECK (escalation_after_seconds >= 30 AND escalation_after_seconds < approval_timeout_seconds);

ALTER TABLE remote_access_requirement_snapshots
    ADD COLUMN escalation_at timestamptz;

UPDATE remote_access_requirement_snapshots requirement
SET escalation_at = requirement.created_at + make_interval(secs => workflow.escalation_after_seconds)
FROM remote_access_approval_workflows workflow
WHERE workflow.id = requirement.workflow_id;

ALTER TABLE remote_access_requirement_snapshots
    ALTER COLUMN escalation_at SET NOT NULL,
    ADD CONSTRAINT remote_access_requirement_escalation_window_ck
        CHECK (escalation_at < deadline_at);

CREATE INDEX remote_access_requirements_escalation_scan
    ON remote_access_requirement_snapshots (escalation_at, id)
    WHERE status = 'pending' AND escalated_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS remote_access_requirements_escalation_scan;
ALTER TABLE remote_access_requirement_snapshots
    DROP CONSTRAINT IF EXISTS remote_access_requirement_escalation_window_ck,
    DROP COLUMN IF EXISTS escalation_at;
ALTER TABLE remote_access_approval_workflows
    DROP CONSTRAINT IF EXISTS remote_access_workflow_escalation_window_ck,
    DROP COLUMN IF EXISTS escalation_after_seconds;
