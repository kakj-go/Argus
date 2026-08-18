import type {
  AccessLease,
  AccessRequest,
  RemoteAccessGrant,
  RemoteAccessPolicy,
  RemoteAccessRecording,
  RemoteAccessSession,
  RecordingEventPage,
  SessionTicketResult,
} from "../../generated/contracts";
import type { RealDomainContext } from "./context";
import { page } from "./context";

export function installRemoteAccessDomains(ctx: RealDomainContext): void {
  const { client, http, remember, idempotencyKey } = ctx;
  const list = async <T>(path: string, query?: { cursor?: string; limit?: number }) => {
    const params = new URLSearchParams();
    if (query?.cursor) params.set("cursor", query.cursor);
    if (query?.limit !== undefined) params.set("limit", String(query.limit));
    return http.request<{ items: T[]; page: { next_cursor: string | null; has_more: boolean } }>(
      `${path}${params.size ? `?${params}` : ""}`,
    );
  };
  const mutation = (body?: unknown) => ({
    method: "POST" as const,
    csrf: true,
    headers: { "Idempotency-Key": idempotencyKey() },
    body,
  });

  client.remoteAccess = {
    async listGrants(query) {
      const value = await list<RemoteAccessGrant>("enterprise/remote-access-grants", query);
      value.items.forEach(remember);
      return page(value);
    },
    async createGrant(input) {
      return remember(await http.request<RemoteAccessGrant>("enterprise/remote-access-grants", mutation(input)));
    },
    async updateGrant(id, input) {
      return remember(await http.request<RemoteAccessGrant>(`enterprise/remote-access-grants/${id}`, { method: "PUT", csrf: true, headers: { "Idempotency-Key": idempotencyKey() }, body: input }));
    },
    disableGrant: (id) => http.request<void>(`enterprise/remote-access-grants/${id}`, { method: "DELETE", csrf: true, headers: { "Idempotency-Key": idempotencyKey() } }),
    async listPolicies(query) {
      const value = await list<RemoteAccessPolicy>("enterprise/remote-access-policies", query);
      value.items.forEach(remember);
      return page(value);
    },
    async createPolicy(input) {
      return remember(await http.request<RemoteAccessPolicy>("enterprise/remote-access-policies", mutation(input)));
    },
    async updatePolicy(id, input) {
      return remember(await http.request<RemoteAccessPolicy>(`enterprise/remote-access-policies/${id}`, { method: "PUT", csrf: true, headers: { "Idempotency-Key": idempotencyKey() }, body: input }));
    },
    disablePolicy: (id) => http.request<void>(`enterprise/remote-access-policies/${id}`, { method: "DELETE", csrf: true, headers: { "Idempotency-Key": idempotencyKey() } }),
    async listRequests(query) { return page(await list<AccessRequest>("enterprise/remote-access-requests", query)); },
    createRequest: (input) => http.request<AccessRequest>("enterprise/remote-access-requests", mutation(input)),
    getRequest: (id) => http.request<AccessRequest>(`enterprise/remote-access-requests/${id}`),
    decideRequest: (id, input) => http.request<AccessRequest>(`enterprise/remote-access-requests/${id}/decisions`, mutation(input)),
    async listLeases(query) { return page(await list<AccessLease>("enterprise/remote-access-leases", query)); },
    revokeLease: (id) => http.request<AccessLease>(`enterprise/remote-access-leases/${id}/revoke`, mutation()),
    async listSessions(query) { return page(await list<RemoteAccessSession>("enterprise/remote-access-sessions", query)); },
    createSession: (input) => http.request<RemoteAccessSession>("enterprise/remote-access-sessions", mutation(input)),
    getSession: (id) => http.request<RemoteAccessSession>(`enterprise/remote-access-sessions/${id}`),
    createTicket: (id) => http.request<SessionTicketResult>(`enterprise/remote-access-sessions/${id}/tickets`, mutation()),
    terminateSession: (id, reason) => http.request<RemoteAccessSession>(`enterprise/remote-access-sessions/${id}/terminate`, mutation({ reason })),
    getRecording: (id) => http.request<RemoteAccessRecording>(`enterprise/remote-access-recordings/${id}`),
    listRecordingEvents: (id, cursor) => http.request<RecordingEventPage>(`enterprise/remote-access-recordings/${id}/events${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`),
  };
}
