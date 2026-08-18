import type { ArgusApiClient } from "../client";
import type {
  CardBindingInvokeResult,
  CardPresentation,
  CardValidationRun,
  CardVersion,
  CardVersionSummary,
  InteractiveCard,
  ToolSchemaCatalog,
} from "../generated/contracts";
import type { MockContext } from "./context";
import type { MockInteractiveCard as LegacyCard } from "./internal-types";
import { resolvePermissions } from "./permissions";

const SCENARIOS = [
  "default",
  "empty",
  "error",
  "large",
  "light",
  "dark",
  "zh-CN",
  "en-US",
] as const;

const validationRuns = new Map<string, CardValidationRun>();

export function createInteractiveCardsDomain(
  ctx: MockContext,
): ArgusApiClient["interactiveCards"] {
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
  const getLegacy = (id: string) =>
    ctx.mustFind(
      db.interactiveCards,
      (entry) => entry.id === id,
      "interactive card",
    );
  const assertEditable = (id: string) => {
    if (!isAdmin()) throw new Error("forbidden");
    const card = getLegacy(id);
    if (card.source === "system") throw new Error("system cards are read-only");
    return card;
  };

  return {
    async list() {
      await ctx.pause();
      return db.interactiveCards
        .filter(
          (card) =>
            card.source === "system" ||
            (card.enterpriseId === ctx.enterpriseId() &&
              (isAdmin() || card.enabled)),
        )
        .map(toCard);
    },
    async get(id) {
      await ctx.pause();
      return toCard(getLegacy(id));
    },
    async listVersions(id) {
      await ctx.pause();
      return [await toVersionSummary(getLegacy(id))];
    },
    async getVersion(id, revision) {
      await ctx.pause();
      const card = getLegacy(id);
      if (revision !== card.revision) throw new Error("version not found");
      return toVersion(card);
    },
    async createConfigurationVersion(id, input) {
      await ctx.pause();
      const card = assertEditable(id);
      if (input.expected_version !== card.revision) {
        throw new Error("version conflict");
      }
      card.name = input.name;
      card.description = input.description;
      card.revision += 1;
      card.enabled = false;
      card.lifecycle = "draft";
      card.bindings = input.slot_bindings.map((binding) => ({
        slotName: binding.slot_name,
        mode: binding.mode,
        toolName: binding.tool_id,
        schemaVersion: binding.output_schema_version,
        fieldPath: binding.path,
      }));
      card.demoData =
        (input.demos.find((demo) => demo.scenario === "default")?.data as
          | Record<string, unknown>
          | undefined) ?? {};
      card.updatedAt = ctx.nowIso();
      ctx.save();
      return toVersion(card);
    },
    async startValidation(id, input) {
      await ctx.pause();
      assertEditable(id);
      const contentHash = await sha256(getLegacy(id).htmlTemplate);
      const run: CardValidationRun = {
        id: crypto.randomUUID(),
        card_id: id,
        revision: input.revision,
        runtime_version: input.runtime_version,
        content_hash: contentHash,
        nonce: crypto.randomUUID(),
        status: "pending",
        required_scenarios: [...SCENARIOS],
        passed_scenarios: [],
        issues: [],
        expires_at: new Date(Date.now() + 30 * 60_000).toISOString(),
        created_at: ctx.nowIso(),
      };
      validationRuns.set(run.id, run);
      return run;
    },
    async submitValidationEvidence(runId, evidence) {
      await ctx.pause();
      const run = validationRuns.get(runId);
      if (!run || run.nonce !== evidence.nonce) {
        throw new Error("validation run invalid");
      }
      const passed = evidence.scenarios
        .filter(
          (item) =>
            item.ready &&
            item.protocol_violations === 0 &&
            item.runtime_errors === 0 &&
            item.serious_a11y_violations === 0 &&
            item.missing_required_slots.length === 0 &&
            !item.size_violation,
        )
        .map((item) => item.scenario);
      run.status = passed.length === SCENARIOS.length ? "passed" : "failed";
      run.passed_scenarios = passed;
      run.issues =
        run.status === "passed"
          ? []
          : [
              {
                code: "CARD_RUNTIME_VALIDATION_FAILED",
                message: "Demo validation failed",
              },
            ];
      return run;
    },
    async changeState(id, action, input) {
      await ctx.pause();
      const card = assertEditable(id);
      if (input.expected_version !== card.revision) {
        throw new Error("version conflict");
      }
      if (action === "deprecate") {
        card.lifecycle = "deprecated";
        card.enabled = false;
      } else if (action === "disable") {
        card.enabled = false;
      } else {
        card.lifecycle = "active";
        card.enabled = true;
      }
      card.updatedAt = ctx.nowIso();
      ctx.save();
      return toCard(card);
    },
    async listToolSchemas() {
      await ctx.pause();
      return toolCatalog();
    },
    async createPresentation(cardInstanceId, input) {
      await ctx.pause();
      const card =
        db.interactiveCards.find((entry) => entry.id === cardInstanceId) ??
        db.interactiveCards.find((entry) => entry.enabled) ??
        db.interactiveCards[0];
      if (!card) throw new Error("Card unavailable");
      return toPresentation(
        card,
        cardInstanceId,
        input.locale,
        input.color_scheme,
      );
    },
    async invokeQueryBinding() {
      await ctx.pause();
      return { status: "succeeded", data: {} } as CardBindingInvokeResult;
    },
    async invokeActionBinding() {
      await ctx.pause();
      return { status: "awaiting_approval" } as CardBindingInvokeResult;
    },
  };
}

