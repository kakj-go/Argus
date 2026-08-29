import { describe, expect, it } from "vitest";
import { clampDockPercent } from "./terminal-dock";

describe("TerminalDock sizing", () => {
  it("clamps the dock to the documented 20%-80% range", () => {
    expect(clampDockPercent(5)).toBe(20);
    expect(clampDockPercent(50)).toBe(50);
    expect(clampDockPercent(95)).toBe(80);
  });
});
