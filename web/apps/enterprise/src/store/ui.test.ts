// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import {
  clampDockPercent,
  useUiStore,
  type TerminalDockPreference,
} from "./ui";

const storageKey = "argus.terminalDock";

function readStored(): TerminalDockPreference {
  return JSON.parse(window.localStorage.getItem(storageKey)!) as TerminalDockPreference;
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("clampDockPercent", () => {
  it("keeps values within the 20-80 range", () => {
    expect(clampDockPercent(5)).toBe(20);
    expect(clampDockPercent(50)).toBe(50);
    expect(clampDockPercent(95)).toBe(80);
    expect(clampDockPercent(Number.NaN)).toBe(50);
  });
});

describe("terminal dock preference", () => {
  it("falls back to bottom/50 for missing or invalid storage", () => {
    window.localStorage.setItem(storageKey, "not-json");
    expect(readPreferenceAfterReload()).toEqual({
      position: "bottom",
      sizePercent: 50,
    });
    window.localStorage.setItem(storageKey, '{"position":"diagonal"}');
    expect(readPreferenceAfterReload()).toEqual({
      position: "bottom",
      sizePercent: 50,
    });
  });

  it("persists position and clamped size changes", () => {
    useUiStore.getState().setTerminalDockPosition("left");
    useUiStore.getState().setTerminalDockSize(200);
    expect(useUiStore.getState().terminalDock).toEqual({
      position: "left",
      sizePercent: 80,
    });
    expect(readStored()).toEqual({ position: "left", sizePercent: 80 });
  });

  it("resets the size back to the default split", () => {
    useUiStore.getState().setTerminalDockSize(30);
    useUiStore.getState().resetTerminalDockSize();
    expect(useUiStore.getState().terminalDock.sizePercent).toBe(50);
    expect(readStored().sizePercent).toBe(50);
  });
});

/** localStorage 只在 store 创建时读取，模拟重新加载需要重新初始化。 */
function readPreferenceAfterReload(): TerminalDockPreference {
  const raw = window.localStorage.getItem(storageKey);
  if (!raw) return { position: "bottom", sizePercent: 50 };
  try {
    const parsed = JSON.parse(raw) as Partial<TerminalDockPreference>;
    if (
      (parsed.position === "bottom" ||
        parsed.position === "left" ||
        parsed.position === "right") &&
      typeof parsed.sizePercent === "number"
    ) {
      return {
        position: parsed.position,
        sizePercent: clampDockPercent(parsed.sizePercent),
      };
    }
  } catch {
    // ignore
  }
  return { position: "bottom", sizePercent: 50 };
}
