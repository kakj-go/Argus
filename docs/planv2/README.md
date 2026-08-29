# PlanV2：遥测仪表盘与 AI 分析

本目录定义 Argus 下一阶段的自定义遥测仪表盘能力，覆盖：

- 用户在后台创建、编辑和查看 Metrics/Logs/Traces 混合仪表盘。
- `/dashboards` 首屏以 Folder + Dashboard 目录组织内容，支持组内创建和根级未分组 Dashboard；创建时先填写名称/描述，进入详情后逐个添加统计图并调整布局。
- Dashboard 详情页提供 OTLP Catalog：顶部按 Metrics/Logs/Traces 分 Tab，Tab 下先选择 Collector 插件，再在指标/字段行下展开 Label、属性和可用值。
- 用户用自然语言让 AI 生成仪表盘，并通过既有 Preview/Commit 流程确认发布。
- 用户在会话中通过 `@` 明确引用仪表盘；AI 读取该仪表盘的已发布查询配置，执行受授权的查询并总结指标、日志和 Trace 证据。
- 仪表盘绑定 Host、Kubernetes Cluster、Namespace、Workload、Service 等资源，从资源详情页快速打开并自动带入查询上下文。
- 实施归并为两个大 Task：Task 1 负责 Dashboard Workbench、Query Runtime、Catalog 和资源入口；Task 2 负责 `/创建仪表盘`、`@Dashboard` 分析、provenance 和 Evidence Projection。

## 文档

| 文件 | 内容 |
| --- | --- |
| [01-telemetry-dashboard-and-ai.md](./01-telemetry-dashboard-and-ai.md) | 产品边界、领域模型、查询执行、AI Skill、资源绑定、API、权限、前端和 E2E 计划 |
| [task-01-dashboard-workbench-and-runtime.md](./task-01-dashboard-workbench-and-runtime.md) | Task 01：Dashboard Workbench、Query Runtime、OTLP Catalog、资源绑定和人工仪表盘闭环 |
| [task-02-ai-dashboard-create-and-analysis.md](./task-02-ai-dashboard-create-and-analysis.md) | Task 02：/创建仪表盘、Dashboard Draft、@Dashboard 分析、provenance 和 Evidence Projection |

## 状态

当前为设计和实施计划，尚未改变现有 M0-M10 的已完成状态。实现时新增一个 Dashboard 领域模块，复用 M4 的 Agent/Tool/Preview/Commit、M5 的 Card Runtime、M7/M10 的 Telemetry Query，不新增第二套遥测存储或查询语言。

## 关键决策摘要

1. `Dashboard` 是长期持久化业务对象，`InteractiveCard` 只用于会话中的预览、分析摘要和确认交互。
2. 一个 Dashboard 可以混排三种信号，但每个 Panel 保留自己的 PromQL、KQL 或 SkyWalking GraphQL 语义。
3. Dashboard 保存不可变 `DashboardRevision`；只有已验证的 Revision 才能被前端执行或被 AI Skill 使用。
4. 资源绑定是导航和查询上下文，不是授权边界；每次打开、刷新、AI 查询和绑定变更都重新执行 explicit resource authorization 与 AuthorizationVersion 校验。
5. 所有持久化变更均使用现有 `.preview/.commit` 协议。产品文案可以称“准备/确认”，不另造一套 `prepare` 协议。
6. 分析入口必须是显式 Dashboard `@` 引用；创建入口必须是显式 `/` Skill，不做用户语言到 Dashboard 的模糊自动匹配。
7. AI 可以从 active Revision 读取 Panel 查询定义并决定查询顺序，但指标 Query Tool 必须校验 Dashboard/Revision/Panel provenance 和查询哈希。
8. 变量设计采用查询变量模型：每个变量独立配置 Metrics/Logs/Traces 查询，后续变量通过查询中的 `$variable` 自动建立依赖，Panel 是否使用变量只看查询是否显式引用；执行语义参考 Grafana，跨信号和值发现参考 SigNoz，配置体验参考 OpenObserve。
9. 变量编辑弹框采用宽屏自适应布局，头部和底部操作区固定，只允许正文区域纵向滚动；变量链、查询编辑器和目录结果不得各自形成滚动容器。
10. 实施顺序先完成 Task 1 的人工仪表盘基线，再在同一领域服务上接入 Task 2 的 AI 创建和 `@Dashboard` 分析；AI 不拥有独立的存储、权限或查询执行路径。
