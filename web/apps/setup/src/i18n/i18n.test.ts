import { describe, expect, it } from "vitest";
import { setupEn, setupZh } from "./setup";

function keys(value: unknown, prefix = ""): string[] {
  if (!value || typeof value !== "object") return [prefix];
  return Object.entries(value as Record<string, unknown>)
    .flatMap(([key, child]) => keys(child, prefix ? `${prefix}.${key}` : key))
    .sort();
}

describe("setup i18n", () => {
  it("keeps zh-CN and en-US keys aligned", () => {
    expect(keys(setupZh)).toEqual(keys(setupEn));
  });
});
