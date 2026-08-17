import type { ArgusApiClient } from "../client";
import {
  calculateModelAmount,
  type ModelCompatibilityResult,
  type ModelUsagePoint,
} from "../types";
import type { MockContext } from "./context";
import { resolvePermissions } from "./permissions";
import { nextId } from "./store";

function compatibility(now: string, ok: boolean): ModelCompatibilityResult {
  return {
    openAICompatible: ok,
    streaming: ok,
    toolCalling: ok,
    structuredOutput: ok,
    testedAt: now,
    diagnostics: ok ? [] : ["OpenAI Compatible capability test failed"],
  };
}

export function createModelsDomain(ctx: MockContext): ArgusApiClient["models"] {
  const { db } = ctx;
  const currentEnterpriseUser = () =>
    db.enterpriseUsers.find((entry) => entry.userId === ctx.actor().id);
  const permissions = () =>
    resolvePermissions(db, ctx.enterpriseId(), ctx.actor().id, ctx.nowIso());
  const isEnterpriseAdmin = () => permissions().has("*");
  const isDepartmentAdmin = () =>
    !permissions().has("*") && permissions().has("model_quota.manage");
  const monthPrefix = () => ctx.nowIso().slice(0, 7);
  const usageFor = (modelId: string) =>
    db.usagePoints.filter(
      (point) => point.modelId === modelId && point.date.startsWith(monthPrefix()),
    );
  const sumAmount = (points: ModelUsagePoint[]) =>
    points.reduce((sum, point) => sum + point.amount, 0);

  return {
    async list() {
      await ctx.pause();
      return db.models.filter((model) => model.enterpriseId === ctx.enterpriseId());
    },
    async get(id) {
      await ctx.pause();
      return ctx.mustFind(db.models, (model) => model.id === id, "AI model");
    },
    async testAndCreate(input) {
      await ctx.pause();
      if (!isEnterpriseAdmin()) throw new Error("forbidden");
      const ok =
        input.baseUrl.startsWith("http") &&
        input.apiKey.trim().length >= 6 &&
        !/invalid|incompatible/i.test(input.modelId + input.apiKey);
      const result = compatibility(ctx.nowIso(), ok);
      if (!ok) return { created: false, compatibility: result };
      const model = {
        id: nextId(db, "model"),
        enterpriseId: ctx.enterpriseId(),
        name: input.name,
        baseUrl: input.baseUrl,
        modelId: input.modelId,
        apiProtocol: input.apiProtocol ?? "chat_completions",
        contextWindowTokens: input.contextWindowTokens ?? 128_000,
        maxOutputTokens: input.maxOutputTokens ?? 8192,
        credentialRef: `secret://model/${nextId(db, "credential")}`,
        inputPricePerMillionTokens: input.inputPricePerMillionTokens,
        outputPricePerMillionTokens: input.outputPricePerMillionTokens,
        compatibility: result,
        healthStatus: "healthy" as const,
        enabled: true,
        revision: 1,
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
      };
      db.models.push(model);
      ctx.audit("ai_model.create", {
        resourceType: "ai_model",
        resourceId: model.id,
        summary: `测试并创建模型 ${model.name}`,
      });
      ctx.save();
      return { created: true, model, compatibility: result };
    },
    async update(id, patch) {
      await ctx.pause();
      if (!isEnterpriseAdmin()) throw new Error("forbidden");
      const model = ctx.mustFind(db.models, (entry) => entry.id === id, "AI model");
      const connectionChanged =
        patch.baseUrl !== undefined || patch.apiKey !== undefined || patch.modelId !== undefined;
      if (connectionChanged) {
        const ok =
          (patch.baseUrl ?? model.baseUrl).startsWith("http") &&
          !/invalid|incompatible/i.test((patch.modelId ?? model.modelId) + (patch.apiKey ?? ""));
        model.compatibility = compatibility(ctx.nowIso(), ok);
        model.enabled = ok && (patch.enabled ?? model.enabled);
        model.healthStatus = ok ? "healthy" : "unreachable";
        if (patch.apiKey) model.credentialRef = `secret://model/${nextId(db, "credential")}`;
      }
      const publicPatch = { ...patch };
      delete publicPatch.apiKey;
      Object.assign(model, publicPatch, {
        revision: model.revision + 1,
        updatedAt: ctx.nowIso(),
      });
      ctx.save();
      return model;
    },
    async delete(id) {
      await ctx.pause();
      if (!isEnterpriseAdmin()) throw new Error("forbidden");
      db.models = db.models.filter((entry) => entry.id !== id);
      db.modelQuotas = db.modelQuotas.filter((entry) => entry.modelId !== id);
      ctx.save();
    },
    async test(id) {
      await ctx.pause();
      const model = ctx.mustFind(db.models, (entry) => entry.id === id, "AI model");
      model.compatibility = compatibility(ctx.nowIso(), true);
      model.healthStatus = "healthy";
      model.updatedAt = ctx.nowIso();
      ctx.save();
      return model.compatibility;
    },
    async listAvailability() {
      await ctx.pause();
      const enterpriseUser = currentEnterpriseUser();
      if (!enterpriseUser) return [];
      return db.models
        .filter((model) => model.enterpriseId === ctx.enterpriseId())
        .map((model) => {
          const points = usageFor(model.id);
          const departmentQuota = db.modelQuotas.find(
            (q) => q.modelId === model.id && q.subjectType === "department" && q.subjectId === enterpriseUser.departmentId,
          );
          const userQuota = db.modelQuotas.find(
            (q) => q.modelId === model.id && q.subjectType === "user" && q.subjectId === enterpriseUser.userId,
          );
          const departmentUsed = sumAmount(points.filter((p) => p.departmentId === enterpriseUser.departmentId));
          const userUsed = sumAmount(points.filter((p) => p.userId === enterpriseUser.userId));
          const departmentRemaining = departmentQuota ? Math.max(0, departmentQuota.monthlyAmount - departmentUsed) : undefined;
          const userRemaining = userQuota ? Math.max(0, userQuota.monthlyAmount - userUsed) : undefined;
          let reason: import("../types").ModelAvailability["reason"];
          if (!model.enabled) reason = "disabled";
          else if (model.healthStatus !== "healthy") reason = "unhealthy";
          else if (!model.compatibility.toolCalling || !model.compatibility.structuredOutput) reason = "compatibility_failed";
          else if (departmentRemaining !== undefined && departmentRemaining <= 0) reason = "department_quota_exhausted";
          else if (userRemaining !== undefined && userRemaining <= 0) reason = "user_quota_exhausted";
          return { modelId: model.id, available: !reason, reason, departmentRemaining, userRemaining };
        });
    },
    async listQuotas(modelId) {
      await ctx.pause();
      const enterpriseUser = currentEnterpriseUser();
      let quotas = db.modelQuotas.filter(
        (quota) => quota.enterpriseId === ctx.enterpriseId() && (!modelId || quota.modelId === modelId),
      );
      if (!isEnterpriseAdmin()) {
        if (!enterpriseUser || !isDepartmentAdmin()) return [];
        const memberIds = new Set(
          db.enterpriseUsers.filter((entry) => entry.departmentId === enterpriseUser.departmentId).map((entry) => entry.userId),
        );
        quotas = quotas.filter(
          (quota) =>
            (quota.subjectType === "department" && quota.subjectId === enterpriseUser.departmentId) ||
            (quota.subjectType === "user" && memberIds.has(quota.subjectId)),
        );
      }
      return quotas;
    },
    async setQuota(input) {
      await ctx.pause();
      const enterpriseUser = currentEnterpriseUser();
      if (input.subjectType === "department" && !isEnterpriseAdmin()) throw new Error("forbidden");
      if (input.subjectType === "user" && !isEnterpriseAdmin()) {
        if (!enterpriseUser || !isDepartmentAdmin()) throw new Error("forbidden");
        const target = db.enterpriseUsers.find((entry) => entry.userId === input.subjectId);
        if (!target || target.departmentId !== enterpriseUser.departmentId) throw new Error("cross-department quota denied");
      }
      if (input.subjectType === "user" && input.monthlyAmount !== undefined) {
        const target = db.enterpriseUsers.find((entry) => entry.userId === input.subjectId);
        const departmentQuota = db.modelQuotas.find(
          (quota) => quota.modelId === input.modelId && quota.subjectType === "department" && quota.subjectId === target?.departmentId,
        );
        if (departmentQuota && input.monthlyAmount > departmentQuota.monthlyAmount) {
          throw new Error("user quota exceeds department quota");
        }
      }
      const existing = db.modelQuotas.find(
        (quota) => quota.modelId === input.modelId && quota.subjectType === input.subjectType && quota.subjectId === input.subjectId,
      );
      if (input.monthlyAmount === undefined) {
        if (existing) db.modelQuotas = db.modelQuotas.filter((quota) => quota.id !== existing.id);
        ctx.save();
        return null;
      }
      if (existing) {
        existing.monthlyAmount = input.monthlyAmount;
        existing.updatedAt = ctx.nowIso();
        ctx.save();
        return existing;
      }
      const quota = {
        id: nextId(db, "quota"),
        enterpriseId: ctx.enterpriseId(),
        modelId: input.modelId,
        subjectType: input.subjectType,
        subjectId: input.subjectId,
        monthlyAmount: input.monthlyAmount,
        updatedAt: ctx.nowIso(),
      };
      db.modelQuotas.push(quota);
      ctx.save();
      return quota;
    },
    async usage(range) {
      await ctx.pause();
      const from = range?.from ?? "0000-00-00";
      const to = range?.to ?? "9999-99-99";
      const enterpriseUser = currentEnterpriseUser();
      let points = db.usagePoints.filter(
        (point) => point.date >= from && point.date <= to && (!range?.modelId || point.modelId === range.modelId),
      );
      if (!isEnterpriseAdmin() && enterpriseUser) {
        points = isDepartmentAdmin()
          ? points.filter((point) => point.departmentId === enterpriseUser.departmentId)
          : points.filter((point) => point.userId === enterpriseUser.userId);
      }
      const totalRequests = points.reduce((sum, point) => sum + point.requestCount, 0);
      const successCount = points.reduce((sum, point) => sum + point.successCount, 0);
      return {
        from,
        to,
        modelId: range?.modelId,
        totalInputTokens: points.reduce((sum, point) => sum + point.inputTokens, 0),
        totalOutputTokens: points.reduce((sum, point) => sum + point.outputTokens, 0),
        totalRequests,
        successRate: totalRequests ? successCount / totalRequests : 1,
        totalAmount: points.reduce((sum, point) => sum + point.amount, 0),
        avgLatencyMs: totalRequests
          ? points.reduce((sum, point) => sum + point.avgLatencyMs * point.requestCount, 0) / totalRequests
          : 0,
        errorCount: points.reduce((sum, point) => sum + point.errorCount, 0),
        toolCallingFailures: points.reduce((sum, point) => sum + point.toolCallingFailures, 0),
        structuredOutputFailures: points.reduce((sum, point) => sum + point.structuredOutputFailures, 0),
        points,
      };
    },
  };
}

export { calculateModelAmount };
