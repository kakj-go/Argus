# 当前实现盘点与 Kubernetes 落地路线

## 1. 文档定位

本文连接三类信息：

- `docs/00` 至 `docs/12` 已确定的产品和架构约束。
- 仓库截至 2026-08-15 的实际代码状态。
- 从当前骨架推进到可安装、可升级、可验证的 Kubernetes 交付物的实施顺序。

本文不改变既有架构边界。规范性约束仍以[已决策事项与系统不变量](./00-decisions-and-invariants.md)为准，完整目标部署设计仍以[服务组件与 Kubernetes 一键部署](./10-service-components-and-kubernetes-deployment.md)为准。

## 2. 文档体系梳理

现有文档可以按五组阅读：

| 文档组 | 内容 | 解决的问题 |
| --- | --- | --- |
| `00`、`01` | 决策、不变量、产品定位、总体架构 | 哪些边界不能在实现中自行改变 |
| `02`、`03` | 企业身份、RoleBinding/DataScope、Connector、Host、Kubernetes、远程访问 | 谁可以通过哪条连接路径操作哪些资源 |
| `04`、`05`、`06` | Agent、MCP、Preview/Commit、Card、安全和 MVP | AI、浏览器、Card 与确定性执行如何隔离 |
| `07`、`08` | 初始化、双层管理门户、模型和 OpenSandbox | 平台域与企业域如何启动和治理 |
| `09`、`10`、`11`、`12` | 遥测、Kubernetes 部署、运行时状态、技术栈 | 服务如何部署、扩缩容、持久化和测试 |

需要始终一起理解的关键关系：

1. `enterprise_id` 是第一版唯一业务和安全隔离边界；第一版不实现 Project。
2. Host/Kubernetes 标签用于归类与 DataScope 选择；Bastion Scope 和 Telemetry Group 只表达网络或遥测拓扑，不能传播权限。
3. PostgreSQL 保存唯一业务状态；Redis 只做缓存、通知、限流和短期协调。
4. 所有变更操作使用 Preview/Confirm/Commit；模型和浏览器都不能接触私有提交 Token。
5. Connector 控制链路、OTLP 摄入链路、Telemetry Query 链路必须使用独立服务、凭证和扩缩容策略。
6. 第一版所有平台依赖均随 Argus 安装到同一 Kubernetes 集群，不提供外部托管中间件分支。

## 3. 仓库当前状态

### 3.1 总体结论

截至 2026-08-15，当前仓库是“前端产品原型较完整、M0 契约已冻结、后端业务仍处于骨架阶段、Evaluation 部署基座已经可安装”的状态：

| 范围 | 当前状态 | 可交付程度 |
| --- | --- | --- |
| 企业门户 | 页面、路由、i18n、共享 UI、mock 领域和 Playwright E2E 已存在 | 可进行前端产品流程验证 |
| 平台门户 | 页面、路由、i18n、Sandbox/企业/管理员管理 mock 已存在 | 可进行前端产品流程验证 |
| 初始化门户 | 初始化向导、校验、i18n 和 mock 状态机已存在 | 可进行前端产品流程验证 |
| API Client | 已有覆盖当前原型的手写领域接口和 localStorage mock；M0 已新增独立的 `@argus/api-client/contracts` 生成契约 | 现有根接口尚未迁移，真实 Adapter 仍待 M1 |
| `argus-server` | 可启动 HTTP Server，已有 `/healthz`、`/readyz` 和语言协商 | 仅服务骨架 |
| Worker/Gateway/Telemetry/Connector | 进程入口和生命周期骨架已存在 | 尚无领域行为、Agent Loop 和协议实现 |
| `argusctl` | 已实现 preflight、plan、镜像、install、status、verify、tunnel、uninstall | 可安装和验证 Evaluation；Production 安装硬阻断 |
| OpenAPI/protobuf/migration | M0 OpenAPI、JSON Schema、protobuf、错误/状态注册表、Go/TypeScript/Proto 生成和 Breaking Check 已完成；已有最小 PostgreSQL/ClickHouse Migration | 契约可供实现使用，但数据库与领域服务仍未落地 |
| Kubernetes 交付物 | 三个 Dockerfile、六个 Chart、Profile、Schema、版本锁和本地 Registry Loader 已存在 | 可部署完整 Evaluation 基座 |

