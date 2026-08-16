// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CardManifest, RenderPlan } from "@argus/api-client/contracts";
import {
  CARD_BRIDGE_VERSION,
  CARD_MAX_MESSAGE_BYTES,
  encodedMessageBytes,
  isCardBridgeEnvelope,
} from "./protocol";
import { createCardHost } from "./sandbox-host";

class FakePort {
  peer?: FakePort;
  onmessage: ((event: MessageEvent) => void) | null = null;
  closed = false;
  start() {}
  close() { this.closed = true; }
  postMessage(data: unknown) {
    if (!this.closed) this.peer?.onmessage?.(new MessageEvent("message", { data }));
  }
}

class FakeMessageChannel {
  port1 = new FakePort();
  port2 = new FakePort();
  constructor() {
    this.port1.peer = this.port2;
    this.port2.peer = this.port1;
  }
}

const MANIFEST: CardManifest = {
  schema_version: "argus.card_manifest/v1",
  card_id: "card-1",
  revision: 1,
  source: "system",
  entrypoint_hash: "sha256:abc",
  bridge_version: CARD_BRIDGE_VERSION,
  slots: [],
  allowed_resources: [],
  supported_locales: ["zh-CN"],
  default_locale: "zh-CN",
  supported_color_schemes: ["dark"],
  max_message_bytes: CARD_MAX_MESSAGE_BYTES,
};

const PLAN: RenderPlan = {
  schema_version: "argus.render_plan/v1",
  card_id: "card-1",
  card_revision: 1,
  card_instance_id: "instance-1",
  data_bindings: [],
  query_binding_ids: { list: "query-1" },
  action_binding_ids: { commit: "action-1" },
  locale: "zh-CN",
  color_scheme: "dark",
};

const NONCE = "nonce-1234567890";

function message(type: string, sequence: number, payload: Record<string, unknown>, nonce = NONCE) {
  return {
    bridge_version: CARD_BRIDGE_VERSION,
    message_id: `message-${sequence}`,
    nonce,
    sequence,
    type,
    payload,
  };
}

describe("card bridge envelope", () => {
  it("accepts the generated snake_case envelope", () => {
    expect(isCardBridgeEnvelope(message("card.ready", 1, {}))).toBe(true);
  });

  it("rejects old versions and invalid sequences", () => {
    expect(isCardBridgeEnvelope({ ...message("card.ready", 1, {}), bridge_version: "argus.card-bridge.v1" })).toBe(false);
    expect(isCardBridgeEnvelope(message("card.ready", 0, {}))).toBe(false);
  });

  it("measures serialized bytes for the 1 MiB gate", () => {
    expect(encodedMessageBytes({ value: "x".repeat(32) })).toBeGreaterThan(32);
  });
});

describe("createCardHost", () => {
  const OriginalMessageChannel = globalThis.MessageChannel;

  beforeEach(() => {
    Object.defineProperty(globalThis, "MessageChannel", {
      configurable: true,
      value: FakeMessageChannel,
    });
  });

  afterEach(() => {
    Object.defineProperty(globalThis, "MessageChannel", {
      configurable: true,
      value: OriginalMessageChannel,
    });
    document.body.innerHTML = "";
  });

  it("uses one exact-origin hello and moves business traffic to MessagePort", async () => {
    const iframe = document.createElement("iframe");
    document.body.append(iframe);
    const post = vi.spyOn(iframe.contentWindow!, "postMessage");
    const onQueryInvoke = vi.fn().mockResolvedValue({ ok: true });
    const host = createCardHost(iframe, {
      card_origin: "https://cards.example.test/runtime",
      manifest: MANIFEST,
      render_plan: PLAN,
      html: "<p>card</p>",
      nonce: NONCE,
      context: { locale: "zh-CN", color_scheme: "dark", design_tokens: {} },
      onQueryInvoke,
    });
    iframe.dispatchEvent(new Event("load"));
    expect(post).toHaveBeenCalledTimes(1);
    expect(post.mock.calls[0]?.[1]).toBe("https://cards.example.test");
    const calls = post.mock.calls as unknown as Array<[unknown, string, FakePort[]]>;
    const runtimePort = calls[0]?.[2]?.[0] as FakePort;
    runtimePort.postMessage(message("card.ready", 1, {}));
    runtimePort.postMessage(message("query.invoke", 2, { request_id: "request-123456789", query_binding_id: "query-1" }));
    await Promise.resolve();
    expect(onQueryInvoke).toHaveBeenCalledWith("query-1");
    expect(post).toHaveBeenCalledTimes(1);
    host.destroy();
  });

  it("rejects bad nonce, duplicate/out-of-order sequences, forged binding, oversized, and post-destroy messages", () => {
    const iframe = document.createElement("iframe");
    document.body.append(iframe);
    const post = vi.spyOn(iframe.contentWindow!, "postMessage");
    const violations = vi.fn();
    const host = createCardHost(iframe, {
      card_origin: "https://cards.example.test",
      manifest: MANIFEST,
      render_plan: PLAN,
      html: "<p>card</p>",
      nonce: NONCE,
      context: { locale: "zh-CN", color_scheme: "dark", design_tokens: {} },
      onProtocolViolation: violations,
    });
    iframe.dispatchEvent(new Event("load"));
    const calls = post.mock.calls as unknown as Array<[unknown, string, FakePort[]]>;
    const runtimePort = calls[0]?.[2]?.[0] as FakePort;
    runtimePort.postMessage(message("card.ready", 1, {}, "wrong-12345678901"));
    runtimePort.postMessage(message("card.ready", 1, {}));
    runtimePort.postMessage(message("card.ready", 1, {}));
    runtimePort.postMessage(message("card.resize", 3, { height: 200 }));
    runtimePort.postMessage(message("card.resize", 2, { height: 100 }));
    runtimePort.postMessage(message("action.invoke", 4, { request_id: "request-123456789", action_binding_id: "forged" }));
    runtimePort.postMessage(message("card.resize", 5, { value: "x".repeat(CARD_MAX_MESSAGE_BYTES) }));
    host.destroy();
    runtimePort.postMessage(message("card.ready", 6, {}));
    expect(violations.mock.calls.map((call) => call[0])).toEqual([
      "nonce_mismatch",
      "sequence_replay",
      "sequence_replay",
      "binding_not_allowed",
      "message_too_large",
      "destroyed",
    ]);
  });
});
