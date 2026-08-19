import type { ArgusApiClient } from "../client";
import type {
  KubernetesCluster,
  KubernetesClusterPage,
  KubernetesResource,
  KubernetesResourcePage,
  PodLogs,
} from "../generated/contracts";
import type { K8sWorkload } from "../provisional";
import type { MockKubernetesCluster } from "./resource-models";
import type { MockContext } from "./context";

function clusterContract(value: MockKubernetesCluster): KubernetesCluster {
  return {
    id: value.id,
    enterprise_id: value.enterpriseId,
    name: value.name,
    api_server: value.apiServer,
    connection_mode: value.connectionMode,
    bastion_scope_id: value.bastionScopeId,
    connector_id: value.connectorId,
    credential_id: value.credentialRef || undefined,
    environment: value.environment,
    labels: value.labels,
    labels_version: 1,
    resource_version: value.resourceVersion ?? 1,
    connection_status: value.connectionStatus,
    kubernetes_version: value.version || undefined,
    node_count: value.nodeCount,
    ready_node_count: value.readyNodeCount,
    status: "active",
    created_at: value.createdAt,
    updated_at: value.updatedAt,
  };
}

function clusterPage(
  items: MockKubernetesCluster[],
  limit?: number,
): KubernetesClusterPage {
  return {
    items: items.slice(0, limit).map(clusterContract),
    page: {
      next_cursor: null,
      has_more: limit !== undefined && items.length > limit,
      partial: { partial: false, reasons: [] },
    },
  };
}

const WORKLOADS: K8sWorkload[] = [
  {
    clusterId: "k8s-prod-east",
    namespace: "production",
    kind: "Deployment",
    name: "checkout-api",
    ready: "12/12",
    restartCount: 2,
    status: "healthy",
  },
  {
    clusterId: "k8s-prod-east",
    namespace: "production",
    kind: "StatefulSet",
    name: "order-db",
    ready: "3/3",
    restartCount: 0,
    status: "healthy",
  },
  {
    clusterId: "k8s-staging",
    namespace: "staging",
    kind: "Deployment",
    name: "payment-worker",
    ready: "5/6",
    restartCount: 6,
    status: "degraded",
  },
  {
    clusterId: "k8s-staging",
    namespace: "staging",
    kind: "DaemonSet",
    name: "argus-otelcol",
    ready: "7/8",
    restartCount: 1,
    status: "degraded",
  },
];

function resourcePage(items: KubernetesResource[]): KubernetesResourcePage {
  return {
    items,
    page: {
      next_cursor: null,
      has_more: false,
      partial: { partial: false, reasons: [] },
    },
  };
}

function mockResources(
  clusterId: string,
  resourceType: KubernetesResource["resource_type"],
  bindings: MockContext["db"]["nodeBindings"],
): KubernetesResource[] {
  const workloads = WORKLOADS.filter((entry) => entry.clusterId === clusterId);
  if (resourceType === "namespace") {
    return [...new Set(workloads.map((entry) => entry.namespace))].map(
      (name) => ({
        cluster_id: clusterId,
        resource_type: resourceType,
        name,
        labels: {},
        summary: {
          workloads: workloads.filter((entry) => entry.namespace === name)
            .length,
        },
      }),
    );
  }
  if (resourceType === "node") {
    return bindings
      .filter((entry) => entry.kubernetes_cluster_id === clusterId)
      .map((entry) => ({
        cluster_id: clusterId,
        resource_type: resourceType,
        name: entry.node_name,
        labels: {},
        summary: { host: entry.host_id ?? "", status: entry.status },
      }));
  }
  if (resourceType === "pod") {
    return workloads.map((entry) => ({
      cluster_id: clusterId,
      resource_type: resourceType,
      namespace: entry.namespace,
      name: `${entry.name}-0`,
      labels: { workload: entry.name },
      summary: {
        workload: `${entry.kind}/${entry.name}`,
        ready: entry.ready,
        restarts: entry.restartCount,
        status: entry.status,
      },
    }));
  }
  const kind =
    resourceType === "deployment"
      ? "Deployment"
      : resourceType === "statefulset"
        ? "StatefulSet"
        : resourceType === "daemonset"
          ? "DaemonSet"
          : "Service";
  return workloads
    .filter((entry) => entry.kind === kind)
    .map((entry) => ({
      cluster_id: clusterId,
      resource_type: resourceType,
      namespace: entry.namespace,
      name: entry.name,
      labels: {},
      summary: {
        ready: entry.ready,
        restarts: entry.restartCount,
        status: entry.status,
      },
    }));
}

