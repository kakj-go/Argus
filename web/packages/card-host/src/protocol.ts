import type { BridgeMessage } from "@argus/api-client/contracts";

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
      "binding.result",
      "bridge.error",
      "bridge.destroyed",
    ].includes(message.type) &&
    typeof message.payload === "object" &&
    message.payload !== null
  );
}
