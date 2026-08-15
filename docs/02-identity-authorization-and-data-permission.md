# 多租户、RBAC 与数据权限

## 1. 权限目标

Argus 是多企业 SaaS，但第一版采用单企业用户模型：一个企业用户只能属于一个企业，平台身份与企业身份互斥。完整授权判断由四部分组成：

```text
企业隔离：请求是否位于主体唯一所属的企业
RBAC：主体是否拥有执行某类操作的功能能力
数据范围：主体可以访问哪个 Project、资源和目标账号
Policy：在当前环境、风险和上下文中是否允许，或需要满足什么附加条件
```

示例：用户拥有项目操作能力，只能操作 Project A 中被授予的非生产主机；生产重启还需要 Step-up MFA 或审批。

```text
permission = automation.host.restart
enterprise_id = current_user.enterprise_id
project_id = project-a
resource.project_id = project-a
resource.environment != production OR obligation.approval_satisfied
```

权限必须覆盖管理后台、Chatbox、MCP Tool、Card Action、OpenAPI、自动化任务和 Telemetry Query，不能只控制菜单或按钮。

## 2. 身份和企业隔离

### 2.1 身份对象

第一版至少包含：

- Enterprise：企业，也是租户、安全隔离、计费和数据归属的最高业务边界。
- PlatformUser：平台身份，只能进入平台管理域。
- EnterpriseUser：企业身份，必须且只能绑定一个 Enterprise。
- Department：企业内部门，不能跨企业包含用户；每个企业用户固定属于一个部门，企业创建时自动生成可重命名的默认部门。
- Project：企业内资源、监控数据、会话和自动化的主要授权边界。
- Role、Permission、RoleBinding：功能权限及其企业/Project 范围绑定。
- ServiceAccount、APIKey：固定绑定一个企业及允许范围的机器身份。
- Policy、ApprovalPolicy：上下文限制、MFA、审批和会话约束。
- AuthorizationVersion：授权变化后使旧票据、缓存和待执行操作失效的版本。

第一版不提供用户到多个企业的 Membership，也不提供登录后的企业切换。身份必须满足：

```text
PlatformUser   -> enterprise_id IS NULL
EnterpriseUser -> enterprise_id IS NOT NULL
```

同一登录身份不能同时具有 PlatformRole 和 EnterpriseRole。邮箱或登录名的唯一性策略必须避免同一个登录身份被绑定到两个企业；后续外部 IdP 也必须在认证映射时执行相同约束。

### 2.2 平台和企业管理域

平台管理域第一版只有：

```text
PlatformRole
    platform_super_admin
```

平台超级管理员负责初始化、创建企业、创建/禁用初始企业管理员、企业状态和平台 OpenSandbox 基座。它不能进入企业工作台或管理后台，也不能读取企业资源、模型、监控正文、远程会话、Secret、Tool Result 或企业审计正文；空 `enterprise_id` 必须被视为平台上下文，不能获得任何企业权限。

企业身份必须具有唯一 `EnterpriseMembership` 才能进入工作台和企业管理后台。企业身份不能调用平台 API；平台身份不能调用企业 API。前端路由守卫、会话恢复和服务端授权三层都必须执行该身份域检查，不能只依赖菜单隐藏。

企业内置角色建议为：

```text
EnterpriseScope
    enterprise_admin
    iam_admin
    security_auditor

ProjectScope
    project_admin
    project_operator
    project_viewer
    project_approver
```

`enterprise_admin` 是企业控制面管理员，可以管理用户、Project、角色、策略、模型和资源配置，但不自动获得生产 Shell、目标账号、Secret 原值或 AI 生产执行权限。企业管理员可以为自己创建 Project Role Binding 或 Remote Access Grant，但必须经过 Step-up Authentication 并写入高优先级审计。

## 3. Project 和资源归属

企业创建时必须创建一个可重命名的默认 Project。第一版以下对象必须直接保存 `enterprise_id + project_id`，不能长期使用空 Project：

```text
Host / KubernetesCluster
CollectorInstance / Telemetry Resource
AlertRule / Dashboard
Conversation / Run
Automation / ServiceAccount execution scope
Project Secret 或 Project Credential
```

企业级模型配置、企业角色模板和企业审计索引可以不属于某个 Project，但任何实际访问资源、遥测或生产执行的请求都必须解析到明确 Project。

第一版每个受管 Host 和 KubernetesCluster 只能属于一个 Project。跨 Project 移动属于高风险变更，必须 Preview/Commit，并明确处理历史遥测、告警、会话、Automation、SecretRef 和授权失效。实现尚未固化历史数据转移协议前，已经产生遥测数据的资源不得通过普通更新接口直接修改 `project_id`。

