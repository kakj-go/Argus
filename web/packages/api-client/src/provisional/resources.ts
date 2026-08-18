import type { ISODateString } from "../types/common";
import type { KubernetesCluster } from "../generated/contracts";

export type CollectorStatus =
  | "not_installed"
  | "installing"
  | "converged"
  | "config_stale"
  | "backlog"
  | "interrupted";

export interface CollectorInstallState {
  id: string;
  enterpriseId: string;
  targetType: "host" | "kubernetes_cluster";
  targetId: string;
  role: "leaf" | "edge_gateway" | "daemonset" | "direct";
  profile: string;
  version: string;
  desiredRevision: number;
  effectiveRevision: number;
  status: "installing" | "converged" | "backlog" | "interrupted";
  progress: number;
  updatedAt: ISODateString;
}

export interface K8sNodeBinding {
  id: string;
  enterpriseId: string;
  clusterId: string;
  nodeName: string;
  hostId?: string;
  status: "proposed" | "verified" | "rejected";
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface CollectionClaim {
  id: string;
  enterpriseId: string;
  clusterId?: string;
  hostId?: string;
  nodeBindingId?: string;
  collectorId: string;
  profile: string;
  signal: "metrics" | "logs" | "traces";
  scope: string;
  status: "active" | "conflict" | "released";
  conflictWithId?: string;
  createdAt: ISODateString;
}

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
