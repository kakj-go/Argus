import { useFormContext } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Field, Input } from "@argus/ui";
import type { SetupDraft } from "../lib/validation";

/** 第 1 步：验证 Setup Token。 */
export function StepToken() {
  const { t } = useTranslation();
  const {
    register,
    formState: { errors },
  } = useFormContext<SetupDraft>();

  return (
    <div className="argus-setup-fields">
      <Field
        error={errors.setupToken?.message}
        hint={t("setup.token.hint")}
        label={t("setup.token.label")}
      >
        <Input
          {...register("setupToken")}
          autoFocus
          placeholder={t("setup.token.placeholder")}
          type="password"
        />
      </Field>
    </div>
  );
}