function toCard(card: LegacyCard): InteractiveCard {
  return {
    id: card.id,
    enterprise_id: card.enterpriseId,
    source: card.source,
    slug: card.slug,
    name: card.name,
    description: card.description,
    lifecycle: card.lifecycle,
    enabled: card.enabled,
    availability: card.enabled ? "available" : "disabled",
    active_revision: card.enabled ? card.revision : undefined,
    latest_revision: card.revision,
    version: card.revision,
    created_by: card.createdBy,
    created_at: card.createdAt,
    updated_at: card.updatedAt,
  };
}

async function toVersionSummary(card: LegacyCard): Promise<CardVersionSummary> {
  const contentHash = await sha256(card.htmlTemplate);
  return {
    card_id: card.id,
    revision: card.revision,
    status: card.enabled ? "active" : "draft",
    content_hash: contentHash,
    manifest_hash: contentHash,
    created_by: card.createdBy,
    created_at: card.createdAt,
  };
}

async function toVersion(card: LegacyCard): Promise<CardVersion> {
  const contentHash = await sha256(card.htmlTemplate);
  return {
    ...(await toVersionSummary(card)),
    manifest: manifest(card, contentHash),
    entrypoint_html: card.htmlTemplate,
    slot_bindings: card.bindings.map((binding) => ({
      slot_name: binding.slotName,
      slot_kind: "data",
      mode: binding.mode,
      tool_id: binding.toolName,
      output_schema_version: binding.schemaVersion,
      schema_hash: "0".repeat(64),
      path: binding.fieldPath,
      value_type:
        card.slots.find((slot) => slot.name === binding.slotName)?.type ??
        "object",
    })),
    demos: SCENARIOS.map((scenario) => ({
      scenario,
      data: scenario === "default" ? card.demoData : {},
    })),
  };
}

function manifest(card: LegacyCard, contentHash: string): CardVersion["manifest"] {
  return {
    schema_version: "argus.card_manifest/v1",
    card_id: card.id,
    revision: card.revision,
    source: card.source,
    entrypoint_hash: contentHash,
    bridge_version: "argus.card_bridge/v1",
    slots: card.slots.map((slot) => ({
      name: slot.name,
      kind: "data",
      value_type: slot.type,
      required: slot.required,
      ai_generated: slot.aiGenerated ?? false,
    })),
    allowed_resources: ["inline_style"],
    supported_locales: ["zh-CN", "en-US"],
    default_locale: "zh-CN",
    supported_color_schemes: ["light", "dark"],
    max_message_bytes: 1024 * 1024,
  };
}

function toolCatalog(): ToolSchemaCatalog {
  return {
    items: ["host.list", "kubernetes.cluster.list", "connector.list"].map(
      (tool_id) => ({
        tool_id,
        tool_family: tool_id,
        risk: "read",
        execution_mode: "parallel_safe",
        output_schema_version: `${tool_id}/v1`,
        compatible_output_versions: [`${tool_id}/v1`],
        schema_hash: `mock-${tool_id}`,
        output_schema: {},
        fields: [
          {
            path: "$.items",
            value_type: "array",
            semantic_type: "resource_collection",
          },
        ],
      }),
    ),
  };
}

async function toPresentation(
  card: LegacyCard,
  instanceId: string,
  locale: "zh-CN" | "en-US",
  colorScheme: "light" | "dark",
): Promise<CardPresentation> {
  const contentHash = await sha256(card.htmlTemplate);
  return {
    presentation_id: crypto.randomUUID(),
    card_instance: {
      id: instanceId,
      card_id: card.id,
      card_revision: card.revision,
      conversation_id: "mock-conversation",
      status: "active",
      created_at: card.createdAt,
    },
    manifest: manifest(card, contentHash),
    entrypoint_html: card.htmlTemplate,
    render_plan: {
      schema_version: "argus.render_plan/v1",
      card_id: card.id,
      card_revision: card.revision,
      card_instance_id: instanceId,
      data_bindings: [],
      query_binding_ids: {},
      action_binding_ids: {},
      locale,
      color_scheme: colorScheme,
    },
    initial_data: card.demoData,
    partial: false,
    locale_fallback: false,
    expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
  };
}

async function sha256(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}
