import type { Environment, ISODateString } from "./common";

export type ClusterConnectionMode = "via_bastion" | "direct" | "in_cluster";

export type ClusterConnectionStatus =
  | "connected"
  | "degraded"
  | "disconnected";

export interface K8sCluster {
  id: string;
  enterpriseId: string;
  name: string;
  apiServer: string;
  connectionMode: ClusterConnectionMode;
  bastionScopeId?: string;
  connectorId?: string;
  /** kubeconfig is stored as a Secret; only the reference is kept here. */
  credentialRef: string;
  version: string;
  environment: Environment;
  labels: Record<string, string>;
  connectionStatus: ClusterConnectionStatus;
  nodeCount: number;
  readyNodeCount: number;
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface CreateK8sClusterInput {
  name: string;
  apiServer: string;
  connectionMode: ClusterConnectionMode;
  bastionScopeId?: string;
  credentialRef: string;
  defaultNamespace?: string;
  environment: Environment;
  labels?: Record<string, string>;
}

export interface UpdateK8sClusterInput {
  name?: string;
  credentialRef?: string;
  environment?: Environment;
  labels?: Record<string, string>;
}

/** Trusted physical binding between a Kubernetes Node and an Argus Host. */
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

export type CollectionSignal = "metrics" | "logs" | "traces";

/**
 * A collector's claim of responsibility for a physical resource + signal.
 * Two active owners of the same claim produce a conflict.
 */
export interface CollectionClaim {
  id: string;
  enterpriseId: string;
  clusterId?: string;
  hostId?: string;
  nodeBindingId?: string;
  collectorId: string;
  profile: string;
  signal: CollectionSignal;
  scope: string;
  status: "active" | "conflict" | "released";
  conflictWithId?: string;
  createdAt: ISODateString;
}

/** Flat workload row for cluster detail lists. */
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
