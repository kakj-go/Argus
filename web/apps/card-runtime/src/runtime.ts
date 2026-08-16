import type { BridgeMessage, RenderPlan } from "@argus/api-client/contracts";

export const BRIDGE_VERSION = "argus.card_bridge/v1" as const;
export const MAX_MESSAGE_BYTES = 1024 * 1024;

const ALLOWED_RESOURCES = new Set([
  "inline_style",
  "inline_script",
  "image_data",
  "font_data",
]);

export type CardRuntimeContext = {
  locale: "zh-CN" | "en-US";
  color_scheme: "light" | "dark";
  design_tokens: Record<string, string>;
};

export type HelloPayload = {
  bridge_version: typeof BRIDGE_VERSION;
  message_id: string;
  type: "host.hello";
  nonce: string;
  sequence: 1;
  payload: {
    html: string;
    entrypoint_hash: string;
    allowed_resources: string[];
    max_message_bytes: number;
    locale: "zh-CN" | "en-US";
    color_scheme: "light" | "dark";
    render_plan: RenderPlan;
    initial_data: Record<string, unknown>;
  };
};

export interface ArgusCardApi {
  readonly context: CardRuntimeContext;
  readonly data: Record<string, unknown>;
  query(bindingId: string): Promise<unknown>;
  action(bindingId: string): Promise<unknown>;
  onContext(listener: (context: CardRuntimeContext) => void): () => void;
  onData(listener: (data: Record<string, unknown>) => void): () => void;
  resize(height?: number): void;
}

declare global {
  interface Window {
    argusCard?: ArgusCardApi;
  }
}

export function byteLength(value: unknown): number {
  try {
    return new TextEncoder().encode(JSON.stringify(value)).byteLength;
  } catch {
    return Number.POSITIVE_INFINITY;
  }
}

export async function sha256(value: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(value),
  );
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export async function verifyEntrypointHash(
  html: string,
  entrypointHash: string,
): Promise<boolean> {
  const expected = expectedSha256(entrypointHash);
  return expected !== null && (await sha256(html)) === expected;
}

export function expectedSha256(value: string): string | null {
  const match = /^sha256:([0-9a-f]{64})$/.exec(value);
  return match?.[1] ?? null;
}

