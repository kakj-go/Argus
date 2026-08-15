import type { ArgusApiClient } from "../client";
import type { MockContext } from "./context";
import { nextId } from "./store";

/** Hosts, remote access sessions and host collector management. */
export function createHostsDomain(ctx: MockContext): ArgusApiClient["hosts"] {
  const { db } = ctx;

  return {
    async list(filter, query) {
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
      if (filter?.status?.length) {
        items = items.filter((entry) =>
          filter.status?.includes(entry.connectionStatus),
        );
      }
      if (filter?.connectionMode?.length) {
        items = items.filter((entry) =>
          filter.connectionMode?.includes(entry.connectionMode),
        );
      }
      if (filter?.bastionScopeId) {
        items = items.filter(
          (entry) => entry.bastionScopeId === filter.bastionScopeId,
        );
      }
      if (filter?.environment?.length) {
        items = items.filter((entry) =>
          filter.environment?.includes(entry.environment),
        );
      }
      return ctx.paginate(items, query);
    },
    async get(id) {
      await ctx.pause();
      return ctx.mustFind(db.hosts, (entry) => entry.id === id, "host");
    },
    async previewCreate(input) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "host.create",
        title: `新增主机 ${input.name}`,
        params: { ...input },
      });
    },
    async update(id, patch) {
      await ctx.pause();
      const host = ctx.mustFind(db.hosts, (entry) => entry.id === id, "host");
      Object.assign(host, patch, { updatedAt: ctx.nowIso() });
      ctx.audit("host.update", {
        resourceType: "host",
        resourceId: id,
        summary: `更新主机 ${host.name}`,
      });
      ctx.save();
      return host;
    },
    async delete(id) {
      await ctx.pause();
      const host = ctx.mustFind(db.hosts, (entry) => entry.id === id, "host");
      db.hosts = db.hosts.filter((entry) => entry.id !== id);
      for (const scope of db.bastionScopes) {
        scope.memberHostIds = scope.memberHostIds.filter(
          (hostId) => hostId !== id,
        );
      }
      ctx.audit("host.delete", {
        resourceType: "host",
        resourceId: id,
        summary: `删除主机 ${host.name}`,
      });
      ctx.save();
    },
    async testConnection(id) {
      await ctx.pause();
      const host = ctx.mustFind(db.hosts, (entry) => entry.id === id, "host");
      const reachable = host.connectionStatus !== "offline";
      ctx.audit("host.test_connection", {
        resourceType: "host",
        resourceId: id,
        summary: `连接测试 ${host.name}`,
        result: reachable ? "success" : "failure",
      });
      ctx.save();
      return {
        success: reachable,
        latencyMs: reachable ? 180 : 0,
        checks: [
          { name: "dns_resolve", status: "passed", detail: host.address },
          {
            name: "network_route",
            status: reachable ? "passed" : "failed",
            detail: reachable ? host.connectionMode : "unreachable",
          },
          { name: "authentication", status: reachable ? "passed" : "skipped" },
        ],
      };
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
        params: {
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
