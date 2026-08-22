# 已决策事项与系统不变量

本文记录跨文档必须保持一致的架构决策。其他文档中的“建议”“可以”或示例如果与本文冲突，以本文为准；尚未决定的内容必须显式标为“开放问题”，不能由实现人员自行选择不同语义。

## 1. 决策状态

| 状态 | 含义 |
| --- | --- |
| 已决策 | 第一版实现、Schema、测试和部署必须遵守 |
| 实施可选 | 语义已固定，底层技术实现可以替换 |
| 后续能力 | 第一版不实现，但当前对象和协议预留兼容边界 |
| 开放问题 | 实现前必须形成 ADR，不允许在不同模块中分别决定 |

## 2. 企业、身份与资源范围

第一版统一使用 `enterprise_id` 表示企业、租户、安全隔离、计费和数据归属的最高且唯一业务边界。旧讨论中的 Tenant 与 Enterprise 是同一个概念；接口、数据库、Token、审计、Kafka 属性和 ClickHouse 物化列不得同时保留 `tenant_id` 与 `enterprise_id` 两套字段。

身份域固定为：

```text
platform identity   -> enterprise_id 必须为空，只能进入平台管理域
enterprise identity -> enterprise_id 必须存在且只能绑定一个企业，并直接保存 department_id
```

同一登录身份不能同时拥有平台角色和企业角色。平台超级管理员创建企业和初始企业管理员，但不能加入企业、切换成企业身份或通过空 `enterprise_id` 获得所有企业权限。第一版不实现 `EnterpriseMembership`、一个用户属于多个企业、企业切换或通用用户 `Group`；企业内组织归属统一使用 `Department`。

第一版不实现 `Project`、`project_id`、Default Project、Project Selector、Project RoleBinding 或跨 Project 行为。企业内资源归类通过 Host 和 KubernetesCluster 的用户自定义标签完成，标准字段统一为：

```text
labels: Record<string, string>
```

约束如下：

- 标签键和值必须经过长度、字符集、数量和总大小校验；同一资源的标签键唯一。
- `argus.io/*` 为系统保留命名空间，普通用户不能写入或覆盖。
- Host 和 KubernetesCluster 的列表、过滤、批量操作、遥测和授权选择器复用同一标签语义，不再并行维护 `tags`。
- 标签是企业内归类、筛选和资源范围授权的条件，不是新的租户边界。
- 标签选择器必须使用受限、版本化语法并由服务端编译，禁止接收任意 SQL、CEL 或正则作为资源查询条件。
- 修改被生效 DataScope 或 RemoteAccessGrant 选择器命中的标签属于授权敏感变更，必须 Preview/Commit、递增相关 `AuthorizationVersion` 并使旧票据、游标、Pending Action 和活动订阅及时失效。

范围含义必须分离：

```text
enterprise_id                    -> 唯一租户和安全隔离边界
RoleBinding + DataScope          -> 功能能力与企业内资源授权范围
Host/KubernetesCluster labels    -> 用户归类、筛选和资源选择条件
Bastion Scope                    -> 网络接入和堡垒机路由范围
Telemetry Group                  -> 独立 Collector 的遥测转发拓扑
```

Bastion Scope、Telemetry Group 或标签关系都不能跨企业传播权限。所有企业业务表、Token、任务、审计、Kafka 消息和遥测记录必须携带或可由受信关系解析出 `enterprise_id`；通过资源 ID 操作时必须重新校验资源真实 `enterprise_id`，不能只凭全局唯一 ID 或客户端上下文跳过归属检查。

## 3. 授权与权威状态

- RoleBinding 只在企业范围内向 User、Department 或 ServiceAccount 授予稳定的功能能力。
- DataScope 向主体或 RoleBinding 授予显式资源 ID 和/或经过校验的标签选择器；RoleBinding 没有匹配的 DataScope 时，不得推导为企业内全部资源。
- RemoteAccessGrant 独立限定 Host、ManagedAccount、协议、动作和有效期；可以使用显式 Host ID 或经过校验的 Host 标签选择器，不能使用“企业内全部主机”作为隐式默认。
- PostgreSQL 保存所有唯一业务状态，包括 User、Department、RoleBinding、DataScope、RemoteAccessGrant、ManagedAccount、AuthorizationVersion、Run、Step、Task、ToolCall、PendingAction、Approval、ActionBinding、Execution、ConnectorCommand、BastionScope、RemoteAccessSession 和审计索引。
- Redis 用于短期锁、租约加速、幂等窗口、限流、Session Registry 缓存、状态变更通知和短期一次性数据，但不能成为任何不可恢复状态的唯一存储。
- PostgreSQL 与 Redis 之间不使用跨存储分布式事务。业务状态先通过 PostgreSQL 事务和条件更新提交，再通过 Transactional Outbox 发布 Redis Stream/PubSub 通知。
- Redis 丢失或被清空后，系统必须能够从 PostgreSQL 重建可恢复状态并继续运行。

