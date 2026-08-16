import type { ArgusApiClient } from "../client";
import type {
  ConnectorRegistrationResult,
  ConnectorUninstallResult,
  ListQuery,
  Page,
  User,
} from "../types";
import type { TaskEvent, TaskViewModel } from "../provisional";
import { createApprovalsDomain } from "./approvals";
import { createAuditDomain } from "./audit";
import { createAuthDomain } from "./auth";
import { createInteractiveCardsDomain } from "./interactive-cards";
import { createConnectorsDomain } from "./connectors";
import type { AuditEntry, BaseContext, MockContext } from "./context";
import { createConversationsDomain } from "./conversations";
import { createEngine } from "./engine";
import { createHostsDomain } from "./hosts";
import { createKubernetesDomain } from "./kubernetes";
import { createModelsDomain } from "./models";
import { createOrgDomain } from "./org";
import { createPlatformDomain } from "./platform";
import { createSecretsDomain } from "./secrets";
import { createSeedDb } from "./seed";
import { createSetupDomain } from "./setup";
import {
  clearStoredDb,
  Emitter,
  loadDb,
  nextId,
  saveDb,
  type MockDb,
} from "./store";
import { createTasksDomain } from "./tasks";

export interface MockOptions {
  /** Fixed latency in ms; defaults to a random 150-400ms per call. */
  delay?: number;
  /** Delay between simulated task steps; defaults to 350ms. */
  stepDelay?: number;
  /** Disable localStorage persistence (useful for tests). */
  persist?: boolean;
  /** Seed the platform as uninitialized to exercise the setup wizard. */
  initialized?: boolean;
}

export interface MockApiClient extends ArgusApiClient {
  /** Clears `argus-mock:*` storage and reseeds the in-memory database. */
  reset(): void;
  /** Test/demo helpers that simulate external actors (Connectors). */
  simulate: {
    connectorRegister(
      scopeId: string,
      input?: { deviceFingerprint?: string; enrollmentTokenId?: string },
    ): ConnectorRegistrationResult;
    connectorUninstall(
      scopeId: string,
      uninstallCommandId?: string,
    ): ConnectorUninstallResult;
    setConnectorOnline(connectorId: string, online: boolean): void;
  };
}

