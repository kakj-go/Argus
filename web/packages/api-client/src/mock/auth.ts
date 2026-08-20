import type { ArgusApiClient } from "../client";
import type { SessionInfo, User } from "../types";
import type { MockContext } from "./context";

/** Session lifecycle for mutually exclusive platform or single-enterprise identities. */
export function createAuthDomain(ctx: MockContext): ArgusApiClient["auth"] {
  const { db } = ctx;
  const breakGlassSessions: Awaited<
    ReturnType<ArgusApiClient["auth"]["listBreakGlassSessions"]>
  > = [];
  let stepUpExpiresAt: string | undefined;

  function withStepUp(session: SessionInfo): SessionInfo {
    return stepUpExpiresAt
      ? { ...session, step_up_expires_at: stepUpExpiresAt }
      : session;
  }

  function sessionInfo(user: User): SessionInfo {
    const enterpriseUser = db.enterpriseUsers.find(
      (entry) => entry.userId === user.id,
    );
    const now = ctx.nowIso();
    const expiresAt = new Date(Date.now() + 8 * 60 * 60 * 1000).toISOString();
    if (user.platformRole) {
      return withStepUp({
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
        amr: ["password"],
        mfa_state: user.mfaEnabled ? "enabled" : "enrollment_required",
        authenticated_at: now,
      });
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
    return withStepUp({
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
      amr: ["password"],
      mfa_state: user.mfaEnabled ? "enabled" : "disabled",
      authenticated_at: now,
    });
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
      stepUpExpiresAt = undefined;
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
    async completeMfaLogin() {
      const user = db.users.find((entry) => entry.id === db.session.userId);
      if (!user) throw new Error("invalid MFA challenge");
      return { ...sessionInfo(user), amr: ["password", "totp"] };
    },
    async enrollTotp() {
      return { enrollment_id: "mock-mfa-enrollment", secret: "JBSWY3DPEHPK3PXP", otpauth_uri: "otpauth://totp/Argus%3Amock?secret=JBSWY3DPEHPK3PXP", expires_at: new Date(Date.now() + 600_000).toISOString() };
    },
    async verifyTotpEnrollment() {
      const user = db.users.find((entry) => entry.id === db.session.userId);
      if (!user) throw new Error("unauthenticated");
      user.mfaEnabled = true;
      ctx.save();
      return { codes: Array.from({ length: 10 }, (_, index) => `MOCK-${String(index + 1).padStart(4, "0")}-CODE`), generated_at: ctx.nowIso() };
    },
    async regenerateRecoveryCodes() {
      return { codes: Array.from({ length: 10 }, (_, index) => `MOCK-${String(index + 1).padStart(4, "0")}-NEWC`), generated_at: ctx.nowIso() };
    },
    async disableTotp() {
      const user = db.users.find((entry) => entry.id === db.session.userId);
      if (!user || user.platformRole) throw new Error("MFA cannot be disabled");
      user.mfaEnabled = false;
      stepUpExpiresAt = undefined;
      ctx.save();
    },
    async stepUp() {
      stepUpExpiresAt = new Date(Date.now() + 300_000).toISOString();
      return { expires_at: stepUpExpiresAt, amr: ["password", "totp"] };
    },
    async listBreakGlassSessions() {
      return breakGlassSessions.slice();
    },
    async createBreakGlassSession(input) {
      const user = db.users.find((entry) => entry.id === db.session.userId);
      const subjectRecord = db.enterpriseUsers.find((entry) => entry.userId === user?.id);
      if (!user || !subjectRecord) throw new Error("unauthenticated");
      const created = { id: crypto.randomUUID(), enterprise_id: subjectRecord.enterpriseId, user_id: user.id, reason: input.reason, ticket_ref: input.ticket_ref, status: "active" as const, expires_at: new Date(Date.now() + 900_000).toISOString(), created_at: ctx.nowIso() };
      breakGlassSessions.push(created);
      return created;
    },
    async revokeBreakGlassSession(id) {
      const index = breakGlassSessions.findIndex((entry) => entry.id === id);
      const current = breakGlassSessions[index];
      if (current) {
        breakGlassSessions[index] = {
          ...current,
          status: "revoked",
        };
      }
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
      stepUpExpiresAt = undefined;
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
      stepUpExpiresAt = undefined;
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
