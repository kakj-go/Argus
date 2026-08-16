import { useTranslation } from "react-i18next";
import { KeyValueGrid, type KeyValueItem } from "@argus/ui";
import type { SetupDraft } from "../lib/validation";

/** 第 4 步：确认提交。摘要回顾全部填写内容，密码与凭证掩码展示。 */
export function StepReview({ draft }: { draft: SetupDraft }) {
  const { t } = useTranslation();

  const items: KeyValueItem[] = [
    { label: t("setup.token.label"), value: t("setup.review.tokenMasked") },
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
    {
      label: t("setup.sandbox.enable"),
      value: draft.sandbox.enabled
        ? draft.sandbox.endpoint.trim()
        : t("setup.review.notEnabled"),
    },
  ];
  if (draft.sandbox.enabled) {
    items.push(
      {
        label: t("setup.sandbox.credential.label"),
        value: t("setup.review.masked"),
      },
      {
        label: t("setup.sandbox.storage.label"),
        value: draft.sandbox.storage.trim(),
      },
    );
  }

  return (
    <div className="argus-setup-fields">
      <p className="argus-setup-step-intro">{t("setup.review.intro")}</p>
      <KeyValueGrid columns={2} items={items} />
    </div>
  );
}
