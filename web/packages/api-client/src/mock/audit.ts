import type { ArgusApiClient } from "../client";
import type { MockContext } from "./context";

/** Enterprise audit trail queries. */
export function createAuditDomain(ctx: MockContext): ArgusApiClient["audit"] {
  const { db } = ctx;

  return {
    async list(filter, query) {
      await ctx.pause();
      let items = db.auditEvents.filter(
        (entry) => entry.enterpriseId === ctx.enterpriseId(),
      );
      if (filter?.action) {
        items = items.filter((entry) => entry.action === filter.action);
      }
      if (filter?.actorUserId) {
        items = items.filter(
          (entry) => entry.actorUserId === filter.actorUserId,
        );
      }
      if (filter?.resourceType) {
        items = items.filter(
          (entry) => entry.resourceType === filter.resourceType,
        );
      }
      if (filter?.result) {
        items = items.filter((entry) => entry.result === filter.result);
      }
      if (filter?.query) {
        items = items.filter((entry) =>
          entry.summary.includes(filter.query ?? ""),
        );
      }
      return ctx.paginate(items, query);
    },
  };
}
