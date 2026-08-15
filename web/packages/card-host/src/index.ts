import type { CardPresentationContext } from "./protocol";

export {
  CARD_BRIDGE_VERSION,
  isCardBridgeEnvelope,
  type CardBridgeBindings,
  type CardBridgeError,
  type CardBridgeMessage,
  type CardLocale,
  type CardMessageType,
  type CardNotifyLevel,
  type CardPresentationContext,
  type CardTheme,
  type CardToHostMessage,
  type HostToCardMessage,
} from "./protocol";

export {
  CARD_RUNTIME_MARKER,
  injectCardRuntime,
  type InjectCardRuntimeOptions,
} from "./card-runtime";

export {
  createCardHost,
  generateCardNonce,
  type ActionInvokeHandler,
  type CardHost,
  type CardHostOptions,
  type CardProtocolViolation,
  type NotifyHandler,
  type ProtocolViolationHandler,
  type QueryInvokeHandler,
} from "./sandbox-host";

export {
  collectDesignTokens,
  openCardLink,
  SandboxCard,
  type SandboxCardProps,
} from "./react";

/**
 * Legacy aliases kept for backwards compatibility with the initial draft of
 * this package; prefer the protocol types above.
 */

/** @deprecated Use CardBridgeBindings plus the cardInstanceId field. */
export type CardBinding = {
  cardInstanceId: string;
  queryBindingIds: string[];
  actionBindingIds: string[];
};

/** @deprecated Use CardToHostMessage / HostToCardMessage. */
export type CardHostEvent =
  | {
      type: "host.context";
      sequence: number;
      context: CardPresentationContext;
    }
  | { type: "card.ready"; sequence: number }
  | {
      type: "query.invoke";
      sequence: number;
      bindingId: string;
      input: unknown;
    }
  | { type: "action.invoke"; sequence: number; bindingId: string };
