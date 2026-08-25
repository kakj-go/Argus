# PlanV2：遥测仪表盘、AI 创建与资源绑定

## 1. 目标与非目标

### 1.1 目标

交付一条可审计、可授权、可恢复的闭环：

```text
后台或 Chatbox 创建 Dashboard
→ 生成/编辑 DashboardRevision
→ 查询语句、变量、资源绑定和预算验证
→ 用户确认 Commit
→ 从 Dashboard 页面查看和刷新
→ 从 Host/Kubernetes 详情页一键进入
→ 用户通过 `@` 明确引用已发布 Dashboard
→ 执行该 Revision 中的 Metrics/Logs/Traces 查询
→ 将确定性证据投影给模型并生成总结
```

目标用户体验：

- 管理员可创建“支付服务健康度”“主机资源趋势”等企业仪表盘。
- 一个页面可以同时展示指标趋势、日志表格、Trace 慢请求和统计卡片。
- 页面顶部统一控制时间范围、变量和自动刷新。
- 从主机、Kubernetes Cluster、Namespace、Workload 或 Service 详情页点击“关联仪表盘”即可打开对应视图。
- 用户可以输入“`@支付服务健康度` 最近一小时有没有问题”，AI 读取该仪表盘的已发布查询配置，自行选择查询顺序并基于证据总结。
- 用户通过 `/创建仪表盘` 显式调用创建 Skill，AI 生成 JSON Draft，用户点击确认后才真正保存。

### 1.2 非目标

第一阶段不实现：

- 告警规则、通知渠道、值班排班和自动修复；Panel 阈值只用于展示和分析证据。
- 任意 SQL、任意 ClickHouse 查询、任意 Collector 配置或任意 DSL 注入。
- 重新实现 PromQL、KQL 或 Trace GraphQL；继续使用 M10 三个独立 Engine。
- 用一个新的统一查询 AST 替换现有三种 Query Wire Format。
- 把 Dashboard 做成可执行任意 Tool 的 Card。
- 个人私有 Dashboard；第一版沿用企业级可见性模型。
- 通过绑定绕过 DataScope，或把绑定当作授权授予机制。
- 第一阶段的动态标签绑定、大规模语义向量搜索和未引用 Dashboard 的模糊自动匹配。

## 2. 与现有架构的关系

PlanV2 不改变已确定的服务边界：

| 能力 | 复用的现有边界 |
| --- | --- |
| Dashboard 持久化和 REST | `argus-server` 领域服务、PostgreSQL、OpenAPI |
| Dashboard 执行 | `argus-telemetry query` 的 M10 PromQL/KQL/SkyWalking Engine |
| AI 查询和创建 | M4 Agent Harness、Tool Registry、Run、ToolResultProjection |
| 用户确认和提交 | M4 PendingAction、Action Binding、Action Executor、Execution |
| 会话内预览和确认 | M5 Card Runtime、RenderPlan、Binding ID |
| 图表、日志、Trace 展示 | 现有 `@argus/ui` Telemetry 组件和 ECharts |
| 权限 | M2 RoleBinding/DataScope/AuthorizationVersion |
| 资源入口 | M3 Host/Kubernetes 资源事实和资源详情路由 |

Dashboard 不是 `TelemetryGroup`。`TelemetryGroup` 继续只表示 Collector 网络拓扑；Dashboard 的分组使用 `DashboardFolder`。

## 3. 领域对象

### 3.1 DashboardFolder

企业内的展示分组，不承担权限或资源授权。

```text
DashboardFolder
├── id
├── enterprise_id
├── name
├── description
├── sort_order
├── created_by / updated_by
├── created_at / updated_at
└── status = active | archived
```

第一阶段一个 Dashboard 只能属于一个 Folder；允许根级 Dashboard（`folder_id = null`）。Folder 的创建、改名、归档使用 Preview/Commit。

产品界面以 Folder + Dashboard 混合目录作为 `/dashboards` 的第一入口：

- 目录默认展示所有分组，每个分组下面直接列出该分组的 Dashboard。
- `folder_id = null` 的 Dashboard 放在“未分组”根级区域，不需要人为创建一个“未分组”实体 Folder。
- 点击分组进入组内视图，组内仍然可以继续创建多个 Dashboard；新建入口默认继承当前 Folder。
- Dashboard 的 `name` 和 `description` 在创建时单独填写，创建成功后再进入 Dashboard 详情页添加统计图。
- 分组只负责导航和组织，不改变 Dashboard 的权限、DataScope、Binding 或查询执行边界。

### 3.2 Dashboard

Dashboard 是稳定的用户入口和权限对象，当前生效内容由 `active_revision_id` 指向不可变 Revision。

```text
Dashboard
├── id
├── enterprise_id
├── folder_id
├── name
├── description
├── aliases[] / tags{}
├── active_revision_id
├── lifecycle = draft | active | archived
├── visibility = enterprise | restricted
├── created_by / updated_by
├── created_at / updated_at
└── archived_at
```

`name` 和 `description` 是 `@` 候选列表的主要过滤字段；`aliases` 和 `tags` 是可选增强，不允许把敏感资源名称或日志正文写入搜索索引。它们不构成没有显式 `@` 时的自动选择依据。

### 3.3 DashboardRevision

Revision 采用不可变 JSON Spec；修改任何 Panel、变量、布局、默认时间、刷新间隔或绑定建议都会创建新 Revision。

```text
DashboardRevision
├── id
├── dashboard_id
├── revision_number
├── schema_version = argus.telemetry_dashboard/v1
├── spec_json
├── spec_hash
├── validation_status = pending | valid | invalid
├── validation_report_json
├── query_catalog_snapshot_json
├── created_by / created_at
└── published_at
```

只有 `validation_status=valid` 且被 `active_revision_id` 引用的 Revision 可以被普通 Dashboard 执行或 AI Skill 使用。草稿、无效 Revision 只能在编辑页和 Preview 中查看。

### 3.4 DashboardSpec

```json
{
  "schema_version": "argus.telemetry_dashboard/v1",
  "default_time_range": {"kind": "relative", "seconds": 3600},
  "default_refresh_seconds": 0,
  "variables": [],
  "panels": [],
  "layout": {"columns": 12, "row_height": 8}
}
```

Spec 不直接保存资源绑定。绑定是独立对象，避免 Dashboard 配置和资源生命周期耦合。

### 3.5 Panel 与 QueryTarget

一个 Dashboard 可以混排三种信号；一个 Panel 只属于一个 Signal，但可包含多个同语义 Target。

```text
Panel
├── id / title / description
├── type = time_series | stat | table | logs | trace_list | trace_timeline
├── signal = metrics | logs | traces
├── targets[]
├── layout = {x, y, w, h, min_w, min_h}
├── unit / decimals
├── thresholds[]
├── legend / display_options
└── links[]

QueryTarget
├── id
├── language = promql | kql | skywalking_graphql
├── expression
├── pipeline
├── operation_name / variables_schema
├── query_mode = instant | range
├── min_step_seconds
├── required_variables[]
├── scope_mode = current_target | all_bound_targets | viewer_scope
├── budget
└── query_hash
```

