export const accountZh = {
  "account.title": "我的账号",
  "account.description": "管理密码、多因素认证和短期安全会话",
  "account.profile.title": "账号信息",
  "account.profile.username": "用户名",
  "account.profile.displayName": "姓名",
  "account.profile.email": "邮箱",
  "account.profile.department": "部门",
  "account.password.title": "修改密码",
  "account.password.current": "当前密码",
  "account.password.next": "新密码",
  "account.password.confirm": "确认新密码",
  "account.password.rule": "12-1024 个字符，包含字母和数字，且不包含账号标识",
  "account.password.required": "请完整填写所有密码字段",
  "account.password.mismatch": "两次输入的新密码不一致",
  "account.password.weak": "新密码强度不足",
  "account.password.submit": "更新密码",
  "account.password.failed": "密码更新失败，请检查当前密码和密码策略",
  "account.passwordPolicy.min_length": "新密码至少需要 12 个字符",
  "account.passwordPolicy.max_length": "新密码不能超过 1024 个字符",
  "account.passwordPolicy.letter_required": "新密码必须包含至少一个字母",
  "account.passwordPolicy.digit_required": "新密码必须包含至少一个数字",
  "account.passwordPolicy.common_password": "该密码过于常见，请使用其他密码",
  "account.passwordPolicy.contains_identity":
    "新密码不能包含用户名或邮箱账号部分",
  "account.passwordPolicy.reused_password": "新密码不能与当前密码相同",
  "account.requestReference": "请求 ID：{{requestId}}",
  "account.mfa.title": "多因素认证",
  "account.mfa.description":
    "TOTP 用于远程访问、关键操作和 Break Glass。恢复码只展示一次。",
  "account.mfa.enroll": "设置认证器",
  "account.mfa.disable": "停用 MFA",
  "account.mfa.proof": "验证码或恢复码",
  "account.mfa.stepUp": "完成五分钟 Step-up",
  "account.mfa.regenerate": "重新生成恢复码",
  "account.mfa.stepUpActive": "Step-up 有效至",
  "account.mfa.enrollmentTitle": "绑定认证器",
  "account.mfa.enrollmentDescription":
    "将密钥添加到认证器，然后输入当前 6 位验证码。",
  "account.mfa.secret": "TOTP 密钥",
  "account.mfa.code": "6 位验证码",
  "account.mfa.codeRequired": "请输入验证码",
  "account.mfa.codeInvalid": "请输入当前 6 位数字验证码",
  "account.mfa.proofInvalid": "请输入 6-64 位验证码或恢复码",
  "account.mfa.verify": "验证并启用",
  "account.mfa.recoveryTitle": "恢复码",
  "account.mfa.recoveryDescription":
    "这些恢复码不会再次显示，请存放在安全位置。",
  "account.mfa.invalid": "认证证明无效或已过期",
  "account.mfa.failed": "无法完成 MFA 操作，请检查本地 OpenBao 状态",
  "account.mfa.state.disabled": "未启用",
  "account.mfa.state.enrollment_required": "必须设置",
  "account.mfa.state.enabled": "已启用",
  "account.breakGlass.title": "Break Glass",
  "account.breakGlass.description":
    "仅替代明确允许的审批义务，不扩大角色或数据范围。最长 15 分钟。",
  "account.breakGlass.reason": "原因",
  "account.breakGlass.reasonInvalid": "原因需为 8-2048 个字符",
  "account.breakGlass.ticket": "工单引用",
  "account.breakGlass.ticketRequired": "请输入工单引用",
  "account.breakGlass.ticketInvalid": "工单引用不能超过 256 个字符",
  "account.breakGlass.create": "创建紧急会话",
  "account.breakGlass.revoke": "撤销",
  "account.breakGlass.none": "没有活动的 Break Glass 会话",
  "account.breakGlass.failed":
    "Break Glass 操作失败，请先完成 Step-up 并检查本地策略开关",
  "account.breakGlass.expires": "到期时间",
};

