import type { ArgusApiClient } from "../client";
import type {
  AuditEvent as AuditEventContract,
  AuditEventPage,
  AuthenticatedSession,
  CreatedApiKeySecret,
  CreatedUserCredential,
  BastionScope,
  BastionScopePage,
  ConnectionTest,
  Connector,
  ConnectorPage,
  Credential,
  CredentialCreate,
  CredentialUpdate,
  Department as DepartmentContract,
  Enterprise as EnterpriseContract,
  EnterpriseUser as EnterpriseUserContract,
  LoginResult,
  Host,
  HostPage,
  ConnectorInstallOperation,
  KubernetesCluster,
  KubernetesClusterPage,
  KubernetesResourcePage,
  ManagedAccount,
  ManagedAccountCreate,
  ManagedAccountUpdate,
  PendingActionPublic,
  PodLogs,
  Role as RoleContract,
  RoleBinding as RoleBindingContract,
  ServiceAccount as ServiceAccountContract,
  Secret as SecretContract,
  SetupInitializeResult,
  SetupStatus as SetupStatusContract,
  CollectionClaim,
  CollectionProfile,
  CollectorDistributionVersion,
  CollectorInstance,
  CollectorPage,
  KubernetesNodeHostBinding,
  TelemetryRoute,
  TelemetryUsage,
  TelemetryOverview,
  PromQLInstantQuery,
  PromQLRangeQuery,
  KQLQuery,
  SkyWalkingTraceGraphQLQuery,
  PrometheusQueryResponse,
  KQLQueryResponse,
  SkyWalkingGraphQLResponse,
  RouteTestCreate,
  RouteTestResult,
  BreakGlassCreate,
  BreakGlassSession,
  MfaCodeRequest,
  MfaCompleteRequest,
  RecoveryCodesResult,
  StepUpSession,
  TotpEnrollment,
  TotpVerifyRequest,
  PKIStatus as PKIStatusContract,
} from "../generated/contracts";
import type {
  AuditEvent,
  DataAuthorizationPage,
  Department,
  Enterprise,
  EnterpriseAdmin,
  Page,
  PlatformAuditEvent,
  PlatformPKIStatus,
  Role,
  RoleBinding,
  ServiceAccount,
  SessionInfo,
  Secret,
  User,
  UserRoleAssignments,
} from "../types";
import {
  ClientOperationUnavailableError,
  MfaRequiredError,
  PasswordChangeRequiredError,
} from "../transport/errors";
import { HttpTransport, type HttpTransportOptions } from "../transport/http";
import { SseTransport } from "../transport/sse";
import { WebSocketTransport } from "../transport/websocket";
import { installAgentDomains } from "./real/agent";
import { installCardDomains } from "./real/card";
import type { RealDomainContext } from "./real/context";
import { installSandboxDomains } from "./real/sandbox";
import { installWorkflowDomains } from "./real/workflow";
import { installRemoteAccessDomains } from "./real/remote-access";

export type Portal = "setup" | "platform" | "enterprise";

export interface RealAdapterOptions extends HttpTransportOptions {
  portal: Portal;
}

function unavailable(operation: string): Promise<never> {
  return Promise.reject(new ClientOperationUnavailableError(operation));
}

function unavailableSync(operation: string): never {
  throw new ClientOperationUnavailableError(operation);
}

function idempotencyKey(): string {
  return crypto.randomUUID();
}

function page<T>(value: {
  items: T[];
  page: { next_cursor: string | null; has_more: boolean };
}): Page<T> {
  return {
    items: value.items,
    nextCursor: value.page.next_cursor,
    hasMore: value.page.has_more,
  };
}

function sessionInfo(value: AuthenticatedSession): SessionInfo {
  return {
    session: value.session,
    user: value.user,
    permissions: value.permissions,
    amr: value.amr,
    mfa_state: value.mfa_state,
    authenticated_at: value.authenticated_at,
    step_up_expires_at: value.step_up_expires_at,
  };
}

function enterprise(value: EnterpriseContract): Enterprise {
  return {
    id: value.id,
    name: value.name,
    code: value.code,
    status: value.status,
    timezone: value.timezone,
    remark: value.remark,
    createdAt: value.created_at,
  };
}

function enterpriseAdmin(value: EnterpriseUserContract): EnterpriseAdmin {
  return {
    id: value.id,
    enterpriseId: value.enterprise_id,
    username: value.username,
    displayName: value.display_name,
    email: value.email,
    credentialStatus:
      value.status === "disabled"
        ? "disabled"
        : value.last_login_at
          ? "active"
          : "temporary_password",
    lastLoginAt: value.last_login_at,
    createdAt: value.created_at,
    version: value.version,
  };
}

function user(value: EnterpriseUserContract): User {
  return {
    id: value.id,
    username: value.username,
    displayName: value.display_name,
    email: value.email,
    status: value.status,
    mfaEnabled: value.mfa_enabled,
    lastLoginAt: value.last_login_at,
    createdAt: value.created_at,
    version: value.version,
  };
}

function auditEvent(value: AuditEventContract): AuditEvent {
  return {
    id: value.id,
    enterpriseId: value.enterprise_id ?? null,
    actorUserId: value.actor_id,
    actorName:
      value.actor_display_name ?? value.actor_username ?? value.actor_id,
    actorType: value.actor_type,
    actorUsername: value.actor_username,
    action: value.action,
    origin:
      value.actor_type === "system"
        ? "system"
        : value.domain === "platform"
          ? "platform_ui"
          : "admin_ui",
    resourceType: value.resource_type,
    resourceId: value.resource_id,
    resourceName: value.resource_display_name,
    summary:
      typeof value.details.summary === "string"
        ? value.details.summary
        : value.action,
    result: value.result,
    createdAt: value.created_at,
  };
}

function secretMetadata(value: SecretContract): Secret {
  return value;
}

export interface RealAdapter {
  client: ArgusApiClient;
  http: HttpTransport;
  sse: SseTransport;
  websocket: WebSocketTransport;
}