`expression` 仍由对应 Engine 的 Parser/Validator 校验。保存时生成 `query_hash`，执行时用 Revision 中的查询，不允许浏览器修改已发布查询文本。

Panel 采用逐个创建的生命周期：Dashboard 可以先以空 Spec 发布，详情页显示“添加第一张统计图”空态；用户每次添加一个 Panel 后再填写标题、说明、Signal、展示类型和查询。Panel 的布局字段 `layout.w/h` 在详情页可以通过拖拽或扩大/缩小操作更新，布局变更会进入新的 Revision，但不会改变 Panel 的查询哈希。

### 3.6 DashboardVariable

变量是 Dashboard 级的查询定义和运行时选择器。Argus 不把 Metrics、Logs、Traces 强行归一化成同一个字段模型，而是允许每个变量独立选择信号、编写选项查询，并把查询结果映射成下拉选项。

```text
DashboardVariable
├── key / label
├── kind = query | interval
├── signal = metrics | logs | traces
├── query
├── extract = label_values | field_values | attribute_values | static_values
├── value_field / text_field
├── dependencies[]            # 由 query 中的 $variable 自动解析
├── selection = single | multi | multi_all
├── default_values[] / max_values
├── refresh = dashboard_load | time_range_change | manual
└── collector / source_item    # 可选的目录来源 provenance
```

变量应用规则：

- Panel 查询中的变量引用（如 `$service`）是变量真正生效的唯一事实；变量定义不会隐式修改所有 Panel。
- 后一个变量只需在自己的查询中引用前一个变量，例如 `label_values(kube_pod_info{namespace=~"$namespace"}, pod)`，系统自动解析 `dependencies`，不提供容易与查询语义冲突的手工依赖下拉框。
- 保存时构建变量依赖图，拒绝未定义变量和循环依赖；运行态按依赖顺序刷新下拉框，前置变量没有值时禁用后置变量。
- `signal` 只决定查询解析器和结果语义：Metrics 通常使用 `label_values`，Logs 使用字段值，Traces 使用属性值或受控范围值。三种变量可以在同一 Dashboard 中串联，是否被某个 Panel 使用仍只看 Panel 查询是否显式引用其 Key。
- 值必须经过类型、长度、数量、允许字段、资源范围和原生 Parser 重新校验；失败时整个 Variable/Panel 返回结构化错误，不执行未经校验的字符串。

变量编辑弹框采用“左侧变量链 + 右侧查询编辑器”：右侧依次提供基本信息、Metrics/Logs/Traces 查询、结果提取映射、自动依赖、结果预览和 Panel 引用关系。编辑态显示查询与依赖诊断；运行态只显示递进式下拉框。可以使用辅助按钮把 `$variable` 插入选中的 Panel，但不会静默改写手写查询。

弹框按桌面 Web 端宽屏视口设计，使用稳定的最大内容宽度；移动端收缩布局不属于当前版本实现或验收范围。Header 和 Footer 始终固定可见；外层弹框、变量链和查询编辑器不创建独立纵向滚动，只有正文区域承担滚动，避免出现两个同步滚轮。查询文本框仅在自身内容超出时允许内部滚动。

#### 设计来源与取舍

- Grafana：作为执行语义基线。Dashboard 变量是模板上下文，Panel 查询显式引用变量，变量之间通过查询引用形成链路。
- SigNoz：作为 OpenTelemetry 三信号和值发现的参考，保留 Dynamic Variables / Related Values 的可观测性语义，但不把不同信号的查询强行合成一个 AST。
- OpenObserve：作为字段发现、结果映射和依赖配置体验的参考。

因此 Argus 的最终模型是“Grafana 的显式引用 + SigNoz 的跨信号语义 + OpenObserve 的配置体验”，而不是复制某一个产品的变量对象。

### 3.7 OTLP Catalog 与指标发现

Dashboard 详情页右上角提供“指标查询”入口。该入口打开受权限和 DataScope 裁剪的 OTLP Catalog，不直接修改 Panel 查询：

1. Catalog 顶部先按信号显示三个 Tab：`Metrics`、`Logs`、`Traces`；切换 Tab 会重置当前插件和字段选择，避免把不同信号的语义混在一起。
2. 每个 Tab 下先展示当前信号可用的 OTLP Collector 插件列表，例如 `collector-prometheus`、`collector-hostmetrics`、`collector-otlp-logs` 或 `collector-otlp-traces`；插件是指标/字段目录的来源边界，点击插件后才加载它提供的条目。
3. Metrics 插件列出可用指标名；点击某个指标后在该指标行下方展开可用 Label 及其有限值，例如 `service`、`status`、`instance`。
4. Logs 插件不伪装成“指标列表”，而是展示可过滤日志字段；点击字段行后展开字段和值，例如 `service.name`、`severity_text`、`http.route`、`body`。字段值和全文检索都必须有长度、数量和敏感字段限制。
5. Traces 插件展示资源属性、Span 属性和受控派生字段；点击属性行后展开属性和值，例如 `service.name`、`span.name`、`status.code`、`duration_ms`。时延字段使用操作符和值范围，不把每一条 Trace 或 Span 当作下拉选项。
6. 选择字段和值后可以“带入过滤条件”，生成结构化变量/过滤 Draft；最终仍由对应 Query Engine Parser 校验，不能把 Catalog 文本直接拼接进已发布查询。

日志和 Trace 的推荐界面模型是“字段目录 + 值/范围 + 查询语义”，而不是 Metrics 的“指标名 + Label”模型：

```text
OTLP Logs
  service.name       -> payment-api / order-api
  severity_text      -> ERROR / WARN / INFO
  http.route         -> /v1/payments / /health
  body               -> contains(timeout | retry)  # 受控全文检索

OTLP Traces
  service.name       -> payment-api / risk-provider
  span.name          -> POST /v1/payments/authorize
  status.code        -> ERROR / OK
  duration_ms        -> >400 / >1000 / between(100,400)
```

这样可以让 Agent 在 `@Dashboard` 分析时理解“过滤维度”和“证据对象”的差异：Metrics 返回时序/聚合值，Logs 返回字段投影和日志样本，Traces 返回服务、Span、状态和时延摘要。

### 3.8 DashboardBinding

绑定既是资源详情页的快捷入口，也是 Dashboard 查询的上下文来源，但不是授权边界。

```text
DashboardBinding
├── id / enterprise_id / dashboard_id
├── target_type = host | kubernetes_cluster | kubernetes_namespace |
│                kubernetes_node | kubernetes_workload | kubernetes_service | kubernetes_pod
├── target_id
├── target_uid              # Kubernetes 对象使用稳定 UID
├── cluster_id / namespace  # Kubernetes 对象保留结构化定位
├── propagation = direct | descendants
├── binding_mode = fixed    # dynamic label/selector 进入后续阶段
├── context_mapping_json
├── status = active | stale | revoked
├── created_by / created_at / revoked_at
└── last_verified_at
```

