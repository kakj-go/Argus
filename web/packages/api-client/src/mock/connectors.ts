import type { ArgusApiClient } from "../client";
import type {
  BastionScope,
  ConnectorEnrollmentPurpose,
  ConnectorEnrollmentToken,
  ConnectorUninstallCommand,
} from "../types";
import type { MockContext } from "./context";
import { nextId } from "./store";

/** Connectors and Bastion Scopes, including the pending-registration flow. */
export function createConnectorsDomain(
  ctx: MockContext,
): ArgusApiClient["connectors"] {
  const { db } = ctx;

  function uniqueId(prefix: string, exists: (id: string) => boolean): string {
    let id = nextId(db, prefix);
    while (exists(id)) id = nextId(db, prefix);
    return id;
  }

  function newEnrollmentToken(
    scope: BastionScope,
    purpose: ConnectorEnrollmentPurpose,
  ): ConnectorEnrollmentToken {
    const tokenValue = `enroll_${Math.random().toString(16).slice(2, 14)}`;
    return {
      id: uniqueId("etok", (id) =>
        db.enrollmentTokens.some((entry) => entry.id === id),
      ),
      enterpriseId: scope.enterpriseId,
      bastionScopeId: scope.id,
      purpose,
      status: "active",
      token: tokenValue,
      installCommand: `curl -fsSL https://argus.example/install.sh | sh -s -- --token ${tokenValue}`,
      expiresAt: new Date(Date.now() + 24 * 3_600_000).toISOString(),
      remainingUses: 1,
      createdBy: ctx.actor().id,
      createdAt: ctx.nowIso(),
    };
  }

  function activeConnector(scope: BastionScope) {
    return db.connectors.find((entry) => entry.id === scope.activeConnectorId);
  }

  return {
    async list(query) {
      await ctx.pause();
      const items = db.connectors.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      return ctx.paginate(items, query);
    },
    async get(id) {
      await ctx.pause();
      return ctx.mustFind(
        db.connectors,
        (entry) => entry.id === id,
        "connector",
      );
    },
    async listBastionScopes() {
      await ctx.pause();
      return db.bastionScopes.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
    },
    async getBastionScope(id) {
      await ctx.pause();
      return ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === id,
        "bastion scope",
      );
    },
    async createBastionScope(input) {
      await ctx.pause();
      const scope: BastionScope = {
        id: nextId(db, "scope"),
        enterpriseId: ctx.enterpriseId(),
        name: input.name,
        environment: input.environment,
        tags: input.tags ?? {},
        status: "pending",
        memberHostIds: [],
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
      };
      const enrollmentToken = newEnrollmentToken(scope, "initial_registration");
      scope.registrationToken = enrollmentToken;
      db.bastionScopes.push(scope);
      db.enrollmentTokens.push(enrollmentToken);
      ctx.audit("bastion_scope.create", {
        resourceType: "bastion_scope",
        resourceId: scope.id,
        summary: `创建待注册堡垒机范围 ${scope.name}`,
      });
      ctx.save();
      return { scope, enrollmentToken };
    },
    async updateBastionScope(scopeId, input) {
      await ctx.pause();
      const enterpriseId = ctx.enterpriseId();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId && entry.enterpriseId === enterpriseId,
        "bastion scope",
      );
      if (input.name !== undefined) scope.name = input.name;
      if (input.environment !== undefined) {
        scope.environment = input.environment;
      }
      if (input.tags !== undefined) scope.tags = input.tags;
      scope.updatedAt = ctx.nowIso();

      const host = db.hosts.find((entry) => entry.id === scope.connectorHostId);
      if (host) {
        if (input.name !== undefined) host.name = input.name;
        if (input.environment !== undefined) {
          host.environment = input.environment;
        }
        if (input.tags !== undefined) {
          host.tags = { ...input.tags, role: "bastion" };
        }
        host.updatedAt = ctx.nowIso();
      }
      const connector = db.connectors.find(
        (entry) => entry.id === scope.activeConnectorId,
      );
      if (connector && input.name !== undefined) connector.name = input.name;

      ctx.audit("bastion_scope.update", {
        resourceType: "bastion_scope",
        resourceId: scope.id,
        summary: `更新堡垒机范围 ${scope.name}`,
      });
      ctx.save();
      return scope;
    },
    async regenerateEnrollmentToken(scopeId) {
      await ctx.pause();
      const enterpriseId = ctx.enterpriseId();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId && entry.enterpriseId === enterpriseId,
        "bastion scope",
      );
      const connector = activeConnector(scope);
      const canInstall =
        scope.status === "pending" ||
        scope.status === "uninstalled" ||
        connector?.status === "offline";
      if (!canInstall) {
        throw new Error(
          "online Connector must be uninstalled or offline before replacement",
        );
      }
      const purpose: ConnectorEnrollmentPurpose = scope.connectorHostId
        ? "connector_replacement"
        : "initial_registration";
      const enrollmentToken = newEnrollmentToken(scope, purpose);
      for (const token of db.enrollmentTokens) {
        if (token.bastionScopeId === scope.id && token.status === "active") {
          token.status = "revoked";
          token.remainingUses = 0;
        }
      }
      if (connector?.status === "offline") {
        scope.activeConnectorId = undefined;
        const host = db.hosts.find(
          (entry) => entry.id === scope.connectorHostId,
        );
        if (host) {
          host.connectorId = undefined;
          host.connectionStatus = "offline";
          host.updatedAt = ctx.nowIso();
        }
      }
      db.enrollmentTokens.push(enrollmentToken);
      scope.registrationToken = enrollmentToken;
      scope.status = "pending";
      scope.updatedAt = ctx.nowIso();
      ctx.audit("bastion_scope.token_regenerate", {
        resourceType: "bastion_scope",
        resourceId: scope.id,
        summary: `重新生成注册令牌 ${scope.name}`,
      });
      ctx.save();
      return enrollmentToken;
    },
    async createUninstallCommand(scopeId) {
      await ctx.pause();
      const enterpriseId = ctx.enterpriseId();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId && entry.enterpriseId === enterpriseId,
        "bastion scope",
      );
      const connector = activeConnector(scope);
      if (!connector || connector.status !== "online") {
        throw new Error("only an online Connector can be uninstalled");
      }
      const existing = scope.uninstallCommand;
      if (
        existing?.status === "active" &&
        Date.parse(existing.expiresAt) > Date.now()
      ) {
        return existing;
      }
      for (const command of db.uninstallCommands) {
        if (
          command.bastionScopeId === scope.id &&
          command.status === "active"
        ) {
          command.status = "revoked";
        }
      }
      const tokenValue = `uninstall_${Math.random().toString(16).slice(2, 14)}`;
      const command: ConnectorUninstallCommand = {
        id: uniqueId("ucmd", (id) =>
          db.uninstallCommands.some((entry) => entry.id === id),
        ),
        enterpriseId,
        bastionScopeId: scope.id,
        connectorId: connector.id,
        connectionEpoch: connector.connectionEpoch,
        status: "active",
        token: tokenValue,
        uninstallCommand: `curl -fsSL https://argus.example/uninstall.sh | sh -s -- --token ${tokenValue}`,
        expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
        createdBy: ctx.actor().id,
        createdAt: ctx.nowIso(),
      };
      db.uninstallCommands.push(command);
      scope.uninstallCommand = command;
      ctx.audit("connector.uninstall_command.create", {
        resourceType: "bastion_scope",
        resourceId: scope.id,
        summary: `生成堡垒机卸载命令 ${scope.name}`,
      });
      ctx.save();
      return command;
    },
    async deleteBastionScope(scopeId) {
      await ctx.pause();
      const enterpriseId = ctx.enterpriseId();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId && entry.enterpriseId === enterpriseId,
        "bastion scope",
      );
      const connector = activeConnector(scope);
      if (scope.status !== "uninstalled" && connector?.status !== "offline") {
        throw new Error(
          "bastion must be offline or uninstalled before deletion",
        );
      }
      const members = db.hosts.filter(
        (host) =>
          host.bastionScopeId === scope.id && host.id !== scope.connectorHostId,
      );
      if (members.length > 0 || scope.memberHostIds.length > 0) {
        throw new Error(
          "move all member hosts out before deleting the bastion",
        );
      }
      const activeSession = db.remoteSessions.some(
        (session) =>
          session.bastionScopeId === scope.id &&
          ["authorized", "connecting", "active"].includes(session.status),
      );
      if (activeSession) {
        throw new Error("terminate active remote sessions before deletion");
      }

      ctx.audit("bastion_scope.delete", {
        resourceType: "bastion_scope",
        resourceId: scope.id,
        summary: `删除堡垒机范围 ${scope.name}`,
      });
      db.bastionScopes = db.bastionScopes.filter(
        (entry) => entry.id !== scope.id,
      );
      db.connectors = db.connectors.filter(
        (entry) => entry.bastionScopeId !== scope.id,
      );
      db.enrollmentTokens = db.enrollmentTokens.filter(
        (entry) => entry.bastionScopeId !== scope.id,
      );
      db.uninstallCommands = db.uninstallCommands.filter(
        (entry) => entry.bastionScopeId !== scope.id,
      );
      if (scope.connectorHostId) {
        db.hosts = db.hosts.filter(
          (entry) => entry.id !== scope.connectorHostId,
        );
        db.collectors = db.collectors.filter(
          (entry) =>
            entry.targetType !== "host" ||
            entry.targetId !== scope.connectorHostId,
        );
      }
      ctx.save();
    },
    async rotateCertificate(connectorId) {
      await ctx.pause();
      const connector = ctx.mustFind(
        db.connectors,
        (entry) => entry.id === connectorId,
        "connector",
      );
      connector.certificateExpiresAt = new Date(
        Date.now() + 90 * 86_400_000,
      ).toISOString();
      ctx.audit("connector.cert.rotate", {
        resourceType: "connector",
        resourceId: connectorId,
        summary: `轮换 Connector 证书 ${connector.name}`,
      });
      ctx.save();
      return connector;
    },
  };
}
