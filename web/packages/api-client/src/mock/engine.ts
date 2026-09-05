import type {
  ConfirmActionResult,
  PendingActionPublic,
  RiskLevel,
} from "../types";
import type {
  ActionOneTimeResult,
  CollectorInstance,
  ConnectorInstallOperation,
  Execution,
  TelemetryRoute,
} from "../generated/contracts";
import type { TaskViewModel, TaskStep } from "../provisional";
import type {
  MockCardInstance as CardInstance,
  MockChatMessage as ChatMessage,
  MockChatStreamEvent as ChatStreamEvent,
  MockToolCallTrace as ToolCallTrace,
} from "./chat-types";
import type { BaseContext, Engine } from "./context";
import { nextId, registerSettling } from "./store";
import type {
  ConnectorEnrollmentPurpose,
  MockBastionScope,
  MockHost,
} from "./resource-models";
import { publicActionType } from "./action-type";
import { mockInstallInstructionSets } from "./install-instructions";

type MockTelemetryRouteInput = {
  kind: "direct_argus" | "bastion_gateway";
  transport: "direct" | "executor_tunnel" | "bastion_tunnel";
  loopbackPort?: number;
  gatewayCollectorId?: string;
};

function parseTelemetryRouteInput(
  input: Record<string, unknown>,
): MockTelemetryRouteInput {
  const kind = input["route_kind"];
  const transport = input["transport"];
  if (kind !== "direct_argus" && kind !== "bastion_gateway") {
    throw new Error("TELEMETRY_ROUTE_INVALID");
  }
  if (
    transport !== "direct" &&
    transport !== "executor_tunnel" &&
    transport !== "bastion_tunnel"
  ) {
    throw new Error("TELEMETRY_ROUTE_INVALID");
  }
  const gatewayCollectorId =
    typeof input["gateway_collector_id"] === "string" &&
    input["gateway_collector_id"].length > 0
      ? input["gateway_collector_id"]
      : undefined;
  const loopbackPort = input["loopback_port"];
  if (
    (kind === "direct_argus" &&
      (transport === "bastion_tunnel" || gatewayCollectorId !== undefined)) ||
    (kind === "bastion_gateway" &&
      (transport === "executor_tunnel" || gatewayCollectorId === undefined))
  ) {
    throw new Error("TELEMETRY_ROUTE_INVALID");
  }
  if (transport === "direct") {
    if (loopbackPort !== undefined) throw new Error("TELEMETRY_ROUTE_INVALID");
    return { kind, transport, gatewayCollectorId };
  }
  if (
    typeof loopbackPort !== "number" ||
    !Number.isInteger(loopbackPort) ||
    loopbackPort < 1 ||
    loopbackPort >= 65535
  ) {
    throw new Error("TELEMETRY_ROUTE_INVALID");
  }
  return { kind, transport, loopbackPort, gatewayCollectorId };
}

function mockTelemetryRoute(
  collector: CollectorInstance,
  input: MockTelemetryRouteInput,
  enterpriseId: string,
  now: string,
  routeId: string,
): TelemetryRoute {
  return {
    id: collector.route?.id ?? routeId,
    enterprise_id: enterpriseId,
    collector_id: collector.id,
    kind: input.kind,
    transport: input.transport,
    ...(input.loopbackPort !== undefined
      ? {
          loopback_port: input.loopbackPort,
          tunnel_status: "established" as const,
          tunnel_last_established_at: now,
        }
      : {}),
    ...(input.gatewayCollectorId !== undefined
      ? { gateway_collector_id: input.gatewayCollectorId }
      : {}),
    status: "active",
    last_tested_at: now,
    version: (collector.route?.version ?? 0) + 1,
    created_at: collector.route?.created_at ?? now,
    updated_at: now,
  };
}

const IMMEDIATE_RESOURCE_TOOLS = new Set([
  "host.create",
  "host.update",
  "host.delete",
  "host.enrollment.rotate",
  "host.uninstall.command",
  "kubernetes.cluster.create",
  "kubernetes.cluster.update",
  "kubernetes.cluster.delete",
  "bastion.scope.create",
  "bastion.scope.update",
  "bastion.scope.delete",
  "bastion.connector.replace",
  "bastion.enrollment.rotate",
  "bastion.connector.install.retry",
]);

const TOOL_RISK: Record<string, RiskLevel> = {
  "host.create": "write",
  "host.update": "write",
  "host.delete": "dangerous",
  "host.enrollment.rotate": "write",
  "host.uninstall.command": "dangerous",
  "host.restart": "dangerous",
  "telemetry.host.install": "write",
  "telemetry.kubernetes.install": "write",
  "telemetry.collector.configure": "write",
  "telemetry.collector.route": "write",
  "telemetry.collector.upgrade": "write",
  "kubernetes.cluster.create": "write",
  "kubernetes.cluster.update": "write",
  "kubernetes.cluster.delete": "dangerous",
  "bastion.scope.create": "write",
  "bastion.scope.update": "write",
  "bastion.scope.delete": "dangerous",
  "bastion.connector.replace": "dangerous",
  "bastion.enrollment.rotate": "write",
  "bastion.connector.install.retry": "write",
  "kubernetes.workload.restart": "dangerous",
  "connector.cert.rotate": "write",
};

