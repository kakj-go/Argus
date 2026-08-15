// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CARD_RUNTIME_MARKER, injectCardRuntime } from "./card-runtime";
import { CARD_BRIDGE_VERSION, type CardPresentationContext } from "./protocol";
import { SandboxCard } from "./react";
import { createCardHost } from "./sandbox-host";

const CONTEXT: CardPresentationContext = {
  locale: "zh-CN",
  theme: "dark",
  colorScheme: "dark",
  designTokens: { "--accent": "#dc7654" },
};

afterEach(cleanup);

function createIframe() {
  const iframe = document.createElement("iframe");
  document.body.appendChild(iframe);
  return iframe;
}

/** Simulates a postMessage coming from inside the card iframe. */
function dispatchFromCard(
  iframe: HTMLIFrameElement,
  data: Record<string, unknown>,
) {
  window.dispatchEvent(
    new MessageEvent("message", {
      data: {
        version: CARD_BRIDGE_VERSION,
        cardInstanceId: "card-1",
        nonce: "nonce-1",
        ...data,
      },
      origin: "null",
      source: iframe.contentWindow,
    }),
  );
}

function postedMessages(
  spy: ReturnType<typeof vi.spyOn>,
): Record<string, unknown>[] {
  return spy.mock.calls.map((call) => call[0] as Record<string, unknown>);
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("injectCardRuntime", () => {
  it("injects the bridge script after <head> and preserves content", () => {
    const html =
      '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body><p>hi</p></body></html>';
    const result = injectCardRuntime(html, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
    });
    expect(result).toContain(CARD_RUNTIME_MARKER);
    expect(result).toContain("<p>hi</p>");
    expect(result).toContain('"cardInstanceId":"card-1"');
    expect(result).toContain('"nonce":"nonce-1"');
    expect(result.indexOf(CARD_RUNTIME_MARKER)).toBeGreaterThan(
      result.indexOf("<head>"),
    );
    expect(result.indexOf(CARD_RUNTIME_MARKER)).toBeLessThan(
      result.indexOf("</head>"),
    );
  });

  it("prepends the script when no <head> exists", () => {
    const result = injectCardRuntime("<p>fragment</p>", {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
    });
    expect(result.startsWith(`<script ${CARD_RUNTIME_MARKER}>`)).toBe(true);
    expect(result).toContain("window.argus");
  });
});