Kubernetes 绑定必须保存 `cluster_id + kind + namespace + uid`，不能只保存名称。对象 UID 变化时绑定标记为 `stale`，不自动指向同名新对象。

第一阶段支持 Host、Kubernetes Cluster、Namespace、Workload、Service 的直接绑定；Pod 绑定可作为排障能力但默认不向所有页面传播。按 Host 标签或 Kubernetes Label Selector 的动态绑定进入后续阶段，并且必须有明确的成员快照和刷新策略。

## 4. Dashboard 查询执行

### 4.1 服务端执行原则

前端和 AI 都调用同一个 Dashboard Query Service：

```text
Load active DashboardRevision
→ 校验 Dashboard/Revision 可见性
→ 解析当前 target、绑定资源和变量
→ 重新计算 DataScope
→ 为每个 Panel/Target 建立查询请求
→ 进入 M10 Query Coordinator
→ 结果投影、脱敏、预算统计和审计
→ 返回按 Panel 分组的结果
```

不能让浏览器读取 Dashboard Spec 后自行拼接三类查询；否则会产生权限、变量注入和审计口径分叉。

建议新增内部接口：

```text
ExecuteDashboard(ctx, DashboardExecutionRequest) -> DashboardExecutionResult
```

请求包括 `dashboard_id`、可选 `revision_id`（仅管理员 Preview/调试）、`target_context`、时间范围、变量值、请求来源和预算上限。普通查看固定使用 active Revision。

结果按 Panel 返回：

```text
DashboardPanelResult
├── panel_id / target_id / signal / result_type
├── data
├── query_meta
├── warnings[]
├── partial
├── error_code（单 Panel 失败时）
└── evidence_ref
```

单个 Panel 失败不应伪装为空数据，也不应让其他 Panel 的成功结果丢失。整体结果必须带 `partial=true` 和可解释的错误列表。

### 4.2 时间和刷新

Dashboard 顶部提供统一时间上下文：

- 相对时间与绝对时间。
- 手动刷新。
- 自动刷新：关闭、30 秒、1 分钟、5 分钟、15 分钟。
- 页面不可见时暂停刷新；请求进行中不重入。
- 时间/变量改变时取消旧请求，避免旧结果覆盖新结果。
- Metrics Range Query 的 step 根据时间范围和图表宽度计算，并受 Panel 的 `min_step_seconds` 限制。

时间范围、刷新间隔和变量可以编码到分享 URL；只有用户明确保存默认值时才创建新 Revision。

### 4.3 缓存与预算

允许短期只读缓存，键至少包含：

```text
enterprise_id + dashboard_id + revision_id + panel_id
+ target_context + time_range + variables + authorization_version
```

缓存不能跨 AuthorizationVersion、企业或脱敏权限复用。Dashboard 级预算包含最大 Panel 数、Target 数、总扫描字节、总返回字节、最大并发和单 Panel 超时；仍必须遵守 M10 的 `MaxSamples/MaxSeries/MaxRows` 硬上限。

## 5. AI Skill 与工具

### 5.1 分析入口与 Skill 职责

新增一个内置 Provider-neutral Skill：`telemetry_dashboard_analysis`。

#### 显式 `@` 引用

分析 Dashboard 必须沿用 Chatbox 现有资源引用交互：用户输入 `@` 后从候选列表选择 Dashboard，前端插入一个带稳定 ID 的 mention chip，而不是只插入可变名称文本。

```text
用户输入：@支付服务健康度 最近一小时有没有问题？
客户端结构化消息：
mentions: [{kind: "dashboard", id: "db_01...", label: "支付服务健康度"}]
text: "最近一小时有没有问题？"
```

服务端以 `kind=dashboard + id` 为事实，`label` 仅用于显示。用户只写“支付服务有没有问题”而没有 `@` 引用时，分析 Skill 不应从全企业 Dashboard 中猜一个；应提示用户使用 `@` 选择仪表盘。名称/描述可以用于 `@` 候选列表过滤，但不是隐式执行依据。

Skill 负责：

1. 从 `@` 引用和用户语句识别 Dashboard、资源上下文、时间范围和问题类型。
2. 调用 `telemetry.dashboard.get` 获取当前用户可见的 active Revision 和 Panel 查询定义。
3. 读取各 Panel 的查询语言、查询表达式、变量约束、目标范围和 Panel 类型，自行决定先查哪些 Panel、是否并行以及是否需要补查。
4. 使用已有的 Metrics/Logs/Traces Query Tool 执行查询，并携带 Dashboard provenance。
5. 根据确定性证据生成面向用户的总结，并引用 Dashboard、Revision、Panel、时间范围和结果状态。

Skill 不负责：

- 在没有 `@` 引用时自动猜测 Dashboard。
- 修改 active Revision 的查询语句后把修改后的查询伪装成 Dashboard 结果。
- 绕过 Dashboard 权限、DataScope 或资源绑定。
- 把模型摘要写回 Dashboard 作为事实。
- 自动创建告警或执行修复动作。

### 5.2 只读工具

```text
telemetry.dashboard.get
telemetry.dashboard.binding.list
telemetry.dashboard.inspect
telemetry.dashboard.catalog.metric_names
telemetry.dashboard.catalog.attribute_values
telemetry.dashboard.catalog.log_fields
telemetry.dashboard.catalog.trace_services
```

`@` 候选列表使用一个受授权的 Dashboard mention resolver，按名称、描述、别名和标签过滤，但只返回当前企业内用户有权查看的 Dashboard。Resolver 不是模型可随意调用的语义搜索 Tool，也不在没有 `@` 时自动选择 Dashboard。

`get` 返回活动 Revision 的安全分析上下文。查询语句本身不是秘密，可以提供给模型用于规划，但必须是只读、带哈希和带来源的定义：

```text
DashboardAnalysisContext
├── dashboard_ref / revision_ref
├── target_context / allowed_resource_ids
├── panels[]
│   ├── panel_id / title / signal / visualization
│   ├── language / expression / pipeline / operation_name
│   ├── query_hash
│   ├── required_variables / allowed_variables
│   ├── scope_mode / budget
│   └── supported_query_tools[]
└── context_expiry
```

模型不得从上下文恢复数据库内部 ID、私有 PendingAction 参数、Secret 或未裁剪结果；`expression` 只允许用于调用受约束的 Query Tool。

### 5.3 Dashboard provenance 与指标查询 Tool

分析 Skill 不强制一次性执行所有 Panel，而是允许 Agent 根据用户问题选择 Panel 和顺序。例如先查错误率和 P95，发现异常后再查错误日志和慢 Trace。这正是 Skill 与固定 Dashboard 渲染的区别。

但是，Agent 不能借此把 Dashboard 分析变成任意查询入口。Metrics/Logs/Traces Query Tool 在 Dashboard 分析模式下必须携带服务端可验证的 provenance：

