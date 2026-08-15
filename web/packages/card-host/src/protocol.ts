/**
 * Host <-> Card bridge protocol (docs/05 §8.3).
 *
 * Every message is a postMessage payload carrying the protocol version, the
 * card instance id, the per-channel nonce and a strictly increasing sequence
 * number per direction. The host validates source/origin, nonce and sequence;
 * the injected card runtime validates nonce and sequence for host messages.
 */
export const CARD_BRIDGE_VERSION = "argus.card-bridge.v1" as const;

export type CardLocale = "zh-CN" | "en-US";
export type CardTheme = "light" | "dark";

/** Presentation context pushed to cards; never carries secrets or tokens. */
export type CardPresentationContext = {
  locale: CardLocale;
  theme: CardTheme;
  colorScheme: CardTheme;
  designTokens: Record<string, string>;
};

/** Bound capability ids the card is allowed to invoke. */
export type CardBridgeBindings = {
  queryBindingIds: string[];
  actionBindingIds: string[];
};

export type CardBridgeError = {
  code: string;
  message: string;
};

export type CardNotifyLevel = "info" | "success" | "warning" | "error";

type Envelope = {
  version: typeof CARD_BRIDGE_VERSION;
  cardInstanceId: string;
  nonce: string;
  /** Strictly increasing per direction; duplicates/replays are rejected. */
  sequence: number;
};

/** Host -> card messages. */
export type HostToCardMessage = Envelope &
  (
    | {
        type: "host.init";
        context: CardPresentationContext;
        bindings: CardBridgeBindings;
        initialData: Record<string, unknown>;
      }
    | { type: "host.context"; context: CardPresentationContext }
    | { type: "query.result"; requestId: string; data: unknown }
    | { type: "query.error"; requestId: string; error: CardBridgeError }
    | { type: "action.result"; requestId: string; data: unknown }
    | { type: "action.error"; requestId: string; error: CardBridgeError }
  );

/** Card -> host messages. */
export type CardToHostMessage = Envelope &
  (
    | { type: "card.ready" }
    | { type: "card.resize"; height: number }
    | {
        type: "query.invoke";
        requestId: string;
        queryBindingId: string;
        params?: unknown;
      }
    | {
        type: "action.invoke";
        requestId: string;
        actionBindingId: string;
        params?: unknown;
      }
    | { type: "host.notify"; level: CardNotifyLevel; message: string }
    | { type: "link.open"; url: string }
  );

export type CardBridgeMessage = HostToCardMessage | CardToHostMessage;

export type CardMessageType = CardBridgeMessage["type"];

/** Structural check for the common envelope; payload fields are checked per type. */
export function isCardBridgeEnvelope(value: unknown): value is Envelope & {
  type: string;
} {
  if (typeof value !== "object" || value === null) return false;
  const message = value as Record<string, unknown>;
  return (
    message.version === CARD_BRIDGE_VERSION &&
    typeof message.cardInstanceId === "string" &&
    typeof message.nonce === "string" &&
    typeof message.sequence === "number" &&
    typeof message.type === "string"
  );
}
