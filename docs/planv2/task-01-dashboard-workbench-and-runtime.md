# Task 01：Dashboard Workbench、Query Runtime 与资源入口

## 1. Task 定义

目标：交付不依赖 AI 也可以独立使用的企业级遥测仪表盘能力。用户可以在后台创建分组和仪表盘，逐个添加 Metrics/Logs/Traces 统计图，配置变量、时间范围、自动刷新、布局和资源绑定，并从 Host/Kubernetes 详情页快速打开。

完成后的用户闭环：

~~~text
打开 /dashboards
→ 创建 Folder 或根级 Dashboard
→ 填写 Dashboard 名称和描述
→ 进入空 Dashboard 详情页
→ 逐个创建 Panel
→ 配置查询、变量、时间范围和刷新
→ Preview/Commit 发布 Revision
→ 查看和刷新 Dashboard
→ 从 Host/Kubernetes 详情页通过绑定快速进入
~~~

实现边界：Dashboard、查询执行、权限、资源绑定和审计由同一套后端领域服务提供。Enterprise Web、Card Host 和后续 AI Inspect 都只能调用该服务，不能自行拼接查询或复制保存逻辑。

## 2. 依赖与非目标

### 2.1 上游依赖

- M2 的企业、角色、DataScope 和 AuthorizationVersion。
- M3 的 Host/Kubernetes 资源事实、资源 ID 和 Kubernetes UID。
- M4 的 PendingAction、Action Executor、Execution 和审计。
- M7/M10 的 Metrics、Logs、Traces 摄入与 Query Engine。
- Enterprise 的 @argus/ui、Design Tokens、i18n、API Client 和 E2E 基座。

### 2.2 本 Task 不实现

- /创建仪表盘 Skill、Dashboard Draft 生成和模型调用。
- @Dashboard Mention、AI 查询规划和模型总结。
- 告警规则、通知、值班和自动修复。
- 任意 SQL、任意 ClickHouse 查询、任意 Collector 配置。
- 动态标签绑定；第一版只做显式 Host/Kubernetes 绑定。

## 3. 子任务分解

### T1.1 契约、Schema 与领域不变量

固化 DashboardFolder、Dashboard、DashboardRevision、Panel、QueryTarget、DashboardVariable、DashboardBinding 的 JSON Schema、状态机、错误码、OpenAPI 和审计字段。

必须明确：

- active Revision 不可变，发布通过 active_revision_id 原子更新。
- Panel 查询保存 query_hash，浏览器不能覆盖已发布表达式。
- Binding 只缩小查询上下文，不扩大 DataScope。
- 所有写入都通过 .preview/.commit 和 PendingAction。
- 变量依赖由查询中的 $variable 自动解析，拒绝未定义变量和循环依赖。

交付物：Schema、ADR、OpenAPI、契约测试样例和错误码表。

### T1.2 PostgreSQL 与 Dashboard 领域服务

新增或固化以下表：

- telemetry_dashboard_folders
- telemetry_dashboards
- telemetry_dashboard_revisions
- telemetry_dashboard_bindings
- Dashboard 审计索引或通用审计表中的领域索引

实现领域服务：

~~~text
CreateFolderPreview/Commit
CreateDashboardPreview/Commit
UpdateDashboardPreview/Commit
ArchiveDashboardPreview/Commit
CreateOrUpdatePanelPreview/Commit
UpdateDashboardVariablesPreview/Commit
Attach/DetachBindingPreview/Commit
PublishRevision
LoadActiveRevision
~~~

要求：Repository 只负责持久化；Revision Spec、Spec Hash、validation report 不可变；Commit 使用幂等键；绑定唯一约束包含 dashboard_id、target_type、target_id、target_uid；归档和撤销保留历史事实。

### T1.3 Preview/Commit、权限与审计

Preview 必须完成：

1. 企业、Dashboard、Folder 和用户权限校验。
2. Panel 类型与查询语言兼容性校验。
3. 变量 Key、依赖图、未定义引用和循环依赖校验。
4. 布局、Panel 数量、Target 数和查询预算校验。
5. Binding 目标企业归属、DataScope、Kubernetes UID 和传播策略校验。
6. 小范围样本查询和结果类型校验。
7. 生成 Spec Hash、Diff、validation report、公开 Action Ref 和过期时间。

Commit 只接收 action_ref 或 action_binding_id，不接收可变名称、查询、变量或资源参数。

审计至少包含 Folder、Dashboard、Revision、Panel、Variable、Binding 的创建、修改、发布、归档和撤销，以及查询来源、Target、变量摘要、时间范围、预算和结果状态。

### T1.4 Dashboard Query Runtime 与 Catalog

实现统一内部接口：

~~~text
ExecuteDashboard(ctx, DashboardExecutionRequest)
  -> DashboardExecutionResult
~~~

执行流程：

~~~text
Load active Revision
→ 校验可见性和 Revision 状态
→ 解析 target context、Binding 和 DataScope
→ 解析变量依赖并校验运行时值
→ 为 Panel/Target 创建受约束查询
→ 调用 PromQL/KQL/SkyWalking Engine
→ 聚合预算、缓存、取消和超时状态
→ 结果投影、脱敏、审计
→ 返回 Panel 结果
~~~

