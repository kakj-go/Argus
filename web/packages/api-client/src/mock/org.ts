import type { ArgusApiClient } from "../client";
import type {
  DataAuthorizationPage,
  DataAuthorizationResource,
  DataAuthorizationResourceType,
  DataAuthorizationSubjectType,
  RoleBinding,
  User,
  UserRoleAssignments,
} from "../types";
import { ApiError } from "../transport/errors";
import type { MockContext } from "./context";
import { nextId } from "./store";

/** Organization: users, departments, roles, explicit resource grants, policies, API keys. */
export function createOrgDomain(ctx: MockContext): ArgusApiClient["org"] {
  const { db } = ctx;
  const grants = () => (db.dataAuthorizationGrants ??= []);
  const versions = () => (db.dataAuthorizationVersions ??= {});
  const versionKey = (type: DataAuthorizationSubjectType, id: string) =>
    `${type}:${id}`;
  const roleVersions = () => (db.roleAssignmentVersions ??= {});
  const roleVersion = (userId: string) => roleVersions()[userId] ?? 1;
  const effectiveBinding = (binding: RoleBinding) => {
    const now = ctx.nowIso();
    return (
      binding.status === "active" &&
      (!binding.valid_from || binding.valid_from <= now) &&
      (!binding.valid_until || binding.valid_until > now)
    );
  };
  const assignments = (userId: string): UserRoleAssignments => {
    const enterpriseUserRecord = ctx.mustFind(
      db.enterpriseUsers,
      (entry) =>
        entry.userId === userId && entry.enterpriseId === ctx.enterpriseId(),
      "enterpriseUser",
    );
    const department = ctx.mustFind(
      db.departments,
      (entry) =>
        entry.id === enterpriseUserRecord.departmentId &&
        entry.enterprise_id === ctx.enterpriseId(),
      "department",
    );
    const direct = db.roleBindings
      .filter(
        (binding) =>
          binding.enterprise_id === ctx.enterpriseId() &&
          binding.subject_type === "user" &&
          binding.subject_id === userId &&
          effectiveBinding(binding),
      )
      .map((binding) => binding.role_id);
    const inherited = db.roleBindings
      .filter(
        (binding) =>
          binding.enterprise_id === ctx.enterpriseId() &&
          binding.subject_type === "department" &&
          binding.subject_id === enterpriseUserRecord.departmentId &&
          effectiveBinding(binding),
      )
      .map((binding) => ({
        role_id: binding.role_id,
        source_type: "department" as const,
        source_id: department.id,
        source_name: department.name,
      }));
    return {
      direct_role_ids: [...new Set(direct)].sort(),
      inherited_roles: inherited,
      effective_role_ids: [
        ...new Set([...direct, ...inherited.map((item) => item.role_id)]),
      ].sort(),
      authorization_version: roleVersion(userId),
    };
  };

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
      const user = ctx.mustFind(
        db.users,
        (entry) => entry.id === userId,
        "user",
      );
      return {
        id: user.id,
        enterprise_id: record.enterpriseId,
        department_id: record.departmentId,
        username: user.username,
        display_name: user.displayName,
        email: user.email,
        status: user.status,
        mfa_enabled: user.mfaEnabled,
        authorization_version: roleVersion(userId),
        version: user.version ?? 1,
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
      const user = ctx.mustFind(
        db.users,
        (entry) => entry.id === userId,
        "user",
      );
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
      const user = ctx.mustFind(
        db.users,
        (entry) => entry.id === userId,
        "user",
      );
      if (patch.department_id !== undefined) {
        enterpriseUser.departmentId = patch.department_id;
      }
      if (patch.display_name !== undefined) {
        user.displayName = patch.display_name;
      }
      if (patch.email !== undefined) {
        user.email = patch.email ?? undefined;
      }
      if (patch.status !== undefined) {
        user.status = patch.status;
        if (patch.status === "disabled" && db.session.userId === userId) {
          db.session.userId = null;
          db.session.enterpriseId = null;
        }
      }
      user.version = (user.version ?? 1) + 1;
      roleVersions()[userId] = roleVersion(userId) + 1;
      ctx.audit("org.enterpriseUser.update", {
        resourceType: "user",
        resourceId: userId,
        summary: `更新企业用户 ${user.displayName}`,
      });
      ctx.save();
      return {
        id: user.id,
        enterprise_id: enterpriseUser.enterpriseId,
        department_id: enterpriseUser.departmentId,
        username: user.username,
        display_name: user.displayName,
        email: user.email,
        status: user.status,
        mfa_enabled: user.mfaEnabled,
        authorization_version: roleVersion(userId),
        version: user.version,
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
      const department = ctx.mustFind(
        db.departments,
        (entry) => entry.id === id,
        "department",
      );
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
      const department = ctx.mustFind(
        db.departments,
        (entry) => entry.id === id,
        "department",
      );
      if (department.is_default)
        throw new Error("cannot disable default department");
      if (db.enterpriseUsers.some((entry) => entry.departmentId === id)) {
        throw new Error("cannot disable department with members");
      }
      department.status = "disabled";
      department.version += 1;
      department.updated_at = ctx.nowIso();
      ctx.audit("org.department.disable", {
        resourceType: "department",
        resourceId: id,
        summary: `禁用部门 ${department.name}`,
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
        (entry) =>
          entry.id === input.role_id &&
          entry.enterprise_id === ctx.enterpriseId(),
        "role",
      );
      if (input.subject_type === "user") {
        ctx.mustFind(
          db.enterpriseUsers,
          (m) =>
            m.userId === input.subject_id &&
            m.enterpriseId === ctx.enterpriseId(),
          "enterpriseUser",
        );
      } else if (input.subject_type === "department") {
        ctx.mustFind(
          db.departments,
          (entry) =>
            entry.id === input.subject_id &&
            entry.enterprise_id === ctx.enterpriseId(),
          "department",
        );
      } else {
        ctx.mustFind(
          db.serviceAccounts,
          (entry) =>
            entry.id === input.subject_id &&
            entry.enterprise_id === ctx.enterpriseId(),
          "service account",
        );
      }
      const binding: RoleBinding = {
        id: nextId(db, "rb"),
        enterprise_id: ctx.enterpriseId(),
        subject_type: input.subject_type,
        subject_id: input.subject_id,
        role_id: input.role_id,
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
        (entry) =>
          entry.id === id && entry.enterprise_id === ctx.enterpriseId(),
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
        (entry) =>
          entry.id === id && entry.enterprise_id === ctx.enterpriseId(),
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
    async getUserRoleAssignments(userId) {
      await ctx.pause();
      return assignments(userId);
    },
    async replaceUserRoleAssignments(
      userId,
      departmentId,
      roleIds,
      expectedUserVersion,
      expectedAuthorizationVersion,
    ) {
      await ctx.pause();
      const enterpriseUserRecord = ctx.mustFind(
        db.enterpriseUsers,
        (entry) =>
          entry.userId === userId && entry.enterpriseId === ctx.enterpriseId(),
        "enterpriseUser",
      );
      const user = ctx.mustFind(
        db.users,
        (entry) => entry.id === userId,
        "user",
      );
      const current = assignments(userId);
      if (
        (user.version ?? 1) !== expectedUserVersion ||
        current.authorization_version !== expectedAuthorizationVersion
      ) {
        throw new ApiError(
          {
            code: "AUTHORIZATION_VERSION_STALE",
            message_key: "errors.auth.authorization_version_stale",
            request_id: `mock-${Date.now()}`,
            retryable: false,
          },
          409,
        );
      }
      ctx.mustFind(
        db.departments,
        (entry) =>
          entry.id === departmentId &&
          entry.enterprise_id === ctx.enterpriseId(),
        "department",
      );
      const nextRoleIds = [...new Set(roleIds)];
      if (
        nextRoleIds.some(
          (roleId) =>
            !db.roles.some(
              (role) =>
                role.id === roleId &&
                role.enterprise_id === ctx.enterpriseId() &&
                role.status === "active",
            ),
        )
      ) {
        throw new ApiError(
          {
            code: "INVALID_ARGUMENT",
            message_key: "errors.common.invalid_argument",
            request_id: `mock-${Date.now()}`,
            retryable: false,
          },
          422,
        );
      }
      const departmentChanged =
        enterpriseUserRecord.departmentId !== departmentId;
      const rolesChanged =
        current.direct_role_ids.length !== nextRoleIds.length ||
        current.direct_role_ids.some((roleId) => !nextRoleIds.includes(roleId));
      if (!departmentChanged && !rolesChanged) return current;

      const beforeBindings = db.roleBindings.map((binding) => ({ ...binding }));
      const beforeDepartmentId = enterpriseUserRecord.departmentId;
      const beforeUserVersion = user.version;
      const beforeAuthorizationVersion = current.authorization_version;
      const now = ctx.nowIso();
      if (rolesChanged) {
        for (const binding of db.roleBindings) {
          if (
            binding.enterprise_id === ctx.enterpriseId() &&
            binding.subject_type === "user" &&
            binding.subject_id === userId &&
            binding.status === "active" &&
            !nextRoleIds.includes(binding.role_id)
          ) {
            binding.status = "disabled";
            binding.version += 1;
            binding.updated_at = now;
          }
        }
        for (const roleId of nextRoleIds) {
          const existing = db.roleBindings.find(
            (binding) =>
              binding.enterprise_id === ctx.enterpriseId() &&
              binding.subject_type === "user" &&
              binding.subject_id === userId &&
              binding.role_id === roleId,
          );
          if (existing) {
            existing.status = "active";
            existing.valid_from = undefined;
            existing.valid_until = undefined;
            existing.version += 1;
            existing.updated_at = now;
          } else {
            db.roleBindings.push({
              id: nextId(db, "rb"),
              enterprise_id: ctx.enterpriseId(),
              subject_type: "user",
              subject_id: userId,
              role_id: roleId,
              status: "active",
              version: 1,
              created_at: now,
              updated_at: now,
            });
          }
        }
      }
      enterpriseUserRecord.departmentId = departmentId;
      const managerCount = db.enterpriseUsers.filter((enterpriseUser) => {
        const user = db.users.find(
          (entry) => entry.id === enterpriseUser.userId,
        );
        if (
          enterpriseUser.enterpriseId !== ctx.enterpriseId() ||
          user?.status !== "active"
        )
          return false;
        const effective = assignments(enterpriseUser.userId).effective_role_ids;
        const permissions = new Set(
          db.roles
            .filter((role) => effective.includes(role.id))
            .flatMap((role) => role.permissions),
        );
        return (
          (permissions.has("*") || permissions.has("identity.manage")) &&
          (permissions.has("*") || permissions.has("role.manage"))
        );
      }).length;
      if (managerCount === 0) {
        db.roleBindings.splice(0, db.roleBindings.length, ...beforeBindings);
        enterpriseUserRecord.departmentId = beforeDepartmentId;
        user.version = beforeUserVersion;
        roleVersions()[userId] = beforeAuthorizationVersion;
        throw new ApiError(
          {
            code: "LAST_IAM_ADMIN_REQUIRED",
            message_key: "errors.role.last_iam_admin_required",
            request_id: `mock-${Date.now()}`,
            retryable: false,
          },
          409,
        );
      }
      if (departmentChanged) user.version = (user.version ?? 1) + 1;
      roleVersions()[userId] = current.authorization_version + 1;
      ctx.audit("role_assignment.replace", {
        resourceType: "enterprise_user",
        resourceId: userId,
        summary: "更新用户角色绑定",
      });
      ctx.save();
      return assignments(userId);
    },
    async listDataAuthorization(
      subjectType: DataAuthorizationSubjectType,
      subjectId: string,
      resourceType: DataAuthorizationResourceType,
      cursor?: string,
      limit?: number,
    ): Promise<DataAuthorizationPage> {
      await ctx.pause();
      const direct = new Set(
        grants()
          .filter(
            (grant) =>
              grant.subject_type === subjectType &&
              grant.subject_id === subjectId &&
              grant.resource_type === resourceType &&
              grant.active,
          )
          .map((grant) => grant.resource_id),
      );
      const inherited = new Set<string>();
      const inheritedSources = new Map<string, string[]>();
      const addSource = (resourceId: string, source: string) =>
        inheritedSources.set(resourceId, [
          ...(inheritedSources.get(resourceId) ?? []),
          source,
        ]);
      const addRoleGrantsWithSources = (roleIds: string[]) =>
        grants()
          .filter(
            (grant) =>
              grant.subject_type === "role" &&
              roleIds.includes(grant.subject_id) &&
              grant.resource_type === resourceType &&
              grant.active,
          )
          .forEach((grant) => {
            inherited.add(grant.resource_id);
            const role = db.roles.find(
              (entry) => entry.id === grant.subject_id,
            );
            addSource(
              grant.resource_id,
              `角色：${role?.name ?? grant.subject_id}`,
            );
          });
      if (subjectType === "user") {
        const enterpriseUser = db.enterpriseUsers.find(
          (entry) =>
            entry.enterpriseId === ctx.enterpriseId() &&
            entry.userId === subjectId,
        );
        if (enterpriseUser) {
          const roleIds = db.roleBindings
            .filter(
              (binding) =>
                binding.enterprise_id === ctx.enterpriseId() &&
                binding.status === "active" &&
                ((binding.subject_type === "user" &&
                  binding.subject_id === subjectId) ||
                  (binding.subject_type === "department" &&
                    binding.subject_id === enterpriseUser.departmentId)),
            )
            .map((binding) => binding.role_id);
          addRoleGrantsWithSources(roleIds);
          const department = db.departments.find(
            (entry) => entry.id === enterpriseUser.departmentId,
          );
          grants()
            .filter(
              (grant) =>
                grant.subject_type === "department" &&
                grant.subject_id === enterpriseUser.departmentId &&
                grant.resource_type === resourceType &&
                grant.active,
            )
            .forEach((grant) => {
              inherited.add(grant.resource_id);
              addSource(
                grant.resource_id,
                `部门：${department?.name ?? enterpriseUser.departmentId}`,
              );
            });
        }
      } else if (subjectType === "department") {
        addRoleGrantsWithSources(
          db.roleBindings
            .filter(
              (binding) =>
                binding.enterprise_id === ctx.enterpriseId() &&
                binding.subject_type === "department" &&
                binding.subject_id === subjectId &&
                binding.status === "active",
            )
            .map((binding) => binding.role_id),
        );
      } else if (subjectType === "service_account") {
        addRoleGrantsWithSources(
          db.roleBindings
            .filter(
              (binding) =>
                binding.enterprise_id === ctx.enterpriseId() &&
                binding.subject_type === "service_account" &&
                binding.subject_id === subjectId &&
                binding.status === "active",
            )
            .map((binding) => binding.role_id),
        );
      }
      const catalog: DataAuthorizationResource[] =
        resourceType === "host"
          ? db.hosts
              .filter((host) => host.enterpriseId === ctx.enterpriseId())
              .map((host) => ({
                resource_type: resourceType,
                resource_id: host.id,
                name: host.name,
                direct: direct.has(host.id),
                inherited: !direct.has(host.id) && inherited.has(host.id),
                sources: direct.has(host.id)
                  ? ["直接"]
                  : (inheritedSources.get(host.id) ?? []),
              }))
          : db.clusters
              .filter((cluster) => cluster.enterpriseId === ctx.enterpriseId())
              .map((cluster) => ({
                resource_type: resourceType,
                resource_id: cluster.id,
                name: cluster.name,
                direct: direct.has(cluster.id),
                inherited: !direct.has(cluster.id) && inherited.has(cluster.id),
                sources: direct.has(cluster.id)
                  ? ["直接"]
                  : (inheritedSources.get(cluster.id) ?? []),
              }));
      const offset = Math.max(0, Number.parseInt(cursor ?? "0", 10) || 0);
      const pageSize = Math.min(Math.max(limit ?? 50, 1), 200);
      const items = catalog.slice(offset, offset + pageSize);
      const nextOffset = offset + items.length;
      const hasMore = nextOffset < catalog.length;
      const affectedMembers = new Set<string>();
      if (subjectType === "role") {
        db.enterpriseUsers
          .filter(
            (entry) =>
              entry.enterpriseId === ctx.enterpriseId() &&
              db.roleBindings.some(
                (binding) =>
                  binding.enterprise_id === ctx.enterpriseId() &&
                  binding.role_id === subjectId &&
                  binding.status === "active" &&
                  ((binding.subject_type === "user" &&
                    binding.subject_id === entry.userId) ||
                    (binding.subject_type === "department" &&
                      binding.subject_id === entry.departmentId)),
              ),
          )
          .forEach((entry) => affectedMembers.add(`user:${entry.userId}`));
        db.serviceAccounts
          .filter(
            (entry) =>
              entry.enterprise_id === ctx.enterpriseId() &&
              db.roleBindings.some(
                (binding) =>
                  binding.enterprise_id === ctx.enterpriseId() &&
                  binding.role_id === subjectId &&
                  binding.subject_type === "service_account" &&
                  binding.subject_id === entry.id &&
                  binding.status === "active",
              ),
          )
          .forEach((entry) =>
            affectedMembers.add(`service_account:${entry.id}`),
          );
      }
      return {
        items,
        page: {
          next_cursor: hasMore ? String(nextOffset) : null,
          has_more: hasMore,
        },
        authorization_version:
          versions()[versionKey(subjectType, subjectId)] ?? 1,
        affected_member_count:
          subjectType === "role" ? affectedMembers.size : 1,
      };
    },
    async updateDataAuthorization(
      subjectType: DataAuthorizationSubjectType,
      subjectId: string,
      resourceType: DataAuthorizationResourceType,
      resourceIds: string[],
      remove: boolean,
      expectedVersion: number,
    ): Promise<void> {
      await ctx.pause();
      const key = versionKey(subjectType, subjectId);
      const current = versions()[key] ?? 1;
      if (current !== expectedVersion)
        throw new Error("authorization version conflict");
      const values = grants();
      for (const resourceId of resourceIds) {
        const existing = values.find(
          (grant) =>
            grant.subject_type === subjectType &&
            grant.subject_id === subjectId &&
            grant.resource_type === resourceType &&
            grant.resource_id === resourceId,
        );
        if (remove) {
          if (existing) existing.active = false;
        } else if (existing) {
          existing.active = true;
        } else {
          values.push({
            subject_type: subjectType,
            subject_id: subjectId,
            resource_type: resourceType,
            resource_id: resourceId,
            active: true,
          });
        }
      }
      versions()[key] = current + 1;
      ctx.audit("org.data_authorization.update", {
        resourceType,
        resourceId: subjectId,
        summary: `${subjectType}:${subjectId} 数据授权变更`,
      });
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
      const account = {
        id: nextId(db, "sa"),
        enterprise_id: ctx.enterpriseId(),
        name: input.name,
        description: input.description,
        allowed_tool_ids: input.allowed_tool_ids ?? [],
        status: "active" as const,
        authorization_version: 1,
        version: 1,
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
      const prefix = crypto.randomUUID().replaceAll("-", "").slice(0, 8);
      const secret = `argus_ak_${prefix}.${crypto.randomUUID().replaceAll("-", "")}${crypto.randomUUID().replaceAll("-", "")}`;
      const apiKey = {
        id: nextId(db, "ak"),
        enterprise_id: ctx.enterpriseId(),
        service_account_id: serviceAccountId,
        name: input.name,
        prefix,
        status: "active" as const,
        version: 1,
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
    async rotateApiKey(id) {
      await ctx.pause();
      const previous = ctx.mustFind(
        db.apiKeys,
        (entry) => entry.id === id,
        "api key",
      );
      previous.status = "revoked";
      previous.version += 1;
      const prefix = crypto.randomUUID().replaceAll("-", "").slice(0, 8);
      const secret = `argus_ak_${prefix}.${crypto.randomUUID().replaceAll("-", "")}${crypto.randomUUID().replaceAll("-", "")}`;
      const apiKey = {
        ...previous,
        id: nextId(db, "ak"),
        prefix,
        status: "active" as const,
        version: 1,
        created_at: ctx.nowIso(),
      };
      db.apiKeys.push(apiKey);
      ctx.audit("org.api_key.rotate", {
        resourceType: "api_key",
        resourceId: apiKey.id,
        summary: `轮换 API Key ${apiKey.name}`,
      });
      ctx.save();
      return { api_key: apiKey, secret };
    },
    async revokeApiKey(id) {
      await ctx.pause();
      const key = ctx.mustFind(
        db.apiKeys,
        (entry) => entry.id === id,
        "api key",
      );
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
