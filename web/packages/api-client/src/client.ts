import type {
  AIModel,
  ApiKey,
  ApprovalPolicy,
  AuditEvent,
  AuditFilter,
  ConfirmActionResult,
  ConnectionTestResult,
  CreateEnterpriseAdminInput,
  CreateEnterpriseInput,
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
  InviteUserInput,
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
  UpdateAIModelInput,
  UpdateEnterpriseInput,
  UpdateEnterpriseUserInput,
  UpdateRoleBindingInput,
  UpdateSecretInput,
  UsageRange,
  User,
} from "./types";
import type {
  CollectionClaim,
  CollectorInstallState,
  K8sNodeBinding,
  K8sWorkload,
  K8sWorkloadFilter,
} from "./provisional";
import type { TaskEvent, TaskFilter, TaskViewModel } from "./provisional";
import type {
  Automation,
  AutomationRun,
  AutomationWrite,
  ApprovalDecisionCreate,
  ActionOneTimeResult,
  ApprovalRequestView,
  BastionPreviewCreate,
  BastionScope,
  BastionScopePage,
  CompletePasswordChangeRequest,
  ConnectionTest,
  Connector,
  ConnectorPage,
  Conversation,
  ConversationCreate,
  ConversationEvent,
  Credential,
  CredentialCreate,
  CredentialUpdate,
  EnterpriseUser,
  Host,
  HostConnectionTestCreate,
  HostPage,
  HostPreviewCreate,
  HostPreviewUpdate,
  KubernetesCluster,
  KubernetesClusterPage,
  KubernetesConnectionTestCreate,
  KubernetesPreviewCreate,
  KubernetesPreviewUpdate,
  KubernetesResource,
  KubernetesResourcePage,
  ManagedAccount,
  ManagedAccountCreate,
  ManagedAccountUpdate,
  MessageCreate,
  PasswordUpdateRequest,
  PodLogs,
  Run,
  Execution,
  ResourcePreviewUpdate,
  StreamEventEnvelope,
  SandboxUsage,
  InteractiveCard,
  CardVersion,
  CardVersionSummary,
  CardValidationRun,
  CardValidationStart,
  CardValidationEvidence,
  CardConfigurationVersionCreate,
  CardStateCommand,
  CardPresentation,
  CardPresentationCreate,
  CardBindingInvokeResult,
  ToolSchemaCatalog,
  RemoteAccessGrant,
  RemoteAccessGrantWrite,
  RemoteAccessGrantUpdate,
  RemoteAccessPolicy,
  RemoteAccessPolicyWrite,
  RemoteAccessPolicyUpdate,
  AccessRequest,
  AccessRequestCreate,
  RemoteAccessDecisionCreate,
  AccessLease,
  RemoteAccessSession,
  RemoteAccessSessionCreate,
  SessionTicketResult,
  RemoteAccessRecording,
  RecordingEventPage,
} from "./generated/contracts";

export interface CursorListQuery {
  cursor?: string;
  limit?: number;
}

export interface HostListFilter extends CursorListQuery {
  query?: string;
  connection_mode?: Host["connection_mode"];
  bastion_scope_id?: string;
  labels?: Record<string, string[]>;
}

export interface KubernetesResourceQuery extends CursorListQuery {
  resource_type: KubernetesResource["resource_type"];
  namespace?: string;
  query?: string;
}