## 4. Tool 安全不变量

- 查询和诊断 Tool 可以是单阶段；任何改变持久状态、远端系统、权限、配置、凭证、资源标签或网络拓扑的 Tool 必须具有同名 `.preview` 和 `.commit`。
- `.preview` 的内部结果必须包含固定私有字段 `_meta.argus__token`；公开结果只包含 `action_ref`、预览、风险、有效期和可用动作。
- `argus__token` 和 PendingAction 私有参数不得进入模型上下文、用户消息、浏览器、Card DOM、日志、普通 Tool Result、公开 API DTO 或审计正文。
- `.commit` 只接受 `argus__token` 和由服务端生成的幂等上下文，不接受可由用户、浏览器或模型修改的业务参数。
- 用户点击确认后，由 `argus-server` 内的 Action Executor 使用服务端私有 Token 直接调用 `.commit`，不再启动模型推理。
- Commit 必须重新检查当前身份、企业状态、功能权限、DataScope、远程/操作授权、授权版本、审批、资源归属、资源标签/版本和执行前置条件。审批只能满足 Policy 的附加条件，不能补齐缺失的基础权限。

## 5. AI、Card 和 Sandbox 不变量

- AI 负责理解、规划和选择，不是权限、审批、Secret 或唯一状态的边界。
- Model Agent 可发现的 Tool、可读取的数据和可操作的资源不得超过当前企业用户的功能权限与 DataScope。Conversation/Run 只绑定服务端确认的 `enterprise_id`；模型输出的企业、标签选择器或资源 ID 只是候选参数。
- 每个 ToolCall、Run、PendingAction 和 Execution 必须保存目标资源引用、授权版本、功能权限和资源范围快照，以便 Commit、恢复、审计和撤权判断。
- Agent Harness 第一版采用单 Agent 小内核；通用子 Agent、Agent 间消息和动态角色委派延后。Card Render 是同一 Run 内的受限声明式步骤，不拥有独立权限。
- ConversationEvent Ledger、RunState/RunCheckpoint 和 ModelContextProjection 必须分离。完整事件历史只追加并保存在 PostgreSQL/Artifact Store；上下文压缩只能生成派生 ContextSnapshot，不能删除、覆盖或成为唯一历史。
- 模型上下文固定由服务端 ContextAssembler 生成，结构为 Typed Run Checkpoint + Narrative Summary + 未压缩的最近完整 Turn。摘要是不可信派生文本，不能作为授权、审批、Commit、DataScope、资源归属或执行状态事实。
- 大 ToolResult 必须优先通过版本化 Projection Schema 做确定性裁剪，完整结果保存在服务端并以 `result_ref` 引用；不能依靠递归自然语言摘要保存唯一诊断证据。
- 自动 Compaction 按模型 Token 预算触发，保留输出预算和安全余量，切点不能拆开 ToolCall/ToolResult、PendingAction 或 Execution 事件组。压缩失败不得静默截断历史。
- Provider 原生 Compaction 只能是 ModelProvider Adapter 的可选优化，不能清空 Argus 事件账本或成为跨 Provider 唯一恢复状态。
- 模型域只有 `AIModel`；调用直接绑定 `model_id + model_revision`，Message/Run 保存实际模型和调用时价格快照，不保留旧的多层模型对象和路由配置。
- 会话允许显式切换模型，切换只影响后续消息；已开始的 Run 固定原模型，系统不得自动 Fallback。
- 模型额度按企业时区自然月、部门总池和可选个人上限治理。缺失额度表示无限，部门总池始终是最终上限。
- 浏览器和交互卡片只获得 `query_binding_id` 或 `action_binding_id`；真实 Token、私有参数和任意 Tool 名称只保存在服务端。
- Card Runtime 必须使用独立 Origin 或严格沙箱 iframe、按卡片生成的 CSP、版本化 Manifest 和 MessageChannel/MessagePort。禁止以全局 `postMessage('*')` 作为运行时协议。
- 企业交互卡片由企业管理员通过受控流程创建，创建后始终禁用；只有安全、Slot Binding 和全部 Demo 场景验证通过后才能启用。系统卡片完全只读，不存在个人卡片。
- OpenSandbox 默认无生产 Secret、无宿主文件系统、无 Connector 直连、无任意外网和无直接生产执行能力。

## 6. Connector、远程访问和遥测不变量

