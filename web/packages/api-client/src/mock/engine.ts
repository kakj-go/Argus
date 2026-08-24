import type {
  ConfirmActionResult,
  PendingActionPublic,
  RiskLevel,
} from "../types";
import type { TaskViewModel, TaskStep } from "../provisional";
import type {
  MockCardInstance as CardInstance,
  MockChatMessage as ChatMessage,
  MockChatStreamEvent as ChatStreamEvent,
  MockToolCallTrace as ToolCallTrace,
} from "./chat-types";
import type { BaseContext, Engine } from "./context";
import { nextId } from "./store";
import type {
  ConnectorEnrollmentPurpose,
  MockBastionScope,
  MockHost,
} from "./resource-models";

const IMMEDIATE_RESOURCE_TOOLS = new Set([
  "host.create",
  "host.update",
  "host.delete",
  "kubernetes.cluster.create",
  "kubernetes.cluster.update",
  "kubernetes.cluster.delete",
  "bastion.scope.create",
  "bastion.scope.update",
  "bastion.scope.delete",
  "bastion.connector.replace",
]);

const TOOL_RISK: Record<string, RiskLevel> = {
  "host.create": "write",
  "host.update": "write",
  "host.delete": "dangerous",
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
    const enrollment = applySideEffect(action);
    action.status = "succeeded";
    action.result_summary ??= "操作已完成";
    action.updated_at = ctx.nowIso();
    ctx.audit(`${plan.tool}.commit`, {
      resourceType: "pending_action",
      resourceId: action.action_ref,
      summary: action.result_summary,
    });
    ctx.save();
    return enrollment
      ? {
          pending_action: action,
          one_time_result: {
            schema_version: "argus.action_one_time_result/v1",
            execution_id: `mock-${action.action_ref}`,
            result_kind: "connector_enrollment",
            enrollment,
            expires_at: enrollment.expires_at,
          },
        }
      : { pending_action: action };
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

  function finishTask(task: TaskViewModel, action: PendingActionPublic): void {
    task.status = "succeeded";
    task.finishedAt = ctx.nowIso();
    task.progress = 100;
    action.status = "succeeded";
    action.result_summary = "执行成功";
    action.updated_at = ctx.nowIso();
    applySideEffect(action);
    const plan = db.actionPlans[action.action_ref];
    ctx.audit(`${plan?.tool ?? "pending_action"}.commit`, {
      resourceType: "task",
      resourceId: task.id,
      summary: `${action.title} 执行成功`,
    });
    ctx.save();
    ctx.emitTask(task);
  }

  function createEnrollment(
    scope: MockBastionScope,
    purpose: ConnectorEnrollmentPurpose,
    createdBy: string,
  ): NonNullable<ConfirmActionResult["one_time_result"]>["enrollment"] {
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
    const installCommand = `curl -fsSL https://argus.example/install.sh | sh -s -- --token ${token}`;
    const enrollment = {
      id: enrollmentId,
      enterpriseId: scope.enterpriseId,
      bastionScopeId: scope.id,
      purpose,
      status: "active" as const,
      token,
      installCommand,
      expiresAt,
      remainingUses: 1,
      createdBy,
      createdAt: ctx.nowIso(),
    };
    db.enrollmentTokens.push(enrollment);
    scope.registrationToken = enrollment;
    return {
      enrollment_id: enrollmentId,
      install_command: installCommand,
      expires_at: expiresAt,
    };
  }

  function applySideEffect(
    action: PendingActionPublic,
  ):
    | NonNullable<ConfirmActionResult["one_time_result"]>["enrollment"]
    | undefined {
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
      return;
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
      const existing = db.collectors.find(
        (entry) =>
          entry.resource_type === "host" && entry.resource_id === hostId,
      );
      if (existing) {
        existing.status = "converged";
        existing.effective_revision = existing.desired_revision;
        existing.updated_at = ctx.nowIso();
      } else {
        const now = ctx.nowIso();
        db.collectors.push({
          id: nextId(db, "col"),
          enterprise_id: plan.enterprise_id,
          resource_type: "host",
          resource_id: hostId,
          distribution_version_id: String(
            input_data["distribution_version_id"] ?? "dist-linux-arm64-v1",
          ),
          platform: "linux_arm64",
          role: "leaf",
          desired_revision: 1,
          effective_revision: 1,
          status: "converged",
          version: 1,
          created_at: now,
          updated_at: now,
        });
      }
      const host = db.hosts.find((entry) => entry.id === hostId);
      if (host) {
        host.collectorStatus = "converged";
        host.telemetryRoute = String(
          input_data["route_kind"] ??
            input_data["telemetryRoute"] ??
            "direct_argus",
        );
        host.updatedAt = ctx.nowIso();
      }
      return;
    }
    if (plan.tool === "telemetry.kubernetes.install") {
      const now = ctx.nowIso();
      db.collectors.push({
        id: nextId(db, "col"),
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
        effective_revision: 1,
        status: "converged",
        version: 1,
        created_at: now,
        updated_at: now,
      });
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
      const host = db.hosts.find(
        (entry) => entry.id === String(input_data["hostId"] ?? ""),
      );
      if (host) {
        host.telemetryRoute = String(input_data["route"] ?? "direct_argus");
        host.updatedAt = ctx.nowIso();
      }
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
      const scope: MockBastionScope = {
        id: nextId(db, "scope"),
        enterpriseId: plan.enterprise_id,
        name: String(input_data["name"] ?? "new-bastion"),
        environment:
          (input_data["environment"] as
            "development" | "staging" | "production") ?? "production",
        labels: (input_data["labels"] as Record<string, string>) ?? {},
        status: "pending",
        memberHostIds: [],
        createdAt: now,
        updatedAt: now,
        resourceVersion: 1,
      };
      db.bastionScopes.push(scope);
      const enrollment = createEnrollment(
        scope,
        "initial_registration",
        plan.created_by,
      );
      action.result_summary = `已创建堡垒机范围 ${scope.name}`;
      return enrollment;
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
      if (previous) previous.status = "offline";
      scope.activeConnectorId = undefined;
      scope.status = "pending";
      scope.resourceVersion = (scope.resourceVersion ?? 1) + 1;
      scope.updatedAt = ctx.nowIso();
      const enrollment = createEnrollment(
        scope,
        "connector_replacement",
        plan.created_by,
      );
      action.result_summary = `已生成堡垒机替换命令 ${scope.name}`;
      return enrollment;
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
