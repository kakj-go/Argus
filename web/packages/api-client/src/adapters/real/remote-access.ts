import type {
  AccessLease,
  AccessRequest,
  RemoteAccessGrant,
  RemoteAccessRule,
  RemoteAccessRuleSimulationRequest,
  RemoteAccessRuleSimulationResult,
  ApprovalWorkflow,
  SessionProfile,
  RemoteAccessReferences,
  RemoteAccessRecording,
  RemoteAccessSession,
  RecordingEventPage,
  SessionTicketResult,
} from "../../generated/contracts";
import type { RealDomainContext } from "./context";
import { page } from "./context";

export function installRemoteAccessDomains(ctx: RealDomainContext): void {
  const { client, http, remember, expectedVersion, idempotencyKey } = ctx;
  const list = async <T>(path: string, query?: object) => {
    const params = new URLSearchParams();
    Object.entries(query ?? {}).forEach(([key, value]: [string, unknown]) => {
      if (value !== undefined && value !== "") params.set(key, String(value));
    });
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
    getGrant: (id) => http.request<RemoteAccessGrant>(`enterprise/remote-access-grants/${id}`).then(remember),
    async createGrant(input) {
      return remember(await http.request<RemoteAccessGrant>("enterprise/remote-access-grants", mutation(input)));
    },
    async updateGrant(id, input) {
      return remember(await http.request<RemoteAccessGrant>(`enterprise/remote-access-grants/${id}`, { method: "PUT", csrf: true, headers: { "Idempotency-Key": idempotencyKey() }, body: input }));
    },
    async enableGrant(id) {
      return remember(await http.request<RemoteAccessGrant>(`enterprise/remote-access-grants/${id}/enable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async disableGrant(id) {
      return remember(await http.request<RemoteAccessGrant>(`enterprise/remote-access-grants/${id}/disable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async restoreGrant(id) {
      return remember(await http.request<RemoteAccessGrant>(`enterprise/remote-access-grants/${id}/restore?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async archiveGrant(id) {
      return remember(await http.request<RemoteAccessGrant>(`enterprise/remote-access-grants/${id}/archive?expected_version=${expectedVersion(id)}`, mutation()));
    },
    getGrantReferences: (id) => http.request<RemoteAccessReferences>(`enterprise/remote-access-grants/${id}/references`),
    async listRules(query) {
      const value = await list<RemoteAccessRule>("enterprise/remote-access-rules", query);
      value.items.forEach(remember);
      return page(value);
    },
    getRule: (id) => http.request<RemoteAccessRule>(`enterprise/remote-access-rules/${id}`).then(remember),
    async createRule(input) {
      return remember(await http.request<RemoteAccessRule>("enterprise/remote-access-rules", mutation(input)));
    },
    async updateRule(id, input) {
      return remember(await http.request<RemoteAccessRule>(`enterprise/remote-access-rules/${id}`, { method: "PUT", csrf: true, headers: { "Idempotency-Key": idempotencyKey() }, body: input }));
    },
    simulateRule: (input: RemoteAccessRuleSimulationRequest) =>
      http.request<RemoteAccessRuleSimulationResult>("enterprise/remote-access-rules/simulate", { method: "POST", csrf: true, body: input }),
    async enableRule(id) {
      return remember(await http.request<RemoteAccessRule>(`enterprise/remote-access-rules/${id}/enable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async disableRule(id) {
      return remember(await http.request<RemoteAccessRule>(`enterprise/remote-access-rules/${id}/disable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async restoreRule(id) {
      return remember(await http.request<RemoteAccessRule>(`enterprise/remote-access-rules/${id}/restore?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async archiveRule(id) {
      return remember(await http.request<RemoteAccessRule>(`enterprise/remote-access-rules/${id}/archive?expected_version=${expectedVersion(id)}`, mutation()));
    },
    getRuleReferences: (id) => http.request<RemoteAccessReferences>(`enterprise/remote-access-rules/${id}/references`),
    async listApprovalWorkflows(query) {
      const value = await list<ApprovalWorkflow>("enterprise/approval-workflows", query);
      value.items.forEach(remember);
      return page(value);
    },
    getApprovalWorkflow: (id) => http.request<ApprovalWorkflow>(`enterprise/approval-workflows/${id}`).then(remember),
    async createApprovalWorkflow(input) {
      return remember(await http.request<ApprovalWorkflow>("enterprise/approval-workflows", mutation(input)));
    },
    async updateApprovalWorkflow(id, input) {
      return remember(await http.request<ApprovalWorkflow>(`enterprise/approval-workflows/${id}`, { method: "PUT", csrf: true, headers: { "Idempotency-Key": idempotencyKey() }, body: input }));
    },
    async enableApprovalWorkflow(id) {
      return remember(await http.request<ApprovalWorkflow>(`enterprise/approval-workflows/${id}/enable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async disableApprovalWorkflow(id) {
      return remember(await http.request<ApprovalWorkflow>(`enterprise/approval-workflows/${id}/disable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async restoreApprovalWorkflow(id) {
      return remember(await http.request<ApprovalWorkflow>(`enterprise/approval-workflows/${id}/restore?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async archiveApprovalWorkflow(id) {
      return remember(await http.request<ApprovalWorkflow>(`enterprise/approval-workflows/${id}/archive?expected_version=${expectedVersion(id)}`, mutation()));
    },
    getApprovalWorkflowReferences: (id) => http.request<RemoteAccessReferences>(`enterprise/approval-workflows/${id}/references`),
    async listSessionProfiles(query) {
      const value = await list<SessionProfile>("enterprise/session-profiles", query);
      value.items.forEach(remember);
      return page(value);
    },
    getSessionProfile: (id) => http.request<SessionProfile>(`enterprise/session-profiles/${id}`).then(remember),
    async createSessionProfile(input) {
      return remember(await http.request<SessionProfile>("enterprise/session-profiles", mutation(input)));
    },
    async updateSessionProfile(id, input) {
      return remember(await http.request<SessionProfile>(`enterprise/session-profiles/${id}`, { method: "PUT", csrf: true, headers: { "Idempotency-Key": idempotencyKey() }, body: input }));
    },
    async enableSessionProfile(id) {
      return remember(await http.request<SessionProfile>(`enterprise/session-profiles/${id}/enable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async disableSessionProfile(id) {
      return remember(await http.request<SessionProfile>(`enterprise/session-profiles/${id}/disable?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async restoreSessionProfile(id) {
      return remember(await http.request<SessionProfile>(`enterprise/session-profiles/${id}/restore?expected_version=${expectedVersion(id)}`, mutation()));
    },
    async archiveSessionProfile(id) {
      return remember(await http.request<SessionProfile>(`enterprise/session-profiles/${id}/archive?expected_version=${expectedVersion(id)}`, mutation()));
    },
    getSessionProfileReferences: (id) => http.request<RemoteAccessReferences>(`enterprise/session-profiles/${id}/references`),
    async listRequests(query) { return page(await list<AccessRequest>("enterprise/remote-access-requests", query)); },
    createRequest: (input) => http.request<AccessRequest>("enterprise/remote-access-requests", mutation(input)),
    getRequest: (id) => http.request<AccessRequest>(`enterprise/remote-access-requests/${id}`),
    decideRequest: (id, input) => http.request<AccessRequest>(`enterprise/remote-access-requests/${id}/decisions`, mutation(input)),
    resumeRequest: (id) => http.request<AccessRequest>(`enterprise/remote-access-requests/${id}/resume`, mutation()),
    async listLeases(query) { return page(await list<AccessLease>("enterprise/remote-access-leases", query)); },
    revokeLease: (id) => http.request<AccessLease>(`enterprise/remote-access-leases/${id}/revoke`, mutation()),
    async listSessions(query) { return page(await list<RemoteAccessSession>("enterprise/remote-access-sessions", query)); },
    createSession: (input) => http.request<RemoteAccessSession>("enterprise/remote-access-sessions", mutation(input)),
    getSession: (id) => http.request<RemoteAccessSession>(`enterprise/remote-access-sessions/${id}`),
    createTicket: (id) => http.request<SessionTicketResult>(`enterprise/remote-access-sessions/${id}/tickets`, mutation()),
    terminateSession: (id, reason) => http.request<RemoteAccessSession>(`enterprise/remote-access-sessions/${id}/terminate`, mutation({ reason })),
    async listRecordings(query) { return page(await list<RemoteAccessRecording>("enterprise/remote-access-recordings", query)); },
    getRecording: (id) => http.request<RemoteAccessRecording>(`enterprise/remote-access-recordings/${id}`),
    listRecordingEvents: (id, cursor) => http.request<RecordingEventPage>(`enterprise/remote-access-recordings/${id}/events${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`),
  };
}
