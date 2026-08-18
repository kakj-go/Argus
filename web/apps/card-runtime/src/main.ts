import {
  BRIDGE_VERSION,
  buildCardCsp,
  createCardRuntimeSession,
  isHelloPayload,
  isTrustedParentMessage,
  renderCardHtml,
  verifyEntrypointHash,
  type HelloPayload,
} from "./runtime";

let connected = false;

type AxeRuntime = typeof import("axe-core");

async function loadAxeRuntime(): Promise<AxeRuntime> {
  const [left, right] = await Promise.all([
    import("virtual:argus-axe-part-1"),
    import("virtual:argus-axe-part-2"),
  ]);
  const script = document.createElement("script");
  script.textContent = `${left.default}${right.default}\n//# sourceURL=argus-card-runtime-axe.js`;
  document.head.append(script);
  script.remove();
  const axe = (globalThis as typeof globalThis & { axe?: AxeRuntime }).axe;
  if (!axe) throw new Error("axe runtime failed to initialize");
  return axe;
}

type ValidationCounters = {
  protocol_violations: number;
  runtime_errors: number;
};

function observeValidationFailures(enabled: boolean): {
  counters: ValidationCounters;
  cleanup(): void;
} {
  const counters = { protocol_violations: 0, runtime_errors: 0 };
  if (!enabled) return { counters, cleanup() {} };
  const onError = () => { counters.runtime_errors += 1; };
  const onRejection = () => { counters.runtime_errors += 1; };
  const onCspViolation = () => { counters.protocol_violations += 1; };
  window.addEventListener("error", onError);
  window.addEventListener("unhandledrejection", onRejection);
  document.addEventListener("securitypolicyviolation", onCspViolation);
  return {
    counters,
    cleanup() {
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
      document.removeEventListener("securitypolicyviolation", onCspViolation);
    },
  };
}

async function settleRenderedCard(): Promise<void> {
  await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
  await new Promise<void>((resolve) => setTimeout(resolve, 0));
}

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
  const observed = observeValidationFailures(Boolean(hello.payload.validation));
  const axeRuntime = hello.payload.validation ? loadAxeRuntime() : undefined;
  void verifyEntrypointHash(hello.payload.html, hello.payload.entrypoint_hash)
    .then(async (validHash) => {
      if (!validHash) {
        sendError(port, hello, "ENTRYPOINT_HASH_MISMATCH");
        port.close();
        return;
      }
      const axe = axeRuntime ? await axeRuntime : undefined;
      const policy = document.createElement("meta");
      policy.httpEquiv = "Content-Security-Policy";
      policy.content = buildCardCsp(hello.payload.allowed_resources);
      document.head.prepend(policy);
      const session = createCardRuntimeSession(hello, port);
      window.argusCard = session.api;
      const root = document.getElementById("card-root")!;
      renderCardHtml(
        root,
        hello.payload.html,
        hello.payload.allowed_resources.includes("inline_script"),
      );
      session.ready();
      const observer = new ResizeObserver(() => window.argusCard?.resize());
      observer.observe(root);
      window.argusCard.resize();
      const validation = hello.payload.validation;
      if (validation && axe) {
        await settleRenderedCard();
        let seriousA11yViolations = 0;
        try {
          const results = await axe.run(root, {
            runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"] },
          });
          seriousA11yViolations = results.violations.filter(
            (item) => item.impact === "serious" || item.impact === "critical",
          ).length;
        } catch {
          observed.counters.runtime_errors += 1;
        }
        const presentSlots = new Set(
          [...root.querySelectorAll<HTMLElement>("[data-slot]")]
            .map((element) => element.dataset.slot)
            .filter((slot): slot is string => Boolean(slot)),
        );
        const missingRequiredSlots = validation.required_slots
          .filter((slot) => !presentSlots.has(slot));
        session.reportValidation({
          content_hash: validation.content_hash,
          runtime_version: validation.runtime_version,
          nonce: hello.nonce,
          scenario: validation.scenario,
          ready: true,
          protocol_violations: observed.counters.protocol_violations,
          runtime_errors: observed.counters.runtime_errors,
          serious_a11y_violations: seriousA11yViolations,
          missing_required_slots: [...new Set(missingRequiredSlots)].sort(),
          size_violation: root.scrollHeight > 2000 || root.scrollWidth > document.documentElement.clientWidth + 1,
        });
        observed.cleanup();
      }
    })
    .catch(() => {
      observed.counters.runtime_errors += 1;
      sendError(port, hello, "CARD_RUNTIME_FAILED");
      observed.cleanup();
      port.close();
    });
});
