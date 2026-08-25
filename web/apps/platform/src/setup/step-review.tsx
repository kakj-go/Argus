import { useTranslation } from "react-i18next";
import { KeyValueGrid, type KeyValueItem } from "@argus/ui";
import type { SetupDraft } from "./validation";

/** 最后一步：确认提交。摘要不展示密码原值。 */
export function StepReview({ draft }: { draft: SetupDraft }) {
  const { t } = useTranslation();

  const items: KeyValueItem[] = [
    {
      label: t("setup.system.platformName.label"),
      value: draft.platformName.trim(),
    },
    {
      label: t("setup.system.defaultLocale.label"),
      value: draft.defaultLocale === "zh-CN" ? "简体中文" : "English (US)",
    },
    { label: t("setup.system.timezone.label"), value: draft.timezone },
    {
      label: t("setup.system.externalUrl.label"),
      value: draft.externalUrl.trim(),
    },
    {
      label: t("setup.system.username.label"),
      value: draft.admin.username.trim(),
    },
    {
      label: t("setup.system.displayName.label"),
      value: draft.admin.displayName.trim(),
    },
    { label: t("setup.system.email.label"), value: draft.admin.email.trim() },
    {
      label: t("setup.system.password.label"),
      value: t("setup.review.masked"),
    },
  ];

  return (
    <div className="argus-setup-fields">
      <p className="argus-setup-step-intro">{t("setup.review.intro")}</p>
      <KeyValueGrid columns={2} items={items} />
    </div>
  );
}
