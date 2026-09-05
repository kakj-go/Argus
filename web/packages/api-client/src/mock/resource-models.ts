import type {
  ActionOneTimeResult,
  CollectorInstance,
  OnboardingProjection,
} from "../generated/contracts";
import type { Environment, ISODateString } from "../types/common";

export type MockHostConnectionMode =
  | "connector_local"
  | "self_enrolled"
  | "via_bastion"
  | "direct_ssh"
  | "direct_winrm";

export interface MockHost {
  id: string;
  enterpriseId: string;
  name: string;
  hostname: string;
  address: string;
  port: number;
  platform: "linux" | "windows";
  architecture?: "amd64" | "arm64";
  connectionMode: MockHostConnectionMode;
  bastionScopeId?: string;
  connectorId?: string;
  credentialRef?: string;
  environment: Environment;
  labels: Record<string, string>;
  connectionStatus:
    "online" | "offline" | "onboarding" | "degraded" | "unknown";
  collectorStatus: CollectorInstance["status"] | "not_installed";
  liveStatus?: "online" | "offline" | "key_changed";
  probeLatencyMs?: number;
  telemetryRoute?: string;
  lastSeenAt?: ISODateString;
  createdAt: ISODateString;
  updatedAt: ISODateString;
  resourceVersion?: number;
  onboardingState?: OnboardingProjection["state"];
  onboardingExecutionId?: string;
  onboardingOperationId?: string;
  onboardingErrorCode?: string;
}

export interface MockConnector {
  id: string;
  enterpriseId: string;
  name: string;
  hostId: string;
  bastionScopeId: string;
  version: string;
  status: "online" | "offline" | "uninstalled" | "revoked";
  capabilities: string[];
  connectionEpoch: number;
  certificateExpiresAt: ISODateString;
  connectedAt?: ISODateString;
  lastHeartbeatAt?: ISODateString;
  managedHostCount: number;
  createdAt: ISODateString;
}

export type ConnectorEnrollmentPurpose =
  "initial_registration" | "connector_replacement";
export type ConnectorEnrollmentStatus =
  "active" | "consumed" | "revoked" | "expired";

export interface HostEnrollmentToken {
  id: string;
  enterpriseId: string;
  hostId: string;
  status: "active" | "consumed" | "revoked" | "expired";
  token: string;
  instructionSets: ActionOneTimeResult["instruction_sets"];
  expiresAt: string;
  remainingUses: number;
  createdBy: string;
  createdAt: string;
}

export interface ConnectorEnrollmentToken {
  id: string;
  enterpriseId: string;
  bastionScopeId: string;
  purpose: ConnectorEnrollmentPurpose;
  status: ConnectorEnrollmentStatus;
  token: string;
  instructionSets: ActionOneTimeResult["instruction_sets"];
  expiresAt: ISODateString;
  remainingUses: number;
  consumedAt?: ISODateString;
  consumedByDeviceFingerprint?: string;
  registeredConnectorId?: string;
  registeredHostId?: string;
  createdBy: string;
  createdAt: ISODateString;
}

export interface ConnectorUninstallCommand {
  id: string;
  enterpriseId: string;
  bastionScopeId: string;
  connectorId: string;
  connectionEpoch: number;
  status: ConnectorEnrollmentStatus;
  token: string;
  uninstallCommand: string;
  expiresAt: ISODateString;
  consumedAt?: ISODateString;
  createdBy: string;
  createdAt: ISODateString;
}

export interface MockBastionScope {
  id: string;
  enterpriseId: string;
  name: string;
  environment: Environment;
  labels: Record<string, string>;
  status: "pending" | "active" | "degraded" | "uninstalling" | "uninstalled";
  connectorHostId?: string;
  activeConnectorId?: string;
  memberHostIds: string[];
  defaultTelemetryRoute?: string;
  registrationToken?: ConnectorEnrollmentToken;
  uninstallCommand?: ConnectorUninstallCommand;
  createdAt: ISODateString;
  updatedAt: ISODateString;
  resourceVersion?: number;
  onboardingMode?: "command" | "direct_install" | "direct_install_tunnel";
  onboardingState?: OnboardingProjection["state"];
  onboardingExecutionId?: string;
  onboardingOperationId?: string;
  onboardingErrorCode?: string;
  controlTunnelStatus?:
    | "desired"
    | "establishing"
    | "established"
    | "degraded"
    | "down"
    | "removed";
}

export interface MockKubernetesCluster {
  id: string;
  enterpriseId: string;
  name: string;
  apiServer: string;
  connectionMode: "via_bastion" | "direct" | "in_cluster";
  bastionScopeId?: string;
  connectorId?: string;
  credentialRef: string;
  version: string;
  environment: Environment;
  labels: Record<string, string>;
  connectionStatus: "connected" | "degraded" | "disconnected";
  nodeCount: number;
  readyNodeCount: number;
  createdAt: ISODateString;
  updatedAt: ISODateString;
  resourceVersion?: number;
}

export interface ConnectorRegistrationResult {
  success: boolean;
  code:
    | "registered"
    | "idempotent_retry"
    | "token_missing"
    | "token_expired"
    | "token_consumed"
    | "token_revoked";
  message: string;
  connectorId?: string;
  hostId?: string;
}

export interface ConnectorUninstallResult {
  success: boolean;
  code:
    | "uninstalled"
    | "already_uninstalled"
    | "command_missing"
    | "command_expired"
    | "command_revoked";
  message: string;
}
