import type {
  CollectorInstance,
  KubernetesCluster,
  KubernetesNodeHostBinding,
} from "@argus/api-client";
import type { K8sWorkload } from "@argus/api-client/provisional";

type ClusterConnectionStatus = KubernetesCluster["connection_status"];

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

export function connectionStatusTone(status: ClusterConnectionStatus): Tone {
  if (status === "connected") return "success";
  if (status === "degraded") return "warning";
  return "danger";
}

export function collectorStatusTone(
  status: CollectorInstance["status"] | "not_installed",
): Tone {
  switch (status) {
    case "converged":
      return "success";
    case "installing":
      return "info";
    case "backlog":
      return "warning";
    case "degraded":
    case "result_unknown":
      return "danger";
    default:
      return "neutral";
  }
}

export function workloadStatusTone(status: K8sWorkload["status"]): Tone {
  if (status === "healthy") return "success";
  if (status === "degraded") return "warning";
  return "danger";
}

export function bindingStatusTone(
  status: KubernetesNodeHostBinding["status"],
): Tone {
  if (status === "verified") return "success";
  if (status === "proposed") return "info";
  return "danger";
}

/** verified 绑定数 / 集群节点数。 */
export function bindingCoverage(
  cluster: KubernetesCluster,
  bindings: KubernetesNodeHostBinding[] | undefined,
): { verified: number; total: number; percent: number } {
  const verified = (bindings ?? []).filter(
    (entry) => entry.status === "verified",
  ).length;
  const total = cluster.node_count ?? 0;
  const percent = total > 0 ? Math.round((verified / total) * 100) : 0;
  return { verified, total, percent };
}
