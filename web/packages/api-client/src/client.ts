import type {
  AIModel,
  ApiKey,
  ApprovalPolicy,
  AuditEvent,
  AuditFilter,
  BastionScope,
  InteractiveCard,
  InteractiveCardDemoRender,
  InteractiveCardFilter,
  InteractiveCardValidationResult,
  CollectionClaim,
  CollectorInstallState,
  ConfirmActionResult,
  ConnectionTestResult,
  Connector,
  ConnectorEnrollmentToken,
  ConnectorUninstallCommand,
  CreateBastionScopeInput,
  CreateBastionScopeResult,
  CreateInteractiveCardInput,
  CreateEnterpriseAdminInput,
  CreateEnterpriseInput,
  CreateHostInput,
  CreateK8sClusterInput,
  CreateRemoteSessionInput,
  CreateApiKeyInput,
  CreateRoleBindingInput,
  CreateRoleInput,
  CreateSandboxBackendInput,
  CreateSandboxImageInput,
  CreateSandboxProfileInput,
  CreateSecretInput,
  CreateServiceAccountInput,
  CreatedApiKey,
  DataScope,
  Enterprise,
  EnterpriseAdmin,
  EnterpriseSandboxQuota,
  Department,
  Host,
  HostFilter,
  InviteUserInput,
  K8sCluster,
  K8sNodeBinding,
  K8sWorkload,
  K8sWorkloadFilter,
  ListQuery,
  LoginInput,
  ModelAvailability,
  ModelQuota,
  ModelUsageSummary,
  Page,
  PendingActionPublic,
  PendingActionFilter,
  PlatformAuditEvent,
  PreviewActionInput,
  RemoteSession,
  Role,
  RoleBinding,
  SandboxBackend,
  SandboxImage,
  SandboxProfile,
  SandboxSessionMeta,
  SaveDataScopeInput,
  Secret,
  ServiceAccount,
  SessionInfo,
  SetupResult,
  SetupStatus,
  SetupSubmission,
  Unsubscribe,
  TestAndCreateAIModelInput,
  TestAndCreateAIModelResult,
  ToolOutputSchema,
  UpdateAIModelInput,
  UpdateInteractiveCardInput,
  UpdateEnterpriseInput,
  UpdateHostInput,
  UpdateK8sClusterInput,
  UpdateEnterpriseUserInput,
  UpdateRoleBindingInput,
  UpdateSecretInput,
  UsageRange,
  User,
  UpdateBastionScopeInput,
} from "./types";
import type {
  Conversation,
  CreateConversationInput,
  SendMessageInput,
} from "./provisional";
import type { TaskEvent, TaskFilter, TaskViewModel } from "./provisional";
import type {
  CompletePasswordChangeRequest,
  ConversationEvent,
  EnterpriseUser,
  PasswordUpdateRequest,
  StreamEventEnvelope,
} from "./generated/contracts";

/**
 * Typed client for the Argus control plane. Method groups map one-to-one
 * onto future REST resources; every mutation that changes managed
 * infrastructure goes through the Pending Action two-phase flow
 * (preview -> confirm -> optional approval -> execution Task).
 */
export interface ArgusApiClient {
  /** Session lifecycle and enterprise context. */
  auth: {
    login(input: LoginInput): Promise<SessionInfo>;
    completePasswordChange(
      input: CompletePasswordChangeRequest,
    ): Promise<SessionInfo>;
    changePassword(input: PasswordUpdateRequest): Promise<void>;
    logout(): Promise<void>;
    me(): Promise<SessionInfo>;
  };

  /** Chatbox conversations and streaming assistant replies. */
  conversations: {
    list(query?: ListQuery): Promise<Page<Conversation>>;
    get(id: string): Promise<Conversation>;
    create(input?: CreateConversationInput): Promise<Conversation>;
    archive(id: string): Promise<Conversation>;
    listEvents(conversationId: string): Promise<ConversationEvent[]>;
    /**
     * Sends a user message and streams the assistant reply: token deltas,
     * tool call progress, inserted cards and the final message.
     */
    sendMessage(
      conversationId: string,
      input: SendMessageInput,
      options?: { signal?: AbortSignal; last_event_id?: string },
    ): AsyncIterable<StreamEventEnvelope>;
    updateModel(id: string, modelId: string): Promise<Conversation>;
    /** Push immutable events for a conversation (e.g. card_action_result). */
    subscribe(
      conversationId: string,
      listener: (event: ConversationEvent) => void,
    ): Unsubscribe;
  };

