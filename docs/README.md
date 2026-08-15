# Argus 设计文档

Argus 是一个面向 AIOps 场景的多租户 SaaS 控制平面。产品以 Chatbox 作为主要入口，以管理后台作为确定性配置入口，以 MCP Tool 作为业务能力边界，并通过 Connector/Agent 连接主机、Kubernetes 集群及其他受管环境。

本文档集记录当前产品讨论形成的基线设计。它不是最终接口定义；涉及协议字段的示例用于表达约束和职责，实施时应再固化为版本化 JSON Schema、OpenAPI 或 protobuf。

## 阅读顺序

1. [已决策事项与系统不变量](./00-decisions-and-invariants.md)
2. [产品定位与总体架构](./01-product-and-architecture.md)
3. [多租户、RBAC 与数据权限](./02-identity-authorization-and-data-permission.md)
4. [Connector、堡垒机、主机与 Kubernetes 资源管理](./03-connectors-and-resources.md)
5. [Agent、MCP 与两阶段操作](./04-agent-mcp-and-action-workflow.md)
6. [交互卡片与渲染运行时](./05-interactive-cards-and-interactive-ui.md)
7. [安全基线与 MVP 路线](./06-security-and-mvp-roadmap.md)
8. [系统初始化与双层管理门户](./07-bootstrap-and-administration.md)
9. [模型与 OpenSandbox 管理](./08-model-and-sandbox-management.md)
10. [OpenTelemetry 接入与监控数据链路](./09-opentelemetry-observability.md)
11. [服务组件与 Kubernetes 一键部署](./10-service-components-and-kubernetes-deployment.md)
12. [运行时状态、Redis 与横向扩展](./11-runtime-state-and-horizontal-scaling.md)
13. [第一版技术栈与代码结构](./12-technology-stack-and-code-structure.md)
14. [当前实现盘点与 Kubernetes 落地路线](./13-current-implementation-and-kubernetes-rollout.md)
15. [PostgreSQL 部署决策](./14-postgresql-deployment-decision.md)

## 关键术语

| 术语 | 含义 |
| --- | --- |
| Model Agent | 理解意图、规划任务并调用工具的 AI Agent |
| Connector | 安装在主机上的接入代理，主动连接 Argus；在堡垒机主机上承担内网资源代理、命令执行、Artifact Tunnel 和远程会话隧道 |
| Bastion Scope | 由一个已注册 Connector 的堡垒机主机创建的稳定管辖范围，包含堡垒机、其 Connector 和经该堡垒机接入的内网主机 |
| Direct Executor | Argus 服务端中受控的公网 SSH/WinRM 执行角色，只连接经过校验的公网目标，不用于穿透客户内网 |
| Remote Access Session | 用户经授权、可选 MFA/审批后创建的人工 SSH 等远程会话；具有短期票据、录像、审计和生命周期状态 |
| Kubernetes Node Host Binding | Kubernetes Node 与 Argus Host 的可信物理身份绑定，用于识别同一机器上的多个 Collector |
| Collection Claim | 某个 Collector 对特定物理资源、信号和采集范围的责任声明；用于阻止 Host Collector 与 DaemonSet 长期重复采集 |
| MCP Tool | 暴露业务查询或操作能力的工具，不负责决定界面表现 |
| Tool Result | 某一次 MCP Tool 调用的结构化结果，必须具有可追踪的调用标识 |
| InteractiveCard | 系统或企业维护的 HTML/CSS/JavaScript 沙箱 UI，包含 Slot、绑定、Demo、验证和启用状态 |
| Render Plan | 内置“渲染交互卡片”Skill 对 Tool Result、已启用卡片、字段映射和动作绑定作出的声明式渲染决策 |
| AIModel | 企业内一步测试创建的 OpenAI Compatible 模型配置，也是调用、计价、额度和治理的唯一模型对象 |
| Department | 企业内成员的唯一组织归属；每个企业用户固定属于一个部门 |
| Pending Action | 已预览、等待用户确认、可确认/取消/过期的一次服务端操作 |
| `argus__token` | Preview Tool 返回给 Argus 服务端的私有一次性提交能力；模型、用户、浏览器和卡片均不可见 |
| Action Executor | `argus-server` 内负责校验 Card Action、消费私有 Token 并直接调用 Commit Tool 的确定性模块 |
| Host Bridge | 沙箱卡片与 Argus 宿主之间唯一的受控通信通道 |
| Platform Super Admin | 首次初始化创建的平台超级管理员，只管理企业、企业管理员和平台 OpenSandbox 基座 |
| Enterprise Admin | 企业管理员，管理本企业用户、模型、Agent、资源、权限和业务设置 |
| Project | 企业内部资源、监控数据、会话和自动化的主要授权边界；每个项目业务对象必须归属一个 Project |
| Project Role Binding | 将企业用户或部门的角色绑定到整个企业或一个 Project 的授权关系 |
| Managed Account | Host 上可由 Credential Broker 代用的目标账号；账号使用、Secret 查看和账号管理是不同权限 |
| Remote Access Grant | 用户或部门对指定 Project/Host、Managed Account、协议、动作和有效期的人工远程访问授权 |
| Authorization Version | 用户、角色或授权变化时递增的版本，用于使票据、Pending Action、查询和缓存及时失效 |
| Sandbox Profile | 超级管理员批准的语言、镜像、资源、网络和生命周期策略集合 |
| Leaf Collector | 部署在受管机器上，采集本机数据并向 Direct/Edge Gateway 推送的轻量 Collector |
| Edge Gateway Collector | 部署在客户网络出口节点，接收内网 OTLP 并统一向 Argus 推送的 Collector |
| Argus Telemetry Ingest | `argus-telemetry --mode=ingest` 运行角色，接收、认证、限流并将 OTLP 数据写入 Kafka |
| Telemetry Group | 描述不依赖 Bastion Scope 的独立 Collector Gateway 组网；堡垒机范围内的成员路由由 Bastion Scope 和 Telemetry Route 共同约束 |
| argus-server | Argus 控制面 API，承载身份、权限、资源、Tool、Card、Pending Action 和监控控制能力 |
| argus-worker | Argus 异步执行面，承载 Agent Harness、模型调用、Tool Run、安装任务和 OpenSandbox 调用 |
| argus-connector-gateway | 承载 Connector 长连接、命令流、Artifact Tunnel 和经短期票据授权的人工远程会话流，不接收 OTLP 遥测 |
| argus-telemetry | 遥测服务程序，以 ingest 或 query 模式分别承担写入入口和查询入口 |

