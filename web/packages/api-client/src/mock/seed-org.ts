import type {
  ApiKey,
  ApprovalPolicy,
  DataScope,
  Department,
  Role,
  RoleBinding,
  ServiceAccount,
  User,
} from "../types";
import type { MockEnterpriseUserRecord } from "./internal-types";

const DAY = 86_400_000;
const HOUR = 3_600_000;
const MINUTE = 60_000;

export interface BuiltinRoleTemplate {
  key: string;
  name: string;
  description?: string;
  permissions: string[];
}

/**
 * 企业内置角色（docs/02 §2.2）。新企业创建时由 platform.createEnterprise
 * 按此模板生成；种子数据使用语义 id（role-ea 等）但定义保持一致。
 */
export const BUILTIN_ROLE_TEMPLATES: BuiltinRoleTemplate[] = [
  {
    key: "enterprise_admin",
    name: "Enterprise Admin",
    description: "企业管理员",
    permissions: ["*"],
  },
  {
    key: "iam_admin",
    name: "IAM Admin",
    description: "身份与授权管理员",
    permissions: ["identity.user.manage", "audit.read"],
  },
  {
    key: "security_auditor",
    name: "Security Auditor",
    description: "安全审计员",
    permissions: ["audit.read", "remote_access.recording.read"],
  },
  {
    key: "resource_admin",
    name: "Resource Admin",
    description: "项目管理员",
    permissions: [
      "host.read",
      "host.create",
      "host.update",
      "host.connection.test",
      "host.direct_connect",
      "connector.read",
      "connector.create",
      "connector.rotate_credential",
      "bastion_scope.read",
      "bastion_scope.create",
      "bastion_scope.manage",
      "remote_access.request",
      "remote_access.session.create",
      "remote_access.session.approve",
      "remote_access.session.terminate",
      "remote_access.recording.read",
      "kubernetes.cluster.read",
      "kubernetes.cluster.create",
      "kubernetes.pod.read",
      "kubernetes.workload.restart",
      "telemetry.read",
      "telemetry.query.metrics",
      "telemetry.query.logs",
      "telemetry.query.traces",
      "telemetry.live_tail",
      "telemetry.export",
      "telemetry.alert.manage",
      "telemetry.dashboard.manage",
      "telemetry.sensitive_fields.read",
      "credential.manage",
      "credential.use",
      "interactive_card.read",
      "interactive_card.create",
      "interactive_card.update",
      "interactive_card.delete",
      "interactive_card.publish",
    ],
  },
  {
    key: "resource_operator",
    name: "Resource Operator",
    description: "项目操作员",
    permissions: [
      "host.read",
      "host.connection.test",
      "connector.read",
      "bastion_scope.read",
      "remote_access.request",
      "remote_access.session.create",
      "kubernetes.cluster.read",
      "kubernetes.pod.read",
      "kubernetes.workload.restart",
      "telemetry.read",
      "telemetry.query.metrics",
      "telemetry.query.logs",
      "telemetry.query.traces",
      "telemetry.live_tail",
      "credential.use",
      "interactive_card.read",
      "interactive_card.create",
    ],
  },
  {
    key: "resource_viewer",
    name: "Resource Viewer",
    description: "项目只读成员",
    permissions: [
      "host.read",
      "connector.read",
      "bastion_scope.read",
      "kubernetes.cluster.read",
      "kubernetes.pod.read",
      "telemetry.read",
      "telemetry.query.metrics",
      "interactive_card.read",
    ],
  },
  {
    key: "resource_approver",
    name: "Resource Approver",
    description: "项目审批人",
    permissions: [
      "host.read",
      "kubernetes.cluster.read",
      "telemetry.read",
      "telemetry.query.metrics",
      "remote_access.session.approve",
    ],
  },
  {
    key: "department_admin",
    name: "Department Admin",
    description: "部门管理员（模型配额）",
    permissions: ["model_quota.manage", "model_usage.read"],
  },
];

/** createSeedDb 的 org 域部分：身份、授权与访问控制种子。 */
export interface OrgSeed {
  users: User[];
  credentials: Record<string, string>;
  enterpriseUsers: MockEnterpriseUserRecord[];
  departments: Department[];
  roles: Role[];
  roleBindings: RoleBinding[];
  dataScopes: DataScope[];
  approvalPolicies: ApprovalPolicy[];
  serviceAccounts: ServiceAccount[];
  apiKeys: ApiKey[];
}

