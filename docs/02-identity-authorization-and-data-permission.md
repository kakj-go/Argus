# 多租户、RBAC 与数据权限

## 1. 权限目标

Argus 是多企业 SaaS，但第一版采用单企业用户模型。完整授权判断由四部分组成：

```text
企业隔离：请求是否位于主体唯一所属的企业
RBAC：主体是否拥有执行某类操作的功能能力
DataScope：主体可以访问哪些明确资源或标签选择结果
Policy：当前环境、风险和上下文是否允许，或需要满足哪些附加条件
```

示例：用户拥有主机操作能力，DataScope 只包含 `environment=staging` 的 Host；生产重启仍需要显式资源授权和 Step-up MFA 或审批。

```text
permission = automation.host.restart
enterprise_id = current_user.enterprise_id
resource.labels["environment"] = "staging"
resource in effective_data_scope
policy obligations satisfied
```

权限必须覆盖管理后台、Chatbox、MCP Tool、Card Action、OpenAPI、自动化任务和 Telemetry Query，不能只控制菜单或按钮。

## 2. 身份和企业隔离

### 2.1 身份对象

第一版至少包含：

- Enterprise：企业，也是租户、安全隔离、计费和数据归属的唯一业务边界。
- PlatformUser：平台身份，只能进入平台管理域，`enterprise_id` 为空。
- EnterpriseUser：企业身份，直接保存唯一 `enterprise_id + department_id`。
- Department：企业内组织归属，不能跨企业包含用户；企业创建时生成可重命名的默认部门。
- Role、Permission、RoleBinding：企业范围内的功能权限及主体绑定。
- DataScope：显式资源 ID 和/或经过校验的标签选择器组成的资源范围。
- ServiceAccount、APIKey：固定绑定企业、Tool 能力和 DataScope 的机器身份。
- Policy、ApprovalPolicy：上下文限制、MFA、审批和会话约束。
- AuthorizationVersion：授权变化后使旧票据、缓存和待执行操作失效的版本。

第一版不提供 `EnterpriseMembership`、用户到多个企业的 Membership、企业切换、Project 或通用用户 Group。同一登录身份不能同时具有 PlatformRole 和 EnterpriseRole。M2 本地认证要求 Enterprise 用户名全局大小写不敏感唯一，因为登录请求不携带企业参数；后续外部 IdP 映射必须执行同样的唯一身份约束。

### 2.2 平台和企业管理域

平台管理域第一版只有 `platform_super_admin`。它负责初始化、创建企业、创建或禁用初始企业管理员和企业状态，但不能进入企业工作台，也不能读取企业资源、模型、监控正文、远程会话、Secret、Tool Result 或企业审计正文。OpenSandbox 由 Helm 部署，平台治理 API 在 M4 Agent/Sandbox 接入前补充，不属于 M2 Setup 或身份闭环。

企业身份只有在自身 `enterprise_id` 有效、用户启用且 Department 有效时才能进入工作台。企业 API 与平台 API 必须使用不同的路由守卫、Session Audience 和服务端授权入口。

企业内置角色建议为：

```text
enterprise_admin
iam_admin
security_auditor
resource_admin
resource_operator
resource_viewer
resource_approver
```

`enterprise_admin` 是企业控制面管理员，可以管理用户、部门、角色、策略、模型和资源配置，但不自动获得生产 Shell、目标账号、Secret 原值或 AI 生产执行权限。管理员扩大自己的 DataScope 或 RemoteAccessGrant 必须经过 Step-up Authentication 并写入高优先级审计。

M2 只实现 `ALLOW | DENY` 和本地密码认证，用于 Evaluation 身份授权闭环。MFA、恢复码、Step-up 和 Production 强制策略在 M8 完成；在此之前不能把 Evaluation Profile 声明为 Production 身份安全就绪。

## 3. 资源标签和 DataScope

第一版不创建 Project。Host 和 KubernetesCluster 使用统一的 `labels: Record<string,string>` 供用户归类、过滤、批量选择和授权。推荐的用户标签包括 `environment`、`team`、`service`、`region` 等，但产品不赋予某个键固定的 Project 语义。

