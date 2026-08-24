# 当前实现盘点与 Kubernetes 落地路线

## 1. 文档定位

本文连接三类信息：

- `docs/00` 至 `docs/12` 已确定的产品和架构约束。
- 仓库截至 2026-08-18 的实际代码状态。
- 从当前骨架推进到可安装、可升级、可验证的 Kubernetes 交付物的实施顺序。

本文不改变既有架构边界。规范性约束仍以[已决策事项与系统不变量](./00-decisions-and-invariants.md)为准，完整目标部署设计仍以[服务组件与 Kubernetes 一键部署](./10-service-components-and-kubernetes-deployment.md)为准。

## 2. 文档体系梳理

现有文档可以按五组阅读：

| 文档组                 | 内容                                                                   | 解决的问题                            |
| ---------------------- | ---------------------------------------------------------------------- | ------------------------------------- |
| `00`、`01`             | 决策、不变量、产品定位、总体架构                                       | 哪些边界不能在实现中自行改变          |
| `02`、`03`             | 企业身份、RoleBinding/DataScope、Connector、Host、Kubernetes、远程访问 | 谁可以通过哪条连接路径操作哪些资源    |
| `04`、`05`、`06`       | Agent、MCP、Preview/Commit、Card、安全和 MVP                           | AI、浏览器、Card 与确定性执行如何隔离 |
| `07`、`08`             | 初始化、双层管理门户、模型和 OpenSandbox                               | 平台域与企业域如何启动和治理          |
| `09`、`10`、`11`、`12` | 遥测、Kubernetes 部署、运行时状态、技术栈                              | 服务如何部署、扩缩容、持久化和测试    |

需要始终一起理解的关键关系：

1. `enterprise_id` 是第一版唯一业务和安全隔离边界；第一版不实现 Project。
2. Host/Kubernetes 标签用于归类与 DataScope 选择；Bastion Scope 和 Telemetry Group 只表达网络或遥测拓扑，不能传播权限。
3. PostgreSQL 保存唯一业务状态；Redis 只做缓存、通知、限流和短期协调。
4. 所有变更操作使用 Preview/Confirm/Commit；模型和浏览器都不能接触私有提交 Token。
5. Connector 控制链路、OTLP 摄入链路、Telemetry Query 链路必须使用独立服务、凭证和扩缩容策略。
6. 第一版所有平台依赖均随 Argus 安装到同一 Kubernetes 集群，不提供外部托管中间件分支。

## 3. 仓库当前状态

### 3.1 总体结论

截至 2026-08-19，当前仓库已经完成 M0-M7。M2-M7 分别交付 Evaluation 身份授权、资源/Connector、Agent/确定性执行、Card 发布/渲染/Binding、人工远程访问和 OpenTelemetry 遥测闭环；Linux arm64 Host 与 Kubernetes 的 Metrics/Logs/Traces 已通过真实临时 Namespace 验证。

| 范围                               | 当前状态                                                                                               | 可交付程度                                                              |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------- |
| 企业门户                           | M2-M7 身份/IAM、资源、Agent/执行、Card、Remote Access 和 Telemetry 已接 real API                       | Evaluation 范围内的管理、执行、终端、录像与三信号查询可真实使用         |
| 平台门户                           | 首次初始化、平台登录/改密、企业生命周期、临时密码企业管理员、平台审计和 M4 Sandbox 治理已接 real API   | 同一入口完成初始化并切换登录；平台管理与 Sandbox 治理可真实使用         |
| Card Runtime                       | 独立 Origin、CSP/内容哈希/MessagePort 基座已接 CardVersion、公开 RenderPlan、八场景验证和受控 Binding  | 系统/企业 Card 可真实发布、渲染、重新鉴权和触发统一 Action Executor     |
| API Client                         | 生成契约、领域 Port、显式 mock/real Adapter 和 HTTP/SSE/WebSocket Transport 已完成；M2-M7 Path 已接入  | mock/real 配置错误 fail closed；未冻结操作不回退 mock                   |
| `argus-server`                     | M2-M8 身份、资源、Agent/Action、Card、Remote Access、Telemetry 与本地 MFA/恢复 Handler 已接入          | Evaluation 与 local-hardening 可用；Production Profile 继续 fail closed |
| Worker/Gateway/Telemetry/Connector | Worker、Direct Executor、Connector Gateway/Connector 和 Telemetry ingest/writer/query 已实现           | 外部副作用先对账；远程访问与 Collector 命令类型化，Redis 不保存唯一事实 |
| `argusctl`                         | 已实现 preflight、plan、镜像、install、status、verify、tunnel、uninstall                               | 可安装和验证 Evaluation；Production 安装硬阻断                          |
| OpenAPI/protobuf/migration         | M0 门禁、M2-M7 Path/DTO、Connector/Direct Executor/Telemetry protobuf 和六批 Goose/sqlc Schema 已完成  | Evaluation 第一版领域契约与数据模型已落地                               |
| Kubernetes 交付物                  | Dockerfile、六个 Chart、Profile、Schema、版本锁和本地 Registry Loader 已存在；Web 镜像提供两个门户和独立 Card Origin | 可部署完整 Evaluation 基座                                     |

因此，现阶段可以声明“完整依赖和运行角色可部署”，但不能把前端 mock 流程、后端进程健康和“业务后端已完成”视为同一完成度。

### 3.2 前端应用

