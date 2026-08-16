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

export interface SetupSubmission {
  /** Deployment-provided one-time Setup Token. */
  setupToken: string;
  platformName: string;
  defaultLocale: "zh-CN" | "en-US";
  timezone: string;
  externalUrl: string;
  superAdmin: SetupSuperAdminInput;
}

export interface SetupResult {
  success: boolean;
  superAdminUserId: string;
}
