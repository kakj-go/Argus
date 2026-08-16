import type { ArgusApiClient } from "../client";
import type { SessionInfo, User } from "../types";
import type { MockContext } from "./context";

/** Session lifecycle for mutually exclusive platform or single-enterprise identities. */
export function createAuthDomain(ctx: MockContext): ArgusApiClient["auth"] {
  const { db } = ctx;

  function sessionInfo(user: User): SessionInfo {
    const enterpriseUser = db.enterpriseUsers.find(
      (entry) => entry.userId === user.id,
    );
    const now = ctx.nowIso();
    const expiresAt = new Date(Date.now() + 8 * 60 * 60 * 1000).toISOString();
    if (user.platformRole) {
      return {
        session: {
          id: `session-${user.id}`,
          audience: "platform",
          user_id: user.id,
          locale: "zh-CN",
          csrf_required: true,
          issued_at: now,
          expires_at: expiresAt,
        },
        user: {
          id: user.id,
          username: user.username,
          display_name: user.displayName,
          email: user.email,
          role: "platform_super_admin",
          status: user.status === "disabled" ? "disabled" : "active",
          mfa_enabled: user.mfaEnabled,
          version: 1,
          created_at: user.createdAt,
        },
        permissions: ["*"],
      };
    }
    if (!enterpriseUser) throw new Error("enterprise user required");
    const roleIds = db.roleBindings
      .filter(
        (binding) =>
          binding.status === "active" &&
          ((binding.subject_type === "user" &&
            binding.subject_id === user.id) ||
            (binding.subject_type === "department" &&
              binding.subject_id === enterpriseUser.departmentId)),
      )
      .map((binding) => binding.role_id);
    const permissions = [
      ...new Set(
        db.roles
          .filter((role) => roleIds.includes(role.id))
          .flatMap((role) => role.permissions),
      ),
    ];
    return {
      session: {
        id: `session-${user.id}`,
        audience: "enterprise",
        user_id: user.id,
        enterprise_id: enterpriseUser.enterpriseId,
        department_id: enterpriseUser.departmentId,
        authorization_version: 1,
        locale: "zh-CN",
        csrf_required: true,
        issued_at: now,
        expires_at: expiresAt,
      },
      user: {
        id: user.id,
        enterprise_id: enterpriseUser.enterpriseId,
        department_id: enterpriseUser.departmentId,
        username: user.username,
        display_name: user.displayName,
        email: user.email,
        status: user.status,
        mfa_enabled: user.mfaEnabled,
        authorization_version: 1,
        version: 1,
        last_login_at: user.lastLoginAt,
        created_at: user.createdAt,
        updated_at: now,
      },
      permissions,
    };
  }

  return {
    async login(input) {
      await ctx.pause();
      const user = db.users.find((entry) => entry.username === input.username);
      if (
        !user ||
        db.credentials[user.id] !== input.password ||
        user.status === "disabled"
      ) {
        throw new Error("invalid credentials");
      }
      db.session.userId = user.id;
      const enterpriseUser = db.enterpriseUsers.find(
        (m) => m.userId === user.id,
      );
      db.session.enterpriseId = enterpriseUser?.enterpriseId ?? null;
      user.lastLoginAt = ctx.nowIso();
      ctx.audit("auth.login", {
        summary: `${user.displayName} 登录`,
        platform: Boolean(user.platformRole),
        origin: user.platformRole ? "platform_ui" : "admin_ui",
      });
      ctx.save();
      return sessionInfo(user);
    },
    async completePasswordChange(input) {
      await ctx.pause();
      const user = db.users.find(
        (entry) => db.credentials[entry.id] === input.temporary_password,
      );
      if (!user) throw new Error("invalid password change challenge");
      db.credentials[user.id] = input.new_password;
      db.session.userId = user.id;
      const enterpriseUser = db.enterpriseUsers.find(
        (entry) => entry.userId === user.id,
      );
      db.session.enterpriseId = enterpriseUser?.enterpriseId ?? null;
      ctx.save();
      return sessionInfo(user);
    },
    async changePassword(input) {
      await ctx.pause();
      const user = db.users.find((entry) => entry.id === db.session.userId);
      if (!user || db.credentials[user.id] !== input.current_password) {
        throw new Error("invalid credentials");
      }
      db.credentials[user.id] = input.new_password;
      db.session.userId = null;
      db.session.enterpriseId = null;
      ctx.audit("auth.password_change", {
        summary: `${user.displayName} 修改密码`,
        platform: Boolean(user.platformRole),
      });
      ctx.save();
    },
    async logout() {
      await ctx.pause();
      ctx.audit("auth.logout", { summary: "登出" });
      db.session.userId = null;
      db.session.enterpriseId = null;
      ctx.save();
    },
    async me() {
      await ctx.pause();
      const user = db.users.find((entry) => entry.id === db.session.userId);
      if (!user) throw new Error("unauthenticated");
      return sessionInfo(user);
    },
  };
}
