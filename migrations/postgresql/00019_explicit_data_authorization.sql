-- +goose Up

-- Explicit resource grants replace selector-based DataScope authorization.

ALTER TABLE authorization_versions
    DROP CONSTRAINT IF EXISTS authorization_versions_subject_type_check;
ALTER TABLE authorization_versions
    ADD CONSTRAINT authorization_versions_subject_type_check
    CHECK (subject_type IN ('user', 'department', 'role', 'service_account'));

CREATE TABLE IF NOT EXISTS data_authorization_grants (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'department', 'role', 'service_account')),
    subject_id uuid NOT NULL,
    resource_type text NOT NULL CHECK (resource_type IN ('host', 'kubernetes_cluster')),
    resource_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (enterprise_id, subject_type, subject_id, resource_type, resource_id)
);

-- Subject/resource IDs are polymorphic, so enforce ownership with a trigger
-- instead of unsafe cross-table foreign keys.
-- The application validates the same invariants before every write.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_data_authorization_grant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.subject_type = 'user' AND NOT EXISTS (SELECT 1 FROM enterprise_users WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'authorization grant user does not belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.subject_type = 'department' AND NOT EXISTS (SELECT 1 FROM departments WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'authorization grant department does not belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.subject_type = 'role' AND NOT EXISTS (SELECT 1 FROM roles WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'authorization grant role does not belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.subject_type = 'service_account' AND NOT EXISTS (SELECT 1 FROM service_accounts WHERE id = NEW.subject_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'authorization grant service account does not belong to enterprise' USING ERRCODE = '23503';
    END IF;
    IF NEW.resource_type = 'host' AND NOT EXISTS (SELECT 1 FROM hosts WHERE id = NEW.resource_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'authorization grant host does not belong to enterprise' USING ERRCODE = '23503';
    ELSIF NEW.resource_type = 'kubernetes_cluster' AND NOT EXISTS (SELECT 1 FROM kubernetes_clusters WHERE id = NEW.resource_id AND enterprise_id = NEW.enterprise_id) THEN
        RAISE EXCEPTION 'authorization grant cluster does not belong to enterprise' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS data_authorization_grant_integrity ON data_authorization_grants;
CREATE CONSTRAINT TRIGGER data_authorization_grant_integrity
AFTER INSERT OR UPDATE ON data_authorization_grants DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION validate_data_authorization_grant();

CREATE INDEX IF NOT EXISTS data_authorization_grants_subject_idx
    ON data_authorization_grants (enterprise_id, subject_type, subject_id, resource_type, status);
CREATE INDEX IF NOT EXISTS data_authorization_grants_resource_idx
    ON data_authorization_grants (enterprise_id, resource_type, resource_id, status);

DROP TABLE IF EXISTS role_binding_data_scopes;
DROP TABLE IF EXISTS service_account_data_scopes;
DROP TABLE IF EXISTS data_scopes;

ALTER TABLE approval_policies
    DROP COLUMN IF EXISTS label_selector;

DROP INDEX IF EXISTS remote_access_grants_selector_gin;
ALTER TABLE remote_access_grants
    DROP COLUMN IF EXISTS host_selector,
    DROP COLUMN IF EXISTS host_selector_hash;

DROP INDEX IF EXISTS remote_access_rules_host_selector_gin;
ALTER TABLE remote_access_rules
    DROP COLUMN IF EXISTS subject_selector,
    DROP COLUMN IF EXISTS host_selector,
    DROP COLUMN IF EXISTS managed_account_selector;

DELETE FROM role_permissions
WHERE permission_id IN ('data_scope.read', 'data_scope.manage');
DELETE FROM permissions
WHERE id IN ('data_scope.read', 'data_scope.manage');

-- +goose Down
DROP TRIGGER IF EXISTS data_authorization_grant_integrity ON data_authorization_grants;
DROP FUNCTION IF EXISTS validate_data_authorization_grant();
DROP TABLE IF EXISTS data_authorization_grants;