因此，现阶段可以声明“完整依赖和运行角色可部署”，但不能把前端 mock 流程、后端进程健康和“业务后端已完成”视为同一完成度。

### 3.2 前端应用

| 应用 | 主要职责 | 当前主要页面 |
| --- | --- | --- |
| `web/apps/setup` | 首次初始化 | Setup Token、系统信息、超级管理员、OpenSandbox、确认提交 |
| `web/apps/platform` | 平台超级管理员域 | 平台概览、企业、平台管理员、Sandbox、审计、账号 |
| `web/apps/enterprise` | 企业业务域 | Chatbox、主机、Kubernetes、任务、审批、组织权限、模型、Card、Secret、审计 |

三个应用当前都通过 `VITE_API_MODE` 选择 API 模式，但除 `mock` 外的值会回退到 mock。M0 已提供 `@argus/api-client/contracts`；M1 必须补齐真实 HTTP/SSE/WebSocket Adapter，并让现有 mock 与真实 Adapter 共同消费生成契约。

共享包目录已经按目标边界建立，但实现仍存在一致性欠账，不能视为边界已经完全收敛：

| 包 | 职责 |
| --- | --- |
| `@argus/ui` | 目标中的唯一通用组件库；仍需清理业务应用内平行样式/组件 |
| `@argus/design-tokens` | 主题语义 Token |
| `@argus/api-client` | 领域客户端接口、类型和 mock 实现 |
| `@argus/auth` | 登录 Session 和前端认证状态 |
| `@argus/card-host` | 当前 Card iframe、Host Bridge 和绑定调用；尚未完成 CSP、MessagePort 和 Manifest 安全契约 |
| `@argus/observability` | 前端遥测上下文和事件入口 |

当前 Enterprise Playwright 共 25 条用例并全部通过，主要验证浏览器内 mock 流程；Platform 和 Setup 也有基础发布检查。该套件可以验证静态镜像和前端交互，但仍不等同于真实业务 API、Connector、遥测或 Kubernetes 全链路 E2E。

当前前端还存在必须在真实业务开发前处理的偏差：公开 `PendingAction.params` 暴露了本应仅服务端保存的私有参数；Card Runtime 仍使用全局 `postMessage('*')` 而非 MessagePort/CSP；认证状态持久化在 localStorage；Enterprise 页面级样式文件接近 2000 行并存在 `.argus-*`、Design Token 和组件边界不一致；Setup 登录跳转仍是占位行为，Setup 构建存在约 557 KB Chunk 警告。现有 `typecheck/lint/test/build/e2e` 通过只能说明当前原型自洽，不代表这些架构门禁已经满足。

Agent 运行时当前仍只有目录骨架和前端 mock 流事件：M0 已冻结 ConversationEvent、Run/Step/Task、ModelCall、ToolResultProjection、ContextSnapshot、Tool Metadata 和上下文预算契约，但尚无持久化、Agent Loop、ContextAssembler 或 Compactor 实现。前端 mock 仍直接生成回复和 Tool Trace，不能作为真实 Harness、恢复或上下文压缩实现。目标实现见[Agent Harness 与上下文管理](./16-agent-harness-and-context-management.md)。

### 3.3 后端程序与运行角色

仓库维护六个 Go 入口，其中四个是服务端程序，另外两个分别运行在客户环境和部署者环境：

