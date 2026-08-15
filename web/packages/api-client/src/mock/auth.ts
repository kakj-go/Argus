import type { ArgusApiClient } from "../client";
import type { SessionInfo, User } from "../types";
import type { MockContext } from "./context";

/** Session lifecycle for mutually exclusive platform or single-enterprise identities. */
export function createAuthDomain(ctx: MockContext): ArgusApiClient["auth"] {
  const { db } = ctx;

  function sessionInfo(user: User): SessionInfo {
    return {
      user,
      membership: db.memberships.find((m) => m.userId === user.id) ?? null,
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
      const membership = db.memberships.find((m) => m.userId === user.id);
      db.session.enterpriseId = membership?.enterpriseId ?? null;
      user.lastLoginAt = ctx.nowIso();
      ctx.audit("auth.login", {
        summary: `${user.displayName} 登录`,
        platform: Boolean(user.platformRole),
        origin: user.platformRole ? "platform_ui" : "admin_ui",
      });
      ctx.save();
      return sessionInfo(user);
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
