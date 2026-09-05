import type { AuditEvent } from "./audit";
import type { ISODateString as Iso } from "./common";

export type EnterpriseStatus = "active" | "suspended" | "disabled";

export interface Enterprise {
  id: string;
  name: string;
  code: string;
  status: EnterpriseStatus;
  timezone: string;
  sandboxQuotaProfile?: string;
  remark?: string;
  createdAt: Iso;
}

export interface CreateEnterpriseInput {
  name: string;
  code: string;
  timezone?: string;
  sandboxQuotaProfile?: string;
  remark?: string;
}

export interface UpdateEnterpriseInput {
  name?: string;
  timezone?: string;
  sandboxQuotaProfile?: string;
  remark?: string;
}

export type EnterpriseAdminCredentialStatus =
  "temporary_password" | "active" | "disabled";

export interface EnterpriseAdmin {
  id: string;
  enterpriseId: string;
  username: string;
  displayName: string;
  email?: string;
  credentialStatus: EnterpriseAdminCredentialStatus;
  lastLoginAt?: Iso;
  createdAt: Iso;
  version?: number;
  temporaryPassword?: string;
  temporaryPasswordExpiresAt?: Iso;
}

export interface CreateEnterpriseAdminInput {
  enterpriseId: string;
  username: string;
  displayName: string;
  email?: string;
}

export interface SandboxBackend {
  id: string;
  name: string;
  endpoint: string;
  credentialRef: string;
  tlsVerify: boolean;
  enabled: boolean;
  defaultStorage: string;
  healthStatus: "healthy" | "degraded" | "unreachable";
  createdAt: Iso;
}

export interface CreateSandboxBackendInput {
  name: string;
  endpoint: string;
  credentialRef: string;
  tlsVerify?: boolean;
  defaultStorage?: string;
}

export interface SandboxImageLanguage {
  name: string;
  version: string;
}

export interface SandboxImage {
  id: string;
  name: string;
  /** Immutable reference pinned by digest. */
  reference: string;
  digest: string;
  languages: SandboxImageLanguage[];
  scanStatus: "pending" | "passed" | "failed";
  signatureStatus: "verified" | "unsigned" | "failed";
  enabled: boolean;
  createdAt: Iso;
}

export interface CreateSandboxImageInput {
  name: string;
  reference: string;
  digest: string;
  languages: SandboxImageLanguage[];
}

export interface SandboxResources {
  cpu: number;
  memoryMb: number;
  diskMb: number;
  pids: number;
}

export interface SandboxTimeouts {
  commandSeconds: number;
  idleSeconds: number;
  lifetimeSeconds: number;
}

export interface SandboxNetworkPolicy {
  mode: "deny_all" | "allow_list";
  allowedDomains: string[];
}

export interface SandboxCapabilities {
  fileUpload: boolean;
  artifactDownload: boolean;
  secretInjection: boolean;
  gpu: boolean;
}

/** The only execution unit an AI may select; approved by super admins. */
export interface SandboxProfile {
  id: string;
  name: string;
  description: string;
  imageId: string;
  resources: SandboxResources;
  timeouts: SandboxTimeouts;
  network: SandboxNetworkPolicy;
  capabilities: SandboxCapabilities;
  builtin: boolean;
  enabled: boolean;
  createdAt: Iso;
}

export interface CreateSandboxProfileInput {
  name: string;
  description: string;
  imageId: string;
  resources: SandboxResources;
  timeouts: SandboxTimeouts;
  network: SandboxNetworkPolicy;
  capabilities: SandboxCapabilities;
}

/** Per-enterprise sandbox quota, set by super admins (docs/08 §9). */
export interface EnterpriseSandboxQuota {
  enterpriseId: string;
  allowedProfiles: string[];
  maxConcurrentSessions: number;
  maxDailySessionMinutes: number;
  maxDailyCpuMinutes: number;
  maxArtifactStorageMb: number;
  artifactRetentionDays: number;
}

/** Platform-visible sandbox session metadata only, never content. */
export interface SandboxSessionMeta {
  id: string;
  enterpriseId: string;
  userId: string;
  profileId: string;
  conversationId?: string;
  runId?: string;
  purpose?: string;
  status:
    | "requested"
    | "starting"
    | "running"
    | "idle"
    | "terminating"
    | "terminated"
    | "failed"
    | "rejected";
  startedAt?: Iso;
  lastActivityAt?: Iso;
  terminatedAt?: Iso;
}

/** Platform audit is kept separate from enterprise audit (docs/07 §11). */
export type PlatformAuditEvent = AuditEvent;

export type PKIBundleState =
  "stable" | "preparing" | "overlapping" | "retiring" | "failed";
export type PKIRotationDirection = "forward" | "rollback";

export type PKIBundleStatus = {
  epoch: number;
  state: PKIBundleState;
  direction: PKIRotationDirection;
  bundleSha256: string;
  currentCaFingerprints: string[];
  nextCaFingerprints: string[];
  startedAt: Iso;
  retireAt?: Iso;
  lastError?: string;
};

export type PKINodeKind =
  "connector" | "collector" | "kubernetes_connector" | "control_plane";

export type PKINodeTrustStatus =
  "pending" | "acked" | "failed" | "trust_expired";

export type PKINodeStatus = {
  id: string;
  kind: PKINodeKind;
  enterpriseId?: string;
  epoch: number;
  bundleSha256?: string;
  caFingerprints?: string[];
  status: PKINodeTrustStatus;
  blocksCutover: boolean;
  error?: string;
  acknowledgedAt?: Iso;
  updatedAt: Iso;
};

export interface PlatformPKIStatus {
  bundles: PKIBundleStatus[];
  nodes: PKINodeStatus[];
  acknowledgedNodes: number;
  pendingNodes: number;
  failedNodes: number;
  trustExpiredNodes: number;
}
