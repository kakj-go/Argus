// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import {
  AUDIT_ACTION_CODES,
  AUDIT_ACTOR_TYPE_CODES,
  AUDIT_RESOURCE_TYPE_CODES,
  auditCodeKey,
} from "@argus/api-client";
import { aiSettingsEn, aiSettingsZh } from "./ai-settings";
import { accountEn, accountZh } from "./account";
import { automationsEn, automationsZh } from "./automations";
import { chatEn, chatZh } from "./chat";
import { commonEn, commonZh } from "./common";
import { governanceEn, governanceZh } from "./governance";
import { hostsEn, hostsZh } from "./hosts";
import { kubernetesEn, kubernetesZh } from "./kubernetes";
import { loginEn, loginZh } from "./login";
import { remoteAccessEn, remoteAccessZh } from "./remote-access";
import { settingsEn, settingsZh } from "./settings";
import { shellEn, shellZh } from "./shell";
import { telemetryEn, telemetryZh } from "./telemetry";

function keys(value: unknown, prefix = ""): string[] {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    return [prefix];
  return Object.entries(value).flatMap(([key, child]) =>
    keys(child, prefix ? `${prefix}.${key}` : key),
  );
}

const modules = [
  ["account", accountZh, accountEn],
  ["common", commonZh, commonEn],
  ["shell", shellZh, shellEn],
  ["login", loginZh, loginEn],
  ["chat", chatZh, chatEn],
  ["hosts", hostsZh, hostsEn],
  ["kubernetes", kubernetesZh, kubernetesEn],
  ["ai-settings", aiSettingsZh, aiSettingsEn],
  ["automations", automationsZh, automationsEn],
  ["remote-access", remoteAccessZh, remoteAccessEn],
  ["telemetry", telemetryZh, telemetryEn],
  ["settings", settingsZh, settingsEn],
  ["governance", governanceZh, governanceEn],
] as const;

describe("i18n resources", () => {
  for (const [name, zh, en] of modules) {
    it(`${name} has identical zh-CN and en-US keys`, () => {
      expect(keys(zh).sort()).toEqual(keys(en).sort());
    });
  }

  it("covers every known audit action, resource, and actor code", () => {
    for (const resources of [settingsZh, settingsEn]) {
      const audit = resources.settings.audit;
      for (const code of AUDIT_ACTION_CODES) {
        expect(audit.actions[auditCodeKey(code) as keyof typeof audit.actions]).toBeTruthy();
      }
      for (const code of AUDIT_RESOURCE_TYPE_CODES) {
        expect(audit.resourceTypes[auditCodeKey(code) as keyof typeof audit.resourceTypes]).toBeTruthy();
      }
      for (const code of AUDIT_ACTOR_TYPE_CODES) {
        expect(audit.actorTypes[auditCodeKey(code) as keyof typeof audit.actorTypes]).toBeTruthy();
      }
    }
  });
});