标签契约：

- 标签键和值由服务端执行字符集、长度、数量和总大小限制。
- `argus.io/*` 是系统保留命名空间，例如受信资源类型、Collector 绑定或导入来源；用户只能读取，不能写入。
- 所有资源列表、Tool Schema、OpenAPI 和前端筛选器使用 `labels`，不再增加平行 `tags` 字段。
- 选择器第一版只支持版本化白名单操作，例如精确匹配、集合包含、存在/不存在；不支持任意表达式、脚本、SQL 或正则。
- 服务端必须先在当前 `enterprise_id` 内解析选择器，再生成显式授权资源集合或等价的数据库谓词。

`DataScope` 建议结构：

```text
DataScope
├── id / enterprise_id
├── name / description
├── resource_types: host | kubernetes_cluster | ...
├── explicit_resource_ids[]
├── label_selector
├── status / version
└── created_by / created_at / updated_at
```

DataScope 可以直接绑定 User、Department、ServiceAccount，或由 RoleBinding 引用。最终资源范围取有效允许范围的并集，再与 Policy 拒绝和请求过滤做逻辑与。第一版不实现任意层级资产树、跨企业资源共享或通用 ReBAC。

标签变化可能让资源进入或离开授权范围。只要资源命中任何生效中的授权选择器，标签变更就是授权敏感操作，必须：

1. Preview 展示进入和离开的 DataScope、RemoteAccessGrant、Telemetry Scope 及受影响主体。
2. Commit 重新校验资源版本、标签版本和权限。
3. 递增相关主体和机器身份的 AuthorizationVersion。
4. 使未使用票据、PendingAction、游标、查询绑定和活动订阅失效或重新校验。
5. 写入包含变更前后标签与影响摘要的审计记录。

Bastion Scope 和 Telemetry Group 只描述网络/遥测拓扑。它们可以包含不同标签的资源，但不能自动扩展 DataScope。

## 4. Role、Permission 和 RoleBinding

Role 定义稳定的功能动作集合，RoleBinding 将用户、部门或 ServiceAccount 的角色绑定到企业：

```text
RoleBinding
├── enterprise_id
├── subject_type: user | department | service_account
├── subject_id
├── role_id
├── data_scope_ids[]
├── valid_from / valid_until
├── status
└── version
```

约束：

- RoleBinding 只授予功能能力；资源能力必须同时命中 DataScope，不能因为具有 `host.read` 就读取企业内全部 Host。
- 用户和部门获得的允许权限与 DataScope 取并集；第一版 RoleBinding 不表达 Deny。
- 拒绝、MFA、审批、时间和环境条件由 Policy 表达，并采用 Deny 优先。
- RoleBinding、DataScope 或 Department 变化后必须递增相关主体的 AuthorizationVersion。
- 管理 IAM 不代表可以读取 Secret、打开终端或执行生产操作。

建议权限命名：

```text
department.read
department.manage
identity.read
identity.manage
role.read
role.manage
data_scope.read
data_scope.manage

connector.read
connector.create
connector.rotate_credential
bastion_scope.read
bastion_scope.create
bastion_scope.manage

host.read
host.create
host.update
host.labels.update
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
kubernetes.cluster.update
kubernetes.cluster.labels.update
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

`remote_access.*` 只面向人工管理 UI/OpenAPI，不注册为 Model Agent 可发现的 MCP Tool。`automation.command.execute` 与 `remote_access.session.create` 是不同能力。

## 5. 远程访问授权和 Managed Account

功能 RoleBinding 只表明主体可以使用远程访问功能，实际会话必须命中独立的 `RemoteAccessGrant`：

```text
RemoteAccessGrant
├── enterprise_id
├── subject_type / subject_id
├── explicit_host_ids[]
├── host_label_selector
├── managed_account_ids[]
├── protocols / actions
├── valid_from / valid_until
├── policy_id / status / version
└── authorization_version
```

第一版不提供 `all_hosts_in_project` 或其它隐式全量范围。一个 Grant 至少包含显式 Host ID 或经过校验的 Host 标签选择器；空范围表示不授权。

目标登录账号是一等对象：

```text
ManagedAccount
├── enterprise_id / host_id
├── username / credential_ref
├── privilege_level
├── allowed_protocols
└── status
```

普通用户只获得 Credential Broker 的代用能力，不读取密码或私钥原值。`credential.manage`、`credential.use`、`credential.reveal` 和 `remote_access.session.create` 必须彼此独立授权。

远程会话创建、票据签发和实际连接时都必须重新检查 User、Enterprise、功能权限、DataScope、Grant、Host、ManagedAccount、协议、动作、有效期、AuthorizationVersion、MFA/Step-up、审批和会话策略。

## 6. 监控数据权限

监控权限不能由 `host.read` 隐式获得。Metrics、Logs、Traces、Live Tail、Export 和敏感字段读取分别授权。

Telemetry Query 的有效过滤必须由 Query Service 生成：

```text
effective_filter =
    trusted EnterpriseId
