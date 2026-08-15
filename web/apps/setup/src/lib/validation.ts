import type { SetupSubmission } from "@argus/api-client";

/** 向导表单草稿；提交时映射为 `SetupSubmission`。 */
export interface SetupDraft {
  setupToken: string;
  platformName: string;
  defaultLocale: "zh-CN" | "en-US";
  timezone: string;
  externalUrl: string;
  admin: {
    username: string;
    displayName: string;
    email: string;
    password: string;
    confirmPassword: string;
  };
  sandbox: {
    enabled: boolean;
    endpoint: string;
    credential: string;
    storage: string;
  };
}

/** 常见时区候选（select 选项）。 */
export const TIMEZONE_OPTIONS = [
  "Asia/Shanghai",
  "Asia/Hong_Kong",
  "Asia/Singapore",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Europe/London",
  "Europe/Berlin",
  "America/New_York",
  "America/Los_Angeles",
  "UTC",
] as const;

export function createInitialDraft(): SetupDraft {
  return {
    setupToken: "",
    platformName: "",
    defaultLocale: "zh-CN",
    timezone: "Asia/Shanghai",
    externalUrl: "",
    admin: {
      username: "",
      displayName: "",
      email: "",
      password: "",
      confirmPassword: "",
    },
    sandbox: { enabled: false, endpoint: "", credential: "", storage: "" },
  };
}

export function isValidHttpUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const USERNAME_PATTERN = /^[a-zA-Z][a-zA-Z0-9_-]{2,31}$/;

export type Translator = (key: string) => string;

/** 第 1 步：Setup Token。mock 侧约定任意非空 token 均可通过。 */
export function validateToken(draft: SetupDraft, t: Translator): string | null {
  const token = draft.setupToken.trim();
  if (!token) return t("setup.token.required");
  if (token.length < 8) return t("setup.token.tooShort");
  return null;
}

/** 第 2 步：系统信息 + 超级管理员。返回字段名 -> 错误信息。 */
export function validateSystem(
  draft: SetupDraft,
  t: Translator,
): Record<string, string> {
  const errors: Record<string, string> = {};
  const name = draft.platformName.trim();
  if (!name) errors.platformName = t("setup.system.platformName.required");
  else if (name.length > 64)
    errors.platformName = t("setup.system.platformName.tooLong");

  if (!draft.externalUrl.trim())
    errors.externalUrl = t("setup.system.externalUrl.required");
  else if (!isValidHttpUrl(draft.externalUrl.trim()))
    errors.externalUrl = t("setup.system.externalUrl.invalid");

  const { admin } = draft;
  if (!admin.username.trim())
    errors.username = t("setup.system.username.required");
  else if (!USERNAME_PATTERN.test(admin.username.trim()))
    errors.username = t("setup.system.username.invalid");

  if (!admin.displayName.trim())
    errors.displayName = t("setup.system.displayName.required");

  if (!admin.email.trim()) errors.email = t("setup.system.email.required");
  else if (!EMAIL_PATTERN.test(admin.email.trim()))
    errors.email = t("setup.system.email.invalid");

  if (!admin.password) errors.password = t("setup.system.password.required");
  else if (admin.password.length < 12)
    errors.password = t("setup.system.password.tooShort");
  else if (!/[a-zA-Z]/.test(admin.password) || !/\d/.test(admin.password))
    errors.password = t("setup.system.password.weak");

  if (admin.confirmPassword !== admin.password)
    errors.confirmPassword = t("setup.system.confirmPassword.mismatch");

  return errors;
}

/** 第 3 步：OpenSandbox（未启用时恒有效）。 */
export function validateSandbox(
  draft: SetupDraft,
  t: Translator,
): Record<string, string> {
  const errors: Record<string, string> = {};
  if (!draft.sandbox.enabled) return errors;
  const { endpoint, credential, storage } = draft.sandbox;
  if (!endpoint.trim()) errors.endpoint = t("setup.sandbox.endpoint.required");
  else if (!isValidHttpUrl(endpoint.trim()))
    errors.endpoint = t("setup.sandbox.endpoint.invalid");
  if (!credential.trim())
    errors.credential = t("setup.sandbox.credential.required");
  if (!storage.trim()) errors.storage = t("setup.sandbox.storage.required");
  return errors;
}

/** 密码强度：0=弱 1=中 2=强（仅供强度指示，不代表通过校验）。 */
export function passwordStrength(password: string): 0 | 1 | 2 {
  if (!password) return 0;
  let score = 0;
  if (password.length >= 12) score += 1;
  if (password.length >= 16) score += 1;
  if (/[a-z]/.test(password) && /[A-Z]/.test(password)) score += 1;
  if (/\d/.test(password)) score += 1;
  if (/[^a-zA-Z0-9]/.test(password)) score += 1;
  if (score <= 2) return 0;
  if (score <= 3) return 1;
  return 2;
}

/** 草稿 -> 提交载荷。 */
export function toSubmission(draft: SetupDraft): SetupSubmission {
  return {
    setupToken: draft.setupToken.trim(),
    platformName: draft.platformName.trim(),
    defaultLocale: draft.defaultLocale,
    timezone: draft.timezone,
    externalUrl: draft.externalUrl.trim(),
    superAdmin: {
      username: draft.admin.username.trim(),
      displayName: draft.admin.displayName.trim(),
      email: draft.admin.email.trim() || undefined,
      password: draft.admin.password,
    },
    sandbox: draft.sandbox.enabled
      ? {
          enabled: true,
          endpoint: draft.sandbox.endpoint.trim(),
          credentialRef: draft.sandbox.credential.trim(),
          defaultStorage: draft.sandbox.storage.trim(),
        }
      : { enabled: false },
  };
}
