import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { setupEn, setupZh } from "./setup";

/**
 * i18n 模块化注册模式（与 enterprise 应用一致）：
 * - 每个业务模块一个 `i18n/<module>.ts`，导出 `<module>Zh` / `<module>En`
 *   两个资源对象，顶层命名空间与模块同名（如 `setup.*`）。
 * - 新模块在 `modulesZh` / `modulesEn` 清单中各加一行即可生效。
 */
const modulesZh = [setupZh];
const modulesEn = [setupEn];

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