以下对象不能代替 Project：

- Bastion Scope：只表示堡垒机和内网资源的网络接入路径，可以为不同 Project 的目标资源提供路由。
- Telemetry Group：只表示独立 Collector 的遥测转发拓扑。
- Group：第一版不提供独立用户组，授权主体组织由部门承担。
- environment/tags：作为 Policy 输入和后续动态 ResourceSet 条件，不是第一版主要授权边界。

目标资源所属 Project 决定业务授权；Bastion Scope、Connector 或 Gateway 只决定已经授权的请求如何到达目标。

## 4. Role、Permission 和 RoleBinding

Role 定义稳定的功能动作集合，RoleBinding 将用户或部门的角色绑定到企业或一个 Project：

```text
RoleBinding
├── enterprise_id
├── subject_type: user | department
├── subject_id
├── role_id
├── scope_type: enterprise | project
├── scope_id
├── valid_from / valid_until
├── status
└── version
```

约束：

- 企业范围绑定的 `scope_id` 为当前企业；Project 范围绑定必须指向同一企业的 Project。
- 用户和部门获得的允许权限取并集；第一版 RoleBinding 只授予能力，不表达 Deny。
- 拒绝、MFA、审批、时间和环境条件统一由 Policy 表达，并采用 Deny 优先。
- RoleBinding 过期、禁用或删除后必须递增相关主体的 AuthorizationVersion。
- 用户能管理 IAM 不代表可以读取 Secret、打开终端或执行生产操作。

建议权限命名：

```text
project.read
project.create
project.update
project.member.manage

connector.read
connector.create
connector.rotate_credential
bastion_scope.read
bastion_scope.create
bastion_scope.manage

host.read
host.create
host.update
host.connection.test
host.direct_connect
automation.command.execute
automation.template.execute

remote_access.request
remote_access.session.create
remote_access.session.approve
remote_access.session.terminate
remote_access.recording.read

kubernetes.cluster.read
kubernetes.cluster.create
kubernetes.pod.read
kubernetes.workload.restart

telemetry.read
telemetry.query.metrics
telemetry.query.logs
telemetry.query.traces
telemetry.live_tail
telemetry.export
telemetry.alert.manage
telemetry.dashboard.manage
telemetry.sensitive_fields.read

credential.manage
credential.use
credential.reveal
audit.read
interactive_card.create
interactive_card.publish
```

`remote_access.*` 只面向人工管理 UI/OpenAPI，不注册为 Model Agent 可发现的 MCP Tool。`automation.command.execute` 与 `remote_access.session.create` 是不同能力；允许 AI/Automation 执行受控 Tool 不自动允许人工 Shell，反之亦然。

平台管理域使用独立权限：

```text
platform.enterprise.create
platform.enterprise.update
platform.enterprise.suspend
platform.enterprise_admin.create
platform.enterprise_admin.disable
platform.sandbox_profile.manage
platform.sandbox_image.manage
platform.sandbox_quota.manage
platform.sandbox_session.terminate
```

## 5. 远程访问授权和 Managed Account

Project Role Binding 只表明用户是否具备申请远程访问的功能权限，不能自动授予 Project 内所有主机和账号。实际会话必须命中 RemoteAccessGrant：

```text
RemoteAccessGrant
├── enterprise_id / project_id
├── subject_type / subject_id
├── host_scope: explicit_host_ids | all_hosts_in_project
├── managed_account_ids
├── protocols
├── actions
├── valid_from / valid_until
├── policy_id
├── status
└── version
```

第一版至少支持 `connect`，并将以下能力作为独立动作，默认关闭：

```text
clipboard
upload
download
session_share
port_forward
```

目标登录账号必须成为一等对象：

```text
ManagedAccount
├── enterprise_id / project_id / host_id
├── username
├── credential_ref
├── privilege_level
├── allowed_protocols
└── status
```

普通用户只获得 Credential Broker 的代用能力，不读取密码或私钥原值。以下权限必须分离：

```text
credential.manage != credential.use
credential.use    != credential.reveal
credential.use    != remote_access.session.create
```

远程会话创建、票据签发和实际连接时都必须重新检查用户、企业、Project、Host、ManagedAccount、协议、动作、Grant 有效期、AuthorizationVersion、MFA/Step-up、审批和会话策略。

## 6. 监控数据权限

