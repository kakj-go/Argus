import type { CardManifest, RenderPlan } from "@argus/api-client/contracts";
import {
  CARD_BRIDGE_VERSION,
  CARD_MAX_MESSAGE_BYTES,
  encodedMessageBytes,
  isCardBridgeEnvelope,
  isCardValidationReport,
  type CardBridgeBindings,
  type CardBridgeError,
  type CardBridgeMessage,
  type CardNotifyLevel,
  type CardPresentationContext,
  type CardValidationReport,
  type CardValidationRequest,
} from "./protocol";

export type QueryInvokeHandler = (binding_id: string) => unknown | Promise<unknown>;
export type ActionInvokeHandler = (binding_id: string) => unknown | Promise<unknown>;
export type NotifyHandler = (level: CardNotifyLevel, message: string) => void;
export type ProtocolViolationHandler = (reason: CardProtocolViolation, message: unknown) => void;
export type CardProtocolViolation =
  | "origin_mismatch"
  | "nonce_mismatch"
  | "sequence_replay"
  | "message_too_large"
  | "binding_not_allowed"
  | "malformed_message"
  | "destroyed";

export type CardHostOptions = {
  card_origin: string;
  manifest: CardManifest;
  render_plan: RenderPlan;
  html: string;
  nonce?: string;
  bindings?: Partial<CardBridgeBindings>;
  initial_data?: Record<string, unknown>;
  context: CardPresentationContext;
  max_height?: number;
  onReady?: () => void;
  onQueryInvoke?: QueryInvokeHandler;
  onActionInvoke?: ActionInvokeHandler;
  onNotify?: NotifyHandler;
  onResize?: (height: number) => void;
  onProtocolViolation?: ProtocolViolationHandler;
  validation?: CardValidationRequest;
  onValidationReport?: (report: CardValidationReport) => void;
};

export type CardHost = {
  readonly nonce: string;
  readonly ready: boolean;
  setContext: (context: CardPresentationContext) => void;
  setData: (data: Record<string, unknown>) => void;
  setBindings: (bindings: Partial<CardBridgeBindings>) => void;
  destroy: () => void;
};

export function generateCardNonce(): string {
  return crypto.randomUUID();
}

function errorOf(value: unknown, code: string): CardBridgeError {
  return { code, message: value instanceof Error ? value.message : String(value) };
}

