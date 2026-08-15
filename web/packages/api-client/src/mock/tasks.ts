import type { ArgusApiClient } from "../client";
import type { MockContext } from "./context";

/** Execution tasks with progress/log subscriptions. */
export function createTasksDomain(ctx: MockContext): ArgusApiClient["tasks"] {
  const { db } = ctx;

  return {
    async list(filter, query) {
      await ctx.pause();
      let items = db.tasks.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      if (filter?.status?.length) {
        items = items.filter((entry) => filter.status?.includes(entry.status));
      }
      if (filter?.type?.length) {
        items = items.filter((entry) => filter.type?.includes(entry.type));
      }
      if (filter?.query) {
        items = items.filter((entry) =>
          entry.title.includes(filter.query ?? ""),
        );
      }
      return ctx.paginate(items, query);
    },
    async get(id) {
      await ctx.pause();
      return ctx.mustFind(db.tasks, (entry) => entry.id === id, "task");
    },
    async cancel(id) {
      await ctx.pause();
      const task = ctx.mustFind(db.tasks, (entry) => entry.id === id, "task");
      if (task.status === "running" || task.status === "pending") {
        task.status = "cancelled";
        task.finishedAt = ctx.nowIso();
        ctx.audit("task.cancel", {
          resourceType: "task",
          resourceId: id,
          summary: `取消任务 ${task.title}`,
        });
        ctx.save();
        ctx.emitTask(task);
      }
      return task;
    },
    subscribe(listener) {
      return ctx.emitter.on("tasks", listener);
    },
    subscribeTask(id, listener) {
      return ctx.emitter.on(`task:${id}`, listener);
    },
  };
}
