import { describe, expect, it } from "vitest";
import type { TFunction } from "i18next";
import { roleDisplayName } from "./role-presentation";

const translations: Record<string, string> = {
  "settings.org.builtinRoles.enterprise_admin": "企业管理员",
};
const translate = ((key: string, options?: { defaultValue?: string }) =>
  translations[key] ?? options?.defaultValue ?? key) as unknown as TFunction;

describe("roleDisplayName", () => {
  it("localizes a known built-in role by stable key", () => {
    expect(
      roleDisplayName(
        {
          builtin: true,
          builtin_key: "enterprise_admin",
          name: "Enterprise Admin",
        },
        translate,
      ),
    ).toBe("企业管理员");
  });

  it("keeps custom role names unchanged", () => {
    expect(
      roleDisplayName(
        { builtin: false, name: "Production On-call" },
        translate,
      ),
    ).toBe("Production On-call");
  });

  it("falls back to the canonical name for an unknown built-in key", () => {
    expect(
      roleDisplayName(
        { builtin: true, builtin_key: "future_role", name: "Future Role" },
        translate,
      ),
    ).toBe("Future Role");
  });
});
