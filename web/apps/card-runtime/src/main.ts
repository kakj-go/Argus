import {
  BRIDGE_VERSION,
  buildCardCsp,
  createCardApi,
  isHelloPayload,
  isTrustedParentMessage,
  renderCardHtml,
  verifyEntrypointHash,
  type HelloPayload,
} from "./runtime";

let connected = false;

function sendError(port: MessagePort, hello: HelloPayload, code: string): void {
  port.postMessage({
    bridge_version: BRIDGE_VERSION,
    message_id: crypto.randomUUID(),
    type: "bridge.error",
    nonce: hello.nonce,
    sequence: 1,
    payload: { code },
  });
}

window.addEventListener("message", (event: MessageEvent<unknown>) => {
  const parentOrigin = new URLSearchParams(location.search).get("parent_origin");
  if (connected || !isTrustedParentMessage(event, parentOrigin, window.parent)) return;
  if (!isHelloPayload(event.data)) return;
  const hello = event.data;
  connected = true;
  const port = event.ports[0]!;
  void verifyEntrypointHash(hello.payload.html, hello.payload.entrypoint_hash)
    .then((validHash) => {
      if (!validHash) {
        sendError(port, hello, "ENTRYPOINT_HASH_MISMATCH");
        port.close();
        return;
      }
      const policy = document.createElement("meta");
      policy.httpEquiv = "Content-Security-Policy";
      policy.content = buildCardCsp(hello.payload.allowed_resources);
      document.head.prepend(policy);
      window.argusCard = createCardApi(hello, port);
      const root = document.getElementById("card-root")!;
      renderCardHtml(
        root,
        hello.payload.html,
        hello.payload.allowed_resources.includes("inline_script"),
      );
      port.postMessage({
        bridge_version: BRIDGE_VERSION,
        message_id: crypto.randomUUID(),
        type: "card.ready",
        nonce: hello.nonce,
        sequence: 1,
        payload: {},
      });
      const observer = new ResizeObserver(() => window.argusCard?.resize());
      observer.observe(root);
      window.argusCard.resize();
    })
    .catch(() => {
      sendError(port, hello, "CARD_RUNTIME_FAILED");
      port.close();
    });
});
