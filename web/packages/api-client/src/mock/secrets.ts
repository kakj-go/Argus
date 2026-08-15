import type { ArgusApiClient } from "../client";
import type { MockContext } from "./context";
import { nextId } from "./store";

/** Secret metadata; values are write-only and every access is audited. */
export function createSecretsDomain(
  ctx: MockContext,
): ArgusApiClient["secrets"] {
  const { db } = ctx;

  return {
    async list(query) {
      await ctx.pause();
      const items = db.secrets.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      return ctx.paginate(items, query);
    },
    async get(id) {
      await ctx.pause();
      const secret = ctx.mustFind(
        db.secrets,
        (entry) => entry.id === id,
        "secret",
      );
      secret.lastAccessedAt = ctx.nowIso();
      ctx.audit("secret.read", {
        resourceType: "secret",
        resourceId: id,
        summary: `查看 Secret 元数据 ${secret.name}`,
      });
      ctx.save();
      return secret;
    },
    async create(input) {
      await ctx.pause();
      const secret = {
        id: nextId(db, "sec"),
        enterpriseId: ctx.enterpriseId(),
        name: input.name,
        type: input.type,
        description: input.description,
        referenceCount: 0,
        createdBy: ctx.actor().id,
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
      };
      db.secrets.push(secret);
      ctx.audit("secret.create", {
        resourceType: "secret",
        resourceId: secret.id,
        summary: `创建 Secret ${secret.name}`,
      });
      ctx.save();
      return secret;
    },
    async update(id, patch) {
      await ctx.pause();
      const secret = ctx.mustFind(
        db.secrets,
        (entry) => entry.id === id,
        "secret",
      );
      // The value is rotated server-side and never stored on the record.
      const rest = { ...patch };
      delete rest.value;
      Object.assign(secret, rest, { updatedAt: ctx.nowIso() });
      ctx.audit("secret.update", {
        resourceType: "secret",
        resourceId: id,
        summary: `更新 Secret ${secret.name}`,
      });
      ctx.save();
      return secret;
    },
    async delete(id) {
      await ctx.pause();
      db.secrets = db.secrets.filter((entry) => entry.id !== id);
      ctx.audit("secret.delete", {
        resourceType: "secret",
        resourceId: id,
        summary: `删除 Secret ${id}`,
      });
      ctx.save();
    },
  };
}