export function createCardHost(iframe: HTMLIFrameElement, options: CardHostOptions): CardHost {
  const nonce = options.nonce ?? generateCardNonce();
  const maxHeight = options.max_height ?? 2000;
  const targetOrigin = new URL(options.card_origin).origin;
  let context = options.context;
  let bindings: CardBridgeBindings = {
    query_binding_ids: options.bindings?.query_binding_ids ?? Object.values(options.render_plan.query_binding_ids),
    action_binding_ids: options.bindings?.action_binding_ids ?? Object.values(options.render_plan.action_binding_ids),
  };
  let ready = false;
  let destroyed = false;
  let outgoing = 1;
  let incoming = 0;
  let port: MessagePort | undefined;

  const violate = (reason: CardProtocolViolation, message: unknown) => options.onProtocolViolation?.(reason, message);
  const send = (type: CardBridgeMessage["type"], payload: Record<string, unknown>) => {
    if (destroyed || !port) return;
    outgoing += 1;
    const message: CardBridgeMessage = {
      bridge_version: CARD_BRIDGE_VERSION,
      message_id: crypto.randomUUID(),
      nonce,
      sequence: outgoing,
      type,
      payload,
    };
    if (encodedMessageBytes(message) > Math.min(options.manifest.max_message_bytes, CARD_MAX_MESSAGE_BYTES)) {
      violate("message_too_large", message);
      return;
    }
    port.postMessage(message);
  };

  const settle = (kind: "query" | "action", request_id: string, binding_id: string) => {
    const allowed = kind === "query" ? bindings.query_binding_ids : bindings.action_binding_ids;
    const handler = kind === "query" ? options.onQueryInvoke : options.onActionInvoke;
    if (!allowed.includes(binding_id)) {
      violate("binding_not_allowed", { kind, binding_id });
      send("binding.result", { request_id, ok: false, error: { code: "BINDING_NOT_ALLOWED", message: "Binding is not available" } });
      return;
    }
    if (!handler) {
      send("binding.result", { request_id, ok: false, error: { code: "HANDLER_NOT_REGISTERED", message: "Binding handler is unavailable" } });
      return;
    }
    Promise.resolve(handler(binding_id)).then(
      (data) => send("binding.result", { request_id, ok: true, data: data ?? null }),
      (reason) => send("binding.result", { request_id, ok: false, error: errorOf(reason, `${kind.toUpperCase()}_FAILED`) }),
    );
  };

  const onPortMessage = (event: MessageEvent) => {
    if (destroyed) return violate("destroyed", event.data);
    if (encodedMessageBytes(event.data) > Math.min(options.manifest.max_message_bytes, CARD_MAX_MESSAGE_BYTES)) return violate("message_too_large", event.data);
    if (!isCardBridgeEnvelope(event.data)) return violate("malformed_message", event.data);
    const message = event.data;
    if (message.nonce !== nonce) return violate("nonce_mismatch", message);
    if (message.sequence <= incoming) return violate("sequence_replay", message);
    incoming = message.sequence;
    const payload = message.payload as Record<string, unknown>;
    if (message.type === "card.ready") {
      ready = true;
      options.onReady?.();
    } else if (message.type === "query.invoke" || message.type === "action.invoke") {
      const bindingKey = message.type === "query.invoke"
        ? "query_binding_id"
        : "action_binding_id";
      const bindingId = payload[bindingKey];
      if (
        typeof payload.request_id !== "string" ||
        payload.request_id.length < 16 ||
        typeof bindingId !== "string"
      ) return violate("malformed_message", message);
      settle(
        message.type === "query.invoke" ? "query" : "action",
        payload.request_id,
        bindingId,
      );
    } else if (message.type === "card.resize") {
      const height = Number(payload.height);
      if (Number.isFinite(height) && height > 0) options.onResize?.(Math.min(Math.ceil(height), maxHeight));
    } else if (message.type === "card.validation_report") {
      if (!options.validation || !isCardValidationReport(payload)) return violate("malformed_message", message);
      if (
        payload.nonce !== nonce ||
        payload.content_hash !== options.validation.content_hash ||
        payload.runtime_version !== options.validation.runtime_version ||
        payload.scenario !== options.validation.scenario
      ) return violate("malformed_message", message);
      options.onValidationReport?.(payload);
    }
  };

  const connect = () => {
    if (destroyed || !iframe.contentWindow || port) return;
    const channel = new MessageChannel();
    port = channel.port1;
    port.onmessage = onPortMessage;
    port.start();
    const hello: CardBridgeMessage = {
      bridge_version: CARD_BRIDGE_VERSION,
      message_id: crypto.randomUUID(),
      nonce,
      sequence: 1,
      type: "host.hello",
      payload: {
        html: options.html,
        entrypoint_hash: options.manifest.entrypoint_hash,
        allowed_resources: options.manifest.allowed_resources,
        max_message_bytes: Math.min(
          options.manifest.max_message_bytes,
          CARD_MAX_MESSAGE_BYTES,
        ),
        locale: context.locale,
        color_scheme: context.color_scheme,
        render_plan: options.render_plan,
        initial_data: options.initial_data ?? {},
        ...(options.validation ? { validation: {
          content_hash: options.validation.content_hash,
          runtime_version: options.validation.runtime_version,
          scenario: options.validation.scenario,
          required_slots: options.manifest.slots.filter((slot) => slot.required).map((slot) => slot.name),
        } } : {}),
      },
    };
    iframe.contentWindow.postMessage(hello, targetOrigin, [channel.port2]);
  };
  iframe.addEventListener("load", connect, { once: true });

  return {
    nonce,
    get ready() { return ready; },
    setContext(next) { context = next; send("host.context", context); },
    setData(data) { send("data.update", { data }); },
    setBindings(next) {
      bindings = {
        query_binding_ids: next.query_binding_ids ?? bindings.query_binding_ids,
        action_binding_ids: next.action_binding_ids ?? bindings.action_binding_ids,
      };
    },
    destroy() {
      if (destroyed) return;
      send("bridge.destroyed", {});
      destroyed = true;
      iframe.removeEventListener("load", connect);
      port?.close();
      port = undefined;
    },
  };
}
