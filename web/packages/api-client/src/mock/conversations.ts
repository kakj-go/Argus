import type { ArgusApiClient } from "../client";
import type { ChatMessage } from "../types";
import type { MockContext } from "./context";
import { nextId } from "./store";

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
      return ctx.paginate(items, query);
    },
    async get(id) {
      await ctx.pause();
      return ctx.mustFind(
        db.conversations,
        (entry) => entry.id === id,
        "conversation",
      );
    },
    async create(input) {
      await ctx.pause();
      const selectedModelId =
        input?.selectedModelId ??
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
      };
      db.conversations.unshift(conversation);
      ctx.save();
      return conversation;
    },
    async archive(id) {
      await ctx.pause();
      const conversation = ctx.mustFind(
        db.conversations,
        (entry) => entry.id === id,
        "conversation",
      );
      conversation.status = "archived";
      ctx.save();
      return conversation;
    },
    async listMessages(conversationId) {
      await ctx.pause();
      return db.messages.filter(
        (entry) => entry.conversationId === conversationId,
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
        (entry) => entry.id === modelId && entry.enterpriseId === ctx.enterpriseId(),
        "AI model",
      );
      if (!model.enabled || model.healthStatus !== "healthy") {
        throw new Error("model unavailable");
      }
      conversation.selectedModelId = model.id;
      ctx.save();
      return conversation;
    },
    sendMessage(conversationId, input) {
      const conversation = ctx.mustFind(
        db.conversations,
        (entry) => entry.id === conversationId,
        "conversation",
      );
      const membership = db.memberships.find(
        (entry) => entry.userId === ctx.actor().id,
      );
      if (!membership) throw new Error("enterprise membership required");
      const month = ctx.nowIso().slice(0, 7);
      const points = db.usagePoints.filter(
        (point) =>
          point.modelId === conversation.selectedModelId &&
          point.date.startsWith(month),
      );
      const departmentUsed = points
        .filter((point) => point.departmentId === membership.departmentId)
        .reduce((sum, point) => sum + point.amount, 0);
      const userUsed = points
        .filter((point) => point.userId === membership.userId)
        .reduce((sum, point) => sum + point.amount, 0);
      const departmentQuota = db.modelQuotas.find(
        (quota) =>
          quota.modelId === conversation.selectedModelId &&
          quota.subjectType === "department" &&
          quota.subjectId === membership.departmentId,
      );
      const userQuota = db.modelQuotas.find(
        (quota) =>
          quota.modelId === conversation.selectedModelId &&
          quota.subjectType === "user" &&
          quota.subjectId === membership.userId,
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
        content: input.text,
        createdAt: ctx.nowIso(),
        modelId: conversation.selectedModelId,
      };
      db.messages.push(userMessage);
      conversation.lastMessageAt = userMessage.createdAt;
      ctx.save();
      if (input.command?.type === "interactive_card.create") {
        const createdAt = ctx.nowIso();
        const name = input.text.replace(/^\s*\/?\s*/, "").slice(0, 28) || "新交互卡片";
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
            { name: "title", type: "string" as const, required: true, aiGenerated: true },
            { name: "items", type: "array" as const, required: true },
          ],
          bindings: [],
          demoData: { title: name, items: [{ name: "Demo", status: "ok" }] },
          validation: {
            valid: false,
            checkedAt: createdAt,
            passedScenarios: [],
            issues: [{ code: "BINDING_REQUIRED", message: "required slot has no binding", slot: "items" }],
          },
          createdBy: ctx.actor().id,
          createdAt,
          updatedAt: createdAt,
        };
        db.interactiveCards.push(card);
        ctx.save();
        return (async function* () {
          const messageId = nextId(db, "msg");
          yield { type: "message_start" as const, messageId };
          yield {
            type: "token" as const,
            messageId,
            delta: `已创建“${card.name}”草稿。卡片默认禁用，请前往交互卡片详情完成绑定、验证并启用。`,
          };
          yield { type: "interactive_card_created" as const, messageId, interactiveCardId: card.id };
          const message: ChatMessage = {
            id: messageId,
            conversationId,
            role: "assistant",
            content: `已创建“${card.name}”草稿。卡片默认禁用，请前往交互卡片详情完成绑定、验证并启用。`,
            modelId: conversation.selectedModelId,
            modelRevision: db.models.find((entry) => entry.id === conversation.selectedModelId)?.revision,
            inputPricePerMillionSnapshot: db.models.find((entry) => entry.id === conversation.selectedModelId)?.inputPricePerMillionTokens,
            outputPricePerMillionSnapshot: db.models.find((entry) => entry.id === conversation.selectedModelId)?.outputPricePerMillionTokens,
            createdInteractiveCardId: card.id,
            createdAt: ctx.nowIso(),
          };
          db.messages.push(message);
          ctx.save();
          yield { type: "message_done" as const, message };
        })();
      }
      return ctx.streamReply(conversationId, input.text);
    },
    subscribe(conversationId, listener) {
      return ctx.emitter.on(`chat:${conversationId}`, listener);
    },
  };
}
