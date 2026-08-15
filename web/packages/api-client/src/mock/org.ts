import type { ArgusApiClient } from "../client";
import type { RoleBinding, User } from "../types";
import type { MockContext } from "./context";
import { nextId } from "./store";

/** Organization: users, departments, projects, roles, bindings, data scopes, policies, API keys. */
export function createOrgDomain(ctx: MockContext): ArgusApiClient["org"] {
  const { db } = ctx;

  return {
    async listUsers() {
      await ctx.pause();
      const memberIds = new Set(
        db.memberships
          .filter((m) => m.enterpriseId === ctx.enterpriseId())
          .map((m) => m.userId),
      );
      return db.users.filter((entry) => memberIds.has(entry.id));
    },
    async getMembership(userId) {
      await ctx.pause();
      return (
        db.memberships.find(
          (m) => m.userId === userId && m.enterpriseId === ctx.enterpriseId(),
        ) ?? null
      );
    },
    async inviteUser(input) {
      await ctx.pause();
      const user: User = {
        id: nextId(db, "u"),
        username: input.username,
        displayName: input.displayName,
        email: input.email,
        status: "invited",
        mfaEnabled: false,
        createdAt: ctx.nowIso(),
      };
      db.users.push(user);
      db.memberships.push({
        userId: user.id,
        enterpriseId: ctx.enterpriseId(),
        departmentId: input.departmentId,
      });
      // 邀请时勾选的角色转为企业范围 RoleBinding。
      for (const roleId of input.roleIds ?? []) {
        const binding: RoleBinding = {
          id: nextId(db, "rb"),
          enterpriseId: ctx.enterpriseId(),
          subjectType: "user",
          subjectId: user.id,
          roleId,
          scopeType: "enterprise",
          scopeId: ctx.enterpriseId(),
          status: "active",
          createdAt: ctx.nowIso(),
        };
        db.roleBindings.push(binding);
      }
      ctx.audit("org.user.invite", {
        resourceType: "user",
        resourceId: user.id,
        summary: `邀请用户 ${user.displayName}`,
      });
      ctx.save();
      return user;
    },
    async updateUser(userId, patch) {
      await ctx.pause();
      const user = ctx.mustFind(db.users, (entry) => entry.id === userId, "user");
      Object.assign(user, patch);
      ctx.audit("org.user.update", {
        resourceType: "user",
        resourceId: userId,
        summary: `更新用户 ${user.displayName}`,
      });
      ctx.save();
      return user;
    },
    async updateMembership(userId, patch) {
      await ctx.pause();
      const membership = ctx.mustFind(
        db.memberships,
        (m) => m.userId === userId && m.enterpriseId === ctx.enterpriseId(),
        "membership",
      );
      Object.assign(membership, patch);
      ctx.audit("org.membership.update", {
        resourceType: "user",
        resourceId: userId,
        summary: `更新用户部门 ${userId}`,
      });
      ctx.save();
      return membership;
    },
    async listDepartments() {
      await ctx.pause();
      return db.departments.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async createDepartment(input) {
      await ctx.pause();
      const department = {
        id: nextId(db, "dept"),
        enterpriseId: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        default: false,
        createdAt: ctx.nowIso(),
      };
      db.departments.push(department);
      ctx.audit("org.department.create", {
        resourceType: "department",
        resourceId: department.id,
        summary: `创建部门 ${department.name}`,
      });
      ctx.save();
      return department;
    },
    async updateDepartment(id, patch) {
      await ctx.pause();
      const department = ctx.mustFind(db.departments, (entry) => entry.id === id, "department");
      Object.assign(department, patch);
      ctx.audit("org.department.update", {
        resourceType: "department",
        resourceId: id,
        summary: `更新部门 ${department.name}`,
      });
      ctx.save();
      return department;
    },
    async deleteDepartment(id) {
      await ctx.pause();
      const department = ctx.mustFind(db.departments, (entry) => entry.id === id, "department");
      if (department.default) throw new Error("cannot delete default department");
      if (db.memberships.some((entry) => entry.departmentId === id)) {
        throw new Error("cannot delete department with members");
      }
      db.departments = db.departments.filter((entry) => entry.id !== id);
      ctx.audit("org.department.delete", {
        resourceType: "department",
        resourceId: id,
        summary: `删除部门 ${id}`,
      });
      ctx.save();
    },
    async listProjects() {
      await ctx.pause();
      return db.projects.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async createProject(input) {
      await ctx.pause();
      const project = {
        id: nextId(db, "proj"),
        enterpriseId: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        default: false,
        createdAt: ctx.nowIso(),
      };
      db.projects.push(project);
      ctx.audit("org.project.create", {
        resourceType: "project",
        resourceId: project.id,
        summary: `创建项目 ${project.name}`,
      });
      ctx.save();
      return project;
    },
    async updateProject(id, patch) {
      await ctx.pause();
      const project = ctx.mustFind(
        db.projects,
        (entry) => entry.id === id && entry.enterpriseId === ctx.enterpriseId(),
        "project",
      );
      Object.assign(project, patch);
      ctx.audit("org.project.update", {
        resourceType: "project",
        resourceId: id,
        summary: `更新项目 ${project.name}`,
      });
      ctx.save();
      return project;
    },
    async listRoles() {
      await ctx.pause();
      return db.roles.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async createRole(input) {
      await ctx.pause();
      const role = {
        id: nextId(db, "role"),
        enterpriseId: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        builtin: false,
        permissions: input.permissions,
        createdAt: ctx.nowIso(),
      };
      db.roles.push(role);
      ctx.audit("org.role.create", {
        resourceType: "role",
        resourceId: role.id,
        summary: `创建角色 ${role.name}`,
      });
      ctx.save();
      return role;
    },
    async updateRole(id, patch) {
      await ctx.pause();
      const role = ctx.mustFind(db.roles, (entry) => entry.id === id, "role");
      Object.assign(role, patch);
      ctx.audit("org.role.update", {
        resourceType: "role",
        resourceId: id,
        summary: `更新角色 ${role.name}`,
      });
      ctx.save();
      return role;
    },
    async deleteRole(id) {
      await ctx.pause();
      const role = ctx.mustFind(db.roles, (entry) => entry.id === id, "role");
      if (role.builtin) throw new Error("cannot delete builtin role");
      db.roles = db.roles.filter((entry) => entry.id !== id);
      ctx.audit("org.role.delete", {
        resourceType: "role",
        resourceId: id,
        summary: `删除角色 ${role.name}`,
      });
      ctx.save();
    },
    async listRoleBindings() {
      await ctx.pause();
      return db.roleBindings.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async createRoleBinding(input) {
      await ctx.pause();
      ctx.mustFind(
        db.roles,
        (entry) => entry.id === input.roleId && entry.enterpriseId === ctx.enterpriseId(),
        "role",
      );
      if (input.subjectType === "user") {
        ctx.mustFind(
          db.memberships,
          (m) => m.userId === input.subjectId && m.enterpriseId === ctx.enterpriseId(),
          "membership",
        );
      } else {
        ctx.mustFind(
          db.departments,
          (entry) => entry.id === input.subjectId && entry.enterpriseId === ctx.enterpriseId(),
          "department",
        );
      }
      if (input.scopeType === "project") {
        // project 范围绑定必须指向当前企业的项目。
        ctx.mustFind(
          db.projects,
          (entry) => entry.id === input.scopeId && entry.enterpriseId === ctx.enterpriseId(),
          "project",
        );
      }
      const binding: RoleBinding = {
        id: nextId(db, "rb"),
        enterpriseId: ctx.enterpriseId(),
        subjectType: input.subjectType,
        subjectId: input.subjectId,
        roleId: input.roleId,
        scopeType: input.scopeType,
        scopeId:
          input.scopeType === "enterprise" ? ctx.enterpriseId() : input.scopeId,
        validFrom: input.validFrom,
        validUntil: input.validUntil,
        status: input.status ?? "active",
        createdAt: ctx.nowIso(),
      };
      db.roleBindings.push(binding);
      ctx.audit("org.role_binding.create", {
        resourceType: "role_binding",
        resourceId: binding.id,
        summary: `创建授权绑定 ${binding.subjectType}:${binding.subjectId} → ${binding.roleId}`,
      });
      ctx.save();
      return binding;
    },
    async updateRoleBinding(id, patch) {
      await ctx.pause();
      const binding = ctx.mustFind(
        db.roleBindings,
        (entry) => entry.id === id && entry.enterpriseId === ctx.enterpriseId(),
        "role binding",
      );
      Object.assign(binding, patch);
      ctx.audit("org.role_binding.update", {
        resourceType: "role_binding",
        resourceId: id,
        summary: `更新授权绑定 ${id}`,
      });
      ctx.save();
      return binding;
    },
    async deleteRoleBinding(id) {
      await ctx.pause();
      const binding = ctx.mustFind(
        db.roleBindings,
        (entry) => entry.id === id && entry.enterpriseId === ctx.enterpriseId(),
        "role binding",
      );
      db.roleBindings = db.roleBindings.filter((entry) => entry.id !== id);
      ctx.audit("org.role_binding.delete", {
        resourceType: "role_binding",
        resourceId: id,
        summary: `删除授权绑定 ${binding.subjectType}:${binding.subjectId} → ${binding.roleId}`,
      });
      ctx.save();
    },
    async listDataScopes() {
      await ctx.pause();
      return db.dataScopes.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async saveDataScope(input) {
      await ctx.pause();
      const existing = input.id
        ? db.dataScopes.find((entry) => entry.id === input.id)
        : undefined;
      if (existing) {
        Object.assign(existing, input);
        ctx.save();
        return existing;
      }
      const scope = {
        ...input,
        id: nextId(db, "ds"),
        enterpriseId: ctx.enterpriseId(),
        createdAt: ctx.nowIso(),
      };
      db.dataScopes.push(scope);
      ctx.audit("org.data_scope.save", {
        resourceType: "data_scope",
        resourceId: scope.id,
        summary: `保存数据范围 ${scope.name}`,
      });
      ctx.save();
      return scope;
    },
    async deleteDataScope(id) {
      await ctx.pause();
      db.dataScopes = db.dataScopes.filter((entry) => entry.id !== id);
      ctx.save();
    },
    async listApprovalPolicies() {
      await ctx.pause();
      return db.approvalPolicies.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async saveApprovalPolicy(input) {
      await ctx.pause();
      const existing = input.id
        ? db.approvalPolicies.find((entry) => entry.id === input.id)
        : undefined;
      if (existing) {
        Object.assign(existing, input);
        ctx.save();
        return existing;
      }
      const policy = {
        ...input,
        id: nextId(db, "ap"),
        enterpriseId: ctx.enterpriseId(),
        createdAt: ctx.nowIso(),
      };
      db.approvalPolicies.push(policy);
      ctx.audit("org.approval_policy.save", {
        resourceType: "approval_policy",
        resourceId: policy.id,
        summary: `保存审批策略 ${policy.name}`,
      });
      ctx.save();
      return policy;
    },
    async listServiceAccounts() {
      await ctx.pause();
      return db.serviceAccounts.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async createServiceAccount(input) {
      await ctx.pause();
      const account = {
        id: nextId(db, "sa"),
        enterpriseId: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        roleIds: input.roleIds ?? [],
        status: "active" as const,
        createdAt: ctx.nowIso(),
      };
      db.serviceAccounts.push(account);
      ctx.audit("org.service_account.create", {
        resourceType: "service_account",
        resourceId: account.id,
        summary: `创建服务账号 ${account.name}`,
      });
      ctx.save();
      return account;
    },
    async updateServiceAccount(id, patch) {
      await ctx.pause();
      const account = ctx.mustFind(
        db.serviceAccounts,
        (entry) => entry.id === id,
        "service account",
      );
      Object.assign(account, patch);
      ctx.save();
      return account;
    },
    async listApiKeys(serviceAccountId) {
      await ctx.pause();
      return db.apiKeys.filter(
        (entry) => entry.serviceAccountId === serviceAccountId,
      );
    },
    async createApiKey(serviceAccountId, input) {
      await ctx.pause();
      ctx.mustFind(
        db.serviceAccounts,
        (entry) => entry.id === serviceAccountId,
        "service account",
      );
      const secret = `argus_sk_${Math.random().toString(16).slice(2, 18)}`;
      const apiKey = {
        id: nextId(db, "ak"),
        enterpriseId: ctx.enterpriseId(),
        serviceAccountId,
        name: input.name,
        prefix: secret.slice(0, 12),
        scopes: input.scopes ?? [],
        status: "active" as const,
        expiresAt: input.expiresAt,
        createdAt: ctx.nowIso(),
      };
      db.apiKeys.push(apiKey);
      ctx.audit("org.api_key.create", {
        resourceType: "api_key",
        resourceId: apiKey.id,
        summary: `创建 API Key ${apiKey.name}`,
      });
      ctx.save();
      return { apiKey, secret };
    },
    async revokeApiKey(id) {
      await ctx.pause();
      const key = ctx.mustFind(db.apiKeys, (entry) => entry.id === id, "api key");
      key.status = "revoked";
      ctx.audit("org.api_key.revoke", {
        resourceType: "api_key",
        resourceId: id,
        summary: `吊销 API Key ${key.name}`,
      });
      ctx.save();
    },
  };
}