export interface KubernetesPodLogsQuery {
  namespace: string;
  pod: string;
  container?: string;
  tail_lines?: number;
}

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
    create(input?: ConversationCreate): Promise<Conversation>;
    archive(id: string): Promise<Conversation>;
    listEvents(conversationId: string): Promise<ConversationEvent[]>;
    /**
     * Sends a user message and streams the assistant reply: token deltas,
     * tool call progress, inserted cards and the final message.
     */
    sendMessage(
      conversationId: string,
      input: MessageCreate,
      options?: {
        signal?: AbortSignal;
        last_event_id?: string;
        mock_intent?: "interactive_card.create";
      },
    ): AsyncIterable<StreamEventEnvelope>;
    updateModel(id: string, modelId: string): Promise<Conversation>;
    /** Push immutable events for a conversation (e.g. card_action_result). */
    subscribe(
      conversationId: string,
      listener: (event: ConversationEvent) => void,
    ): Unsubscribe;
  };

  runs: {
    get(runId: string): Promise<Run>;
    cancel(runId: string): Promise<Run>;
    compact(runId: string): Promise<Run>;
  };

  executions: {
    list(): Promise<Page<Execution>>;
    get(executionId: string): Promise<Execution>;
    claimOneTimeResult(executionId: string): Promise<ActionOneTimeResult>;
  };

  /** Host inventory and host collector management. */
  hosts: {
    list(filter?: HostListFilter): Promise<HostPage>;
    get(id: string): Promise<Host>;
    createConnectionTest(
      input: HostConnectionTestCreate,
    ): Promise<ConnectionTest>;
    getConnectionTest(id: string): Promise<ConnectionTest>;
    previewCreateResource(
      input: HostPreviewCreate,
    ): Promise<PendingActionPublic>;
    previewUpdateResource(
      id: string,
      input: HostPreviewUpdate,
    ): Promise<PendingActionPublic>;
    previewDeleteResource(
      id: string,
      expectedVersion: number,
    ): Promise<PendingActionPublic>;
    /** Collector install wizard on a host: status -> preview -> confirm. */
    getCollector(hostId: string): Promise<CollectorInstallState | null>;
    previewCollectorInstall(
      hostId: string,
      input: { profile: string; telemetryRoute: string },
    ): Promise<PendingActionPublic>;
  };

  /** Human-only remote access. Tickets never cross into Agent, Card, or Automation APIs. */
  remoteAccess: {
    listGrants(query?: CursorListQuery): Promise<Page<RemoteAccessGrant>>;
    createGrant(input: RemoteAccessGrantWrite): Promise<RemoteAccessGrant>;
    updateGrant(id: string, input: RemoteAccessGrantUpdate): Promise<RemoteAccessGrant>;
    disableGrant(id: string): Promise<void>;
    listPolicies(query?: CursorListQuery): Promise<Page<RemoteAccessPolicy>>;
    createPolicy(input: RemoteAccessPolicyWrite): Promise<RemoteAccessPolicy>;
    updatePolicy(id: string, input: RemoteAccessPolicyUpdate): Promise<RemoteAccessPolicy>;
    disablePolicy(id: string): Promise<void>;
    listRequests(query?: CursorListQuery): Promise<Page<AccessRequest>>;
    createRequest(input: AccessRequestCreate): Promise<AccessRequest>;
    getRequest(id: string): Promise<AccessRequest>;
    decideRequest(id: string, input: RemoteAccessDecisionCreate): Promise<AccessRequest>;
    listLeases(query?: CursorListQuery): Promise<Page<AccessLease>>;
    revokeLease(id: string): Promise<AccessLease>;
    listSessions(query?: CursorListQuery): Promise<Page<RemoteAccessSession>>;
    createSession(input: RemoteAccessSessionCreate): Promise<RemoteAccessSession>;
    getSession(id: string): Promise<RemoteAccessSession>;
    createTicket(id: string): Promise<SessionTicketResult>;
    terminateSession(id: string, reason: string): Promise<RemoteAccessSession>;
    getRecording(id: string): Promise<RemoteAccessRecording>;
    listRecordingEvents(id: string, cursor?: string): Promise<RecordingEventPage>;
  };

  /** Connectors and Bastion Scopes. */
  connectors: {
    list(query?: CursorListQuery): Promise<ConnectorPage>;
    get(id: string): Promise<Connector>;
    listBastionScopes(query?: CursorListQuery): Promise<BastionScopePage>;
    getBastionScope(id: string): Promise<BastionScope>;
    rotateCertificate(connectorId: string): Promise<Connector>;
    previewCreateBastionScope(
      input: BastionPreviewCreate,
    ): Promise<PendingActionPublic>;
    previewUpdateBastionScope(
      scopeId: string,
      input: ResourcePreviewUpdate,
    ): Promise<PendingActionPublic>;
    previewDeleteBastionScope(
      scopeId: string,
      expectedVersion: number,
    ): Promise<PendingActionPublic>;
    previewReplaceBastionConnector(
      scopeId: string,
      expectedVersion: number,
    ): Promise<PendingActionPublic>;
    previewUninstallConnector(
      connectorId: string,
      expectedVersion: number,
    ): Promise<PendingActionPublic>;
  };

  /** Registered Kubernetes clusters, bindings and collection claims. */
  kubernetes: {
    listClusters(query?: CursorListQuery): Promise<KubernetesClusterPage>;
    getCluster(id: string): Promise<KubernetesCluster>;
    createConnectionTest(
      input: KubernetesConnectionTestCreate,
    ): Promise<ConnectionTest>;
    getConnectionTest(id: string): Promise<ConnectionTest>;
    previewCreateResource(
      input: KubernetesPreviewCreate,
    ): Promise<PendingActionPublic>;
    previewUpdateResource(
      id: string,
      input: KubernetesPreviewUpdate,
    ): Promise<PendingActionPublic>;
    previewDeleteResource(
      id: string,
      expectedVersion: number,
    ): Promise<PendingActionPublic>;
    listResources(
      clusterId: string,
      query: KubernetesResourceQuery,
    ): Promise<KubernetesResourcePage>;
    getPodLogs(
      clusterId: string,
      query: KubernetesPodLogsQuery,
    ): Promise<PodLogs>;
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

  /** Immutable approval requirement snapshots and their decisions. */
  approvalRequests: {
    list(): Promise<ApprovalRequestView[]>;
    get(id: string): Promise<ApprovalRequestView>;
    decide(
      id: string,
      input: ApprovalDecisionCreate,
    ): Promise<ApprovalRequestView>;
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

  /** Deterministic cron jobs bound to a ServiceAccount and fixed Tool input. */
  automations: {
    list(): Promise<Automation[]>;
    get(id: string): Promise<Automation>;
    create(input: AutomationWrite): Promise<Automation>;
    update(id: string, input: AutomationWrite): Promise<Automation>;
    enable(id: string, expectedVersion: number): Promise<Automation>;
    disable(id: string, expectedVersion: number): Promise<Automation>;
    listRuns(id: string): Promise<AutomationRun[]>;
  };

  /** Interactive card catalog, bindings, validation and enable gates. */
  interactiveCards: {
    list(): Promise<InteractiveCard[]>;
    get(id: string): Promise<InteractiveCard>;
    listVersions(id: string): Promise<CardVersionSummary[]>;
    getVersion(id: string, revision: number): Promise<CardVersion>;
    createConfigurationVersion(
      id: string,
      input: CardConfigurationVersionCreate,
    ): Promise<CardVersion>;
    startValidation(
      id: string,
      input: CardValidationStart,
    ): Promise<CardValidationRun>;
    submitValidationEvidence(
      runId: string,
      input: CardValidationEvidence,
    ): Promise<CardValidationRun>;
    changeState(
      id: string,
      action: "activate" | "disable" | "rollback" | "deprecate",
      input: CardStateCommand,
    ): Promise<InteractiveCard>;
    listToolSchemas(): Promise<ToolSchemaCatalog>;
    createPresentation(
      cardInstanceId: string,
      input: CardPresentationCreate,
    ): Promise<CardPresentation>;
    invokeQueryBinding(bindingId: string): Promise<CardBindingInvokeResult>;
    invokeActionBinding(bindingId: string): Promise<CardBindingInvokeResult>;
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
    rotate(id: string, value: string, expectedVersion: number): Promise<Secret>;
    delete(id: string): Promise<void>;
    listCredentials(): Promise<Credential[]>;
    createCredential(input: CredentialCreate): Promise<Credential>;
    updateCredential(id: string, input: CredentialUpdate): Promise<Credential>;
    listManagedAccounts(): Promise<ManagedAccount[]>;
    createManagedAccount(input: ManagedAccountCreate): Promise<ManagedAccount>;
    updateManagedAccount(
      id: string,
      input: ManagedAccountUpdate,
    ): Promise<ManagedAccount>;
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
    usage: {
      list(): Promise<SandboxUsage[]>;
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
