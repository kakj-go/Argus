/**
 * 登录页文案 —— 第一个页面级 i18n 模块示例。
 * 每个页面模块新增 `i18n/<module>.ts`，导出 `<module>Zh` / `<module>En`，
 * 并在 `i18n/index.ts` 的模块清单中注册。
 */
export const loginZh = {
  login: {
    title: "登录 Argus",
    subtitle: "企业智能运维平台",
    username: "用户名",
    usernamePlaceholder: "请输入用户名",
    password: "密码",
    passwordPlaceholder: "请输入密码",
    submit: "登录",
    submitting: "登录中…",
    failed: "登录失败，请检查用户名和密码",
    wrongPortal: "该账号属于平台管理域，请使用平台超级管理员门户登录",
    demoHint: "企业管理员演示账号：root / 123456",
    platformPortal: "前往平台超级管理员门户",
  },
};

export const loginEn = {
  login: {
    title: "Sign in to Argus",
    subtitle: "Enterprise AIOps platform",
    username: "Username",
    usernamePlaceholder: "Enter your username",
    password: "Password",
    passwordPlaceholder: "Enter your password",
    submit: "Sign in",
    submitting: "Signing in…",
    failed: "Sign-in failed. Check your username and password.",
    wrongPortal:
      "This account belongs to the platform domain. Use the platform super admin portal.",
    demoHint: "Enterprise admin demo account: root / 123456",
    platformPortal: "Open the platform super admin portal",
  },
};