const TOOL_STEPS: Record<string, string[]> = {
  "host.create": ["连接测试", "创建主机记录", "安装与验证 Collector"],
  "telemetry.host.install": [
    "探测目标环境",
    "传输安装包",
    "校验 Digest",
    "安装并注册服务",
    "健康检查",
  ],
  "telemetry.kubernetes.install": [
    "渲染 DaemonSet",
    "下发工作负载",
    "就绪检查",
  ],
  "telemetry.collector.configure": [
    "校验配置",
    "下发 Revision",
    "确认监控状态",
  ],
  "telemetry.collector.route": ["测试上报路由", "更新出口配置", "确认监控状态"],
  "telemetry.collector.upgrade": [
    "批次备份",
    "滚动升级",
    "健康检查",
    "配置收敛",
  ],
  "kubernetes.cluster.create": ["API 连接测试", "权限校验", "登记集群"],
  "kubernetes.workload.restart": ["冻结自动扩缩", "滚动重启 Pod", "就绪检查"],
  "connector.cert.rotate": ["签发新证书", "重叠窗口切换", "吊销旧证书"],
};

const TOOL_TASK_TYPE: Record<string, TaskViewModel["type"]> = {
  "host.create": "host_onboard",
  "telemetry.host.install": "collector_install",
  "telemetry.kubernetes.install": "collector_install",
  "telemetry.collector.upgrade": "collector_upgrade",
  "kubernetes.cluster.create": "kubernetes_change",
  "kubernetes.workload.restart": "kubernetes_change",
  "connector.cert.rotate": "certificate_rotation",
};

function summarize(tool: string, input_data: Record<string, unknown>): string {
  const name =
    input_data["name"] ?? input_data["workload"] ?? input_data["hostId"] ?? "";
  return `${tool} ${String(name)}`.trim();
}

function buildDiff(
  tool: string,
  input_data: Record<string, unknown>,
): PendingActionPublic["diff"] {
  if (tool === "host.create") {
    return [
      {
        kind: "add",
        text: `+ resource.host ${String(input_data["name"] ?? "")}`,
      },
      { kind: "add", text: "+ telemetry.collector v24.1.3" },
      { kind: "note", text: "无端口与防火墙变更" },
    ];
  }
  return [{ kind: "change", text: `~ ${summarize(tool, input_data)}` }];
}

function chunkText(text: string): string[] {
  const chunks: string[] = [];
  for (let i = 0; i < text.length; i += 6) chunks.push(text.slice(i, i + 6));
  return chunks;
}

/**
 * Pending Action engine: preview persists an immutable plan, confirm/approve
 * drive the state machine, and execution materializes a Task whose steps are
 * simulated on a timer until the side effect is applied.
 */
