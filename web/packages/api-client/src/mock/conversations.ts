import type { ArgusApiClient } from "../client";
import type {
  AgentEvent,
  Conversation,
  ConversationEvent,
  StreamEventEnvelope,
} from "../generated/contracts";
import type { MockConversationRecord } from "./internal-types";
import type { MockChatMessage as ChatMessage } from "./chat-types";
import type { MockChatStreamEvent } from "./chat-types";
import type { MockContext } from "./context";
import { nextId } from "./store";

function serializeMessage(message: ChatMessage): Record<string, unknown> {
  return {
    message_id: message.id,
    conversation_id: message.conversationId,
    role: message.role,
    content: message.content,
    created_at: message.createdAt,
    ...(message.modelId ? { model_id: message.modelId } : {}),
    ...(message.modelRevision ? { model_revision: message.modelRevision } : {}),
    ...(message.inputPricePerMillionSnapshot !== undefined
      ? {
          input_price_per_million_snapshot:
            message.inputPricePerMillionSnapshot,
        }
      : {}),
    ...(message.outputPricePerMillionSnapshot !== undefined
      ? {
          output_price_per_million_snapshot:
            message.outputPricePerMillionSnapshot,
        }
      : {}),
    ...(message.inputTokens !== undefined
      ? { input_tokens: message.inputTokens }
      : {}),
    ...(message.outputTokens !== undefined
      ? { output_tokens: message.outputTokens }
      : {}),
    ...(message.createdInteractiveCardId
      ? { created_interactive_card_id: message.createdInteractiveCardId }
      : {}),
    tool_calls: (message.toolCalls ?? []).map((call) => ({
      call_id: call.callId,
      tool_name: call.toolName,
      status: call.status,
      ...(call.summary ? { summary: call.summary } : {}),
      ...(call.durationMs !== undefined
        ? { duration_ms: call.durationMs }
        : {}),
      started_at: call.startedAt,
    })),
    cards: (message.cards ?? []).map((card) => ({
      card_instance_id: card.id,
      interactive_card_id: card.interactiveCardId,
      version: card.version,
      ...(card.title ? { title: card.title } : {}),
      ...(card.pendingActionRef
        ? { pending_action_ref: card.pendingActionRef }
        : {}),
      ...(card.actionBindingId
        ? { action_binding_id: card.actionBindingId }
        : {}),
    })),
    ...(message.event
      ? {
          card_action_result: {
            type: message.event.type,
            origin: message.event.origin,
            actor_user_id: message.event.actorUserId,
            card_instance_id: message.event.cardInstanceId,
            action: message.event.action,
            tool: message.event.tool,
            status: message.event.status,
            ...(message.event.resultRef
              ? { result_ref: message.event.resultRef }
              : {}),
          },
        }
      : {}),
  };
}

function conversationEvent(
  enterpriseId: string,
  message: ChatMessage,
  sequence: number,
): ConversationEvent {
  return {
    schema_version: "argus.conversation_event/v1",
    event_id: `conversation-${message.id}`,
    sequence,
    enterprise_id: enterpriseId,
    conversation_id: message.conversationId,
    event_type: message.event
      ? "card_action_result"
      : message.role === "user"
        ? "user_message"
        : "assistant_message",
    actor_type: message.role === "user" ? "user" : "model",
    occurred_at: message.createdAt,
    content_hash: `mock-content-${message.id}`.padEnd(64, "0"),
    data_classification: "internal",
    payload: { message: serializeMessage(message) },
  };
}

function publicConversation(value: MockConversationRecord): Conversation {
  return {
    id: value.id,
    title: value.title,
    selected_model_id: value.selectedModelId,
    status: value.status,
    version: value.version ?? 1,
    created_at: value.createdAt,
    updated_at: value.lastMessageAt,
  };
}