| 应用                    | 主要职责         | 当前主要页面                                                              |
| ----------------------- | ---------------- | ------------------------------------------------------------------------- |
| `web/apps/platform`     | 初始化与平台管理 | Setup Token、超级管理员初始化、登录、平台概览、企业、管理员、Sandbox、审计、账号 |
| `web/apps/enterprise`   | 企业业务域       | Chatbox、主机、Kubernetes、任务、审批、组织权限、模型、Card、Secret、审计 |
| `web/apps/card-runtime` | 独立 Card Origin | CSP 下加载并运行已校验的 Card 文档，通过 MessagePort 与 Host 通信         |

Enterprise 与 Platform 必须通过 `VITE_API_MODE=mock|real` 显式选择 API 模式。未知模式、real 缺少 `VITE_API_BASE_URL`，或 Enterprise real 缺少 `VITE_CARD_ORIGIN` 时都会停止启动，不会回退到 mock。M2-M7 的身份/IAM、资源/Connector、Conversation/Run、Model、Approval/Execution、Automation、Sandbox、Card、Remote Access 和 Telemetry Path 均已接入 real Adapter；未冻结领域操作继续稳定返回 `CLIENT_OPERATION_UNAVAILABLE`。

共享包目录已在 M1 按目标边界收敛；后续领域实现必须继续复用这些包，不能在业务应用内重新建立平行基座：

| 包                     | 职责                                                                                         |
| ---------------------- | -------------------------------------------------------------------------------------------- |
| `@argus/ui`            | 唯一通用组件库，承载 AppShell、UserMenu、认证状态页、表格、抽屉、图标按钮、Field requirement 和通用表单反馈 |
| `@argus/design-tokens` | 主题语义 Token                                                                               |
| `@argus/api-client`    | 生成契约、领域 Port、mock/real Adapter、HTTP/SSE/WebSocket Transport 和未冻结临时类型        |
| `@argus/auth`          | `unknown → checking → authenticated                                                          | anonymous | unavailable` 认证状态；localStorage 只保存非权威启动提示 |
| `@argus/card-host`     | Manifest/RenderPlan 与内容哈希校验、精确 Origin 握手、MessagePort Bridge 和受控 Binding 调用 |
| `@argus/observability` | 前端遥测上下文和事件入口                                                                     |

前端 Playwright 同时支持 Enterprise、Platform 和 Card Runtime 三个 Origin；初始化流程在 Platform Origin 内验证。既有 mock 套件覆盖产品流程、Audience、Labels、Card Bridge/CSP 与 `zh-CN/en-US × light/dark` axe 门禁；real 模式覆盖 M2 身份、M3 资源接入、M4 Chat/Model/Approval/Execution/Automation/Sandbox、M5 Card、M6 Remote Access 和 M7 Telemetry。真实业务证据由各里程碑 Kubernetes E2E 在临时 Namespace 中运行，不以 mock Playwright 替代。

M1 已清除 Project、Membership、旧 `tags` 和公开 PendingAction 私有字段。两个门户及初始化流程的全部可见 Field 已显式声明 `required/optional/none`，共享组件统一必填星号、复合字段和 ARIA；真实写表单统一使用 React Hook Form + Zod，普通边界消费 bundled OpenAPI 生成的对象/标量约束。临时密码、APIKey、Bastion 安装结果、Execution 一次性结果和 Secret 原值只按一次性结果边界处理。M2-M7 已冻结领域 DTO 均使用生成的 snake_case 契约。

Agent 运行时已完成 Provider-neutral 单 Agent 小内核：PostgreSQL 持久化不可变 ConversationEvent、Run/Step/Task、ModelCall、ToolResult、ContextSnapshot、PendingAction 和 Execution 事实；`ModelUsage` 只从 ModelCall 聚合查询。五个 Worker Pool 通过 Lease/Fence 恢复，支持双模型协议、ContextAssembler、确定性投影与 Compaction、Tool 权限/Schema 门禁、审批和 Action Executor。可信 `run_id` 从 Agent Preview 贯穿 PendingAction/Execution 并在完成后恢复同一 Run 的 Verify Step。实现边界见[Agent Harness 与上下文管理](./16-agent-harness-and-context-management.md)。

### 3.3 后端程序与运行角色

仓库维护十三个 Go 入口：六个生产程序、四个一次性 Job/管理命令、两个仅 E2E 构建的测试程序，以及一个不进入生产部署的跨平台开发工具：

