import { describe, expect, it } from "vitest";
import { ApiError } from "./transport/errors";
import {
  passwordPolicyRuleFromError,
  validatePasswordPolicy,
} from "./password-policy";

describe("password policy", () => {
  it.each([
    ["short", "min_length"],
    ["abcdefghijkl", "digit_required"],
    ["987654321098", "letter_required"],
    ["password1234", "common_password"],
    ["operator-secure-2026", "contains_identity"],
  ] as const)("rejects %s with %s", (password, rule) => {
    expect(
      validatePasswordPolicy(password, {
        username: "operator",
        email: "mailbox@example.test",
      }),
    ).toBe(rule);
  });

  it("uses Unicode characters and rejects password reuse", () => {
    const password = "安全密码甲乙丙丁戊己庚辛壬0";
    expect(validatePasswordPolicy(password)).toBeNull();
    expect(
      validatePasswordPolicy(password, { previousPassword: password }),
    ).toBe("reused_password");
  });

  it("matches Go by requiring a Unicode decimal digit", () => {
    expect(validatePasswordPolicy("abcdefghijkⅧ")).toBe("digit_required");
    expect(validatePasswordPolicy("abcdefghijk١")).toBeNull();
  });

  it("reads a safe rule from an API error", () => {
    const error = new ApiError(
      {
        code: "PASSWORD_WEAK",
        message_key: "errors.auth.password.contains_identity",
        params: { field: "password", rule: "contains_identity" },
        request_id: "request-password-policy",
        retryable: false,
      },
      422,
    );
    expect(passwordPolicyRuleFromError(error)).toBe("contains_identity");
  });
});
