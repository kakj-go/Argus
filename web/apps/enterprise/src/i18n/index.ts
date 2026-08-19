import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { aiSettingsEn, aiSettingsZh } from "./ai-settings";
import { automationsEn, automationsZh } from "./automations";
import { chatEn, chatZh } from "./chat";
import { commonEn, commonZh } from "./common";
import { hostsEn, hostsZh } from "./hosts";
import { kubernetesEn, kubernetesZh } from "./kubernetes";
import { loginEn, loginZh } from "./login";
import { remoteAccessEn, remoteAccessZh } from "./remote-access";
import { shellEn, shellZh } from "./shell";
import { telemetryEn, telemetryZh } from "./telemetry";

/**
 * i18n 模块化注册模式：
 * - 每个业务模块一个 `i18n/<module>.ts`，导出 `<module>Zh` / `<module>En`
 *   两个资源对象，顶层命名空间与模块同名（如 `login.*`）。
 * - 新模块在这里的 `modulesZh` / `modulesEn` 清单中各加一行即可生效，
 *   资源对象在这里浅合并后注入 i18next。
 * - 通用/跨模块文案放 `common.ts`，外壳与导航文案放 `shell.ts`。
 */
const modulesZh = [
  commonZh,
  shellZh,
  loginZh,
  chatZh,
  hostsZh,
  kubernetesZh,
  aiSettingsZh,
  automationsZh,
  remoteAccessZh,
  telemetryZh,
];
const modulesEn = [
  commonEn,
  shellEn,
  loginEn,
  chatEn,
  hostsEn,
  kubernetesEn,
  aiSettingsEn,
  automationsEn,
  remoteAccessEn,
  telemetryEn,
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
