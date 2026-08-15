/** One-time initialization wizard payloads (docs/07 §2-4). */

export type PlatformState = "uninitialized" | "initializing" | "initialized";

export interface SetupStatus {
  state: PlatformState;
  platformName?: string;
}

export interface SetupSuperAdminInput {
  username: string;
  displayName: string;
  email?: string;
  password: string;
}

export interface SetupSandboxInput {
  enabled: boolean;
  endpoint?: string;
  credentialRef?: string;
  defaultStorage?: string;
}

export interface SetupSubmission {
  /** Deployment-provided one-time Setup Token. */
  setupToken: string;
  platformName: string;
  defaultLocale: "zh-CN" | "en-US";
  timezone: string;
  externalUrl: string;
  superAdmin: SetupSuperAdminInput;
  sandbox: SetupSandboxInput;
}

export interface SetupResult {
  success: boolean;
  superAdminUserId: string;
}
