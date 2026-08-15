import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Field, Input } from "@argus/ui";
import type { SetupDraft } from "../lib/validation";

export interface StepProps {
  draft: SetupDraft;
  /** 字段名 -> 错误信息；仅展示已触摸（blur 过）字段的错误。 */
  errors: Record<string, string>;
}

/** 第 1 步：验证 Setup Token。 */
export function StepToken({
  draft,
  errors,
  onChange,
}: StepProps & { onChange: (patch: Partial<SetupDraft>) => void }) {
  const { t } = useTranslation();
  const [touched, setTouched] = useState(false);

  return (
    <div className="setup-fields">
      <Field
        error={touched ? errors.setupToken : undefined}
        hint={t("setup.token.hint")}
        label={t("setup.token.label")}
      >
        <Input
          autoFocus
          onBlur={() => setTouched(true)}
          onChange={(event) => onChange({ setupToken: event.target.value })}
          placeholder={t("setup.token.placeholder")}
          type="password"
          value={draft.setupToken}
        />
      </Field>
    </div>
  );
}