export function createRealAdapter(options: RealAdapterOptions): RealAdapter {
  let csrfToken: string | undefined;
  const http = new HttpTransport({
    ...options,
    csrf_token: options.csrf_token ?? (() => csrfToken),
  });
  const sse = new SseTransport(http);
  const portal = options.portal;
  const versions = new Map<string, number>();
  const remember = <T extends { id: string; version?: number }>(
    value: T,
  ): T => {
    const version =
      value.version ??
      (value as T & { resource_version?: number }).resource_version;
    if (version !== undefined) versions.set(value.id, version);
    return value;
  };
  const expectedVersion = (id: string): number => versions.get(id) ?? 1;
  const authenticated = (value: AuthenticatedSession): SessionInfo => {
    csrfToken = value.csrf_token;
    return sessionInfo(value);
  };
  const requireAudience = (): "platform" | "enterprise" => {
    if (portal === "setup") {
      throw new ClientOperationUnavailableError("auth");
    }
    return portal;
  };

  const client = createUnavailableClient();
  const domainContext: RealDomainContext = {
    client,
    http,
    sse,
    versions,
    remember,
    expectedVersion,
    idempotencyKey,
  };
  client.auth = {
    async login(input) {
      const audience = requireAudience();
      const result = await http.request<LoginResult>(`${audience}/auth/login`, {
        method: "POST",
        body: input,
      });
      if (result.status === "password_change_required") {
        throw new PasswordChangeRequiredError(result.password_change_challenge);
      }
      if (result.status === "mfa_required") {
        throw new MfaRequiredError(result.mfa_challenge);
      }
      return authenticated(result.authenticated_session);
    },
    async completePasswordChange(input) {
      const audience = requireAudience();
      return authenticated(
        await http.request<AuthenticatedSession>(
          `${audience}/auth/complete-password-change`,
          { method: "POST", body: input },
        ),
      );
    },
    async completeMfaLogin(input: MfaCompleteRequest) {
      return authenticated(
        await http.request<AuthenticatedSession>(
          `${requireAudience()}/auth/mfa/complete`,
          { method: "POST", body: input },
        ),
      );
    },
    async enrollTotp(): Promise<TotpEnrollment> {
      return http.request<TotpEnrollment>(
        `${requireAudience()}/account/mfa/totp/enroll`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
        },
      );
    },
    async verifyTotpEnrollment(
      input: TotpVerifyRequest,
    ): Promise<RecoveryCodesResult> {
      return http.request<RecoveryCodesResult>(
        `${requireAudience()}/account/mfa/totp/verify`,
        { method: "POST", csrf: true, body: input },
      );
    },
    async regenerateRecoveryCodes(
      input: MfaCodeRequest,
    ): Promise<RecoveryCodesResult> {
      return http.request<RecoveryCodesResult>(
        `${requireAudience()}/account/mfa/recovery-codes/regenerate`,
        { method: "POST", csrf: true, body: input },
      );
    },
    async disableTotp(input: MfaCodeRequest): Promise<void> {
      await http.request<void>(
        `${requireAudience()}/account/mfa/totp/disable`,
        { method: "POST", csrf: true, body: input },
      );
      csrfToken = undefined;
    },
    async stepUp(input: MfaCodeRequest): Promise<StepUpSession> {
      return http.request<StepUpSession>(`${requireAudience()}/auth/step-up`, {
        method: "POST",
        csrf: true,
        body: input,
      });
    },
    async listBreakGlassSessions(): Promise<BreakGlassSession[]> {
      if (portal !== "enterprise")
        throw new ClientOperationUnavailableError("break-glass");
      return http.request<BreakGlassSession[]>(
        "enterprise/break-glass-sessions",
      );
    },
    async createBreakGlassSession(
      input: BreakGlassCreate,
    ): Promise<BreakGlassSession> {
      if (portal !== "enterprise")
        throw new ClientOperationUnavailableError("break-glass");
      return http.request<BreakGlassSession>(
        "enterprise/break-glass-sessions",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      );
    },
    async revokeBreakGlassSession(id: string): Promise<void> {
      if (portal !== "enterprise")
        throw new ClientOperationUnavailableError("break-glass");
      await http.request<void>(
        `enterprise/break-glass-sessions/${encodeURIComponent(id)}/revoke`,
        { method: "POST", csrf: true },
      );
    },
    async changePassword(input) {
      await http.request<void>(`${requireAudience()}/account/password`, {
        method: "PUT",
        csrf: true,
        body: input,
      });
      csrfToken = undefined;
    },
    async logout() {
      await http.request<void>(`${requireAudience()}/auth/session`, {
        method: "DELETE",
        csrf: true,
      });
      csrfToken = undefined;
    },
    async me() {
      return authenticated(
        await http.request<AuthenticatedSession>(
          `${requireAudience()}/auth/session`,
        ),
      );
    },
  };

  client.setup = {
    async status() {
      const value = await http.request<SetupStatusContract>("setup/status");
      return { state: value.state, platformName: value.platform_name };
    },
    async submit(input) {
      const result = await http.request<SetupInitializeResult>(
        "setup/initialize",
        {
          method: "POST",
          headers: {
            "X-Argus-Setup-Token": input.setupToken,
            "Idempotency-Key": idempotencyKey(),
          },
          body: {
            platform_name: input.platformName,
            default_locale: input.defaultLocale,
            timezone: input.timezone,
            external_url: input.externalUrl,
            super_admin: {
              username: input.superAdmin.username,
              display_name: input.superAdmin.displayName,
              email: input.superAdmin.email,
              password: input.superAdmin.password,
            },
          },
        },
      );
      return {
        success: result.state === "initialized",
        superAdminUserId: result.platform_user_id,
      };
    },
  };

  client.platform.enterprises = {
    async list() {
      const value = await http.request<{
        items: EnterpriseContract[];
        page: { next_cursor: string | null; has_more: boolean };
      }>("platform/enterprises");
      return page({
        items: value.items.map((item) => enterprise(remember(item))),
        page: value.page,
      });
    },
    async get(id) {
      return enterprise(
        remember(
          await http.request<EnterpriseContract>(`platform/enterprises/${id}`),
        ),
      );
    },
    async create(input) {
      return enterprise(
        remember(
          await http.request<EnterpriseContract>("platform/enterprises", {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
            body: {
              name: input.name,
              code: input.code,
              timezone: input.timezone ?? "Asia/Shanghai",
              remark: input.remark,
            },
          }),
        ),
      );
    },
    async update(id, patch) {
      return enterprise(
        remember(
          await http.request<EnterpriseContract>(`platform/enterprises/${id}`, {
            method: "PUT",
            csrf: true,
            body: {
              ...patch,
              expected_version: expectedVersion(id),
            },
          }),
        ),
      );
    },
    suspend: (id) => changeEnterpriseState("suspend", id),
    activate: (id) => changeEnterpriseState("activate", id),
    disable: (id) => changeEnterpriseState("disable", id),
  };

  async function changeEnterpriseState(
    action: "suspend" | "activate" | "disable",
    id: string,
  ): Promise<Enterprise> {
    return enterprise(
      remember(
        await http.request<EnterpriseContract>(
          `platform/enterprises/${id}/${action}?expected_version=${expectedVersion(id)}`,
          { method: "POST", csrf: true },
        ),
      ),
    );
  }

  client.platform.admins = {
    async list(enterpriseId) {
      const suffix = enterpriseId
        ? `?enterprise_id=${encodeURIComponent(enterpriseId)}`
        : "";
      const value = await http.request<{ items: EnterpriseUserContract[] }>(
        `platform/enterprise-admins${suffix}`,
      );
      return value.items.map((item) => enterpriseAdmin(remember(item)));
    },
    async create(input) {
      const value = await http.request<CreatedUserCredential>(
        "platform/enterprise-admins",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            enterprise_id: input.enterpriseId,
            username: input.username,
            display_name: input.displayName,
            email: input.email,
          },
        },
      );
      return withAdminCredential(value);
    },
    async resetAuth(id) {
      return withAdminCredential(
        await http.request<CreatedUserCredential>(
          `platform/enterprise-admins/${id}/reset-password`,
          {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
          },
        ),
      );
    },
    async disable(id) {
      return enterpriseAdmin(
        remember(
          await http.request<EnterpriseUserContract>(
            `platform/enterprise-admins/${id}/disable?expected_version=${expectedVersion(id)}`,
            { method: "POST", csrf: true },
          ),
        ),
      );
    },
  };

  installSandboxDomains(domainContext);

  function withAdminCredential(value: CreatedUserCredential): EnterpriseAdmin {
    return {
      ...enterpriseAdmin(remember(value.user)),
      temporaryPassword: value.temporary_password,
      temporaryPasswordExpiresAt: value.expires_at,
    };
  }

  client.org = createOrganizationClient(http, versions, remember);
  installWorkflowDomains(domainContext);
  installAgentDomains(domainContext);
  installCardDomains(domainContext);
  installRemoteAccessDomains(domainContext);

  client.audit = {
    list: (filter, query) =>
      listAudit("enterprise", filter?.action, query?.page?.limit),
  };
  client.platform.audit = {
    list: (filter, query): Promise<Page<PlatformAuditEvent>> =>
      listAudit("platform", filter?.action, query?.page?.limit),
  };
  client.platform.pki = {
    async get(): Promise<PlatformPKIStatus> {
      const value = await http.request<PKIStatusContract>("platform/pki");
      return {
        bundles: value.bundles.map((bundle) => ({
          epoch: bundle.epoch,
          state: bundle.state,
          direction: bundle.direction,
          bundleSha256: bundle.bundle_sha256,
          currentCaFingerprints: bundle.current_ca_fingerprints,
          nextCaFingerprints: bundle.next_ca_fingerprints,
          startedAt: bundle.started_at,
          retireAt: bundle.retire_at,
          lastError: bundle.last_error,
        })),
        nodes: value.nodes.map((node) => ({
          id: node.id,
          kind: node.kind,
          enterpriseId: node.enterprise_id,
          epoch: node.epoch,
          bundleSha256: node.bundle_sha256,
          caFingerprints: node.ca_fingerprints,
          status: node.status,
          blocksCutover: node.blocks_cutover,
          error: node.error,
          acknowledgedAt: node.acknowledged_at,
          updatedAt: node.updated_at,
        })),
        acknowledgedNodes: value.acknowledged_nodes,
        pendingNodes: value.pending_nodes,
        failedNodes: value.failed_nodes,
        trustExpiredNodes: value.trust_expired_nodes,
      };
    },
  };

  client.secrets = {
    async list() {
      const value = await http.request<{
        items: SecretContract[];
        page: { next_cursor: string | null; has_more: boolean };
      }>("enterprise/secrets");
      return page({
        items: value.items.map((item) => secretMetadata(remember(item))),
        page: value.page,
      });
    },
    async get(id) {
      return secretMetadata(
        remember(
          await http.request<SecretContract>(`enterprise/secrets/${id}`),
        ),
      );
    },
    async create(input) {
      return secretMetadata(
        remember(
          await http.request<SecretContract>("enterprise/secrets", {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
            body: input,
          }),
        ),
      );
    },
    async update(id, patch) {
      return secretMetadata(
        remember(
          await http.request<SecretContract>(`enterprise/secrets/${id}`, {
            method: "PUT",
            csrf: true,
            body: patch,
          }),
        ),
      );
    },
    async rotate(id, value, expectedVersion) {
      return secretMetadata(
        remember(
          await http.request<SecretContract>(
            `enterprise/secrets/${id}/rotate`,
            {
              method: "POST",
              csrf: true,
              headers: { "Idempotency-Key": idempotencyKey() },
              body: { value, expected_version: expectedVersion },
            },
          ),
        ),
      );
    },
    async delete(id) {
      await http.request<void>(
        `enterprise/secrets/${id}?expected_version=${expectedVersion(id)}`,
        { method: "DELETE", csrf: true },
      );
    },
    async listCredentials() {
      const value = await http.request<{ items: Credential[] }>(
        "enterprise/credentials",
      );
      return value.items.map(remember);
    },
    async createCredential(input: CredentialCreate) {
      return remember(
        await http.request<Credential>("enterprise/credentials", {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        }),
      );
    },
    async updateCredential(id: string, input: CredentialUpdate) {
      return remember(
        await http.request<Credential>(`enterprise/credentials/${id}`, {
          method: "PUT",
          csrf: true,
          body: input,
        }),
      );
    },
    async listManagedAccounts() {
      const value = await http.request<{ items: ManagedAccount[] }>(
        "enterprise/managed-accounts",
      );
      return value.items.map(remember);
    },
    async createManagedAccount(input: ManagedAccountCreate) {
      return remember(
        await http.request<ManagedAccount>("enterprise/managed-accounts", {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        }),
      );
    },
    async updateManagedAccount(id: string, input: ManagedAccountUpdate) {
      return remember(
        await http.request<ManagedAccount>(
          `enterprise/managed-accounts/${id}`,
          { method: "PUT", csrf: true, body: input },
        ),
      );
    },
  };

  client.hosts = {
    ...client.hosts,
    async list(filter) {
      const params = new URLSearchParams();
      if (filter?.query) params.set("query", filter.query);
      if (filter?.connection_mode) {
        params.set("connection_mode", filter.connection_mode);
      }
      if (filter?.bastion_scope_id) {
        params.set("bastion_scope_id", filter.bastion_scope_id);
      }
      if (filter?.labels) params.set("labels", JSON.stringify(filter.labels));
      if (filter?.cursor) params.set("cursor", filter.cursor);
      if (filter?.limit !== undefined)
        params.set("limit", String(filter.limit));
      const value = await http.request<HostPage>(
        `enterprise/hosts${params.size ? `?${params}` : ""}`,
      );
      value.items.forEach(remember);
      return value;
    },
    async get(id) {
      return remember(await http.request<Host>(`enterprise/hosts/${id}`));
    },
    async createConnectionTest(input) {
      return http.request<ConnectionTest>("enterprise/hosts/connection-tests", {
        method: "POST",
        csrf: true,
        headers: { "Idempotency-Key": idempotencyKey() },
        body: input,
      });
    },
    getConnectionTest: (id) =>
      http.request<ConnectionTest>(`enterprise/connection-tests/${id}`),
    previewCreateResource: (input) =>
      http.request<PendingActionPublic>(
        "enterprise/hosts/actions/preview-create",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewUpdateResource: (id, input) =>
      http.request<PendingActionPublic>(
        `enterprise/hosts/${id}/actions/preview-update`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewDeleteResource: (id, version) =>
      http.request<PendingActionPublic>(
        `enterprise/hosts/${id}/actions/preview-delete`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { expected_version: version },
        },
      ),
    getCollector: async (id) =>
      (await http.request<CollectorInstance | undefined>(
        `enterprise/hosts/${id}/collector`,
      )) ?? null,
    previewCollectorAction: (id, action, input) =>
      http.request<PendingActionPublic>(
        `enterprise/hosts/${id}/collector/actions/preview-${action}`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewCollectorInstall: (id, input) =>
      client.hosts.previewCollectorAction(id, "install", input),
    previewEnrollmentRotate: (id, version) =>
      http.request<PendingActionPublic>(
        `enterprise/hosts/${id}/actions/preview-enrollment-rotate`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { expected_version: version },
        },
      ),
    previewUninstallCommand: (id, version) =>
      http.request<PendingActionPublic>(
        `enterprise/hosts/${id}/actions/preview-uninstall-command`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { expected_version: version },
        },
      ),
  };

  client.kubernetes = {
    ...client.kubernetes,
    async listClusters(query) {
      const params = new URLSearchParams();
      if (query?.cursor) params.set("cursor", query.cursor);
      if (query?.limit !== undefined) params.set("limit", String(query.limit));
      const value = await http.request<KubernetesClusterPage>(
        `enterprise/kubernetes-clusters${params.size ? `?${params}` : ""}`,
      );
      value.items.forEach(remember);
      return value;
    },
    async getCluster(id) {
      return remember(
        await http.request<KubernetesCluster>(
          `enterprise/kubernetes-clusters/${id}`,
        ),
      );
    },
    createConnectionTest: (input) =>
      http.request<ConnectionTest>(
        "enterprise/kubernetes-clusters/connection-tests",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    getConnectionTest: (id) =>
      http.request<ConnectionTest>(`enterprise/connection-tests/${id}`),
    previewCreateResource: (input) =>
      http.request<PendingActionPublic>(
        "enterprise/kubernetes-clusters/actions/preview-create",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewUpdateResource: (id, input) =>
      http.request<PendingActionPublic>(
        `enterprise/kubernetes-clusters/${id}/actions/preview-update`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewDeleteResource: (id, version) =>
      http.request<PendingActionPublic>(
        `enterprise/kubernetes-clusters/${id}/actions/preview-delete`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { expected_version: version },
        },
      ),
    listResources: (id, query) => {
      const params = new URLSearchParams({
        resource_type: query.resource_type,
      });
      if (query.namespace) params.set("namespace", query.namespace);
      if (query.query) params.set("query", query.query);
      if (query.cursor) params.set("cursor", query.cursor);
      if (query.limit !== undefined) params.set("limit", String(query.limit));
      return http.request<KubernetesResourcePage>(
        `enterprise/kubernetes-clusters/${id}/resources?${params}`,
      );
    },
    getPodLogs: (id, query) => {
      const params = new URLSearchParams({
        namespace: query.namespace,
        pod: query.pod,
      });
      if (query.container) params.set("container", query.container);
      if (query.tail_lines !== undefined) {
        params.set("tail_lines", String(query.tail_lines));
      }
      return http.request<PodLogs>(
        `enterprise/kubernetes-clusters/${id}/pod-logs?${params}`,
      );
    },
    listNodeBindings: (clusterId) =>
      http.request<KubernetesNodeHostBinding[]>(
        `enterprise/telemetry/node-host-bindings?kubernetes_cluster_id=${encodeURIComponent(clusterId)}`,
      ),
    verifyNodeBinding: (bindingId, input) =>
      http.request<PendingActionPublic>(
        `enterprise/telemetry/node-host-bindings/${bindingId}/actions/preview-confirm`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    listCollectionClaims: (clusterId) =>
      http.request<CollectionClaim[]>(
        `enterprise/telemetry/collection-claims${clusterId ? `?resource_id=${encodeURIComponent(clusterId)}` : ""}`,
      ),
    getCollector: async (id) =>
      (await http.request<CollectorInstance | undefined>(
        `enterprise/kubernetes-clusters/${id}/collector`,
      )) ?? null,
    previewCollectorAction: (id, action, input) =>
      http.request<PendingActionPublic>(
        `enterprise/kubernetes-clusters/${id}/collector/actions/preview-${action}`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewCollectorInstall: (id, input) =>
      client.kubernetes.previewCollectorAction(id, "install", input),
  };

  client.telemetry = {
    listDistributions: () =>
      http.request<CollectorDistributionVersion[]>(
        "enterprise/telemetry/distributions",
      ),
    listProfiles: () =>
      http.request<CollectionProfile[]>("enterprise/telemetry/profiles"),
    listCollectors: async () =>
      (await http.request<CollectorPage>("enterprise/telemetry/collectors"))
        .items,
    listRoutes: () =>
      http.request<TelemetryRoute[]>("enterprise/telemetry/routes"),
    listClaims: (resourceId) =>
      http.request<CollectionClaim[]>(
        `enterprise/telemetry/collection-claims${resourceId ? `?resource_id=${encodeURIComponent(resourceId)}` : ""}`,
      ),
    testRoute: (input: RouteTestCreate) =>
      http.request<RouteTestResult>("enterprise/telemetry/routes/tests", {
        method: "POST",
        csrf: true,
        headers: { "Idempotency-Key": idempotencyKey() },
        body: input,
      }),
    usage: () => http.request<TelemetryUsage>("enterprise/telemetry/usage"),
    overview: (input) =>
      http.request<TelemetryOverview>("enterprise/telemetry/query/overview", {
        method: "POST",
        body: input,
      }),
    queryMetrics: (input: PromQLInstantQuery) =>
      http.request<PrometheusQueryResponse>("enterprise/metrics/query", {
        method: "POST",
        body: input,
      }),
    queryMetricsRange: (input: PromQLRangeQuery) =>
      http.request<PrometheusQueryResponse>("enterprise/metrics/query_range", {
        method: "POST",
        body: input,
      }),
    queryLogs: (input: KQLQuery) =>
      http.request<KQLQueryResponse>("enterprise/logs/query", {
        method: "POST",
        body: input,
      }),
    queryTraces: (input: SkyWalkingTraceGraphQLQuery) =>
      http.request<SkyWalkingGraphQLResponse>("enterprise/traces/graphql", {
        method: "POST",
        body: input,
      }),
  };

  client.connectors = {
    ...client.connectors,
    async list(query) {
      const params = new URLSearchParams();
      if (query?.cursor) params.set("cursor", query.cursor);
      if (query?.limit !== undefined) params.set("limit", String(query.limit));
      const value = await http.request<ConnectorPage>(
        `enterprise/connectors${params.size ? `?${params}` : ""}`,
      );
      value.items.forEach(remember);
      return value;
    },
    async get(id) {
      return remember(
        await http.request<Connector>(`enterprise/connectors/${id}`),
      );
    },
    async listBastionScopes(query) {
      const params = new URLSearchParams();
      if (query?.cursor) params.set("cursor", query.cursor);
      if (query?.limit !== undefined) params.set("limit", String(query.limit));
      const value = await http.request<BastionScopePage>(
        `enterprise/bastion-scopes${params.size ? `?${params}` : ""}`,
      );
      value.items.forEach(remember);
      return value;
    },
    async getBastionScope(id) {
      return remember(
        await http.request<BastionScope>(`enterprise/bastion-scopes/${id}`),
      );
    },
    previewCreateBastionScope: (input) =>
      http.request<PendingActionPublic>(
        "enterprise/bastion-scopes/actions/preview-create",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewUpdateBastionScope: (id, input) =>
      http.request<PendingActionPublic>(
        `enterprise/bastion-scopes/${id}/actions/preview-update`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    previewDeleteBastionScope: (id, version) =>
      http.request<PendingActionPublic>(
        `enterprise/bastion-scopes/${id}/actions/preview-delete`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { expected_version: version },
        },
      ),
    previewEnrollmentRotate: (id, version) =>
      http.request<PendingActionPublic>(
        `enterprise/bastion-scopes/${id}/actions/preview-enrollment-rotate`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { expected_version: version },
        },
      ),
    previewConnectorReplacement: (id, input) =>
      http.request<PendingActionPublic>(
        `enterprise/bastion-scopes/${id}/actions/preview-connector-replacement`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      ),
    getInstallOperation: (id) =>
      http.request<ConnectorInstallOperation>(
        `enterprise/connector-install-operations/${id}`,
      ),
    previewRetryInstallOperation: (id) =>
      http.request<PendingActionPublic>(
        `enterprise/connector-install-operations/${id}/actions/preview-retry`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
        },
      ),
    previewUninstallConnector: (id, version) =>
      http.request<PendingActionPublic>(
        `enterprise/connectors/${id}/actions/preview-uninstall`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: { expected_version: version },
        },
      ),
    async rotateCertificate(id) {
      return remember(
        await http.request<Connector>(
          `enterprise/connectors/${id}/rotate-certificate?expected_version=${expectedVersion(id)}`,
          {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
          },
        ),
      );
    },
  };

  async function listAudit(
    audience: "platform" | "enterprise",
    action?: string,
    limit?: number,
  ): Promise<Page<AuditEvent>> {
    const params = new URLSearchParams();
    if (action) params.set("action", action);
    if (limit) params.set("limit", String(limit));
    const value = await http.request<AuditEventPage>(
      `${audience}/audit-events?${params}`,
    );
    return page({ items: value.items.map(auditEvent), page: value.page });
  }

  return {
    client,
    http,
    sse,
    websocket: new WebSocketTransport(),
  };
}

function createOrganizationClient(
  http: HttpTransport,
  versions: Map<string, number>,
  remember: <T extends { id: string; version?: number }>(value: T) => T,
): ArgusApiClient["org"] {
  const expectedVersion = (id: string) => versions.get(id) ?? 1;
  return {
    async listUsers() {
      const value = await http.request<{ items: EnterpriseUserContract[] }>(
        "enterprise/users",
      );
      return value.items.map((item) => user(remember(item)));
    },
    async getEnterpriseUser(id) {
      return remember(
        await http.request<EnterpriseUserContract>(`enterprise/users/${id}`),
      );
    },
    async inviteUser(input) {
      const credential = await http.request<CreatedUserCredential>(
        "enterprise/users",
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      );
      return {
        ...user(remember(credential.user)),
        temporaryPassword: credential.temporary_password,
        temporaryPasswordExpiresAt: credential.expires_at,
      };
    },
    async updateUser(id, patch) {
      const updated = await http.request<EnterpriseUserContract>(
        `enterprise/users/${id}`,
        {
          method: "PUT",
          csrf: true,
          body: {
            display_name: patch.displayName,
            status: patch.status,
            expected_version: expectedVersion(id),
          },
        },
      );
      return user(remember(updated));
    },
    async updateEnterpriseUser(id, patch) {
      return remember(
        await http.request<EnterpriseUserContract>(`enterprise/users/${id}`, {
          method: "PUT",
          csrf: true,
          body: { ...patch, expected_version: expectedVersion(id) },
        }),
      );
    },
    async listDepartments() {
      const value = await http.request<{ items: DepartmentContract[] }>(
        "enterprise/departments",
      );
      return value.items.map(remember) as Department[];
    },
    async createDepartment(input) {
      return remember(
        await http.request<DepartmentContract>("enterprise/departments", {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        }),
      ) as Department;
    },
    async updateDepartment(id, patch) {
      return remember(
        await http.request<DepartmentContract>(`enterprise/departments/${id}`, {
          method: "PUT",
          csrf: true,
          body: { ...patch, expected_version: expectedVersion(id) },
        }),
      ) as Department;
    },
    async deleteDepartment(id) {
      await http.request<void>(
        `enterprise/departments/${id}?expected_version=${expectedVersion(id)}`,
        { method: "DELETE", csrf: true },
      );
    },
    async listRoles() {
      const value = await http.request<{ items: RoleContract[] }>(
        "enterprise/roles",
      );
      return value.items.map(remember) as Role[];
    },
    async createRole(input) {
      return remember(
        await http.request<RoleContract>("enterprise/roles", {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        }),
      ) as Role;
    },
    async updateRole(id, patch) {
      return remember(
        await http.request<RoleContract>(`enterprise/roles/${id}`, {
          method: "PUT",
          csrf: true,
          body: { ...patch, expected_version: expectedVersion(id) },
        }),
      ) as Role;
    },
    async deleteRole(id) {
      await http.request<void>(
        `enterprise/roles/${id}?expected_version=${expectedVersion(id)}`,
        { method: "DELETE", csrf: true },
      );
    },
    async listRoleBindings() {
      const value = await http.request<{ items: RoleBindingContract[] }>(
        "enterprise/role-bindings",
      );
      return value.items.map(remember) as RoleBinding[];
    },
    async createRoleBinding(input) {
      return remember(
        await http.request<RoleBindingContract>("enterprise/role-bindings", {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        }),
      ) as RoleBinding;
    },
    async updateRoleBinding(id, patch) {
      return remember(
        await http.request<RoleBindingContract>(
          `enterprise/role-bindings/${id}`,
          {
            method: "PUT",
            csrf: true,
            body: {
              ...patch,
              valid_from: patch.valid_from ?? null,
              valid_until: patch.valid_until ?? null,
              expected_version: expectedVersion(id),
            },
          },
        ),
      ) as RoleBinding;
    },
    async deleteRoleBinding(id) {
      await http.request<void>(
        `enterprise/role-bindings/${id}?expected_version=${expectedVersion(id)}`,
        { method: "DELETE", csrf: true },
      );
    },
    getUserRoleAssignments(userId) {
      return http.request<UserRoleAssignments>(
        `enterprise/users/${userId}/role-assignments`,
      );
    },
    replaceUserRoleAssignments(
      userId,
      departmentId,
      roleIds,
      expectedUserVersion,
      expectedAuthorizationVersion,
    ) {
      return http.request<UserRoleAssignments>(
        `enterprise/users/${userId}/role-assignments`,
        {
          method: "PUT",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            department_id: departmentId,
            role_ids: roleIds,
            expected_user_version: expectedUserVersion,
            expected_authorization_version: expectedAuthorizationVersion,
          },
        },
      );
    },
    async listDataAuthorization(
      subjectType,
      subjectId,
      resourceType,
      cursor,
      limit,
    ) {
      const params = new URLSearchParams({ resource_type: resourceType });
      if (cursor) params.set("cursor", cursor);
      if (limit !== undefined) params.set("limit", String(limit));
      return http.request<DataAuthorizationPage>(
        `enterprise/data-authorizations/${subjectType}/${subjectId}?${params.toString()}`,
      );
    },
    async updateDataAuthorization(
      subjectType,
      subjectId,
      resourceType,
      resourceIds,
      remove,
      expectedVersion,
    ) {
      await http.request<void>(
        `enterprise/data-authorizations/${subjectType}/${subjectId}?resource_type=${resourceType}`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: {
            resource_type: resourceType,
            resource_ids: resourceIds,
            remove,
            expected_version: expectedVersion,
          },
        },
      );
    },
    listApprovalPolicies: () => unavailable("org.listApprovalPolicies"),
    saveApprovalPolicy: () => unavailable("org.saveApprovalPolicy"),
    async listServiceAccounts() {
      const value = await http.request<{ items: ServiceAccountContract[] }>(
        "enterprise/service-accounts",
      );
      return value.items.map(remember) as ServiceAccount[];
    },
    async createServiceAccount(input) {
      return remember(
        await http.request<ServiceAccountContract>(
          "enterprise/service-accounts",
          {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
            body: {
              ...input,
              allowed_tool_ids: input.allowed_tool_ids ?? [],
            },
          },
        ),
      ) as ServiceAccount;
    },
    async updateServiceAccount(id, patch) {
      return remember(
        await http.request<ServiceAccountContract>(
          `enterprise/service-accounts/${id}`,
          {
            method: "PUT",
            csrf: true,
            body: { ...patch, expected_version: expectedVersion(id) },
          },
        ),
      ) as ServiceAccount;
    },
    async listApiKeys(serviceAccountId) {
      const value = await http.request<{
        items: CreatedApiKeySecret["api_key"][];
      }>(`enterprise/service-accounts/${serviceAccountId}/api-keys`);
      return value.items.map(remember);
    },
    async createApiKey(serviceAccountId, input) {
      return http.request<CreatedApiKeySecret>(
        `enterprise/service-accounts/${serviceAccountId}/api-keys`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body: input,
        },
      );
    },
    async rotateApiKey(id) {
      return http.request<CreatedApiKeySecret>(
        `enterprise/api-keys/${id}/rotate?expected_version=${expectedVersion(id)}`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
        },
      );
    },
    async revokeApiKey(id) {
      await http.request<void>(
        `enterprise/api-keys/${id}/revoke?expected_version=${expectedVersion(id)}`,
        { method: "POST", csrf: true },
      );
    },
  };
}

