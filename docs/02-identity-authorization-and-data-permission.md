# 多租户、RBAC 与数据权限

## 1. 权限目标

Argus 不能只依赖传统 RBAC。完整授权判断由三部分组成：

```text
RBAC：用户是否拥有执行某类操作的能力
数据权限：用户可以访问哪些具体资源
Policy：在当前环境、风险和上下文中是否允许执行
```

示例：用户拥有 `host.restart` 权限，但只能操作所属团队的非生产主机。

```text
permission = host.restart
enterprise_id = current_enterprise
resource.team_id in current_user.team_ids
resource.environment != production
```

## 2. 多租户模型

建议至少包含：

- Enterprise：企业，也是第一版唯一业务隔离边界；旧术语 Tenant 与 Enterprise 等价。
- User：平台用户身份。
- Membership：用户与租户之间的成员关系。
- Group：租户内用户组。
- Role：角色。
- Permission：原子权限。
- RoleBinding：用户或组在某个范围内的角色绑定。
- ServiceAccount：机器身份。
- APIKey：受限 API 凭证。

用户身份不能直接等同于企业成员。一个用户可以属于多个企业，并在每个企业中拥有不同角色和数据范围。

### 2.1 平台身份与企业身份

平台权限和企业权限必须分离：

```text
PlatformRole
    platform_super_admin

EnterpriseRole
    enterprise_admin
    security_admin
    ops_admin
    operator
    viewer
    自定义角色
```

`platform_super_admin` 不是一个拥有所有企业权限的万能角色。它只作用于平台管理域，默认不能读取企业会话、模型凭证、主机、Kubernetes、Secret 和 Tool Result。

企业管理员通过 Membership 获得企业范围内的角色。即使同一个登录身份同时具有平台角色和企业 Membership，两种身份上下文也必须明确切换并分别审计，不能隐式继承权限。

## 3. 权限命名

建议使用稳定的资源动作形式：

```text
connector.read
connector.create
connector.rotate_credential
host.read
host.create
host.execute
host.restart
kubernetes.cluster.read
kubernetes.cluster.create
kubernetes.pod.read
kubernetes.workload.restart
telemetry.read
telemetry.group.create
telemetry.collector.install
telemetry.collector.configure
telemetry.collector.upgrade
telemetry.collector.uninstall
telemetry.gateway.manage
telemetry.query.metrics
telemetry.query.logs
telemetry.query.traces
card_skill.create
card_skill.publish
audit.read
```

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

企业模型管理使用企业权限：

```text
model_provider.manage
model_deployment.manage
model_alias.manage
agent_profile.manage
model_routing.manage
model_usage.read
```

权限应作用于 API、MCP Tool 和后台任务，而不只是菜单和按钮。

## 4. 数据权限范围

每个核心业务资源至少包含：

```text
enterprise_id
project_id 或 resource_group_id
owner_user_id
owner_team_id
environment
tags
created_by
created_at
updated_at
```

数据权限可以使用以下范围组合：

- 当前租户全部资源。
- 指定项目或资源组。
- 指定团队。
- 当前用户创建或拥有的资源。
- 指定环境，例如 development、staging、production。
- 标签表达式，例如 `region=cn-east AND criticality!=high`。
- 明确资源 ID 列表。

第一版可以采用 RBAC + Scope Filter；后续再演进到 ABAC 或 ReBAC。无论实现选型如何，策略入口必须统一。

## 5. 统一授权决策

所有入口调用同一个授权服务：

```json
{
  "subject": {
    "enterprise_id": "ent-1",
    "user_id": "user-1"
  },
  "action": "host.restart",
  "resource": {
    "type": "host",
    "id": "host-1",
    "environment": "production",
    "team_id": "team-sre"
  },
  "context": {
    "origin": "card_action",
    "conversation_id": "chat-1",
    "ip": "...",
    "time": "..."
  }
}
```

决策结果不只返回 allow/deny，还应返回可审计原因：

```json
{
  "allowed": false,
  "code": "PRODUCTION_APPROVAL_REQUIRED",
  "reason": "生产环境主机重启需要额外审批"
}
```

## 6. Tool 与数据权限

每次 Tool 调用执行两层检查：

1. 调用前检查用户是否能调用该 Tool。
2. 查询或变更时强制注入数据范围，或逐资源检查。

Tool 返回值必须在进入模型上下文和卡片渲染前完成字段级脱敏。Card Skill 的字段来源约束不能替代数据权限；卡片只能消费调用者本来就有权获得的数据。

对于列表查询，服务端先把用户提交的业务过滤条件解析为白名单表达式，再与授权服务返回的 Scope Filter 做逻辑与；客户端过滤条件永远不能扩大数据权限：

```text
effective_filter = authorization_scope AND validated_user_filter
```

游标必须由服务端签名并绑定 enterprise_id、排序字段、有效过滤器哈希和有效期，不能接受客户端构造的任意数据库游标。

## 7. 风险等级与审批

建议为 Tool 定义风险等级：

| 等级 | 示例 | 默认策略 |
| --- | --- | --- |
| read | 查询主机、Pod、日志 | 可配置免确认 |
| write | 新增主机、更新配置 | 预览后确认 |
| dangerous | 删除、重启、执行命令 | 强制确认和短期授权 |
| critical | 生产批量操作、修改权限 | 多人审批或增强认证 |

Tool 的风险标注只能作为策略输入，不能被视为可信授权；服务端仍应根据实际资源和参数计算最终风险。

## 8. 审计要求

至少记录：

- 谁在什么租户发起操作。
- 来源是管理后台、模型、卡片点击、OpenAPI 还是自动化任务。
- 使用了哪个 Tool 和版本。
- 参数摘要和敏感字段脱敏结果。
- 命中的权限和策略。
- 是否经过预览、确认或审批。
- Connector 和目标资源。
- 执行结果、耗时、错误和重试。
- Card Action 对应的用户手势与 Card Instance。

审计记录应防篡改，并与普通应用日志分离保存。