```json
{
  "dashboard_ref": "db_01...",
  "revision_ref": "rev_07...",
  "panel_id": "error-rate",
  "query_hash": "sha256...",
  "target_context": {"type": "host", "id": "host_01..."},
  "from": "...",
  "to": "..."
}
```

服务端重新读取 active/指定 Revision，核对 `panel_id + query_hash + language + expression`，并将目标资源裁剪为：

```text
DashboardBinding/Target Context
∩ 当前用户 DataScope
∩ 当前 AuthorizationVersion
```

查询表达式被修改、Panel 不属于该 Revision、Revision 已失效、目标不在绑定范围或查询 Tool 与 Signal 不匹配时，调用必须拒绝。普通的通用遥测查询 Tool 仍可保留给独立诊断场景，但不能被伪装成 Dashboard 证据。

`inspect` 是一个方便的组合 Tool；它可以由服务端直接执行所有 Panel，也可以由 Analysis Skill 分解为带 provenance 的 `telemetry.promql.query`、`telemetry.kql.query` 和 `telemetry.skywalking.trace` 调用。两条路径必须复用同一授权、预算和结果投影。

### 5.4 `inspect` 输入

```json
{
  "dashboard_id": "db_01...",
  "target_context": {"type": "host", "id": "host_01..."},
  "time_range": {"from": "...", "to": "..."},
  "variables": {"service": ["payment-api"]},
  "panel_ids": ["latency", "errors"]
}
```

`inspect` 本身从 active Revision 恢复查询，不接受 `expression`、`pipeline` 或任意 SQL 作为输入。返回模型安全的 `DashboardEvidenceProjection`：

```text
DashboardEvidenceProjection
├── dashboard_id / revision_id / dashboard_name
├── target_context / effective_resource_ids
├── time_range / data_freshness
├── panels[]
│   ├── panel_id / title / signal
│   ├── summary_stats
│   ├── top_series / top_log_patterns / slow_traces
│   ├── threshold_observations
│   ├── query_meta / warnings / partial
│   └── evidence_ref
└── projection_hash
```

完整大结果继续保存在 Tool Result/Artifact Store；模型只收到确定性摘要、有限样本和 `evidence_ref`。所有结论都应能回溯到 `dashboard_id + revision_id + panel_id + query_meta`。

### 5.5 AI 总结的证据约束

模型回答应区分：

- `observed`：查询直接观察到的事实。
- `inferred`：模型基于多个事实作出的推断。
- `unknown`：数据为空、partial、权限裁剪或查询失败导致无法判断。

没有证据的 Panel 不能被描述为“正常”。如果只查询到了当前用户 DataScope 的部分资源，必须明确说明范围。

## 6. AI 创建与 UI 创建

### 6.1 显式创建 Skill 与工具

创建 Dashboard 必须由用户显式输入 `/` 命令并选择创建 Skill，例如：

```text
/创建仪表盘 创建生产支付服务健康度，包含错误率、P95、错误日志和慢 Trace
```

中文、英文和其他 locale 只改变命令的显示文案；内部命令 ID 固定为 `telemetry.dashboard.create`，避免把自然语言文案当作协议。

`telemetry_dashboard_create` Skill 的职责是：

1. 收集名称、描述、Folder、Panel、变量、默认时间和可选绑定目标。
2. 按 `argus.telemetry_dashboard/v1` JSON 结构生成一个完整 Draft。
3. 必要时调用 Catalog Tool 查找真实指标、Label、日志字段和 Trace 服务。
4. 调用唯一的 JSON 接收工具进行服务端校验和 Preview。
5. 将 Preview 交给用户确认；不在 Skill 或模型中执行 Commit。

Skill 的 Prompt/Schema 可以描述 JSON 结构和字段含义，但它不是安全边界；安全边界是下面的 Tool Schema 和领域服务校验。

```text
telemetry.dashboard_folder.create.preview
telemetry.dashboard_folder.create.commit
telemetry.dashboard_folder.update.preview
telemetry.dashboard_folder.update.commit

telemetry.dashboard.create.preview
telemetry.dashboard.create.commit
telemetry.dashboard.update.preview
telemetry.dashboard.update.commit
telemetry.dashboard.archive.preview
telemetry.dashboard.archive.commit

telemetry.dashboard.binding.attach.preview
telemetry.dashboard.binding.attach.commit
telemetry.dashboard.binding.detach.preview
telemetry.dashboard.binding.detach.commit
```

所有 `.commit` 只存在隐藏 Action Catalog，不能出现在 Model Agent Tool Registry 中。产品文案可以写“准备并确认”，协议继续使用项目已经冻结的 `.preview/.commit`。

### 6.2 `dashboard.create.preview`

`telemetry.dashboard.create.preview` 是唯一的 Dashboard 创建入口，直接接收结构化 JSON Draft。Preview 输入可以来自 UI 表单或 `/创建仪表盘` Skill 生成的 Draft：

```text
DashboardDraft
├── name / description / folder_id
├── spec
├── proposed_bindings[]
└── source_request（可选，供审计和诊断，不作为执行事实）
```

Tool 的 Input Schema 必须设置 `additionalProperties=false`，服务端不得接受 Skill 之外的自由字段。Preview 必须：

1. 校验名称、描述、Folder、Panel 数量、布局和 Schema。
2. 校验每个查询语言、字段、GraphQL Operation、变量和预算。
3. 对查询做小范围样本执行，确认返回类型与 Panel 类型兼容。
4. 校验绑定目标、资源企业归属、Kubernetes UID、DataScope 和传播策略。
5. 生成不可变 DashboardRevision 草稿、Spec Hash、查询校验报告和预览数据。
6. 生成公开 `argus.pending_action/v1`、`action_ref`、风险、过期时间；私有 Token 仅保存在服务端。

Preview 失败不能创建 active Dashboard。局部 Panel 失败时必须指出 Panel、查询语言、错误码和修复建议。

### 6.3 Commit

用户点击 Card 或后台确认按钮后：

```text
浏览器 → action_binding_id/action_ref
→ argus-server Action Executor
→ 读取私有 Token 和不可变计划
→ 重新检查权限、DataScope、AuthorizationVersion、资源版本
→ 原子创建 Dashboard、Revision、Binding
```

Commit 不接受 Dashboard 名称、查询文本、资源 ID 或变量等业务参数。响应丢失时使用 Execution ID 查询，不重复创建。

UI 直接创建和 AI 创建必须调用相同的领域服务。UI 不能绕过 Preview；模型也不能绕过用户确认。

### 6.4 AI 生成策略

模型生成 Dashboard 前应优先调用 Catalog Tool 确认真实指标、Label、日志字段、Trace 服务和资源。生成结果必须是结构化 Draft JSON，而不是直接生成前端代码或 Markdown 配置。Tool 负责解析 JSON、校验 Schema、验证每个查询和生成 Preview；模型不能通过提示词声称“已验证”。

当服务或资源存在多个候选时，Skill 必须通过 `@` 资源选择或明确澄清让用户选择。不存在可验证查询时，Dashboard 只能保存为 Draft，不能进入 active。