监控权限不能由 `host.read` 或 `project.read` 隐式获得：

```text
host.read
!= telemetry.query.metrics
!= telemetry.query.logs
!= telemetry.query.traces
!= telemetry.live_tail
!= telemetry.export
```

Telemetry Query 的有效过滤必须由 Query Service 生成：

```text
effective_filter =
    trusted EnterpriseId
AND authorized ProjectId / ResourceIds
AND allowed Signal
AND validated_user_filter
AND field_projection_and_masking
AND time_range_and_query_budget
```

所有遥测记录必须携带受信的 `EnterpriseId`、`ProjectId`、`ResourceId` 和 `CollectorId`。这些字段来自 Ingest 认证结果和服务端资源关系，并覆盖客户端自报的同名属性。企业用户、Model Agent、交互卡片 和浏览器不直接连接 ClickHouse，也不能提交任意 SQL。

Metrics、Logs、Traces、Live Tail 和 Export 分别授权。Logs/Traces 中的命令行、环境变量、HTTP Header、SQL 和业务字段在进入模型上下文、浏览器或 Card 前完成字段级脱敏；`telemetry.sensitive_fields.read` 也必须受 Policy 和审计约束，不能作为绕过 Secret 管理的能力。

企业管理员可以配置角色模板。建议默认能力：

| 角色 | Metrics | Logs | Traces | Live Tail | Export |
| --- | --- | --- | --- | --- | --- |
| `project_viewer` | 允许 | 可配置 | 可配置 | 拒绝 | 拒绝 |
| `project_operator` | 允许 | 允许 | 允许 | 可配置 | 拒绝 |
| `project_admin` | 允许 | 允许 | 允许 | 允许 | 可配置 |

## 7. AI、Conversation 和自动化权限

Conversation 和 Run 必须绑定服务端确认的 `enterprise_id + project_id`。Project 切换是明确的用户操作；模型生成的企业、Project 或资源 ID 只是候选参数，不能改变授权上下文。

Model Agent 的有效能力必须满足：

```text
可发现 Tool <= 当前用户权限
可读取数据 <= 当前用户 Project/Signal/字段范围
可操作资源 <= 当前用户 Project/资源授权
```

Tool 返回值必须在进入模型上下文和卡片渲染前完成数据范围裁剪与字段脱敏。交互卡片 的字段来源约束不能替代数据权限。跨 Project 查询只允许企业权限明确允许的用户发起，并对每个目标 Project 分别授权；结果因权限被裁剪时可以返回稳定的 partial 标记，但不得泄露未授权资源的名称和属性。

定时和无人值守 Automation 必须绑定 ServiceAccount，并在每次执行时检查当前 ServiceAccount 状态、Project、允许 Tool、资源范围、AuthorizationVersion 和 Policy。不能长期继承创建人的旧权限快照，也不能使用人工 RemoteAccessSession 票据。

## 8. 统一授权决策

所有入口调用同一个授权服务：

```json
{
  "subject": {
    "identity_scope": "enterprise",
    "enterprise_id": "ent-1",
    "user_id": "user-1",
    "authorization_version": 37
  },
  "action": "automation.host.restart",
  "resource": {
    "type": "host",
    "id": "host-1",
    "project_id": "project-a",
    "environment": "production"
  },
  "context": {
    "origin": "card_action",
    "conversation_id": "chat-1",
    "ip": "...",
    "time": "..."
  }
}
```

决策不能只返回布尔值。第一版固定结果为：

```text
ALLOW
DENY
REQUIRE_MFA
REQUIRE_APPROVAL
```

示例：

```json
{
  "decision": "REQUIRE_APPROVAL",
  "reason_code": "PRODUCTION_OPERATION_REQUIRES_APPROVAL",
  "obligations": {
    "step_up_mfa": true,
    "approval_policy_id": "ap-01",
    "record_session": false,
    "max_targets": 5
  },
  "policy_version": 12,
  "authorization_version": 37
}
```

授权优先级固定为：

```text
身份/企业/Project 失效
> Policy 显式拒绝
> REQUIRE_MFA / REQUIRE_APPROVAL
> RoleBinding 或 RemoteAccessGrant 允许
> 默认拒绝
```

审批只能满足已经授权操作的附加条件，不能补齐缺失的 Role、Project、资源、ManagedAccount、Signal 或 Tool 权限。

## 9. 数据过滤、分页和批量操作

列表查询先把用户业务过滤解析为白名单表达式，再与授权服务返回的 Project/Resource Scope 做逻辑与：

```text
effective_filter = authorization_scope AND validated_user_filter
```