function agentEvent(
  runId: string,
  sequence: number,
  source: MockChatStreamEvent,
  occurredAt: string,
): AgentEvent {
  let event_type: AgentEvent["event_type"] = "message_delta";
  let payload: Record<string, unknown> = {};
  if (source.type === "message_start") {
    event_type = "message_started";
    payload = { message_id: source.messageId };
  } else if (source.type === "token") {
    payload = { message_id: source.messageId, delta: source.delta };
  } else if (source.type === "tool_call") {
    event_type = "tool_call_started";
    payload = {
      message_id: source.messageId,
      tool_call_id: source.toolCall.callId,
      tool_name: source.toolCall.toolName,
      started_at: source.toolCall.startedAt,
    };
  } else if (source.type === "tool_call_update") {
    event_type = "tool_call_completed";
    payload = {
      message_id: source.messageId,
      tool_call_id: source.callId,
      status: source.status,
      duration_ms: source.durationMs,
      ...(source.summary ? { summary: source.summary } : {}),
    };
  } else if (source.type === "card") {
    event_type = source.card.pendingActionRef
      ? "pending_action_created"
      : "message_delta";
    payload = {
      message_id: source.messageId,
      card: {
        card_instance_id: source.card.id,
        interactive_card_id: source.card.interactiveCardId,
        version: source.card.version,
        ...(source.card.title ? { title: source.card.title } : {}),
        ...(source.card.pendingActionRef
          ? { pending_action_ref: source.card.pendingActionRef }
          : {}),
        ...(source.card.actionBindingId
          ? { action_binding_id: source.card.actionBindingId }
          : {}),
      },
      ...(source.card.pendingActionRef
        ? { action_ref: source.card.pendingActionRef }
        : {}),
    };
  } else if (source.type === "interactive_card_created") {
    payload = {
      message_id: source.messageId,
      created_interactive_card_id: source.interactiveCardId,
    };
  } else if (source.type === "card_action_result") {
    event_type = "message_completed";
    payload = { message_id: source.messageId };
  } else if (source.type === "message_done") {
    event_type = "message_completed";
    payload = {
      message_id: source.message.id,
      message: serializeMessage(source.message),
    };
  } else if (source.type === "error") {
    event_type = "run_failed";
    payload = { error_code: "MOCK_STREAM_FAILED", message: source.message };
  }
  return {
    schema_version: "argus.agent_event/v1",
    event_id: `${runId}-event-${sequence}`,
    sequence,
    run_id: runId,
    event_type,
    occurred_at: occurredAt,
    payload,
  };
}

async function* streamEnvelopes(
  source: AsyncIterable<MockChatStreamEvent>,
  signal?: AbortSignal,
): AsyncGenerator<StreamEventEnvelope> {
  const runId = `run-${crypto.randomUUID()}`;
  let sequence = 0;
  for await (const event of source) {
    if (signal?.aborted) return;
    sequence += 1;
    const agent = agentEvent(runId, sequence, event, new Date().toISOString());
    yield {
      schema_version: "argus.stream_event/v1",
      event_id: agent.event_id,
      sequence,
      event_type: "agent_event",
      occurred_at: agent.occurred_at,
      terminal: false,
      resume_cursor: agent.event_id,
      data: agent,
    };
    if (event.type === "message_done") {
      sequence += 1;
      const completed: AgentEvent = {
        schema_version: "argus.agent_event/v1",
        event_id: `${runId}-event-${sequence}`,
        sequence,
        run_id: runId,
        event_type: "run_completed",
        occurred_at: new Date().toISOString(),
        payload: { stop_reason: "completed" },
      };
      yield {
        schema_version: "argus.stream_event/v1",
        event_id: completed.event_id,
        sequence,
        event_type: "agent_event",
        occurred_at: completed.occurred_at,
        terminal: true,
        close_reason: "normal",
        resume_cursor: completed.event_id,
        data: completed,
      };
      return;
    }
  }
}