## 7. 资源绑定与后台快速查询

### 7.1 绑定语义

一个 Dashboard 可以绑定多个 Host 或 Kubernetes 对象。绑定关系支持：

- `direct`：只在目标对象详情页显示。
- `descendants`：允许向 Namespace、Workload、Service 等下级对象传播。

第一阶段只支持显式资源绑定。Host 标签和 Kubernetes Label Selector 的动态绑定进入后续阶段，并必须保存成员快照、刷新时间和变更审计。

绑定不能扩大权限；从资源页打开 Dashboard 后，查询有效资源集合是：

```text
当前 target context
∩ Dashboard binding
∩ 当前用户 DataScope
```

### 7.2 查询上下文

Panel 的 `scope_mode`：

```text
current_target       从资源详情页进入时只查询当前对象
all_bound_targets    查询当前 Dashboard 的全部有效绑定对象
viewer_scope         查询用户 DataScope 中允许且与 Panel 资源类型匹配的对象
```

从 Host 页面打开：

```text
/enterprise/dashboards/{dashboard_id}?target_type=host&target_id={host_id}
```

Kubernetes 对象使用结构化上下文：

```text
cluster_id + kind + namespace + uid
```

名称只能用于显示，不能作为稳定身份或自动重新绑定依据。

### 7.3 资源详情页

Host、Kubernetes Cluster、Namespace、Workload、Service 详情页增加统一的“关联仪表盘”区域：

- 展示当前用户有权查看且绑定有效的 Dashboard。
- 支持置顶一个默认 Dashboard，默认值仍受权限检查。
- “打开”进入 Dashboard 页面并自动设置 `current_target`。
- 绑定为 `stale`、资源被撤权或 Dashboard 被归档时不展示为可用入口，只显示原因给有管理权限的用户。

Dashboard 编辑页增加“绑定资源”按钮：

- Host/Kubernetes 分栏选择器。
- Kubernetes 按 Cluster → Namespace → Workload/Service/Pod 展开。
- 绑定前显示受影响 Panel、变量可用性和预计查询范围。
- 解绑、批量绑定和传播策略修改均走 Preview/Commit。

## 8. REST 与内部接口草案

### 8.1 REST

建议新增 `/enterprise` 下的资源：

```text
GET    /dashboards
POST   /dashboards/preview
POST   /dashboards/{id}/actions/preview
GET    /dashboards/{id}
GET    /dashboards/{id}/revisions
POST   /dashboards/{id}/execute
GET    /dashboards/{id}/bindings
POST   /dashboards/{id}/bindings/preview
GET    /dashboard-folders
POST   /dashboard-folders/preview
```

`POST /dashboards/{id}/execute` 接受时间范围、变量、当前 Target 和 Panel IDs，不接受查询文本。UI、Card Host 和 AI Inspect 最终都调用同一个 Dashboard Query Service。

### 8.2 内部服务

```text
CreateDashboardPreview
UpdateDashboardPreview
ExecuteDashboard
SearchDashboards
InspectDashboard
ResolveDashboardBindings
```

HTTP Handler、MCP Tool、后台 Worker 不得各自拼接 Dashboard 查询或资源绑定逻辑。

## 9. 存储与索引

建议新增 PostgreSQL 表：

```text
telemetry_dashboard_folders
telemetry_dashboards
telemetry_dashboard_revisions
telemetry_dashboard_bindings
telemetry_dashboard_audit_events（如通用审计索引不足）
```

关键约束：

- 每张表都有 `enterprise_id`，并建立企业范围索引。
- `dashboard_revisions` 的 `spec_json`、`spec_hash` 和 `validation_report_json` 不可变。
- `dashboard_bindings` 对 `(dashboard_id, target_type, target_id, target_uid)` 做唯一约束。
- Kubernetes 绑定索引覆盖 `(enterprise_id, cluster_id, namespace, target_type, target_uid)`。
- Dashboard 的 active Revision 只能通过事务更新指针。
- Folder、Dashboard 和 Binding 的删除优先使用归档/撤销，保留审计和历史 Revision。
- 不在 ClickHouse 增加 Dashboard 专属事实表；Dashboard 查询继续读取 Metrics/Logs/Traces 现有租户表。

## 10. 权限、审计与安全不变量

### 10.1 权限

建议新增权限：

```text
telemetry.dashboard.read
telemetry.dashboard.create
telemetry.dashboard.update
telemetry.dashboard.archive
telemetry.dashboard.bind
telemetry.dashboard.inspect
telemetry.dashboard.catalog.read
```

Dashboard 权限不能授予遥测数据权限。每次读取、执行、AI Inspect、绑定和 Commit 都重新校验：企业状态、Dashboard 可见性、Signal 权限、DataScope、字段脱敏权限和 AuthorizationVersion。

### 10.2 审计

审计至少记录：

- Dashboard/Folder/Revision 的创建、发布、修改、归档和恢复。
- 查询来源（`admin_ui`、`model_agent`、`card_runtime`）、Dashboard、Revision、Panel、Target、变量摘要、时间范围、预算和结果状态。
- Binding attach/detach、传播策略和资源 UID。
- AI 生成的原始用户请求引用、Draft Hash、Preview/Commit 关联和用户确认者。

日志和模型上下文中不得出现 `argus__token`、私有业务参数、完整 Secret、未裁剪大结果或任意 SQL。

### 10.3 撤权与失效

- DataScope 或 AuthorizationVersion 变化后，缓存立即失效。
- Dashboard 仍存在，但不可见资源从有效绑定集合移除。
- Kubernetes UID 漂移使 Binding 进入 `stale`，不得自动绑定同名新对象。
- Revision 查询校验失败、依赖字段删除或 Query Engine 升级不兼容时，阻止发布新 Revision；已有 active Revision 继续按兼容策略运行并报告警告。

## 11. 前端交付

新增 Enterprise 页面：

```text
/dashboards                 DashboardFolder 和 Dashboard 列表
/dashboards/$dashboardId   Dashboard 查看页
/dashboards/$dashboardId/edit
```

查看页包含：

- `/dashboards` 首屏是 Folder + Dashboard 目录，支持组内创建 Dashboard、根级“未分组”区域和搜索。
- Dashboard 名称、描述、Folder 和绑定目标摘要。
- 时间范围、刷新间隔、变量下拉框和手动刷新按钮。
- 响应式 Grid Panel 布局。
- 空 Dashboard 显示逐个添加统计图的空态；每个 Panel 支持独立扩大/缩小和布局保存。
- 右上角“指标查询”打开 OTLP Catalog：Metrics 显示指标/Label/值，Logs 显示字段/值，Traces 显示属性/范围。
- Metrics、Logs、Traces 统一加载状态、空态、错误态和 partial 状态。
- Panel 查询元数据、数据新鲜度和“打开资源详情/Trace”链接。

仪表盘详情页和统计图编辑页分离：