详情、批量操作、导出和统计必须使用相同授权入口。批量操作要逐个展开真实目标并计算风险；某个目标无权时默认整体拒绝，除非 Tool Schema 明确声明并返回可审计的部分成功语义。

游标必须由服务端签名并绑定 `enterprise_id`、`project_id`、AuthorizationVersion、排序字段、有效过滤器哈希和有效期，不能接受客户端构造的任意数据库游标。

## 10. 风险、审批和 Break Glass

| 等级 | 示例 | 默认策略 |
| --- | --- | --- |
| read | 查询主机、Pod、Metrics | 可配置免确认 |
| write | 新增主机、更新配置 | Preview 后确认 |
| dangerous | 执行命令、重启、生产远程会话 | Preview/会话预览、Step-up MFA，可配置非本人审批 |
| critical | 生产批量操作、修改权限、暴露敏感字段 | 非本人审批或受控 Break Glass |

Access Request 与 Action Approval 必须分离：

- Access Request 申请一段时间内对 Host/ManagedAccount 的人工访问，批准后生成短期 AccessLease 或激活受限 Grant。
- Action Approval 审批一个已经 Preview 的不可变生产操作，只作用于该 Pending Action。

创建人确认不能满足“非创建人审批”。企业只有一名可用管理员时，不得用同一人多个账号伪造双人审批；Policy 可以允许受控 Break Glass，但必须要求 Step-up MFA、明确原因/工单、最短有效期、强制录像或完整执行审计、默认关闭文件传输，并产生高优先级通知和审计事件。

## 11. 撤权、版本和活动状态

用户禁用、部门变化、RoleBinding/RemoteAccessGrant/Policy 变化、Project 移动和企业停用必须更新相关 AuthorizationVersion，并通过 Outbox 通知缓存和活动通道。

第一版撤权语义：

| 对象 | 撤权后的行为 |
| --- | --- |
| 未使用远程会话票据 | 立即失效 |
| Requested/Authorized 会话 | 拒绝继续连接 |
| 活动远程会话 | 立即终止；协议无法即时终止时进入有上限的安全结束窗口并审计 |
| 未执行 Pending Action | 立即失效 |
| 已审批未 Commit 操作 | Commit 重新鉴权并拒绝 |
| 新 Telemetry Query | 立即拒绝 |
| SSE/WebSocket/Live Tail | 建立、恢复和周期性检查时拒绝并断开 |
| Automation | 下次执行按 ServiceAccount 当前权限重新判断 |

Redis 可以缓存授权结果和版本，但 PostgreSQL 保存权威授权状态。短期 Token、Action Binding、游标和会话票据必须绑定 AuthorizationVersion；仅依赖 TTL 等待旧权限自然过期不满足生产撤权要求。

## 12. 审计要求

至少记录：

- 平台或企业身份、企业、Project、用户/ServiceAccount。
- 来源是管理后台、模型、卡片点击、OpenAPI 还是自动化任务。
- 命中的 RoleBinding、RemoteAccessGrant、Policy、Approval 和 AuthorizationVersion。
- 使用的 Tool、版本、Conversation、Run、参数摘要和敏感字段脱敏结果。
- Connector、目标资源、Bastion Scope、连接模式和执行路径。
- RemoteAccessSession 的 ManagedAccount、协议、动作、理由、审批、票据摘要、开始/结束、录像引用、剪贴板和文件传输策略；不记录密码、私钥或票据原值。
- Telemetry Query 的 Signal、Project/Resource Scope、时间范围、字段投影、脱敏、扫描预算、导出和裁剪结果。
- Preview、确认、审批、Break Glass、执行结果、耗时、错误、重试和结果未知状态。

审计记录应防篡改，并与普通应用日志分离保存。审计查询本身也要按平台域、企业域和必要的 Project 范围授权；平台超级管理员默认不能读取企业审计正文。

## 13. 第一版实现边界

第一版实现企业/Project 两级 RoleBinding、明确 Host/ManagedAccount 的 RemoteAccessGrant、Project 级 Telemetry 权限和类型化 Policy。以下能力延后：

- 一个用户属于多个企业。
- 通用 ABAC/ReBAC 引擎和任意策略 DSL。
- 动态标签 ResourceSet 和多层资产授权树。
- 跨 Project 共享同一个业务资源。
- RDP、SFTP、通用端口转发和复杂命令审批。

这些延后项不能通过在不同 API 中临时加入特殊判断实现；需要扩展时先形成 ADR 并保持统一授权入口。
