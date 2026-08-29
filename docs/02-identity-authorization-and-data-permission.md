# 多租户、RBAC 与数据权限

> 当前基线：数据授权由 `data_authorization_grants` 独立保存。它只绑定 `user`、`department`、`role`、`service_account` 与明确的 Host 或 Kubernetes Cluster ID。标签只用于展示、搜索、普通筛选和保存视图，不参与授权判断。

## 1. 总体模型

Argus 的授权判断由四层组成：

```text
enterprise_id                  -> 租户和安全隔离
功能权限                       -> Role/Permission/RoleBinding
数据授权                       -> DataAuthorizationGrant 的明确资源 ID
操作义务                       -> MFA、审批、远程访问规则和会话策略
```

`RoleBinding` 只表示功能权限，不携带显式资源授权。用户的有效资源是：

```text
用户直接授权 ∪ 所属部门授权 ∪ 已绑定角色授权
```

不支持 Deny，也不支持从用户侧排除继承资源。用户只能移除自己的直接授权；继承授权必须在角色或部门上修改。

## 2. 身份与主体

- `EnterpriseUser` 固定属于一个企业和一个部门。
- `Department` 是用户继承数据授权的组织主体。
- `Role` 定义稳定的功能权限集合。
- `RoleBinding` 将用户、部门或 ServiceAccount 绑定到角色。
- `ServiceAccount` 是受控机器主体；APIKey 只认证到 ServiceAccount，不单独配置资源范围。
- `AuthorizationVersion` 在授权关系、角色绑定或主体组织关系变化时递增，用于失效游标、会话、PendingAction 和缓存。

平台超级管理员与企业身份互斥，不能因为平台身份进入企业资源范围。

## 3. 显式资源授权

```text
data_authorization_grants
├── enterprise_id
├── subject_type: user | department | role | service_account
├── subject_id
├── resource_type: host | kubernetes_cluster
├── resource_id
├── status / version
├── created_by
└── timestamps
```

授权关系必须同时满足主体和资源属于同一企业。查询返回资源名称、直接/继承来源和主体授权版本。批量变更使用 `expected_version`，事务中完成写入、审计和授权失效通知。

角色授权变更前显示受影响成员数量，并一次性递增相关用户的 `AuthorizationVersion`。授权查询采用并集，不实现 Deny。

Kubernetes 只授权 Cluster；Namespace、Pod、Workload、Service 等下级对象继承 Cluster 授权，不提供 Namespace 独立授权。

## 4. 资源与操作行为

- 列表和详情：需要功能读取权限，并按明确资源 ID 过滤。无授权详情统一返回不可见结果，避免泄露资源存在性。
- 编辑和删除：同时需要对应操作权限、读取权限和目标资源授权。
- 创建：只检查创建功能权限，不要求已有数据授权。
- 创建成功后：在资源创建事务中给实际创建主体授予该资源的只读授权。
- Host 授权统一复用于 Telemetry、Agent、远程访问和 Connector 资源校验。
- Kubernetes Cluster 授权统一复用于其所有下级对象。

创建授权不能被“全量主体”或空资源列表隐式扩大；资源 ID 必须在提交事务中真实存在后写入授权关系。

## 5. 标签边界

Host 和 Kubernetes Cluster 保留 `labels: Record<string,string>`，并继续执行字符集、数量、长度和 `argus.io/*` 保留命名空间校验。标签可用于页面展示、搜索、普通列表筛选、保存视图和资源归类。

标签不能用于创建授权范围、动态扩大资源授权、Remote Access Grant/Rule 的授权判断、Approval Policy 的匹配、AuthorizationVersion 失效或授权影响分析。因此标签变化不会改变授权结果，也不会递增授权版本。

## 6. 远程访问与遥测

远程访问先检查显式 Host 授权，再检查 RemoteAccessGrant、ManagedAccount、协议、动作和有效期。满足这些基础条件即可访问；RemoteAccessRule 是可选叠加层，用于拒绝、MFA、审批、通知或收紧 SessionProfile，不能补齐或扩大缺失的功能权限、资源授权和 Grant。没有匹配 Rule 时使用系统安全默认 SessionProfile。

Telemetry Query 在受信 `EnterpriseId + ResourceId + CollectorId` 基础上应用显式授权资源 ID、Signal 权限、字段脱敏、时间范围和查询预算。Web、Agent、Card 和 OpenAPI 复用同一裁剪结果。

## 7. 管理入口

组织设置不再提供独立“权限管理”页面。角色、用户、部门和 ServiceAccount 列表均提供“数据授权”入口：

- 弹框包含 Host、Kubernetes 两个 Tab。
- 左侧是未授权资源，右侧是已授权资源。
- 支持搜索、分页、多选、批量移动和全部移动。
- 用户/部门弹框展示直接与继承来源，继承资源不可直接移除。
- 角色保存显示受影响成员数量并要求风险确认。

角色编辑器仍保留功能权限矩阵；删除的是独立显式资源授权管理页，不是功能权限管理。

用户编辑抽屉同时承担部门与用户直接角色绑定管理：具备 `identity.manage + role.manage` 的管理员可在一次命令中调整部门并批量替换用户主体的直接角色，部门主体继承的角色只读展示并标明来源，不能从用户侧移除。该应用层命令由 Authorization 服务协调，Identity 仍拥有 EnterpriseUser 数据，Authorization 仍拥有 RoleBinding 数据；服务端在同一事务中依次锁定企业和用户、校验 `expected_user_version + expected_authorization_version`、更新两个领域的数据、写审计并使目标用户授权失效，不能由前端循环调用单条 RoleBinding API 或先后调用两个接口拼接。企业必须始终保留至少一名通过直接或部门角色合计获得 `identity.manage + role.manage` 的有效用户。

## 8. API 约束

```text
GET  /api/v1/enterprise/data-authorizations/{subject_type}/{subject_id}
POST /api/v1/enterprise/data-authorizations/{subject_type}/{subject_id}
GET  /api/v1/enterprise/users/{user_id}/role-assignments
PUT  /api/v1/enterprise/users/{user_id}/role-assignments
```

GET 按 `resource_type=host|kubernetes_cluster` 查询资源目录和授权来源。POST 支持批量添加/移除、`expected_version` 和事务化审计。继承授权的移除请求必须失败，不能通过用户主体绕过角色或部门授权。

用户角色分配 GET 返回 `direct_role_ids`、带部门来源的 `inherited_roles`、`effective_role_ids` 和 `authorization_version`。PUT 请求包含 `department_id`、`role_ids`、`expected_user_version` 和 `expected_authorization_version`，原子更新部门与永久有效的用户直接角色；部门继承角色不在请求体中，而是依据更新后的部门重新计算。命令仅递增目标用户的相关版本并失效其 Session、远程访问租约和相关运行时对象。

## 9. 迁移原则

项目处于开发阶段，允许破坏性迁移。旧 `data_scopes`、`role_binding_data_scopes`、`service_account_data_scopes` 和相关 selector 字段直接删除，不保留兼容适配层。OpenAPI、Go、TypeScript 和 sqlc 生成物必须由生成工具同步。
