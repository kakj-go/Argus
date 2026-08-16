# M2：初始化、身份与授权闭环

## 目标

交付第一条真实业务垂直路径：全新安装完成 Setup，平台管理员创建企业和初始管理员，企业管理员建立用户/部门、RoleBinding、DataScope 并登录企业门户。

## 前置条件

- M0 完成。
- M1 HTTP/SSE/WebSocket Transport、显式 real Adapter、路由守卫和认证边界已完成。

## 任务

- [x] `M2-DB-01` 实现 Setup、Enterprise、PlatformUser、EnterpriseUser、Department、Session、Role/Permission、RoleBinding、DataScope、AuthorizationVersion、Audit Migration。
- [x] `M2-SETUP-01` 实现系统状态、一次性 Setup Token、初始化事务/恢复、永久锁定和离线密码恢复。
- [x] `M2-IDENTITY-01` 实现 Platform/Enterprise 互斥身份、不同 Audience、Argon2id、Cookie Session、CSRF、撤销和限流。
- [x] `M2-ENTERPRISE-01` 实现企业生命周期、默认 Department、内置 Role 模板和初始管理员临时密码激活。
- [x] `M2-IAM-01` 实现 EnterpriseUser/Department CRUD 和部门变化的 AuthorizationVersion 更新。
- [x] `M2-AUTH-01` 实现 RoleBinding、DataScope、标签选择器编译和统一授权决策服务。
- [x] `M2-AUTH-02` 实现列表/详情/批量/游标的统一企业与 DataScope 过滤。
- [x] `M2-MACHINE-01` 实现 ServiceAccount/APIKey 单次显示、哈希、Tool/DataScope Scope、轮换与撤销。
- [x] `M2-AUDIT-01` 实现平台/企业审计分域、字段脱敏和审计查询授权。
- [x] `M2-WEB-01` 在 M1 real Adapter 上补充冻结后的 Setup/Identity/IAM Path，将三个门户核心流程切到真实 API；禁止复制 DTO、重写 Transport 或回退 mock。
- [x] `M2-OUTBOX-01` 建立授权变化 Outbox 和 Redis 快速失效，不让 Redis 成为事实来源。

## 固定实现边界

- M2 只提供本地密码和 24 小时临时密码首次改密，不提供激活链接、邮件、SMTP 或重发邀请。
- Enterprise 用户名全局大小写不敏感唯一，因为企业登录请求只提交 `username + password`，没有可用于消歧的企业参数。
- Setup Token 由 `argusctl` 生成到独立 Kubernetes Secret，Server 每次从只读 Volume 读取；初始化后禁止轮换。
- Setup 不收集 OpenSandbox 配置。OpenSandbox 仍由 Helm 部署，治理 API 是 M4 Agent/Sandbox 接入前置任务。
- PostgreSQL 保存 Session、授权版本、审计、Outbox 和幂等事实；Redis 不可用时 Server 以 degraded 状态运行，已有 Session 继续校验，新登录 fail closed。
- M2 的授权决策只产生 `ALLOW` 或 `DENY`。平台超级管理员 MFA、恢复码和 Step-up 属于 M8 Production 硬阻断。
- RoleBinding 更新显式提交可空的 `valid_from`、`valid_until`；APIKey 固定绑定创建时的 ServiceAccount AuthorizationVersion。

## 测试

- 平台身份不能访问企业 API，企业身份不能访问平台 API。
- 一个 EnterpriseUser 不能绑定第二个企业或脱离 Department。
- DataScope 的显式 ID、标签选择器、并集、空范围和跨企业 ID 均有正反例。
- 用户禁用、部门/RoleBinding/DataScope 变化使旧 Session、游标和 Binding 失效。
- Kubernetes 临时 Namespace 完成 Setup → Platform → Enterprise 全流程并清理。

## 实现证据

- 契约：`api/openapi/paths/m2.yaml`、`api/openapi/components/m2.yaml`、错误码和状态机注册表，以及按领域生成的 Go/TypeScript 契约。
- 数据与服务：`migrations/postgresql/00001_m2_identity_authorization.sql`、`internal/{platform,identity,authorization,audit,outbox,pagination}`、`internal/storage/{postgres,redis}`；sqlc 查询按 platform、identity、authorization、machine、audit、idempotency 分域生成，单文件均低于 2000 行。
- 前端：Setup、Platform、Enterprise real Adapter，双 Audience 登录/首次改密，企业/IAM/ServiceAccount/APIKey 页面；M2 真实写表单统一使用 React Hook Form + Zod，EnterpriseUser 与 Department 均提供可审计、可撤权的启停闭环。
- 部署：独立 Goose Migration Job、Setup Token Secret Volume、`argusctl setup-token rotate` 和 `argusctl admin reset-password`。
- 自动化：Migration 真实 PostgreSQL 测试、密码/授权/游标/审计单测、契约门禁、四 Origin real Playwright 和 `make e2e-m2-k8s`。
- 2026-08-16 的临时 Namespace 验收已完成 Setup、双 Audience、跨企业拒绝、DataScope、APIKey 创建/幂等/轮换/撤销、审计、撤权、Redis 停止、Server 重启和无条件清理；验收运行 `20260816110652-35870` 通过后确认无残留 Namespace 与 Lease。

## 退出标准

- 三个门户核心身份流程完全脱离 mock。
- 企业隔离、功能权限和资源范围由同一授权服务执行。
- 平台管理员无法读取企业业务正文，企业管理员不因 IAM 权限获得 Secret/远程执行权限。
- 完成状态仅代表 Evaluation 身份授权闭环，不代表 Production 身份安全就绪；平台超级管理员 MFA 仍阻断 M8 Production Profile。

## 不包含

- Host/Kubernetes 真实资源接入。
- Agent、Card、Remote Access 和 Telemetry。
- M1 已完成的前端 Adapter、认证恢复状态机、Card Runtime 传输基座和样式/UI 门禁。