| 二进制                         | 部署位置                 | 目标职责                                          | 当前状态                                                                                                   |
| ------------------------------ | ------------------------ | ------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `argus-server`                 | `argus-system`           | Web/API、身份、权限、领域服务、Action Executor    | M2-M7 Evaluation API 可用                                                                                  |
| `argus-worker`                 | `argus-system`           | Agent、Tool Run、任务、Sandbox、安装执行          | 五个 M4 Pool 可按 Profile 合并或拆分部署；Direct Executor、Collector 与 Remote Access 执行可用             |
| `argus-connector-gateway`      | `argus-system`           | Connector 长连接、命令流、Artifact、Remote Access | mTLS、Registry、epoch、Drain、类型化命令和远程会话跨副本路由可用                                           |
| `argus-telemetry`              | `argus-observability`    | `ingest`、`writer`、`query` 三种模式              | OTLP → Kafka → ClickHouse 与授权 Query 可用                                                                |
| `argus-connector`              | 受管主机/堡垒机          | 主动 mTLS 接入、命令和 Artifact/会话隧道          | Probe、Kubernetes Read、Collector 管理、SSH/WinRS 会话和 Uninstall 可用                                    |
| `argusctl`                     | 部署者工作站或 CI Runner | Preflight、Install、Upgrade、Verify、Backup、Restore、Uninstall | Evaluation 与 Local Hardening 闭环已实现；Production 安装继续 fail closed                         |
| `argus-migrate`                | Migration Job            | Goose Migration 与 advisory lock                  | M2-M7 Schema 由独立 Job 迁移，普通 Server 不修改 Schema                                                    |
| `argus-card-catalog-sync`      | Catalog Sync Job         | 幂等同步版本化系统 Card 目录                      | 系统 Card 只读发布、依赖状态和不可变 Revision 同步可用                                                     |
| `argus-telemetry-catalog-sync` | Catalog Sync Job         | 幂等同步 Distribution/Profile 目录                | Linux arm64 active，Windows amd64 `validation_pending`                                                     |
| `argus-telemetry-dlq-replay`   | 受控 Job/管理命令        | 重放已登记的 Telemetry DLQ                        | 以稳定记录 ID 和平台审计执行                                                                               |
| `argus-replay-model`           | 临时 E2E Namespace       | 固定 Model/Sandbox 回放                           | 仅 `m4e2e` 测试构建使用，生产制品扫描禁止携带                                                              |
| `argus-telemetry-e2e`          | 临时 E2E Namespace       | 生成确定性 OTLP 三信号                            | 仅 E2E 镜像包含，生产制品扫描禁止携带                                                                      |
| `argus-dev`                    | 开发者工作站或 CI Runner | 跨平台检查、构建、发布和 Kubernetes E2E 编排      | Windows、Linux、macOS 使用同一命令；E2E doctor 检查实际目标 Context；生产部署仍只由 `argusctl + Helm` 完成 |

`argus-worker` 保留 agent、action、compaction、automation、sandbox 五条队列和 Processor。Evaluation 通过一个 `argus-worker --pool=default` Deployment 运行这五类任务；Local Hardening 与 Production 使用五个独立 Deployment，以便分别扩缩容和限制网络权限。Direct Executor Pool 在所有 Profile 中均由独立 Deployment 运行。所有 Pool 使用 PostgreSQL Lease/Fence 恢复，Redis 只唤醒；Tool Gateway 当前是 Worker 进程内可信 Registry，未来拆分时才增加内部 mTLS 网络边界。

Evaluation 当前有 Web、Server、合并 Worker、Direct Executor、Connector Gateway、Telemetry Ingest/Writer/Query 共 8 个 Argus 常驻运行角色；相较五个普通 Worker 全拆分的拓扑减少 4 个常驻 Pod，完整 Evaluation 环境预计约 22 个常驻 Pod。代价是五类任务共享 Worker 资源、进程故障域和 NetworkPolicy 权限并集，任一 Processor 致命退出会使整个 Worker Pod 重启。

### 3.4 已交付部署基础与剩余边界

截至 2026-08-20 已交付：

