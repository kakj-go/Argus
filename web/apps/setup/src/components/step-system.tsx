import { Controller, useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Field, Input, Select } from "@argus/ui";
import {
  passwordStrength,
  TIMEZONE_OPTIONS,
  type SetupDraft,
} from "../lib/validation";

/** 第 2 步：系统信息 + 超级管理员。 */
export function StepSystem() {
  const { t } = useTranslation();
  const {
    control,
    register,
    watch,
    formState: { errors },
  } = useFormContext<SetupDraft>();

  const password = watch("admin.password");
  const strength = passwordStrength(password);
  const strengthKeys = ["weak", "medium", "strong"] as const;

  return (
    <div className="argus-setup-fields">
      <h3 className="argus-setup-section">
        {t("setup.system.platformSection")}
      </h3>
      <Field
        error={errors.platformName?.message}
        label={t("setup.system.platformName.label")}
      >
        <Input
          {...register("platformName")}
          placeholder={t("setup.system.platformName.placeholder")}
        />
      </Field>
      <div className="argus-setup-grid-2">
        <Field label={t("setup.system.defaultLocale.label")}>
          <Controller
            control={control}
            name="defaultLocale"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={[
                  { value: "zh-CN", label: "简体中文" },
                  { value: "en-US", label: "English (US)" },
                ]}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field label={t("setup.system.timezone.label")}>
          <Controller
            control={control}
            name="timezone"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={TIMEZONE_OPTIONS.map((zone) => ({
                  value: zone,
                  label: zone,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
      </div>
      <Field
        error={errors.externalUrl?.message}
        hint={t("setup.system.externalUrl.hint")}
        label={t("setup.system.externalUrl.label")}
      >
        <Input
          {...register("externalUrl")}
          placeholder={t("setup.system.externalUrl.placeholder")}
        />
      </Field>

      <h3 className="argus-setup-section">{t("setup.system.adminSection")}</h3>
      <div className="argus-setup-grid-2">
        <Field
          error={errors.admin?.username?.message}
          hint={t("setup.system.username.hint")}
          label={t("setup.system.username.label")}
        >
          <Input
            {...register("admin.username")}
            placeholder={t("setup.system.username.placeholder")}
          />
        </Field>
        <Field
          error={errors.admin?.displayName?.message}
          label={t("setup.system.displayName.label")}
        >
          <Input
            {...register("admin.displayName")}
            placeholder={t("setup.system.displayName.placeholder")}
          />
        </Field>
      </div>
      <Field
        error={errors.admin?.email?.message}
        label={t("setup.system.email.label")}
      >
        <Input
          {...register("admin.email")}
          placeholder={t("setup.system.email.placeholder")}
          type="email"
        />
      </Field>
      <div className="argus-setup-grid-2">
        <Field
          error={errors.admin?.password?.message}
          hint={t("setup.system.password.hint")}
          label={t("setup.system.password.label")}
        >
          <Input {...register("admin.password")} type="password" />
        </Field>
        <Field
          error={errors.admin?.confirmPassword?.message}
          label={t("setup.system.confirmPassword.label")}
        >
          <Input {...register("admin.confirmPassword")} type="password" />
        </Field>
      </div>
      {password && (
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