/** Creates a fully self-contained mock of the Argus control plane API. */
export function createMockApiClient(options: MockOptions = {}): MockApiClient {
  const persist = options.persist !== false;
  const emitter = new Emitter();
  const db: MockDb = (persist ? loadDb() : null) ?? seed();

  function seed(): MockDb {
    const fresh = createSeedDb();
    if (options.initialized === false) {
      fresh.platformState = { state: "uninitialized", name: "" };
      fresh.users = fresh.users.filter((user) => user.id !== "u-admin");
      delete fresh.credentials["u-admin"];
    }
    return fresh;
  }

  function save(): void {
    if (persist) saveDb(db);
  }

  function pause(ms?: number): Promise<void> {
    const latency = ms ?? options.delay ?? 150 + Math.random() * 250;
    if (latency <= 0) return Promise.resolve();
    return new Promise((resolve) => setTimeout(resolve, latency));
  }

  const nowIso = () => new Date().toISOString();

  function actor(): Pick<User, "id" | "displayName"> {
    const user = db.users.find((entry) => entry.id === db.session.userId);
    return user
      ? { id: user.id, displayName: user.displayName }
      : { id: "system", displayName: "system" };
  }

  function enterpriseId(): string {
    const user = db.users.find((entry) => entry.id === db.session.userId);
    const enterpriseUser = db.enterpriseUsers.find(
      (entry) => entry.userId === user?.id,
    );
    if (
      !user ||
      user.platformRole ||
      !enterpriseUser ||
      enterpriseUser.enterpriseId !== db.session.enterpriseId
    ) {
      throw new Error("enterprise enterpriseUser required");
    }
    return enterpriseUser.enterpriseId;
  }

  function requirePlatform(): void {
    const user = db.users.find((entry) => entry.id === db.session.userId);
    if (user?.platformRole !== "platform_super_admin") {
      throw new Error("platform super administrator required");
    }
  }

  function audit(action: string, entry: AuditEntry): void {
    const who = actor();
    db.auditEvents.unshift({
      id: nextId(db, "aud"),
      enterpriseId: entry.platform ? null : (db.session.enterpriseId ?? null),
      actorUserId: who.id,
      actorName: who.displayName,
      action,
      origin: entry.origin ?? (entry.platform ? "platform_ui" : "admin_ui"),
      resourceType: entry.resourceType,
      resourceId: entry.resourceId,
      summary: entry.summary,
      result: entry.result ?? "success",
      createdAt: nowIso(),
    });
  }

  function paginate<T>(items: T[], query?: ListQuery): Page<T> {
    const limit = query?.page?.limit ?? 50;
    const cursor = query?.page?.cursor ?? null;
    const offset = cursor ? Number.parseInt(cursor, 10) || 0 : 0;
    const slice = items.slice(offset, offset + limit);
    const nextOffset = offset + slice.length;
    const hasMore = nextOffset < items.length;
    return {
      items: slice,
      nextCursor: hasMore ? String(nextOffset) : null,
      hasMore,
    };
  }

  function mustFind<T>(
    items: T[],
    predicate: (item: T) => boolean,
    what: string,
  ): T {
    const found = items.find(predicate);
    if (!found) throw new Error(`${what} not found`);
    return found;
  }

  function emitTask(task: TaskViewModel): void {
    const event: TaskEvent = { type: "task_updated", task: { ...task } };
    emitter.emit("tasks", event);
    emitter.emit(`task:${task.id}`, event);
  }

  const base: BaseContext = {
    db,
    options: { delay: options.delay, stepDelay: options.stepDelay ?? 350 },
    emitter,
    nowIso,
    pause,
    save,
    actor,
    requirePlatform,
    enterpriseId,
    audit,
    paginate,
    mustFind,
    emitTask,
  };
  const ctx: MockContext = { ...base, ...createEngine(base) };

  return {
    auth: createAuthDomain(ctx),
    conversations: createConversationsDomain(ctx),
    hosts: createHostsDomain(ctx),
    connectors: createConnectorsDomain(ctx),
    kubernetes: createKubernetesDomain(ctx),
    tasks: createTasksDomain(ctx),
    approvals: createApprovalsDomain(ctx),
    models: createModelsDomain(ctx),
    interactiveCards: createInteractiveCardsDomain(ctx),
    org: createOrgDomain(ctx),
    secrets: createSecretsDomain(ctx),
    audit: createAuditDomain(ctx),
    platform: createPlatformDomain(ctx),
    setup: createSetupDomain(ctx),

    reset() {
      clearStoredDb();
      const fresh = seed();
      // Mutate in place so domain closures keep observing the same db object.
      const target = db as unknown as Record<string, unknown>;
      for (const [key, value] of Object.entries(fresh)) {
        target[key] = value;
      }
      save();
    },

    simulate: {
      connectorRegister(scopeId, input = {}) {
        const scope = mustFind(
          db.bastionScopes,
          (entry) => entry.id === scopeId,
          "bastion scope",
        );
        const deviceFingerprint =
          input.deviceFingerprint ?? `mock-device:${scopeId}`;
        const token = input.enrollmentTokenId
          ? db.enrollmentTokens.find(
              (entry) => entry.id === input.enrollmentTokenId,
            )
          : (scope.registrationToken ??
            [...db.enrollmentTokens]
              .reverse()
              .find((entry) => entry.bastionScopeId === scopeId));
        if (!token || token.bastionScopeId !== scopeId) {
          return {
            success: false,
            code: "token_missing",
            message: "安装命令不存在，请在堡垒机编辑页生成新命令。",
          };
        }
        if (
          token.status === "active" &&
          Date.parse(token.expiresAt) <= Date.now()
        ) {
          token.status = "expired";
          token.remainingUses = 0;
          if (scope.registrationToken?.id === token.id) {
            scope.registrationToken = undefined;
          }
          save();
        }
        if (token.status === "consumed") {
          if (token.consumedByDeviceFingerprint === deviceFingerprint) {
            return {
              success: true,
              code: "idempotent_retry",
              message: "同一设备重复注册，返回首次注册结果。",
              connectorId: token.registeredConnectorId,
              hostId: token.registeredHostId,
            };
          }
          return {
            success: false,
            code: "token_consumed",
            message: "该安装命令已被其他机器使用，请在堡垒机编辑页生成新命令。",
          };
        }
        if (token.status === "revoked") {
          return {
            success: false,
            code: "token_revoked",
            message: "该安装命令已被新命令替代，请使用最新命令。",
          };
        }
        if (token.status === "expired") {
          return {
            success: false,
            code: "token_expired",
            message: "该安装命令已过期，请在堡垒机编辑页生成新命令。",
          };
        }

        const registeredAt = nowIso();
        token.status = "consumed";
        token.remainingUses = 0;
        token.consumedAt = registeredAt;
        token.consumedByDeviceFingerprint = deviceFingerprint;

        const hostId = scope.connectorHostId ?? nextId(db, "host");
        const connectorId = nextId(db, "conn");
        const existingHost = db.hosts.find((entry) => entry.id === hostId);
        if (existingHost) {
          existingHost.connectorId = connectorId;
          existingHost.connectionStatus = "online";
          existingHost.updatedAt = registeredAt;
        } else {
          db.hosts.push({
            id: hostId,
            enterpriseId: scope.enterpriseId,
            name: `${scope.name}-gw`,
            hostname: `${scope.name}-gw`,
            address: "10.9.0.2",
            port: 22,
            platform: "linux",
            connectionMode: "connector_local",
            bastionScopeId: scope.id,
            connectorId,
            environment: scope.environment,
            labels: { ...scope.labels, role: "bastion" },
            connectionStatus: "online",
            collectorStatus: "not_installed",
            createdAt: registeredAt,
            updatedAt: registeredAt,
          });
        }
        const previousConnector = db.connectors.find(
          (entry) => entry.id === scope.activeConnectorId,
        );
        const latestEpoch = db.connectors
          .filter((entry) => entry.bastionScopeId === scope.id)
          .reduce((max, entry) => Math.max(max, entry.connectionEpoch), 0);
        db.connectors.push({
          id: connectorId,
          enterpriseId: scope.enterpriseId,
          name: scope.name,
          hostId,
          bastionScopeId: scope.id,
          version: "v24.1.3",
          status: "online",
          capabilities: ["command", "artifact_tunnel", "remote_session"],
          connectionEpoch: latestEpoch + 1,
          certificateExpiresAt: new Date(
            Date.now() + 90 * 86_400_000,
          ).toISOString(),
          connectedAt: registeredAt,
          lastHeartbeatAt: registeredAt,
          managedHostCount: 1,
          createdAt: registeredAt,
        });
        if (previousConnector) previousConnector.status = "offline";
        scope.status = "active";
        scope.connectorHostId = hostId;
        scope.activeConnectorId = connectorId;
        scope.uninstallCommand = undefined;
        if (scope.registrationToken?.id === token.id) {
          scope.registrationToken = undefined;
        }
        scope.updatedAt = registeredAt;
        token.registeredConnectorId = connectorId;
        token.registeredHostId = hostId;
        audit("connector.register", {
          resourceType: "bastion_scope",
          resourceId: scope.id,
          summary: `Connector 注册完成，Scope ${scope.name} 已激活`,
        });
        save();
        return {
          success: true,
          code: "registered",
          message: "Connector 注册成功。",
          connectorId,
          hostId,
        };
      },
      connectorUninstall(scopeId, uninstallCommandId) {
        const scope = mustFind(
          db.bastionScopes,
          (entry) => entry.id === scopeId,
          "bastion scope",
        );
        if (scope.status === "uninstalled") {
          return {
            success: true,
            code: "already_uninstalled",
            message: "堡垒机已经卸载。",
          };
        }
        const command = uninstallCommandId
          ? db.uninstallCommands.find(
              (entry) => entry.id === uninstallCommandId,
            )
          : scope.uninstallCommand;
        if (!command || command.bastionScopeId !== scope.id) {
          return {
            success: false,
            code: "command_missing",
            message: "卸载命令不存在，请重新生成。",
          };
        }
        if (
          command.status === "active" &&
          Date.parse(command.expiresAt) <= Date.now()
        ) {
          command.status = "expired";
          scope.uninstallCommand = undefined;
          save();
        }
        if (command.status === "expired") {
          return {
            success: false,
            code: "command_expired",
            message: "卸载命令已过期，请重新生成。",
          };
        }
        if (command.status !== "active") {
          return {
            success: false,
            code: "command_revoked",
            message: "卸载命令已失效，请重新生成。",
          };
        }
        const connector = db.connectors.find(
          (entry) => entry.id === command.connectorId,
        );
        if (
          !connector ||
          scope.activeConnectorId !== connector.id ||
          connector.connectionEpoch !== command.connectionEpoch
        ) {
          command.status = "revoked";
          scope.uninstallCommand = undefined;
          save();
          return {
            success: false,
            code: "command_revoked",
            message: "堡垒机身份已经变化，原卸载命令已失效。",
          };
        }

        const uninstalledAt = nowIso();
        scope.status = "uninstalling";
        command.status = "consumed";
        command.consumedAt = uninstalledAt;
        connector.status = "uninstalled";
        scope.status = "uninstalled";
        scope.activeConnectorId = undefined;
        scope.uninstallCommand = undefined;
        scope.updatedAt = uninstalledAt;
        const host = db.hosts.find(
          (entry) => entry.id === scope.connectorHostId,
        );
        if (host) {
          host.connectorId = undefined;
          host.connectionStatus = "offline";
          host.updatedAt = uninstalledAt;
        }
        for (const token of db.enrollmentTokens) {
          if (token.bastionScopeId === scope.id && token.status === "active") {
            token.status = "revoked";
            token.remainingUses = 0;
          }
        }
        for (const session of db.remoteSessions) {
          if (
            session.bastionScopeId === scope.id &&
            ["authorized", "connecting", "active"].includes(session.status)
          ) {
            session.status = "connection_lost";
            session.endedAt = uninstalledAt;
          }
        }
        audit("connector.uninstall", {
          resourceType: "bastion_scope",
          resourceId: scope.id,
          summary: `Connector 已卸载，Scope ${scope.name} 等待重新安装`,
        });
        save();
        return {
          success: true,
          code: "uninstalled",
          message: "堡垒机卸载完成，可以安装新的 Connector。",
        };
      },
      setConnectorOnline(connectorId, online) {
        const connector = mustFind(
          db.connectors,
          (entry) => entry.id === connectorId,
          "connector",
        );
        if (connector.status === "uninstalled" && online) {
          throw new Error("uninstalled Connector cannot reconnect");
        }
        connector.status = online ? "online" : "offline";
        if (online) connector.lastHeartbeatAt = nowIso();
        const scope = db.bastionScopes.find(
          (entry) => entry.activeConnectorId === connector.id,
        );
        if (scope) {
          scope.status = online ? "active" : "degraded";
          scope.updatedAt = nowIso();
          const host = db.hosts.find(
            (entry) => entry.id === scope.connectorHostId,
          );
          if (host) {
            host.connectionStatus = online ? "online" : "offline";
            host.updatedAt = nowIso();
          }
        }
        save();
      },
    },
  };
}
