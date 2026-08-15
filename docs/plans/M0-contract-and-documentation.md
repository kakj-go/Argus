# M0：契约与文档冻结

## 目标

在继续业务实现前冻结跨前后端、Worker、Connector、Card 和遥测都会依赖的第一版契约，清除 Project、Membership、通用 Group 和公开私有参数等旧语义。

## 前置条件

- `docs/00` 至 `docs/16` 的架构决策已确认。
- Production PostgreSQL 与 Sandbox Runtime ADR 可以继续开放，但不得阻塞业务契约。

## 任务

- [ ] `M0-ID-01` 固化 Enterprise、PlatformUser、EnterpriseUser、Department、Session、ServiceAccount、APIKey Schema。
- [ ] `M0-AUTH-01` 固化 Role、Permission、RoleBinding、DataScope、AuthorizationVersion 和授权决策 Schema。
- [ ] `M0-LABEL-01` 固化 `labels: Record<string,string>` 的键值限制、`argus.io/*` 保留规则、标签版本和错误码。
- [ ] `M0-LABEL-02` 固化标签选择器 v1，只支持精确、集合、存在/不存在及复杂度上限。
- [ ] `M0-ACTION-01` 分离 PendingAction 公共 DTO、内部不可变计划、私有参数和 Token Store。
- [ ] `M0-ACTION-02` 固化 Preview/Commit、Approval、Execution、ActionBinding 状态机和错误码。
- [ ] `M0-CARD-01` 固化 Card Manifest、RenderPlan、Data/Query/Action Binding 和 Bridge 消息 Schema。
- [ ] `M0-API-01` 固化统一错误、分页游标、批量结果、幂等键和 `partial` 语义。
- [ ] `M0-STREAM-01` 固化 SSE 事件 envelope、恢复游标、心跳、重连和 AuthorizationVersion 失效语义。
- [ ] `M0-STREAM-02` 固化 Connector/Remote Access WebSocket 或 gRPC Stream 握手和关闭原因。
- [ ] `M0-TELEMETRY-01` 固化可信 `EnterpriseId + ResourceId + CollectorId` 和 Query Schema。
- [ ] `M0-AGENT-01` 固化 ConversationEvent、Run/Step/Task、AgentEvent、RunCheckpoint 和 ModelCall Schema。
- [ ] `M0-CONTEXT-01` 固化 ToolResultProjection、ModelContextProjection、ContextSnapshot、Source Range、Projection Hash 和压缩状态/错误码。
- [ ] `M0-CONTEXT-02` 固化模型 Context Window、输出保留、安全余量、Token 估算、合法 Turn 切点和 Compaction 计费契约。
- [ ] `M0-TOOL-01` 固化 Tool 的 risk、agent_visibility、execution_mode 和 result_projection_schema 元数据。
- [ ] `M0-CONTRACT-01` 建立 OpenAPI、protobuf、JSON Schema lint、代码生成和 Breaking Check。
- [ ] `M0-DOC-01` 全量扫描并禁止在第一版契约新增 `project_id`、`ProjectId`、EnterpriseMembership 和通用 Group。

## 测试

- Schema 正反例覆盖标签、选择器、Agent Event、ContextSnapshot、ToolResult Projection、私有字段泄漏和状态迁移。
- OpenAPI/protobuf 生成后工作区无漂移。
- Breaking Check 能拒绝删除字段、改变枚举语义和复用错误码。
- 公共 PendingAction/Tool Result JSON 深度搜索不到 Token 和私有参数。
- ContextSnapshot 切点不能拆开 ToolCall/ToolResult、PendingAction 或 Execution 事件组，且原始 Event 不被压缩覆盖。

## 退出标准

- 生成的 Go/TypeScript 类型可编译。
- mock 与未来真实 Adapter 可以共享同一接口。
- 所有跨模块开放问题有 ADR 或明确进入后续里程碑。
- 除“第一版不实现”的说明外，规范文档不再把 Project 当成现有对象。

## 不包含

- 领域服务和数据库业务实现。
- Production HA 选型。
- 完整 UI 重构。
