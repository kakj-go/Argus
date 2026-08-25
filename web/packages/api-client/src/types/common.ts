/**
 * Shared primitives for the Argus API client domain model.
 * Field naming follows camelCase; timestamps are ISO 8601 strings.
 */

export type ISODateString = string;

export type Environment = "development" | "staging" | "production";

/** Tool risk levels defined in docs/02; drive confirmation and approval policy. */
export type RiskLevel = "read" | "write" | "dangerous" | "critical";

/** Where an audited action originated from. */
export type AuditOrigin =
  | "admin_ui"
  | "admin_chatbox"
  | "card_action"
  | "openapi"
  | "platform_ui"
  | "system";

export type SortDirection = "asc" | "desc";

export interface SortSpec {
  field: string;
  direction: SortDirection;
}

export interface PageParams {
  cursor?: string | null;
  limit?: number;
}

/** Server-signed cursor page, mirroring the list protocol in docs/05. */
export interface Page<T> {
  items: T[];
  nextCursor: string | null;
  hasMore: boolean;
}

export interface ListQuery {
  sort?: SortSpec[];
  page?: PageParams;
}

/** Loose reference to a related domain object (task, audit, card bindings). */
export interface ResourceRef {
  type: string;
  id: string;
  name?: string;
}

export interface ConnectionCheck {
  name: string;
  status: "passed" | "failed" | "skipped";
  detail?: string;
}

export interface ConnectionTestResult {
  success: boolean;
  latencyMs: number;
  checks: ConnectionCheck[];
}

/** Cancels an event subscription returned by subscribe-style methods. */
export type Unsubscribe = () => void;
