package authorization

import "testing"

func TestPermissionRegistryVersionAndBuiltinRolesIncludeM7(t *testing.T) {
	if PermissionRegistryVersion != 9 {
		t.Fatalf("permission registry version = %d, want 9", PermissionRegistryVersion)
	}

	for _, permission := range []string{
		"data_authorization.read", "data_authorization.manage",
		"conversation.use", "model.manage", "model.quota.manage", "approval_policy.manage",
		"approval.decide", "execution.read", "interactive_card.read",
		"interactive_card.create", "interactive_card.update", "interactive_card.publish", "interactive_card.deprecate",
		"remote_access.grant.read", "remote_access.grant.manage",
		"remote_access.rule.read", "remote_access.rule.manage", "remote_access.workflow.read", "remote_access.workflow.manage",
		"remote_access.session_profile.read", "remote_access.session_profile.manage", "remote_access.governance.references.read",
		"remote_access.request", "remote_access.session.create", "remote_access.session.approve", "remote_access.session.terminate", "remote_access.recording.read",
	} {
		if _, ok := PermissionRegistry[permission]; !ok {
			t.Errorf("M4 permission %q is missing from registry", permission)
		}
	}
	for _, permission := range []string{
		"telemetry.collector.read", "telemetry.collector.manage", "telemetry.query.metrics", "telemetry.query.logs",
		"telemetry.query.traces", "telemetry.sensitive_fields.read", "telemetry.usage.read",
	} {
		if _, ok := PermissionRegistry[permission]; !ok {
			t.Errorf("M7 permission %q is missing from registry", permission)
		}
	}

	for _, role := range BuiltinRoles {
		seen := make(map[string]struct{}, len(role.Permissions))
		for _, permission := range role.Permissions {
			if _, ok := PermissionRegistry[permission]; !ok {
				t.Errorf("role %q references unknown permission %q", role.Key, permission)
			}
			if _, duplicate := seen[permission]; duplicate {
				t.Errorf("role %q repeats permission %q", role.Key, permission)
			}
			seen[permission] = struct{}{}
		}
	}

	assertRolePermissions(t, "enterprise_admin", "model.manage", "model.quota.manage", "approval_policy.manage",
		"interactive_card.read", "interactive_card.create", "interactive_card.update", "interactive_card.publish", "interactive_card.deprecate",
		"remote_access.grant.manage", "remote_access.rule.manage", "remote_access.workflow.manage", "remote_access.session_profile.manage")
	assertRolePermissions(t, "resource_admin", "conversation.use", "model.read", "approval.decide")
	assertRolePermissions(t, "resource_operator", "remote_access.request", "remote_access.session.create")
	assertRolePermissions(t, "resource_viewer", "remote_access.request", "remote_access.session.create")
	assertRolePermissions(t, "resource_approver", "approval.read", "approval.decide", "execution.read", "remote_access.session.approve")
	assertRoleLacksPermissions(t, "resource_admin", "remote_access.grant.read", "remote_access.rule.manage", "remote_access.session.terminate", "remote_access.recording.read")
	assertRoleLacksPermissions(t, "resource_operator", "remote_access.rule.read", "remote_access.governance.references.read", "remote_access.session.terminate")
	assertRoleLacksPermissions(t, "resource_viewer", "remote_access.session_profile.read")
	assertRoleLacksPermissions(t, "resource_approver", "remote_access.workflow.read", "remote_access.recording.read")
}

func assertRoleLacksPermissions(t *testing.T, key string, permissions ...string) {
	t.Helper()
	for _, role := range BuiltinRoles {
		if role.Key != key {
			continue
		}
		available := make(map[string]struct{}, len(role.Permissions))
		for _, permission := range role.Permissions {
			available[permission] = struct{}{}
		}
		for _, permission := range permissions {
			if _, ok := available[permission]; ok {
				t.Errorf("role %q unexpectedly has permission %q", key, permission)
			}
		}
		return
	}
	t.Errorf("builtin role %q is missing", key)
}

func assertRolePermissions(t *testing.T, key string, permissions ...string) {
	t.Helper()
	for _, role := range BuiltinRoles {
		if role.Key != key {
			continue
		}
		available := make(map[string]struct{}, len(role.Permissions))
		for _, permission := range role.Permissions {
			available[permission] = struct{}{}
		}
		for _, permission := range permissions {
			if _, ok := available[permission]; !ok {
				t.Errorf("role %q is missing permission %q", key, permission)
			}
		}
		return
	}
	t.Errorf("builtin role %q is missing", key)
}
