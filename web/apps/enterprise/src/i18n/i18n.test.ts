// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { aiSettingsEn, aiSettingsZh } from "./ai-settings";
import { chatEn, chatZh } from "./chat";
import { commonEn, commonZh } from "./common";
import { governanceEn, governanceZh } from "./governance";
import { hostsEn, hostsZh } from "./hosts";
import { kubernetesEn, kubernetesZh } from "./kubernetes";
import { loginEn, loginZh } from "./login";
import { settingsEn, settingsZh } from "./settings";
import { shellEn, shellZh } from "./shell";

function keys(value: unknown, prefix = ""): string[] {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    return [prefix];
  return Object.entries(value).flatMap(([key, child]) =>
    keys(child, prefix ? `${prefix}.${key}` : key),
  );
}

const modules = [
  ["common", commonZh, commonEn],
  ["shell", shellZh, shellEn],
  ["login", loginZh, loginEn],
  ["chat", chatZh, chatEn],
  ["hosts", hostsZh, hostsEn],
  ["kubernetes", kubernetesZh, kubernetesEn],
  ["ai-settings", aiSettingsZh, aiSettingsEn],
  ["settings", settingsZh, settingsEn],
  ["governance", governanceZh, governanceEn],
] as const;

describe("i18n resources", () => {
  for (const [name, zh, en] of modules) {
    it(`${name} has identical zh-CN and en-US keys`, () => {
      expect(keys(zh).sort()).toEqual(keys(en).sort());
    });
  }
});
