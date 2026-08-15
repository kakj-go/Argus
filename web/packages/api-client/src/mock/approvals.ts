import type { ArgusApiClient } from "../client";
import type { MockContext } from "./context";

/**
 * Pending Actions: the two-phase mutation flow. confirm() records the user
 * gesture; actions whose risk matches an approval policy wait for
 * approve()/reject() before an execution Task is created.
 */
export function createApprovalsDomain(
  ctx: MockContext,
): ArgusApiClient["approvals"] {
  const { db } = ctx;

  return {
    async list(filter, query) {
      await ctx.pause();
      let items = db.pendingActions.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      if (filter?.status?.length) {
        items = items.filter((entry) => filter.status?.includes(entry.status));
      }
      if (filter?.riskLevel?.length) {
        items = items.filter((entry) =>
          filter.riskLevel?.includes(entry.riskLevel),
        );
      }
      if (filter?.query) {
        items = items.filter((entry) =>
          entry.title.includes(filter.query ?? ""),
        );
      }
      return ctx.paginate(items, query);
    },
    async get(actionRef) {
      await ctx.pause();
      return ctx.getAction(actionRef);
    },
    async preview(input) {
      await ctx.pause();
      return ctx.createPendingAction(input);
    },
    async confirm(actionRef) {
      await ctx.pause();
      const action = ctx.getAction(actionRef);
      if (action.status !== "awaiting_confirmation") {
        throw new Error(`cannot confirm action in status ${action.status}`);
      }
      ctx.ensureNotExpired(action);
      ctx.audit("pending_action.confirm", {
        resourceType: "pending_action",
        resourceId: action.id,
        summary: `${action.title} 已确认`,
        origin: action.conversationId ? "card_action" : "admin_ui",
      });
      if (action.approval?.required) {
        action.status = "awaiting_approval";
        action.updatedAt = ctx.nowIso();
        ctx.save();
        return { pendingAction: action };
      }
      const task = ctx.startExecution(action);
      return { pendingAction: action, task };
    },
    async cancel(actionRef) {
      await ctx.pause();
      const action = ctx.getAction(actionRef);
      if (
        action.status !== "awaiting_confirmation" &&
        action.status !== "awaiting_approval"
      ) {
        throw new Error(`cannot cancel action in status ${action.status}`);
      }
      action.status = "cancelled";
      action.updatedAt = ctx.nowIso();
      ctx.audit("pending_action.cancel", {
        resourceType: "pending_action",
        resourceId: action.id,
        summary: `${action.title} 已取消`,
      });
      ctx.save();
      return action;
    },
    async approve(actionRef, comment) {
      await ctx.pause();
      const action = ctx.getAction(actionRef);
      if (action.status !== "awaiting_approval" || !action.approval) {
        throw new Error(`cannot approve action in status ${action.status}`);
      }
      ctx.ensureNotExpired(action);
      const who = ctx.actor();
      if (action.approval.separationOfDuty && action.createdBy === who.id) {
        throw new Error("separation of duty: creator cannot approve");
      }
      action.approval.decisions.push({
        userId: who.id,
        userName: who.displayName,
        decision: "approved",
        reason: comment,
        at: ctx.nowIso(),
      });
      ctx.audit("approval.approve", {
        resourceType: "pending_action",
        resourceId: action.id,
        summary: `审批通过 ${action.title}`,
      });
      const approved = action.approval.decisions.filter(
        (decision) => decision.decision === "approved",
      ).length;
      if (approved >= action.approval.minApprovers) {
        action.status = "ready";
        ctx.startExecution(action);
      } else {
        action.updatedAt = ctx.nowIso();
        ctx.save();
      }
      return action;
    },
    async reject(actionRef, reason) {
      await ctx.pause();
      const action = ctx.getAction(actionRef);
      if (action.status !== "awaiting_approval" || !action.approval) {
        throw new Error(`cannot reject action in status ${action.status}`);
      }
      const who = ctx.actor();
      action.approval.decisions.push({
        userId: who.id,
        userName: who.displayName,
        decision: "rejected",
        reason,
        at: ctx.nowIso(),
      });
      action.status = "rejected";
      action.updatedAt = ctx.nowIso();
      ctx.audit("approval.reject", {
        resourceType: "pending_action",
        resourceId: action.id,
        summary: `审批驳回 ${action.title}`,
      });
      ctx.save();
      return action;
    },
  };
}
