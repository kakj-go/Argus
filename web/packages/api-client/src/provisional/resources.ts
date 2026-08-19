import type { KubernetesCluster } from "../generated/contracts";

export interface K8sWorkload {
  clusterId: string;
  namespace: string;
  kind: "Deployment" | "StatefulSet" | "DaemonSet";
  name: string;
  ready: string;
  restartCount: number;
  status: "healthy" | "degraded" | "failed";
}

export interface K8sWorkloadFilter {
  namespaces?: string[];
  query?: string;
  kinds?: K8sWorkload["kind"][];
  minRestartCount?: number;
}

export type ProvisionalKubernetesCluster = KubernetesCluster;
