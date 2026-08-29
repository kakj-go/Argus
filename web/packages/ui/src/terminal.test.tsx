// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "./locale";
import { TerminalEmulator } from "./terminal";

const terminalHarness = vi.hoisted(() => {
  class FakeTerminal {
    static instances: FakeTerminal[] = [];
    writes: string[] = [];
    private dataHandler?: (value: string) => void;
    private resizeHandler?: (value: { cols: number; rows: number }) => void;

    constructor() {
      FakeTerminal.instances.push(this);
    }

    loadAddon() {}
    open() {}
    focus() {}
    clear() {}
    write(value: string) {
      this.writes.push(value);
    }
    onData(handler: (value: string) => void) {
      this.dataHandler = handler;
      return { dispose: () => { this.dataHandler = undefined; } };
    }
    onResize(handler: (value: { cols: number; rows: number }) => void) {
      this.resizeHandler = handler;
      return { dispose: () => { this.resizeHandler = undefined; } };
    }
    dispose() {}
  }
  class FakeFitAddon {
    fit() {}
  }
  return { FakeTerminal, FakeFitAddon };
});

vi.mock("@xterm/xterm", () => ({ Terminal: terminalHarness.FakeTerminal }));
vi.mock("@xterm/addon-fit", () => ({ FitAddon: terminalHarness.FakeFitAddon }));

describe("TerminalEmulator PTY mode", () => {
  beforeEach(() => {
    terminalHarness.FakeTerminal.instances = [];
    vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("creates one xterm and incrementally writes raw chunks", async () => {
    const { rerender } = render(
      <LocaleProvider>
        <TerminalEmulator
          lines={[{ kind: "stdout", content: "prompt> " }]}
          mode="pty"
          sessionId="session-1"
        />
      </LocaleProvider>,
    );

    await waitFor(() => expect(terminalHarness.FakeTerminal.instances).toHaveLength(1));
    const instance = terminalHarness.FakeTerminal.instances[0]!;
    expect(instance.writes).toEqual(["prompt> "]);

    rerender(
      <LocaleProvider>
        <TerminalEmulator
          lines={[
            { kind: "stdout", content: "prompt> " },
            { kind: "stdout", content: "next\r\n[2K" },
          ]}
          mode="pty"
          sessionId="session-1"
        />
      </LocaleProvider>,
    );

    await waitFor(() => expect(instance.writes).toEqual(["prompt> ", "next\r\n[2K"]));
    expect(terminalHarness.FakeTerminal.instances).toHaveLength(1);
  });
});
