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
    changePasswordTitle: "设置新密码",
    changePasswordSubtitle: "临时密码仅用于首次登录，请设置长期密码",
    newPassword: "新密码",
    confirmPassword: "确认新密码",
    passwordMismatch: "两次输入的新密码不一致",
    passwordTooShort: "新密码至少需要 12 位",
    required: "此字段不能为空",
    changePasswordSubmit: "完成改密并登录",
    mfaTitle: "验证多因素认证",
    mfaSubtitle: "请输入认证器中的 6 位验证码或一个恢复码",
    mfaCode: "验证码或恢复码",
    mfaInvalid: "请输入有效的验证码或恢复码",
    mfaSubmit: "验证并登录",
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
    changePasswordTitle: "Set a new password",
    changePasswordSubtitle:
      "The temporary password is only valid for first sign-in.",
    newPassword: "New password",
    confirmPassword: "Confirm new password",
    passwordMismatch: "The new passwords do not match",
    passwordTooShort: "The new password must be at least 12 characters",
    required: "This field is required",
    changePasswordSubmit: "Change password and sign in",
    mfaTitle: "Verify multi-factor authentication",
    mfaSubtitle: "Enter the 6-digit authenticator code or a recovery code.",
    mfaCode: "Authenticator or recovery code",
    mfaInvalid: "Enter a valid authenticator or recovery code",
    mfaSubmit: "Verify and sign in",
    demoHint: "Enterprise admin demo account: root / 123456",
    platformPortal: "Open the platform super admin portal",
  },
};
