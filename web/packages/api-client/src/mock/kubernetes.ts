import type { ArgusApiClient } from "../client";
import type { K8sWorkload } from "../types";
import type { MockContext } from "./context";

const WORKLOADS: K8sWorkload[] = [
  {
    clusterId: "k8s-prod-east", namespace: "production", kind: "Deployment",
    name: "checkout-api", ready: "12/12", restartCount: 2, status: "healthy",
  },
  {
    clusterId: "k8s-prod-east", namespace: "production", kind: "StatefulSet",
    name: "order-db", ready: "3/3", restartCount: 0, status: "healthy",
  },
  {
    clusterId: "k8s-staging", namespace: "staging", kind: "Deployment",
    name: "payment-worker", ready: "5/6", restartCount: 6, status: "degraded",
  },
  {
    clusterId: "k8s-staging", namespace: "staging", kind: "DaemonSet",
    name: "argus-otelcol", ready: "7/8", restartCount: 1, status: "degraded",
  },
];

/** Kubernetes clusters, node bindings, claims and cluster collectors. */
export function createKubernetesDomain(
  ctx: MockContext,
): ArgusApiClient["kubernetes"] {
  const { db } = ctx;

  return {
    async listClusters(query) {
      await ctx.pause();
      const items = db.clusters.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      return ctx.paginate(items, query);
    },
    async getCluster(id) {
      await ctx.pause();
      return ctx.mustFind(db.clusters, (entry) => entry.id === id, "cluster");
    },
    async previewCreateCluster(input) {
      await ctx.pause();
      return ctx.createPendingAction({
        tool: "kubernetes.cluster.create",
        title: `接入集群 ${input.name}`,
        input_data: { ...input },
      });
    },
    async updateCluster(id, patch) {
      await ctx.pause();
      const cluster = ctx.mustFind(
        db.clusters,
        (entry) => entry.id === id,
        "cluster",
      );
      Object.assign(cluster, patch, { updatedAt: ctx.nowIso() });
      ctx.audit("kubernetes.cluster.update", {
        resourceType: "kubernetes_cluster",
        resourceId: id,
        summary: `更新集群 ${cluster.name}`,
      });
      ctx.save();
      return cluster;
    },
    async deleteCluster(id) {
      await ctx.pause();
      const cluster = ctx.mustFind(
        db.clusters,
        (entry) => entry.id === id,
        "cluster",
      );
      db.clusters = db.clusters.filter((entry) => entry.id !== id);
      db.nodeBindings = db.nodeBindings.filter(
        (entry) => entry.clusterId !== id,
      );
      ctx.audit("kubernetes.cluster.delete", {
        resourceType: "kubernetes_cluster",
        resourceId: id,
        summary: `移除集群 ${cluster.name}`,
      });
      ctx.save();
    },
    async testClusterConnection(id) {
      await ctx.pause();
      const cluster = ctx.mustFind(
        db.clusters,
        (entry) => entry.id === id,
        "cluster",
      );
      const ok = cluster.connectionStatus !== "disconnected";
      return {
        success: ok,
        latencyMs: ok ? 240 : 0,
        checks: [
          {
            name: "api_server",
            status: ok ? "passed" : "failed",
            detail: cluster.apiServer,
          },
          { name: "credentials", status: ok ? "passed" : "skipped" },
          {
            name: "rbac_scope",
            status: "passed",
            detail: "namespace scope verified",
          },
        ],
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
      return db.nodeBindings.filter((entry) => entry.clusterId === clusterId);
    },
    async verifyNodeBinding(bindingId, input) {
      await ctx.pause();
      const binding = ctx.mustFind(
        db.nodeBindings,
        (entry) => entry.id === bindingId,
        "node binding",
      );
      binding.hostId = input.hostId;
      binding.status = "verified";
      binding.updatedAt = ctx.nowIso();
      ctx.audit("kubernetes.node_binding.verify", {
        resourceType: "kubernetes_cluster",
        resourceId: binding.clusterId,
        summary: `验证节点绑定 ${binding.nodeName} ↔ ${input.hostId}`,
      });
      ctx.save();
      return binding;
    },
    async listCollectionClaims(clusterId) {
      await ctx.pause();
      return db.collectionClaims.filter(
        (entry) =>
          entry.enterpriseId === ctx.enterpriseId() &&
          (clusterId === undefined || entry.clusterId === clusterId),
      );
    },
    async getCollector(clusterId) {
      await ctx.pause();
      return (
        db.collectors.find(
          (entry) =>
            entry.targetType === "kubernetes_cluster" &&
            entry.targetId === clusterId,
        ) ?? null
      );
    },
    async previewCollectorInstall(clusterId, input) {
      await ctx.pause();
      const cluster = ctx.mustFind(
        db.clusters,
        (entry) => entry.id === clusterId,
        "cluster",
      );
      return ctx.createPendingAction({
        tool: "telemetry.kubernetes.install",
        title: `安装 DaemonSet Collector · ${cluster.name}`,
        input_data: { clusterId, profile: input.profile },
      });
    },
  };
}
