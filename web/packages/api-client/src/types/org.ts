import type { Environment, ISODateString, RiskLevel } from "./common";

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
}

export interface EnterpriseMembership {
  userId: string;
  enterpriseId: string;
  departmentId: string;
}

export interface Project {
  id: string;
  enterpriseId: string;
  name: string;
  description?: string;
  default: boolean;
  createdAt: ISODateString;
}

export type RoleBindingSubjectType = "user" | "department";
export type RoleBindingScopeType = "enterprise" | "project";
export type RoleBindingStatus = "active" | "disabled";

export interface RoleBinding {
  id: string;
  enterpriseId: string;
  subjectType: RoleBindingSubjectType;
  subjectId: string;
  roleId: string;
  /** enterprise→当前企业 id；project→项目 id。 */
  scopeType: RoleBindingScopeType;
  scopeId: string;
  validFrom?: ISODateString;
  validUntil?: ISODateString;
  status: RoleBindingStatus;
  createdAt: ISODateString;
}

export interface CreateRoleBindingInput {
  subjectType: RoleBindingSubjectType;
  subjectId: string;
  roleId: string;
  scopeType: RoleBindingScopeType;
  scopeId: string;
  validFrom?: ISODateString;
  validUntil?: ISODateString;
  status?: RoleBindingStatus;
}

export interface UpdateRoleBindingInput {
  status?: RoleBindingStatus;
  validFrom?: ISODateString;
  validUntil?: ISODateString;
}

export interface Department {
  id: string;
  enterpriseId: string;
  name: string;
  description?: string;
  default: boolean;
  createdAt: ISODateString;
}

export interface Role {
  id: string;
  enterpriseId: string;
  name: string;
  description?: string;
  builtin: boolean;
  permissions: string[];
  createdAt: ISODateString;
}

export interface CreateRoleInput {
  name: string;
  description?: string;
  permissions: string[];
}

export interface DataScope {
  id: string;
  enterpriseId: string;
  name: string;
  subjectType: "role" | "department" | "user";
  subjectId: string;
  environments: Environment[];
  tagExpression?: string;
  resourceGroupIds?: string[];
  resourceIds?: string[];
  onlyOwned: boolean;
  createdAt: ISODateString;
}

export interface ApprovalPolicy {
  id: string;
  enterpriseId: string;
  name: string;
  description?: string;
  matchRiskLevels: RiskLevel[];
  matchEnvironments: Environment[];
  minApprovers: number;
  approverRoleIds: string[];
  separationOfDuty: boolean;
  enabled: boolean;
  createdAt: ISODateString;
}

export interface ServiceAccount {
  id: string;
  enterpriseId: string;
  name: string;
  description?: string;
  roleIds: string[];
  status: "active" | "disabled";
  lastUsedAt?: ISODateString;
  createdAt: ISODateString;
}

export interface ApiKey {
  id: string;
  enterpriseId: string;
  serviceAccountId: string;
  name: string;
  prefix: string;
  scopes: string[];
  status: "active" | "revoked";
  expiresAt?: ISODateString;
  lastUsedAt?: ISODateString;
  createdAt: ISODateString;
}

export interface CreatedApiKey {
  apiKey: ApiKey;
  secret: string;
}

export interface LoginInput {
  username: string;
  password: string;
}

export interface SessionInfo {
  user: User;
  membership: EnterpriseMembership | null;
}

export interface InviteUserInput {
  username: string;
  displayName: string;
  email?: string;
  /** 邀请时创建企业范围 RoleBinding。 */
  roleIds?: string[];
  departmentId: string;
}

export interface UpdateMembershipInput {
  departmentId?: string;
}
