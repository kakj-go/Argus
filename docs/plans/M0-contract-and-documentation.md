# M0：契约与文档冻结

## 目标

在继续业务实现前冻结跨前后端、Worker、Connector、Card 和遥测都会依赖的第一版契约，清除 Project、Membership、通用 Group 和公开私有参数等旧语义。

## 前置条件

- `docs/00` 至 `docs/16` 的架构决策已确认。
- Production PostgreSQL 与 Sandbox Runtime ADR 可以继续开放，但不得阻塞业务契约。

## 已冻结边界

- M0 只交付权威契约源、生成产物、兼容性门禁和文档，不迁移现有手写前端 DTO/Mock；迁移归 M1。
- 公共 REST、SSE 和 JSON Schema 字段统一使用 `snake_case`，REST 前缀为 `/api/v1`。
- OpenAPI 管理浏览器/外部 HTTP DTO；JSON Schema 管理运行时文档；protobuf 管理内部 RPC、Connector 和可信遥测身份。
- `@argus/api-client/contracts` 是独立生成入口，现有 `@argus/api-client` 根接口在 M1 前保持不变。

## 任务

- [x] `M0-ID-01` 固化 Enterprise、PlatformUser、EnterpriseUser、Department、Session、ServiceAccount、APIKey Schema。
- [x] `M0-AUTH-01` 固化 Role、Permission、RoleBinding、DataScope、AuthorizationVersion 和授权决策 Schema。
- [x] `M0-LABEL-01` 固化 `labels: Record<string,string>` 的键值限制、`argus.io/*` 保留规则、标签版本和错误码。
- [x] `M0-LABEL-02` 固化标签选择器 v1，只支持精确、集合、存在/不存在及复杂度上限。
- [x] `M0-ACTION-01` 分离 PendingAction 公共 DTO、内部不可变计划、私有参数和 Token Store。
- [x] `M0-ACTION-02` 固化 Preview/Commit、Approval、Execution、ActionBinding 状态机和错误码。
- [x] `M0-CARD-01` 固化 Card Manifest、RenderPlan、Data/Query/Action Binding 和 Bridge 消息 Schema。
- [x] `M0-API-01` 固化统一错误、分页游标、批量结果、幂等键和 `partial` 语义。
- [x] `M0-STREAM-01` 固化 SSE 事件 envelope、恢复游标、心跳、重连和 AuthorizationVersion 失效语义。
- [x] `M0-STREAM-02` 固化 Connector/Remote Access gRPC Stream 握手和关闭原因。
- [x] `M0-TELEMETRY-01` 固化可信 `EnterpriseId + ResourceId + CollectorId` 和 Query Schema。
- [x] `M0-AGENT-01` 固化 ConversationEvent、Run/Step/Task、AgentEvent、RunCheckpoint 和 ModelCall Schema。
- [x] `M0-CONTEXT-01` 固化 ToolResultProjection、ModelContextProjection、ContextSnapshot、Source Range、Projection Hash 和压缩状态/错误码。
- [x] `M0-CONTEXT-02` 固化模型 Context Window、输出保留、安全余量、Token 估算、合法 Turn 切点和 Compaction 计费契约。
- [x] `M0-TOOL-01` 固化 Tool 的 risk、agent_visibility、execution_mode 和 result_projection_schema 元数据。
- [x] `M0-CONTRACT-01` 建立 OpenAPI、protobuf、JSON Schema lint、代码生成和 Breaking Check。
- [x] `M0-DOC-01` 对新契约严格禁止 `project_id`、`ProjectId`、EnterpriseMembership；现有前端旧引用使用只减不增基线并由 M1 删除。

## 交付证据

- OpenAPI、JSON Schema、protobuf 和语义注册表位于 `api/`，生成 Go/TypeScript 以九个领域包拆分，protobuf 按服务域拆分，产物均可独立编译。
- PendingAction 公共 DTO、生命周期记录、不可变计划记录和私有 Token Record 使用独立权威 Schema；浏览器生成入口不引用后三者。
- `make contract-lint` 校验 OpenAPI、protobuf、Schema Fixture、安全投影、状态机和旧契约基线。
- `make contract-check` 验证代码生成幂等，`make contract-breaking` 对比 `origin/main`；首次合并建立基线。
- 公开 TypeScript 契约不包含 PendingAction 私有计划/Token、ActionBinding 内部记录或 Worker Lease/Fence 字段。

## 测试

- 每个 OpenAPI 原生 DTO、JSON Schema 根节点和 `$defs` 都有自动生成的正常、边界和非法 Fixture；标签、选择器、公共 API、Agent Event、ContextSnapshot、ToolResult Projection、私有字段泄漏和状态迁移另有手写语义 Fixture。
- OpenAPI/protobuf 生成后工作区无漂移。
- Breaking Check 能拒绝删除字段、改变枚举语义和复用错误码。
- 公共 PendingAction、ToolResult、AgentEvent、Stream 和 Card Bridge 动态 JSON 在任意嵌套深度都拒绝 Token、私有参数、Credential 和 RemoteAccessTicket。
- Card Bridge 语义测试覆盖错误版本、Origin、nonce、乱序、重复消息、伪造 Binding ID 和消息大小限制。
- ContextSnapshot 切点不能拆开 ToolCall/ToolResult、PendingAction 或 Execution 事件组，且原始 Event 不被压缩覆盖。
- 旧前端契约基线按文件路径、匹配行内容指纹和次数精确校验；M1 只能删除现有指纹，不能以同文件等量替换绕过门禁。

## 完成审计

截至 2026-08-16，M0 原计划中的契约、生成、Fixture、上下文切点和旧契约门禁均已闭环。首次合并会在 `origin/main` 建立 OpenAPI、JSON Schema、语义注册表和 protobuf breaking baseline；这是基线建立步骤，不是后续里程碑任务。

## 退出标准

- 生成的 Go/TypeScript/protobuf 类型可编译且单文件不超过 2000 行。
- M1 可以让 mock 与未来真实 Adapter 消费同一 `@argus/api-client/contracts` 契约，不再重新定义协议 DTO。
- 所有跨模块开放问题有 ADR 或明确进入后续里程碑。
- 除“第一版不实现”的说明外，规范文档不再把 Project 当成现有对象。

## 不包含

- 领域服务和数据库业务实现。
- Production HA 选型。
- 完整 UI 重构。
