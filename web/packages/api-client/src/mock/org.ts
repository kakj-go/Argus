import type { ArgusApiClient } from "../client";
import type { RoleBinding, User } from "../types";
import type { MockContext } from "./context";
import { nextId } from "./store";

/** Organization: users, departments, roles, bindings, data scopes, policies, API keys. */
export function createOrgDomain(ctx: MockContext): ArgusApiClient["org"] {
  const { db } = ctx;

  return {
    async listUsers() {
      await ctx.pause();
      const memberIds = new Set(
        db.enterpriseUsers
          .filter((m) => m.enterpriseId === ctx.enterpriseId())
          .map((m) => m.userId),
      );
      return db.users.filter((entry) => memberIds.has(entry.id));
    },
    async getEnterpriseUser(userId) {
      await ctx.pause();
      const record = db.enterpriseUsers.find(
          (m) => m.userId === userId && m.enterpriseId === ctx.enterpriseId(),
        );
      if (!record) return null;
      const user = ctx.mustFind(db.users, (entry) => entry.id === userId, "user");
      return {
        id: user.id,
        enterprise_id: record.enterpriseId,
        department_id: record.departmentId,
        username: user.username,
        display_name: user.displayName,
        email: user.email,
        status: user.status,
        mfa_enabled: user.mfaEnabled,
        authorization_version: 1,
        last_login_at: user.lastLoginAt,
        created_at: user.createdAt,
        updated_at: user.createdAt,
      };
    },
    async inviteUser(input) {
      await ctx.pause();
      const user: User = {
        id: nextId(db, "u"),
        username: input.username,
        displayName: input.display_name,
        email: input.email,
        status: "invited",
        mfaEnabled: false,
        createdAt: ctx.nowIso(),
      };
      db.users.push(user);
      db.enterpriseUsers.push({
        userId: user.id,
        enterpriseId: ctx.enterpriseId(),
        departmentId: input.department_id,
      });
      // 邀请时勾选的角色转为企业范围 RoleBinding。
      for (const role_id of input.role_ids ?? []) {
        const binding: RoleBinding = {
          id: nextId(db, "rb"),
          enterprise_id: ctx.enterpriseId(),
          subject_type: "user",
          subject_id: user.id,
          role_id,
          data_scope_ids: [],
          status: "active",
          version: 1,
          created_at: ctx.nowIso(),
          updated_at: ctx.nowIso(),
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
    async updateEnterpriseUser(userId, patch) {
      await ctx.pause();
      const enterpriseUser = ctx.mustFind(
        db.enterpriseUsers,
        (m) => m.userId === userId && m.enterpriseId === ctx.enterpriseId(),
        "enterpriseUser",
      );
      Object.assign(enterpriseUser, patch);
      ctx.audit("org.enterpriseUser.update", {
        resourceType: "user",
        resourceId: userId,
        summary: `更新用户部门 ${userId}`,
      });
      ctx.save();
      const user = ctx.mustFind(db.users, (entry) => entry.id === userId, "user");
      return {
        id: user.id,
        enterprise_id: enterpriseUser.enterpriseId,
        department_id: enterpriseUser.departmentId,
        username: user.username,
        display_name: user.displayName,
        email: user.email,
        status: user.status,
        mfa_enabled: user.mfaEnabled,
        authorization_version: 1,
        last_login_at: user.lastLoginAt,
        created_at: user.createdAt,
        updated_at: ctx.nowIso(),
      };
    },
    async listDepartments() {
      await ctx.pause();
      return db.departments.filter(
        (entry) => entry.enterprise_id === ctx.enterpriseId(),
      );
    },
    async createDepartment(input) {
      await ctx.pause();
      const department = {
        id: nextId(db, "dept"),
        enterprise_id: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        is_default: false,
        status: "active" as const,
        version: 1,
        created_at: ctx.nowIso(),
        updated_at: ctx.nowIso(),
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
      department.version += 1;
      department.updated_at = ctx.nowIso();
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
      if (department.is_default) throw new Error("cannot delete default department");
      if (db.enterpriseUsers.some((entry) => entry.departmentId === id)) {
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
    async listRoles() {
      await ctx.pause();
      return db.roles.filter(
        (entry) => entry.enterprise_id === ctx.enterpriseId(),
      );
    },
    async createRole(input) {
      await ctx.pause();
      const role = {
        id: nextId(db, "role"),
        enterprise_id: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        builtin: false,
        permissions: input.permissions,
        status: "active" as const,
        version: 1,
        created_at: ctx.nowIso(),
        updated_at: ctx.nowIso(),
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
      role.version += 1;
      role.updated_at = ctx.nowIso();
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
        (entry) => entry.enterprise_id === ctx.enterpriseId(),
      );
    },
    async createRoleBinding(input) {
      await ctx.pause();
      ctx.mustFind(
        db.roles,
        (entry) => entry.id === input.role_id && entry.enterprise_id === ctx.enterpriseId(),
        "role",
      );
      if (input.subject_type === "user") {
        ctx.mustFind(
          db.enterpriseUsers,
          (m) => m.userId === input.subject_id && m.enterpriseId === ctx.enterpriseId(),
          "enterpriseUser",
        );
      } else if (input.subject_type === "department") {
        ctx.mustFind(
          db.departments,
          (entry) => entry.id === input.subject_id && entry.enterprise_id === ctx.enterpriseId(),
          "department",
        );
      } else {
        ctx.mustFind(
          db.serviceAccounts,
          (entry) => entry.id === input.subject_id && entry.enterprise_id === ctx.enterpriseId(),
          "service account",
        );
      }
      for (const dataScopeId of input.data_scope_ids) {
        ctx.mustFind(
          db.dataScopes,
          (entry) => entry.id === dataScopeId && entry.enterprise_id === ctx.enterpriseId(),
          "data scope",
        );
      }
      const binding: RoleBinding = {
        id: nextId(db, "rb"),
        enterprise_id: ctx.enterpriseId(),
        subject_type: input.subject_type,
        subject_id: input.subject_id,
        role_id: input.role_id,
        data_scope_ids: [...input.data_scope_ids],
        valid_from: input.valid_from,
        valid_until: input.valid_until,
        status: input.status ?? "active",
        version: 1,
        created_at: ctx.nowIso(),
        updated_at: ctx.nowIso(),
      };
      db.roleBindings.push(binding);
      ctx.audit("org.role_binding.create", {
        resourceType: "role_binding",
        resourceId: binding.id,
        summary: `创建授权绑定 ${binding.subject_type}:${binding.subject_id} → ${binding.role_id}`,
      });
      ctx.save();
      return binding;
    },
    async updateRoleBinding(id, patch) {
      await ctx.pause();
      const binding = ctx.mustFind(
        db.roleBindings,
        (entry) => entry.id === id && entry.enterprise_id === ctx.enterpriseId(),
        "role binding",
      );
      Object.assign(binding, patch);
      binding.version += 1;
      binding.updated_at = ctx.nowIso();
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
        (entry) => entry.id === id && entry.enterprise_id === ctx.enterpriseId(),
        "role binding",
      );
      db.roleBindings = db.roleBindings.filter((entry) => entry.id !== id);
      ctx.audit("org.role_binding.delete", {
        resourceType: "role_binding",
        resourceId: id,
        summary: `删除授权绑定 ${binding.subject_type}:${binding.subject_id} → ${binding.role_id}`,
      });
      ctx.save();
    },
    async listDataScopes() {
      await ctx.pause();
      return db.dataScopes.filter(
        (entry) => entry.enterprise_id === ctx.enterpriseId(),
      );
    },
    async saveDataScope(input) {
      await ctx.pause();
      const existing = input.id
        ? db.dataScopes.find((entry) => entry.id === input.id)
        : undefined;
      if (existing) {
        Object.assign(existing, input);
        existing.status = input.status ?? existing.status;
        existing.version += 1;
        existing.updated_at = ctx.nowIso();
        ctx.save();
        return existing;
      }
      const now = ctx.nowIso();
      const scope = {
        ...input,
        id: nextId(db, "ds"),
        enterprise_id: ctx.enterpriseId(),
        status: input.status ?? ("active" as const),
        version: 1,
        created_at: now,
        updated_at: now,
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
        (entry) => entry.enterprise_id === ctx.enterpriseId(),
      );
    },
    async createServiceAccount(input) {
      await ctx.pause();
      for (const dataScopeId of input.data_scope_ids ?? []) {
        ctx.mustFind(
          db.dataScopes,
          (entry) => entry.id === dataScopeId && entry.enterprise_id === ctx.enterpriseId(),
          "data scope",
        );
      }
      const account = {
        id: nextId(db, "sa"),
        enterprise_id: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        allowed_tool_ids: input.allowed_tool_ids ?? [],
        data_scope_ids: input.data_scope_ids ?? [],
        status: "active" as const,
        authorization_version: 1,
        created_at: ctx.nowIso(),
        updated_at: ctx.nowIso(),
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
      for (const dataScopeId of patch.data_scope_ids ?? []) {
        ctx.mustFind(
          db.dataScopes,
          (entry) => entry.id === dataScopeId && entry.enterprise_id === ctx.enterpriseId(),
          "data scope",
        );
      }
      Object.assign(account, patch);
      account.authorization_version += 1;
      account.updated_at = ctx.nowIso();
      ctx.save();
      return account;
    },
    async listApiKeys(serviceAccountId) {
      await ctx.pause();
      return db.apiKeys.filter(
        (entry) => entry.service_account_id === serviceAccountId,
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
        enterprise_id: ctx.enterpriseId(),
        service_account_id: serviceAccountId,
        name: input.name,
        prefix: secret.slice(0, 12),
        status: "active" as const,
        expires_at: input.expires_at,
        created_at: ctx.nowIso(),
      };
      db.apiKeys.push(apiKey);
      ctx.audit("org.api_key.create", {
        resourceType: "api_key",
        resourceId: apiKey.id,
        summary: `创建 API Key ${apiKey.name}`,
      });
      ctx.save();
      return { api_key: apiKey, secret };
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
