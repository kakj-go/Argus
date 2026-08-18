// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { CardManifest, RenderPlan } from "@argus/api-client/contracts";
import { CARD_BRIDGE_VERSION, CARD_MAX_MESSAGE_BYTES } from "./protocol";
import { SandboxCard } from "./react";

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const HTML = "<p>card</p>";
const CONTENT_HASH = "a".repeat(64);

const MANIFEST: CardManifest = {
  schema_version: "argus.card_manifest/v1",
  card_id: "card-1",
  revision: 1,
  source: "system",
  entrypoint_hash: CONTENT_HASH,
  bridge_version: CARD_BRIDGE_VERSION,
  slots: [],
  allowed_resources: [],
  supported_locales: ["zh-CN"],
  default_locale: "zh-CN",
  supported_color_schemes: ["light"],
  max_message_bytes: CARD_MAX_MESSAGE_BYTES,
};

const PLAN: RenderPlan = {
  schema_version: "argus.render_plan/v1",
  card_id: "card-1",
  card_revision: 1,
  card_instance_id: "instance-1",
  data_bindings: [],
  query_binding_ids: {},
  action_binding_ids: {},
  locale: "zh-CN",
  color_scheme: "light",
};

describe("SandboxCard", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("waits for the iframe's real load event before starting the bridge", async () => {
    const bytes = Uint8Array.from({ length: 32 }, () => 0xaa).buffer;
    let resolveDigest: ((value: ArrayBuffer) => void) | undefined;
    vi.spyOn(crypto.subtle, "digest").mockReturnValue(new Promise((resolve) => {
      resolveDigest = resolve;
    }));

    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    await act(async () => {
      root.render(
        <SandboxCard
          card_origin="https://cards.example.test/runtime"
          manifest={MANIFEST}
          render_plan={PLAN}
          html={HTML}
          locale="zh-CN"
          color_scheme="light"
        />,
      );
    });

    const iframe = container.querySelector("iframe");
    expect(iframe).not.toBeNull();
    const dispatch = vi.spyOn(iframe!, "dispatchEvent");

    await act(async () => {
      resolveDigest?.(bytes);
      await Promise.resolve();
    });

    expect(dispatch).not.toHaveBeenCalledWith(expect.objectContaining({ type: "load" }));
    await act(async () => root.unmount());
  });
});
