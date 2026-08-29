-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_remote_access_governance_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'remote access governance objects must be archived, not deleted'
        USING ERRCODE = '55000';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER remote_access_grants_reject_delete
BEFORE DELETE ON remote_access_grants
FOR EACH ROW EXECUTE FUNCTION reject_remote_access_governance_delete();

CREATE TRIGGER remote_access_rules_reject_delete
BEFORE DELETE ON remote_access_rules
FOR EACH ROW EXECUTE FUNCTION reject_remote_access_governance_delete();

CREATE TRIGGER remote_access_workflows_reject_delete
BEFORE DELETE ON remote_access_approval_workflows
FOR EACH ROW EXECUTE FUNCTION reject_remote_access_governance_delete();

CREATE TRIGGER remote_access_session_profiles_reject_delete
BEFORE DELETE ON remote_access_session_profiles
FOR EACH ROW EXECUTE FUNCTION reject_remote_access_governance_delete();

-- +goose Down

DROP TRIGGER IF EXISTS remote_access_session_profiles_reject_delete ON remote_access_session_profiles;
DROP TRIGGER IF EXISTS remote_access_workflows_reject_delete ON remote_access_approval_workflows;
DROP TRIGGER IF EXISTS remote_access_rules_reject_delete ON remote_access_rules;
DROP TRIGGER IF EXISTS remote_access_grants_reject_delete ON remote_access_grants;
DROP FUNCTION IF EXISTS reject_remote_access_governance_delete();
