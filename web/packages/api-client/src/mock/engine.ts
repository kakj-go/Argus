import type {
  CardInstance,
  ChatMessage,
  ChatStreamEvent,
  Host,
  PendingAction,
  RiskLevel,
  Task,
  TaskStep,
  ToolCallTrace,
} from "../types";
import type { BaseContext, Engine } from "./context";
import { nextId } from "./store";

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

const TOOL_TASK_TYPE: Record<string, Task["type"]> = {
  "host.create": "host_onboard",
  "telemetry.host.install": "collector_install",
  "telemetry.kubernetes.install": "collector_install",
  "telemetry.collector.upgrade": "collector_upgrade",
  "kubernetes.cluster.create": "kubernetes_change",
  "kubernetes.workload.restart": "kubernetes_change",
  "connector.cert.rotate": "certificate_rotation",
};

function summarize(tool: string, params: Record<string, unknown>): string {
  const name = params["name"] ?? params["workload"] ?? params["hostId"] ?? "";
  return `${tool} ${String(name)}`.trim();
}

function buildDiff(
  tool: string,
  params: Record<string, unknown>,
): PendingAction["diff"] {
  if (tool === "host.create") {
    return [
      { kind: "add", text: `+ resource.host ${String(params["name"] ?? "")}` },
      { kind: "add", text: "+ telemetry.collector v24.1.3" },
      { kind: "note", text: "无端口与防火墙变更" },
    ];
  }
  return [{ kind: "change", text: `~ ${summarize(tool, params)}` }];
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
  ): PendingAction {
    const riskLevel = TOOL_RISK[input.tool] ?? "write";
    const policy = db.approvalPolicies.find(
      (candidate) =>
        candidate.enterpriseId === ctx.enterpriseId() &&
        candidate.enabled &&
        candidate.matchRiskLevels.includes(riskLevel),
    );
    const who = ctx.actor();
    const action: PendingAction = {
      id: nextId(db, "pa"),
      actionRef: "",
      enterpriseId: ctx.enterpriseId(),
      tool: input.tool,
      title: input.title ?? input.tool,
      summary: summarize(input.tool, input.params),
      riskLevel,
      preview: { ...input.params },
      params: { ...input.params },
      diff: buildDiff(input.tool, input.params),
      planHash: `sha256:${Math.random().toString(16).slice(2, 10)}…${Math.random()
        .toString(16)
        .slice(2, 6)}`,
      expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
      status: "awaiting_confirmation",
      createdBy: who.id,
      createdByName: who.displayName,
      conversationId: input.conversationId,
      approval: policy
        ? {
            required: true,
            policyId: policy.id,
            policyName: policy.name,
            minApprovers: policy.minApprovers,
            approverRoleIds: [...policy.approverRoleIds],
            separationOfDuty: policy.separationOfDuty,
            decisions: [],
          }
        : undefined,
      createdAt: ctx.nowIso(),
      updatedAt: ctx.nowIso(),
    };
    action.actionRef = `pa_ref_${action.id}`;
    db.pendingActions.unshift(action);
    ctx.audit(`${input.tool}.preview`, {
      resourceType: "pending_action",
      resourceId: action.id,
      summary: action.title,
    });
    ctx.save();
    return action;
  }

  function getAction(actionRef: string): PendingAction {
    return ctx.mustFind(
      db.pendingActions,
      (entry) => entry.actionRef === actionRef || entry.id === actionRef,
      "pending action",
    );
  }

  function ensureNotExpired(action: PendingAction): void {
    if (
      (action.status === "awaiting_confirmation" ||
        action.status === "awaiting_approval") &&
      Date.parse(action.expiresAt) < Date.now()
    ) {
      action.status = "expired";
      action.updatedAt = ctx.nowIso();
      ctx.save();
      throw new Error("pending action expired; preview again");
    }
  }

  function startExecution(action: PendingAction): Task {
    action.status = "executing";
    action.updatedAt = ctx.nowIso();
    const who = ctx.actor();
    const stepNames = TOOL_STEPS[action.tool] ?? ["执行计划", "验证结果"];
    const steps: TaskStep[] = stepNames.map((name, index) => ({
      id: `s${index + 1}`,
      name,
      status: "pending",
    }));
    const task: Task = {
      id: nextId(db, "task"),
      enterpriseId: action.enterpriseId,
      type: TOOL_TASK_TYPE[action.tool] ?? "generic",
      title: action.title,
      status: "running",
      origin: action.conversationId ? "admin_chatbox" : "admin_ui",
      createdBy: who.id,
      createdByName: who.displayName,
      relatedResources: [],
      steps,
      logs: [],
      pendingActionId: action.id,
      progress: 0,
      startedAt: ctx.nowIso(),
      createdAt: ctx.nowIso(),
    };
    action.taskId = task.id;
    db.tasks.unshift(task);
    ctx.save();
    ctx.emitTask(task);
    scheduleSteps(task, action);
    return task;
  }

  function scheduleSteps(task: Task, action: PendingAction): void {
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

  function finishTask(task: Task, action: PendingAction): void {
    task.status = "succeeded";
    task.finishedAt = ctx.nowIso();
    task.progress = 100;
    action.status = "succeeded";
    action.resultSummary = "执行成功";
    action.updatedAt = ctx.nowIso();
    applySideEffect(action);
    ctx.audit(`${action.tool}.commit`, {
      resourceType: "task",
      resourceId: task.id,
      summary: `${action.title} 执行成功`,
    });
    ctx.save();
    ctx.emitTask(task);
  }

  function applySideEffect(action: PendingAction): void {
    const params = action.params;
    if (action.tool === "host.create") {
      const host: Host = {
        id: nextId(db, "host"),
        enterpriseId: action.enterpriseId,
        name: String(params["name"] ?? "new-host"),
        hostname: String(params["hostname"] ?? params["name"] ?? "new-host"),
        address: String(params["address"] ?? ""),
        port: Number(params["port"] ?? 22),
        platform: (params["platform"] as Host["platform"]) ?? "linux",
        connectionMode:
          (params["connectionMode"] as Host["connectionMode"]) ?? "direct_ssh",
        bastionScopeId: params["bastionScopeId"] as string | undefined,
        connectorId: params["connectorId"] as string | undefined,
        credentialRef: params["credentialRef"] as string | undefined,
        environment:
          (params["environment"] as Host["environment"]) ?? "production",
        tags: (params["tags"] as Record<string, string>) ?? {},
        ownerTeamId: params["ownerTeamId"] as string | undefined,
        connectionStatus: "online",
        collectorStatus: "installing",
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
      };
      db.hosts.push(host);
      const scope = db.bastionScopes.find(
        (entry) => entry.id === host.bastionScopeId,
      );
      if (scope) {
        scope.memberHostIds.push(host.id);
        scope.updatedAt = ctx.nowIso();
      }
      action.resultSummary = `已创建主机 ${host.name}`;
      return;
    }
    if (action.tool === "telemetry.host.install") {
      const hostId = String(params["hostId"] ?? "");
      const existing = db.collectors.find(
        (entry) => entry.targetType === "host" && entry.targetId === hostId,
      );
      if (existing) {
        existing.status = "converged";
        existing.progress = 100;
        existing.updatedAt = ctx.nowIso();
      } else {
        db.collectors.push({
          id: nextId(db, "col"),
          enterpriseId: action.enterpriseId,
          targetType: "host",
          targetId: hostId,
          role: "leaf",
          profile: String(params["profile"] ?? "host-basic"),
          version: "v24.1.3",
          desiredRevision: 1,
          effectiveRevision: 1,
          status: "converged",
          progress: 100,
          updatedAt: ctx.nowIso(),
        });
      }
      const host = db.hosts.find((entry) => entry.id === hostId);
      if (host) {
        host.collectorStatus = "converged";
        host.telemetryRoute = String(
          params["telemetryRoute"] ?? "direct_argus",
        );
        host.updatedAt = ctx.nowIso();
      }
      return;
    }
    if (action.tool === "telemetry.kubernetes.install") {
      db.collectors.push({
        id: nextId(db, "col"),
        enterpriseId: action.enterpriseId,
        targetType: "kubernetes_cluster",
        targetId: String(params["clusterId"] ?? ""),
        role: "daemonset",
        profile: String(params["profile"] ?? "k8s-daemonset"),
        version: "v24.1.3",
        desiredRevision: 1,
        effectiveRevision: 1,
        status: "converged",
        progress: 100,
        updatedAt: ctx.nowIso(),
      });
      return;
    }
    if (action.tool === "telemetry.collector.configure") {
      const hostId = String(params["hostId"] ?? "");
      const collector = db.collectors.find(
        (entry) => entry.targetType === "host" && entry.targetId === hostId,
      );
      if (collector) {
        collector.profile = String(params["profile"] ?? collector.profile);
        collector.desiredRevision += 1;
        collector.effectiveRevision = collector.desiredRevision;
        collector.status = "converged";
        collector.updatedAt = ctx.nowIso();
      }
      return;
    }
    if (action.tool === "telemetry.collector.route") {
      const host = db.hosts.find(
        (entry) => entry.id === String(params["hostId"] ?? ""),
      );
      if (host) {
        host.telemetryRoute = String(params["route"] ?? "direct_argus");
        host.updatedAt = ctx.nowIso();
      }
      return;
    }
    if (action.tool === "telemetry.collector.upgrade") {
      const toVersion = String(params["toVersion"] ?? "v24.2.0");
      for (const collector of db.collectors) {
        if (collector.enterpriseId === action.enterpriseId) {
          collector.version = toVersion;
          collector.desiredRevision += 1;
          collector.effectiveRevision = collector.desiredRevision;
          collector.status = "converged";
          collector.updatedAt = ctx.nowIso();
        }
      }
      return;
    }
    if (action.tool === "kubernetes.cluster.create") {
      db.clusters.push({
        id: nextId(db, "k8s"),
        enterpriseId: action.enterpriseId,
        name: String(params["name"] ?? "new-cluster"),
        apiServer: String(params["apiServer"] ?? ""),
        connectionMode:
          (params["connectionMode"] as
            "via_bastion" | "direct" | "in_cluster") ?? "direct",
        bastionScopeId: params["bastionScopeId"] as string | undefined,
        credentialRef: String(params["credentialRef"] ?? ""),
        version: "v1.31.4",
        environment:
          (params["environment"] as "development" | "staging" | "production") ??
          "production",
        tags: (params["tags"] as Record<string, string>) ?? {},
        connectionStatus: "connected",
        nodeCount: 0,
        readyNodeCount: 0,
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
      });
      return;
    }
    if (action.tool === "connector.cert.rotate") {
      const connector = db.connectors.find(
        (entry) => entry.id === params["connectorId"],
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
        conversationId,
        params: {
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
        pendingActionRef: action.actionRef,
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
    const membership = db.memberships.find(
      (entry) => entry.userId === ctx.actor().id,
    );
    if (model && membership) {
      db.usagePoints.push({
        date: message.createdAt.slice(0, 10),
        modelId: model.id,
        departmentId: membership.departmentId,
        userId: membership.userId,
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
    startExecution,
    streamReply,
  };
}
