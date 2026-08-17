import type { ArgusApiClient } from "../client";
import type {
  BastionScope,
  BastionScopePage,
  Connector,
  ConnectorPage,
} from "../generated/contracts";
import type { MockBastionScope, MockConnector } from "./resource-models";
import type { MockContext } from "./context";

function connectorContract(value: MockConnector): Connector {
  return {
    id: value.id,
    enterprise_id: value.enterpriseId,
    role: "bastion",
    name: value.name,
    host_id: value.hostId || undefined,
    bastion_scope_id: value.bastionScopeId || undefined,
    software_version: value.version || undefined,
    status: value.status,
    capabilities: value.capabilities,
    connection_epoch: value.connectionEpoch,
    certificate_expires_at: value.certificateExpiresAt,
    connected_at: value.connectedAt,
    last_heartbeat_at: value.lastHeartbeatAt,
    version: 1,
    created_at: value.createdAt,
    updated_at: value.lastHeartbeatAt ?? value.createdAt,
  };
}

function scopeContract(value: MockBastionScope): BastionScope {
  return {
    id: value.id,
    enterprise_id: value.enterpriseId,
    name: value.name,
    environment: value.environment,
    labels: value.labels,
    status: value.status === "degraded" ? "suspected_offline" : value.status,
    connector_host_id: value.connectorHostId,
    active_connector_id: value.activeConnectorId,
    fencing_generation: 1,
    member_count: value.memberHostIds.length,
    resource_version: value.resourceVersion ?? 1,
    created_at: value.createdAt,
    updated_at: value.updatedAt,
  };
}

function page<T>(items: T[], limit: number | undefined) {
  return {
    items: items.slice(0, limit),
    page: {
      next_cursor: null,
      has_more: limit !== undefined && items.length > limit,
      partial: { partial: false as const, reasons: [] },
    },
  };
}

/** Mock implementation of the same generated M3 Connector Port as real mode. */
export function createConnectorsDomain(
  ctx: MockContext,
): ArgusApiClient["connectors"] {
  const { db } = ctx;
  return {
    async list(query): Promise<ConnectorPage> {
      await ctx.pause();
      const items = db.connectors
        .filter((entry) => entry.enterpriseId === ctx.enterpriseId())
        .map(connectorContract);
      return page(items, query?.limit);
    },
    async get(id) {
      await ctx.pause();
      return connectorContract(
        ctx.mustFind(db.connectors, (entry) => entry.id === id, "connector"),
      );
    },
    async listBastionScopes(query): Promise<BastionScopePage> {
      await ctx.pause();
      const items = db.bastionScopes
        .filter((entry) => entry.enterpriseId === ctx.enterpriseId())
        .map(scopeContract);
      return page(items, query?.limit);
    },
    async getBastionScope(id) {
      await ctx.pause();
      return scopeContract(
        ctx.mustFind(
          db.bastionScopes,
          (entry) => entry.id === id,
          "bastion scope",
        ),
      );
    },
    async previewCreateBastionScope(input) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "bastion.scope.create",
        title: `创建堡垒机范围 ${input.name}`,
        input_data: { ...input },
      });
    },
    async previewUpdateBastionScope(scopeId, input) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "bastion.scope.update",
        title: `更新堡垒机范围 ${scopeId}`,
        input_data: { scope_id: scopeId, ...input },
      });
    },
    async previewDeleteBastionScope(scopeId, expectedVersion) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "bastion.scope.delete",
        title: `删除堡垒机范围 ${scopeId}`,
        input_data: { scope_id: scopeId, expected_version: expectedVersion },
      });
    },
    async previewReplaceBastionConnector(scopeId, expectedVersion) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "bastion.connector.replace",
        title: `替换堡垒机 Connector ${scopeId}`,
        input_data: { scope_id: scopeId, expected_version: expectedVersion },
      });
    },
    async previewUninstallConnector(connectorId, expectedVersion) {
      await ctx.pause();
      const connector = ctx.mustFind(
        db.connectors,
        (entry) => entry.id === connectorId,
        "connector",
      );
      if (connector.status !== "online") {
        throw new Error("connector must be online before uninstall");
      }
      return ctx.createPendingAction({
        tool: "connector.uninstall",
        title: `卸载 Connector ${connector.name}`,
        input_data: {
          connector_id: connectorId,
          expected_version: expectedVersion,
        },
      });
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
      return connectorContract(connector);
    },
  };
}