/** Kubernetes clusters, node bindings, claims and cluster collectors. */
export function createKubernetesDomain(
  ctx: MockContext,
): ArgusApiClient["kubernetes"] {
  const { db } = ctx;
  const connectionTests = new Map<
    string,
    Awaited<ReturnType<ArgusApiClient["kubernetes"]["createConnectionTest"]>>
  >();

  return {
    async listClusters(query) {
      await ctx.pause();
      const items = db.clusters.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      return clusterPage(items, query?.limit);
    },
    async getCluster(id) {
      await ctx.pause();
      return clusterContract(
        ctx.mustFind(db.clusters, (entry) => entry.id === id, "cluster"),
      );
    },
    async createConnectionTest(input) {
      await ctx.pause();
      const now = ctx.nowIso();
      const id = `ct-k8s-${Date.now()}`;
      const value = {
        id,
        enterprise_id: ctx.enterpriseId(),
        target_type: "kubernetes_cluster" as const,
        path:
          input.connection_mode === "via_bastion"
            ? ("connector" as const)
            : ("direct" as const),
        status: "succeeded" as const,
        checks: [
          {
            name: "api_server",
            status: "passed" as const,
            detail: input.api_server,
          },
          { name: "credentials", status: "passed" as const },
        ],
        latency_ms: 180,
        remote_version: "v1.31.0",
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
        tool: "kubernetes.cluster.create",
        title: `接入集群 ${input.name}`,
        input_data: { ...input },
      });
    },
    async previewUpdateResource(id, input) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "kubernetes.cluster.update",
        title: `更新集群 ${id}`,
        input_data: { id, ...input },
      });
    },
    async previewDeleteResource(id, expectedVersion) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "kubernetes.cluster.delete",
        title: `删除集群 ${id}`,
        input_data: { id, expected_version: expectedVersion },
      });
    },
    async listResources(clusterId, query) {
      await ctx.pause();
      let items = mockResources(
        clusterId,
        query.resource_type,
        db.nodeBindings,
      );
      if (query.namespace) {
        items = items.filter((entry) => entry.namespace === query.namespace);
      }
      if (query.query) {
        items = items.filter((entry) => entry.name.includes(query.query ?? ""));
      }
      return resourcePage(items.slice(0, query.limit));
    },
    async getPodLogs(clusterId, query): Promise<PodLogs> {
      await ctx.pause();
      const content = [
        "2026-08-16T08:00:00Z INFO pod started",
        "2026-08-16T08:00:01Z INFO readiness probe passed",
        "2026-08-16T08:00:02Z INFO request handled status=200",
      ].join("\n");
      return {
        cluster_id: clusterId,
        namespace: query.namespace,
        pod: query.pod,
        container: query.container,
        content,
        truncated: false,
        bytes: new TextEncoder().encode(content).byteLength,
      };
    },
    async listWorkloads(clusterId, filter) {
      await ctx.pause();
      return WORKLOADS.filter(
        (entry) =>
          entry.clusterId === clusterId &&
          (!filter?.namespaces?.length ||
            filter.namespaces.includes(entry.namespace)) &&
          (!filter?.query || entry.name.includes(filter.query)) &&
          (filter?.minRestartCount === undefined ||
            entry.restartCount >= filter.minRestartCount),
      );
    },
    async listNodeBindings(clusterId) {
      await ctx.pause();
      return db.nodeBindings.filter(
        (entry) => entry.kubernetes_cluster_id === clusterId,
      );
    },
    async verifyNodeBinding(bindingId, input) {
      await ctx.pause();
      const binding = ctx.mustFind(
        db.nodeBindings,
        (entry) => entry.id === bindingId,
        "node binding",
      );
      return ctx.createPendingAction({
        tool: "telemetry.node_binding.confirm",
        title: `验证节点绑定 ${binding.node_name} ↔ ${input.host_id}`,
        input_data: {
          binding_id: binding.id,
          host_id: input.host_id,
          expected_version: input.expected_version,
        },
      });
    },
    async listCollectionClaims(clusterId) {
      await ctx.pause();
      return db.collectionClaims.filter(
        (entry) =>
          entry.enterprise_id === ctx.enterpriseId() &&
          (clusterId === undefined ||
            entry.physical_resource_ref.includes(clusterId)),
      );
    },
    async getCollector(clusterId) {
      await ctx.pause();
      return (
        db.collectors.find(
          (entry) =>
            entry.resource_type === "kubernetes_cluster" &&
            entry.resource_id === clusterId,
        ) ?? null
      );
    },
    async previewCollectorAction(clusterId, action, input) {
      await ctx.pause();
      const cluster = ctx.mustFind(
        db.clusters,
        (entry) => entry.id === clusterId,
        "cluster",
      );
      return ctx.createPendingAction({
        tool: `telemetry.kubernetes.${action}`,
        title: `${action} DaemonSet Collector · ${cluster.name}`,
        input_data: {
          cluster_id: clusterId,
          distribution_version_id: input.distribution_version_id,
          profile_ids: input.profile_ids,
          route_kind: input.route_kind,
          gateway_collector_id: input.gateway_collector_id,
          expected_version: input.expected_version,
        },
      });
    },
    async previewCollectorInstall(clusterId, input) {
      return this.previewCollectorAction(clusterId, "install", input);
    },
  };
}
