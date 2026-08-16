import { describe, expect, it } from "vitest";
import { accountEn, accountZh } from "./account";
import { adminsEn, adminsZh } from "./admins";
import { auditEn, auditZh } from "./audit";
import { commonEn, commonZh } from "./common";
import { dashboardEn, dashboardZh } from "./dashboard";
import { enterprisesEn, enterprisesZh } from "./enterprises";
import { loginEn, loginZh } from "./login";
import { sandboxEn, sandboxZh } from "./sandbox";
import { shellEn, shellZh } from "./shell";

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
  ["enterprises", enterprisesZh, enterprisesEn],
  ["admins", adminsZh, adminsEn],
  ["sandbox", sandboxZh, sandboxEn],
  ["audit", auditZh, auditEn],
  ["account", accountZh, accountEn],
] as const;

describe("platform i18n", () => {
  for (const [name, zh, en] of modules) {
    it(`${name} keeps zh-CN and en-US keys aligned`, () => {
      expect(keys(zh)).toEqual(keys(en));
    });
  }
});
