import type { ArgusApiClient } from "../client";
import type {
  DemoScenario,
  InteractiveCardValidationIssue,
  ToolOutputSchema,
} from "../types";
import type { MockContext } from "./context";
import { resolvePermissions } from "./permissions";
import { nextId } from "./store";

const SCENARIOS: DemoScenario[] = ["default", "empty", "error", "large", "light", "dark", "zh-CN", "en-US"];

const TOOL_SCHEMAS: ToolOutputSchema[] = [
  {
    toolName: "host.list",
    version: "2026-08-01",
    fields: [
      { path: "items", type: "array", description: "Host rows" },
      { path: "items[].name", type: "string" },
      { path: "items[].status", type: "string" },
    ],
  },
  {
    toolName: "telemetry.query",
    version: "2026-08-01",
    fields: [
      { path: "series", type: "array" },
      { path: "summary", type: "object" },
      { path: "summary.value", type: "number" },
    ],
  },
];

export function createInteractiveCardsDomain(ctx: MockContext): ArgusApiClient["interactiveCards"] {
  const { db } = ctx;
  const isAdmin = () => {
    const permissions = resolvePermissions(
      db,
      ctx.enterpriseId(),
      ctx.actor().id,
      ctx.nowIso(),
    );
    return permissions.has("*") || permissions.has("interactive_card.create");
  };
  const getCard = (id: string) =>
    ctx.mustFind(db.interactiveCards, (entry) => entry.id === id, "interactive card");
  const assertEditable = (id: string) => {
    if (!isAdmin()) throw new Error("forbidden");
    const card = getCard(id);
    if (card.source === "system") throw new Error("system cards are read-only");
    return card;
  };

  const validateCard = (id: string) => {
    const card = getCard(id);
    const issues: InteractiveCardValidationIssue[] = [];
    if (/src=["']https?:|eval\(|new Function/.test(card.htmlTemplate)) {
      issues.push({ code: "UNSAFE_TEMPLATE", message: "unsafe external or dynamic code" });
    }
    for (const slot of card.slots) {
      if (slot.required && !card.htmlTemplate.includes(`data-slot="${slot.name}"`)) {
        issues.push({ code: "SLOT_MARKER_MISSING", message: "required slot marker missing", slot: slot.name });
      }
      if (slot.required && !slot.aiGenerated) {
        const binding = card.bindings.find((entry) => entry.slotName === slot.name);
        if (!binding) {
          issues.push({ code: "BINDING_REQUIRED", message: "required slot has no binding", slot: slot.name });
        } else {
          const schema = TOOL_SCHEMAS.find(
            (entry) => entry.toolName === binding.toolName && entry.version === binding.schemaVersion,
          );
          const field = schema?.fields.find((entry) => entry.path === binding.fieldPath);
          if (!field || field.type !== slot.type) {
            issues.push({ code: "BINDING_TYPE_MISMATCH", message: "binding field type does not match slot", slot: slot.name });
          }
        }
      }
    }
    const result = {
      valid: issues.length === 0,
      checkedAt: ctx.nowIso(),
      passedScenarios: issues.length === 0 ? SCENARIOS : [],
      issues,
    };
    card.validation = result;
    card.updatedAt = ctx.nowIso();
    return result;
  };

  return {
    async list(filter) {
      await ctx.pause();
      const admin = isAdmin();
      return db.interactiveCards.filter((card) => {
        const visible =
          card.source === "system" ||
          (card.enterpriseId === ctx.enterpriseId() && (admin || card.enabled));
        if (!visible || (filter?.source && card.source !== filter.source)) return false;
        if (filter?.lifecycle?.length && !filter.lifecycle.includes(card.lifecycle)) return false;
        return !filter?.query || card.name.toLowerCase().includes(filter.query.toLowerCase()) || card.slug.includes(filter.query.toLowerCase());
      });
    },
    async get(id) {
      await ctx.pause();
      const card = getCard(id);
      if (card.source === "enterprise" && !isAdmin() && !card.enabled) throw new Error("not found");
      return card;
    },
    async create(input) {
      await ctx.pause();
      if (!isAdmin()) throw new Error("forbidden");
      const card = {
        id: nextId(db, "card"),
        enterpriseId: ctx.enterpriseId(),
        source: "enterprise" as const,
        slug: input.slug,
        name: input.name,
        description: input.description,
        version: "0.1.0",
        revision: 1,
        lifecycle: "draft" as const,
        enabled: false,
        htmlTemplate: input.htmlTemplate,
        slots: input.slots ?? [],
        bindings: [],
        demoData: input.demoData ?? {},
        createdBy: ctx.actor().id,
        createdAt: ctx.nowIso(),
        updatedAt: ctx.nowIso(),
      };
      db.interactiveCards.push(card);
      validateCard(card.id);
      ctx.save();
      return card;
    },
    async update(id, patch) {
      await ctx.pause();
      const card = assertEditable(id);
      Object.assign(card, patch, {
        enabled: false,
        lifecycle: "draft",
        validation: undefined,
        revision: card.revision + 1,
        updatedAt: ctx.nowIso(),
      });
      ctx.save();
      return card;
    },
    async delete(id) {
      await ctx.pause();
      assertEditable(id);
      db.interactiveCards = db.interactiveCards.filter((entry) => entry.id !== id);
      ctx.save();
    },
    async updateBindings(id, bindings) {
      await ctx.pause();
      const card = assertEditable(id);
      card.bindings = bindings;
      card.enabled = false;
      card.lifecycle = "draft";
      card.validation = undefined;
      card.revision += 1;
      card.updatedAt = ctx.nowIso();
      ctx.save();
      return card;
    },
    async validate(id) {
      await ctx.pause();
      assertEditable(id);
      const result = validateCard(id);
      ctx.save();
      return result;
    },
    async renderDemo(id) {
      await ctx.pause();
      const card = getCard(id);
      return { interactiveCardId: card.id, html: card.htmlTemplate, demoData: card.demoData };
    },
    async enable(id) {
      await ctx.pause();
      const card = assertEditable(id);
      const result = validateCard(id);
      if (!result.valid || result.passedScenarios.length !== SCENARIOS.length) {
        throw new Error("interactive card validation gate failed");
      }
      card.enabled = true;
      card.lifecycle = "active";
      card.updatedAt = ctx.nowIso();
      ctx.save();
      return card;
    },
    async disable(id) {
      await ctx.pause();
      const card = assertEditable(id);
      card.enabled = false;
      card.lifecycle = "draft";
      card.updatedAt = ctx.nowIso();
      ctx.save();
      return card;
    },
    async deprecate(id) {
      await ctx.pause();
      const card = assertEditable(id);
      card.enabled = false;
      card.lifecycle = "deprecated";
      card.updatedAt = ctx.nowIso();
      ctx.save();
      return card;
    },
    async listToolSchemas() {
      await ctx.pause();
      return TOOL_SCHEMAS;
    },
  };
}
