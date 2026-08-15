import type { ISODateString } from "./common";

export type SecretType =
  | "ssh_password"
  | "ssh_private_key"
  | "winrm_password"
  | "kubeconfig"
  | "api_token"
  | "basic_auth";

/**
 * Secrets only ever expose metadata. Values are write-only, encrypted at
 * rest, and every access produces an audit event (docs/03 §10).
 */
export interface Secret {
  id: string;
  enterpriseId: string;
  name: string;
  type: SecretType;
  description?: string;
  referenceCount: number;
  lastAccessedAt?: ISODateString;
  createdBy: string;
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface CreateSecretInput {
  name: string;
  type: SecretType;
  description?: string;
  value: string;
}

export interface UpdateSecretInput {
  name?: string;
  description?: string;
  /** Rotates the stored value; never returned back. */
  value?: string;
}