- 新建仪表盘只提交 `name`、`description`、`folder_id`，创建成功后直接进入空详情页。
- 详情页的“添加统计图”才进入 Panel 配置；统计图编辑页是单列表单，只展示当前统计图的标题、说明、Signal、展示类型、查询和预览。
- 统计图编辑页不重复展示 Dashboard 名称、描述和分组，也不承载 Panels 列表、变量管理、资源绑定或复制仪表盘，避免把 Dashboard 级配置误认为 Panel 配置。
- 详情页顶部单独管理时间范围、刷新频率、变量过滤器、资源绑定和 Panel 布局；点击已有统计图主体可以进入该统计图的编辑页。

统计图编辑页包含：

- 当前统计图的标题、说明、Signal、展示类型和查询 Builder/DSL 编辑。
- 查询校验、样本运行、Preview Diff、查询预算和确认操作。

仪表盘变量管理包含：

- 详情页“变量”按钮打开变量管理弹框，支持逐个新增和删除变量，并在保存后生成顶部下拉过滤器。
- 变量属于 Dashboard 级对象；Panel 只能引用已经声明的变量，不能在 Panel 编辑页隐式创建变量。
- 变量配置弹框采用“通用配置 + 信号目录”：通用区域只填写 Key、显示名称、单选/多选/All、刷新时机和默认值。
- 下方按 `Metrics`、`Logs`、`Traces` 分 Tab，先选择 OTLP Collector 插件，再选择指标/日志字段/Trace 属性，展开后选择 Label、字段或属性和值；不要求用户手工填写跨信号映射和查询 DSL。
- 多选、All 是变量选择策略；当前选择值属于运行时上下文，不直接修改 active Revision。
- 配置区展示顶部过滤器预览和 Panel 引用关系；引用关系由 Panel 查询中的 `$variable` 解析得到，不维护与查询语句并行的执行绑定清单。
- 底层仍保存 Signal、Collector、Attribute 和选项来源等类型化信息，界面通过目录选择生成这些字段，保存前校验变量已选择有效的过滤字段。
- 弹框采用视口内宽屏布局，正文使用唯一纵向滚动区域；头部和底部操作区固定，不能因变量数量、查询预览或目录结果增长而被滚出视口。

必须复用 `@argus/ui`、现有 Telemetry 图表组件、Design Tokens、`.argus-*` 类名和模块 i18n；页面文件保持低于 2000 行并按 `dashboard/` 组件拆分。

## 12. 两个大实现 Task 与子任务清单

为了避免把后台工作台、查询运行时和 Agent 编排揉成一个不可验收的大项目，PlanV2 归并为两个顶层实现 Task。两个 Task 共享同一套 Dashboard 领域服务、Revision、Query Runtime、权限和审计，不允许分别实现两套保存或查询逻辑。

两个详细 Task 文件是实施时的主要执行入口：

- [Task 01：Dashboard Workbench、Query Runtime 与资源入口](./task-01-dashboard-workbench-and-runtime.md)
- [Task 02：AI 创建仪表盘与 @Dashboard 分析](./task-02-ai-dashboard-create-and-analysis.md)

### Task 1：Dashboard Workbench、Query Runtime 与资源入口

**目标**：交付可以真实创建、编辑、发布、查看、刷新和绑定资源的 Metrics/Logs/Traces 仪表盘。这个 Task 完成后，即使没有 AI，管理员也能在后台独立使用完整仪表盘。

**包含的实现范围**：

1. **契约和领域模型**：固化 `DashboardFolder`、`Dashboard`、`DashboardRevision`、`Panel`、`QueryTarget`、`DashboardVariable`、`DashboardBinding` 的 JSON Schema、状态机、错误码、OpenAPI 和审计字段；明确 active Revision 不可变、绑定不属于授权边界、查询文本不能由浏览器覆盖。
2. **存储和领域服务**：新增 PostgreSQL Migration、Repository、领域服务和索引；实现 Folder/Dashboard/Revision/Panel/Variable/Binding 的创建、更新、归档、发布；所有写操作接入 Preview/Commit、PendingAction、幂等和审计。
3. **查询运行时**：实现统一 `ExecuteDashboard`，从 active Revision 恢复查询，解析时间范围、变量依赖、Target Context 和 `scope_mode`，调用 M10 PromQL/KQL/SkyWalking Engine；提供总预算、并发、超时、取消、缓存、partial、单 Panel 错误和结果投影。
4. **OTLP Catalog**：提供 Metrics 指标/Label/值、Logs 字段/值、Traces 属性/范围的受控目录查询；所有目录结果经过权限、DataScope、敏感字段和数量限制，不能直接拼接成已发布查询。
5. **后台工作台**：实现分组 + 仪表盘列表、根级未分组、名称/描述创建、仪表盘详情页、逐个 Panel 创建和编辑、Panel 网格布局、放大缩小、Panel 查询校验、样本运行、时间范围、自动刷新、变量下拉框和唯一滚动区域的变量弹框。
6. **资源绑定和入口**：实现 Dashboard 绑定 Host、Kubernetes Cluster/Namespace/Workload/Service；资源详情页显示有效关联 Dashboard；打开时携带 `current_target`；处理 DataScope、AuthorizationVersion、Kubernetes UID 漂移、资源删除、stale Binding 和撤权。
7. **安全、审计和测试**：补齐权限矩阵、字段脱敏、查询来源、Revision、Panel、Target、变量摘要、预算和结果状态审计；完成 API/领域/Query Runtime/Playwright 测试。

**Task 1 的交付顺序**：

```text
契约与 Migration
→ Dashboard/Revision/Preview/Commit
→ ExecuteDashboard 与 Catalog
→ 工作台列表、详情和 Panel 编辑
→ 变量和布局
→ Host/Kubernetes Binding 与资源入口
→ UI/接口/E2E 验收
```

**Task 1 退出标准**：

- 用户可以从列表创建 Dashboard，填写名称、描述和分组，并发布 active Revision。
- 用户可以逐个添加 Metrics、Logs、Traces Panel，修改查询和布局，并通过统一运行时查看结果。
- 变量可以按查询引用自动形成依赖链，Panel 只在显式引用变量时生效。
- Dashboard 从 Host/Kubernetes 详情页打开时，查询范围正确带入当前资源上下文。
- UI、Card Host 和内部 Inspect 调用获得相同的权限裁剪、预算和结果语义。
- 任何未验证查询、越权 Target、stale Binding、重复 Commit 或 partial 结果都有明确错误/状态，不能伪装成空数据或正常。

### Task 2：AI 创建仪表盘与 `@Dashboard` 分析工作台

**目标**：在 Task 1 的稳定 Dashboard、Catalog 和 Query Runtime 之上，提供用户显式创建和分析仪表盘的会话能力。AI 只负责理解、规划和调用工具，不能成为新的 Dashboard 存储或查询执行实现。

**包含的实现范围**：