- Connector 只建立出站 mTLS 长连接，处理控制命令、批准 Artifact、端口/协议隧道和经授权的人工远程会话；Collector 不复用 Connector 通道发送遥测。
- Connector 命令必须具有持久化状态、幂等键、连接代次 `connection_epoch`、过期时间和结果未知状态，不能把断连直接视为执行失败。
- 安装 Connector 并注册为堡垒机时必须创建稳定的 Bastion Scope 和对应 Host；经堡垒机接入的内网主机只能归属一个 Bastion Scope。Bastion Scope 不能与可轮换、可重装的 Connector 实例共用同一主键和生命周期。
- 主机连接模式第一版固定为 `connector_local`、`via_bastion`、`direct_ssh` 和 `direct_winrm`。Direct 模式只能由受控 Direct Executor 访问经校验的公网目标，必须执行固定出口、协议/端口白名单、Host Key/目标身份校验和 SSRF/私网地址拦截。
- Remote Access Session 是人工操作边界，不等同于 MCP Tool Commit。它必须使用短期一次性会话票据，并校验 Enterprise、Host、ManagedAccount、协议、动作、Grant、DataScope、授权版本、MFA/审批、最长时长、录像与审计；AI、Card、Automation 和 OpenSandbox 不得获得交互式会话票据。
- 所有已建立管理连接的 Host 都提供统一的“命令行”入口；人工命令行与自动化任务可以复用底层连接适配器，但必须使用不同的票据、API、队列、状态机和审计类型。
- Bastion Scope 成员的 Telemetry Route 只能是直接推送 Argus 或推送到所属堡垒机上已启用 Gateway 模式的 Collector；独立主机不得选择任何 Bastion Scope 内成员作为上游。
- 同一物理 Kubernetes Node 上的同一 `CollectionClaim` 默认只能有一个活动采集所有者；迁移期临时重叠必须指定主实例和过期时间。
- “监控插件”在产品层表示由 Argus 管理的版本化 Collection Profile，不表示运行时下载任意 Collector 插件或向用户暴露任意 YAML。
- 遥测身份来自 Ingest 认证和受信资源目录，固定为 `EnterpriseId + ResourceId + CollectorId`；客户端自报的同名字段必须被覆盖或拒绝，不保留 `ProjectId`。
- Telemetry Query 必须强制企业、授权资源 ID/标签选择器解析结果、Signal、字段脱敏、时间范围和查询预算；Web、Model Agent 和 Card 复用同一安全投影。

## 7. 部署、扩缩容与技术栈不变量

- 自研服务端程序固定为 `argus-server`、`argus-worker`、`argus-connector-gateway` 和 `argus-telemetry`；领域模块不拆成独立微服务。
- 所有服务端实例按无本地唯一状态设计。Worker 使用 PostgreSQL Lease/Fence，Connector Gateway 使用共享 Session Registry，Telemetry Ingest/Query 按各自协议横向扩展。
- 控制、Connector、遥测写入和遥测查询使用不同的 Deployment、端口、凭证、队列和 NetworkPolicy。
- 第一版完整安装包含 PostgreSQL、Redis、S3 兼容 Artifact Store、OpenSandbox、Kafka、Altinity ClickHouse Operator、ClickHouse/Keeper 和 Writer；依赖部署到同一 Kubernetes 集群的隔离命名空间，外部托管中间件延后。
- 前端固定 React + TypeScript + Vite；共享组件只进入 `@argus/ui`，样式使用 Design Token 与 `.argus-*` 类名，单文件不超过 2000 行。
- 后端固定 Go；外部 API 使用 REST/OpenAPI，内部服务和 Connector 使用 gRPC/protobuf，单文件不超过 2000 行。
- PostgreSQL Schema 由版本化 Migration 管理，普通服务启动不得隐式修改 Schema。
- ClickHouse 对产品暴露 Metrics、Logs、Traces 三个查询协议；M10 事实存储按 Enterprise UUID 创建租户物理表。表名只能由受信身份通过 `TenantTableRouter` 生成，物理表内仍保存 `ResourceId` 和 `CollectorId` 供企业内部授权裁剪。

## 8. 第一版开放问题

以下问题不阻止契约与控制面开工，但进入对应里程碑前必须形成 ADR 或 Benchmark：

- Production PostgreSQL Operator、同步复制、PITR 和故障恢复目标。
- Kafka 与 ClickHouse 的容量档位、分片键和写入吞吐基准。
- Card 独立 Origin 的最终域名、静态资源发布和浏览器兼容策略。
- 标签选择器的第一版精确语法、复杂度上限和索引策略。
- Remote Access Gateway 的协议库、录像格式和强制终止实现。
- SBOM、签名、镜像扫描和离线制品的交付工具链。
