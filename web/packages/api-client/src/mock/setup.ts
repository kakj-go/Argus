import type { ArgusApiClient } from "../client";
import type { User } from "../types";
import type { MockContext } from "./context";
import { nextId } from "./store";

/** One-time platform initialization wizard. */
export function createSetupDomain(ctx: MockContext): ArgusApiClient["setup"] {
  const { db } = ctx;

  return {
    async status() {
      await ctx.pause();
      return {
        state: db.platformState.state,
        platformName: db.platformState.name || undefined,
      };
    },
    async submit(input) {
      await ctx.pause();
      if (db.platformState.state !== "uninitialized") {
        throw new Error("platform already initialized");
      }
      if (!input.setupToken) throw new Error("invalid setup token");
      const admin: User = {
        id: nextId(db, "u"),
        username: input.superAdmin.username,
        displayName: input.superAdmin.displayName,
        email: input.superAdmin.email,
        platformRole: "platform_super_admin",
        status: "active",
        mfaEnabled: false,
        createdAt: ctx.nowIso(),
      };
      db.users.push(admin);
      db.credentials[admin.id] = input.superAdmin.password;
      db.platformState = { state: "initialized", name: input.platformName };
      db.auditEvents.unshift({
        id: nextId(db, "aud"),
        enterpriseId: null,
        actorUserId: admin.id,
        actorName: admin.displayName,
        action: "setup.initialize",
        origin: "platform_ui",
        summary: "平台初始化完成",
        result: "success",
        createdAt: ctx.nowIso(),
      });
      ctx.save();
      return { success: true, superAdminUserId: admin.id };
    },
  };
}
