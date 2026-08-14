# 已决策事项与系统不变量

本文记录跨文档必须保持一致的架构决策。其他文档中的“建议”“可以”或示例如果与本文冲突，以本文为准；尚未决定的内容必须显式标为“开放问题”，不能由实现人员自行选择不同语义。

## 1. 决策状态

| 状态 | 含义 |
| --- | --- |
| 已决策 | 第一版实现、Schema、测试和部署必须遵守 |
| 实施可选 | 语义已固定，底层技术实现可以替换 |
| 后续能力 | 第一版不实现，但当前对象和协议预留兼容边界 |
| 开放问题 | 实现前必须形成 ADR，不允许在不同模块中分别决定 |

## 2. 企业是唯一业务隔离边界

第一版统一使用 `enterprise_id` 表示企业、租户、安全隔离、计费和数据归属的最高业务边界。旧讨论中的 Tenant 与 Enterprise 是同一个概念；接口、数据库、Token、审计、Kafka 属性和 ClickHouse 物化列不得同时保留 `tenant_id` 与 `enterprise_id` 两套字段。

企业内部的数据范围使用：

```text
project_id / resource_group_id
owner_user_id / owner_team_id
environment / tags
```

平台管理域不属于任何企业。平台超级管理员的身份上下文不能通过空 `enterprise_id` 自动获得所有企业权限。

## 3. 权威状态和缓存

- PostgreSQL 保存所有唯一业务状态，包括 Run、Step、Task、ToolCall、PendingAction、Approval、ActionBinding、Execution、ConnectorCommand 和审计索引。
- Redis 用于短期锁、租约加速、幂等窗口、限流、Session Registry 缓存、状态变更通知和短期一次性数据，但不能成为任何不可恢复状态的唯一存储。
- PostgreSQL 与 Redis 之间不使用跨存储分布式事务。业务状态先通过 PostgreSQL 事务和条件更新提交，再通过 Transactional Outbox 发布 Redis Stream/PubSub 通知。
- Redis 丢失或被清空后，系统必须能够从 PostgreSQL 重建可恢复状态并继续运行。

## 4. Tool 安全不变量

- 查询和诊断 Tool 可以是单阶段；任何改变持久状态、远端系统、权限、配置、凭证或网络拓扑的 Tool 必须具有同名 `.preview` 和 `.commit`。
- `.preview` 的内部结果必须包含固定私有字段 `_meta.argus__token`；公开结果只包含 `action_ref`、预览、风险、有效期和可用动作。
- `argus__token` 不得进入模型上下文、用户消息、浏览器、Card DOM、日志、普通 Tool Result 或审计正文。
- `.commit` 只接受 `argus__token` 和由服务端生成的幂等上下文，不接受可由用户、浏览器或模型修改的业务参数。
- 用户点击确认后，由 `argus-server` 内的 Action Executor 使用服务端私有 Token 直接调用 `.commit`，不再启动模型推理。
- Commit 必须重新检查当前身份、企业状态、权限、审批、资源归属、资源版本和执行前置条件。

## 5. AI、Card 和 Sandbox 不变量

- AI 负责理解、规划和选择，不是权限、审批、Secret 或唯一状态的边界。
- 浏览器和 Card Skill 只获得 `action_binding_id`；真实 `argus__token` 只保存在服务端。
- Card Skill 只能读取已绑定数据、执行已绑定查询或动作，不能提交任意 Tool 名称。
- OpenSandbox 默认无生产 Secret、无宿主文件系统、无 Connector 直连、无任意外网和无直接生产执行能力。

## 6. Connector 和遥测不变量

- Connector 只建立出站 mTLS 长连接，并只处理控制命令和批准 Artifact；Collector 不复用 Connector 通道发送遥测。
- Connector 命令必须具有持久化状态、幂等键、连接代次 `connection_epoch`、过期时间和结果未知状态，不能把断连直接视为执行失败。
- 遥测企业身份来自 Ingest 认证结果，资源身份来自受信 Collector 凭证链；客户端自报的同名属性必须被覆盖。
- 遥测写入采用至少一次语义；重复、DLQ 和查询修正策略属于发布门禁，不能假设 Kafka 或 ClickHouse 提供端到端 exactly-once。

## 7. 部署和扩缩容不变量

- PostgreSQL Migration 必须先于 `argus-server`/`argus-worker` 就绪；ClickHouse Schema Migration 必须先于 Writer 和 Query 就绪。
- `argus-server`、`argus-worker`、`argus-connector-gateway`、Telemetry Ingest、Telemetry Query 和 Writer 均按无本地唯一状态设计，允许横向扩展。
- Migration、Bootstrap、定时协调和单次修复任务通过 Lease/Leader Election 保证同一逻辑任务只有一个所有者。
- 横向扩展不能依赖 Pod 本地 Session、内存队列或本地文件保存唯一业务状态。

## 8. 第一版开放问题

以下事项必须在对应功能进入开发前形成 ADR：

1. PostgreSQL 内置高可用实现和备份组件选型。
2. Telemetry Metrics 的重复点、Counter Reset、Rate 和降采样语义。
3. 标准 Kafka Receiver 是否满足延迟标记和 DLQ 隔离；不满足时启用最小自研 Writer。
4. Production OpenSandbox 使用 gVisor、Kata、微虚拟机还是独立集群。
5. 外部身份提供商、MFA 和企业级 SSO 的第一版范围。
