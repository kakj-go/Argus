# Task 02：AI 创建仪表盘与 @Dashboard 分析

## 1. Task 定义

目标：在 Task 01 提供的 Dashboard 领域服务、OTLP Catalog 和 Query Runtime 之上，接入两个显式 AI 能力：用户通过 /创建仪表盘 生成结构化 Dashboard Draft 并确认发布；用户通过 @ 明确引用仪表盘，让 Agent 根据已发布 Panel 查询获取指标、日志和 Trace 证据并总结是否存在问题。

完成后的用户闭环：

~~~text
用户输入 /创建仪表盘
→ Skill 收集需求并调用 Catalog
→ 生成 DashboardDraft JSON
→ dashboard.create.preview
→ 用户查看 Preview 并点击确认
→ Action Executor Commit
→ 返回 Dashboard 入口

用户输入 @支付服务健康度 最近一小时有没有问题
→ 客户端生成稳定 Dashboard Mention
→ dashboard.get 获取 active Revision
→ Agent 选择 Panel 和查询顺序
→ 通过 provenance 校验的 Query Tool 查询
→ Evidence Projection
→ 输出 observed/inferred/unknown 总结
~~~

AI 只负责理解、规划和调用工具，不能成为新的 Dashboard 存储、权限或查询执行实现。

## 2. 依赖与非目标

### 2.1 必须先完成 Task 01

- Dashboard、Revision、Panel、Variable、Binding Schema 已冻结。
- ExecuteDashboard 能从 active Revision 执行 Metrics/Logs/Traces。
- Catalog、Preview/Commit、权限、explicit resource authorization、AuthorizationVersion 已可调用。
- 至少有一个真实后台页面和 API E2E 证明手工创建闭环。
- Tool Result Projection、PendingAction、Action Executor 和 Chatbox Mention 基座可复用。

### 2.2 本 Task 不实现

- 没有 @ 时从全企业 Dashboard 中模糊猜测仪表盘。
- 模型直接执行任意 PromQL、KQL、SQL 或 Trace GraphQL。
- 模型直接提交 Commit 或读取私有 PendingAction 参数。
- 把 AI 摘要写回 Dashboard 作为事实。
- 自动创建告警、自动修复或自动修改生产资源。

## 3. 子任务分解

### T2.1 /创建仪表盘 Skill

注册显式命令，内部命令 ID 固定为 telemetry.dashboard.create。自然语言只用于收集需求，不作为服务端协议。

Skill 需要收集：

- Dashboard 名称、描述和 Folder。
- Panel 标题、Signal、展示类型、查询意图和布局。
- Metrics/Logs/Traces 变量、默认值、依赖和刷新策略。
- 默认时间范围、自动刷新和可选资源绑定。
- 用户指定的服务、主机、Kubernetes 对象和环境范围。

Skill 生成 JSON Draft，不生成前端 HTML、Markdown 配置或直接可执行查询。

### T2.2 Catalog 辅助生成

模型生成 Draft 前优先调用受授权 Catalog：

- Metrics：确认指标名、Label 和有限值。
- Logs：确认字段、字段值、严重级别和受控检索语义。
- Traces：确认服务、Span 属性、状态和时延范围。
- Resources：确认 Host/Kubernetes 资源 ID、UID 和用户可用范围。

Catalog 的候选不唯一时，Skill 必须返回澄清项，不允许模型自行选择相似名称。Catalog 结果只能作为 Draft 的来源证据，最终仍由服务端 Query Parser 和 explicit resource authorization 重新校验。

### T2.3 DashboardDraft JSON 接收与校验工具

定义版本化 DashboardDraft：

~~~json
{
  "schema_version": "argus.telemetry_dashboard/v1",
  "name": "支付服务健康度",
  "description": "支付服务核心链路的统一观察面",
  "folder_id": "folder_01",
  "spec": {
    "default_time_range": {"kind": "relative", "seconds": 3600},
    "default_refresh_seconds": 0,
    "variables": [],
    "panels": [],
    "layout": {"columns": 12, "row_height": 8}
  },
  "proposed_bindings": []
}
~~~

Tool Input Schema 必须设置 additionalProperties=false，并拒绝 Skill 未声明的字段。

服务端校验：