  /** Host inventory, remote sessions and host collector management. */
  hosts: {
    list(filter?: HostFilter, query?: ListQuery): Promise<Page<Host>>;
    get(id: string): Promise<Host>;
    /** Two-phase creation; confirm via approvals.confirm(actionRef). */
    previewCreate(input: CreateHostInput): Promise<PendingActionPublic>;
    update(id: string, patch: UpdateHostInput): Promise<Host>;
    delete(id: string): Promise<void>;
    testConnection(id: string): Promise<ConnectionTestResult>;
    /** Collector install wizard on a host: status -> preview -> confirm. */
    getCollector(hostId: string): Promise<CollectorInstallState | null>;
    previewCollectorInstall(
      hostId: string,
      input: { profile: string; telemetryRoute: string },
    ): Promise<PendingActionPublic>;
    /** Human remote access sessions (never exposed to AI/cards). */
    listSessions(filter?: {
      hostId?: string;
      status?: RemoteSession["status"][];
    }): Promise<RemoteSession[]>;
    createSession(input: CreateRemoteSessionInput): Promise<RemoteSession>;
    getSession(id: string): Promise<RemoteSession>;
    terminateSession(id: string): Promise<RemoteSession>;
  };

  /** Connectors and Bastion Scopes. */
  connectors: {
    list(query?: ListQuery): Promise<Page<Connector>>;
    get(id: string): Promise<Connector>;
    listBastionScopes(): Promise<BastionScope[]>;
    getBastionScope(id: string): Promise<BastionScope>;
    /** Creates a pending scope plus its one-time enrollment token. */
    createBastionScope(
      input: CreateBastionScopeInput,
    ): Promise<CreateBastionScopeResult>;
    updateBastionScope(
      scopeId: string,
      input: UpdateBastionScopeInput,
    ): Promise<BastionScope>;
    regenerateEnrollmentToken(
      scopeId: string,
    ): Promise<ConnectorEnrollmentToken>;
    createUninstallCommand(scopeId: string): Promise<ConnectorUninstallCommand>;
    deleteBastionScope(scopeId: string): Promise<void>;
    rotateCertificate(connectorId: string): Promise<Connector>;
  };

  /** Registered Kubernetes clusters, bindings and collection claims. */
  kubernetes: {
    listClusters(query?: ListQuery): Promise<Page<K8sCluster>>;
    getCluster(id: string): Promise<K8sCluster>;
    previewCreateCluster(
      input: CreateK8sClusterInput,
    ): Promise<PendingActionPublic>;
    updateCluster(
      id: string,
      patch: UpdateK8sClusterInput,
    ): Promise<K8sCluster>;
    deleteCluster(id: string): Promise<void>;
    testClusterConnection(id: string): Promise<ConnectionTestResult>;
    listWorkloads(
      clusterId: string,
      filter?: K8sWorkloadFilter,
    ): Promise<K8sWorkload[]>;
    listNodeBindings(clusterId: string): Promise<K8sNodeBinding[]>;
    verifyNodeBinding(
      bindingId: string,
      input: { hostId: string },
    ): Promise<K8sNodeBinding>;
    listCollectionClaims(clusterId?: string): Promise<CollectionClaim[]>;
    /** DaemonSet collector install wizard on a cluster. */
    getCollector(clusterId: string): Promise<CollectorInstallState | null>;
    previewCollectorInstall(
      clusterId: string,
      input: { profile: string },
    ): Promise<PendingActionPublic>;
  };

  /** Execution tasks with steps, logs and progress subscriptions. */
  tasks: {
    list(filter?: TaskFilter, query?: ListQuery): Promise<Page<TaskViewModel>>;
    get(id: string): Promise<TaskViewModel>;
    cancel(id: string): Promise<TaskViewModel>;
    subscribe(listener: (event: TaskEvent) => void): Unsubscribe;
    subscribeTask(
      id: string,
      listener: (event: TaskEvent) => void,
    ): Unsubscribe;
  };

