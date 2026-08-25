import type { ISODateString, RiskLevel } from "./common";
import type {
  ApiKey,
  DataScope,
  EnterpriseUser,
  EnterpriseUserUpdate,
  Permission,
  PlatformUser,
  RoleBinding,
  Session,
} from "../generated/contracts";

export type {
  ApiKey,
  DataScope,
  Department,
  Role,
  RoleBinding,
  ServiceAccount,
} from "../generated/contracts";

export type PlatformRole = "platform_super_admin";
export type UserStatus = "active" | "invited" | "disabled";

export interface User {
  id: string;
  username: string;
  displayName: string;
  email?: string;
  platformRole?: PlatformRole;
  status: UserStatus;
  mfaEnabled: boolean;
  lastLoginAt?: ISODateString;
  createdAt: ISODateString;
  version?: number;
  temporaryPassword?: string;
  temporaryPasswordExpiresAt?: ISODateString;
}

export type RoleBindingSubjectType = RoleBinding["subject_type"];
export type RoleBindingStatus = RoleBinding["status"];

export interface CreateRoleBindingInput {
  subject_type: RoleBindingSubjectType;
  subject_id: string;
  role_id: string;
  data_scope_ids: string[];
  valid_from?: ISODateString;
  valid_until?: ISODateString;
  status?: RoleBindingStatus;
}

export interface UpdateRoleBindingInput {
  data_scope_ids?: string[];
  status?: RoleBindingStatus;
  valid_from?: ISODateString | null;
  valid_until?: ISODateString | null;
}

export interface CreateRoleInput {
  name: string;
  description?: string;
  permissions: string[];
}

export interface SaveDataScopeInput {
  id?: string;
  name: string;
  description?: string;
  resource_types: DataScope["resource_types"];
  explicit_resource_ids: string[];
  match_all: boolean;
  label_selector?: DataScope["label_selector"];
  status?: DataScope["status"];
}

export interface CreateServiceAccountInput {
  name: string;
  description?: string;
  allowed_tool_ids?: string[];
  data_scope_ids?: string[];
}

export interface CreateApiKeyInput {
  name: string;
  expires_at?: string;
}

export interface ApprovalPolicy {
  id: string;
  enterpriseId: string;
  name: string;
  matchRiskLevels: Array<Exclude<RiskLevel, "read">>;
  toolIds?: string[];
  resourceTypes?: string[];
  labelSelector?: import("../generated/contracts").LabelSelector;
  minApprovers: number;
  approverRoleIds: string[];
  separationOfDuty: boolean;
  expiresAfterSeconds?: number;
  enabled: boolean;
  createdAt: ISODateString;
}

export interface CreatedApiKey {
  api_key: ApiKey;
  secret: string;
}

export interface LoginInput {
  username: string;
  password: string;
}

export interface SessionInfo {
  session: Session;
  user: PlatformUser | EnterpriseUser;
  permissions: Permission[];
  amr: import("../generated/contracts").AuthenticationMethod[];
  mfa_state: import("../generated/contracts").MfaState;
  authenticated_at: string;
  step_up_expires_at?: string;
}

export interface InviteUserInput {
  username: string;
  display_name: string;
  email?: string;
  /** 邀请时创建企业范围 RoleBinding。 */
  role_ids?: string[];
  department_id: string;
}

export type UpdateEnterpriseUserInput = Omit<
  EnterpriseUserUpdate,
  "expected_version"
>;
