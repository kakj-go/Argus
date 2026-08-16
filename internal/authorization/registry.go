package authorization

type BuiltinRole struct {
	Key         string
	Name        string
	Description string
	Permissions []string
}

var PermissionRegistry = map[string]string{
	"department.read":        "Read departments",
	"department.manage":      "Manage departments",
	"identity.read":          "Read enterprise identities",
	"identity.manage":        "Manage enterprise identities",
	"role.read":              "Read roles and bindings",
	"role.manage":            "Manage roles and bindings",
	"data_scope.read":        "Read data scopes",
	"data_scope.manage":      "Manage data scopes",
	"service_account.read":   "Read service accounts and API keys",
	"service_account.manage": "Manage service accounts and API keys",
	"audit.read":             "Read enterprise audit events",
}

var BuiltinRoles = []BuiltinRole{
	{Key: "enterprise_admin", Name: "Enterprise Admin", Permissions: registryKeys()},
	{Key: "iam_admin", Name: "IAM Admin", Permissions: []string{"department.read", "department.manage", "identity.read", "identity.manage", "role.read", "role.manage", "data_scope.read", "data_scope.manage", "service_account.read", "service_account.manage"}},
	{Key: "security_auditor", Name: "Security Auditor", Permissions: []string{"department.read", "identity.read", "role.read", "data_scope.read", "service_account.read", "audit.read"}},
	{Key: "resource_admin", Name: "Resource Admin", Permissions: []string{"data_scope.read"}},
	{Key: "resource_operator", Name: "Resource Operator", Permissions: []string{"data_scope.read"}},
	{Key: "resource_viewer", Name: "Resource Viewer", Permissions: []string{"data_scope.read"}},
	{Key: "resource_approver", Name: "Resource Approver", Permissions: []string{"data_scope.read", "audit.read"}},
}

func registryKeys() []string {
	keys := make([]string, 0, len(PermissionRegistry))
	for key := range PermissionRegistry {
		keys = append(keys, key)
	}
	slicesSort(keys)
	return keys
}

func slicesSort(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
