import type { ArgusApiClient } from "../client";
import type {
  AuditEvent as AuditEventContract,
  AuditEventPage,
  AuthenticatedSession,
  CreatedApiKeySecret,
  CreatedUserCredential,
  DataScope as DataScopeContract,
  Department as DepartmentContract,
  Enterprise as EnterpriseContract,
  EnterpriseUser as EnterpriseUserContract,
  LoginResult,
  Role as RoleContract,
  RoleBinding as RoleBindingContract,
  ServiceAccount as ServiceAccountContract,
  SetupInitializeResult,
  SetupStatus as SetupStatusContract,
} from "../generated/contracts";
import type {
  AuditEvent,
  DataScope,
  Department,
  Enterprise,
  EnterpriseAdmin,
  Page,
  PlatformAuditEvent,
  Role,
  RoleBinding,
  ServiceAccount,
  SessionInfo,
  User,
} from "../types";
import {
  ClientOperationUnavailableError,
  PasswordChangeRequiredError,
} from "../transport/errors";
import { HttpTransport, type HttpTransportOptions } from "../transport/http";
import { SseTransport } from "../transport/sse";
import { WebSocketTransport } from "../transport/websocket";

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
    actorName: value.actor_id,
    action: value.action,
    origin:
      value.actor_type === "system"
        ? "system"
        : value.domain === "platform"
          ? "platform_ui"
          : "admin_ui",
    resourceType: value.resource_type,
    resourceId: value.resource_id,
    summary:
      typeof value.details.summary === "string"
        ? value.details.summary
        : value.action,
    result: value.result,
    createdAt: value.created_at,
  };
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
  const portal = options.portal;
  const versions = new Map<string, number>();
  const remember = <T extends { id: string; version?: number }>(
    value: T,
  ): T => {
    if (value.version !== undefined) versions.set(value.id, value.version);
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

  function withAdminCredential(value: CreatedUserCredential): EnterpriseAdmin {
    return {
      ...enterpriseAdmin(remember(value.user)),
      temporaryPassword: value.temporary_password,
      temporaryPasswordExpiresAt: value.expires_at,
    };
  }

  client.org = createOrganizationClient(http, versions, remember);
  client.audit = {
    list: (filter, query) =>
      listAudit("enterprise", filter?.action, query?.page?.limit),
  };
  client.platform.audit = {
    list: (filter, query): Promise<Page<PlatformAuditEvent>> =>
      listAudit("platform", filter?.action, query?.page?.limit),
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
    sse: new SseTransport(http),
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
    async listDataScopes() {
      const value = await http.request<{ items: DataScopeContract[] }>(
        "enterprise/data-scopes",
      );
      return value.items.map(remember) as DataScope[];
    },
    async saveDataScope(scope) {
      if (!scope.id) {
        return remember(
          await http.request<DataScopeContract>("enterprise/data-scopes", {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": idempotencyKey() },
            body: scope,
          }),
        ) as DataScope;
      }
      const { id, ...body } = scope;
      return remember(
        await http.request<DataScopeContract>(`enterprise/data-scopes/${id}`, {
          method: "PUT",
          csrf: true,
          body: { ...body, expected_version: expectedVersion(id) },
        }),
      ) as DataScope;
    },
    async deleteDataScope(id) {
      await http.request<void>(
        `enterprise/data-scopes/${id}?expected_version=${expectedVersion(id)}`,
        { method: "DELETE", csrf: true },
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
              data_scope_ids: input.data_scope_ids ?? [],
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
    hosts: {
      list: () => unavailable("hosts.list"),
      get: () => unavailable("hosts.get"),
      previewCreate: () => unavailable("hosts.previewCreate"),
      update: () => unavailable("hosts.update"),
      delete: () => unavailable("hosts.delete"),
      testConnection: () => unavailable("hosts.testConnection"),
      getCollector: () => unavailable("hosts.getCollector"),
      previewCollectorInstall: () =>
        unavailable("hosts.previewCollectorInstall"),
      listSessions: () => unavailable("hosts.listSessions"),
      createSession: () => unavailable("hosts.createSession"),
      getSession: () => unavailable("hosts.getSession"),
      terminateSession: () => unavailable("hosts.terminateSession"),
    },
    connectors: {
      list: () => unavailable("connectors.list"),
      get: () => unavailable("connectors.get"),
      listBastionScopes: () => unavailable("connectors.listBastionScopes"),
      getBastionScope: () => unavailable("connectors.getBastionScope"),
      createBastionScope: () => unavailable("connectors.createBastionScope"),
      updateBastionScope: () => unavailable("connectors.updateBastionScope"),
      regenerateEnrollmentToken: () =>
        unavailable("connectors.regenerateEnrollmentToken"),
      createUninstallCommand: () =>
        unavailable("connectors.createUninstallCommand"),
      deleteBastionScope: () => unavailable("connectors.deleteBastionScope"),
      rotateCertificate: () => unavailable("connectors.rotateCertificate"),
    },
    kubernetes: {
      listClusters: () => unavailable("kubernetes.listClusters"),
      getCluster: () => unavailable("kubernetes.getCluster"),
      previewCreateCluster: () =>
        unavailable("kubernetes.previewCreateCluster"),
      updateCluster: () => unavailable("kubernetes.updateCluster"),
      deleteCluster: () => unavailable("kubernetes.deleteCluster"),
      testClusterConnection: () =>
        unavailable("kubernetes.testClusterConnection"),
      listWorkloads: () => unavailable("kubernetes.listWorkloads"),
      listNodeBindings: () => unavailable("kubernetes.listNodeBindings"),
      verifyNodeBinding: () => unavailable("kubernetes.verifyNodeBinding"),
      listCollectionClaims: () =>
        unavailable("kubernetes.listCollectionClaims"),
      getCollector: () => unavailable("kubernetes.getCollector"),
      previewCollectorInstall: () =>
        unavailable("kubernetes.previewCollectorInstall"),
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
      create: () => unavailable("interactiveCards.create"),
      update: () => unavailable("interactiveCards.update"),
      delete: () => unavailable("interactiveCards.delete"),
      updateBindings: () => unavailable("interactiveCards.updateBindings"),
      validate: () => unavailable("interactiveCards.validate"),
      renderDemo: () => unavailable("interactiveCards.renderDemo"),
      enable: () => unavailable("interactiveCards.enable"),
      disable: () => unavailable("interactiveCards.disable"),
      deprecate: () => unavailable("interactiveCards.deprecate"),
      listToolSchemas: () => unavailable("interactiveCards.listToolSchemas"),
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
      listDataScopes: () => unavailable("org.listDataScopes"),
      saveDataScope: () => unavailable("org.saveDataScope"),
      deleteDataScope: () => unavailable("org.deleteDataScope"),
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
      delete: () => unavailable("secrets.delete"),
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
      audit: { list: () => unavailable("platform.audit.list") },
    },
    setup: {
      status: () => unavailable("setup.status"),
      submit: () => unavailable("setup.submit"),
    },
  };
}