function createUnavailableClient(): ArgusApiClient {
  return {
    auth: {
      login: () => unavailable("auth.login"),
      completePasswordChange: () => unavailable("auth.completePasswordChange"),
      completeMfaLogin: () => unavailable("auth.completeMfaLogin"),
      enrollTotp: () => unavailable("auth.enrollTotp"),
      verifyTotpEnrollment: () => unavailable("auth.verifyTotpEnrollment"),
      regenerateRecoveryCodes: () =>
        unavailable("auth.regenerateRecoveryCodes"),
      disableTotp: () => unavailable("auth.disableTotp"),
      stepUp: () => unavailable("auth.stepUp"),
      listBreakGlassSessions: () => unavailable("auth.listBreakGlassSessions"),
      createBreakGlassSession: () =>
        unavailable("auth.createBreakGlassSession"),
      revokeBreakGlassSession: () =>
        unavailable("auth.revokeBreakGlassSession"),
      changePassword: () => unavailable("auth.changePassword"),
      logout: () => unavailable("auth.logout"),
      me: () => unavailable("auth.me"),
    },
    conversations: {
      list: () => unavailable("conversations.list"),
      get: () => unavailable("conversations.get"),
      create: () => unavailable("conversations.create"),
      archive: () => unavailable("conversations.archive"),
      listEvents: () => unavailable("conversations.listEvents"),
      sendMessage: () => unavailableSync("conversations.sendMessage"),
      updateModel: () => unavailable("conversations.updateModel"),
      subscribe: () => unavailableSync("conversations.subscribe"),
    },
    runs: {
      get: () => unavailable("runs.get"),
      cancel: () => unavailable("runs.cancel"),
      compact: () => unavailable("runs.compact"),
    },
    executions: {
      list: () => unavailable("executions.list"),
      get: () => unavailable("executions.get"),
      claimOneTimeResult: () => unavailable("executions.claimOneTimeResult"),
    },
    hosts: {
      list: () => unavailable("hosts.list"),
      get: () => unavailable("hosts.get"),
      createConnectionTest: () => unavailable("hosts.createConnectionTest"),
      getConnectionTest: () => unavailable("hosts.getConnectionTest"),
      previewCreateResource: () => unavailable("hosts.previewCreateResource"),
      previewUpdateResource: () => unavailable("hosts.previewUpdateResource"),
      previewDeleteResource: () => unavailable("hosts.previewDeleteResource"),
      getCollector: () => unavailable("hosts.getCollector"),
      previewCollectorAction: () => unavailable("hosts.previewCollectorAction"),
      previewCollectorInstall: () =>
        unavailable("hosts.previewCollectorInstall"),
      previewEnrollmentRotate: () =>
        unavailable("hosts.previewEnrollmentRotate"),
      previewUninstallCommand: () =>
        unavailable("hosts.previewUninstallCommand"),
    },
    remoteAccess: {
      listGrants: () => unavailable("remoteAccess.listGrants"),
      getGrant: () => unavailable("remoteAccess.getGrant"),
      createGrant: () => unavailable("remoteAccess.createGrant"),
      updateGrant: () => unavailable("remoteAccess.updateGrant"),
      enableGrant: () => unavailable("remoteAccess.enableGrant"),
      disableGrant: () => unavailable("remoteAccess.disableGrant"),
      restoreGrant: () => unavailable("remoteAccess.restoreGrant"),
      archiveGrant: () => unavailable("remoteAccess.archiveGrant"),
      getGrantReferences: () => unavailable("remoteAccess.getGrantReferences"),
      listRules: () => unavailable("remoteAccess.listRules"),
      getRule: () => unavailable("remoteAccess.getRule"),
      createRule: () => unavailable("remoteAccess.createRule"),
      updateRule: () => unavailable("remoteAccess.updateRule"),
      simulateRule: () => unavailable("remoteAccess.simulateRule"),
      enableRule: () => unavailable("remoteAccess.enableRule"),
      disableRule: () => unavailable("remoteAccess.disableRule"),
      restoreRule: () => unavailable("remoteAccess.restoreRule"),
      archiveRule: () => unavailable("remoteAccess.archiveRule"),
      getRuleReferences: () => unavailable("remoteAccess.getRuleReferences"),
      listApprovalWorkflows: () =>
        unavailable("remoteAccess.listApprovalWorkflows"),
      getApprovalWorkflow: () =>
        unavailable("remoteAccess.getApprovalWorkflow"),
      createApprovalWorkflow: () =>
        unavailable("remoteAccess.createApprovalWorkflow"),
      updateApprovalWorkflow: () =>
        unavailable("remoteAccess.updateApprovalWorkflow"),
      enableApprovalWorkflow: () =>
        unavailable("remoteAccess.enableApprovalWorkflow"),
      disableApprovalWorkflow: () =>
        unavailable("remoteAccess.disableApprovalWorkflow"),
      restoreApprovalWorkflow: () =>
        unavailable("remoteAccess.restoreApprovalWorkflow"),
      archiveApprovalWorkflow: () =>
        unavailable("remoteAccess.archiveApprovalWorkflow"),
      getApprovalWorkflowReferences: () =>
        unavailable("remoteAccess.getApprovalWorkflowReferences"),
      listSessionProfiles: () =>
        unavailable("remoteAccess.listSessionProfiles"),
      getSessionProfile: () => unavailable("remoteAccess.getSessionProfile"),
      createSessionProfile: () =>
        unavailable("remoteAccess.createSessionProfile"),
      updateSessionProfile: () =>
        unavailable("remoteAccess.updateSessionProfile"),
      enableSessionProfile: () =>
        unavailable("remoteAccess.enableSessionProfile"),
      disableSessionProfile: () =>
        unavailable("remoteAccess.disableSessionProfile"),
      restoreSessionProfile: () =>
        unavailable("remoteAccess.restoreSessionProfile"),
      archiveSessionProfile: () =>
        unavailable("remoteAccess.archiveSessionProfile"),
      getSessionProfileReferences: () =>
        unavailable("remoteAccess.getSessionProfileReferences"),
      listRequests: () => unavailable("remoteAccess.listRequests"),
      createRequest: () => unavailable("remoteAccess.createRequest"),
      getRequest: () => unavailable("remoteAccess.getRequest"),
      decideRequest: () => unavailable("remoteAccess.decideRequest"),
      resumeRequest: () => unavailable("remoteAccess.resumeRequest"),
      listLeases: () => unavailable("remoteAccess.listLeases"),
      revokeLease: () => unavailable("remoteAccess.revokeLease"),
      listSessions: () => unavailable("remoteAccess.listSessions"),
      createSession: () => unavailable("remoteAccess.createSession"),
      getSession: () => unavailable("remoteAccess.getSession"),
      createTicket: () => unavailable("remoteAccess.createTicket"),
      terminateSession: () => unavailable("remoteAccess.terminateSession"),
      listRecordings: () => unavailable("remoteAccess.listRecordings"),
      getRecording: () => unavailable("remoteAccess.getRecording"),
      listRecordingEvents: () =>
        unavailable("remoteAccess.listRecordingEvents"),
    },
    connectors: {
      list: () => unavailable("connectors.list"),
      get: () => unavailable("connectors.get"),
      listBastionScopes: () => unavailable("connectors.listBastionScopes"),
      getBastionScope: () => unavailable("connectors.getBastionScope"),
      rotateCertificate: () => unavailable("connectors.rotateCertificate"),
      previewCreateBastionScope: () =>
        unavailable("connectors.previewCreateBastionScope"),
      previewUpdateBastionScope: () =>
        unavailable("connectors.previewUpdateBastionScope"),
      previewDeleteBastionScope: () =>
        unavailable("connectors.previewDeleteBastionScope"),
      previewEnrollmentRotate: () =>
        unavailable("connectors.previewEnrollmentRotate"),
      previewConnectorReplacement: () =>
        unavailable("connectors.previewConnectorReplacement"),
      getInstallOperation: () => unavailable("connectors.getInstallOperation"),
      previewRetryInstallOperation: () =>
        unavailable("connectors.previewRetryInstallOperation"),
      previewUninstallConnector: () =>
        unavailable("connectors.previewUninstallConnector"),
    },
    kubernetes: {
      listClusters: () => unavailable("kubernetes.listClusters"),
      getCluster: () => unavailable("kubernetes.getCluster"),
      createConnectionTest: () =>
        unavailable("kubernetes.createConnectionTest"),
      getConnectionTest: () => unavailable("kubernetes.getConnectionTest"),
      previewCreateResource: () =>
        unavailable("kubernetes.previewCreateResource"),
      previewUpdateResource: () =>
        unavailable("kubernetes.previewUpdateResource"),
      previewDeleteResource: () =>
        unavailable("kubernetes.previewDeleteResource"),
      listResources: () => unavailable("kubernetes.listResources"),
      getPodLogs: () => unavailable("kubernetes.getPodLogs"),
      listWorkloads: () => unavailable("kubernetes.listWorkloads"),
      listNodeBindings: () => unavailable("kubernetes.listNodeBindings"),
      verifyNodeBinding: () => unavailable("kubernetes.verifyNodeBinding"),
      listCollectionClaims: () =>
        unavailable("kubernetes.listCollectionClaims"),
      getCollector: () => unavailable("kubernetes.getCollector"),
      previewCollectorAction: () =>
        unavailable("kubernetes.previewCollectorAction"),
      previewCollectorInstall: () =>
        unavailable("kubernetes.previewCollectorInstall"),
    },
    telemetry: {
      listDistributions: () => unavailable("telemetry.listDistributions"),
      listProfiles: () => unavailable("telemetry.listProfiles"),
      listCollectors: () => unavailable("telemetry.listCollectors"),
      listRoutes: () => unavailable("telemetry.listRoutes"),
      listClaims: () => unavailable("telemetry.listClaims"),
      testRoute: () => unavailable("telemetry.testRoute"),
      usage: () => unavailable("telemetry.usage"),
      overview: () => unavailable("telemetry.overview"),
      queryMetrics: () => unavailable("telemetry.queryMetrics"),
      queryMetricsRange: () => unavailable("telemetry.queryMetricsRange"),
      queryLogs: () => unavailable("telemetry.queryLogs"),
      queryTraces: () => unavailable("telemetry.queryTraces"),
    },
    tasks: {
      list: () => unavailable("tasks.list"),
      get: () => unavailable("tasks.get"),
      cancel: () => unavailable("tasks.cancel"),
      subscribe: () => unavailableSync("tasks.subscribe"),
      subscribeTask: () => unavailableSync("tasks.subscribeTask"),
    },
    approvals: {
      list: () => unavailable("approvals.list"),
      get: () => unavailable("approvals.get"),
      preview: () => unavailable("approvals.preview"),
      confirm: () => unavailable("approvals.confirm"),
      cancel: () => unavailable("approvals.cancel"),
      approve: () => unavailable("approvals.approve"),
      reject: () => unavailable("approvals.reject"),
    },
    approvalRequests: {
      list: () => unavailable("approvalRequests.list"),
      get: () => unavailable("approvalRequests.get"),
      decide: () => unavailable("approvalRequests.decide"),
    },
    models: {
      list: () => unavailable("models.list"),
      get: () => unavailable("models.get"),
      testAndCreate: () => unavailable("models.testAndCreate"),
      update: () => unavailable("models.update"),
      delete: () => unavailable("models.delete"),
      test: () => unavailable("models.test"),
      listAvailability: () => unavailable("models.listAvailability"),
      listQuotas: () => unavailable("models.listQuotas"),
      setQuota: () => unavailable("models.setQuota"),
      usage: () => unavailable("models.usage"),
    },
    interactiveCards: {
      list: () => unavailable("interactiveCards.list"),
      get: () => unavailable("interactiveCards.get"),
      listVersions: () => unavailable("interactiveCards.listVersions"),
      getVersion: () => unavailable("interactiveCards.getVersion"),
      createConfigurationVersion: () =>
        unavailable("interactiveCards.createConfigurationVersion"),
      startValidation: () => unavailable("interactiveCards.startValidation"),
      submitValidationEvidence: () =>
        unavailable("interactiveCards.submitValidationEvidence"),
      changeState: () => unavailable("interactiveCards.changeState"),
      listToolSchemas: () => unavailable("interactiveCards.listToolSchemas"),
      createPresentation: () =>
        unavailable("interactiveCards.createPresentation"),
      invokeQueryBinding: () =>
        unavailable("interactiveCards.invokeQueryBinding"),
      invokeActionBinding: () =>
        unavailable("interactiveCards.invokeActionBinding"),
    },
    org: {
      listUsers: () => unavailable("org.listUsers"),
      getEnterpriseUser: () => unavailable("org.getEnterpriseUser"),
      inviteUser: () => unavailable("org.inviteUser"),
      updateUser: () => unavailable("org.updateUser"),
      updateEnterpriseUser: () => unavailable("org.updateEnterpriseUser"),
      listDepartments: () => unavailable("org.listDepartments"),
      createDepartment: () => unavailable("org.createDepartment"),
      updateDepartment: () => unavailable("org.updateDepartment"),
      deleteDepartment: () => unavailable("org.deleteDepartment"),
      listRoles: () => unavailable("org.listRoles"),
      createRole: () => unavailable("org.createRole"),
      updateRole: () => unavailable("org.updateRole"),
      deleteRole: () => unavailable("org.deleteRole"),
      listRoleBindings: () => unavailable("org.listRoleBindings"),
      createRoleBinding: () => unavailable("org.createRoleBinding"),
      updateRoleBinding: () => unavailable("org.updateRoleBinding"),
      deleteRoleBinding: () => unavailable("org.deleteRoleBinding"),
      getUserRoleAssignments: () => unavailable("org.getUserRoleAssignments"),
      replaceUserRoleAssignments: () =>
        unavailable("org.replaceUserRoleAssignments"),
      listDataAuthorization: () => unavailable("org.listDataAuthorization"),
      updateDataAuthorization: () => unavailable("org.updateDataAuthorization"),
      listApprovalPolicies: () => unavailable("org.listApprovalPolicies"),
      saveApprovalPolicy: () => unavailable("org.saveApprovalPolicy"),
      listServiceAccounts: () => unavailable("org.listServiceAccounts"),
      createServiceAccount: () => unavailable("org.createServiceAccount"),
      updateServiceAccount: () => unavailable("org.updateServiceAccount"),
      listApiKeys: () => unavailable("org.listApiKeys"),
      createApiKey: () => unavailable("org.createApiKey"),
      rotateApiKey: () => unavailable("org.rotateApiKey"),
      revokeApiKey: () => unavailable("org.revokeApiKey"),
    },
    secrets: {
      list: () => unavailable("secrets.list"),
      get: () => unavailable("secrets.get"),
      create: () => unavailable("secrets.create"),
      update: () => unavailable("secrets.update"),
      rotate: () => unavailable("secrets.rotate"),
      delete: () => unavailable("secrets.delete"),
      listCredentials: () => unavailable("secrets.listCredentials"),
      createCredential: () => unavailable("secrets.createCredential"),
      updateCredential: () => unavailable("secrets.updateCredential"),
      listManagedAccounts: () => unavailable("secrets.listManagedAccounts"),
      createManagedAccount: () => unavailable("secrets.createManagedAccount"),
      updateManagedAccount: () => unavailable("secrets.updateManagedAccount"),
    },
    audit: { list: () => unavailable("audit.list") },
    platform: {
      enterprises: {
        list: () => unavailable("platform.enterprises.list"),
        get: () => unavailable("platform.enterprises.get"),
        create: () => unavailable("platform.enterprises.create"),
        update: () => unavailable("platform.enterprises.update"),
        suspend: () => unavailable("platform.enterprises.suspend"),
        activate: () => unavailable("platform.enterprises.activate"),
        disable: () => unavailable("platform.enterprises.disable"),
      },
      admins: {
        list: () => unavailable("platform.admins.list"),
        create: () => unavailable("platform.admins.create"),
        resetAuth: () => unavailable("platform.admins.resetAuth"),
        disable: () => unavailable("platform.admins.disable"),
      },
      sandboxBackends: {
        list: () => unavailable("platform.sandboxBackends.list"),
        create: () => unavailable("platform.sandboxBackends.create"),
        update: () => unavailable("platform.sandboxBackends.update"),
        test: () => unavailable("platform.sandboxBackends.test"),
      },
      images: {
        list: () => unavailable("platform.images.list"),
        create: () => unavailable("platform.images.create"),
        setEnabled: () => unavailable("platform.images.setEnabled"),
      },
      profiles: {
        list: () => unavailable("platform.profiles.list"),
        create: () => unavailable("platform.profiles.create"),
        update: () => unavailable("platform.profiles.update"),
      },
      quotas: {
        get: () => unavailable("platform.quotas.get"),
        update: () => unavailable("platform.quotas.update"),
      },
      sessions: {
        list: () => unavailable("platform.sessions.list"),
        terminate: () => unavailable("platform.sessions.terminate"),
      },
      usage: {
        list: () => unavailable("platform.usage.list"),
      },
      audit: { list: () => unavailable("platform.audit.list") },
      pki: { get: () => unavailable("platform.pki.get") },
    },
    setup: {
      status: () => unavailable("setup.status"),
      submit: () => unavailable("setup.submit"),
    },
  };
}