  /**
   * Pending Actions: two-phase mutations and their approvals.
   * preview() persists an immutable plan; confirm() records the user
   * gesture; dangerous actions then wait for approve()/reject() before
   * an execution Task is created.
   */
  approvals: {
    list(
      filter?: PendingActionFilter,
      query?: ListQuery,
    ): Promise<Page<PendingActionPublic>>;
    get(actionRef: string): Promise<PendingActionPublic>;
    preview(input: PreviewActionInput): Promise<PendingActionPublic>;
    confirm(actionRef: string): Promise<ConfirmActionResult>;
    cancel(actionRef: string): Promise<PendingActionPublic>;
    approve(actionRef: string, comment?: string): Promise<PendingActionPublic>;
    reject(actionRef: string, reason: string): Promise<PendingActionPublic>;
  };

  /** Enterprise OpenAI-compatible models and monthly amount governance. */
  models: {
    list(): Promise<AIModel[]>;
    get(id: string): Promise<AIModel>;
    testAndCreate(
      input: TestAndCreateAIModelInput,
    ): Promise<TestAndCreateAIModelResult>;
    update(id: string, patch: UpdateAIModelInput): Promise<AIModel>;
    delete(id: string): Promise<void>;
    test(id: string): Promise<import("./types").ModelCompatibilityResult>;
    listAvailability(): Promise<ModelAvailability[]>;
    listQuotas(modelId?: string): Promise<ModelQuota[]>;
    setQuota(
      input: Omit<
        ModelQuota,
        "id" | "enterpriseId" | "updatedAt" | "monthlyAmount"
      > & { monthlyAmount?: number },
    ): Promise<ModelQuota | null>;
    usage(range?: UsageRange): Promise<ModelUsageSummary>;
  };

  /** Interactive card catalog, bindings, validation and enable gates. */
  interactiveCards: {
    list(filter?: InteractiveCardFilter): Promise<InteractiveCard[]>;
    get(id: string): Promise<InteractiveCard>;
    create(input: CreateInteractiveCardInput): Promise<InteractiveCard>;
    update(
      id: string,
      patch: UpdateInteractiveCardInput,
    ): Promise<InteractiveCard>;
    delete(id: string): Promise<void>;
    updateBindings(
      id: string,
      bindings: import("./types").SlotBinding[],
    ): Promise<InteractiveCard>;
    validate(id: string): Promise<InteractiveCardValidationResult>;
    renderDemo(id: string): Promise<InteractiveCardDemoRender>;
    enable(id: string): Promise<InteractiveCard>;
    disable(id: string): Promise<InteractiveCard>;
    deprecate(id: string): Promise<InteractiveCard>;
    listToolSchemas(): Promise<ToolOutputSchema[]>;
  };

  /** Organization: users, departments, roles, bindings, data scopes, policies, API keys. */
  org: {
    listUsers(): Promise<User[]>;
    getEnterpriseUser(userId: string): Promise<EnterpriseUser | null>;
    inviteUser(input: InviteUserInput): Promise<User>;
    updateUser(
      userId: string,
      patch: { displayName?: string; status?: User["status"] },
    ): Promise<User>;
    updateEnterpriseUser(
      userId: string,
      patch: UpdateEnterpriseUserInput,
    ): Promise<EnterpriseUser>;
    listDepartments(): Promise<Department[]>;
    createDepartment(input: {
      name: string;
      description?: string;
    }): Promise<Department>;
    updateDepartment(
      id: string,
      patch: {
        name?: string;
        description?: string;
        status?: Department["status"];
      },
    ): Promise<Department>;
    deleteDepartment(id: string): Promise<void>;
    listRoles(): Promise<Role[]>;
    createRole(input: CreateRoleInput): Promise<Role>;
    updateRole(id: string, patch: Partial<CreateRoleInput>): Promise<Role>;
    deleteRole(id: string): Promise<void>;
    listRoleBindings(): Promise<RoleBinding[]>;
    createRoleBinding(input: CreateRoleBindingInput): Promise<RoleBinding>;
    updateRoleBinding(
      id: string,
      patch: UpdateRoleBindingInput,
    ): Promise<RoleBinding>;
    deleteRoleBinding(id: string): Promise<void>;
    listDataScopes(): Promise<DataScope[]>;
    saveDataScope(scope: SaveDataScopeInput): Promise<DataScope>;
    deleteDataScope(id: string): Promise<void>;
    listApprovalPolicies(): Promise<ApprovalPolicy[]>;
    saveApprovalPolicy(
      policy: Omit<ApprovalPolicy, "id" | "enterpriseId" | "createdAt"> & {
        id?: string;
      },
    ): Promise<ApprovalPolicy>;
    listServiceAccounts(): Promise<ServiceAccount[]>;
    createServiceAccount(
      input: CreateServiceAccountInput,
    ): Promise<ServiceAccount>;
    updateServiceAccount(
      id: string,
      patch: {
        description?: string;
        status?: "active" | "disabled";
        allowed_tool_ids?: string[];
        data_scope_ids?: string[];
      },
    ): Promise<ServiceAccount>;
    listApiKeys(serviceAccountId: string): Promise<ApiKey[]>;
    createApiKey(
      serviceAccountId: string,
      input: CreateApiKeyInput,
    ): Promise<CreatedApiKey>;
    rotateApiKey(id: string): Promise<CreatedApiKey>;
    revokeApiKey(id: string): Promise<void>;
  };

