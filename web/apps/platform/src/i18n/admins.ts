/** 企业管理员文案。 */
export const adminsZh = {
  "admins.title": "企业管理员",
  "admins.description": "企业初始管理员的临时密码与认证管理；平台不提供代登录",
  "admins.invite": "创建管理员",
  "admins.empty": "暂无企业管理员",
  "admins.table.displayName": "姓名",
  "admins.table.username": "用户名",
  "admins.table.email": "邮箱",
  "admins.table.enterprise": "所属企业",
  "admins.table.credentialStatus": "认证状态",
  "admins.table.lastLogin": "最近登录",
  "admins.status.temporary_password": "等待首次改密",
  "admins.status.active": "已启用",
  "admins.status.disabled": "已禁用",
  "admins.action.resetAuth": "重置认证",
  "admins.action.disable": "禁用",
  "admins.confirm.resetAuth.title": "重置登录认证",
  "admins.confirm.resetAuth.description":
    "重置后该管理员的现有会话和密码失效，并生成一次性临时密码。",
  "admins.confirm.disable.title": "禁用企业管理员",
  "admins.confirm.disable.description":
    "禁用后该管理员无法登录企业门户，请确认。",
  "admins.form.title": "创建企业管理员",
  "admins.form.description":
    "创建后系统仅展示一次临时密码，由平台超管通过安全渠道发送",
  "admins.form.enterprise": "所属企业",
  "admins.form.username": "用户名",
  "admins.form.displayName": "姓名",
  "admins.form.email": "邮箱",
  "admins.form.required": "此字段不能为空",
  "admins.form.usernameInvalid": "用户名至少需要 3 个字符",
  "admins.form.emailInvalid": "请输入有效的邮箱地址",
  "admins.created.title": "管理员已创建",
  "admins.created.description":
    "请立即通过安全渠道发送以下临时密码；关闭后无法再次查看：",
  "admins.noImpersonation.title": "不提供代登录",
  "admins.noImpersonation.description":
    "平台超级管理员不能以企业管理员身份登录，所有认证变更均产生平台审计事件。",
};

export const adminsEn = {
  "admins.title": "Enterprise admins",
  "admins.description":
    "Create enterprise admins with temporary passwords; impersonation is not offered",
  "admins.invite": "Create admin",
  "admins.empty": "No enterprise admins yet",
  "admins.table.displayName": "Name",
  "admins.table.username": "Username",
  "admins.table.email": "Email",
  "admins.table.enterprise": "Enterprise",
  "admins.table.credentialStatus": "Credential status",
  "admins.table.lastLogin": "Last login",
  "admins.status.temporary_password": "Awaiting first password change",
  "admins.status.active": "Active",
  "admins.status.disabled": "Disabled",
  "admins.action.resetAuth": "Reset auth",
  "admins.action.disable": "Disable",
  "admins.confirm.resetAuth.title": "Reset authentication",
  "admins.confirm.resetAuth.description":
    "Existing sessions and passwords are revoked and a one-time temporary password is generated.",
  "admins.confirm.disable.title": "Disable enterprise admin",
  "admins.confirm.disable.description":
    "The admin can no longer sign in to the enterprise portal. Please confirm.",
  "admins.form.title": "Create enterprise admin",
  "admins.form.description":
    "A one-time temporary password is generated for secure offline delivery",
  "admins.form.enterprise": "Enterprise",
  "admins.form.username": "Username",
  "admins.form.displayName": "Display name",
  "admins.form.email": "Email",
  "admins.form.required": "This field is required",
  "admins.form.usernameInvalid": "Username must be at least 3 characters",
  "admins.form.emailInvalid": "Enter a valid email address",
  "admins.created.title": "Admin created",
  "admins.created.description":
    "Deliver this temporary password over a secure channel now; it cannot be shown again:",
  "admins.noImpersonation.title": "No impersonation",
  "admins.noImpersonation.description":
    "Super admins cannot sign in as enterprise admins; every auth change is audited in the platform domain.",
};
