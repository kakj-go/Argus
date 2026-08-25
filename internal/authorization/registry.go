package authorization

const PermissionRegistryVersion int32 = 6

type BuiltinRole struct {
	Key         string
	Name        string
	Description string
	Permissions []string
}

var PermissionRegistry = map[string]string{
	"department.read":                 "Read departments",
	"department.manage":               "Manage departments",
	"identity.read":                   "Read enterprise identities",
	"identity.manage":                 "Manage enterprise identities",
	"role.read":                       "Read roles and bindings",
	"role.manage":                     "Manage roles and bindings",
	"data_scope.read":                 "Read data scopes",
	"data_scope.manage":               "Manage data scopes",
	"service_account.read":            "Read service accounts and API keys",
	"service_account.manage":          "Manage service accounts and API keys",
	"audit.read":                      "Read enterprise audit events",
	"host.read":                       "Read hosts",
	"host.manage":                     "Manage hosts",
	"host.test":                       "Test host connections",
	"kubernetes.read":                 "Read Kubernetes clusters and resources",
	"kubernetes.manage":               "Manage Kubernetes clusters",
	"kubernetes.logs":                 "Read bounded Kubernetes Pod logs",
	"secret.read":                     "Read Secret metadata",
	"secret.manage":                   "Create and rotate Secrets",
	"credential.read":                 "Read Credential metadata",
	"credential.manage":               "Manage Credentials",
	"credential.use":                  "Use Credentials through the broker",
	"managed_account.read":            "Read managed accounts",
	"managed_account.manage":          "Manage managed accounts",
	"bastion_scope.read":              "Read Bastion Scopes",
	"bastion_scope.manage":            "Manage Bastion Scopes",
	"connector.read":                  "Read Connector diagnostics",
	"connector.manage":                "Manage Connector lifecycle",
	"pending_action.read":             "Read resource Pending Actions",
	"pending_action.confirm":          "Confirm resource Pending Actions",
	"conversation.read":               "Read conversations and immutable events",
	"conversation.use":                "Create messages and run the Model Agent",
	"model.read":                      "Read enabled AI model metadata and own availability",
	"model.manage":                    "Manage AI models and pricing",
	"model.quota.manage":              "Manage department and user model quotas",
	"model.usage.read":                "Read governed model usage",
	"approval_policy.read":            "Read approval policies",
	"approval_policy.manage":          "Manage approval policies",
	"approval.read":                   "Read approval requests",
	"approval.decide":                 "Approve or reject eligible requests",
	"execution.read":                  "Read deterministic execution state",
	"interactive_card.read":           "Read the interactive Card catalog",
	"interactive_card.create":         "Create enterprise Card drafts through Chat",
	"interactive_card.update":         "Create Card configuration revisions",
	"interactive_card.publish":        "Validate, activate, disable, and roll back Cards",
	"interactive_card.deprecate":      "Deprecate enterprise Cards",
	"remote_access.grant.read":        "Read remote access grants",
	"remote_access.grant.manage":      "Manage remote access grants",
	"remote_access.policy.read":       "Read remote access policies",
	"remote_access.policy.manage":     "Manage remote access policies",
	"remote_access.request":           "Request remote access for oneself",
	"remote_access.session.create":    "Create remote access sessions from a lease",
	"remote_access.session.approve":   "Approve eligible remote access requests",
	"remote_access.session.terminate": "Terminate remote access sessions",
	"remote_access.recording.read":    "Read authorized remote access recordings",
	"telemetry.collector.read":        "Read Collector catalog and status",
	"telemetry.collector.manage":      "Manage Collector lifecycle and routes",
	"telemetry.query.metrics":         "Query authorized metrics",
	"telemetry.query.logs":            "Query authorized logs",
	"telemetry.query.traces":          "Query authorized traces",
	"telemetry.sensitive_fields.read": "Read governed sensitive telemetry fields",
	"telemetry.usage.read":            "Read telemetry usage and retention",
}

var BuiltinRoles = []BuiltinRole{
	{Key: "enterprise_admin", Name: "Enterprise Admin", Permissions: registryKeys()},
	{Key: "iam_admin", Name: "IAM Admin", Permissions: []string{"department.read", "department.manage", "identity.read", "identity.manage", "role.read", "role.manage", "data_scope.read", "data_scope.manage", "service_account.read", "service_account.manage"}},
	{Key: "security_auditor", Name: "Security Auditor", Permissions: []string{"department.read", "identity.read", "role.read", "data_scope.read", "service_account.read", "audit.read"}},
	{Key: "resource_admin", Name: "Resource Admin", Permissions: []string{"data_scope.read", "host.read", "host.manage", "host.test", "kubernetes.read", "kubernetes.manage", "kubernetes.logs", "secret.read", "secret.manage", "credential.read", "credential.manage", "credential.use", "managed_account.read", "managed_account.manage", "bastion_scope.read", "bastion_scope.manage", "connector.read", "connector.manage", "pending_action.read", "pending_action.confirm", "conversation.read", "conversation.use", "model.read", "approval_policy.read", "approval.read", "approval.decide", "execution.read", "remote_access.grant.read", "remote_access.policy.read", "remote_access.request", "remote_access.session.create", "remote_access.session.approve", "remote_access.session.terminate", "remote_access.recording.read", "telemetry.collector.read", "telemetry.collector.manage", "telemetry.query.metrics", "telemetry.query.logs", "telemetry.query.traces", "telemetry.usage.read"}},
	{Key: "resource_operator", Name: "Resource Operator", Permissions: []string{"data_scope.read", "host.read", "host.test", "kubernetes.read", "kubernetes.logs", "secret.read", "credential.read", "credential.use", "managed_account.read", "bastion_scope.read", "connector.read", "pending_action.read", "conversation.read", "conversation.use", "model.read", "approval_policy.read", "approval.read", "approval.decide", "execution.read", "remote_access.grant.read", "remote_access.policy.read", "remote_access.request", "remote_access.session.create", "remote_access.session.terminate", "telemetry.collector.read", "telemetry.query.metrics", "telemetry.query.logs", "telemetry.query.traces", "telemetry.usage.read"}},
	{Key: "resource_viewer", Name: "Resource Viewer", Permissions: []string{"data_scope.read", "host.read", "kubernetes.read", "secret.read", "credential.read", "managed_account.read", "bastion_scope.read", "connector.read", "pending_action.read", "conversation.read", "conversation.use", "model.read", "execution.read", "remote_access.grant.read", "remote_access.policy.read", "remote_access.request", "remote_access.session.create", "telemetry.collector.read", "telemetry.query.metrics", "telemetry.query.logs", "telemetry.query.traces"}},
	{Key: "resource_approver", Name: "Resource Approver", Permissions: []string{"data_scope.read", "host.read", "kubernetes.read", "secret.read", "credential.read", "managed_account.read", "bastion_scope.read", "connector.read", "pending_action.read", "audit.read", "approval_policy.read", "approval.read", "approval.decide", "execution.read", "remote_access.grant.read", "remote_access.policy.read", "remote_access.session.approve", "remote_access.session.terminate", "remote_access.recording.read"}},
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
