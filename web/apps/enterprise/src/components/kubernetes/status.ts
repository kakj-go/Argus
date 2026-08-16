import type {
  ClusterConnectionStatus,
  CollectorInstallState,
  K8sCluster,
  K8sNodeBinding,
  K8sWorkload,
} from "@argus/api-client";

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

export function connectionStatusTone(status: ClusterConnectionStatus): Tone {
  if (status === "connected") return "success";
  if (status === "degraded") return "warning";
  return "danger";
}

export function collectorStatusTone(
  status: CollectorInstallState["status"] | "not_installed",
): Tone {
  switch (status) {
    case "converged":
      return "success";
    case "installing":
      return "info";
    case "backlog":
      return "warning";
    case "interrupted":
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

export function bindingStatusTone(status: K8sNodeBinding["status"]): Tone {
  if (status === "verified") return "success";
  if (status === "proposed") return "info";
  return "danger";
}

/** verified 绑定数 / 集群节点数。 */
export function bindingCoverage(
  cluster: K8sCluster,
  bindings: K8sNodeBinding[] | undefined,
): { verified: number; total: number; percent: number } {
  const verified = (bindings ?? []).filter(
    (entry) => entry.status === "verified",
  ).length;
  const total = cluster.nodeCount;
  const percent = total > 0 ? Math.round((verified / total) * 100) : 0;
  return { verified, total, percent };
}
