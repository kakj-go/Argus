import { describe, expect, it } from "vitest";
import { accountEn, accountZh } from "./account";
import { adminsEn, adminsZh } from "./admins";
import { auditEn, auditZh } from "./audit";
import { commonEn, commonZh } from "./common";
import { dashboardEn, dashboardZh } from "./dashboard";
import { enterprisesEn, enterprisesZh } from "./enterprises";
import { loginEn, loginZh } from "./login";
import { pkiEn, pkiZh } from "./pki";
import { sandboxEn, sandboxZh } from "./sandbox";
import { setupEn, setupZh } from "./setup";
import { shellEn, shellZh } from "./shell";
import { ERROR_CODES, errorsEn, errorsZh } from "./errors";

function keys(value: unknown, prefix = ""): string[] {
  if (!value || typeof value !== "object") return [prefix];
  return Object.entries(value as Record<string, unknown>)
    .flatMap(([key, child]) => keys(child, prefix ? `${prefix}.${key}` : key))
    .sort();
}

const modules = [
  ["common", commonZh, commonEn],
  ["shell", shellZh, shellEn],
  ["login", loginZh, loginEn],
  ["dashboard", dashboardZh, dashboardEn],
  ["pki", pkiZh, pkiEn],
  ["enterprises", enterprisesZh, enterprisesEn],
  ["admins", adminsZh, adminsEn],
  ["sandbox", sandboxZh, sandboxEn],
  ["audit", auditZh, auditEn],
  ["account", accountZh, accountEn],
  ["setup", setupZh, setupEn],
] as const;

describe("platform i18n", () => {
  for (const [name, zh, en] of modules) {
    it(`${name} keeps zh-CN and en-US keys aligned`, () => {
      expect(keys(zh)).toEqual(keys(en));
    });
  }

  it("has a zh-CN and en-US message for every API error code", () => {
    for (const code of ERROR_CODES) {
      expect(errorsZh.errors.codes[code]).toBeTruthy();
      expect(errorsEn.errors.codes[code]).toBeTruthy();
      expect(errorsZh.errors.codes[code]).not.toBe(code);
      expect(errorsEn.errors.codes[code]).not.toBe(code);
    }
  });
});
