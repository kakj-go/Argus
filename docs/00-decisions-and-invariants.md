# 已决策事项与系统不变量

本文记录跨文档必须保持一致的架构决策。其他文档中的“建议”“可以”或示例如果与本文冲突，以本文为准；尚未决定的内容必须显式标为“开放问题”，不能由实现人员自行选择不同语义。

## 1. 决策状态

| 状态 | 含义 |
| --- | --- |
| 已决策 | 第一版实现、Schema、测试和部署必须遵守 |
| 实施可选 | 语义已固定，底层技术实现可以替换 |
| 后续能力 | 第一版不实现，但当前对象和协议预留兼容边界 |
| 开放问题 | 实现前必须形成 ADR，不允许在不同模块中分别决定 |

## 2. 企业与 Project 隔离边界

第一版统一使用 `enterprise_id` 表示企业、租户、安全隔离、计费和数据归属的最高业务边界。旧讨论中的 Tenant 与 Enterprise 是同一个概念；接口、数据库、Token、审计、Kafka 属性和 ClickHouse 物化列不得同时保留 `tenant_id` 与 `enterprise_id` 两套字段。

身份域第一版固定为：

```text
platform identity   -> enterprise_id 必须为空，只能进入平台管理域
enterprise identity -> enterprise_id 必须存在且终身至多绑定一个企业，只能进入该企业业务域
```

同一登录身份不能同时拥有平台角色和企业角色。平台超级管理员创建企业和初始企业管理员，但不能加入企业、切换成企业身份或通过空 `enterprise_id` 获得所有企业权限。第一版不实现一个用户属于多个企业，不保留企业 Membership 和企业切换语义。

`project_id` 是企业内部资源、监控数据、会话和自动化的主要授权边界。企业创建时必须创建一个可重命名的默认 Project；Host、KubernetesCluster、Collector、Telemetry Resource、Alert Rule、Dashboard、Conversation、Run 和 Automation 等项目业务对象不得长期处于无 Project 状态。

以下范围含义必须分离：

```text
Project / Project RoleBinding -> 企业内业务和数据授权范围
Bastion Scope                 -> 网络接入和堡垒机路由范围
Telemetry Group               -> 独立 Collector 的遥测转发拓扑
environment / tags            -> Policy 输入和后续动态筛选，不是租户边界
```

Bastion Scope 或 Telemetry Group 的成员关系不能自动授予 Project 权限。目标资源的 `enterprise_id + project_id` 才是授权判断的业务归属；网络路径只决定已经授权的操作如何到达资源。

所有企业业务表、Token、任务、审计、Kafka 消息和遥测记录必须携带或可由受信关系解析出 `enterprise_id`。所有项目对象和项目操作必须同时校验 `enterprise_id + project_id`，不能只凭全局唯一 ID 跳过归属检查。

## 3. 权威状态和缓存

- PostgreSQL 保存所有唯一业务状态，包括 User、Project、Group、RoleBinding、RemoteAccessGrant、ManagedAccount、AuthorizationVersion、Run、Step、Task、ToolCall、PendingAction、Approval、ActionBinding、Execution、ConnectorCommand、BastionScope、RemoteAccessSession 和审计索引。
- Redis 用于短期锁、租约加速、幂等窗口、限流、Session Registry 缓存、状态变更通知和短期一次性数据，但不能成为任何不可恢复状态的唯一存储。
- PostgreSQL 与 Redis 之间不使用跨存储分布式事务。业务状态先通过 PostgreSQL 事务和条件更新提交，再通过 Transactional Outbox 发布 Redis Stream/PubSub 通知。
- Redis 丢失或被清空后，系统必须能够从 PostgreSQL 重建可恢复状态并继续运行。

## 4. Tool 安全不变量

- 查询和诊断 Tool 可以是单阶段；任何改变持久状态、远端系统、权限、配置、凭证或网络拓扑的 Tool 必须具有同名 `.preview` 和 `.commit`。
- `.preview` 的内部结果必须包含固定私有字段 `_meta.argus__token`；公开结果只包含 `action_ref`、预览、风险、有效期和可用动作。
- `argus__token` 不得进入模型上下文、用户消息、浏览器、Card DOM、日志、普通 Tool Result 或审计正文。
- `.commit` 只接受 `argus__token` 和由服务端生成的幂等上下文，不接受可由用户、浏览器或模型修改的业务参数。
- 用户点击确认后，由 `argus-server` 内的 Action Executor 使用服务端私有 Token 直接调用 `.commit`，不再启动模型推理。
- Commit 必须重新检查当前身份、企业状态、Project 权限、远程/操作授权、授权版本、审批、资源归属、资源版本和执行前置条件。审批只能满足 Policy 的附加条件，不能补齐缺失的基础权限。

