// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import {
  BRIDGE_VERSION,
  MAX_MESSAGE_BYTES,
  buildCardCsp,
  createCardApi,
  expectedSha256,
  isHelloPayload,
  isTrustedParentMessage,
  sha256,
  verifyEntrypointHash,
  type HelloPayload,
} from "./runtime";

const HASH = `sha256:${"a".repeat(64)}`;
const NONCE = "nonce-1234567890";

function hello(): HelloPayload {
  return {
    bridge_version: BRIDGE_VERSION,
    message_id: "message-1",
    type: "host.hello",
    nonce: NONCE,
    sequence: 1,
    payload: {
      html: "<p>card</p>",
      entrypoint_hash: HASH,
      allowed_resources: ["inline_style"],
      max_message_bytes: MAX_MESSAGE_BYTES,
      locale: "zh-CN",
      color_scheme: "dark",
      render_plan: {
        schema_version: "argus.render_plan/v1",
        card_id: "card-1",
        card_revision: 1,
        card_instance_id: "instance-1",
        data_bindings: [],
        query_binding_ids: { list: "query-1" },
        action_binding_ids: { commit: "action-1" },
        locale: "zh-CN",
        color_scheme: "dark",
      },
      initial_data: { value: 1 },
    },
  };
}

describe("card runtime handshake", () => {
  it("accepts the frozen v1 hello envelope", () => {
    expect(isHelloPayload(hello())).toBe(true);
  });

  it("rejects malformed, oversized, duplicate-capability, and wrong-version messages", () => {
    expect(isHelloPayload({ ...hello(), payload: undefined })).toBe(false);
    expect(isHelloPayload({ ...hello(), bridge_version: "argus.card_bridge/v2" })).toBe(false);
    expect(
      isHelloPayload({
        ...hello(),
        payload: { ...hello().payload, allowed_resources: ["inline_style", "inline_style"] },
      }),
    ).toBe(false);
    expect(
      isHelloPayload({
        ...hello(),
        payload: { ...hello().payload, html: "x".repeat(MAX_MESSAGE_BYTES) },
      }),
    ).toBe(false);
  });

  it("requires a canonical sha256 entrypoint hash", () => {
    expect(expectedSha256(HASH)).toBe("a".repeat(64));
    expect(expectedSha256("a".repeat(64))).toBeNull();
    expect(expectedSha256(`sha256:${"z".repeat(64)}`)).toBeNull();
  });

  it("rejects an entrypoint whose content does not match the declared hash", async () => {
    const validHash = `sha256:${await sha256("<p>trusted</p>")}`;
    await expect(verifyEntrypointHash("<p>trusted</p>", validHash)).resolves.toBe(true);
    await expect(verifyEntrypointHash("<p>changed</p>", validHash)).resolves.toBe(false);
  });

  it("accepts only one port from the configured parent origin and window", () => {
    const parent = {} as Window;
    const port = {} as MessagePort;
    const event = { origin: "https://app.example.test", source: parent, ports: [port] };
    expect(isTrustedParentMessage(event, "https://app.example.test", parent)).toBe(true);
    expect(isTrustedParentMessage({ ...event, origin: "https://evil.example.test" }, "https://app.example.test", parent)).toBe(false);
    expect(isTrustedParentMessage({ ...event, source: {} as Window }, "https://app.example.test", parent)).toBe(false);
    expect(isTrustedParentMessage({ ...event, ports: [] }, "https://app.example.test", parent)).toBe(false);
  });
});

describe("card runtime bridge", () => {
  it("sends only binding IDs and resolves binding results", async () => {
    const channel = new MessageChannel();
    const received = new Promise<Record<string, unknown>>((resolve) => {
      channel.port2.onmessage = (event) => resolve(event.data as Record<string, unknown>);
      channel.port2.start();
    });
    const api = createCardApi(hello(), channel.port1);
    const pending = api.query("query-1");
    const invocation = await received;
    expect(invocation.type).toBe("query.invoke");
    const payload = invocation.payload as Record<string, unknown>;
    expect(payload.query_binding_id).toBe("query-1");
    expect(payload).not.toHaveProperty("params");
    channel.port2.postMessage({
      bridge_version: BRIDGE_VERSION,
      message_id: "result-1",
      nonce: NONCE,
      sequence: 2,
      type: "binding.result",
      payload: { request_id: payload.request_id, ok: true, data: { ok: true } },
    });
    await expect(pending).resolves.toEqual({ ok: true });
    await expect(api.action("forged")).rejects.toThrow("not available");
    channel.port1.close();
    channel.port2.close();
  });
});

describe("card runtime CSP", () => {
  it("denies scripts, network, frames, objects, workers, and forms by default", () => {
    const policy = buildCardCsp([]);
    expect(policy).toContain("default-src 'none'");
    expect(policy).toContain("script-src 'none'");
    expect(policy).toContain("connect-src 'none'");
    expect(policy).toContain("frame-src 'none'");
    expect(policy).toContain("object-src 'none'");
    expect(policy).toContain("worker-src 'none'");
    expect(policy).toContain("form-action 'none'");
  });

  it("opens only explicitly declared inline/data capabilities", () => {
    const policy = buildCardCsp([
      "inline_script",
      "inline_style",
      "image_data",
      "font_data",
    ]);
    expect(policy).toContain("script-src 'unsafe-inline'");
    expect(policy).toContain("style-src 'unsafe-inline'");
    expect(policy).toContain("img-src data:");
    expect(policy).toContain("font-src data:");
    expect(policy).toContain("connect-src 'none'");
  });
});
