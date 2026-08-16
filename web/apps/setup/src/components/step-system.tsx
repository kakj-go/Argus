import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Field, Input, Select } from "@argus/ui";
import {
  passwordStrength,
  TIMEZONE_OPTIONS,
  type SetupDraft,
} from "../lib/validation";
import type { StepProps } from "./step-token";

type Admin = SetupDraft["admin"];

/** 第 2 步：系统信息 + 超级管理员。 */
export function StepSystem({
  draft,
  errors,
  onChange,
  onAdminChange,
}: StepProps & {
  onChange: (patch: Partial<SetupDraft>) => void;
  onAdminChange: (patch: Partial<Admin>) => void;
}) {
  const { t } = useTranslation();
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const touch = (key: string) =>
    setTouched((prev) => (prev[key] ? prev : { ...prev, [key]: true }));
  const errorFor = (key: string) => (touched[key] ? errors[key] : undefined);

  const strength = passwordStrength(draft.admin.password);
  const strengthKeys = ["weak", "medium", "strong"] as const;

  return (
    <div className="argus-setup-fields">
      <h3 className="argus-setup-section">
        {t("setup.system.platformSection")}
      </h3>
      <Field
        error={errorFor("platformName")}
        label={t("setup.system.platformName.label")}
      >
        <Input
          onBlur={() => touch("platformName")}
          onChange={(event) => onChange({ platformName: event.target.value })}
          placeholder={t("setup.system.platformName.placeholder")}
          value={draft.platformName}
        />
      </Field>
      <div className="argus-setup-grid-2">
        <Field label={t("setup.system.defaultLocale.label")}>
          <Select
            onValueChange={(value) =>
              onChange({
                defaultLocale: value as SetupDraft["defaultLocale"],
              })
            }
            options={[
              { value: "zh-CN", label: "简体中文" },
              { value: "en-US", label: "English (US)" },
            ]}
            value={draft.defaultLocale}
          />
        </Field>
        <Field label={t("setup.system.timezone.label")}>
          <Select
            onValueChange={(value) => onChange({ timezone: value })}
            options={TIMEZONE_OPTIONS.map((zone) => ({
              value: zone,
              label: zone,
            }))}
            value={draft.timezone}
          />
        </Field>
      </div>
      <Field
        error={errorFor("externalUrl")}
        hint={t("setup.system.externalUrl.hint")}
        label={t("setup.system.externalUrl.label")}
      >
        <Input
          onBlur={() => touch("externalUrl")}
          onChange={(event) => onChange({ externalUrl: event.target.value })}
          placeholder={t("setup.system.externalUrl.placeholder")}
          value={draft.externalUrl}
        />
      </Field>

      <h3 className="argus-setup-section">{t("setup.system.adminSection")}</h3>
      <div className="argus-setup-grid-2">
        <Field
          error={errorFor("username")}
          hint={t("setup.system.username.hint")}
          label={t("setup.system.username.label")}
        >
          <Input
            onBlur={() => touch("username")}
            onChange={(event) =>
              onAdminChange({ username: event.target.value })
            }
            placeholder={t("setup.system.username.placeholder")}
            value={draft.admin.username}
          />
        </Field>
        <Field
          error={errorFor("displayName")}
          label={t("setup.system.displayName.label")}
        >
          <Input
            onBlur={() => touch("displayName")}
            onChange={(event) =>
              onAdminChange({ displayName: event.target.value })
            }
            placeholder={t("setup.system.displayName.placeholder")}
            value={draft.admin.displayName}
          />
        </Field>
      </div>
      <Field error={errorFor("email")} label={t("setup.system.email.label")}>
        <Input
          onBlur={() => touch("email")}
          onChange={(event) => onAdminChange({ email: event.target.value })}
          placeholder={t("setup.system.email.placeholder")}
          type="email"
          value={draft.admin.email}
        />
      </Field>
      <div className="argus-setup-grid-2">
        <Field
          error={errorFor("password")}
          hint={t("setup.system.password.hint")}
          label={t("setup.system.password.label")}
        >
          <Input
            onBlur={() => touch("password")}
            onChange={(event) =>
              onAdminChange({ password: event.target.value })
            }
            type="password"
            value={draft.admin.password}
          />
        </Field>
        <Field
          error={errorFor("confirmPassword")}
          label={t("setup.system.confirmPassword.label")}
        >
          <Input
            onBlur={() => touch("confirmPassword")}
            onChange={(event) =>
              onAdminChange({ confirmPassword: event.target.value })
            }
            type="password"
            value={draft.admin.confirmPassword}
          />
        </Field>
      </div>
      {draft.admin.password && (
        <div className={`argus-setup-strength is-${strengthKeys[strength]}`}>
          <span className="argus-setup-strength__label">
            {t("setup.system.strength.label")}：
            {t(`setup.system.strength.${strengthKeys[strength]}`)}
          </span>
          <span className="argus-setup-strength__bars">
            {[0, 1, 2].map((level) => (
              <i
                aria-hidden
                className={level <= strength ? "is-on" : ""}
                key={level}
              />
            ))}
          </span>
        </div>
      )}
    </div>
  );
}
