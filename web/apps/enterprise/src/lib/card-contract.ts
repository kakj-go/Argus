import type { CardManifest, RenderPlan } from "@argus/api-client/contracts";

export const cardOrigin =
  import.meta.env.VITE_CARD_ORIGIN ?? "http://localhost:4176";

export function provisionalCardContract(input: {
  card_id: string;
  revision: number;
  card_instance_id: string;
  action_binding_ids?: string[];
  locale?: "zh-CN" | "en-US";
  color_scheme?: "light" | "dark";
}): { manifest: CardManifest; render_plan: RenderPlan } {
  const action_binding_ids = Object.fromEntries(
    (input.action_binding_ids ?? []).map((id, index) => [
      `action_${index}`,
      id,
    ]),
  );
  return {
    manifest: {
      schema_version: "argus.card_manifest/v1",
      card_id: input.card_id,
      revision: input.revision,
      source: "system",
      entrypoint_hash: "",
      bridge_version: "argus.card_bridge/v1",
      slots: [],
      allowed_resources: ["inline_style"],
      supported_locales: ["zh-CN", "en-US"],
      default_locale: "zh-CN",
      supported_color_schemes: ["light", "dark"],
      max_message_bytes: 1024 * 1024,
    },
    render_plan: {
      schema_version: "argus.render_plan/v1",
      card_id: input.card_id,
      card_revision: input.revision,
      card_instance_id: input.card_instance_id,
      data_bindings: [],
      query_binding_ids: {},
      action_binding_ids,
      locale: input.locale ?? "zh-CN",
      color_scheme: input.color_scheme ?? "light",
    },
  };
}
