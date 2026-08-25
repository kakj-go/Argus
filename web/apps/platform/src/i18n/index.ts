import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { accountEn, accountZh } from "./account";
import { adminsEn, adminsZh } from "./admins";
import { auditEn, auditZh } from "./audit";
import { commonEn, commonZh } from "./common";
import { dashboardEn, dashboardZh } from "./dashboard";
import { enterprisesEn, enterprisesZh } from "./enterprises";
import { loginEn, loginZh } from "./login";
import { sandboxEn, sandboxZh } from "./sandbox";
import { setupEn, setupZh } from "./setup";
import { shellEn, shellZh } from "./shell";
import { errorsEn, errorsZh } from "./errors";

/**
 * i18n 模块化注册模式（与 enterprise 应用一致）：
 * 每个业务模块一个 `i18n/<module>.ts`，导出 `<module>Zh` / `<module>En`，
 * 在这里浅合并后注入 i18next。
 */
const modulesZh = [
  commonZh,
  shellZh,
  loginZh,
  dashboardZh,
  enterprisesZh,
  adminsZh,
  sandboxZh,
  auditZh,
  accountZh,
  setupZh,
  errorsZh,
];
const modulesEn = [
  commonEn,
  shellEn,
  loginEn,
  dashboardEn,
  enterprisesEn,
  adminsEn,
  sandboxEn,
  auditEn,
  accountEn,
  setupEn,
  errorsEn,
];

const storedLocale = window.localStorage.getItem("argus.locale");

void i18n.use(initReactI18next).init({
  lng: storedLocale === "en-US" ? "en-US" : "zh-CN",
  fallbackLng: "zh-CN",
  supportedLngs: ["zh-CN", "en-US"],
  interpolation: { escapeValue: false },
  resources: {
    "zh-CN": { translation: Object.assign({}, ...modulesZh) },
    "en-US": { translation: Object.assign({}, ...modulesEn) },
  },
});

export default i18n;