| 二进制 | 部署位置 | 目标职责 | 当前状态 |
| --- | --- | --- | --- |
| `argus-server` | `argus-system` | Web/API、身份、权限、领域服务、Action Executor | 仅 HTTP 健康检查与 Locale Middleware |
| `argus-worker` | `argus-system` | Agent、Tool Run、任务、Sandbox、安装执行 | 生命周期骨架 |
| `argus-connector-gateway` | `argus-system` | Connector 长连接、命令流、Artifact、Remote Access | 生命周期骨架 |
| `argus-telemetry` | `argus-observability` | `ingest` 写 Kafka；`query` 查 ClickHouse | 模式校验和生命周期骨架 |
| `argus-connector` | 受管主机/堡垒机 | 主动 mTLS 接入、命令和 Artifact/会话隧道 | 生命周期骨架，不部署在平台集群 |
| `argusctl` | 部署者工作站或 CI Runner | Preflight、Install、Upgrade、Verify、Uninstall | Evaluation 安装闭环已实现；独立 Upgrade 子命令待补 |

`argus-worker` 还需要以独立 Deployment 运行 Direct Executor Pool。它复用二进制，但不能复用普通 Worker 的队列、ServiceAccount、NetworkPolicy 或出口策略。

### 3.4 已交付部署基础与剩余边界

截至 2026-08-15 已交付：

- Backend/Web/安全修复 MinIO 多阶段 Dockerfile，ARM64 实际构建和 AMD64 OCI 构建路径。
- `ArgusInstallConfig v1alpha1` JSON Schema、Evaluation/Production Profile 和版本锁定清单。
- Foundation、Data Operators、Data、Sandbox、Platform、Telemetry Pipeline 六个 Helm Release。
- PostgreSQL、Redis、MinIO、OpenSandbox、Strimzi/Kafka、Altinity/ClickHouse、Keeper 和 OTel Writer 的实际 Evaluation 集成。
- `argusctl preflight/plan/images/install/status/verify/tunnel/uninstall` 与阶段状态 ConfigMap。
- 最小 PostgreSQL/ClickHouse Migration、真实中间件往返、OpenSandbox 生命周期、Pod 重建持久化和 Kubernetes 静态前端 E2E。

仍未完成且不能由部署基座替代：

- 真实 OpenAPI/protobuf 业务接口、身份/RBAC、Connector、Agent、OTLP 摄入和查询业务实现。
- 前端从 localStorage mock 切换到真实 HTTP/SSE/WebSocket 客户端。
- SBOM、镜像签名、漏洞门禁、备份恢复和独立 Upgrade 工作流。
- Production PostgreSQL HA 与 OpenSandbox 强化 Runtime ADR；两项未完成前 Production 安装保持硬阻断。

## 4. Kubernetes 目标拓扑

### 4.1 命名空间

沿用三个命名空间，不把中间件全部堆入一个故障域：

| Namespace | 资源 |
| --- | --- |
| `argus-system` | Web、Server、Worker、Direct Executor、Connector Gateway、PostgreSQL、Redis、MinIO、Migration/Bootstrap Job |
| `argus-sandbox` | OpenSandbox API/控制组件及隔离 Runtime 工作负载 |
| `argus-observability` | Strimzi、Kafka、Altinity Operator、ClickHouse/Keeper、Telemetry Ingest/Query、Writer、Schema Job |

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
        Writer["otel-clickhouse-writer"]
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

| Host | SPA | API 路由 |
| --- | --- | --- |
| `argus.example.com` | enterprise | `/api`、SSE/WSS 转发到 `argus-server` |
| `platform.argus.example.com` | platform | `/api` 转发到 `argus-server` 的平台 Audience |
| `setup.argus.example.com` | setup | 仅初始化状态和提交 API |

这样可以保持三个应用当前都从 `/` 启动，避免路径前缀与现有 Router 冲突。三个 Host 可以共享静态 Deployment，但认证 Cookie 必须使用精确 Host/Path 和不同 Audience，不能通过父域 Cookie 混合平台与企业身份。

如果后续决定将静态资源嵌入 `argus-server`，需要单独评估镜像发布耦合、缓存和三个身份域的路由规则；第一阶段不建议同时维护两种生产托管模式。

### 4.4 对外入口