1. 名称、描述、Folder 和企业归属。
2. Schema Version、Panel 数量、布局和展示类型。
3. 每个 Signal、Query Language、expression、pipeline、operation_name 和 variables_schema。
4. 变量 Key、查询依赖、未定义引用、循环依赖、默认值和最大选项数。
5. 查询预算、时间范围、刷新频率和资源绑定范围。
6. 资源 ID、Kubernetes UID、explicit resource authorization 和 AuthorizationVersion。
7. Catalog provenance、Parser/Validator 结果和样本返回类型。

校验失败只能返回结构化错误和修复建议，不能创建 active Dashboard。

### T2.4 Preview/Commit 与确认工作台

UI 创建和 AI 创建必须共用：

~~~text
telemetry.dashboard.create.preview
telemetry.dashboard.update.preview
telemetry.dashboard.create.commit
telemetry.dashboard.update.commit
~~~

Preview 返回：

- Dashboard 和 Folder 摘要。
- Panel 列表、查询语言、查询校验和样本结果。
- 变量依赖图、默认值、绑定目标和预计查询范围。
- Spec Hash、Diff、风险、预算、过期时间和公开 Action Ref。
- 部分 Panel 失败、Catalog 不确定和需要用户澄清的事项。

用户在 Preview Card 或后台确认页点击确认；Action Executor 根据 Action Ref 读取服务端私有计划并 Commit。模型、Skill、浏览器和 Card 都不能携带可变业务参数调用 Commit。

重复点击、网络超时和 Worker 重启必须通过 Execution ID、幂等键和 ResultUnknown 对账恢复，不能重复创建 Revision 或 Binding。

### T2.5 Dashboard @ Mention Resolver

分析必须使用显式 @ 引用：

~~~text
用户输入：@支付服务健康度 最近一小时有没有问题

结构化消息：
mentions: [{kind: "dashboard", id: "db_01", label: "支付服务健康度"}]
text: "最近一小时有没有问题"
~~~

要求：

- 用户输入 @ 后展示当前企业、当前用户有权访问的 Dashboard 候选。
- 名称、描述、别名和标签只用于候选过滤，不构成未引用时的自动选择依据。
- 消息事实使用稳定 Dashboard ID，label 只用于显示。
- Dashboard 被归档、撤权或不可见时，Mention 解析必须返回明确错误。
- 没有 @ 时，Skill 应提示用户先引用 Dashboard，而不是猜测目标。

### T2.6 dashboard.get 与 Agent 查询规划

实现只读 Tool：telemetry.dashboard.get。

返回模型安全的 DashboardAnalysisContext：

~~~text
dashboard_ref / revision_ref
dashboard_name / description
target_context / allowed_resource_ids
panels[]
  panel_id / title / signal / visualization
  language / expression / pipeline / operation_name
  query_hash
  required_variables / allowed_variables
  scope_mode / budget
context_expiry
~~~

Agent 可以根据问题选择 Panel、决定先查错误率还是延迟、是否并行查询、发现异常后是否补查 Logs/Traces，但不能修改 expression、pipeline、Panel 类型或变量定义。

### T2.7 provenance、Inspect 与 Query Tool 门禁

分析模式下的 Metrics/Logs/Traces Query Tool 必须携带：

~~~json
{
  "dashboard_ref": "db_01",
  "revision_ref": "rev_07",
  "panel_id": "error-rate",
  "query_hash": "sha256:...",
  "target_context": {"type": "host", "id": "host_01"},
  "from": "...",
  "to": "..."
}
~~~

服务端重新读取 Revision，复核 dashboard_ref、revision_ref、panel_id、query_hash、language、signal 和 expression，并重新执行：

~~~text
DashboardBinding/Target Context
∩ 当前用户 explicit resource authorization
∩ 当前 AuthorizationVersion
∩ Query Tool Signal 与预算
~~~

以下情况必须拒绝：查询表达式被修改、Panel 不属于 Revision、Revision 失效、Target 不在绑定范围、Query Tool 与 Signal 不匹配、变量值不合法或预算超限。

telemetry.dashboard.inspect 可以作为组合 Tool；也可以由 Agent 分解为带 provenance 的 PromQL、KQL 和 SkyWalking Query Tool。两条路径必须复用 Task 01 的 ExecuteDashboard、权限、预算和结果投影。

### T2.8 Evidence Projection 与模型总结

Inspect 返回模型安全的 DashboardEvidenceProjection：