- Backend/Web/安全修复 MinIO 多阶段 Dockerfile，ARM64 实际构建和 AMD64 OCI 构建路径。
- `ArgusInstallConfig v1alpha1` JSON Schema、Evaluation/Production Profile 和版本锁定清单。
- Foundation、Data Operators、Data、Sandbox、Platform、Telemetry Pipeline 六个 Helm Release。
- PostgreSQL、Redis、MinIO、OpenSandbox、Strimzi/Kafka、Altinity/ClickHouse、Keeper 和 OTel Writer 的实际 Evaluation 集成。
- `argusctl preflight/plan/images/install/status/verify/tunnel/uninstall` 与阶段状态 ConfigMap。
- Local Registry 的 `argusctl images load` 每次都会滚动重启节点 Image Loader，使 init container 重新 pull 可复用的 `dev` 标签；配合 `pullPolicy: Never` 时不会继续运行节点缓存中的旧镜像。
- `argus-dev` 为每次 E2E 构建写入 run 专属 OCI label，并通过 Registry V2 API 删除本次精确 tag；删除前会验证 manifest digest 未被正式 `dev` 或其他 tag 共享，Registry 不支持删除或发现共享时 fail closed，避免零残留清理误伤正式镜像。
- M2 PostgreSQL Schema、独立 Goose Migration Job/advisory lock、按领域拆分的 sqlc 数据访问、Outbox Relay 和 Redis Stream 去重。
- Setup Token Secret/轮换、平台与企业双 Audience、Argon2id、Cookie/CSRF/Origin、临时密码首次改密和离线管理员重置。
- 密码策略已由 `api/contracts/password-policy.json` 同时生成 Go/TypeScript 实现，首次登录与账户改密会返回安全的失败规则、长度边界和请求 ID；前端五条密码流程使用同一策略并展示可排查信息。
- HTTP 错误响应的 `message/params/trace_id/request_id` 已跨领域 handler 保留，所有业务错误 mapper 写入稳定 `error_code` 关联日志；错误码注册表已清理重复键并增加“代码返回值必须登记”的契约门禁。
- `/api/v1` 已启用嵌入式 bundled OpenAPI 请求校验；非法 body/path/query/header 在进入领域 Handler 前返回安全 `INVALID_ARGUMENT`。前端可把 `params.field` 回填到 RHF Field，其他错误显示表单摘要和 request ID，不回显输入、正则、堆栈或内部路径。
- 企业生命周期、EnterpriseUser/Department 启停、默认 Department/七个内置 Role/默认空 DataScope、统一授权、签名游标、ServiceAccount/APIKey 和分域 hash-chain 审计。
- `go run ./cmd/argus-dev e2e run --suite m2`：真实 Platform 初始化 → Platform 登录 → Enterprise → IAM → APIKey → Audit → 撤权，Redis 停止和 Server 重启恢复，三 Origin real Playwright，以及成功/失败无条件清理。
- M3 OpenAPI/protobuf、Goose/sqlc、Envelope Encryption、Credential Lease、Host/Kubernetes、网络路径变更绑定冻结 ConnectionTest、最小 PendingAction、Bastion/Connector、Gateway Registry/Pub/Sub/Command/sweeper 和 DataScope 撤权实现。
- cert-manager Connector PKI、Gateway 独立 ServiceAccount/Issuer/RBAC、独立 Direct Executor CA/mTLS RPC、固定 IP/DNS 重校验、Redirect/TLS/SSRF 防护，以及对应 Helm Service/RBAC/NetworkPolicy。
- Enterprise real Adapter 与页面已接 Host 路径迁移、Kubernetes 有界资源/Pod Logs、Secret/ManagedAccount、Bastion/Connector 卸载和 Preview/Confirm；`in_cluster` 一次性安装命令只在确认结果中展示，Collector、Remote Access 和 Agent 操作不读取 mock 数据。
- Connector 卸载结果等待 Gateway ACK 后才删除本地身份；无类型化清理证明的 reconcile 保持 `result_unknown`。有效重连恢复 Bastion Scope/Kubernetes 在线状态，Scope 删除要求已卸载或已 fencing 离线，并逻辑删除根 Host。
- `go run ./cmd/argus-dev e2e run --suite m3`：真实 Secret/Credential/ManagedAccount、Bastion/Connector、证书轮换与 ACK 后卸载、Host 跨 Scope 迁移、双 Gateway 派发、内网/公网 Host、三种 Kubernetes 接入、DataScope 撤权、Redis 停止和 Server/Gateway 恢复，M2 3 条与 M3 6 条 real Playwright，以及成功/失败无条件清理。2026-08-17 的成功运行号为 `20260817060430-49810`，脱敏诊断位于本地同名 `artifacts/m3-e2e/` 目录，Namespace/PVC/Lease 零残留。
- M4 Conversation/Run/Task/Model/Approval/Execution/Automation/Sandbox 契约、Migration、五个 Worker Pool、双协议 Model Provider、ContextAssembler/Compaction、Tool 权限与严格 Schema Registry、确定性 Projection、不可变 AutomationRevision 和确定性 Action Executor。
- Enterprise Chat/Model/Approval/Execution/Automation 与 Platform Sandbox 页面已接 real API；Replay Model Provider 仅存在于 `m4e2e` build tag，生产 Artifact 扫描拒绝测试 Provider、mock seed 和私网模型开关。
- `go run ./cmd/argus-dev e2e run --suite m4`：真实身份与资源基座、双模型协议、Chat Tool、可信 Run→PendingAction→Execution→Verify 绑定、用户确认、多策略审批、Worker 删除、Redis 清空、AutomationRevision 固定、ResultUnknown 不重放、模型额度耗尽、Sandbox 生命周期与配额、real Playwright，以及成功/失败无条件清理。2026-08-17 的成功运行号为 `20260817144832-31660`，脱敏诊断位于 `artifacts/m4-e2e/20260817144832-31660`，Namespace/PVC/Lease 零残留。
- M5 OpenAPI/JSON Schema、Goose/sqlc、不可变 CardVersion、系统 Catalog Sync、企业 Chat Draft、静态/浏览器验证、`card.render`、CardPresentation、Query/Action Binding、版本切换/回滚和授权变化后重新物化。
- Enterprise Card 管理页与 Chat 已接 real API；Card Runtime 继续复用独立 Origin、CSP、内容哈希和 MessagePort，浏览器只持有短期 Binding ID，不获得 Tool 参数、Commit Token 或私有计划。
- `go run ./cmd/argus-dev e2e run --suite m5`：两版企业 Card 八场景验证、系统优先/企业精确匹配、DataScope 撤权、Action Binding 重放/双击、非创建人审批、Commit/Verify、回滚、Redis 清空和 Server 重启恢复。2026-08-17 的最终成功运行号为 `20260817211415-4363`，脱敏诊断位于 `artifacts/m5-e2e/20260817211415-4363`，Namespace/PVC/Lease 零残留。
- M6 RemoteAccessGrant/Policy、AccessRequest/Lease、Session/Ticket、SSH PTY、HTTPS WinRS PowerShell 行模式、Connector/Direct 双向流、跨 Gateway peer 路由、并发限制、撤权和加密录像。
- Enterprise Host/组织设置/审批中心已接 real Remote Access API；`@argus/ui` 使用 `@xterm/xterm`，Ticket 只存在于终端组件内存，录像播放器通过授权 API 增量读取 asciicast v2 NDJSON。远程会话策略要求 MFA 时，浏览器使用正式 Step-up 对话框获取 fresh proof 并自动重试 AccessRequest，登录 MFA 不被错误复用为操作级保证。
- Gateway 外部 WSS `9445`、内部 peer mTLS `9446`、Connector `9443` 和 Direct Executor `9444` 分离；peer owner 通过 Kubernetes API 解析 Ready Pod IP，NetworkPolicy 与最小 Pod `get` RBAC 已自动化。
- `go run ./cmd/argus-dev e2e run --suite m6`：真实 SSH PTY、TLS WinRS 模拟器、Ticket 重放、跨 Gateway/Redis fallback/30 秒 Drain、AuthorizationVersion 旧 Lease 失效、MinIO 连续中断 fail closed、录像、终止、M6 real Playwright 和 Redis 降级恢复。2026-08-18 的最终成功运行号为 `20260818072400-79219`，脱敏诊断位于 `artifacts/m6-e2e/20260818072400-79219`，Namespace/PVC/Lease 零残留。
- M7 Telemetry OpenAPI/protobuf、PostgreSQL/ClickHouse Migration、Distribution/Profile/Collector/Route/Claim/NodeBinding 控制面、独立 mTLS PKI、OTLP gRPC/HTTP Ingest、Kafka Topic/DLQ、最小 Go Writer、ClickHouse 三信号 Schema、授权 Query、Tool 与 Telemetry Overview Card 已完成。
- Linux arm64 OCB Distribution、Host Direct/Bastion 类型化安装路径、Kubernetes Agent/Gateway mTLS 固定模板、严格 Artifact TLS、canonical Operation Plan Hash、Credential Lease、Fence、`result_unknown` 对账和 Windows amd64 `validation_pending` 支持矩阵已落地。
- NodeBinding 保留完整 IP 证据用于匹配，但人工确认哈希只绑定 Node UID/Name、Provider ID、Machine ID 和 System UUID；IP 波动不误失效，强身份漂移会撤销 Binding。Kubernetes Gateway 同 Collector 转发还需匹配可信 Collector ID 与证书序列，kubelet 采集保持证书校验并使用最小 `nodes/stats` RBAC。
- Enterprise Host/Kubernetes Collector 与 Metrics/Logs/Traces 页面、Telemetry 保留期/用量/Catalog 页面已接 real API；ECharts 图表包含表格替代、键盘和读屏语义。
- `go run ./cmd/argus-dev e2e run --suite m7`：Linux arm64 Collector 构建/安装、Kubernetes Agent/Gateway mTLS、NodeBinding 保持/漂移、Direct 与 Bastion Gateway 的真实三信号、Kafka backlog、DLQ replay、Redis outage 持久队列、Pod 删除恢复、Query 跨企业/DataScope/预算/脱敏/授权版本矩阵、Telemetry Card 激活、M2-M5 与 M7 real Playwright。2026-08-19 的最终成功运行号为 `20260819140437-21054`，脱敏诊断位于 `artifacts/m7-e2e/20260819140437-21054`，三个 Namespace、运行相关 PVC 和 Lease 零残留。
- `go run ./cmd/argus-dev e2e run --suite m10-query`：单进程 PromQL/KQL/SkyWalking GraphQL、企业同步租户 Schema lifecycle、每企业六张 ClickHouse 物理表、Native Histogram/Summary、Trace spans/edges、Query Audit、`MaxSamples/MaxSeries` 预算、权限/脱敏、Kafka backlog/DLQ、Redis/PostgreSQL 恢复和 M2-M5/M7 real Playwright。2026-08-22 的最终成功运行号为 `20260822063330-17805`，已验证 Metrics/KQL/GraphQL 三种独立 wire format、固定 SkyWalking SDL 和稳定启动门禁；诊断位于 `artifacts/m10-query-e2e/20260822063330-17805`，三个临时 Namespace、相关 PVC 和 E2E Lease 零残留。