AND authorized ResourceIds or validated label-derived scope
AND allowed Signal
AND validated_user_filter
AND field_projection_and_masking
AND time_range_and_query_budget
```

所有遥测记录携带受信的 `EnterpriseId`、`ResourceId` 和 `CollectorId`。这些字段来自 Ingest 认证结果与服务端资源关系并覆盖客户端自报的同名属性。企业用户、Model Agent、Card 和浏览器不能直接连接 ClickHouse，也不能提交任意 SQL。

标签选择器在查询前由授权服务解析为资源范围；ClickHouse 不直接把用户提供的标签表达式当作授权条件。Logs/Traces 中的命令行、环境变量、HTTP Header、SQL 和业务字段在进入模型上下文、浏览器或 Card 前完成字段级脱敏。

## 7. AI、Conversation 和自动化权限

Conversation 和 Run 只绑定服务端确认的 `enterprise_id`，不保存 `project_id`。每次 ToolCall、Run Step、PendingAction 和 Execution 保存目标资源引用、授权版本和资源范围快照。

Model Agent 的有效能力满足：

```text
可发现 Tool <= 当前用户功能权限
可读取数据 <= 当前用户 DataScope / Signal / 字段范围
可操作资源 <= 当前用户 DataScope / Tool / Policy
```

模型生成的资源 ID 或标签选择器只是候选参数。Tool 返回值必须在进入模型上下文和卡片渲染前完成数据裁剪与字段脱敏；结果因权限被裁剪时可以返回稳定的 `partial` 标记，但不得泄露未授权资源名称和属性。

定时和无人值守 Automation 绑定固定企业的 ServiceAccount、允许 Tool 与 DataScope。每次执行重新检查 ServiceAccount 状态、Tool、目标资源、AuthorizationVersion 和 Policy，不能长期继承创建人的旧权限快照，也不能使用人工 RemoteAccessSession 票据。

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
    "labels": {
      "environment": "production",
      "team": "payments"
    },
    "version": 8
  },
  "context": {
    "origin": "card_action",
    "conversation_id": "chat-1",
    "ip": "...",
    "time": "..."
  }
}
```

决策结果固定为 `ALLOW | DENY | REQUIRE_MFA | REQUIRE_APPROVAL`，并返回稳定 `reason_code`、Obligations、Policy Version、AuthorizationVersion 和命中的 DataScope 摘要。

授权优先级：

```text
身份或企业失效
> Policy 显式拒绝
> 缺失功能权限或资源范围
> REQUIRE_MFA / REQUIRE_APPROVAL
> 允许
```

审批只能满足已经授权操作的附加条件，不能补齐缺失的 Role、DataScope、Resource、ManagedAccount、Signal 或 Tool 权限。

## 9. 数据过滤、分页和批量操作

列表查询先解析授权资源范围，再与用户业务过滤做逻辑与：

```text
effective_filter = enterprise_scope AND authorization_scope AND validated_user_filter
```

详情、批量操作、导出和统计必须复用相同授权入口。批量操作逐个展开真实目标并计算风险；某个目标无权时默认整体拒绝，除非 Tool Schema 明确声明并返回可审计的部分成功语义。