必须支持 Metrics Range/Instant、Logs 字段投影/样本、Traces 服务/Span/时延摘要；current_target、all_bound_targets、viewer_scope；总预算、最大并发、超时、取消、最大返回量和最大扫描量；单 Panel error、partial、no_data 和整体成功的区分。

缓存键至少包含企业、Dashboard、Revision、Panel、Target、时间、变量和 AuthorizationVersion。

Catalog 提供：Metrics 指标名/Label/有限值，Logs 字段/字段值/受控全文检索提示，Traces 资源属性/Span 属性/服务/状态/时延范围。Catalog 结果必须经过 DataScope、字段白名单、脱敏、数量、长度和敏感字段限制。

### T1.5 REST、API Client 与前端数据流

建议接口：

~~~text
GET    /dashboard-folders
POST   /dashboard-folders/preview
GET    /dashboards
POST   /dashboards/preview
GET    /dashboards/{id}
GET    /dashboards/{id}/revisions
POST   /dashboards/{id}/actions/preview
POST   /dashboards/{id}/execute
GET    /dashboards/{id}/bindings
POST   /dashboards/{id}/bindings/preview
GET    /dashboards/catalog/*
~~~

前端必须通过 @argus/api-client 使用生成的契约类型；mock Adapter 和 real Adapter 共用同一 DTO，不允许页面自行扩展隐式字段。

### T1.6 Dashboard Workbench

页面：

~~~text
/dashboards
/dashboards/:dashboardId
/dashboards/:dashboardId/edit
~~~

列表页：Folder + Dashboard 混合展示、根级未分组区域、组内创建、名称/描述创建和列表搜索。

详情页：顶部名称/描述/Folder/Revision/绑定摘要；时间范围、自动刷新、立即刷新、变量下拉框；空 Dashboard 添加第一张统计图；Panel 逐个添加、调整宽高和顺序；显示查询元数据、新鲜度、加载、错误、partial 和无数据状态；右上角打开 OTLP Catalog。

Panel 编辑页只编辑当前 Panel 的标题、描述、Signal、展示类型和查询，支持 Builder/DSL、查询校验、样本运行、Preview Diff 和保存草稿，不重复承载 Dashboard 级配置。

变量编辑弹框：每个变量独立配置 Metrics/Logs/Traces 查询；查询中的 $variable 自动形成依赖；Metrics/Logs/Traces 分 Tab，先选 Collector，再选指标/字段/属性和值；宽屏自适应，Header/Footer 固定，只有正文区域纵向滚动。

### T1.7 Resource Binding 与快速入口

- Dashboard 页面选择 Host、Kubernetes Cluster、Namespace、Workload、Service。
- Host/Kubernetes 详情页展示当前用户有权查看且 Binding 有效的 Dashboard。
- 打开 Dashboard 时携带结构化 current_target。
- Kubernetes 绑定保存 cluster_id、kind、namespace、uid，名称只用于显示。
- UID 漂移、资源删除、撤权后标记 stale 或 revoked，不自动跟随同名对象。
- descendants 传播只影响导航和查询上下文，不影响权限。

### T1.8 测试、可观测性与退出门禁

必须实现：

- Domain、Repository、Schema、Preview、Commit 单元和契约测试。
- Query Runtime 的三信号、变量依赖、预算、缓存、取消、partial 和 DataScope 测试。
- Playwright 覆盖列表、创建、Panel、变量、Catalog、刷新、布局和绑定。
- 临时 Kubernetes Namespace E2E，验证服务重启、Redis 清空、重复 Commit、资源删除和 UID 漂移。
- 失败时清理 Namespace、PVC、Topic、Bucket、Lease、测试绑定和诊断文件。

## 4. 执行顺序

~~~text
T1.1 契约
→ T1.2 存储与领域服务
→ T1.3 Preview/Commit
→ T1.4 Query Runtime/Catalog
→ T1.5 REST/API Client
→ T1.6 Workbench
→ T1.7 Binding
→ T1.8 E2E 与发布门禁
~~~

## 5. Task 退出标准

- 用户可以通过后台创建并发布一个包含三种信号的真实 Dashboard。
- 用户可以逐个添加 Panel、修改查询、变量和布局，并通过统一运行时查看结果。
- UI、Card Host 和后续 AI Inspect 通过同一个 ExecuteDashboard 获得一致的权限裁剪、预算和结果语义。
- 从 Host/Kubernetes 详情页打开时，查询范围正确带入当前资源上下文。
- 未验证查询、越权资源、stale Binding、重复提交和 partial 结果均有明确状态。
- 所有写操作可审计、可恢复，页面不暴露私有 PendingAction 参数。

## 6. 主计划映射

对应主计划：P2V-0、P2V-1、P2V-2、P2V-3，以及 P2V-5 中的 UI、Runtime、Binding 和通用发布测试。