export function createOrgSeed(now: number): OrgSeed {
  const ago = (offsetMs: number) => new Date(now - offsetMs).toISOString();
  const template = (key: string): BuiltinRoleTemplate => {
    const found = BUILTIN_ROLE_TEMPLATES.find((entry) => entry.key === key);
    if (!found) throw new Error(`unknown builtin role template: ${key}`);
    return found;
  };
  const acmeRole = (id: string, key: string): Role => {
    const roleTemplate = template(key);
    return {
      id,
      enterprise_id: "ent-acme",
      builtin_key: roleTemplate.key,
      name: roleTemplate.name,
      description: roleTemplate.description,
      builtin: true,
      permissions: [...roleTemplate.permissions],
      status: "active",
      version: 1,
      created_at: ago(120 * DAY),
      updated_at: ago(120 * DAY),
    };
  };

  return {
    users: [
      {
        id: "u-admin",
        username: "admin",
        displayName: "平台超级管理员",
        email: "admin@argus.local",
        platformRole: "platform_super_admin",
        status: "active",
        mfaEnabled: true,
        lastLoginAt: ago(HOUR),
        createdAt: ago(120 * DAY),
      },
      {
        id: "u-root",
        username: "root",
        displayName: "企业超级管理员",
        email: "root@acme.example",
        status: "active",
        mfaEnabled: true,
        lastLoginAt: ago(HOUR),
        createdAt: ago(120 * DAY),
      },
      {
        id: "u-chenxi",
        username: "chenxi",
        displayName: "陈曦",
        email: "chenxi@acme.example",
        status: "active",
        mfaEnabled: false,
        lastLoginAt: ago(2 * HOUR),
        createdAt: ago(120 * DAY),
      },
      {
        id: "u-wanglei",
        username: "wanglei",
        displayName: "王磊",
        email: "wanglei@acme.example",
        status: "active",
        mfaEnabled: true,
        lastLoginAt: ago(30 * MINUTE),
        createdAt: ago(118 * DAY),
      },
      {
        id: "u-lina",
        username: "lina",
        displayName: "李娜",
        email: "lina@acme.example",
        status: "active",
        mfaEnabled: false,
        createdAt: ago(90 * DAY),
      },
      {
        id: "u-gadmin",
        username: "globex-admin",
        displayName: "Globex 管理员",
        email: "admin@globex.example",
        status: "active",
        mfaEnabled: false,
        lastLoginAt: ago(DAY),
        createdAt: ago(80 * DAY),
      },
    ],
    credentials: {
      "u-admin": "123456",
      "u-root": "123456",
      "u-chenxi": "123456",
      "u-wanglei": "123456",
      "u-lina": "123456",
      "u-gadmin": "123456",
    },
    enterpriseUsers: [
      { userId: "u-root", enterpriseId: "ent-acme", departmentId: "dept-sre" },
      {
        userId: "u-chenxi",
        enterpriseId: "ent-acme",
        departmentId: "dept-sre",
      },
      {
        userId: "u-wanglei",
        enterpriseId: "ent-acme",
        departmentId: "dept-sre",
      },
      { userId: "u-lina", enterpriseId: "ent-acme", departmentId: "dept-pay" },
      {
        userId: "u-gadmin",
        enterpriseId: "ent-globex",
        departmentId: "dept-globex-default",
      },
    ],
    departments: [
      {
        id: "dept-sre",
        enterprise_id: "ent-acme",
        name: "SRE",
        description: "站点可靠性团队",
        is_default: true,
        status: "active",
        version: 1,
        created_at: ago(100 * DAY),
        updated_at: ago(100 * DAY),
      },
      {
        id: "dept-pay",
        enterprise_id: "ent-acme",
        name: "支付团队",
        is_default: false,
        status: "active",
        version: 1,
        created_at: ago(90 * DAY),
        updated_at: ago(90 * DAY),
      },
      {
        id: "dept-globex-default",
        enterprise_id: "ent-globex",
        name: "默认部门",
        is_default: true,
        status: "active",
        version: 1,
        created_at: ago(80 * DAY),
        updated_at: ago(80 * DAY),
      },
    ],
    roles: [
      acmeRole("role-ea", "enterprise_admin"),
      acmeRole("role-iam", "iam_admin"),
      acmeRole("role-audit", "security_auditor"),
      acmeRole("role-pa", "resource_admin"),
      acmeRole("role-po", "resource_operator"),
      acmeRole("role-pv", "resource_viewer"),
      acmeRole("role-pap", "resource_approver"),
      acmeRole("role-da", "department_admin"),
      {
        id: "role-g-ea",
        enterprise_id: "ent-globex",
        builtin_key: "enterprise_admin",
        name: template("enterprise_admin").name,
        description: template("enterprise_admin").description,
        builtin: true,
        permissions: ["*"],
        status: "active",
        version: 1,
        created_at: ago(80 * DAY),
        updated_at: ago(80 * DAY),
      },
      {
        id: "role-g-pv",
        enterprise_id: "ent-globex",
        builtin_key: "resource_viewer",
        name: template("resource_viewer").name,
        description: template("resource_viewer").description,
        builtin: true,
        permissions: [...template("resource_viewer").permissions],
        status: "active",
        version: 1,
        created_at: ago(80 * DAY),
        updated_at: ago(80 * DAY),
      },
    ],
    roleBindings: (
      [
        {
          id: "rb-root-ea",
          enterprise_id: "ent-acme",
          subject_type: "user",
          subject_id: "u-root",
          role_id: "role-ea",
          data_scope_ids: [],
          status: "active",
          created_at: ago(120 * DAY),
        },
        {
          id: "rb-chenxi-ea",
          enterprise_id: "ent-acme",
          subject_type: "user",
          subject_id: "u-chenxi",
          role_id: "role-ea",
          data_scope_ids: [],
          status: "active",
          created_at: ago(120 * DAY),
        },
        {
          id: "rb-wanglei-da",
          enterprise_id: "ent-acme",
          subject_type: "user",
          subject_id: "u-wanglei",
          role_id: "role-da",
          data_scope_ids: [],
          status: "active",
          created_at: ago(118 * DAY),
        },
        {
          id: "rb-wanglei-po",
          enterprise_id: "ent-acme",
          subject_type: "user",
          subject_id: "u-wanglei",
          role_id: "role-po",
          data_scope_ids: ["ds-ops-nonprod"],
          status: "active",
          created_at: ago(118 * DAY),
        },
        {
          id: "rb-lina-pv",
          enterprise_id: "ent-acme",
          subject_type: "user",
          subject_id: "u-lina",
          role_id: "role-pv",
          data_scope_ids: ["ds-ops-nonprod"],
          status: "active",
          created_at: ago(90 * DAY),
        },
        {
          id: "rb-deptpay-pv",
          enterprise_id: "ent-acme",
          subject_type: "department",
          subject_id: "dept-pay",
          role_id: "role-pv",
          data_scope_ids: ["ds-ops-nonprod"],
          status: "active",
          created_at: ago(90 * DAY),
        },
        {
          id: "rb-gadmin-ea",
          enterprise_id: "ent-globex",
          subject_type: "user",
          subject_id: "u-gadmin",
          role_id: "role-g-ea",
          data_scope_ids: [],
          status: "active",
          created_at: ago(80 * DAY),
        },
      ] satisfies Array<Omit<RoleBinding, "version" | "updated_at">>
    ).map((binding) => ({
      ...binding,
      version: 1,
      updated_at: binding.created_at,
    })),
    dataScopes: [
      {
        id: "ds-ops-nonprod",
        enterprise_id: "ent-acme",
        name: "ops 非生产范围",
        description: "开发与预发布环境的运维资源",
        resource_types: ["host", "kubernetes_cluster"],
        explicit_resource_ids: [],
        match_all: false,
        label_selector: {
          schema_version: "argus.label_selector/v1",
          requirements: [
            {
              key: "environment",
              operator: "in",
              values: ["development", "staging"],
            },
          ],
        },
        status: "active",
        version: 1,
        created_at: ago(100 * DAY),
        updated_at: ago(100 * DAY),
      },
    ],
    approvalPolicies: [
      {
        id: "ap-prod-danger",
        enterpriseId: "ent-acme",
        name: "生产危险操作审批",
        matchRiskLevels: ["dangerous", "critical"],
        minApprovers: 1,
        approverRoleIds: ["role-po"],
        separationOfDuty: true,
        enabled: true,
        createdAt: ago(100 * DAY),
      },
      {
        id: "ap-critical",
        enterpriseId: "ent-acme",
        name: "关键操作双人审批",
        matchRiskLevels: ["critical"],
        minApprovers: 2,
        approverRoleIds: ["role-po", "role-audit"],
        separationOfDuty: true,
        enabled: true,
        createdAt: ago(100 * DAY),
      },
    ],
    serviceAccounts: [
      {
        id: "sa-ci",
        enterprise_id: "ent-acme",
        name: "sre-schedule",
        description: "自动化巡检任务",
        allowed_tool_ids: ["host.list", "telemetry.query"],
        data_scope_ids: ["ds-ops-nonprod"],
        status: "active",
        authorization_version: 1,
        version: 1,
        created_at: ago(60 * DAY),
        updated_at: ago(90 * MINUTE),
      },
    ],
    apiKeys: [
      {
        id: "ak-0001",
        enterprise_id: "ent-acme",
        service_account_id: "sa-ci",
        name: "ci-readonly",
        prefix: "7f3a9c21",
        status: "active",
        version: 1,
        last_used_at: ago(90 * MINUTE),
        created_at: ago(60 * DAY),
      },
    ],
  };
}
