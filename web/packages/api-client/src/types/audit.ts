import type { AuditOrigin, ISODateString } from "./common";

export type AuditResult = "success" | "failure" | "denied";
export type AuditActorType =
  | "platform_user"
  | "enterprise_user"
  | "service_account"
  | "connector"
  | "direct_executor"
  | "remote_access_gateway"
  | "system";

export interface AuditEvent {
  id: string;
  /** Null for platform-domain events visible only to super admins. */
  enterpriseId: string | null;
  actorUserId: string;
  actorName: string;
  actorType: AuditActorType;
  actorUsername?: string;
  /** Stable action name, e.g. "host.create", "pending_action.confirm". */
  action: string;
  origin: AuditOrigin;
  resourceType?: string;
  resourceId?: string;
  resourceName?: string;
  summary: string;
  result: AuditResult;
  createdAt: ISODateString;
}

export interface AuditFilter {
  action?: string;
  actorUserId?: string;
  resourceType?: string;
  result?: AuditResult;
  query?: string;
}
