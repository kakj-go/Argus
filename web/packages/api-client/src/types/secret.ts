import type { Secret } from "../generated/contracts";

export type SecretType = Secret["type"];

export type {
  Credential,
  CredentialCreate,
  CredentialUpdate,
  ManagedAccount,
  ManagedAccountCreate,
  ManagedAccountUpdate,
  Secret,
  SecretCreate as CreateSecretInput,
  SecretUpdate as UpdateSecretInput,
} from "../generated/contracts";
