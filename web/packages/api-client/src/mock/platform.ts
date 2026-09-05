import type { ArgusApiClient } from "../client";
import type { MockContext } from "./context";
import { BUILTIN_ROLE_TEMPLATES } from "./seed-org";
import { nextId } from "./store";

/** Platform super admin domain: enterprises, admins and OpenSandbox. */
export function createPlatformDomain(
  ctx: MockContext,
): ArgusApiClient["platform"] {
  const { db } = ctx;
  const platformPause = async () => {
    ctx.requirePlatform();
    await ctx.pause();
  };

  return {
    enterprises: {
      async list(query) {
        await platformPause();
        return ctx.paginate(db.enterprises, query);
      },
      async get(id) {
        await platformPause();
        return ctx.mustFind(
          db.enterprises,
          (entry) => entry.id === id,
          "enterprise",
        );
      },
      async create(input) {
        await platformPause();
        const enterprise = {
          id: nextId(db, "ent"),
          name: input.name,
          code: input.code,
          status: "active" as const,
          timezone: input.timezone ?? "Asia/Shanghai",
          sandboxQuotaProfile: input.sandboxQuotaProfile,
          remark: input.remark,
          createdAt: ctx.nowIso(),
        };
        db.enterprises.push(enterprise);
        db.departments.push({
          id: nextId(db, "dept"),
          enterprise_id: enterprise.id,
          name: "默认部门",
          description: "企业创建时自动生成",
          is_default: true,
          status: "active",
          version: 1,
          created_at: ctx.nowIso(),
          updated_at: ctx.nowIso(),
        });
        // 内置角色与 seed 保持一致，保证新企业开箱可用。
        for (const template of BUILTIN_ROLE_TEMPLATES) {
          db.roles.push({
            id: nextId(db, "role"),
            enterprise_id: enterprise.id,
            builtin_key: template.key,
            name: template.name,
            description: template.description,
            builtin: true,
            permissions: [...template.permissions],
            status: "active",
            version: 1,
            created_at: ctx.nowIso(),
            updated_at: ctx.nowIso(),
          });
        }
        ctx.audit("platform.enterprise.create", {
          platform: true,
          resourceType: "enterprise",
          resourceId: enterprise.id,
          summary: `创建企业 ${enterprise.name}`,
        });
        ctx.save();
        return enterprise;
      },
      async update(id, patch) {
        await platformPause();
        const enterprise = ctx.mustFind(
          db.enterprises,
          (entry) => entry.id === id,
          "enterprise",
        );
        Object.assign(enterprise, patch);
        ctx.audit("platform.enterprise.update", {
          platform: true,
          resourceType: "enterprise",
          resourceId: id,
          summary: `更新企业 ${enterprise.name}`,
        });
        ctx.save();
        return enterprise;
      },
      async suspend(id) {
        await platformPause();
        const enterprise = ctx.mustFind(
          db.enterprises,
          (entry) => entry.id === id,
          "enterprise",
        );
        enterprise.status = "suspended";
        ctx.audit("platform.enterprise.suspend", {
          platform: true,
          resourceType: "enterprise",
          resourceId: id,
          summary: `暂停企业 ${enterprise.name}`,
        });
        ctx.save();
        return enterprise;
      },
      async activate(id) {
        await platformPause();
        const enterprise = ctx.mustFind(
          db.enterprises,
          (entry) => entry.id === id,
          "enterprise",
        );
        enterprise.status = "active";
        ctx.audit("platform.enterprise.activate", {
          platform: true,
          resourceType: "enterprise",
          resourceId: id,
          summary: `恢复企业 ${enterprise.name}`,
        });
        ctx.save();
        return enterprise;
      },
      async disable(id) {
        await platformPause();
        const enterprise = ctx.mustFind(
          db.enterprises,
          (entry) => entry.id === id,
          "enterprise",
        );
        enterprise.status = "disabled";
        ctx.audit("platform.enterprise.disable", {
          platform: true,
          resourceType: "enterprise",
          resourceId: id,
          summary: `停用企业 ${enterprise.name}`,
        });
        ctx.save();
        return enterprise;
      },
    },
    admins: {
      async list(targetEnterpriseId) {
        await platformPause();
        return db.enterpriseAdmins.filter(
          (entry) =>
            !targetEnterpriseId || entry.enterpriseId === targetEnterpriseId,
        );
      },
      async create(input) {
        await platformPause();
        const admin = {
          id: nextId(db, "ea"),
          enterpriseId: input.enterpriseId,
          username: input.username,
          displayName: input.displayName,
          email: input.email,
          credentialStatus: "temporary_password" as const,
          createdAt: ctx.nowIso(),
        };
        db.enterpriseAdmins.push(admin);
        ctx.audit("platform.enterprise_admin.create", {
          platform: true,
          resourceType: "enterprise_admin",
          resourceId: admin.id,
          summary: `创建企业管理员 ${admin.displayName}`,
        });
        ctx.save();
        return admin;
      },
      async resetAuth(id) {
        await platformPause();
        const admin = ctx.mustFind(
          db.enterpriseAdmins,
          (entry) => entry.id === id,
          "enterprise admin",
        );
        admin.credentialStatus = "temporary_password";
        ctx.audit("platform.enterprise_admin.reset_auth", {
          platform: true,
          resourceType: "enterprise_admin",
          resourceId: id,
          summary: `重置登录认证 ${admin.username}`,
        });
        ctx.save();
        return admin;
      },
      async disable(id) {
        await platformPause();
        const admin = ctx.mustFind(
          db.enterpriseAdmins,
          (entry) => entry.id === id,
          "enterprise admin",
        );
        admin.credentialStatus = "disabled";
        ctx.audit("platform.enterprise_admin.disable", {
          platform: true,
          resourceType: "enterprise_admin",
          resourceId: id,
          summary: `禁用企业管理员 ${admin.username}`,
        });
        ctx.save();
        return admin;
      },
    },
    sandboxBackends: {
      async list() {
        await platformPause();
        return db.sandboxBackends;
      },
      async create(input) {
        await platformPause();
        const backend = {
          id: nextId(db, "sbk"),
          name: input.name,
          endpoint: input.endpoint,
          credentialRef: input.credentialRef,
          tlsVerify: input.tlsVerify ?? true,
          enabled: true,
          defaultStorage: input.defaultStorage ?? "sandbox-artifacts",
          healthStatus: "healthy" as const,
          createdAt: ctx.nowIso(),
        };
        db.sandboxBackends.push(backend);
        ctx.audit("platform.sandbox_backend.create", {
          platform: true,
          resourceType: "sandbox_backend",
          resourceId: backend.id,
          summary: `登记 Sandbox Backend ${backend.name}`,
        });
        ctx.save();
        return backend;
      },
      async update(id, patch) {
        await platformPause();
        const backend = ctx.mustFind(
          db.sandboxBackends,
          (entry) => entry.id === id,
          "sandbox backend",
        );
        Object.assign(backend, patch);
        ctx.audit("platform.sandbox_backend.update", {
          platform: true,
          resourceType: "sandbox_backend",
          resourceId: id,
          summary: `更新 Sandbox Backend ${backend.name}`,
        });
        ctx.save();
        return backend;
      },
      async test(id) {
        await platformPause();
        const backend = ctx.mustFind(
          db.sandboxBackends,
          (entry) => entry.id === id,
          "sandbox backend",
        );
        return {
          success: backend.enabled,
          latencyMs: 210,
          checks: [
            { name: "endpoint", status: "passed", detail: backend.endpoint },
            { name: "tls", status: backend.tlsVerify ? "passed" : "skipped" },
            { name: "capacity", status: "passed" },
          ],
        };
      },
    },
    images: {
      async list() {
        await platformPause();
        return db.sandboxImages;
      },
      async create(input) {
        await platformPause();
        const image = {
          id: nextId(db, "img"),
          name: input.name,
          reference: input.reference,
          digest: input.digest,
          languages: input.languages,
          scanStatus: "pending" as const,
          signatureStatus: "unsigned" as const,
          enabled: false,
          createdAt: ctx.nowIso(),
        };
        db.sandboxImages.push(image);
        ctx.audit("platform.sandbox_image.manage", {
          platform: true,
          resourceType: "sandbox_image",
          resourceId: image.id,
          summary: `登记镜像 ${image.name}`,
        });
        ctx.save();
        return image;
      },
      async setEnabled(id, enabled) {
        await platformPause();
        const image = ctx.mustFind(
          db.sandboxImages,
          (entry) => entry.id === id,
          "sandbox image",
        );
        image.enabled = enabled;
        ctx.audit("platform.sandbox_image.manage", {
          platform: true,
          resourceType: "sandbox_image",
          resourceId: id,
          summary: `${enabled ? "启用" : "停用"}镜像 ${image.name}`,
        });
        ctx.save();
        return image;
      },
    },
    profiles: {
      async list() {
        await platformPause();
        return db.sandboxProfiles;
      },
      async create(input) {
        await platformPause();
        const profile = {
          id: nextId(db, "sp"),
          name: input.name,
          description: input.description,
          imageId: input.imageId,
          resources: input.resources,
          timeouts: input.timeouts,
          network: input.network,
          capabilities: input.capabilities,
          builtin: false,
          enabled: true,
          createdAt: ctx.nowIso(),
        };
        db.sandboxProfiles.push(profile);
        ctx.audit("platform.sandbox_profile.manage", {
          platform: true,
          resourceType: "sandbox_profile",
          resourceId: profile.id,
          summary: `创建 Sandbox Profile ${profile.name}`,
        });
        ctx.save();
        return profile;
      },
      async update(id, patch) {
        await platformPause();
        const profile = ctx.mustFind(
          db.sandboxProfiles,
          (entry) => entry.id === id,
          "sandbox profile",
        );
        Object.assign(profile, patch);
        ctx.audit("platform.sandbox_profile.manage", {
          platform: true,
          resourceType: "sandbox_profile",
          resourceId: id,
          summary: `更新 Sandbox Profile ${profile.name}`,
        });
        ctx.save();
        return profile;
      },
    },
    quotas: {
      async get(targetEnterpriseId) {
        await platformPause();
        const quota = db.sandboxQuotas.find(
          (entry) => entry.enterpriseId === targetEnterpriseId,
        );
        if (!quota) throw new Error("sandbox quota not found");
        return quota;
      },
      async update(targetEnterpriseId, patch) {
        await platformPause();
        const quota = db.sandboxQuotas.find(
          (entry) => entry.enterpriseId === targetEnterpriseId,
        );
        if (!quota) throw new Error("sandbox quota not found");
        Object.assign(quota, patch);
        ctx.audit("platform.sandbox_quota.manage", {
          platform: true,
          resourceType: "enterprise",
          resourceId: targetEnterpriseId,
          summary: `更新企业 Sandbox 配额 ${targetEnterpriseId}`,
        });
        ctx.save();
        return quota;
      },
    },
    sessions: {
      async list(filter) {
        await platformPause();
        return db.sandboxSessions.filter(
          (entry) =>
            (!filter?.enterpriseId ||
              entry.enterpriseId === filter.enterpriseId) &&
            (!filter?.status?.length || filter.status.includes(entry.status)),
        );
      },
      async terminate(id) {
        await platformPause();
        const session = ctx.mustFind(
          db.sandboxSessions,
          (entry) => entry.id === id,
          "sandbox session",
        );
        session.status = "terminated";
        session.terminatedAt = ctx.nowIso();
        ctx.audit("platform.sandbox_session.terminate", {
          platform: true,
          resourceType: "sandbox_session",
          resourceId: id,
          summary: `终止 Sandbox 会话 ${id}`,
        });
        ctx.save();
        return session;
      },
    },
    usage: {
      async list() {
        await platformPause();
        return [];
      },
    },
    audit: {
      async list(filter, query) {
        await platformPause();
        let items = db.auditEvents.filter(
          (entry) => entry.enterpriseId === null,
        );
        if (filter?.action) {
          items = items.filter((entry) => entry.action === filter.action);
        }
        if (filter?.query) {
          items = items.filter((entry) =>
            entry.summary.includes(filter.query ?? ""),
          );
        }
        return ctx.paginate(items, query);
      },
    },
    pki: {
      async get() {
        await platformPause();
        const now = ctx.nowIso();
        const bundleSha256 =
          "5b2cc31ad7190d66c2fd4d39f6187c341f863e0d57d0a517be0152cf881b7483";
        const caFingerprint =
          "c8209260ec4f8399c98cb8d44b3614667d932fa14f93ca9f56641a48efab87b1";
        return {
          bundles: [
            {
              epoch: 1,
              state: "stable" as const,
              direction: "forward" as const,
              bundleSha256,
              currentCaFingerprints: [caFingerprint],
              nextCaFingerprints: [],
              startedAt: now,
            },
          ],
          nodes: [
            {
              id: "argus-server/platform-0",
              kind: "control_plane" as const,
              epoch: 1,
              bundleSha256,
              caFingerprints: [caFingerprint],
              status: "acked" as const,
              blocksCutover: true,
              acknowledgedAt: now,
              updatedAt: now,
            },
            {
              id: "connector-demo",
              kind: "kubernetes_connector" as const,
              enterpriseId: db.enterprises[0]?.id,
              epoch: 1,
              bundleSha256,
              caFingerprints: [caFingerprint],
              status: "acked" as const,
              blocksCutover: true,
              acknowledgedAt: now,
              updatedAt: now,
            },
          ],
          acknowledgedNodes: 2,
          pendingNodes: 0,
          failedNodes: 0,
          trustExpiredNodes: 0,
        };
      },
    },
  };
}