上述运行号来自迁移前的旧 Shell Harness，可继续作为当时版本的验收记录。新的 Go Harness 已覆盖对应场景，但仍须在 `go run ./cmd/argus-dev doctor e2e` 通过（包括至少 25 GiB 可用磁盘）后完成一次实时集群重跑；Make target 仅保留为兼容别名。

仍未完成且不能由部署基座替代：

- SBOM、镜像签名、漏洞门禁、备份恢复和独立 Upgrade 工作流。
- Production PostgreSQL/Kafka/ClickHouse HA、外部 KMS/HSM、Connector/Telemetry CA 根轮换及重启恢复演练、固定出口生产验证、Remote Access 录像不可变保留/恢复、Linux amd64 与真实 Windows 兼容矩阵、OpenSandbox 强化 Runtime ADR，以及平台超级管理员 MFA/恢复/Step-up 的生产验证；这些未完成前 Production 安装保持硬阻断。MFA 能力已存在，但 `spec.security.platformMfaRequired` 在所有 Profile 中默认关闭，需要部署者显式开启。

## 4. Kubernetes 目标拓扑

### 4.1 命名空间

沿用三个命名空间，不把中间件全部堆入一个故障域：

| Namespace             | 资源                                                                                                       |
| --------------------- | ---------------------------------------------------------------------------------------------------------- |
| `argus-system`        | Web、Server、Worker、Direct Executor、Connector Gateway、PostgreSQL、Redis、MinIO、Migration/Bootstrap Job |
| `argus-sandbox`       | OpenSandbox API/控制组件及隔离 Runtime 工作负载                                                            |
| `argus-observability` | Strimzi、Kafka、Altinity Operator、ClickHouse/Keeper、Telemetry Ingest/Query、Writer、Schema Job           |