| 入口 | 后端 | 暴露方式 | 关键要求 |
| --- | --- | --- | --- |
| Enterprise/Platform/Setup Web | `argus-web`、`argus-server` | HTTPS Ingress/Gateway | Cookie、CSRF、CSP、SSE/WSS |
| Connector | `argus-connector-gateway` | 独立 TLS/L4 或支持长连接的 Gateway | mTLS、Drain、连接指标 |
| Remote Access | `argus-connector-gateway` 独立 Listener | HTTPS/WSS | 一次性票据、录像、独立限流 |
| OTLP gRPC | `argus-telemetry-ingest:4317` | 支持 HTTP/2 的 L4/L7 入口 | 独立证书、认证、背压 |
| OTLP HTTP | `argus-telemetry-ingest:4318` | HTTPS | 独立限流和请求体限制 |

PostgreSQL、Redis、Kafka、ClickHouse、OpenSandbox、Telemetry Query、Writer 和 Direct Executor 不对集群外暴露 Service。

## 5. Release 与资源所有权

保持多 Release 编排，避免一个 Helm 事务同时管理 CRD、Operator、有状态服务和业务 Deployment：

| Release | 主要资源 | 前置条件 |
| --- | --- | --- |
| `argus-foundation` | Namespace、ServiceAccount、RBAC、Quota、LimitRange、NetworkPolicy、Certificate | 集群预检通过 |
| `argus-data-operators` | Strimzi、Altinity Operator 及锁定 CRD | Foundation |
| `argus-data` | PostgreSQL、Redis、MinIO、Kafka CR、ClickHouseInstallation/Keeper | Operator Ready、StorageClass Ready |
| `argus-sandbox` | OpenSandbox 和 Runtime 配置 | RuntimeClass 与 Artifact Store Ready |
| `argus-platform` | Web、Server、Worker、Direct Executor、Gateway、PostgreSQL Migration、Bootstrap | PostgreSQL Ready |
| `argus-telemetry-pipeline` | ClickHouse Migration、Ingest、Writer、Query、Topic/ACL | Kafka/ClickHouse Ready |

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
8. **Telemetry**：先发布 Ingest，再发布 Writer 和 Query；Ingest 只依赖 Kafka 可写，Writer/Query 依赖 ClickHouse Schema Ready。
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

| 数据 | 权威存储 | 备份要求 |
| --- | --- | --- |
| 身份、RBAC、Run、Task、Action、审计索引 | PostgreSQL | 全量 + PITR/WAL + 恢复演练 |
| 热缓存、通知、限流 | Redis | 可重建；生产可为可用性启用持久化 |
| Artifact、录像、附件、备份目标 | MinIO | 版本控制、生命周期、跨故障域备份 |
| 遥测缓冲 | Kafka | 保留期覆盖 Writer 修复窗口，不替代长期备份 |
| Metrics/Logs/Traces | ClickHouse | 对象存储备份、Schema/数据恢复演练 |

默认升级和卸载不能删除 PVC、Bucket、备份、CRD 或主密钥。Production 默认 `retainData=true`。

## 8. 网络策略基线

所有命名空间先默认拒绝，再按调用关系放行：

- Web 只能访问 Server，不访问数据库和中间件。
- Server 可访问 PostgreSQL、Redis、Telemetry Query 和必要的内部 Gateway API。
- 普通 Worker 可访问 PostgreSQL、Redis、OpenSandbox、Gateway 和允许的模型端点。
- Direct Executor 只访问任务依赖和经校验的公网 SSH/WinRM；必须拒绝私网、环回、链路本地、云元数据、集群网段和平台内部地址。
- Connector Gateway 可访问 PostgreSQL、Redis、Artifact Store；Connector 和 Remote Listener 使用独立入口策略。
- Telemetry Ingest 只访问认证/配额所需控制数据和 Kafka，不直接写 ClickHouse。
- Writer 只消费 Kafka 并写 ClickHouse。
- Telemetry Query 只使用 ClickHouse 只读账号。
- Sandbox 默认只访问受控 Artifact Bucket 和明确批准的网络目标，不访问 Server 数据库、Gateway、ClickHouse 或 Kubernetes API。

