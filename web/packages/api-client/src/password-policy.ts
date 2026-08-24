import { ApiError } from "./transport/errors";
import {
  PASSWORD_POLICY_CONTRACT,
  type GeneratedPasswordPolicyRule,
} from "./generated/password-policy";

export const PASSWORD_POLICY = {
  minLength: PASSWORD_POLICY_CONTRACT.minLength,
  maxLength: PASSWORD_POLICY_CONTRACT.maxLength,
} as const;

export type PasswordPolicyRule = GeneratedPasswordPolicyRule;

export interface PasswordPolicyContext {
  username?: string;
  email?: string;
  previousPassword?: string;
}

const weakPasswords = new Set<string>(PASSWORD_POLICY_CONTRACT.commonPasswords);
const passwordRules = new Set<PasswordPolicyRule>(
  PASSWORD_POLICY_CONTRACT.rules,
);

function characterLength(value: string): number {
  return Array.from(value).length;
}

export function validatePasswordPolicy(
  password: string,
  context: PasswordPolicyContext = {},
): PasswordPolicyRule | null {
  const length = characterLength(password);
  if (length < PASSWORD_POLICY.minLength) return "min_length";
  if (length > PASSWORD_POLICY.maxLength) return "max_length";
  if (!/\p{L}/u.test(password)) return "letter_required";
  if (!/\p{Nd}/u.test(password)) return "digit_required";

  const normalized = password.toLowerCase();
  if (weakPasswords.has(normalized)) return "common_password";

  const emailAccount = context.email?.split("@", 1)[0];
  for (const source of [context.username, emailAccount]) {
    const fragment = source?.trim().toLowerCase() ?? "";
    if (characterLength(fragment) >= 3 && normalized.includes(fragment)) {
      return "contains_identity";
    }
  }
  if (
    context.previousPassword !== undefined &&
    password === context.previousPassword
  ) {
    return "reused_password";
  }
  return null;
}

export function passwordPolicyRuleFromError(
  error: unknown,
): PasswordPolicyRule | null {
  if (!(error instanceof ApiError) || error.code !== "PASSWORD_WEAK") {
    return null;
  }
  const rule = error.params?.rule;
  if (
    typeof rule === "string" &&
    passwordRules.has(rule as PasswordPolicyRule)
  ) {
    return rule as PasswordPolicyRule;
  }
  const prefix = "errors.auth.password.";
  if (error.message_key.startsWith(prefix)) {
    const fromKey = error.message_key.slice(
      prefix.length,
    ) as PasswordPolicyRule;
    if (passwordRules.has(fromKey)) return fromKey;
  }
  return null;
}

export function apiErrorRequestId(error: unknown): string | null {
  return error instanceof ApiError && error.request_id !== "unknown"
    ? error.request_id
    : null;
}
