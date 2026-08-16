import type {
  ConnectionTestResult,
  Environment,
  ISODateString,
} from "./common";

/** Host connection modes fixed in docs/03 §2.2. */
export type HostConnectionMode =
  "connector_local" | "via_bastion" | "direct_ssh" | "direct_winrm";

export type HostPlatform = "linux" | "windows";

export type HostConnectionStatus =
  "online" | "offline" | "onboarding" | "degraded" | "unknown";

/** Collector lifecycle shown in the host detail "installed components" area. */
export type CollectorStatus =
  | "not_installed"
  | "installing"
  | "converged"
  | "config_stale"
  | "backlog"
  | "interrupted";

export interface Host {
  id: string;
  enterpriseId: string;
  name: string;
  hostname: string;
  address: string;
  port: number;
  platform: HostPlatform;
  connectionMode: HostConnectionMode;
  /** Required when connectionMode is via_bastion or connector_local. */
  bastionScopeId?: string;
  connectorId?: string;
  /** Only a secret reference is stored; values are never exposed. */
  credentialRef?: string;
  environment: Environment;
  labels: Record<string, string>;
  connectionStatus: HostConnectionStatus;
  collectorStatus: CollectorStatus;
  /** Telemetry route label, e.g. an Edge Gateway the host pushes through. */
  telemetryRoute?: string;
  lastSeenAt?: ISODateString;
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface CreateHostInput {
  name: string;
  hostname?: string;
  address: string;
  port: number;
  platform: HostPlatform;
  connectionMode: HostConnectionMode;
  bastionScopeId?: string;
  credentialRef?: string;
  environment: Environment;
  labels?: Record<string, string>;
}

export interface UpdateHostInput {
  name?: string;
  address?: string;
  port?: number;
  credentialRef?: string;
  environment?: Environment;
  labels?: Record<string, string>;
}

export interface HostFilter {
  query?: string;
  status?: HostConnectionStatus[];
  platform?: HostPlatform[];
  connectionMode?: HostConnectionMode[];
  bastionScopeId?: string;
  environment?: Environment[];
  labels?: Record<string, string[]>;
}

export type ConnectorStatus = "online" | "offline" | "uninstalled";

export interface Connector {
  id: string;
  enterpriseId: string;
  name: string;
  hostId: string;
  bastionScopeId: string;
  version: string;
  status: ConnectorStatus;
  capabilities: string[];
  /** Monotonic connection epoch assigned by the gateway. */
  connectionEpoch: number;
  certificateExpiresAt: ISODateString;
  connectedAt?: ISODateString;
  lastHeartbeatAt?: ISODateString;
  managedHostCount: number;
  createdAt: ISODateString;
}

export type BastionScopeStatus =
  "pending" | "active" | "degraded" | "uninstalling" | "uninstalled";

export type ConnectorEnrollmentPurpose =
  "initial_registration" | "connector_replacement";

export type ConnectorEnrollmentStatus =
  "active" | "consumed" | "revoked" | "expired";

/** One-time enrollment token for initial registration or Connector replacement. */
export interface ConnectorEnrollmentToken {
  id: string;
  enterpriseId: string;
  bastionScopeId: string;
  purpose: ConnectorEnrollmentPurpose;
  status: ConnectorEnrollmentStatus;
  token: string;
  installCommand: string;
  expiresAt: ISODateString;
  remainingUses: number;
  consumedAt?: ISODateString;
  consumedByDeviceFingerprint?: string;
  registeredConnectorId?: string;
  registeredHostId?: string;
  createdBy: string;
  createdAt: ISODateString;
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

export interface BastionScope {
  id: string;
  enterpriseId: string;
  name: string;
  environment: Environment;
  labels: Record<string, string>;
  status: BastionScopeStatus;
  connectorHostId?: string;
  activeConnectorId?: string;
  memberHostIds: string[];
  defaultTelemetryRoute?: string;
  /** Currently active, unconsumed installation or replacement command. */
  registrationToken?: ConnectorEnrollmentToken;
  /** Currently active command for uninstalling the active Connector. */
  uninstallCommand?: ConnectorUninstallCommand;
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface CreateBastionScopeInput {
  name: string;
  environment: Environment;
  labels?: Record<string, string>;
}

export type UpdateBastionScopeInput = Partial<CreateBastionScopeInput>;

export interface CreateBastionScopeResult {
  scope: BastionScope;
  enrollmentToken: ConnectorEnrollmentToken;
}

/** Remote access session state machine from docs/03 §2.3. */
export type RemoteSessionStatus =
  | "requested"
  | "awaiting_approval"
  | "authorized"
  | "connecting"
  | "active"
  | "terminated"
  | "failed"
  | "expired"
  | "connection_lost";

export interface RemoteSession {
  id: string;
  enterpriseId: string;
  userId: string;
  userName?: string;
  hostId: string;
  hostName?: string;
  connectionMode: HostConnectionMode;
  connectorId?: string;
  bastionScopeId?: string;
  protocol: "ssh" | "winrm";
  targetAccount: string;
  reason: string;
  approvalRef?: string;
  status: RemoteSessionStatus;
  startedAt?: ISODateString;
  lastActivityAt?: ISODateString;
  endedAt?: ISODateString;
  recordingRef?: string;
  createdAt: ISODateString;
}

export interface CreateRemoteSessionInput {
  hostId: string;
  targetAccount: string;
  reason: string;
  protocol?: "ssh" | "winrm";
}

/** Collector install/convergence state tracked on a host or cluster. */
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
  /** 0-100 while status is installing. */
  progress: number;
  updatedAt: ISODateString;
}

export type { ConnectionTestResult };