生产环境建议将 Sandbox、Kafka、ClickHouse 调度到独立 Node Pool，并用 Taint/Toleration、Topology Spread 和资源配额降低相互影响。

### 4.2 工作负载

```mermaid
flowchart TB
    User["Browser / OpenAPI"] --> Edge["Ingress or Gateway"]
    Connector["argus-connector"] --> Edge
    Collector["OTel Collector"] --> Edge

    subgraph System["argus-system"]
        Web["argus-web"]
        Server["argus-server"]
        Worker["argus-worker"]
        Direct["argus-direct-executor"]
        Gateway["argus-connector-gateway"]
        PG["PostgreSQL"]
        Redis["Redis"]
        MinIO["MinIO"]
    end

    subgraph Sandbox["argus-sandbox"]
        OS["OpenSandbox"]
        Runtime["Isolated Runtime"]
    end

    subgraph Observability["argus-observability"]
        Ingest["argus-telemetry ingest"]
        Kafka["Kafka"]
        Writer["argus-telemetry writer"]
        CH["ClickHouse + Keeper"]
        Query["argus-telemetry query"]
    end

    Edge --> Web
    Edge --> Server
    Edge --> Gateway
    Edge --> Ingest
    Server --> PG
    Server --> Redis
    Server --> Query
    Worker --> PG
    Worker --> Redis
    Worker --> Gateway
    Worker --> OS
    Direct --> PG
    Gateway --> MinIO
    OS --> Runtime
    Runtime --> MinIO
    Ingest --> Kafka
    Kafka --> Writer
    Writer --> CH
    Query --> CH
```

### 4.3 前端交付建议

三个 Vite 应用构建为静态资源，使用一个 `argus-web` 镜像和一个 Deployment。静态服务器按 Host 选择不同 SPA 根目录：

| Host                         | SPA        | API 路由                                     |
| ---------------------------- | ---------- | -------------------------------------------- |
| `argus.example.com`          | enterprise | `/api`、SSE/WSS 转发到 `argus-server`        |
| `platform.argus.example.com` | platform   | 初始化状态/提交以及平台 Audience API          |
| `cards.argus.example.com`    | card-runtime | Card iframe 与受控 Binding API             |

三个 Host 可以共享静态 Deployment，但 Platform 和 Enterprise 的认证 Cookie 必须使用精确 Host/Path 和不同 Audience，不能通过父域 Cookie 混合身份；Card Runtime 继续保持独立 Origin。

如果后续决定将静态资源嵌入 `argus-server`，需要单独评估镜像发布耦合、缓存和三个身份域的路由规则；第一阶段不建议同时维护两种生产托管模式。

### 4.4 对外入口

| 入口                          | 后端                                    | 暴露方式                           | 关键要求                   |
| ----------------------------- | --------------------------------------- | ---------------------------------- | -------------------------- |
| Enterprise/Platform Web       | `argus-web`、`argus-server`             | HTTPS Ingress/Gateway              | Cookie、CSRF、CSP、SSE/WSS |
| Card Runtime                  | `argus-web`                              | 独立 HTTPS Origin                  | iframe sandbox、CSP、MessagePort |
| Connector                     | `argus-connector-gateway`               | 独立 TLS/L4 或支持长连接的 Gateway | mTLS、Drain、连接指标      |
| Remote Access                 | `argus-connector-gateway` 独立 Listener | HTTPS/WSS                          | 一次性票据、录像、独立限流 |
| OTLP gRPC                     | `argus-telemetry-ingest:4317`           | 支持 HTTP/2 的 L4/L7 入口          | 独立证书、认证、背压       |
| OTLP HTTP                     | `argus-telemetry-ingest:4318`           | HTTPS                              | 独立限流和请求体限制       |

PostgreSQL、Redis、Kafka、ClickHouse、OpenSandbox、Telemetry Query、Writer 和 Direct Executor 不对集群外暴露 Service。

## 5. Release 与资源所有权

保持多 Release 编排，避免一个 Helm 事务同时管理 CRD、Operator、有状态服务和业务 Deployment：

| Release                    | 主要资源                                                                       | 前置条件                             |
| -------------------------- | ------------------------------------------------------------------------------ | ------------------------------------ |
| `argus-foundation`         | Namespace、ServiceAccount、RBAC、Quota、LimitRange、NetworkPolicy、Certificate | 集群预检通过                         |
| `argus-data-operators`     | Strimzi、Altinity Operator 及锁定 CRD                                          | Foundation                           |
| `argus-data`               | PostgreSQL、Redis、MinIO、Kafka CR、ClickHouseInstallation/Keeper              | Operator Ready、StorageClass Ready   |
| `argus-sandbox`            | OpenSandbox 和 Runtime 配置                                                    | RuntimeClass 与 Artifact Store Ready |
| `argus-platform`           | Web、Server、Worker、Direct Executor、Gateway、PostgreSQL Migration、Bootstrap | PostgreSQL Ready                     |
| `argus-telemetry-pipeline` | ClickHouse Migration、Ingest、Writer、Query、Topic/ACL                         | Kafka/ClickHouse Ready               |

Operator 是集群级或共享能力时，`argusctl` 必须检测兼容版本和资源所有者，不得在卸载某个 Argus 实例时误删其他实例仍在使用的 CRD。

## 6. 安装顺序与门禁

推荐的确定性安装状态机：

