# Argus 设计文档

Argus 是一个面向 AIOps 场景的多租户 SaaS 控制平面。产品以 Chatbox 作为主要入口，以管理后台作为确定性配置入口，以 MCP Tool 作为业务能力边界，并通过 Connector/Agent 连接主机、Kubernetes 集群及其他受管环境。

本文档集记录当前产品讨论形成的基线设计。它不是最终接口定义；涉及协议字段的示例用于表达约束和职责，实施时应再固化为版本化 JSON Schema、OpenAPI 或 protobuf。

## 阅读顺序

1. [已决策事项与系统不变量](./00-decisions-and-invariants.md)
2. [产品定位与总体架构](./01-product-and-architecture.md)
3. [多租户、RBAC 与数据权限](./02-identity-authorization-and-data-permission.md)
4. [Connector、主机与 Kubernetes 资源管理](./03-connectors-and-resources.md)
5. [Agent、MCP 与两阶段操作](./04-agent-mcp-and-action-workflow.md)
6. [Card Skill 与交互式 UI](./05-card-skills-and-interactive-ui.md)
7. [安全基线与 MVP 路线](./06-security-and-mvp-roadmap.md)
8. [系统初始化与双层管理门户](./07-bootstrap-and-administration.md)
9. [模型与 OpenSandbox 管理](./08-model-and-sandbox-management.md)
10. [OpenTelemetry 接入与监控数据链路](./09-opentelemetry-observability.md)
11. [服务组件与 Kubernetes 一键部署](./10-service-components-and-kubernetes-deployment.md)
12. [运行时状态、Redis 与横向扩展](./11-runtime-state-and-horizontal-scaling.md)

## 关键术语

| 术语 | 含义 |
| --- | --- |
| Model Agent | 理解意图、规划任务并调用工具的 AI Agent |
| Connector | 部署在目标网络中的连接节点，主动连接 Argus 服务端 |
| Host Agent | Connector 在一台机器上的运行实例；第一版中可与 Connector 使用同一程序 |
| MCP Tool | 暴露业务查询或操作能力的工具，不负责决定界面表现 |
| Tool Result | 某一次 MCP Tool 调用的结构化结果，必须具有可追踪的调用标识 |
| Card Skill | 面向 AI 的说明、HTML/CSS/JavaScript UI、数据 Slot、Action Slot、权限和 Demo 的版本化卡片包 |
| Render Plan | AI 对 Tool Result、Card Skill、字段映射和动作绑定作出的声明式渲染决策 |
| Pending Action | 已预览、等待用户确认、可确认/取消/过期的一次服务端操作 |
| `argus__token` | Preview Tool 返回给 Argus 服务端的私有一次性提交能力；模型、用户、浏览器和卡片均不可见 |
| Action Executor | `argus-server` 内负责校验 Card Action、消费私有 Token 并直接调用 Commit Tool 的确定性模块 |
| Host Bridge | 沙箱卡片与 Argus 宿主之间唯一的受控通信通道 |
| Platform Super Admin | 首次初始化创建的平台超级管理员，只管理企业、企业管理员和平台 OpenSandbox 基座 |
| Enterprise Admin | 企业管理员，管理本企业用户、模型、Agent、资源、权限和业务设置 |
| Model Alias | 供 Agent Profile 引用的逻辑模型名称，不直接等同于供应商模型 ID |
| Sandbox Profile | 超级管理员批准的语言、镜像、资源、网络和生命周期策略集合 |
| Leaf Collector | 部署在受管机器上，采集本机数据并向 Direct/Edge Gateway 推送的轻量 Collector |
| Edge Gateway Collector | 部署在客户网络出口节点，接收内网 OTLP 并统一向 Argus 推送的 Collector |
| Argus Telemetry Ingest | `argus-telemetry --mode=ingest` 运行角色，接收、认证、限流并将 OTLP 数据写入 Kafka |
| Telemetry Group | 描述一组互通资源、Collector 角色、推送拓扑和采集 Profile 的企业对象 |
| argus-server | Argus 控制面 API，承载身份、权限、资源、Tool、Card、Pending Action 和监控控制能力 |
| argus-worker | Argus 异步执行面，承载 Agent Harness、模型调用、Tool Run、安装任务和 OpenSandbox 调用 |
| argus-connector-gateway | 只承载 Connector 长连接、命令流和 Artifact Tunnel 的控制链路网关 |
| argus-telemetry | 遥测服务程序，以 ingest 或 query 模式分别承担写入入口和查询入口 |

## 已确定的设计原则

- Chatbox 是交互和编排层，不承载新增主机、查询 Pod 等原生业务逻辑。
- 管理后台、Chatbox、OpenAPI 和自动化任务复用同一套领域服务、权限检查和审计链路。
- Tool 只产出业务数据；Card Skill 不绑定固定 Tool；二者由 AI 生成的 Render Plan 动态连接。
- 数据绑定应引用 `tool_call_id + path`，避免复制值后丢失来源。
- 用户确认后的提交由卡片事件直接触发，不再经过模型推理。
- 所有变更 Tool 必须成对提供 `.preview` 和 `.commit`；Preview 的 `_meta.argus__token` 仅由服务端消费，Commit 不接受可变业务参数。
- AI 可以自由生成具有丰富视觉和交互的 HTML/CSS/JavaScript，但只能运行在受限沙箱中。
- 所有特权操作都必须经过服务端授权、Action Binding 和审计；静态代码扫描不是唯一安全边界。
- 涉及生产变更的能力必须以服务端状态机、幂等和短期一次性授权为基础，不能依赖一次模型上下文维持状态。
- 平台超级管理员与企业管理员使用不同管理域；超级管理员默认无权进入企业会话、模型凭证和受管资源。
- AI 只能选择超级管理员批准的 Sandbox Profile，不能自行指定任意镜像或扩大资源、网络权限。
- OpenTelemetry Collector 本身承担采集与 OTLP 推送；Argus 不额外开发一个重复的遥测 Pusher 进程。
- 互通网络中的 Collector 可以组成 Leaf → Edge Gateway 拓扑，仅 Edge Gateway 需要访问 Argus。
- 第一版只维护 `argus-server`、`argus-worker`、`argus-connector-gateway` 和 `argus-telemetry` 四个自研服务端程序，内部领域模块不拆成微服务。
- PostgreSQL 保存唯一业务状态；Redis 只承担短期锁、租约、限流、Session Registry 加速和状态通知，清空 Redis 不得造成业务状态丢失。
- 六类服务端工作负载均按无本地唯一状态设计并支持横向扩展；Migration、Bootstrap 和协调任务使用 Lease 保证单一所有者。
- 控制链路、遥测推送链路和遥测查询链路使用不同服务、端口、凭证与扩缩容策略。
- Kubernetes 一键部署必须包含 OpenSandbox、Kafka、ClickHouse 等依赖；ClickHouse 统一由 Altinity ClickHouse Operator 管理。
