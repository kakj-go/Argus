import type { AuditOrigin, ISODateString } from "./common";

export type AuditResult = "success" | "failure" | "denied";

export interface AuditEvent {
  id: string;
  /** Null for platform-domain events visible only to super admins. */
  enterpriseId: string | null;
  actorUserId: string;
  actorName: string;
  /** Stable action name, e.g. "host.create", "pending_action.confirm". */
  action: string;
  origin: AuditOrigin;
  resourceType?: string;
  resourceId?: string;
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
