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
        (entry) =>
          db.actionPlans[entry.action_ref]?.enterprise_id ===
          ctx.enterpriseId(),
      );
      if (filter?.status?.length) {
        items = items.filter((entry) => filter.status?.includes(entry.status));
      }
      if (filter?.risk?.length) {
        items = items.filter((entry) => filter.risk?.includes(entry.risk));
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
        resourceId: action.action_ref,
        summary: `${action.title} 已确认`,
        origin: db.actionPlans[action.action_ref]?.conversation_id
          ? "card_action"
          : "admin_ui",
      });
      if (action.approval?.required) {
        action.status = "awaiting_approval";
        action.available_actions = ["approve", "reject", "cancel"];
        action.updated_at = ctx.nowIso();
        ctx.save();
        return { pending_action: action };
      }
      const committed = ctx.commitResourceAction(action);
      if (committed) return committed;
      const task = ctx.startExecution(action);
      return {
        pending_action: action,
        execution: {
          execution_id: task.id,
          action_ref: action.action_ref,
          status: "running",
          created_at: task.createdAt,
          updated_at: task.createdAt,
        },
      };
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
      action.available_actions = [];
      action.updated_at = ctx.nowIso();
      ctx.audit("pending_action.cancel", {
        resourceType: "pending_action",
        resourceId: action.action_ref,
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
      const plan = db.actionPlans[action.action_ref];
      if (!plan) throw new Error("pending action plan unavailable");
      if (action.approval.separation_of_duty && plan.created_by === who.id) {
        throw new Error("separation of duty: creator cannot approve");
      }
      plan.approval_decisions.push({
        actor_user_id: who.id,
        actor_name: who.displayName,
        decision: "approved",
        reason: comment,
        decided_at: ctx.nowIso(),
      });
      action.approval.approved_count = plan.approval_decisions.filter(
        (decision) => decision.decision === "approved",
      ).length;
      ctx.audit("approval.approve", {
        resourceType: "pending_action",
        resourceId: action.action_ref,
        summary: `审批通过 ${action.title}`,
      });
      if (action.approval.approved_count >= action.approval.minimum_approvers) {
        action.status = "ready";
        ctx.startExecution(action);
      } else {
        action.updated_at = ctx.nowIso();
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
      const plan = db.actionPlans[action.action_ref];
      if (!plan) throw new Error("pending action plan unavailable");
      plan.approval_decisions.push({
        actor_user_id: who.id,
        actor_name: who.displayName,
        decision: "rejected",
        reason,
        decided_at: ctx.nowIso(),
      });
      action.status = "rejected";
      action.available_actions = [];
      action.result_summary = reason;
      action.updated_at = ctx.nowIso();
      ctx.audit("approval.reject", {
        resourceType: "pending_action",
        resourceId: action.action_ref,
        summary: `审批驳回 ${action.title}`,
      });
      ctx.save();
      return action;
    },
  };
}
