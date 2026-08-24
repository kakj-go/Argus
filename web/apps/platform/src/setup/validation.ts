import {
  formConstraint,
  validatePasswordPolicy,
  type SetupSubmission,
} from "@argus/api-client";
import { z } from "zod";

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
const USERNAME_PATTERN = /^[a-zA-Z][a-zA-Z0-9_-]{2,127}$/;

const setupConstraints = {
  platformName: formConstraint("SetupInitializeRequest", "platform_name"),
  timezone: formConstraint("SetupInitializeRequest", "timezone"),
  externalUrl: formConstraint("SetupInitializeRequest", "external_url"),
  username: formConstraint("SetupSuperAdminInput", "username"),
  displayName: formConstraint("SetupSuperAdminInput", "display_name"),
  email: formConstraint("SetupSuperAdminInput", "email"),
};

export type Translator = (key: string) => string;

export function createSetupSchemas(t: Translator) {
  const token = z.object({
    setupToken: z
      .string()
      .trim()
      .min(1, t("setup.token.required"))
      .min(8, t("setup.token.tooShort")),
  });
  const admin = z
    .object({
      username: z
        .string()
        .trim()
        .min(
          setupConstraints.username.minLength ?? 1,
          t("setup.system.username.required"),
        )
        .max(
          setupConstraints.username.maxLength ?? 128,
          t("setup.system.username.invalid"),
        )
        .regex(USERNAME_PATTERN, t("setup.system.username.invalid")),
      displayName: z
        .string()
        .trim()
        .min(1, t("setup.system.displayName.required"))
        .max(
          setupConstraints.displayName.maxLength ?? 128,
          t("setup.system.displayName.tooLong"),
        ),
      email: z
        .string()
        .trim()
        .refine(
          (value) => value === "" || EMAIL_PATTERN.test(value),
          t("setup.system.email.invalid"),
        ),
      password: z.string().min(1, t("setup.system.password.required")),
      confirmPassword: z.string(),
    })
    .superRefine((value, context) => {
      const rule = validatePasswordPolicy(value.password, {
        username: value.username,
        email: value.email,
      });
      if (rule) {
        context.addIssue({
          code: "custom",
          path: ["password"],
          message: t(`setup.system.password.${rule}`),
        });
      }
      if (value.confirmPassword !== value.password) {
        context.addIssue({
          code: "custom",
          path: ["confirmPassword"],
          message: t("setup.system.confirmPassword.mismatch"),
        });
      }
    });
  const system = z.object({
    platformName: z
      .string()
      .trim()
      .min(
        setupConstraints.platformName.minLength ?? 1,
        t("setup.system.platformName.required"),
      )
      .max(
        setupConstraints.platformName.maxLength ?? 128,
        t("setup.system.platformName.tooLong"),
      ),
    defaultLocale: z.enum(["zh-CN", "en-US"]),
    timezone: z
      .string()
      .min(setupConstraints.timezone.minLength ?? 1)
      .max(setupConstraints.timezone.maxLength ?? 128),
    externalUrl: z
      .string()
      .trim()
      .min(1, t("setup.system.externalUrl.required"))
      .refine(isValidHttpUrl, t("setup.system.externalUrl.invalid")),
    admin,
  });
  return { token, system, setup: token.merge(system) };
}

export type SetupDraft = z.infer<
  ReturnType<typeof createSetupSchemas>["setup"]
>;

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
  };
}