Kubernetes NetworkPolicy 通常不能独立保证固定公网出口和 DNS Rebinding 防护。Direct Executor 还需要集群 Egress Gateway/NAT、防火墙和应用层目标复验共同约束。

## 9. Evaluation 与 Production

| 能力 | Evaluation | Production |
| --- | --- | --- |
| 用途 | 开发、演示、功能 E2E | 正式业务 |
| Argus 副本 | 每角色 1 个 | 无状态关键角色至少 2 个 |
| PostgreSQL | 单实例、较小 PVC | HA、反亲和、PITR；具体 Operator 待 ADR |
| Redis | 单实例 | HA/故障转移，仍不保存唯一事实 |
| Kafka | Strimzi 低副本 KRaft | 至少 3 Broker，`min.insync.replicas >= 2` |
| ClickHouse | 单 Shard/单 Replica 可接受 | 至少 2 Replica，Keeper 至少 3 个 |
| Sandbox | 可显式使用降级 Runtime | 必须使用批准的强化 Runtime |
| PDB/HPA | 可简化 | 按角色配置 PDB、HPA 和拓扑分布 |
| 数据保留 | 短 TTL | 按业务 SLO、成本和合规配置 |

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
2. 记录常驻 Argus 工作负载副本数；资源不足时只暂停常驻无状态服务，不删除数据组件。
3. 复用已安装且版本兼容的集群级 Operator；在临时 Namespace 创建独立 CR 和数据卷。
4. 使用 Evaluation Profile 安装完整依赖和 Argus 工作负载。
5. 运行 `argusctl verify`、后端契约/集成测试和三个门户的 Playwright 流程。
6. 验证初始化、平台/企业身份隔离、RoleBinding/DataScope、标签撤权、Preview/Commit、Connector、Sandbox、OTLP 写入/查询和 Pod 故障接管。
7. 导出失败日志、事件、Pod 状态和必要的脱敏 Artifact。
8. 无论成功失败都删除三个临时 Namespace，并等待 PVC/LoadBalancer 等资源收敛。
9. 按记录恢复常驻工作负载副本数并释放测试 Lease。

集群级 CRD/Operator 不属于临时 Namespace 的生命周期，测试清理不得删除它们。所有 E2E Secret、Bucket、Kafka Topic 和 ClickHouse 数据必须带 `run-id`，避免跨用例污染。

## 11. 实施路线

里程碑唯一口径为[端到端实现计划](./15-end-to-end-implementation-plan.md)和[分阶段任务文件](./plans/README.md)，不在本盘点文档维护另一套编号。

- M0 契约与文档冻结已完成，首次合并后成为后续 Breaking Check 基线。
- 下一步执行 M1 前端与 API 基座，把手写 DTO/Mock 迁移到生成契约并建立真实 Adapter 边界。
- M2 交付 Setup → Platform → Enterprise 身份授权垂直闭环；后续按 M3-M8 依次推进资源连接、Agent/Action、Card、远程访问、遥测和 Production 门禁。

## 12. 当前优先级结论

M0 已完成。当前最合理的下一交付目标是在现有可重复安装的 Evaluation 基座上完成“前端/API 基座 → 真实控制面垂直闭环”：

1. 按 M1 清理 Project/Membership/tags/PendingAction 私有字段等旧前端契约，建立显式 mock/real Adapter。
2. 以初始化、平台/企业身份、Department、RoleBinding/DataScope、Session 和审计替换第一批 mock。
3. 用真实 HTTP 客户端跑通三个门户，并在临时命名空间验证跨企业和范围外资源拒绝。
4. 再进入 Connector、执行、Card、Remote Access 和 Telemetry 的分阶段闭环。

详细顺序、任务拆分和阶段退出标准见[端到端实现计划](./15-end-to-end-implementation-plan.md)与[分阶段任务文件](./plans/README.md)。
