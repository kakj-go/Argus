-- +goose Up

UPDATE permissions
SET registry_version = 8
WHERE id IN (
    'remote_access.grant.read',
    'remote_access.grant.manage',
    'remote_access.rule.read',
    'remote_access.rule.manage',
    'remote_access.workflow.read',
    'remote_access.workflow.manage',
    'remote_access.session_profile.read',
    'remote_access.session_profile.manage',
    'remote_access.governance.references.read',
    'remote_access.session.terminate',
    'remote_access.recording.read'
);

DELETE FROM role_permissions assignment
USING roles role
WHERE assignment.role_id = role.id
  AND role.builtin = true
  AND role.identity_key <> 'enterprise_admin'
  AND assignment.permission_id IN (
      'remote_access.grant.read',
      'remote_access.grant.manage',
      'remote_access.rule.read',
      'remote_access.rule.manage',
      'remote_access.workflow.read',
      'remote_access.workflow.manage',
      'remote_access.session_profile.read',
      'remote_access.session_profile.manage',
      'remote_access.governance.references.read',
      'remote_access.session.terminate',
      'remote_access.recording.read'
  );

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles role
CROSS JOIN permissions permission
WHERE role.builtin = true
  AND role.identity_key = 'enterprise_admin'
  AND permission.registry_version = 8
ON CONFLICT DO NOTHING;

ALTER TABLE remote_access_grants
    ADD COLUMN status text NOT NULL DEFAULT 'enabled'
    CHECK (status IN ('draft','enabled','disabled','archived'));
UPDATE remote_access_grants SET status = CASE WHEN enabled THEN 'enabled' ELSE 'disabled' END;
ALTER TABLE remote_access_grants DROP COLUMN enabled;
DROP INDEX IF EXISTS remote_access_grants_subject;
CREATE INDEX remote_access_grants_subject ON remote_access_grants (enterprise_id, subject_type, subject_id, status, valid_until);

ALTER TABLE remote_access_sessions ADD COLUMN gateway_instance text;

-- +goose Down

ALTER TABLE remote_access_sessions DROP COLUMN gateway_instance;
ALTER TABLE remote_access_grants ADD COLUMN enabled boolean NOT NULL DEFAULT false;
UPDATE remote_access_grants SET enabled = status = 'enabled';
ALTER TABLE remote_access_grants DROP COLUMN status;
DROP INDEX IF EXISTS remote_access_grants_subject;
CREATE INDEX remote_access_grants_subject ON remote_access_grants (enterprise_id, subject_type, subject_id, enabled, valid_until);

UPDATE permissions
SET registry_version = 7
WHERE registry_version = 8;

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles role
CROSS JOIN permissions permission
WHERE role.builtin = true
  AND role.identity_key = 'resource_admin'
  AND permission.id IN (
      'remote_access.grant.read',
      'remote_access.rule.read',
      'remote_access.rule.manage',
      'remote_access.workflow.read',
      'remote_access.workflow.manage',
      'remote_access.session_profile.read',
      'remote_access.session_profile.manage',
      'remote_access.governance.references.read',
      'remote_access.session.terminate',
      'remote_access.recording.read'
  )
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles role
CROSS JOIN permissions permission
WHERE role.builtin = true
  AND role.identity_key = 'resource_operator'
  AND permission.id IN (
      'remote_access.grant.read',
      'remote_access.rule.read',
      'remote_access.workflow.read',
      'remote_access.session_profile.read',
      'remote_access.governance.references.read',
      'remote_access.session.terminate'
  )
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles role
CROSS JOIN permissions permission
WHERE role.builtin = true
  AND role.identity_key = 'resource_viewer'
  AND permission.id IN (
      'remote_access.grant.read',
      'remote_access.rule.read',
      'remote_access.workflow.read',
      'remote_access.session_profile.read'
  )
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT role.id, permission.id
FROM roles role
CROSS JOIN permissions permission
WHERE role.builtin = true
  AND role.identity_key = 'resource_approver'
  AND permission.id IN (
      'remote_access.grant.read',
      'remote_access.rule.read',
      'remote_access.workflow.read',
      'remote_access.session_profile.read',
      'remote_access.governance.references.read',
      'remote_access.session.terminate',
      'remote_access.recording.read'
  )
ON CONFLICT DO NOTHING;