## 5. AI、Card 和 Sandbox 不变量

- AI 负责理解、规划和选择，不是权限、审批、Secret 或唯一状态的边界。
- Model Agent 可发现的 Tool、可读取的数据和可操作的资源不得超过当前用户在当前企业和 Project 的权限。Conversation/Run 必须保存服务端确认的 `project_id`；模型输出的企业、Project 或资源 ID 只是候选参数。
- 模型域只有 `AIModel`；调用直接绑定 `model_id + model_revision`，Message/Run 保存实际模型和调用时价格快照，不保留旧的多层模型对象和路由配置。
- 会话允许显式切换模型，切换只影响后续消息；已开始的 Run 固定原模型，系统不得自动 Fallback。
- 模型额度按企业时区自然月、部门总池和可选个人上限治理。缺失额度表示无限，部门总池始终是最终上限。
- 浏览器和交互卡片只获得 `action_binding_id`；真实 `argus__token` 只保存在服务端。
- 交互卡片只能读取已绑定数据、执行已绑定查询或动作，不能提交任意 Tool 名称。
- 企业交互卡片由企业超级管理员通过会话 `/` 命令创建，创建后始终禁用；只有安全、Slot Binding 和全部 Demo 场景验证通过后才能启用。系统卡片完全只读。
- OpenSandbox 默认无生产 Secret、无宿主文件系统、无 Connector 直连、无任意外网和无直接生产执行能力。

## 6. Connector 和遥测不变量

- Connector 只建立出站 mTLS 长连接，处理控制命令、批准 Artifact、端口/协议隧道和经授权的人工远程会话；Collector 不复用 Connector 通道发送遥测。
- Connector 命令必须具有持久化状态、幂等键、连接代次 `connection_epoch`、过期时间和结果未知状态，不能把断连直接视为执行失败。
- 安装 Connector 并注册为堡垒机时必须创建稳定的 Bastion Scope 和对应 Host；经堡垒机接入的内网主机只能归属一个 Bastion Scope。Bastion Scope 不能与可轮换、可重装的 Connector 实例共用同一主键和生命周期。
- 主机连接模式第一版固定为 `connector_local`、`via_bastion`、`direct_ssh` 和 `direct_winrm`。Direct 模式只能由受控 Direct Executor 访问经校验的公网目标，必须执行固定出口、协议/端口白名单、Host Key/目标身份校验和 SSRF/私网地址拦截。
- Remote Access Session 是人工操作边界，不等同于 MCP Tool Commit。它必须使用短期一次性会话票据，并校验 Project、Host、ManagedAccount、协议、动作、授权有效期、授权版本、MFA/审批、最长时长、录像与审计；AI、交互卡片、Automation 和 OpenSandbox 不得获得交互式会话票据。
- 所有已建立管理连接的 Host 都提供统一的“命令行”入口；SSH/Connector 本机路径使用交互式 PTY，WinRM 路径使用受审计的 PowerShell Runspace。人工命令行与 Collector 安装等自动化任务可以复用 Connector/Direct Executor、Credential Broker 和连接适配器，但必须使用不同的票据、API、队列、状态机和审计类型；自动化任务不得通过向人工终端注入命令实现。
- 堡垒机主机可以同时运行 `argus-connector` 和 Edge Gateway Collector，但两者必须是独立进程、端口、凭证、队列和资源限制。只有 Collector 接收和推送 OTLP。
- Bastion Scope 成员的 Telemetry Route 只能是直接推送 Argus 或推送到所属堡垒机上已启用 Gateway 模式的 Collector；独立主机不得选择任何 Bastion Scope 内的堡垒机或成员作为上游。
- 同一物理 Kubernetes Node 可以同时运行 Host Collector 和 DaemonSet Collector，但同一物理资源上的同一 `CollectionClaim` 默认只能有一个活动采集所有者。系统必须先建立 Kubernetes Node 与 Host 的可信绑定，再对 Profile 展开后的 Claim 做冲突检查；迁移期临时重叠必须指定主实例和过期时间，不能把重复采集作为常态。
- “监控插件”在产品层表示由 Argus 管理的版本化 Collection Profile，不表示运行时下载任意 Collector 插件或向用户暴露任意 YAML。第一版 Trace 输入以 OTLP 为主并提供 Jaeger/Zipkin 兼容；Logs 以主动读取系统、受控文件和 Kubernetes 容器日志为主；Metrics 使用 Argus 封装并锁定版本的 OpenTelemetry Contrib Receiver/Profile。
- 遥测企业、Project 和资源身份来自 Ingest 认证结果及受信 Collector/Resource 关系；客户端自报的 `EnterpriseId`、`ProjectId`、`ResourceId`、`CollectorId` 必须被覆盖。Telemetry Query 必须同时强制企业、Project/Resource、Signal、字段脱敏和查询预算，不能只注入 `EnterpriseId`。
- 遥测写入采用至少一次语义；重复、DLQ 和查询修正策略属于发布门禁，不能假设 Kafka 或 ClickHouse 提供端到端 exactly-once。

