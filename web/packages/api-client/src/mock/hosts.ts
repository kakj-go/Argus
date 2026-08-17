import type { ArgusApiClient } from "../client";
import type { Host, HostPage } from "../generated/contracts";
import type { MockHost } from "./resource-models";
import type { MockContext } from "./context";
import { nextId } from "./store";

function hostContract(value: MockHost): Host {
  const contract: Host = {
    id: value.id,
    enterprise_id: value.enterpriseId,
    name: value.name,
    hostname: value.hostname || undefined,
    address: value.address,
    port: value.port,
    platform: value.platform,
    connection_mode: value.connectionMode,
    bastion_scope_id: value.bastionScopeId,
    connector_id: value.connectorId,
    environment: value.environment,
    labels: value.labels,
    labels_version: 1,
    resource_version: value.resourceVersion ?? 1,
    connection_status: value.connectionStatus,
    last_seen_at: value.lastSeenAt,
    status: "active",
    created_at: value.createdAt,
    updated_at: value.updatedAt,
  };
  return Object.assign(contract, {
    collectorStatus: value.collectorStatus,
    telemetryRoute: value.telemetryRoute,
  });
}

function hostPage(items: MockHost[], limit?: number): HostPage {
  return {
    items: items.slice(0, limit).map(hostContract),
    page: {
      next_cursor: null,
      has_more: limit !== undefined && items.length > limit,
      partial: { partial: false, reasons: [] },
    },
  };
}

/** Hosts, remote access sessions and host collector management. */
export function createHostsDomain(ctx: MockContext): ArgusApiClient["hosts"] {
  const { db } = ctx;
  const connectionTests = new Map<
    string,
    Awaited<ReturnType<ArgusApiClient["hosts"]["createConnectionTest"]>>
  >();

  return {
    async list(filter) {
      await ctx.pause();
      let items = db.hosts.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      if (filter?.query) {
        const q = filter.query.toLowerCase();
        items = items.filter(
          (entry) =>
            entry.name.toLowerCase().includes(q) || entry.address.includes(q),
        );
      }
      if (filter?.connection_mode) {
        items = items.filter(
          (entry) => entry.connectionMode === filter.connection_mode,
        );
      }
      if (filter?.bastion_scope_id) {
        items = items.filter(
          (entry) => entry.bastionScopeId === filter.bastion_scope_id,
        );
      }
      if (filter?.labels) {
        items = items.filter((entry) =>
          Object.entries(filter.labels ?? {}).every(([key, values]) =>
            values.includes(entry.labels[key] ?? ""),
          ),
        );
      }
      return hostPage(items, filter?.limit);
    },
    async get(id) {
      await ctx.pause();
      return hostContract(
        ctx.mustFind(db.hosts, (entry) => entry.id === id, "host"),
      );
    },
    async createConnectionTest(input) {
      await ctx.pause();
      const now = ctx.nowIso();
      const id = nextId(db, "ct");
      const value = {
        id,
        enterprise_id: ctx.enterpriseId(),
        target_type: "host" as const,
        path:
          input.connection_mode === "via_bastion"
            ? ("connector" as const)
            : ("direct" as const),
        status: "succeeded" as const,
        checks: [
          {
            name: "dns_resolve",
            status: "passed" as const,
            detail: input.address,
          },
          { name: "authentication", status: "passed" as const },
        ],
        latency_ms: 120,
        expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
        created_at: now,
        updated_at: now,
      };
      connectionTests.set(id, value);
      return value;
    },
    async getConnectionTest(id) {
      await ctx.pause();
      const value = connectionTests.get(id);
      if (!value) throw new Error("connection test not found");
      return value;
    },
    async previewCreateResource(input) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "host.create",
        title: `新增主机 ${input.name}`,
        input_data: { ...input },
      });
    },
    async previewUpdateResource(id, input) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "host.update",
        title: `更新主机 ${id}`,
        input_data: { id, ...input },
      });
    },
    async previewDeleteResource(id, expectedVersion) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "host.delete",
        title: `删除主机 ${id}`,
        input_data: { id, expected_version: expectedVersion },
      });
    },
    async getCollector(hostId) {
      await ctx.pause();
      return (
        db.collectors.find(
          (entry) => entry.targetType === "host" && entry.targetId === hostId,
        ) ?? null
      );
    },
    async previewCollectorInstall(hostId, input) {
      await ctx.pause();
      const host = ctx.mustFind(
        db.hosts,
        (entry) => entry.id === hostId,
        "host",
      );
      return ctx.createPendingAction({
        tool: "telemetry.host.install",
        title: `安装 OTLP 收集器 · ${host.name}`,
        input_data: {
          hostId,
          profile: input.profile,
          telemetryRoute: input.telemetryRoute,
        },
      });
    },
    async listSessions(filter) {
      await ctx.pause();
      return db.remoteSessions.filter(
        (entry) =>
          entry.enterpriseId === ctx.enterpriseId() &&
          (!filter?.hostId || entry.hostId === filter.hostId) &&
          (!filter?.status?.length || filter.status.includes(entry.status)),
      );
    },
    async createSession(input) {
      await ctx.pause();
      const host = ctx.mustFind(
        db.hosts,
        (entry) => entry.id === input.hostId,
        "host",
      );
      const who = ctx.actor();
      const session = {
        id: nextId(db, "rs"),
        enterpriseId: ctx.enterpriseId(),
        userId: who.id,
        userName: who.displayName,
        hostId: host.id,
        hostName: host.name,
        connectionMode: host.connectionMode,
        connectorId: host.connectorId,
        bastionScopeId: host.bastionScopeId,
        protocol: input.protocol ?? ("ssh" as const),
        targetAccount: input.targetAccount,
        reason: input.reason,
        status: "active" as const,
        startedAt: ctx.nowIso(),
        lastActivityAt: ctx.nowIso(),
        recordingRef: `rec-${nextId(db, "rec")}`,
        createdAt: ctx.nowIso(),
      };
      db.remoteSessions.unshift(session);
      ctx.audit("remote_access.session.create", {
        resourceType: "remote_session",
        resourceId: session.id,
        summary: `${who.displayName} 打开 ${host.name} 远程会话`,
      });
      ctx.save();
      return session;
    },
    async getSession(id) {
      await ctx.pause();
      return ctx.mustFind(
        db.remoteSessions,
        (entry) => entry.id === id,
        "remote session",
      );
    },
    async terminateSession(id) {
      await ctx.pause();
      const session = ctx.mustFind(
        db.remoteSessions,
        (entry) => entry.id === id,
        "remote session",
      );
      session.status = "terminated";
      session.endedAt = ctx.nowIso();
      ctx.audit("remote_access.session.terminate", {
        resourceType: "remote_session",
        resourceId: id,
        summary: `终止远程会话 ${id}`,
      });
      ctx.save();
      return session;
    },
  };
}