1. **Preflight**：检查 Kubernetes/API、StorageClass、Ingress/Gateway、证书、DNS、NetworkPolicy、RuntimeClass、节点容量、固定 Egress 和 Operator 冲突。
2. **Foundation**：创建命名空间、最小 RBAC、默认拒绝网络策略、Quota、证书和镜像拉取 Secret。
3. **Operators**：安装或复用 Strimzi 与 Altinity，等待 CRD、Webhook 和 Controller Ready。
4. **Data**：创建 PostgreSQL、Redis、MinIO、Kafka、ClickHouse/Keeper，逐项执行读写和法定副本检查。
5. **Sandbox**：安装 OpenSandbox，验证隔离 Runtime、无生产 Secret、受控对象存储访问和默认拒绝外网。
6. **Schema**：先完成 PostgreSQL Migration；ClickHouse Migration 独立执行并记录 Schema Version。
7. **Control Plane**：发布 Web、Server、Worker、Direct Executor 和 Connector Gateway。
8. **Telemetry**：先发布 Ingest，再发布 Writer 和 Query；Ingest 依赖窄身份控制数据 Adapter、Redis 和 Kafka，但不依赖 ClickHouse，Writer/Query 依赖 ClickHouse Schema Ready。
9. **Bootstrap**：生成短期 Setup Token Secret，只输出 Secret 名称、读取命令、URL 和过期时间。
10. **Verify**：完成控制面、Sandbox、Connector、Direct Egress 和 OTLP 写入/查询的安装验证。

每一阶段必须幂等并写入安装状态。失败后再次执行应从未完成阶段恢复，而不是删除整个 Release 重装。

## 7. 配置、Secret 与持久化

### 7.1 配置分层

建议配置来源优先级为：

```text
版本化默认值
< Evaluation/Production Profile
< ArgusInstallConfig
< SecretRef/集群发现结果
```

普通 ConfigMap 只保存非敏感配置、端点、Feature Gate 和 Schema Version。数据库密码、Cookie/Token 签名密钥、Envelope Encryption 主密钥、mTLS CA、对象存储凭证和模型 Provider Secret 必须使用 SecretRef。

### 7.2 持久化边界

| 数据                                    | 权威存储   | 备份要求                                   |
| --------------------------------------- | ---------- | ------------------------------------------ |
| 身份、RBAC、Run、Task、Action、审计索引 | PostgreSQL | 全量 + PITR/WAL + 恢复演练                 |
| 热缓存、通知、限流                      | Redis      | 可重建；生产可为可用性启用持久化           |
| Artifact、录像、附件、备份目标          | MinIO      | 版本控制、生命周期、跨故障域备份           |
| 遥测缓冲                                | Kafka      | 保留期覆盖 Writer 修复窗口，不替代长期备份 |
| Metrics/Logs/Traces                     | ClickHouse | 对象存储备份、Schema/数据恢复演练          |

默认升级和卸载不能删除 PVC、Bucket、备份、CRD 或主密钥。Production 默认 `retainData=true`。

## 8. 网络策略基线

所有命名空间先默认拒绝，再按调用关系放行：

- Web 只能访问 Server，不访问数据库和中间件。
- Server 可访问 PostgreSQL、Redis、Telemetry Query 和必要的内部 Gateway API。
- 普通 Worker 可访问 PostgreSQL、Redis、OpenSandbox、Gateway 和允许的模型端点。
- Direct Executor 只访问任务依赖和经校验的公网 SSH/WinRM；必须拒绝私网、环回、链路本地、云元数据、集群网段和平台内部地址。
- Connector Gateway 可访问 PostgreSQL、Redis、Artifact Store；Connector 和 Remote Listener 使用独立入口策略。
- Telemetry Ingest 只通过窄控制数据 Adapter 读取 Collector/Certificate/Route 身份并访问 Redis/Kafka，不直接写 ClickHouse，也不能修改资源、IAM 或 Action 事实。
- Writer 只通过窄结算 Adapter 读取 Retention、登记 DLQ、累计 Usage，并消费 Kafka、写 ClickHouse；`local-hardening` 已为 Ingest/Writer 签发独立表级最小权限 PostgreSQL Login，Production 凭证轮换仍需环境验证。
- Telemetry Query 只使用 ClickHouse 只读账号。
- Sandbox 默认只访问受控 Artifact Bucket 和明确批准的网络目标，不访问 Server 数据库、Gateway、ClickHouse 或 Kubernetes API。

Kubernetes NetworkPolicy 通常不能独立保证固定公网出口和 DNS Rebinding 防护。Direct Executor 还需要集群 Egress Gateway/NAT、防火墙和应用层目标复验共同约束。

## 9. Evaluation 与 Production

| 能力        | Evaluation                   | Production                                |
| ----------- | ---------------------------- | ----------------------------------------- |
| 用途        | 开发、演示、功能 E2E         | 正式业务                                  |
| Argus 副本  | 每角色 1 个                  | 无状态关键角色至少 2 个                   |
| Worker 拓扑 | 一个 default Pool Deployment | 五个按 Pool 拆分的 Deployment             |
| PostgreSQL  | 单实例、较小 PVC             | HA、反亲和、PITR；具体 Operator 待 ADR    |
| Redis       | 单实例                       | HA/故障转移，仍不保存唯一事实             |
| Kafka       | Strimzi 低副本 KRaft         | 至少 3 Broker，`min.insync.replicas >= 2` |
| ClickHouse  | 单 Shard/单 Replica 可接受   | 至少 2 Replica，Keeper 至少 3 个          |
| Sandbox     | 可显式使用降级 Runtime       | 必须使用批准的强化 Runtime                |
| PDB/HPA     | 可简化                       | 按角色配置 PDB、HPA 和拓扑分布            |
| 数据保留    | 短 TTL                       | 按业务 SLO、成本和合规配置                |