export const accountEn = {
  "account.title": "My account",
  "account.description":
    "Manage password, multi-factor authentication, and short-lived security sessions",
  "account.profile.title": "Profile",
  "account.profile.username": "Username",
  "account.profile.displayName": "Display name",
  "account.profile.email": "Email",
  "account.profile.department": "Department",
  "account.password.title": "Change password",
  "account.password.current": "Current password",
  "account.password.next": "New password",
  "account.password.confirm": "Confirm new password",
  "account.password.rule":
    "12-1024 characters with letters and numbers and no account identifiers",
  "account.password.required": "Complete all password fields",
  "account.password.mismatch": "The new passwords do not match",
  "account.password.weak": "The new password is too weak",
  "account.password.submit": "Update password",
  "account.password.failed":
    "Password update failed. Check the current password and policy.",
  "account.passwordPolicy.min_length":
    "The new password must contain at least 12 characters",
  "account.passwordPolicy.max_length":
    "The new password must not exceed 1024 characters",
  "account.passwordPolicy.letter_required":
    "The new password must contain at least one letter",
  "account.passwordPolicy.digit_required":
    "The new password must contain at least one number",
  "account.passwordPolicy.common_password":
    "This password is too common; choose another password",
  "account.passwordPolicy.contains_identity":
    "The new password must not contain the username or email account name",
  "account.passwordPolicy.reused_password":
    "The new password must differ from the current password",
  "account.requestReference": "Request ID: {{requestId}}",
  "account.mfa.title": "Multi-factor authentication",
  "account.mfa.description":
    "TOTP protects remote access, critical actions, and break glass. Recovery codes are shown once.",
  "account.mfa.enroll": "Set up authenticator",
  "account.mfa.disable": "Disable MFA",
  "account.mfa.proof": "Authenticator or recovery code",
  "account.mfa.stepUp": "Start five-minute step-up",
  "account.mfa.regenerate": "Regenerate recovery codes",
  "account.mfa.stepUpActive": "Step-up valid until",
  "account.mfa.enrollmentTitle": "Connect authenticator",
  "account.mfa.enrollmentDescription":
    "Add the secret to your authenticator, then enter the current 6-digit code.",
  "account.mfa.secret": "TOTP secret",
  "account.mfa.code": "6-digit code",
  "account.mfa.codeRequired": "Enter the verification code",
  "account.mfa.codeInvalid": "Enter the current 6-digit authenticator code",
  "account.mfa.proofInvalid":
    "Enter a 6-64 character authenticator or recovery code",
  "account.mfa.verify": "Verify and enable",
  "account.mfa.recoveryTitle": "Recovery codes",
  "account.mfa.recoveryDescription":
    "These codes will not be shown again. Store them securely.",
  "account.mfa.invalid": "The authentication proof is invalid or expired",
  "account.mfa.failed":
    "The MFA operation failed. Check the local OpenBao service.",
  "account.mfa.state.disabled": "Disabled",
  "account.mfa.state.enrollment_required": "Setup required",
  "account.mfa.state.enabled": "Enabled",
  "account.breakGlass.title": "Break glass",
  "account.breakGlass.description":
    "Replaces only explicitly allowed approval obligations and never expands RBAC or data scope. Maximum 15 minutes.",
  "account.breakGlass.reason": "Reason",
  "account.breakGlass.reasonInvalid":
    "The reason must contain 8-2048 characters",
  "account.breakGlass.ticket": "Ticket reference",
  "account.breakGlass.ticketRequired": "Enter the ticket reference",
  "account.breakGlass.ticketInvalid":
    "The ticket reference must not exceed 256 characters",
  "account.breakGlass.create": "Create emergency session",
  "account.breakGlass.revoke": "Revoke",
  "account.breakGlass.none": "No active break-glass sessions",
  "account.breakGlass.failed":
    "Break glass failed. Complete step-up and check the local policy switch.",
  "account.breakGlass.expires": "Expires",
};