export function buildCardCsp(allowed: string[]): string {
  const script = allowed.includes("inline_script") ? "'unsafe-inline'" : "'none'";
  const style = allowed.includes("inline_style") ? "'unsafe-inline'" : "'none'";
  const image = allowed.includes("image_data") ? "data:" : "'none'";
  const font = allowed.includes("font_data") ? "data:" : "'none'";
  return [
    "default-src 'none'",
    `script-src ${script}`,
    `style-src ${style}`,
    `img-src ${image}`,
    `font-src ${font}`,
    "connect-src 'none'",
    "frame-src 'none'",
    "object-src 'none'",
    "worker-src 'none'",
    "base-uri 'none'",
    "form-action 'none'",
  ].join("; ");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isHelloPayload(value: unknown): value is HelloPayload {
  if (!isRecord(value) || byteLength(value) > MAX_MESSAGE_BYTES) return false;
  if (
    value.bridge_version !== BRIDGE_VERSION ||
    value.type !== "host.hello" ||
    value.sequence !== 1 ||
    typeof value.message_id !== "string" ||
    !value.message_id ||
    typeof value.nonce !== "string" ||
    value.nonce.length < 16 ||
    !isRecord(value.payload)
  ) return false;
  const payload = value.payload;
  return (
    typeof payload.html === "string" &&
    typeof payload.entrypoint_hash === "string" &&
    expectedSha256(payload.entrypoint_hash) !== null &&
    Array.isArray(payload.allowed_resources) &&
    payload.allowed_resources.length <= ALLOWED_RESOURCES.size &&
    payload.allowed_resources.every(
      (resource) => typeof resource === "string" && ALLOWED_RESOURCES.has(resource),
    ) &&
    new Set(payload.allowed_resources).size === payload.allowed_resources.length &&
    Number.isInteger(payload.max_message_bytes) &&
    Number(payload.max_message_bytes) >= 1024 &&
    Number(payload.max_message_bytes) <= MAX_MESSAGE_BYTES &&
    (payload.locale === "zh-CN" || payload.locale === "en-US") &&
    (payload.color_scheme === "light" || payload.color_scheme === "dark") &&
    isRecord(payload.render_plan) &&
    payload.render_plan.schema_version === "argus.render_plan/v1" &&
    isRecord(payload.initial_data)
  );
}

export function isTrustedParentMessage(
  event: Pick<MessageEvent<unknown>, "origin" | "source" | "ports">,
  parentOrigin: string | null,
  parentWindow: Window,
): boolean {
  return (
    Boolean(parentOrigin) &&
    event.origin === parentOrigin &&
    event.source === parentWindow &&
    event.ports.length === 1
  );
}

function isBridgeMessage(value: unknown, maxBytes: number): value is BridgeMessage {
  if (!isRecord(value) || byteLength(value) > maxBytes) return false;
  return (
    value.bridge_version === BRIDGE_VERSION &&
    typeof value.message_id === "string" &&
    typeof value.nonce === "string" &&
    value.nonce.length >= 16 &&
    Number.isSafeInteger(value.sequence) &&
    Number(value.sequence) > 0 &&
    typeof value.type === "string" &&
    isRecord(value.payload)
  );
}

function requestError(value: unknown): Error {
  const detail = isRecord(value) ? value : {};
  const error = isRecord(detail.error) ? detail.error : {};
  return new Error(
    typeof error.message === "string" ? error.message : "Card binding failed",
  );
}

export function createCardApi(
  hello: HelloPayload,
  port: MessagePort,
): ArgusCardApi {
  const maxBytes = Math.min(hello.payload.max_message_bytes, MAX_MESSAGE_BYTES);
  const queryBindings = new Set(Object.values(hello.payload.render_plan.query_binding_ids));
  const actionBindings = new Set(Object.values(hello.payload.render_plan.action_binding_ids));
  const pending = new Map<string, { resolve(value: unknown): void; reject(reason: unknown): void }>();
  const contextListeners = new Set<(context: CardRuntimeContext) => void>();
  const dataListeners = new Set<(data: Record<string, unknown>) => void>();
  let incoming = 1;
  let outgoing = 1;
  let destroyed = false;
  let context: CardRuntimeContext = {
    locale: hello.payload.locale,
    color_scheme: hello.payload.color_scheme,
    design_tokens: {},
  };
  let data = hello.payload.initial_data;

  const send = (type: BridgeMessage["type"], payload: Record<string, unknown>) => {
    if (destroyed) throw new Error("Card bridge is destroyed");
    outgoing += 1;
    const message: BridgeMessage = {
      bridge_version: BRIDGE_VERSION,
      message_id: crypto.randomUUID(),
      nonce: hello.nonce,
      sequence: outgoing,
      type,
      payload,
    };
    if (byteLength(message) > maxBytes) throw new Error("Card bridge message is too large");
    port.postMessage(message);
  };

  const invoke = (kind: "query" | "action", bindingId: string) => {
    const bindings = kind === "query" ? queryBindings : actionBindings;
    if (!bindings.has(bindingId)) {
      return Promise.reject(new Error("Binding is not available to this card"));
    }
    const requestId = crypto.randomUUID();
    return new Promise<unknown>((resolve, reject) => {
      pending.set(requestId, { resolve, reject });
      try {
        send(`${kind}.invoke`, {
          request_id: requestId,
          [`${kind}_binding_id`]: bindingId,
        });
      } catch (error) {
        pending.delete(requestId);
        reject(error);
      }
    });
  };

  port.onmessage = (event: MessageEvent<unknown>) => {
    if (!isBridgeMessage(event.data, maxBytes)) return;
    const message = event.data;
    if (message.nonce !== hello.nonce || message.sequence <= incoming) return;
    incoming = message.sequence;
    const payload = message.payload as Record<string, unknown>;
    if (message.type === "binding.result" && typeof payload.request_id === "string") {
      const request = pending.get(payload.request_id);
      if (!request) return;
      pending.delete(payload.request_id);
      if (payload.ok === true) request.resolve(payload.data);
      else request.reject(requestError(payload));
    } else if (message.type === "host.context") {
      if (
        (payload.locale === "zh-CN" || payload.locale === "en-US") &&
        (payload.color_scheme === "light" || payload.color_scheme === "dark")
      ) {
        context = {
          locale: payload.locale,
          color_scheme: payload.color_scheme,
          design_tokens: isRecord(payload.design_tokens)
            ? payload.design_tokens as Record<string, string>
            : {},
        };
        applyContext(context);
        contextListeners.forEach((listener) => listener(context));
      }
    } else if (message.type === "data.update" && isRecord(payload.data)) {
      data = payload.data;
      dataListeners.forEach((listener) => listener(data));
    } else if (message.type === "bridge.destroyed") {
      destroyed = true;
      pending.forEach((request) => request.reject(new Error("Card bridge was destroyed")));
      pending.clear();
      port.close();
    }
  };
  port.start();

  const api: ArgusCardApi = {
    get context() { return context; },
    get data() { return data; },
    query: (bindingId) => invoke("query", bindingId),
    action: (bindingId) => invoke("action", bindingId),
    onContext(listener) {
      contextListeners.add(listener);
      return () => contextListeners.delete(listener);
    },
    onData(listener) {
      dataListeners.add(listener);
      return () => dataListeners.delete(listener);
    },
    resize(height = document.documentElement.scrollHeight) {
      send("card.resize", { height: Math.max(1, Math.ceil(height)) });
    },
  };
  applyContext(context);
  return api;
}

export function applyContext(context: CardRuntimeContext): void {
  document.documentElement.lang = context.locale;
  document.documentElement.dataset.colorScheme = context.color_scheme;
  document.documentElement.style.colorScheme = context.color_scheme;
  for (const [name, value] of Object.entries(context.design_tokens)) {
    if (name.startsWith("--")) document.documentElement.style.setProperty(name, value);
  }
}

export function renderCardHtml(
  root: HTMLElement,
  html: string,
  allowInlineScripts: boolean,
): void {
  const template = document.createElement("template");
  template.innerHTML = html;
  const scripts = [...template.content.querySelectorAll("script")];
  for (const script of scripts) script.remove();
  root.replaceChildren(template.content.cloneNode(true));
  if (!allowInlineScripts) return;
  for (const source of scripts) {
    if (source.src) throw new Error("External card scripts are not allowed");
    const script = document.createElement("script");
    if (source.type) script.type = source.type;
    script.textContent = source.textContent;
    root.append(script);
  }
}