Production Profile 目前仍受两个开放 ADR 阻塞：PostgreSQL HA/备份组件，以及 OpenSandbox 强化 Runtime 选型。未完成 ADR 前可以交付 Evaluation，但不能宣称 Production Profile 已定型。

## 10. Kubernetes E2E 方案

前端现有 Playwright E2E 继续作为快速 UI 门禁；新增全链路 E2E 使用唯一运行 ID 创建临时命名空间组：

```text
argus-e2e-<run-id>-system
argus-e2e-<run-id>-sandbox
argus-e2e-<run-id>-observability
```

推荐流程：

1. 获取集群级测试 Lease，防止多个重型 E2E 同时争抢资源。
2. 使用专用、干净的测试 Context；不得在已有正式 Argus 的同一集群中安装第二套上游 Operator。
3. `doctor e2e` 检查 Strimzi/OpenSandbox 固定 ClusterRole 的 Helm 所有权，发现占用时在镜像构建前以能力错误退出。
4. 在专用集群中使用 Evaluation Profile 安装独立 Operator、CRD、数据卷和 Argus 工作负载。
5. 运行 `argusctl verify`、后端契约/集成测试和两个门户及 Card Runtime 的 Playwright 流程。
6. 验证初始化、平台/企业身份隔离、RoleBinding/DataScope、标签撤权、Preview/Commit、Connector、Sandbox、OTLP 写入/查询和 Pod 故障接管。
7. 导出失败日志、事件、Pod 状态和必要的脱敏 Artifact。
8. 无论成功失败都删除三个临时 Namespace、测试拥有的 Operator/CRD、PVC、Fixture、镜像和 Cluster RBAC，并等待资源收敛。
9. 释放测试 Lease；正式部署位于其他 Context，不参与暂停、接管或恢复。

完整 E2E 对目标 Context 拥有独占生命周期；它只删除由本次 release 标记拥有的 CRD，找不到 ownership label 时拒绝宽泛删除。所有 E2E Secret、Bucket、Kafka Topic 和 ClickHouse 数据必须带 `run-id`，避免跨用例污染。

## 11. 实施路线

里程碑唯一口径为[端到端实现计划](./15-end-to-end-implementation-plan.md)和[分阶段任务文件](./plans/README.md)，不在本盘点文档维护另一套编号。

- M0 契约与文档、M1 前端/API 基座、M2 身份授权、M3 资源/Connector、M4 Agent/确定性执行、M5 Card、M6 Remote Access 和 M7 Telemetry Evaluation 闭环均已完成。
- M8 本地范围已实现 MFA/Step-up、OpenBao Transit、备份恢复、升级和供应链基座；Production HA、容量、固定出口和跨集群灾备转入独立 Validation 清单。

## 12. 当前优先级结论

截至 2026-08-24，M0-M7 与 M8 本地加固范围已完成，后续工作集中在持续回归和 Production Validation：

1. 在专用测试 Context 先运行 `go run ./cmd/argus-dev doctor e2e`，能力满足后运行 `go run ./cmd/argus-dev e2e run --suite m8`，持续归档 OpenBao、故障注入、备份和新 Namespace 恢复证据。
2. 运行 `go run ./cmd/argus-dev release local`，归档 SBOM、漏洞、License、离线 Manifest 和本地签名。
3. 保持 Production Profile fail closed，并维护 HA、容量、固定出口、KMS HA、AMD64/Windows 和跨集群灾备清单。

详细顺序、任务拆分和阶段退出标准见[端到端实现计划](./15-end-to-end-implementation-plan.md)与[分阶段任务文件](./plans/README.md)。

## 13. M8 最终验证记录（2026-08-24）

运行 `fv-20260824-m8-final13` 在当时无其他 Argus release 的 Docker Desktop `v1.36.1`、arm64 节点上完成 M6、M7、Local Hardening 和全新 Namespace Restore。M6 身份/资源/Remote Access、M7 Telemetry（12/12，zh-CN/en-US、明暗主题）、`argusctl verify`、OpenBao/Redis/Pod 故障验证、7 文件备份、MinIO 对象回读和恢复后 `argusctl verify` 均通过。首次运行因主机磁盘 22.1 GiB 低于 25 GiB 门槛被 fail closed；清理未使用 BuildKit 缓存后以 44.2 GiB 重跑通过。

清理顺序固定为脱敏诊断、停止本地进程/端口转发、Fixture/镜像清理、`argusctl uninstall`、Namespace/PVC/Lease/Cluster RBAC 删除和 CRD 收敛。端口转发在 Service 已由卸载删除时的 NotFound 被视为幂等成功，其他清理错误仍使运行失败。

后续在已安装正式 `argus` release 的同一 Context 上尝试回归时，暴露出 Strimzi/OpenSandbox 固定 ClusterRole 的 Helm ownership 冲突。当前 Harness 已将该条件前移到 `doctor e2e`；这类 Context 被明确判定为不兼容，正式命名空间保持运行，不再经过长时间镜像构建后才失败。