1. **`/创建仪表盘` Skill**：注册显式创建命令，收集名称、描述、Folder、Panel、变量、默认时间、刷新和可选绑定；通过 Catalog 确认真实指标、日志字段、Trace 服务和资源。
2. **结构化 Draft 工具**：定义 `DashboardDraft` 的严格 Input Schema（`additionalProperties=false`），直接接收 JSON；服务端校验 Schema、查询语言、Panel 类型、变量依赖、布局、预算、绑定和资源权限。
3. **Preview/Commit 集成**：UI 创建和 AI 创建共用 `dashboard.create.preview`、`dashboard.update.preview` 和领域服务；Preview 返回查询校验、样本数据、Diff、风险、Spec Hash 和公开 Action Ref；用户点击确认后由 Action Executor Commit，Skill 和模型不能直接提交。
4. **Dashboard `@` Mention**：沿用 Chatbox 资源引用方式，输入 `@` 后通过授权 resolver 展示名称/描述/标签候选，消息中保存稳定 Dashboard ID；没有 `@` 时不得根据自然语言模糊猜测仪表盘。
5. **分析上下文和查询规划**：实现 `telemetry.dashboard.get`，向 Agent 提供 active Revision 的 Panel 查询定义、变量约束、支持的 Query Tool、Target 范围和预算；Agent 可以选择 Panel、决定查询顺序和并行策略，但不能修改查询文本。
6. **Dashboard provenance 和 Inspect**：实现 `telemetry.dashboard.inspect` 及带 provenance 的 Metrics/Logs/Traces Query Tool；服务端复核 `dashboard_ref + revision_ref + panel_id + query_hash + signal`，重新执行权限、DataScope、绑定和预算校验。
7. **Evidence Projection 和总结**：将结果投影为统计摘要、Top Pattern、日志样本、慢 Trace、阈值观察、partial、warning 和 `evidence_ref`；模型输出必须区分 `observed`、`inferred`、`unknown`，并提供返回 Dashboard/Panel/资源详情的入口。
8. **会话/Card 工作台和测试**：接入 Chatbox mention、Skill、Tool Result Projection、Preview Card 和确认 Card；覆盖 AI 生成、用户确认、失败恢复、越权拒绝、查询哈希不匹配、Revision 失效、数据不足和三类信号混排 E2E。

**Task 2 的交付顺序**：

```text
DashboardDraft Schema 与 Skill
→ Catalog 辅助生成和 Preview
→ 用户确认 Commit
→ @Dashboard resolver 与稳定引用
→ dashboard.get/inspect 与 provenance 门禁
→ Agent 选择 Panel 和查询顺序
→ Evidence Projection 与总结
→ Chatbox/Card/E2E 验收
```

**Task 2 退出标准**：

- 用户通过 `/创建仪表盘` 可以生成结构化 Draft，查看 Preview 后点击确认创建真实 Dashboard。
- 用户通过 `@` 明确引用 Dashboard 后，Agent 能读取 active Revision，自行选择 Panel 查询并总结 Metrics/Logs/Traces 证据。
- Agent 不能调用未授权 Dashboard、修改已发布查询、绕过 Binding/DataScope 或把任意查询伪装成 Dashboard 证据。
- 查询结果、模型总结、Preview 和 Commit 都能回溯到 Dashboard、Revision、Panel、query_hash、Target 和时间范围。
- AI 创建和 AI 分析在模型重试、Worker 重启、Redis 清空、权限变化和部分查询失败时保持可恢复、可审计、可解释。

### 两个 Task 的依赖和发布关系

| 交付批次 | 内容 | 依赖 | 用户可见结果 |
| --- | --- | --- | --- |
| Batch A | Task 1 的契约、领域服务、Runtime、Catalog | M2/M7/M10 现有能力 | 后端可创建并执行 Dashboard |
| Batch B | Task 1 的后台工作台和资源入口 | Batch A | 用户可以手工创建、查看、刷新和绑定仪表盘 |
| Batch C | Task 2 的 AI 创建 | Batch A，Catalog 和 Preview/Commit 可用 | 用户可以 `/创建仪表盘` 并确认发布 |
| Batch D | Task 2 的 `@Dashboard` 分析 | Batch A、B，Inspect 和 Evidence Projection 可用 | 用户可以 `@仪表盘` 让 Agent 检测和总结内容 |

Task 1 的 Batch A/B 是第一版人工仪表盘基线；Task 2 的 Batch C/D 在此基线上叠加 AI 能力。两者不能分别复制 Dashboard 保存、权限或查询执行逻辑。

下面的 `P2V-0 ~ P2V-5` 是上述两个 Task 的细粒度检查清单：`P2V-0 ~ P2V-3` 主要归属 Task 1，`P2V-4` 主要归属 Task 2，`P2V-5` 为两个 Task 共同的发布门禁。

### P2V-0：契约与架构冻结

- [ ] `P2V-CONTRACT-01` 固化 Dashboard、Folder、Revision、Panel、Target、Variable、Binding JSON Schema。
- [ ] `P2V-ADR-01` 新增 Dashboard 领域 ADR，确认不复用 InteractiveCard、不新增 Query Engine、不把 Binding 当授权边界。
- [ ] `P2V-OPENAPI-01` 增加 REST 路径、错误码、分页、执行结果和 Preview 公共投影。
- [ ] `P2V-TOOL-01` 冻结 MCP Tool Catalog、权限、Input/Output Schema 和 Projection Schema。

### P2V-1：存储与领域服务

- [ ] `P2V-DB-01` Goose Migration、sqlc 查询、唯一约束和企业级索引。
- [ ] `P2V-DOMAIN-01` Dashboard/Folder/Revision 生命周期、Spec Hash 和 active Revision 原子发布。
- [ ] `P2V-VALIDATE-01` 查询语言、变量、Panel 类型、布局、预算和绑定目标验证。
- [ ] `P2V-ACTION-01` 创建、更新、归档和绑定的 Preview/Commit 接入通用 PendingAction/Execution。
- [ ] `P2V-AUDIT-01` 补齐创建、发布、查询、绑定和撤权审计事件。

### P2V-2：Dashboard Query Runtime

- [ ] `P2V-EXEC-01` 实现 `ExecuteDashboard`，统一处理时间、变量、Target、DataScope 和 Panel 结果。
- [ ] `P2V-EXEC-02` 接入 M10 PromQL/KQL/SkyWalking Engine，保持三种原生结果语义。
- [ ] `P2V-EXEC-03` 实现 Dashboard 总预算、并发、缓存、取消、partial 和单 Panel 错误投影。
- [ ] `P2V-CATALOG-01` 实现指标名、Label/字段、服务和有限值的受控 Catalog 查询。
- [ ] `P2V-EXEC-04` 为 UI、Card Host、MCP Inspect 复用同一执行服务和安全投影。

### P2V-3：后台页面与资源入口

