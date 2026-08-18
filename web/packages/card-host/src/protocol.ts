import type {
  BridgeMessage,
  CardRuntimeValidationReport,
} from "@argus/api-client/contracts";

export const CARD_BRIDGE_VERSION = "argus.card_bridge/v1" as const;
export const CARD_MAX_MESSAGE_BYTES = 1024 * 1024;

export type CardLocale = "zh-CN" | "en-US";
export type CardTheme = "light" | "dark";
export type CardNotifyLevel = "info" | "success" | "warning" | "error";
export type CardPresentationContext = {
  locale: CardLocale;
  color_scheme: CardTheme;
  design_tokens: Record<string, string>;
};
export type CardBridgeBindings = {
  query_binding_ids: string[];
  action_binding_ids: string[];
};
export type CardBridgeError = { code: string; message: string };
export type CardBridgeMessage = BridgeMessage;
export type CardValidationReport = CardRuntimeValidationReport;
export type CardValidationRequest = Pick<
  CardRuntimeValidationReport,
  "content_hash" | "runtime_version" | "scenario" | "nonce"
>;
export type HostToCardMessage = BridgeMessage;
export type CardToHostMessage = BridgeMessage;
export type CardMessageType = BridgeMessage["type"];

export function encodedMessageBytes(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value)).byteLength;
}

export function isCardBridgeEnvelope(value: unknown): value is BridgeMessage {
  if (typeof value !== "object" || value === null) return false;
  const message = value as Record<string, unknown>;
  return (
    message.bridge_version === CARD_BRIDGE_VERSION &&
    typeof message.message_id === "string" &&
    message.message_id.length > 0 &&
    typeof message.nonce === "string" &&
    message.nonce.length >= 16 &&
    Number.isSafeInteger(message.sequence) &&
    (message.sequence as number) > 0 &&
    typeof message.type === "string" &&
    [
      "host.hello",
      "card.ready",
      "host.context",
      "data.update",
      "query.invoke",
      "action.invoke",
      "card.resize",
      "card.slot_selected",
      "card.validation_report",
      "binding.result",
      "bridge.error",
      "bridge.destroyed",
    ].includes(message.type) &&
    typeof message.payload === "object" &&
    message.payload !== null
  );
}

export function isCardValidationReport(value: unknown): value is CardValidationReport {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const report = value as Record<string, unknown>;
  return (
    typeof report.content_hash === "string" && /^[a-f0-9]{64}$/.test(report.content_hash) &&
    typeof report.runtime_version === "string" && report.runtime_version.length > 0 &&
    typeof report.nonce === "string" && report.nonce.length >= 16 &&
    ["default", "empty", "error", "large", "light", "dark", "zh-CN", "en-US"].includes(String(report.scenario)) &&
    typeof report.ready === "boolean" &&
    Number.isSafeInteger(report.protocol_violations) && Number(report.protocol_violations) >= 0 &&
    Number.isSafeInteger(report.runtime_errors) && Number(report.runtime_errors) >= 0 &&
    Number.isSafeInteger(report.serious_a11y_violations) && Number(report.serious_a11y_violations) >= 0 &&
    Array.isArray(report.missing_required_slots) && report.missing_required_slots.every((slot) => typeof slot === "string") &&
    typeof report.size_violation === "boolean"
  );
}
