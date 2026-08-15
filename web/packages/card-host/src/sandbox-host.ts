import {
  CARD_BRIDGE_VERSION,
  isCardBridgeEnvelope,
  type CardBridgeBindings,
  type CardBridgeError,
  type CardNotifyLevel,
  type CardPresentationContext,
  type CardToHostMessage,
  type HostToCardMessage,
} from "./protocol";

export type QueryInvokeHandler = (
  queryBindingId: string,
  params: unknown,
) => unknown | Promise<unknown>;

export type ActionInvokeHandler = (
  actionBindingId: string,
  params: unknown,
) => unknown | Promise<unknown>;

export type NotifyHandler = (level: CardNotifyLevel, message: string) => void;

export type ProtocolViolationHandler = (
  reason: CardProtocolViolation,
  message: unknown,
) => void;

export type CardProtocolViolation =
  | "origin_mismatch"
  | "instance_mismatch"
  | "nonce_mismatch"
  | "sequence_replay"
  | "malformed_message";

export type CardHostOptions = {
  cardInstanceId: string;
  /**
   * Channel nonce. Must match the nonce baked into the srcdoc via
   * injectCardRuntime. Generated when omitted — read it back from
   * `CardHost.nonce` before building the srcdoc.
   */
  nonce?: string;
  /** Capability allowlist; invokes outside of it get an error reply. */
  bindings?: Partial<CardBridgeBindings>;
  initialData?: Record<string, unknown>;
  context: CardPresentationContext;
  /**
   * Expected postMessage origin. Opaque-origin sandboxed iframes report
   * "null", which is the default accepted value (plus the host origin for
   * non-sandboxed usage such as tests).
   */
  expectedOrigin?: string;
  /** Clamp for card.resize, defaults to 2000 (docs/05 §8.4 size policy). */
  maxHeight?: number;
  onReady?: () => void;
  onQueryInvoke?: QueryInvokeHandler;
  onActionInvoke?: ActionInvokeHandler;
  onNotify?: NotifyHandler;
  onOpenLink?: (url: string) => void;
  onResize?: (height: number) => void;
  onProtocolViolation?: ProtocolViolationHandler;
};

export type CardHost = {
  /** Channel nonce shared with the injected card runtime. */
  readonly nonce: string;
  /** True after the card completed the nonce handshake. */
  readonly ready: boolean;
  /**
   * Push locale/theme/token changes. Before the handshake the latest context
   * is cached and delivered with host.init; afterwards a host.context message
   * is sent in place, without reloading the card.
   */
  setContext: (context: CardPresentationContext) => void;
  /** Update the capability allowlist; applied from the next message on. */
  setBindings: (bindings: Partial<CardBridgeBindings>) => void;
  destroy: () => void;
};

export function generateCardNonce(): string {
  const cryptoApi = globalThis.crypto;
  if (cryptoApi?.getRandomValues) {
    const bytes = new Uint8Array(16);
    cryptoApi.getRandomValues(bytes);
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join(
      "",
    );
  }
  return `${Date.now().toString(16)}${Math.random().toString(16).slice(2)}`;
}

const DEFAULT_MAX_HEIGHT = 2000;

function toError(value: unknown, code: string): CardBridgeError {
  const message =
    value instanceof Error
      ? value.message
      : typeof value === "string"
        ? value
        : "card bridge invocation failed";
  return { code, message };
}

/**
 * Creates the host side of the card bridge for one sandboxed iframe.
 * The handshake: the card runtime posts `card.ready` echoing the nonce; only
 * then the host sends `host.init` with context, bindings and initial data.
 */