/** Conversations and streaming assistant replies. */
export function createConversationsDomain(
  ctx: MockContext,
): ArgusApiClient["conversations"] {
  const { db } = ctx;

  return {
    async list(query) {
      await ctx.pause();
      const items = db.conversations
        .filter((entry) => entry.enterpriseId === ctx.enterpriseId())
        .sort((a, b) => b.lastMessageAt.localeCompare(a.lastMessageAt));
      return ctx.paginate(items.map(publicConversation), query);
    },
    async get(id) {
      await ctx.pause();
      return publicConversation(
        ctx.mustFind(
          db.conversations,
          (entry) => entry.id === id,
          "conversation",
        ),
      );
    },
    async create(input) {
      await ctx.pause();
      const selectedModelId =
        input?.selected_model_id ??
        db.models.find(
          (model) => model.enterpriseId === ctx.enterpriseId() && model.enabled,
        )?.id;
      if (!selectedModelId) throw new Error("no available model");
      const conversation = {
        id: nextId(db, "conv"),
        enterpriseId: ctx.enterpriseId(),
        title: input?.title ?? "新的会话",
        createdBy: ctx.actor().id,
        selectedModelId,
        status: "active" as const,
        lastMessageAt: ctx.nowIso(),
        createdAt: ctx.nowIso(),
        version: 1,
      };
      db.conversations.unshift(conversation);
      ctx.save();
      return publicConversation(conversation);
    },
    async archive(id) {
      await ctx.pause();
      const conversation = ctx.mustFind(
        db.conversations,
        (entry) => entry.id === id,
        "conversation",
      );
      conversation.status = "archived";
      conversation.version = (conversation.version ?? 1) + 1;
      ctx.save();
      return publicConversation(conversation);
    },
    async listEvents(conversationId) {
      await ctx.pause();
      return db.messages
        .filter((entry) => entry.conversationId === conversationId)
        .map((message, index) =>
          conversationEvent(ctx.enterpriseId(), message, index + 1),
        );
    },
    async updateModel(id, modelId) {
      await ctx.pause();
      const conversation = ctx.mustFind(
        db.conversations,
        (entry) => entry.id === id,
        "conversation",
      );
      const model = ctx.mustFind(
        db.models,
        (entry) =>
          entry.id === modelId && entry.enterpriseId === ctx.enterpriseId(),
        "AI model",
      );
      if (!model.enabled || model.healthStatus !== "healthy") {
        throw new Error("model unavailable");
      }
      conversation.selectedModelId = model.id;
      conversation.version = (conversation.version ?? 1) + 1;
      ctx.save();
      return publicConversation(conversation);
    },
    sendMessage(conversationId, input, options) {
      const conversation = ctx.mustFind(
        db.conversations,
        (entry) => entry.id === conversationId,
        "conversation",
      );
      const enterpriseUser = db.enterpriseUsers.find(
        (entry) => entry.userId === ctx.actor().id,
      );
      if (!enterpriseUser)
        throw new Error("enterprise enterpriseUser required");
      const month = ctx.nowIso().slice(0, 7);
      const points = db.usagePoints.filter(
        (point) =>
          point.modelId === conversation.selectedModelId &&
          point.date.startsWith(month),
      );
      const departmentUsed = points
        .filter((point) => point.departmentId === enterpriseUser.departmentId)
        .reduce((sum, point) => sum + point.amount, 0);
      const userUsed = points
        .filter((point) => point.userId === enterpriseUser.userId)
        .reduce((sum, point) => sum + point.amount, 0);
      const departmentQuota = db.modelQuotas.find(
        (quota) =>
          quota.modelId === conversation.selectedModelId &&
          quota.subjectType === "department" &&
          quota.subjectId === enterpriseUser.departmentId,
      );
      const userQuota = db.modelQuotas.find(
        (quota) =>
          quota.modelId === conversation.selectedModelId &&
          quota.subjectType === "user" &&
          quota.subjectId === enterpriseUser.userId,
      );
      if (departmentQuota && departmentUsed >= departmentQuota.monthlyAmount) {
        throw new Error("department quota exhausted");
      }
      if (userQuota && userUsed >= userQuota.monthlyAmount) {
        throw new Error("user quota exhausted");
      }
      const userMessage: ChatMessage = {
        id: nextId(db, "msg"),
        conversationId,
        role: "user",
        content: input.content,
        createdAt: ctx.nowIso(),
        modelId: conversation.selectedModelId,
      };
      db.messages.push(userMessage);
      conversation.lastMessageAt = userMessage.createdAt;
      ctx.save();
      if (options?.mock_intent === "interactive_card.create") {
        const createdAt = ctx.nowIso();
        const name =
          input.content.replace(/^\s*\/?\s*/, "").slice(0, 28) || "新交互卡片";
        const card = {
          id: nextId(db, "card"),
          enterpriseId: ctx.enterpriseId(),
          source: "enterprise" as const,
          slug: `generated-${Date.now()}`,
          name,
          description: "由 AI 根据会话需求生成",
          version: "0.1.0",
          revision: 1,
          lifecycle: "draft" as const,
          enabled: false,
          htmlTemplate:
            '<article style="padding:16px;font-family:system-ui"><h3 data-slot="title"></h3><div data-slot="items"></div></article>',
          slots: [
            {
              name: "title",
              type: "string" as const,
              required: true,
              aiGenerated: true,
            },
            { name: "items", type: "array" as const, required: true },
          ],
          bindings: [],
          demoData: { title: name, items: [{ name: "Demo", status: "ok" }] },
          validation: {
            valid: false,
            checkedAt: createdAt,
            passedScenarios: [],
            issues: [
              {
                code: "BINDING_REQUIRED",
                message: "required slot has no binding",
                slot: "items",
              },
            ],
          },
          createdBy: ctx.actor().id,
          createdAt,
          updatedAt: createdAt,
        };
        db.interactiveCards.push(card);
        ctx.save();
        return streamEnvelopes(
          (async function* () {
            const messageId = nextId(db, "msg");
            const content = `已创建“${card.name}”草稿。卡片默认禁用，请前往交互卡片详情完成绑定、验证并启用。`;
            yield { type: "message_start" as const, messageId };
            yield { type: "token" as const, messageId, delta: content };
            yield {
              type: "interactive_card_created" as const,
              messageId,
              interactiveCardId: card.id,
            };
            const message: ChatMessage = {
              id: messageId,
              conversationId,
              role: "assistant",
              content,
              modelId: conversation.selectedModelId,
              modelRevision: db.models.find(
                (entry) => entry.id === conversation.selectedModelId,
              )?.revision,
              inputPricePerMillionSnapshot: db.models.find(
                (entry) => entry.id === conversation.selectedModelId,
              )?.inputPricePerMillionTokens,
              outputPricePerMillionSnapshot: db.models.find(
                (entry) => entry.id === conversation.selectedModelId,
              )?.outputPricePerMillionTokens,
              createdInteractiveCardId: card.id,
              createdAt: ctx.nowIso(),
            };
            db.messages.push(message);
            ctx.save();
            yield { type: "message_done" as const, message };
          })(),
          options.signal,
        );
      }
      return streamEnvelopes(
        ctx.streamReply(conversationId, input.content),
        options?.signal,
      );
    },
    subscribe(conversationId, listener) {
      return ctx.emitter.on(`chat:${conversationId}`, listener);
    },
  };
}
