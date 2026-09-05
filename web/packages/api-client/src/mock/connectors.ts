import type { ArgusApiClient } from "../client";
import type {
  BastionScope,
  BastionScopePage,
  Connector,
  ConnectorPage,
} from "../generated/contracts";
import type { MockBastionScope, MockConnector } from "./resource-models";
import type { MockContext } from "./context";
import { ApiError } from "../transport/errors";

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
    onboarding_mode: value.onboardingMode ?? "command",
    onboarding: {
      state:
        value.onboardingState ??
        (value.status === "pending" ? "command_available" : "registered"),
      execution_id: value.onboardingExecutionId,
      operation_id: value.onboardingOperationId,
      error_code: value.onboardingErrorCode,
      updated_at: value.updatedAt,
    },
    connector_host_id: value.connectorHostId,
    active_connector_id: value.activeConnectorId,
    control_tunnel_status: value.controlTunnelStatus,
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
      const name = input.name.toLocaleLowerCase();
      const conflicts =
        db.bastionScopes.some(
          (entry) =>
            entry.enterpriseId === ctx.enterpriseId() &&
            entry.name.toLocaleLowerCase() === name,
        ) ||
        db.hosts.some(
          (entry) =>
            entry.enterpriseId === ctx.enterpriseId() &&
            entry.name.toLocaleLowerCase() === name,
        );
      if (conflicts) {
        throw new ApiError(
          {
            code: "RESOURCE_NAME_CONFLICT",
            message_key: "errors.common.resource_name_conflict",
            request_id: `mock-request-${Date.now()}`,
            retryable: false,
          },
          409,
        );
      }
      return ctx.createPendingAction({
        tool: "bastion.scope.create",
        title: `创建堡垒机范围 ${input.name}`,
        input_data: { ...input },
      });
    },
    async previewUpdateBastionScope(scopeId, input) {
      await ctx.pause();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId,
        "bastion scope",
      );
      return ctx.createPendingAction({
        tool: "bastion.scope.update",
        input_data: { scope_id: scopeId, name: scope.name, ...input },
      });
    },
    async previewDeleteBastionScope(scopeId, expectedVersion) {
      await ctx.pause();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId,
        "bastion scope",
      );
      return ctx.createPendingAction({
        tool: "bastion.scope.delete",
        input_data: {
          scope_id: scopeId,
          name: scope.name,
          expected_version: expectedVersion,
        },
      });
    },
    async previewEnrollmentRotate(scopeId, expectedVersion) {
      await ctx.pause();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId,
        "bastion scope",
      );
      if (
        (scope.onboardingMode ?? "command") !== "command" ||
        scope.activeConnectorId
      ) {
        throw new Error("BASTION_ENROLLMENT_ROTATE_NOT_ALLOWED");
      }
      return ctx.createPendingAction({
        tool: "bastion.enrollment.rotate",
        title: `轮换堡垒机安装命令 ${scope.name}`,
        input_data: {
          scope_id: scopeId,
          name: scope.name,
          expected_version: expectedVersion,
        },
      });
    },
    async previewConnectorReplacement(scopeId, input) {
      await ctx.pause();
      const scope = ctx.mustFind(
        db.bastionScopes,
        (entry) => entry.id === scopeId,
        "bastion scope",
      );
      if (!scope.activeConnectorId) {
        throw new Error("CONNECTOR_COMMAND_STATE_CONFLICT");
      }
      if (
        (scope.onboardingMode === "direct_install" ||
          scope.onboardingMode === "direct_install_tunnel") &&
        (!input.address ||
          !input.port ||
          !input.username ||
          !input.credential_id ||
          !input.connection_test_id)
      ) {
        throw new Error("CONNECTION_TEST_REQUIRED");
      }
      return ctx.createPendingAction({
        tool: "bastion.connector.replace",
        title: `替换堡垒机 Connector ${scopeId}`,
        input_data: {
          scope_id: scopeId,
          name: scope.name,
          install_mode: scope.onboardingMode ?? "command",
          ...input,
        },
      });
    },
    async getInstallOperation(operationId) {
      await ctx.pause();
      const operation = ctx.mustFind(
        db.connectorInstallOperations,
        (entry) => entry.id === operationId,
        "connector install operation",
      );
      return {
        ...operation,
        events: operation.events.map((event) => ({ ...event })),
      };
    },
    async previewRetryInstallOperation(operationId) {
      await ctx.pause();
      const operation = ctx.mustFind(
        db.connectorInstallOperations,
        (entry) => entry.id === operationId,
        "connector install operation",
      );
      if (!["failed", "expired", "result_unknown"].includes(operation.status)) {
        throw new Error("CONNECTOR_INSTALL_RETRY_NOT_ALLOWED");
      }
      return ctx.createPendingAction({
        tool: "bastion.connector.install.retry",
        title: `重试 Connector 安装 ${operation.id}`,
        input_data: { operation_id: operation.id },
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
          name: connector.name,
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