export function createCardHost(
  iframe: HTMLIFrameElement,
  options: CardHostOptions,
): CardHost {
  const nonce = options.nonce ?? generateCardNonce();
  const maxHeight = options.maxHeight ?? DEFAULT_MAX_HEIGHT;
  let context = options.context;
  let bindings: CardBridgeBindings = {
    queryBindingIds: options.bindings?.queryBindingIds ?? [],
    actionBindingIds: options.bindings?.actionBindingIds ?? [],
  };
  let ready = false;
  let destroyed = false;
  let outgoingSequence = 0;
  let lastIncomingSequence = 0;

  const post = (type: HostToCardMessage["type"], payload: object) => {
    const target = iframe.contentWindow;
    if (destroyed || !target) return;
    outgoingSequence += 1;
    target.postMessage(
      {
        version: CARD_BRIDGE_VERSION,
        cardInstanceId: options.cardInstanceId,
        nonce,
        sequence: outgoingSequence,
        type,
        ...payload,
      },
      "*",
    );
  };

  const sendInit = () => {
    post("host.init", {
      context,
      bindings,
      initialData: options.initialData ?? {},
    });
  };

  const violate = (reason: CardProtocolViolation, message: unknown) => {
    options.onProtocolViolation?.(reason, message);
  };

  const acceptOrigin = (origin: string): boolean => {
    if (options.expectedOrigin !== undefined) {
      return origin === options.expectedOrigin;
    }
    return (
      origin === "null" || origin === "" || origin === window.location.origin
    );
  };

  const handleInvoke = (
    kind: "query" | "action",
    requestId: string,
    bindingId: string,
    params: unknown,
  ) => {
    const allowed =
      kind === "query"
        ? bindings.queryBindingIds.includes(bindingId)
        : bindings.actionBindingIds.includes(bindingId);
    const handler =
      kind === "query" ? options.onQueryInvoke : options.onActionInvoke;
    if (!allowed) {
      post(`${kind}.error`, {
        requestId,
        error: {
          code: "BINDING_NOT_ALLOWED",
          message: `${kind} binding "${bindingId}" is not bound to this card`,
        },
      });
      return;
    }
    if (!handler) {
      post(`${kind}.error`, {
        requestId,
        error: {
          code: "HANDLER_NOT_REGISTERED",
          message: `host has no ${kind} invoke handler`,
        },
      });
      return;
    }
    let invoked: unknown;
    try {
      invoked = handler(bindingId, params);
    } catch (reason) {
      post(`${kind}.error`, {
        requestId,
        error: toError(reason, `${kind.toUpperCase()}_FAILED`),
      });
      return;
    }
    Promise.resolve(invoked).then(
      (data) => post(`${kind}.result`, { requestId, data: data ?? null }),
      (reason) =>
        post(`${kind}.error`, {
          requestId,
          error: toError(reason, `${kind.toUpperCase()}_FAILED`),
        }),
    );
  };

  const handleMessage = (message: CardToHostMessage) => {
    switch (message.type) {
      case "card.ready":
        ready = true;
        sendInit();
        options.onReady?.();
        break;
      case "card.resize": {
        const height = Number(message.height);
        if (Number.isFinite(height) && height > 0) {
          options.onResize?.(Math.min(Math.ceil(height), maxHeight));
        }
        break;
      }
      case "query.invoke":
        handleInvoke(
          "query",
          message.requestId,
          message.queryBindingId,
          message.params,
        );
        break;
      case "action.invoke":
        handleInvoke(
          "action",
          message.requestId,
          message.actionBindingId,
          message.params,
        );
        break;
      case "host.notify":
        options.onNotify?.(message.level, message.message);
        break;
      case "link.open":
        if (typeof message.url === "string") options.onOpenLink?.(message.url);
        break;
      default:
        violate("malformed_message", message);
        break;
    }
  };

  const listener = (event: MessageEvent) => {
    if (destroyed) return;
    if (event.source !== iframe.contentWindow) return;
    if (!acceptOrigin(event.origin)) {
      violate("origin_mismatch", event.data);
      return;
    }
    const data = event.data;
    if (!isCardBridgeEnvelope(data)) {
      // Foreign messages on the shared window channel are ignored silently.
      return;
    }
    if (data.cardInstanceId !== options.cardInstanceId) {
      violate("instance_mismatch", data);
      return;
    }
    if (data.nonce !== nonce) {
      violate("nonce_mismatch", data);
      return;
    }
    if (data.sequence <= lastIncomingSequence) {
      violate("sequence_replay", data);
      return;
    }
    lastIncomingSequence = data.sequence;
    handleMessage(data as CardToHostMessage);
  };

  window.addEventListener("message", listener);

  return {
    get nonce() {
      return nonce;
    },
    get ready() {
      return ready;
    },
    setContext(next) {
      context = next;
      if (ready) post("host.context", { context });
      // Before ready the latest context is cached and sent with host.init.
    },
    setBindings(next) {
      bindings = {
        queryBindingIds: next.queryBindingIds ?? bindings.queryBindingIds,
        actionBindingIds: next.actionBindingIds ?? bindings.actionBindingIds,
      };
    },
    destroy() {
      destroyed = true;
      window.removeEventListener("message", listener);
    },
  };
}