describe("createCardHost", () => {
  it("completes the nonce handshake and sends host.init", () => {
    const iframe = createIframe();
    const spy = vi.spyOn(iframe.contentWindow!, "postMessage");
    const onReady = vi.fn();
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      bindings: { queryBindingIds: ["qb-1"], actionBindingIds: ["ab-1"] },
      initialData: { title: "Hosts" },
      context: CONTEXT,
      onReady,
    });
    expect(host.ready).toBe(false);
    dispatchFromCard(iframe, { type: "card.ready", sequence: 1 });
    expect(host.ready).toBe(true);
    expect(onReady).toHaveBeenCalledOnce();
    const init = postedMessages(spy).find((m) => m.type === "host.init");
    expect(init).toBeDefined();
    expect(init).toMatchObject({
      version: CARD_BRIDGE_VERSION,
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      sequence: 1,
      bindings: { queryBindingIds: ["qb-1"], actionBindingIds: ["ab-1"] },
      initialData: { title: "Hosts" },
      context: CONTEXT,
    });
    host.destroy();
  });

  it("rejects messages with a wrong nonce", () => {
    const iframe = createIframe();
    const spy = vi.spyOn(iframe.contentWindow!, "postMessage");
    const onProtocolViolation = vi.fn();
    const onReady = vi.fn();
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      context: CONTEXT,
      onProtocolViolation,
      onReady,
    });
    dispatchFromCard(iframe, {
      type: "card.ready",
      sequence: 1,
      nonce: "forged",
    });
    expect(host.ready).toBe(false);
    expect(onReady).not.toHaveBeenCalled();
    expect(onProtocolViolation).toHaveBeenCalledWith(
      "nonce_mismatch",
      expect.objectContaining({ nonce: "forged" }),
    );
    expect(postedMessages(spy)).toHaveLength(0);
    host.destroy();
  });

  it("rejects replayed or out-of-order sequences", () => {
    const iframe = createIframe();
    vi.spyOn(iframe.contentWindow!, "postMessage");
    const onProtocolViolation = vi.fn();
    const onResize = vi.fn();
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      context: CONTEXT,
      onProtocolViolation,
      onResize,
    });
    dispatchFromCard(iframe, { type: "card.ready", sequence: 1 });
    dispatchFromCard(iframe, { type: "card.resize", sequence: 2, height: 200 });
    expect(onResize).toHaveBeenCalledWith(200);
    // Replay of sequence 2 must be dropped.
    dispatchFromCard(iframe, { type: "card.resize", sequence: 2, height: 999 });
    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onProtocolViolation).toHaveBeenCalledWith(
      "sequence_replay",
      expect.objectContaining({ sequence: 2 }),
    );
    host.destroy();
  });

  it("ignores foreign sources and non-bridge messages silently", () => {
    const iframe = createIframe();
    const onProtocolViolation = vi.fn();
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      context: CONTEXT,
      onProtocolViolation,
    });
    window.dispatchEvent(
      new MessageEvent("message", { data: { unrelated: true } }),
    );
    expect(onProtocolViolation).not.toHaveBeenCalled();
    expect(host.ready).toBe(false);
    host.destroy();
  });

  it("caches setContext before ready and pushes host.context after", () => {
    const iframe = createIframe();
    const spy = vi.spyOn(iframe.contentWindow!, "postMessage");
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      context: CONTEXT,
    });
    const light: CardPresentationContext = {
      ...CONTEXT,
      theme: "light",
      colorScheme: "light",
    };
    host.setContext(light);
    expect(postedMessages(spy)).toHaveLength(0);
    dispatchFromCard(iframe, { type: "card.ready", sequence: 1 });
    const init = postedMessages(spy).find((m) => m.type === "host.init");
    expect(init).toMatchObject({ context: light });
    host.setContext({ ...light, locale: "en-US" });
    const contextPush = postedMessages(spy).find(
      (m) => m.type === "host.context",
    );
    expect(contextPush).toMatchObject({
      context: { ...light, locale: "en-US" },
    });
    host.destroy();
  });

  it("routes query.invoke to the handler and replies with query.result", async () => {
    const iframe = createIframe();
    const spy = vi.spyOn(iframe.contentWindow!, "postMessage");
    const onQueryInvoke = vi.fn().mockResolvedValue({ items: [1, 2] });
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      bindings: { queryBindingIds: ["qb-1"] },
      context: CONTEXT,
      onQueryInvoke,
    });
    dispatchFromCard(iframe, { type: "card.ready", sequence: 1 });
    dispatchFromCard(iframe, {
      type: "query.invoke",
      sequence: 2,
      requestId: "r1",
      queryBindingId: "qb-1",
      params: { limit: 10 },
    });
    expect(onQueryInvoke).toHaveBeenCalledWith("qb-1", { limit: 10 });
    await flush();
    const result = postedMessages(spy).find((m) => m.type === "query.result");
    expect(result).toMatchObject({ requestId: "r1", data: { items: [1, 2] } });
    host.destroy();
  });

  it("rejects invokes outside the binding allowlist", () => {
    const iframe = createIframe();
    const spy = vi.spyOn(iframe.contentWindow!, "postMessage");
    const onQueryInvoke = vi.fn();
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      bindings: { queryBindingIds: ["qb-1"] },
      context: CONTEXT,
      onQueryInvoke,
    });
    dispatchFromCard(iframe, { type: "card.ready", sequence: 1 });
    dispatchFromCard(iframe, {
      type: "query.invoke",
      sequence: 2,
      requestId: "r2",
      queryBindingId: "qb-other",
    });
    expect(onQueryInvoke).not.toHaveBeenCalled();
    const error = postedMessages(spy).find((m) => m.type === "query.error");
    expect(error).toMatchObject({
      requestId: "r2",
      error: { code: "BINDING_NOT_ALLOWED" },
    });
    host.destroy();
  });

  it("clamps resize to maxHeight", () => {
    const iframe = createIframe();
    const onResize = vi.fn();
    const host = createCardHost(iframe, {
      cardInstanceId: "card-1",
      nonce: "nonce-1",
      context: CONTEXT,
      maxHeight: 500,
      onResize,
    });
    dispatchFromCard(iframe, { type: "card.ready", sequence: 1 });
    dispatchFromCard(iframe, {
      type: "card.resize",
      sequence: 2,
      height: 100000,
    });
    expect(onResize).toHaveBeenCalledWith(500);
    host.destroy();
  });
});

