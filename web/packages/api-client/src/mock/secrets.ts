import type { ArgusApiClient } from "../client";
import type { MockContext } from "./context";
import { nextId } from "./store";
import type { Credential, ManagedAccount } from "../generated/contracts";

/** Secret metadata; values are write-only and every access is audited. */
export function createSecretsDomain(
  ctx: MockContext,
): ArgusApiClient["secrets"] {
  const { db } = ctx;
  const credentials: Credential[] = db.secrets.map((item) => ({
    id: `cred-${item.id}`,
    enterprise_id: item.enterprise_id,
    name: item.name,
    protocol:
      item.type === "kubeconfig"
        ? "kubernetes"
        : item.type === "winrm_password"
          ? "winrm"
          : "ssh",
    username: item.type === "kubeconfig" ? undefined : "argus",
    secret_id: item.id,
    status: "active",
    version: 1,
    created_at: item.created_at,
    updated_at: item.updated_at,
  }));
  const managedAccounts: ManagedAccount[] = [];

  return {
    async list(query) {
      await ctx.pause();
      const items = db.secrets.filter(
        (entry) => entry.enterprise_id === ctx.enterpriseId(),
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
      secret.last_accessed_at = ctx.nowIso();
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
        enterprise_id: ctx.enterpriseId(),
        name: input.name,
        type: input.type,
        description: input.description,
        status: "active" as const,
        current_version: 1,
        reference_count: 0,
        created_by: ctx.actor().id,
        version: 1,
        created_at: ctx.nowIso(),
        updated_at: ctx.nowIso(),
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
      Object.assign(secret, patch, {
        version: secret.version + 1,
        updated_at: ctx.nowIso(),
      });
      ctx.audit("secret.update", {
        resourceType: "secret",
        resourceId: id,
        summary: `更新 Secret ${secret.name}`,
      });
      ctx.save();
      return secret;
    },
    async rotate(id, _value, expectedVersion) {
      await ctx.pause();
      const secret = ctx.mustFind(
        db.secrets,
        (entry) => entry.id === id,
        "secret",
      );
      if (secret.version !== expectedVersion) {
        throw new Error("secret version conflict");
      }
      secret.current_version += 1;
      secret.version += 1;
      secret.updated_at = ctx.nowIso();
      ctx.audit("secret.rotate", {
        resourceType: "secret",
        resourceId: id,
        summary: `轮换 Secret ${secret.name}`,
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
    async listCredentials() {
      await ctx.pause();
      return credentials.filter(
        (item) => item.enterprise_id === ctx.enterpriseId(),
      );
    },
    async createCredential(input) {
      await ctx.pause();
      const now = ctx.nowIso();
      const value: Credential = {
        id: nextId(db, "cred"),
        enterprise_id: ctx.enterpriseId(),
        ...input,
        status: "active",
        version: 1,
        created_at: now,
        updated_at: now,
      };
      credentials.push(value);
      return value;
    },
    async updateCredential(id, input) {
      await ctx.pause();
      const value = credentials.find((item) => item.id === id);
      if (!value) throw new Error("credential not found");
      Object.assign(value, input, {
        version: value.version + 1,
        updated_at: ctx.nowIso(),
      });
      return value;
    },
    async listManagedAccounts() {
      await ctx.pause();
      return managedAccounts.filter(
        (item) => item.enterprise_id === ctx.enterpriseId(),
      );
    },
    async createManagedAccount(input) {
      await ctx.pause();
      const now = ctx.nowIso();
      const value: ManagedAccount = {
        id: nextId(db, "ma"),
        enterprise_id: ctx.enterpriseId(),
        ...input,
        status: "active",
        version: 1,
        created_at: now,
        updated_at: now,
      };
      managedAccounts.push(value);
      return value;
    },
    async updateManagedAccount(id, input) {
      await ctx.pause();
      const value = managedAccounts.find((item) => item.id === id);
      if (!value) throw new Error("managed account not found");
      Object.assign(value, input, {
        version: value.version + 1,
        updated_at: ctx.nowIso(),
      });
      return value;
    },
  };
}