- [ ] `P2V-WEB-01` Dashboard/Folder 列表、查看、编辑和 Preview 页面。
- [ ] `P2V-WEB-02` 时间范围、刷新、类型化变量、布局、加载/错误/partial/无数据状态。
- [ ] `P2V-BINDING-01` Host、Kubernetes Cluster/Namespace/Workload/Service 直接绑定和撤销。
- [ ] `P2V-BINDING-02` 在 Host/Kubernetes 详情页展示关联 Dashboard，并通过 `current_target` 打开。
- [ ] `P2V-BINDING-03` Kubernetes UID 漂移、资源删除、撤权和 `descendants` 传播处理。
- [ ] `P2V-WEB-03` zh-CN/en-US、light/dark、键盘和读屏验证。

### P2V-4：AI Skill 与自然语言创建

- [ ] `P2V-AI-01` 注册 `telemetry_dashboard_analysis` Skill、Dashboard Get/Inspect Tool 和显式 `@` mention resolver。
- [ ] `P2V-AI-02` 实现 Dashboard `@` 候选列表、稳定引用 ID、名称/描述过滤和未引用时的明确提示。
- [ ] `P2V-AI-03` 实现 Evidence Projection、事实/推断/未知分类和 `evidence_ref` 回溯。
- [ ] `P2V-AI-04` 实现 `/创建仪表盘` Skill 和自然语言 Dashboard Draft JSON 生成，优先调用 Catalog 确认真实查询对象。
- [ ] `P2V-AI-05` 将 AI Create/Update 接入 Preview Card、用户点击确认和隐藏 Commit。
- [ ] `P2V-AI-06` 支持创建时的绑定建议；绑定目标必须在 Preview 中逐项展示并可取消。
- [ ] `P2V-AI-07` 为 PromQL/KQL/SkyWalking Query Tool 增加 Dashboard provenance、Panel query_hash 和 Revision 复核门禁。

### P2V-5：端到端验收与发布门禁

- [ ] `P2V-E2E-01` UI 创建 Dashboard：Panel、变量、绑定、Preview、Confirm、刷新和审计。
- [ ] `P2V-E2E-02` AI 创建 Dashboard：自然语言、Catalog、Preview Card、Commit、Revision 和资源入口。
- [ ] `P2V-E2E-03` 使用 `@` 引用 Dashboard 后执行 Inspect，覆盖 Agent 选择 Panel、调用三类 Query Tool、混排和总结证据。
- [ ] `P2V-E2E-04` 从 Host、Cluster、Namespace、Workload、Service 详情页打开并验证 Target 上下文。
- [ ] `P2V-E2E-05` 覆盖跨企业、DataScope、AuthorizationVersion、敏感字段、Kubernetes UID 漂移和 stale Binding。
- [ ] `P2V-E2E-06` 覆盖 Redis 清空、Query/Server 重启、缓存失效、重复 Commit、partial Panel 和失败清理。
- [ ] `P2V-RELEASE-01` 将临时 Namespace、PVC、Topic、Bucket、Lease 和诊断脱敏清理纳入官方 Harness。

## 13. 关键用户流程

### 13.1 后台创建并绑定

```text
管理员打开 Dashboard 编辑页
→ 填写名称/描述/Folder
→ 添加 Metrics、Logs、Traces Panel
→ 配置类型化变量、默认时间和刷新间隔
→ 选择 Host/Kubernetes 绑定目标
→ 点击准备
→ 服务端验证查询、权限、预算和绑定
→ 展示 Preview/Diff/样本数据
→ 用户确认
→ Action Executor Commit
→ 发布 active Revision
→ 资源详情页显示“关联仪表盘”入口
```

### 13.2 AI 创建

```text
用户：创建一个生产支付服务健康度仪表盘，包含错误率、P95、错误日志和慢 Trace
→ Skill 识别意图
→ Catalog 查询可用指标/字段/服务
→ 生成结构化 DashboardDraft
→ dashboard.create.preview
→ 用户查看 Panel、变量、绑定和查询样本
→ 用户点击确认
→ Action Executor Commit
→ 返回 Dashboard 入口
```

### 13.3 AI 查询和总结

```text
用户：@支付服务健康度 最近一小时有没有问题？
→ 客户端解析稳定 Dashboard mention
→ dashboard.get(active_revision, panel query definitions)
→ Agent 选择需要的 Panel 和查询顺序
→ telemetry.promql.query / telemetry.kql.query / telemetry.skywalking.trace（带 provenance）
→ dashboard.inspect 或等价组合执行结果
→ Dashboard Query Runtime 执行已保存查询
→ Evidence Projection 返回统计、Top Pattern、慢 Trace、partial 和引用
→ 模型输出 observed/inferred/unknown 分层总结
→ 提供打开 Dashboard 和具体 Panel/资源的入口
```

### 13.4 从资源详情快速进入

```text
用户打开 Host 或 Kubernetes Workload 详情
→ 服务端查询有效 DashboardBinding
→ 用户点击 Dashboard
→ 路由携带 target context
→ Dashboard 以 current_target 执行 Panel
→ 用户可切换 all_bound_targets 或 viewer_scope
```

## 14. 测试和完成定义

除 `docs/plans/README.md` 中的通用完成定义外，PlanV2 必须满足：

- 查询语句永远来自已验证 active Revision，浏览器和模型不能覆盖表达式。
- Preview/Commit 的私有 Token、完整草稿参数和未裁剪结果不出现在模型、Card、浏览器 DOM、网络日志或审计正文中。
- 同一 Dashboard 从 UI、Card 和 AI Inspect 获得一致的授权裁剪和查询语义。
- 绑定只缩小有效资源范围，不扩大 DataScope；撤权后旧缓存、链接和查询立即失效或返回明确错误。
- Kubernetes 对象使用 UID，UID 漂移不自动跟随同名对象。
- Panel 错误、查询超时、partial 和无数据可区分；AI 不得把无证据状态总结为正常。
- 资源详情页的 Dashboard 入口只展示当前用户有权访问的 active Dashboard。
- 所有页面通过 `zh-CN/en-US × light/dark`、键盘、读屏和移动/桌面布局验证。
- 完整流程在临时 Kubernetes Namespace 执行，成功和失败都删除 Namespace、PVC、Topic、Bucket、Lease 和测试绑定。

## 15. 架构影响说明

本计划不改变现有服务进程边界、租户边界、Telemetry Query Engine、Card Runtime 或 Preview/Commit 安全协议。新增的是 `Dashboard` 领域模块和一组对现有服务的组合调用。

实现开始前需要新增 Dashboard ADR，并在代码合入同时更新：

- `docs/00-decisions-and-invariants.md`：补充 Dashboard Revision、绑定非授权边界和已发布查询才能被 AI 执行的不变量。
- `docs/09-opentelemetry-observability.md`：将 Dashboard/AI 分析从“后续大屏”更新为 PlanV2 范围，并列出新的 Query/Binding Tool。
- `docs/04-agent-mcp-and-action-workflow.md`：补充 Dashboard Preview/Commit 和 Inspect Tool 的暴露边界。
- `docs/05-interactive-cards-and-interactive-ui.md`：说明 Card 仅承载 Dashboard Preview/分析摘要，不承载 Dashboard 本体。
