import { describe, expect, it, vi } from "vitest";
import {
  consumeSetupTokenFromFragment,
  createSetupCredentialStore,
  listenForSetupTokenFragments,
} from "./setup-credential";

describe("consumeSetupTokenFromFragment", () => {
  it("returns the setup token and removes it from the URL", () => {
    const replaceState = vi.fn();
    const token = consumeSetupTokenFromFragment(
      {
        pathname: "/login",
        search: "?source=installer",
        hash: "#argus_setup_token=token%2Fvalue",
      },
      { state: { navigation: true }, replaceState },
    );

    expect(token).toBe("token/value");
    expect(replaceState).toHaveBeenCalledWith(
      { navigation: true },
      "",
      "/login?source=installer",
    );
  });

  it("preserves unrelated fragment values", () => {
    const replaceState = vi.fn();
    consumeSetupTokenFromFragment(
      {
        pathname: "/login",
        search: "",
        hash: "#argus_setup_token=token&locale=zh-CN",
      },
      { state: null, replaceState },
    );

    expect(replaceState).toHaveBeenCalledWith(null, "", "/login#locale=zh-CN");
  });

  it("does not rewrite the URL when the credential is missing", () => {
    const replaceState = vi.fn();
    expect(
      consumeSetupTokenFromFragment(
        { pathname: "/login", search: "", hash: "" },
        { state: null, replaceState },
      ),
    ).toBe("");
    expect(replaceState).not.toHaveBeenCalled();
  });
});

describe("setup credential store", () => {
  it("accepts a setup link opened in the current tab and keeps it in memory", () => {
    const replaceState = vi.fn();
    const listeners = new Set<() => void>();
    const location = {
      pathname: "/login",
      search: "?initialized=false",
      hash: "",
    };
    const target = {
      location,
      history: { state: null, replaceState },
      addEventListener: (_type: "hashchange", listener: () => void) =>
        listeners.add(listener),
      removeEventListener: (_type: "hashchange", listener: () => void) =>
        listeners.delete(listener),
    };
    const store = createSetupCredentialStore();
    const subscriber = vi.fn();
    store.subscribe(subscriber);
    const stop = listenForSetupTokenFragments(target, store);

    location.hash = "#argus_setup_token=same-tab-token";
    for (const listener of listeners) listener();

    expect(store.getSnapshot()).toBe("same-tab-token");
    expect(replaceState).toHaveBeenCalledWith(
      null,
      "",
      "/login?initialized=false",
    );
    expect(subscriber).toHaveBeenCalledOnce();

    stop();
    expect(listeners).toHaveLength(0);
  });

  it("clears the in-memory credential after use", () => {
    const store = createSetupCredentialStore();
    store.consumeFromFragment(
      {
        pathname: "/login",
        search: "",
        hash: "#argus_setup_token=one-time-token",
      },
      { state: null, replaceState: vi.fn() },
    );

    store.clear();

    expect(store.getSnapshot()).toBe("");
  });
});