export function createEngine(ctx: BaseContext): Engine {
  const { db } = ctx;

  function createPendingAction(
    input: Parameters<Engine["createPendingAction"]>[0],
  ): PendingActionPublic {
    const riskLevel = TOOL_RISK[input.tool] ?? "write";
    const policy = db.approvalPolicies.find(
      (candidate) =>
        candidate.enterpriseId === ctx.enterpriseId() &&
        candidate.enabled &&
        riskLevel !== "read" &&
        candidate.matchRiskLevels.includes(riskLevel),
    );
    const who = ctx.actor();
    const actionRef = `pa_ref_${nextId(db, "pa")}`;
    const now = ctx.nowIso();
    const action: PendingActionPublic = {
      schema_version: "argus.pending_action/v1",
      action_ref: actionRef,
      action_type: publicActionType(input.tool),
      title: input.title ?? input.tool,
      summary: summarize(input.tool, input.input_data),
      risk: riskLevel,
      preview: { ...input.input_data },
      diff: buildDiff(input.tool, input.input_data),
      expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
      status: "awaiting_confirmation",
      available_actions: ["confirm", "cancel"],
      approval: policy
        ? {
            required: true,
            policy_ref: policy.id,
            minimum_approvers: policy.minApprovers,
            approved_count: 0,
            separation_of_duty: policy.separationOfDuty,
          }
        : undefined,
      created_at: now,
      updated_at: now,
    };
    db.actionPlans[actionRef] = {
      tool: input.tool,
      enterprise_id: ctx.enterpriseId(),
      created_by: who.id,
      created_by_name: who.displayName,
      conversation_id: input.conversation_id,
      input_data: { ...input.input_data },
      approval_decisions: [],
    };
    db.pendingActions.unshift(action);
    ctx.audit(`${input.tool}.preview`, {
      resourceType: "pending_action",
      resourceId: action.action_ref,
      summary: action.title,
    });
    ctx.save();
    return action;
  }

  function getAction(actionRef: string): PendingActionPublic {
    return ctx.mustFind(
      db.pendingActions,
      (entry) => entry.action_ref === actionRef,
      "pending action",
    );
  }

  function ensureNotExpired(action: PendingActionPublic): void {
    if (
      (action.status === "awaiting_confirmation" ||
        action.status === "awaiting_approval") &&
      Date.parse(action.expires_at) < Date.now()
    ) {
      action.status = "expired";
      action.updated_at = ctx.nowIso();
      action.available_actions = [];
      ctx.save();
      throw new Error("pending action expired; preview again");
    }
  }

  function startExecution(action: PendingActionPublic): TaskViewModel {
    const plan = db.actionPlans[action.action_ref];
    if (!plan) throw new Error("pending action plan unavailable");
    action.status = "executing";
    action.available_actions = [];
    action.updated_at = ctx.nowIso();
    const who = ctx.actor();
    const stepNames = TOOL_STEPS[plan.tool] ?? ["执行计划", "验证结果"];
    const steps: TaskStep[] = stepNames.map((name, index) => ({
      id: `s${index + 1}`,
      name,
      status: "pending",
    }));
    const task: TaskViewModel = {
      id: nextId(db, "task"),
      enterpriseId: plan.enterprise_id,
      type: TOOL_TASK_TYPE[plan.tool] ?? "generic",
      title: action.title,
      status: "running",
      origin: plan.conversation_id ? "admin_chatbox" : "admin_ui",
      createdBy: who.id,
      createdByName: who.displayName,
      relatedResources: [],
      steps,
      logs: [],
      pendingActionId: action.action_ref,
      progress: 0,
      startedAt: ctx.nowIso(),
      createdAt: ctx.nowIso(),
    };
    plan.task_id = task.id;
    action.execution_ref = task.id;
    db.tasks.unshift(task);
    ctx.save();
    ctx.emitTask(task);
    scheduleSteps(task, action);
    return task;
  }

  function commitResourceAction(
    action: PendingActionPublic,
  ): ConfirmActionResult | undefined {
    const plan = db.actionPlans[action.action_ref];
    if (
      !plan ||
      plan.conversation_id ||
      !IMMEDIATE_RESOURCE_TOOLS.has(plan.tool)
    ) {
      return undefined;
    }
    action.status = "executing";
    action.available_actions = [];
    action.updated_at = ctx.nowIso();
    const sideEffect = applySideEffect(action);
    const execution = createResourceExecution(action, sideEffect);
    action.execution_ref = execution.execution_id;
    action.status = sideEffect?.operation ? "executing" : "succeeded";
    action.result_summary ??= "操作已完成";
    action.updated_at = ctx.nowIso();
    ctx.audit(`${plan.tool}.commit`, {
      resourceType: "pending_action",
      resourceId: action.action_ref,
      summary: action.result_summary,
    });
    ctx.save();
    if (sideEffect?.operation) {
      scheduleConnectorInstall(sideEffect.operation, action, execution);
    }
    return { pending_action: action, execution };
  }

  function createResourceExecution(
    action: PendingActionPublic,
    sideEffect: SideEffectResult,
  ): Execution {
    const now = ctx.nowIso();
    const execution: Execution = {
      execution_id: nextId(db, "exec"),
      action_ref: action.action_ref,
      status: sideEffect?.operation ? "result_unknown" : "succeeded",
      one_time_result_state: sideEffect?.instructionSets
        ? "available"
        : "unavailable",
      resource_ref: sideEffect?.resource,
      operation_ref: sideEffect?.operation
        ? { kind: "connector_install", id: sideEffect.operation.id }
        : undefined,
      created_at: now,
      updated_at: now,
    };
    db.executions.unshift(execution);
    if (sideEffect?.instructionSets) {
      db.oneTimeResults[execution.execution_id] = {
        schema_version: "argus.action_one_time_result/v3",
        execution_id: execution.execution_id,
        result_kind: sideEffect.resultKind,
        instruction_sets: sideEffect.instructionSets,
        expires_at: sideEffect.expiresAt,
      };
      markCommandProjection(
        sideEffect,
        execution.execution_id,
        "command_available",
      );
    }
    return execution;
  }

  function markCommandProjection(
    sideEffect: Exclude<SideEffectResult, undefined>,
    executionId: string,
    state: "command_available" | "command_consumed" | "command_expired",
  ) {
    if (!sideEffect.resource) return;
    if (sideEffect.resource.resource_type === "host") {
      const host = db.hosts.find(
        (entry) => entry.id === sideEffect.resource?.resource_id,
      );
      if (host) {
        host.onboardingState = state;
        host.onboardingExecutionId = executionId;
        host.updatedAt = ctx.nowIso();
      }
      return;
    }
    const scope = db.bastionScopes.find(
      (entry) => entry.id === sideEffect.resource?.resource_id,
    );
    if (scope) {
      scope.onboardingState = state;
      scope.onboardingExecutionId = executionId;
      scope.updatedAt = ctx.nowIso();
    }
  }

  function scheduleSteps(
    task: TaskViewModel,
    action: PendingActionPublic,
  ): void {
    let index = 0;
    const runNext = () => {
      const step = task.steps[index];
      if (!step) {
        finishTask(task, action);
        return;
      }
      step.status = "running";
      step.startedAt = ctx.nowIso();
      ctx.emitTask(task);
      setTimeout(() => {
        step.status = "done";
        step.finishedAt = ctx.nowIso();
        task.logs.push({
          timestamp: ctx.nowIso(),
          level: "info",
          message: `${step.name} 完成`,
        });
        index += 1;
        task.progress = Math.round((index / task.steps.length) * 100);
        ctx.emitTask(task);
        ctx.save();
        runNext();
      }, ctx.options.stepDelay);
    };
    runNext();
  }

  function scheduleConnectorInstall(
    operation: ConnectorInstallOperation,
    action: PendingActionPublic,
    execution: Execution,
  ) {
    const stages: ConnectorInstallOperation["stage"][] = [
      "queued",
      "ssh_connecting",
      "artifact_verifying",
      "artifact_transferring",
      "service_installing",
      ...(operation.install_mode === "direct_install_tunnel"
        ? (["control_tunnel_establishing"] as const)
        : []),
      "enrolling",
      "waiting_connector_online",
      "completed",
    ];
    let index = 0;
    const advance = () => {
      const current = db.connectorInstallOperations.find(
        (entry) => entry.id === operation.id,
      );
      if (
        !current ||
        ["failed", "expired", "cancelled"].includes(current.status)
      )
        return;
      const previous = current.events.at(-1);
      if (previous) previous.status = "succeeded";
      index += 1;
      const stage = stages[index];
      if (!stage) return;
      const now = ctx.nowIso();
      current.stage = stage;
      current.status = stage === "completed" ? "succeeded" : "running";
      current.started_at ??= now;
      current.updated_at = now;
      current.events.push({
        id: nextId(db, "op-event"),
        stage,
        status: "started",
        occurred_at: now,
      });
      if (stage === "completed") {
        current.events[current.events.length - 1]!.status = "succeeded";
        current.completed_at = now;
        current.connector_online_at = now;
        if (current.install_mode === "direct_install_tunnel") {
          current.control_tunnel_status = "established";
        }
        execution.status = "succeeded";
        execution.updated_at = now;
        action.status = "succeeded";
        action.result_summary = "Connector 安装并上线";
        action.updated_at = now;
        const scope = db.bastionScopes.find(
          (entry) => entry.id === current.bastion_scope_id,
        );
        if (scope) {
          scope.status = "active";
          scope.activeConnectorId = current.connector_id;
          scope.connectorHostId = current.host_id;
          scope.controlTunnelStatus = current.control_tunnel_status;
          scope.onboardingState = "registered";
          scope.onboardingExecutionId = execution.execution_id;
          scope.onboardingOperationId = current.id;
          scope.updatedAt = now;
          if (
            !db.connectors.some((entry) => entry.id === current.connector_id)
          ) {
            db.connectors.push({
              id: current.connector_id,
              enterpriseId: scope.enterpriseId,
              name: `${scope.name} Connector`,
              hostId: current.host_id,
              bastionScopeId: scope.id,
              version: "v0.1.0-p4",
              status: "online",
              capabilities: ["ssh", "collector", "telemetry_tunnel"],
              connectionEpoch: 1,
              certificateExpiresAt: new Date(
                Date.now() + 90 * 86_400_000,
              ).toISOString(),
              connectedAt: now,
              lastHeartbeatAt: now,
              managedHostCount: 0,
              createdAt: now,
            });
          }
        }
        ctx.save();
        return;
      }
      ctx.save();
      setTimeout(advance, ctx.options.stepDelay);
    };
    setTimeout(advance, ctx.options.stepDelay);
  }

  function finishTask(task: TaskViewModel, action: PendingActionPublic): void {
    task.status = "succeeded";
    task.finishedAt = ctx.nowIso();
    task.progress = 100;
    action.status = "succeeded";
    action.result_summary = "执行成功";
    action.updated_at = ctx.nowIso();
    applySideEffect(action);
    const execution = db.executions.find(
      (entry) => entry.execution_id === task.id,
    );
    if (execution) {
      execution.status = "succeeded";
      execution.updated_at = ctx.nowIso();
    }
    const plan = db.actionPlans[action.action_ref];
    ctx.audit(`${plan?.tool ?? "pending_action"}.commit`, {
      resourceType: "task",
      resourceId: task.id,
      summary: `${action.title} 执行成功`,
    });
    ctx.save();
    ctx.emitTask(task);
  }

  function createHostEnrollment(
    host: MockHost,
    createdBy: string,
  ): Exclude<SideEffectResult, undefined> {
    for (const existing of db.hostEnrollmentTokens) {
      if (existing.hostId === host.id && existing.status === "active") {
        existing.status = "revoked";
        existing.remainingUses = 0;
      }
    }
    const token = `hst_${nextId(db, "token")}_${Math.random().toString(36).slice(2, 10)}`;
    const expiresAt = new Date(Date.now() + 24 * 60 * 60_000).toISOString();
    const instructionSets = mockInstallInstructionSets(token, expiresAt);
    const record = {
      id: nextId(db, "hostenroll"),
      enterpriseId: host.enterpriseId,
      hostId: host.id,
      status: "active" as const,
      token,
      instructionSets,
      expiresAt,
      remainingUses: 1,
      createdBy,
      createdAt: ctx.nowIso(),
    };
    db.hostEnrollmentTokens.push(record);
    return {
      instructionSets,
      resultKind: "host_install_command",
      expiresAt,
      resource: {
        resource_type: "host",
        resource_id: host.id,
        version: host.resourceVersion ?? 1,
      },
    };
  }

  function createEnrollment(
    scope: MockBastionScope,
    purpose: ConnectorEnrollmentPurpose,
    createdBy: string,
  ): Exclude<SideEffectResult, undefined> {
    for (const existing of db.enrollmentTokens) {
      if (
        existing.bastionScopeId === scope.id &&
        existing.status === "active"
      ) {
        existing.status = "revoked";
        existing.remainingUses = 0;
      }
    }
    const enrollmentId = nextId(db, "enroll");
    const token = `enroll_${nextId(db, "token")}_${Math.random()
      .toString(36)
      .slice(2, 10)}`;
    const expiresAt = new Date(Date.now() + 24 * 60 * 60_000).toISOString();
    const instructionSets = mockInstallInstructionSets(
      token,
      expiresAt,
      "connector",
    );
    const enrollment = {
      id: enrollmentId,
      enterpriseId: scope.enterpriseId,
      bastionScopeId: scope.id,
      purpose,
      status: "active" as const,
      token,
      instructionSets,
      expiresAt,
      remainingUses: 1,
      createdBy,
      createdAt: ctx.nowIso(),
    };
    db.enrollmentTokens.push(enrollment);
    scope.registrationToken = enrollment;
    return {
      instructionSets,
      resultKind: "connector_install_command",
      expiresAt,
      resource: {
        resource_type: "bastion_scope",
        resource_id: scope.id,
        version: scope.resourceVersion ?? 1,
      },
    };
  }

  function createConnectorInstallOperation(
    scope: MockBastionScope,
    input: Record<string, unknown>,
    retryOf?: string,
  ): ConnectorInstallOperation {
    const now = ctx.nowIso();
    const operation: ConnectorInstallOperation = {
      id: nextId(db, "connector-operation"),
      connector_id: nextId(db, "connector"),
      bastion_scope_id: scope.id,
      host_id: scope.connectorHostId ?? nextId(db, "connector-host"),
      retry_of: retryOf,
      connection_test_id:
        typeof input["connection_test_id"] === "string"
          ? input["connection_test_id"]
          : undefined,
      release_version_id: "connector-release-v1",
      install_mode:
        scope.onboardingMode === "direct_install_tunnel"
          ? "direct_install_tunnel"
          : "direct_install",
      stage: "queued",
      status: "queued",
      attempt: 1,
      max_attempts: 3,
      control_tunnel_status:
        scope.onboardingMode === "direct_install_tunnel"
          ? "desired"
          : undefined,
      events: [
        {
          id: nextId(db, "op-event"),
          stage: "queued",
          status: "started",
          occurred_at: now,
        },
      ],
      expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
      created_at: now,
      updated_at: now,
    };
    db.connectorInstallOperations.unshift(operation);
    return operation;
  }

  type SideEffectResult =
    | {
        instructionSets?: ActionOneTimeResult["instruction_sets"];
        resultKind: ActionOneTimeResult["result_kind"];
        expiresAt: string;
        resource?: NonNullable<Execution["resource_ref"]>;
        operation?: ConnectorInstallOperation;
      }
    | undefined;

  function applySideEffect(action: PendingActionPublic): SideEffectResult {
    const plan = db.actionPlans[action.action_ref];
    if (!plan) throw new Error("pending action plan unavailable");
    const { input_data } = plan;
    if (plan.tool === "host.create") {
      const host: MockHost = {
        id: nextId(db, "host"),
        enterpriseId: plan.enterprise_id,
        name: String(input_data["name"] ?? "new-host"),
        hostname: String(
          input_data["hostname"] ?? input_data["name"] ?? "new-host",
        ),
        address: String(input_data["address"] ?? ""),
        port: Number(input_data["port"] ?? 22),
        platform: (input_data["platform"] as MockHost["platform"]) ?? "linux",
        connectionMode:
          (input_data["connection_mode"] as MockHost["connectionMode"]) ??
          (input_data["connectionMode"] as MockHost["connectionMode"]) ??
          "direct_ssh",
        bastionScopeId:
          (input_data["bastion_scope_id"] as string | undefined) ??
          (input_data["bastionScopeId"] as string | undefined),
        connectorId: input_data["connector_id"] as string | undefined,
        credentialRef:
          (input_data["managed_account_id"] as string | undefined) ??
          (input_data["credential_id"] as string | undefined) ??
          (input_data["credentialRef"] as string | undefined),
        environment:
          (input_data["environment"] as MockHost["environment"]) ??
          "production",
        labels: (input_data["labels"] as Record<string, string>) ?? {},
        connectionStatus: "online",
        collectorStatus: "installing",
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
        resourceVersion: 1,
      };
      db.hosts.push(host);
      const scope = db.bastionScopes.find(
        (entry) => entry.id === host.bastionScopeId,
      );
      if (scope) {
        scope.memberHostIds.push(host.id);
        scope.updatedAt = ctx.nowIso();
      }
      action.result_summary = `已创建主机 ${host.name}`;
      if (host.connectionMode === "self_enrolled") {
        host.connectionStatus = "onboarding";
        host.address = "";
        host.port = 0;
        return createHostEnrollment(host, plan.created_by);
      }
      return;
    }
    if (plan.tool === "host.enrollment.rotate") {
      const host = db.hosts.find(
        (entry) => entry.id === String(input_data["host_id"] ?? ""),
      );
      if (!host) return;
      host.resourceVersion = (host.resourceVersion ?? 1) + 1;
      return createHostEnrollment(host, plan.created_by);
    }
    if (plan.tool === "host.uninstall.command") {
      const host = db.hosts.find(
        (entry) => entry.id === String(input_data["host_id"] ?? ""),
      );
      if (!host) return;
      const token = `hun_${nextId(db, "token")}_${Math.random().toString(36).slice(2, 10)}`;
      const expiresAt = new Date(Date.now() + 30 * 60_000).toISOString();
      return {
        instructionSets: mockInstallInstructionSets(token, expiresAt),
        resultKind: "host_uninstall_command",
        expiresAt,
        resource: {
          resource_type: "host",
          resource_id: host.id,
          version: host.resourceVersion ?? 1,
        },
      };
    }
    if (plan.tool === "host.update") {
      const host = db.hosts.find(
        (entry) => entry.id === String(input_data["id"] ?? ""),
      );
      if (!host) return;
      if (input_data["name"] !== undefined)
        host.name = String(input_data["name"]);
      if (input_data["hostname"] !== undefined) {
        host.hostname = String(input_data["hostname"]);
      }
      if (input_data["address"] !== undefined) {
        host.address = String(input_data["address"]);
      }
      if (input_data["port"] !== undefined)
        host.port = Number(input_data["port"]);
      if (input_data["environment"] !== undefined) {
        host.environment = input_data["environment"] as MockHost["environment"];
      }
      if (input_data["labels"] !== undefined) {
        host.labels = input_data["labels"] as Record<string, string>;
      }
      host.resourceVersion = (host.resourceVersion ?? 1) + 1;
      host.updatedAt = ctx.nowIso();
      action.result_summary = `已更新主机 ${host.name}`;
      return;
    }
    if (plan.tool === "host.delete") {
      const id = String(input_data["id"] ?? "");
      db.hosts = db.hosts.filter((entry) => entry.id !== id);
      for (const scope of db.bastionScopes) {
        scope.memberHostIds = scope.memberHostIds.filter(
          (hostId) => hostId !== id,
        );
      }
      action.result_summary = `已删除主机 ${id}`;
      return;
    }
    if (plan.tool === "telemetry.host.install") {
      const hostId = String(
        input_data["host_id"] ?? input_data["hostId"] ?? "",
      );
      if (hostId.length === 0) throw new Error("TELEMETRY_ROUTE_INVALID");
      const routeInput = parseTelemetryRouteInput(input_data);
      let collector = db.collectors.find(
        (entry) =>
          entry.resource_type === "host" && entry.resource_id === hostId,
      );
      const now = ctx.nowIso();
      if (collector) {
        collector.status = "installing";
        collector.desired_revision += 1;
        collector.effective_revision = collector.desired_revision - 1;
        collector.updated_at = now;
      } else {
        const id = nextId(db, "col");
        collector = {
          id,
          enterprise_id: plan.enterprise_id,
          resource_type: "host",
          resource_id: hostId,
          distribution_version_id: String(
            input_data["distribution_version_id"] ?? "dist-linux-arm64-v1",
          ),
          platform: "linux_arm64",
          role: "leaf",
          desired_revision: 1,
          effective_revision: 0,
          status: "installing",
          version: 1,
          created_at: now,
          updated_at: now,
        };
        db.collectors.push(collector);
      }
      collector.route = mockTelemetryRoute(
        collector,
        routeInput,
        plan.enterprise_id,
        now,
        nextId(db, "route"),
      );
      registerSettling(db, collector.id);
      const host = db.hosts.find((entry) => entry.id === hostId);
      if (host) {
        host.collectorStatus = "installing";
        host.telemetryRoute = routeInput.kind;
        host.updatedAt = now;
      }
      return;
    }
    if (plan.tool === "telemetry.kubernetes.install") {
      const now = ctx.nowIso();
      const id = nextId(db, "col");
      db.collectors.push({
        id,
        enterprise_id: plan.enterprise_id,
        resource_type: "kubernetes_cluster",
        resource_id: String(
          input_data["cluster_id"] ?? input_data["clusterId"] ?? "",
        ),
        distribution_version_id: String(
          input_data["distribution_version_id"] ?? "dist-linux-arm64-v1",
        ),
        platform: "linux_arm64",
        role: "daemonset",
        desired_revision: 1,
        effective_revision: 0,
        status: "installing",
        version: 1,
        created_at: now,
        updated_at: now,
      });
      registerSettling(db, id);
      return;
    }
    if (plan.tool === "telemetry.collector.configure") {
      const hostId = String(
        input_data["host_id"] ?? input_data["hostId"] ?? "",
      );
      const collector = db.collectors.find(
        (entry) =>
          entry.resource_type === "host" && entry.resource_id === hostId,
      );
      if (collector) {
        collector.desired_revision += 1;
        collector.effective_revision = collector.desired_revision;
        collector.status = "converged";
        collector.updated_at = ctx.nowIso();
      }
      return;
    }
    if (plan.tool === "telemetry.collector.route") {
      const routeInput = parseTelemetryRouteInput(input_data);
      const host = db.hosts.find(
        (entry) => entry.id === String(input_data["hostId"] ?? ""),
      );
      if (!host) throw new Error("TELEMETRY_ROUTE_INVALID");
      const collector = db.collectors.find(
        (entry) =>
          entry.resource_type === "host" && entry.resource_id === host.id,
      );
      if (!collector) throw new Error("TELEMETRY_ROUTE_INVALID");
      const now = ctx.nowIso();
      collector.route = mockTelemetryRoute(
        collector,
        routeInput,
        plan.enterprise_id,
        now,
        nextId(db, "route"),
      );
      collector.updated_at = now;
      host.telemetryRoute = routeInput.kind;
      host.updatedAt = now;
      return;
    }
    if (plan.tool === "telemetry.collector.upgrade") {
      const toVersion = String(input_data["toVersion"] ?? "v24.2.0");
      for (const collector of db.collectors) {
        if (collector.enterprise_id === plan.enterprise_id) {
          collector.distribution_version_id = toVersion;
          collector.desired_revision += 1;
          collector.effective_revision = collector.desired_revision;
          collector.status = "converged";
          collector.updated_at = ctx.nowIso();
        }
      }
      return;
    }
    if (plan.tool === "kubernetes.cluster.create") {
      db.clusters.push({
        id: nextId(db, "k8s"),
        enterpriseId: plan.enterprise_id,
        name: String(input_data["name"] ?? "new-cluster"),
        apiServer: String(
          input_data["api_server"] ?? input_data["apiServer"] ?? "",
        ),
        connectionMode:
          (input_data["connection_mode"] as
            "via_bastion" | "direct" | "in_cluster") ??
          (input_data["connectionMode"] as
            "via_bastion" | "direct" | "in_cluster") ??
          "direct",
        bastionScopeId:
          (input_data["bastion_scope_id"] as string | undefined) ??
          (input_data["bastionScopeId"] as string | undefined),
        credentialRef: String(
          input_data["credential_id"] ?? input_data["credentialRef"] ?? "",
        ),
        version: "v1.31.4",
        environment:
          (input_data["environment"] as
            "development" | "staging" | "production") ?? "production",
        labels: (input_data["labels"] as Record<string, string>) ?? {},
        connectionStatus: "connected",
        nodeCount: 0,
        readyNodeCount: 0,
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
        resourceVersion: 1,
      });
      return;
    }
    if (plan.tool === "kubernetes.cluster.update") {
      const cluster = db.clusters.find(
        (entry) => entry.id === String(input_data["id"] ?? ""),
      );
      if (!cluster) return;
      if (input_data["name"] !== undefined)
        cluster.name = String(input_data["name"]);
      if (input_data["environment"] !== undefined) {
        cluster.environment = input_data[
          "environment"
        ] as typeof cluster.environment;
      }
      if (input_data["labels"] !== undefined) {
        cluster.labels = input_data["labels"] as Record<string, string>;
      }
      cluster.resourceVersion = (cluster.resourceVersion ?? 1) + 1;
      cluster.updatedAt = ctx.nowIso();
      return;
    }
    if (plan.tool === "kubernetes.cluster.delete") {
      const id = String(input_data["id"] ?? "");
      db.clusters = db.clusters.filter((entry) => entry.id !== id);
      db.nodeBindings = db.nodeBindings.filter(
        (entry) => entry.kubernetes_cluster_id !== id,
      );
      return;
    }
    if (plan.tool === "bastion.scope.create") {
      const now = ctx.nowIso();
      const rootHostId = nextId(db, "host");
      const scope: MockBastionScope = {
        id: nextId(db, "scope"),
        enterpriseId: plan.enterprise_id,
        name: String(input_data["name"] ?? "new-bastion"),
        environment:
          (input_data["environment"] as
            "development" | "staging" | "production") ?? "production",
        labels: (input_data["labels"] as Record<string, string>) ?? {},
        status: "pending",
        connectorHostId: rootHostId,
        memberHostIds: [],
        createdAt: now,
        updatedAt: now,
        resourceVersion: 1,
        onboardingMode:
          (input_data["install_mode"] as MockBastionScope["onboardingMode"]) ??
          "command",
      };
      db.bastionScopes.push(scope);
      db.hosts.push({
        id: rootHostId,
        enterpriseId: plan.enterprise_id,
        name: scope.name,
        hostname: "",
        address:
          scope.onboardingMode === "command"
            ? `connector://${rootHostId}`
            : String(input_data["address"] ?? ""),
        port:
          scope.onboardingMode === "command"
            ? 1
            : Number(input_data["port"] ?? 22),
        platform: "linux",
        connectionMode: "connector_local",
        bastionScopeId: scope.id,
        environment: scope.environment,
        labels: { ...scope.labels },
        connectionStatus: "onboarding",
        collectorStatus: "not_installed",
        createdAt: now,
        updatedAt: now,
        resourceVersion: 1,
      });
      action.result_summary = `已创建堡垒机范围 ${scope.name}`;
      if (scope.onboardingMode === "command") {
        return createEnrollment(scope, "initial_registration", plan.created_by);
      }
      const operation = createConnectorInstallOperation(scope, input_data);
      scope.onboardingState = "installing";
      scope.onboardingOperationId = operation.id;
      return {
        resultKind: "connector_install_command",
        expiresAt: operation.expires_at,
        resource: {
          resource_type: "bastion_scope",
          resource_id: scope.id,
          version: 1,
        },
        operation,
      };
    }
    if (plan.tool === "bastion.scope.update") {
      const scope = db.bastionScopes.find(
        (entry) => entry.id === String(input_data["scope_id"] ?? ""),
      );
      if (!scope) return;
      if (input_data["name"] !== undefined)
        scope.name = String(input_data["name"]);
      if (input_data["environment"] !== undefined) {
        scope.environment = input_data[
          "environment"
        ] as typeof scope.environment;
      }
      if (input_data["labels"] !== undefined) {
        scope.labels = input_data["labels"] as Record<string, string>;
      }
      scope.resourceVersion = (scope.resourceVersion ?? 1) + 1;
      scope.updatedAt = ctx.nowIso();
      return;
    }
    if (plan.tool === "bastion.scope.delete") {
      const id = String(input_data["scope_id"] ?? "");
      db.bastionScopes = db.bastionScopes.filter((entry) => entry.id !== id);
      db.hosts = db.hosts.filter(
        (entry) =>
          entry.bastionScopeId !== id ||
          entry.connectionMode !== "connector_local",
      );
      action.result_summary = `已删除堡垒机范围 ${id}`;
      return;
    }
    if (plan.tool === "bastion.connector.replace") {
      const scope = db.bastionScopes.find(
        (entry) => entry.id === String(input_data["scope_id"] ?? ""),
      );
      if (!scope) return;
      const previous = db.connectors.find(
        (entry) => entry.id === scope.activeConnectorId,
      );
      if (previous) previous.status = "revoked";
      scope.activeConnectorId = undefined;
      scope.status = "pending";
      scope.resourceVersion = (scope.resourceVersion ?? 1) + 1;
      scope.updatedAt = ctx.nowIso();
      action.result_summary = `已生成堡垒机替换命令 ${scope.name}`;
      if ((scope.onboardingMode ?? "command") === "command") {
        return createEnrollment(
          scope,
          "connector_replacement",
          plan.created_by,
        );
      }
      const operation = createConnectorInstallOperation(scope, input_data);
      scope.onboardingState = "installing";
      scope.onboardingOperationId = operation.id;
      return {
        resultKind: "connector_install_command",
        expiresAt: operation.expires_at,
        resource: {
          resource_type: "bastion_scope",
          resource_id: scope.id,
          version: scope.resourceVersion ?? 1,
        },
        operation,
      };
    }
    if (plan.tool === "bastion.enrollment.rotate") {
      const scope = db.bastionScopes.find(
        (entry) => entry.id === String(input_data["scope_id"] ?? ""),
      );
      return scope
        ? createEnrollment(scope, "initial_registration", plan.created_by)
        : undefined;
    }
    if (plan.tool === "bastion.connector.install.retry") {
      const previous = db.connectorInstallOperations.find(
        (entry) => entry.id === String(input_data["operation_id"] ?? ""),
      );
      const scope = db.bastionScopes.find(
        (entry) => entry.id === previous?.bastion_scope_id,
      );
      if (!previous || !scope) return;
      const operation = createConnectorInstallOperation(
        scope,
        input_data,
        previous.id,
      );
      scope.onboardingState = "installing";
      scope.onboardingOperationId = operation.id;
      scope.onboardingErrorCode = undefined;
      return {
        resultKind: "connector_install_command",
        expiresAt: operation.expires_at,
        resource: {
          resource_type: "bastion_scope",
          resource_id: scope.id,
          version: scope.resourceVersion ?? 1,
        },
        operation,
      };
    }
    if (plan.tool === "connector.uninstall") {
      const connector = db.connectors.find(
        (entry) => entry.id === String(input_data["connector_id"] ?? ""),
      );
      if (!connector) return;
      connector.status = "uninstalled";
      connector.lastHeartbeatAt = ctx.nowIso();
      const scope = db.bastionScopes.find(
        (entry) => entry.id === connector.bastionScopeId,
      );
      if (scope) {
        scope.status = "uninstalled";
        scope.activeConnectorId = undefined;
        scope.resourceVersion = (scope.resourceVersion ?? 1) + 1;
        scope.updatedAt = ctx.nowIso();
      }
      action.result_summary = `已卸载 Connector ${connector.name}`;
      return;
    }
    if (plan.tool === "connector.cert.rotate") {
      const connector = db.connectors.find(
        (entry) => entry.id === input_data["connectorId"],
      );
      if (connector) {
        connector.certificateExpiresAt = new Date(
          Date.now() + 90 * 86_400_000,
        ).toISOString();
      }
    }
  }

  async function* streamReply(
    conversationId: string,
    text: string,
  ): AsyncIterable<ChatStreamEvent> {
    const messageId = nextId(db, "msg");
    yield { type: "message_start", messageId };

    const wantsHostCreate =
      /(新增|添加|add|create)/i.test(text) && /(主机|host)/i.test(text);
    const toolNames = wantsHostCreate
      ? ["host.resolve_context", "host.test_connection", "host.create.preview"]
      : ["telemetry.metrics.query"];
    const traces: ToolCallTrace[] = [];
    for (const toolName of toolNames) {
      const callId = nextId(db, "call");
      const startedAt = ctx.nowIso();
      yield {
        type: "tool_call",
        messageId,
        toolCall: { callId, toolName, status: "running", startedAt },
      };
      await ctx.pause(Math.min(ctx.options.delay ?? 200, 200));
      const durationMs = 300 + Math.round(Math.random() * 900);
      const summary =
        toolName === "host.create.preview" ? "预览已生成" : "调用成功";
      yield {
        type: "tool_call_update",
        messageId,
        callId,
        status: "success",
        durationMs,
        summary,
      };
      traces.push({
        callId,
        toolName,
        status: "success",
        startedAt,
        durationMs,
        summary,
      });
    }

    const answer = wantsHostCreate
      ? "已完成目标解析与连接测试，新增主机的不可变预览已生成。请在下方卡片确认或取消；确认后将创建主机并开始安装 OTLP 收集器。"
      : "已完成查询与分析。各项指标整体正常，近一小时未发现新的异常重启；详细数据已渲染在下方卡片中。";
    for (const delta of chunkText(answer)) {
      await ctx.pause(Math.min(ctx.options.delay ?? 60, 60));
      yield { type: "token", messageId, delta };
    }

    const cards: CardInstance[] = [];
    if (wantsHostCreate) {
      const ipMatch = /\b(?:\d{1,3}\.){3}\d{1,3}\b/.exec(text);
      const action = createPendingAction({
        tool: "host.create",
        title: "新增主机",
        conversation_id: conversationId,
        input_data: {
          name: `host-new-${db.hosts.length + 1}`,
          address: ipMatch?.[0] ?? "10.0.9.10",
          port: 22,
          connectionMode: "via_bastion",
          bastionScopeId: "scope-sh",
          connectorId: "conn-sh-01",
          credentialRef: "sec-ssh-prod",
          environment: "production",
        },
      });
      const card: CardInstance = {
        id: nextId(db, "cardi"),
        interactiveCardId: "cs-host-create-confirm",
        version: "3.0.1",
        title: "新增主机确认",
        pendingActionRef: action.action_ref,
        actionBindingId: nextId(db, "cab"),
      };
      cards.push(card);
      yield { type: "card", messageId, card };
    } else {
      const card: CardInstance = {
        id: nextId(db, "cardi"),
        interactiveCardId: "cs-host-overview",
        version: "1.4.0",
        title: "主机概览",
      };
      cards.push(card);
      yield { type: "card", messageId, card };
    }

    const conversation = db.conversations.find(
      (entry) => entry.id === conversationId,
    );
    const model = db.models.find(
      (entry) => entry.id === conversation?.selectedModelId,
    );
    const inputTokens = Math.max(1, Math.ceil(text.length / 3));
    const outputTokens = Math.max(1, Math.ceil(answer.length / 3));
    const message: ChatMessage = {
      id: messageId,
      conversationId,
      role: "assistant",
      content: answer,
      createdAt: ctx.nowIso(),
      toolCalls: traces,
      cards,
      modelId: model?.id,
      modelRevision: model?.revision,
      inputPricePerMillionSnapshot: model?.inputPricePerMillionTokens,
      outputPricePerMillionSnapshot: model?.outputPricePerMillionTokens,
      inputTokens,
      outputTokens,
    };
    db.messages.push(message);
    if (conversation) conversation.lastMessageAt = message.createdAt;
    const enterpriseUser = db.enterpriseUsers.find(
      (entry) => entry.userId === ctx.actor().id,
    );
    if (model && enterpriseUser) {
      db.usagePoints.push({
        date: message.createdAt.slice(0, 10),
        modelId: model.id,
        departmentId: enterpriseUser.departmentId,
        userId: enterpriseUser.userId,
        inputTokens,
        outputTokens,
        requestCount: 1,
        successCount: 1,
        errorCount: 0,
        toolCallingFailures: 0,
        structuredOutputFailures: 0,
        avgLatencyMs: 620,
        inputPricePerMillionSnapshot: model.inputPricePerMillionTokens,
        outputPricePerMillionSnapshot: model.outputPricePerMillionTokens,
        amount:
          (inputTokens / 1_000_000) * model.inputPricePerMillionTokens +
          (outputTokens / 1_000_000) * model.outputPricePerMillionTokens,
      });
    }
    ctx.save();
    yield { type: "message_done", message };
  }

  return {
    createPendingAction,
    getAction,
    ensureNotExpired,
    commitResourceAction,
    startExecution,
    streamReply,
  };
}