  /** Secret metadata; values are write-only and never returned. */
  secrets: {
    list(query?: ListQuery): Promise<Page<Secret>>;
    get(id: string): Promise<Secret>;
    create(input: CreateSecretInput): Promise<Secret>;
    update(id: string, patch: UpdateSecretInput): Promise<Secret>;
    delete(id: string): Promise<void>;
  };

  /** Enterprise audit trail. */
  audit: {
    list(filter?: AuditFilter, query?: ListQuery): Promise<Page<AuditEvent>>;
  };

  /** Platform super admin domain (docs/07). */
  platform: {
    enterprises: {
      list(query?: ListQuery): Promise<Page<Enterprise>>;
      get(id: string): Promise<Enterprise>;
      create(input: CreateEnterpriseInput): Promise<Enterprise>;
      update(id: string, patch: UpdateEnterpriseInput): Promise<Enterprise>;
      suspend(id: string): Promise<Enterprise>;
      activate(id: string): Promise<Enterprise>;
      disable(id: string): Promise<Enterprise>;
    };
    admins: {
      list(enterpriseId?: string): Promise<EnterpriseAdmin[]>;
      create(input: CreateEnterpriseAdminInput): Promise<EnterpriseAdmin>;
      resetAuth(id: string): Promise<EnterpriseAdmin>;
      disable(id: string): Promise<EnterpriseAdmin>;
    };
    sandboxBackends: {
      list(): Promise<SandboxBackend[]>;
      create(input: CreateSandboxBackendInput): Promise<SandboxBackend>;
      update(
        id: string,
        patch: Partial<CreateSandboxBackendInput> & { enabled?: boolean },
      ): Promise<SandboxBackend>;
      test(id: string): Promise<ConnectionTestResult>;
    };
    images: {
      list(): Promise<SandboxImage[]>;
      create(input: CreateSandboxImageInput): Promise<SandboxImage>;
      setEnabled(id: string, enabled: boolean): Promise<SandboxImage>;
    };
    profiles: {
      list(): Promise<SandboxProfile[]>;
      create(input: CreateSandboxProfileInput): Promise<SandboxProfile>;
      update(
        id: string,
        patch: Partial<CreateSandboxProfileInput> & { enabled?: boolean },
      ): Promise<SandboxProfile>;
    };
    quotas: {
      get(enterpriseId: string): Promise<EnterpriseSandboxQuota>;
      update(
        enterpriseId: string,
        patch: Partial<Omit<EnterpriseSandboxQuota, "enterpriseId">>,
      ): Promise<EnterpriseSandboxQuota>;
    };
    sessions: {
      list(filter?: {
        enterpriseId?: string;
        status?: SandboxSessionMeta["status"][];
      }): Promise<SandboxSessionMeta[]>;
      terminate(id: string): Promise<SandboxSessionMeta>;
    };
    audit: {
      list(
        filter?: AuditFilter,
        query?: ListQuery,
      ): Promise<Page<PlatformAuditEvent>>;
    };
  };

  /** One-time platform initialization wizard. */
  setup: {
    status(): Promise<SetupStatus>;
    submit(input: SetupSubmission): Promise<SetupResult>;
  };
}