## 已确定的设计原则

- Chatbox 是交互和编排层，不承载新增主机、查询 Pod 等原生业务逻辑。
- 管理后台、Chatbox、OpenAPI 和自动化任务复用同一套领域服务、权限检查和审计链路。
- Tool 只产出业务数据；已启用交互卡片由内置“渲染交互卡片”Skill 选择，并通过 Render Plan 与真实 `tool_call_id + path` 动态连接。
- 数据绑定应引用 `tool_call_id + path`，避免复制值后丢失来源。
- 用户确认后的提交由卡片事件直接触发，不再经过模型推理。
- 所有变更 Tool 必须成对提供 `.preview` 和 `.commit`；Preview 的 `_meta.argus__token` 仅由服务端消费，Commit 不接受可变业务参数。
- AI 可以自由生成具有丰富视觉和交互的 HTML/CSS/JavaScript，但只能运行在受限沙箱中。
- 所有特权操作都必须经过服务端授权、Action Binding 和审计；静态代码扫描不是唯一安全边界。
- 涉及生产变更的能力必须以服务端状态机、幂等和短期一次性授权为基础，不能依赖一次模型上下文维持状态。
- 平台超级管理员与企业管理员使用不同管理域；超级管理员默认无权进入企业会话、模型凭证和受管资源。
- 平台身份与企业身份互斥；一个企业用户只能属于一个企业，第一版不提供多企业 Membership 或企业切换。
- Project 是企业内部资源和监控数据的主要授权边界；Bastion Scope 与 Telemetry Group 只描述网络或遥测拓扑，不能自动授予 Project 权限。
- 企业管理员负责本企业 IAM，但管理权限不自动等于生产 Shell、目标账号、Secret 原值或 AI 生产执行权限。
- AI 只能选择超级管理员批准的 Sandbox Profile，不能自行指定任意镜像或扩大资源、网络权限。
- OpenTelemetry Collector 本身承担采集与 OTLP 推送；Argus 不额外开发一个重复的遥测 Pusher 进程。
- Connector 可以使所在主机成为堡垒机并承载远程访问隧道，但人工远程会话票据不得提供给 AI、交互卡片 或 Sandbox。
- 人工远程访问必须同时授权目标 Host、Managed Account、协议、动作和有效期；文件传输、剪贴板、会话分享和端口转发不能由“允许连接”隐式获得。
- 所有受管 Host 统一提供命令行入口；人工会话与 Collector 安装/配置自动化可以共享底层连接适配器，但使用独立票据、状态机、队列和审计。
- 公网独立主机可以由受控 Direct Executor 通过 SSH/WinRM 管理；私网地址不得借 Direct Executor 绕过堡垒机边界。
- 堡垒机主机可以同时安装 Edge Gateway Collector；遥测仍由 Collector 的 OTLP Pipeline 转发，不能复用 Connector 控制或远程会话通道。
- 互通网络中的 Collector 可以组成 Leaf → Edge Gateway 拓扑，仅 Edge Gateway 需要访问 Argus。
- Host Collector 与 Kubernetes DaemonSet Collector 可以共存，但同一物理资源上的同一 Collection Claim 默认只能有一个活动采集所有者。
- 第一版只维护 `argus-server`、`argus-worker`、`argus-connector-gateway` 和 `argus-telemetry` 四个自研服务端程序，内部领域模块不拆成微服务。
- PostgreSQL 保存唯一业务状态；Redis 只承担短期锁、租约、限流、Session Registry 加速和状态通知，清空 Redis 不得造成业务状态丢失。
- 服务端工作负载均按无本地唯一状态设计并支持横向扩展；普通 Worker 与 Direct Executor 使用同一程序、不同 Deployment/队列/网络策略，Migration、Bootstrap 和协调任务使用 Lease 保证单一所有者。
- 控制链路、遥测推送链路和遥测查询链路使用不同服务、端口、凭证与扩缩容策略。
- Kubernetes 一键部署必须包含 OpenSandbox、Kafka、ClickHouse 等依赖；ClickHouse 统一由 Altinity ClickHouse Operator 管理。
- 第一版所有中间件随 Argus 安装到同一 Kubernetes 集群的隔离命名空间中；外部托管中间件接入延后。
- 第一版主前端固定为 React + TypeScript + Vite，交互卡片保持框架无关的 HTML/CSS/JavaScript 沙箱运行时。
- 第一版后端固定使用 Go；外部 API 使用 REST/OpenAPI，内部服务与 Connector 使用 gRPC/protobuf。
- ClickHouse 按 Metrics、Logs、Traces 三个共享逻辑数据集组织，企业使用行级 `EnterpriseId` 隔离，不为每个企业创建独立表或分区。
- Telemetry Query 在 `EnterpriseId` 之外还必须执行 Project/Resource、Signal、字段脱敏、时间范围和预算约束；用户、AI 和 Card 复用同一裁剪结果。