~~~text
dashboard_id / revision_id / dashboard_name
target_context / effective_resource_ids
time_range / data_freshness
panels[]
  panel_id / title / signal
  summary_stats
  top_series / top_log_patterns / slow_traces
  threshold_observations
  query_meta / warnings / partial
  evidence_ref
projection_hash
~~~

模型总结必须区分：

- observed：查询直接观察到的事实。
- inferred：基于多个事实得出的推断。
- unknown：无数据、partial、权限裁剪或查询失败导致无法判断。

没有证据的 Panel 不能被总结为正常。结果只提供有限样本和摘要，大结果放在 Tool Result/Artifact Store，并保留可回溯 evidence_ref。

### T2.9 Chatbox、Card 与分析工作台

- Chatbox 输入框支持 Dashboard @ 候选和稳定 Mention Chip。
- /创建仪表盘 展示 Draft Preview Card，用户可以查看 Panel、变量、绑定和风险。
- 用户确认由 Card Action Binding 触发，不再经过模型二次推理。
- @Dashboard 分析结果展示结论、证据、时间范围、Revision、Panel 和资源入口。
- Card 只承载 Preview、确认和分析摘要，不承载 Dashboard 本体，也不能访问宿主 DOM、Cookie 或私有 Token。

## 4. 工具与权限清单

只读或分析工具：

~~~text
telemetry.dashboard.catalog.metric_names
telemetry.dashboard.catalog.attribute_values
telemetry.dashboard.catalog.log_fields
telemetry.dashboard.catalog.trace_services
telemetry.dashboard.get
telemetry.dashboard.inspect
~~~

创建和更新 Preview 工具：

~~~text
telemetry.dashboard.create.preview
telemetry.dashboard.update.preview
~~~

Commit 工具只能存在隐藏 Action Catalog，不能出现在 Model Agent Tool Registry 中：

~~~text
telemetry.dashboard.create.commit
telemetry.dashboard.update.commit
~~~

权限至少包括 telemetry.dashboard.read、create、update、inspect、catalog.read；创建、更新、绑定和分析每次重新校验企业、可见性、Signal 权限、explicit resource authorization、字段脱敏权限和 AuthorizationVersion。

## 5. 测试与退出门禁

### 5.1 Skill 和 Tool 测试

- Draft Schema 拒绝未知字段、非法 Signal、非法 Panel、循环变量和越权 Binding。
- Catalog 候选不唯一时返回澄清项，不自动猜测。
- Preview 失败不能产生 active Dashboard。
- Commit 只接受 Action Ref，重复 Commit 幂等。
- provenance 缺失、query_hash 不匹配、Revision 失效和 Target 越权全部拒绝。

### 5.2 Chatbox/Card 测试

- @ 候选只返回当前用户可见 Dashboard。
- 没有 @ 时不会模糊选择 Dashboard。
- Draft Preview Card 的确认动作不暴露私有参数。
- 伪造 Mention ID、Action Binding ID、Origin 或 Tool Result 来源被拒绝。

### 5.3 E2E 测试

- 自然语言创建 Metrics/Logs/Traces 混合 Dashboard，Preview、确认、Commit 和打开详情页。
- @Dashboard 后 Agent 先查询 Metrics，再根据异常补查 Logs/Traces，并输出证据引用。
- 覆盖模型重试、Worker 重启、Redis 清空、查询 partial、权限变化和 Dashboard 归档。
- 临时 Kubernetes Namespace 测试成功和失败都清理 Namespace、PVC、Topic、Bucket、Lease、Action 和诊断文件。

## 6. Task 退出标准

- 用户可以通过 /创建仪表盘 生成结构化 Draft，查看 Preview 后点击确认创建真实 Dashboard。
- 用户可以通过 @ 明确引用 Dashboard，Agent 能读取 active Revision，自行选择 Panel 查询并总结三类信号证据。
- Agent 不能调用未授权 Dashboard、修改已发布查询、绕过 Binding/explicit resource authorization 或把任意查询伪装成 Dashboard 证据。
- 查询结果、模型总结、Preview 和 Commit 都能回溯到 Dashboard、Revision、Panel、query_hash、Target 和时间范围。
- AI 创建和 AI 分析在模型重试、Worker 重启、Redis 清空、权限变化和部分查询失败时保持可恢复、可审计、可解释。

## 7. 主计划映射

对应主计划：P2V-4，以及 P2V-5 中的 AI 创建、@Dashboard、Inspect、Evidence Projection、Chatbox/Card 和安全发布测试。