游标由服务端签名并绑定 `enterprise_id`、AuthorizationVersion、DataScope/过滤器哈希、排序字段和有效期。标签或授权变化后旧游标必须拒绝，不接受客户端构造的数据库游标。

## 10. 风险、审批和 Break Glass

| 等级 | 示例 | 默认策略 |
| --- | --- | --- |
| read | 查询主机、Pod、Metrics | 可配置免确认 |
| write | 新增主机、更新标签或配置 | Preview 后确认 |
| dangerous | 执行命令、重启、生产远程会话 | Preview/会话预览、Step-up MFA，可配置非本人审批 |
| critical | 生产批量操作、修改权限、暴露敏感字段 | 非本人审批或受控 Break Glass |

Access Request 与 Action Approval 必须分离。创建人确认不能满足“非创建人审批”。单管理员企业只能通过显式 Policy 开启受控 Break Glass，并要求 Step-up MFA、原因/工单、最短有效期、强制录像或完整执行审计及高优先级通知。

## 11. 撤权、版本和活动状态

用户禁用、Department、RoleBinding、DataScope、RemoteAccessGrant、Policy 或授权敏感标签变化以及企业停用，必须更新相关 AuthorizationVersion，并通过 Outbox 通知缓存和活动通道。

| 对象 | 撤权后的行为 |
| --- | --- |
| 未使用远程会话票据 | 立即失效 |
| Requested/Authorized 会话 | 拒绝继续连接 |
| 活动远程会话 | 立即终止；协议受限时进入有上限的安全结束窗口并审计 |
| 未执行 PendingAction | 立即失效 |
| 已审批未 Commit 操作 | Commit 重新鉴权并拒绝 |
| 新 Telemetry Query | 立即拒绝 |
| SSE/WebSocket/Live Tail | 建立、恢复和周期性检查时拒绝并断开 |
| Automation | 下次执行按 ServiceAccount 当前权限重新判断 |
| APIKey | Key 的 AuthorizationVersion 与当前 ServiceAccount 不一致时立即拒绝 |

Redis 可以缓存授权结果和版本，但 PostgreSQL 保存权威授权状态。Redis 不可用时已有 Session 仍从 PostgreSQL 校验，新登录因限流依赖不可用而保守拒绝。短期 Token、APIKey、ActionBinding、游标和会话票据必须绑定 AuthorizationVersion；仅依赖 TTL 不满足生产撤权要求。

## 12. 审计要求

至少记录：

- 平台或企业身份、Enterprise、User/Department/ServiceAccount。
- 来源、命中的 RoleBinding、DataScope、RemoteAccessGrant、Policy、Approval 和 AuthorizationVersion。
- Tool、版本、Conversation、Run、参数摘要、目标资源引用及授权/标签快照。
- Connector、Bastion Scope、连接模式、ManagedAccount、RemoteAccessSession、录像和执行路径。
- Telemetry Query 的 Signal、资源范围、时间范围、字段投影、脱敏、预算、导出和裁剪结果。
- Preview、确认、审批、Break Glass、执行结果、耗时、错误、重试和结果未知状态。

审计记录应防篡改并与普通应用日志分离。平台超级管理员默认不能读取企业审计正文；企业审计读取也必须经过功能权限、DataScope 和字段脱敏。

## 13. 第一版实现边界

第一版实现企业级 RoleBinding、显式资源 ID/标签选择器 DataScope、Host/ManagedAccount RemoteAccessGrant、资源级 Telemetry 权限和类型化 Policy。以下能力延后：

- 一个用户属于多个企业。
- Project 及其导航、RoleBinding、数据归属和跨 Project 行为。
- 通用用户 Group、通用 ABAC/ReBAC 和任意策略 DSL。
- 多层资产授权树、跨企业资源共享和任意动态集合表达式。
- RDP、SFTP、通用端口转发和复杂命令审批。

需要扩展时先形成 ADR 并保持统一授权入口，不能在不同 API 中临时加入特殊判断。
