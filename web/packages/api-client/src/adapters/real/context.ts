import type { ArgusApiClient } from "../../client";
import type { HttpTransport } from "../../transport/http";
import type { SseTransport } from "../../transport/sse";
import type { Page } from "../../types";

export interface RealDomainContext {
  client: ArgusApiClient;
  http: HttpTransport;
  sse: SseTransport;
  versions: Map<string, number>;
  remember<T extends { id: string; version?: number }>(value: T): T;
  expectedVersion(id: string): number;
  idempotencyKey(): string;
}

export function page<T>(value: {
  items: T[];
  page: { next_cursor: string | null; has_more: boolean };
}): Page<T> {
  return {
    items: value.items,
    nextCursor: value.page.next_cursor,
    hasMore: value.page.has_more,
  };
}