function extractNonce(iframe: HTMLElement): string {
  const srcdoc = iframe.getAttribute("srcdoc") ?? "";
  const match =
    /\\"nonce\\":\\"([^\\"]+)\\"/.exec(srcdoc) ??
    /"nonce":"([^"]+)"/.exec(srcdoc);
  if (!match?.[1]) throw new Error("nonce not found in srcdoc");
  return match[1];
}

describe("SandboxCard", () => {
  it("renders a sandboxed iframe with the injected runtime", () => {
    render(
      <SandboxCard
        cardInstanceId="card-1"
        html="<p>hello card</p>"
        title="主机概览"
      />,
    );
    const iframe = screen.getByTitle("主机概览");
    expect(iframe).toHaveAttribute("sandbox", "allow-scripts");
    expect(iframe).toHaveAttribute("referrerpolicy", "no-referrer");
    const srcdoc = iframe.getAttribute("srcdoc") ?? "";
    expect(srcdoc).toContain(CARD_RUNTIME_MARKER);
    expect(srcdoc).toContain("<p>hello card</p>");
    expect(iframe).toHaveStyle({ height: "120px" });
  });

  it("auto-resizes on card.resize within bounds", () => {
    render(
      <SandboxCard cardInstanceId="card-1" html="<p>x</p>" title="card" />,
    );
    const iframe = screen.getByTitle("card") as HTMLIFrameElement;
    const nonce = extractNonce(iframe);
    act(() => {
      dispatchFromCard(iframe, { type: "card.ready", sequence: 1, nonce });
      dispatchFromCard(iframe, {
        type: "card.resize",
        sequence: 2,
        nonce,
        height: 240,
      });
    });
    expect(iframe).toHaveStyle({ height: "240px" });
    act(() => {
      dispatchFromCard(iframe, {
        type: "card.resize",
        sequence: 3,
        nonce,
        height: 99999,
      });
    });
    expect(iframe).toHaveStyle({ height: "2000px" });
  });

  it("forwards query.invoke and opens links in a controlled way", async () => {
    const onQueryInvoke = vi.fn().mockResolvedValue({ ok: true });
    const openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
    render(
      <SandboxCard
        bindings={{ queryBindingIds: ["qb-1"] }}
        cardInstanceId="card-1"
        html="<p>x</p>"
        onQueryInvoke={onQueryInvoke}
        theme="light"
        title="card"
      />,
    );
    const iframe = screen.getByTitle("card") as HTMLIFrameElement;
    const postSpy = vi.spyOn(iframe.contentWindow!, "postMessage");
    const nonce = extractNonce(iframe);
    act(() => {
      dispatchFromCard(iframe, { type: "card.ready", sequence: 1, nonce });
    });
    const init = postedMessages(postSpy).find((m) => m.type === "host.init");
    expect(init).toMatchObject({
      context: expect.objectContaining({ theme: "light", locale: "zh-CN" }),
    });
    act(() => {
      dispatchFromCard(iframe, {
        type: "query.invoke",
        sequence: 2,
        nonce,
        requestId: "r1",
        queryBindingId: "qb-1",
        params: { filter: { status: ["online"] } },
      });
      dispatchFromCard(iframe, {
        type: "link.open",
        sequence: 3,
        nonce,
        url: "https://example.com/doc",
      });
      dispatchFromCard(iframe, {
        type: "link.open",
        sequence: 4,
        nonce,
        url: "javascript:alert(1)",
      });
    });
    expect(onQueryInvoke).toHaveBeenCalledWith("qb-1", {
      filter: { status: ["online"] },
    });
    await act(flush);
    expect(
      postedMessages(postSpy).find((m) => m.type === "query.result"),
    ).toMatchObject({ requestId: "r1", data: { ok: true } });
    expect(openSpy).toHaveBeenCalledTimes(1);
    expect(openSpy).toHaveBeenCalledWith(
      "https://example.com/doc",
      "_blank",
      "noopener,noreferrer",
    );
    openSpy.mockRestore();
  });
});