## 7. 部署和扩缩容不变量

- PostgreSQL Migration 必须先于 `argus-server`/`argus-worker` 就绪；ClickHouse Schema Migration 必须先于 Writer 和 Query 就绪。
- `argus-server`、普通 `argus-worker`、Direct Executor Worker Pool、`argus-connector-gateway`、Telemetry Ingest、Telemetry Query 和 Writer 均按无本地唯一状态设计，允许按运行角色独立横向扩展。
- Migration、Bootstrap、定时协调和单次修复任务通过 Lease/Leader Election 保证同一逻辑任务只有一个所有者。
- 横向扩展不能依赖 Pod 本地 Session、内存队列或本地文件保存唯一业务状态。

第一版部署模式进一步固定为：PostgreSQL、Redis、S3 兼容 Artifact Store、OpenSandbox、Kafka、ClickHouse 及其 Operator/运行时均由 `argusctl` 安装到同一 Kubernetes 集群的隔离命名空间。第一版不实现外部托管 PostgreSQL、Redis、Kafka、ClickHouse、Artifact Store 或 OpenSandbox 的安装适配；相关 Adapter 边界可以保留，但不进入发布和测试矩阵。

## 8. 第一版技术栈不变量

- 主前端使用 React、TypeScript、Vite 和 pnpm workspace；三个门户共享唯一 UI 包和 Design Token。
- 初始化、平台和企业三个门户以及 Card Runtime 必须同时支持 `zh-CN`、`en-US` 与浅色、深色主题。颜色必须来自语义 Design Token；语言切换不能改变权限、状态机、枚举、Tool Schema 或审计事实语义。
- 外部 HTTP API 使用 `Accept-Language` 协商语言并返回 `Content-Language`。后端错误、通知和审计事件必须保留稳定 `code/message_key + params`，不能只保存某一种语言的展示字符串。
- 交互卡片 继续使用框架无关的 HTML/CSS/JavaScript，并运行在受限 iframe 中，不能直接复用主应用运行时权限。
- 后端使用 Go。外部业务 API 使用 REST/OpenAPI 3.1；内部服务、Gateway 转发和 Connector 长连接使用 gRPC/protobuf。
- PostgreSQL 访问使用显式 SQL、`pgx` 和 `sqlc`；不能使用 ORM 隐藏状态机的条件更新、Lease、Fence Token 和 Outbox 事务。
- ClickHouse 对产品暴露 Metrics、Logs、Traces 三个逻辑数据集。所有企业共享版本化物理表并包含受信的 `EnterpriseId`、`ProjectId` 和 `ResourceId`，禁止按企业或 Project 创建独立表或分区。
- ClickHouse 物理层从第一版预留 Local/Distributed Table、复制和 Sharding Key；逻辑三类数据集不等于集群中永远只有三张物理表。

## 9. 第一版开放问题

以下事项必须在对应功能进入开发前形成 ADR：

1. PostgreSQL 内置高可用实现和备份组件选型。
2. Telemetry Metrics 的重复点、Counter Reset、Rate 和降采样语义。
3. 标准 Kafka Receiver 是否满足延迟标记和 DLQ 隔离；不满足时启用最小自研 Writer。
4. 同一 Kubernetes 集群内的 Production OpenSandbox 使用 gVisor、Kata 还是 OpenSandbox 后端原生微虚拟机。
5. 外部身份提供商、MFA 和企业级 SSO 的第一版范围。
6. Metrics 物理层使用统一稀疏 Local Table，还是在 Writer Gate 后按 Metric Type 拆分 Local Table。
