package authorization

import "testing"

func TestPermissionRegistryVersionAndBuiltinRolesIncludeM7(t *testing.T) {
	if PermissionRegistryVersion != 6 {
		t.Fatalf("permission registry version = %d, want 6", PermissionRegistryVersion)
	}

	for _, permission := range []string{
		"conversation.use", "model.manage", "model.quota.manage", "approval_policy.manage",
		"approval.decide", "execution.read", "automation.manage", "interactive_card.read",
		"interactive_card.create", "interactive_card.update", "interactive_card.publish", "interactive_card.deprecate",
		"remote_access.grant.read", "remote_access.grant.manage", "remote_access.policy.read", "remote_access.policy.manage",
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

	assertRolePermissions(t, "enterprise_admin", "model.manage", "model.quota.manage", "approval_policy.manage", "automation.manage",
		"interactive_card.read", "interactive_card.create", "interactive_card.update", "interactive_card.publish", "interactive_card.deprecate",
		"remote_access.grant.manage", "remote_access.policy.manage")
	assertRolePermissions(t, "resource_admin", "conversation.use", "model.read", "approval.decide", "automation.read")
	assertRolePermissions(t, "resource_approver", "approval.read", "approval.decide", "execution.read", "remote_access.session.approve", "remote_access.recording.read")
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
