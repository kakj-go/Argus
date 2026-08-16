# M2：初始化、身份与授权闭环

## 目标

交付第一条真实业务垂直路径：全新安装完成 Setup，平台管理员创建企业和初始管理员，企业管理员建立用户/部门、RoleBinding、DataScope 并登录企业门户。

## 前置条件

- M0 完成。
- M1 HTTP/SSE/WebSocket Transport、显式 real Adapter、路由守卫和认证边界已完成。

## 任务

- [ ] `M2-DB-01` 实现 Setup、Enterprise、PlatformUser、EnterpriseUser、Department、Session、Role/Permission、RoleBinding、DataScope、AuthorizationVersion、Audit Migration。
- [ ] `M2-SETUP-01` 实现系统状态、一次性 Setup Token、初始化事务/恢复、永久锁定和离线密码恢复。
- [ ] `M2-IDENTITY-01` 实现 Platform/Enterprise 互斥身份、不同 Audience、Argon2id、Cookie Session、CSRF、撤销和限流。
- [ ] `M2-ENTERPRISE-01` 实现企业生命周期、默认 Department、内置 Role 模板和初始管理员激活。
- [ ] `M2-IAM-01` 实现 EnterpriseUser/Department CRUD 和部门变化的 AuthorizationVersion 更新。
- [ ] `M2-AUTH-01` 实现 RoleBinding、DataScope、标签选择器编译和统一授权决策服务。
- [ ] `M2-AUTH-02` 实现列表/详情/批量/游标的统一企业与 DataScope 过滤。
- [ ] `M2-MACHINE-01` 实现 ServiceAccount/APIKey 单次显示、哈希、Tool/DataScope Scope、轮换与撤销。
- [ ] `M2-AUDIT-01` 实现平台/企业审计分域、字段脱敏和审计查询授权。
- [ ] `M2-WEB-01` 在 M1 real Adapter 上补充冻结后的 Setup/Identity/IAM Path，将三个门户核心流程切到真实 API；禁止复制 DTO、重写 Transport 或回退 mock。
- [ ] `M2-OUTBOX-01` 建立授权变化 Outbox 和 Redis 快速失效，不让 Redis 成为事实来源。

## 测试

- 平台身份不能访问企业 API，企业身份不能访问平台 API。
- 一个 EnterpriseUser 不能绑定第二个企业或脱离 Department。
- DataScope 的显式 ID、标签选择器、并集、空范围和跨企业 ID 均有正反例。
- 用户禁用、部门/RoleBinding/DataScope 变化使旧 Session、游标和 Binding 失效。
- Kubernetes 临时 Namespace 完成 Setup → Platform → Enterprise 全流程并清理。

## 退出标准

- 三个门户核心身份流程完全脱离 mock。
- 企业隔离、功能权限和资源范围由同一授权服务执行。
- 平台管理员无法读取企业业务正文，企业管理员不因 IAM 权限获得 Secret/远程执行权限。

## 不包含

- Host/Kubernetes 真实资源接入。
- Agent、Card、Remote Access 和 Telemetry。
- M1 已完成的前端 Adapter、认证恢复状态机、Card Runtime 传输基座和样式/UI 门禁。
