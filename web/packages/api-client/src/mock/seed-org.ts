import type {
  ApiKey,
  ApprovalPolicy,
  DataScope,
  Department,
  EnterpriseMembership,
  Project,
  Role,
  RoleBinding,
  ServiceAccount,
  User,
} from "../types";

const DAY = 86_400_000;
const HOUR = 3_600_000;
const MINUTE = 60_000;

export interface BuiltinRoleTemplate {
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
    name: "enterprise_admin",
    description: "企业管理员",
    permissions: ["*"],
  },
  {
    name: "iam_admin",
    description: "身份与授权管理员",
    permissions: ["project.member.manage", "audit.read"],
  },
  {
    name: "security_auditor",
    description: "安全审计员",
    permissions: ["audit.read", "remote_access.recording.read"],
  },
  {
    name: "project_admin",
    description: "项目管理员",
    permissions: [
      "project.read",
      "project.update",
      "project.member.manage",
      "host.read",
      "host.create",
      "host.update",
      "host.connection.test",
      "host.direct_connect",
      "automation.command.execute",
      "automation.template.execute",
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
    name: "project_operator",
    description: "项目操作员",
    permissions: [
      "project.read",
      "host.read",
      "host.connection.test",
      "automation.command.execute",
      "automation.template.execute",
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
    name: "project_viewer",
    description: "项目只读成员",
    permissions: [
      "project.read",
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
    name: "project_approver",
    description: "项目审批人",
    permissions: [
      "project.read",
      "host.read",
      "kubernetes.cluster.read",
      "telemetry.read",
      "telemetry.query.metrics",
      "remote_access.session.approve",
    ],
  },
  {
    name: "department_admin",
    description: "部门管理员（模型配额）",
    permissions: ["model_quota.manage", "model_usage.read"],
  },
];

/** createSeedDb 的 org 域部分：身份、授权与访问控制种子。 */
export interface OrgSeed {
  users: User[];
  credentials: Record<string, string>;
  memberships: EnterpriseMembership[];
  departments: Department[];
  projects: Project[];
  roles: Role[];
  roleBindings: RoleBinding[];
  dataScopes: DataScope[];
  approvalPolicies: ApprovalPolicy[];
  serviceAccounts: ServiceAccount[];
  apiKeys: ApiKey[];
}

export function createOrgSeed(now: number): OrgSeed {
  const ago = (offsetMs: number) => new Date(now - offsetMs).toISOString();
  const template = (name: string): BuiltinRoleTemplate => {
    const found = BUILTIN_ROLE_TEMPLATES.find((entry) => entry.name === name);
    if (!found) throw new Error(`unknown builtin role template: ${name}`);
    return found;
  };
  const acmeRole = (id: string, name: string): Role => ({
    id,
    enterpriseId: "ent-acme",
    name,
    description: template(name).description,
    builtin: true,
    permissions: [...template(name).permissions],
    createdAt: ago(120 * DAY),
  });

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
    memberships: [
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
        enterpriseId: "ent-acme",
        name: "SRE",
        description: "站点可靠性团队",
        default: true,
        createdAt: ago(100 * DAY),
      },
      {
        id: "dept-pay",
        enterpriseId: "ent-acme",
        name: "支付团队",
        default: false,
        createdAt: ago(90 * DAY),
      },
      {
        id: "dept-globex-default",
        enterpriseId: "ent-globex",
        name: "默认部门",
        default: true,
        createdAt: ago(80 * DAY),
      },
    ],
    projects: [
      {
        id: "proj-default",
        enterpriseId: "ent-acme",
        name: "默认项目",
        description: "企业创建时自动生成",
        default: true,
        createdAt: ago(120 * DAY),
      },
      {
        id: "proj-g-default",
        enterpriseId: "ent-globex",
        name: "默认项目",
        description: "企业创建时自动生成",
        default: true,
        createdAt: ago(80 * DAY),
      },
    ],
    roles: [
      acmeRole("role-ea", "enterprise_admin"),
      acmeRole("role-iam", "iam_admin"),
      acmeRole("role-audit", "security_auditor"),
      acmeRole("role-pa", "project_admin"),
      acmeRole("role-po", "project_operator"),
      acmeRole("role-pv", "project_viewer"),
      acmeRole("role-pap", "project_approver"),
      acmeRole("role-da", "department_admin"),
      {
        id: "role-g-ea",
        enterpriseId: "ent-globex",
        name: "enterprise_admin",
        description: template("enterprise_admin").description,
        builtin: true,
        permissions: ["*"],
        createdAt: ago(80 * DAY),
      },
      {
        id: "role-g-pv",
        enterpriseId: "ent-globex",
        name: "project_viewer",
        description: template("project_viewer").description,
        builtin: true,
        permissions: [...template("project_viewer").permissions],
        createdAt: ago(80 * DAY),
      },
    ],
    roleBindings: [
      {
        id: "rb-root-ea",
        enterpriseId: "ent-acme",
        subjectType: "user",
        subjectId: "u-root",
        roleId: "role-ea",
        scopeType: "enterprise",
        scopeId: "ent-acme",
        status: "active",
        createdAt: ago(120 * DAY),
      },
      {
        id: "rb-chenxi-ea",
        enterpriseId: "ent-acme",
        subjectType: "user",
        subjectId: "u-chenxi",
        roleId: "role-ea",
        scopeType: "enterprise",
        scopeId: "ent-acme",
        status: "active",
        createdAt: ago(120 * DAY),
      },
      {
        id: "rb-wanglei-da",
        enterpriseId: "ent-acme",
        subjectType: "user",
        subjectId: "u-wanglei",
        roleId: "role-da",
        scopeType: "enterprise",
        scopeId: "ent-acme",
        status: "active",
        createdAt: ago(118 * DAY),
      },
      {
        id: "rb-wanglei-po",
        enterpriseId: "ent-acme",
        subjectType: "user",
        subjectId: "u-wanglei",
        roleId: "role-po",
        scopeType: "project",
        scopeId: "proj-default",
        status: "active",
        createdAt: ago(118 * DAY),
      },
      {
        id: "rb-lina-pv",
        enterpriseId: "ent-acme",
        subjectType: "user",
        subjectId: "u-lina",
        roleId: "role-pv",
        scopeType: "project",
        scopeId: "proj-default",
        status: "active",
        createdAt: ago(90 * DAY),
      },
      {
        id: "rb-deptpay-pv",
        enterpriseId: "ent-acme",
        subjectType: "department",
        subjectId: "dept-pay",
        roleId: "role-pv",
        scopeType: "project",
        scopeId: "proj-default",
        status: "active",
        createdAt: ago(90 * DAY),
      },
      {
        id: "rb-gadmin-ea",
        enterpriseId: "ent-globex",
        subjectType: "user",
        subjectId: "u-gadmin",
        roleId: "role-g-ea",
        scopeType: "enterprise",
        scopeId: "ent-globex",
        status: "active",
        createdAt: ago(80 * DAY),
      },
    ],
    dataScopes: [
      {
        id: "ds-ops-nonprod",
        enterpriseId: "ent-acme",
        name: "ops 非生产范围",
        subjectType: "role",
        subjectId: "role-po",
        environments: ["development", "staging"],
        tagExpression: "criticality!=high",
        onlyOwned: false,
        createdAt: ago(100 * DAY),
      },
    ],
    approvalPolicies: [
      {
        id: "ap-prod-danger",
        enterpriseId: "ent-acme",
        name: "生产危险操作审批",
        description: "production 环境 dangerous/critical 操作需 project_operator 审批",
        matchRiskLevels: ["dangerous", "critical"],
        matchEnvironments: ["production", "staging"],
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
        matchEnvironments: [],
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
        enterpriseId: "ent-acme",
        name: "sre-schedule",
        description: "自动化巡检任务",
        roleIds: ["role-pv"],
        status: "active",
        lastUsedAt: ago(90 * MINUTE),
        createdAt: ago(60 * DAY),
      },
    ],
    apiKeys: [
      {
        id: "ak-0001",
        enterpriseId: "ent-acme",
        serviceAccountId: "sa-ci",
        name: "ci-readonly",
        prefix: "argus_sk_7f3a",
        scopes: ["host.read", "telemetry.read"],
        status: "active",
        lastUsedAt: ago(90 * MINUTE),
        createdAt: ago(60 * DAY),
      },
    ],
  };
}
