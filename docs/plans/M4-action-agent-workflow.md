# M4：确定性执行、Agent 与 Tool 闭环

## 目标

交付可恢复、可审批、不可被模型或浏览器绕过的查询与变更执行链路，并让 Chatbox 使用真实 Model/Tool/Run。

## 前置条件

- M2 授权闭环完成。
- M3 已提供 Host/Kubernetes 查询、资源管理专用 PendingAction/Plan/Token 和可恢复的 Connector/Direct 执行基座。

## 已冻结实现边界

- `argus-worker` 使用 `agent`、`action`、`compaction`、`automation`、`sandbox` 五个独立 PostgreSQL Task Queue 和 Processor；Evaluation 由一个 `--pool=default` Deployment 同时运行五类任务，Local Hardening/Production 使用五个拆分 Deployment。PostgreSQL Task Lease/Fence 是唯一领取事实，Redis 只负责唤醒。
- Tool Gateway 在 M4 内实现为 Worker 进程内可信 Registry，统一执行 Tool 权限、ServiceAccount `allowed_tool_ids` 和严格 Input Schema 校验。`.commit` 只接受 `action_executor` 身份；未来若拆成独立服务，必须使用内部 mTLS 且不得经公共 Ingress 暴露。
- 首批模型可见 Catalog 已冻结 Host、Connector、PendingAction 和 Kubernetes Cluster/Namespace/Node/Pod/Deployment/StatefulSet/DaemonSet/Service/Pod Logs 查询，以及 Host/Kubernetes create/update/delete Preview；对应 Commit 只存在于隐藏 Action Catalog。
- ToolResult 完整内容以 Artifact 保存；列表投影最多 50 项、Pod Logs 最多 32 KiB、模型投影总量最多 64 KiB，并保存稳定 Projection Hash、`projected_bytes`、资源引用和公开 `result_ref`。
- Agent Loop 注入的可信 `run_id` 贯穿 PendingAction 与 Execution；Preview 使原 Run 进入 `waiting_input`，Commit 完成后 Verify Task 只能恢复同一 Run。公开 Run 返回稳定 `stop_reason` 和 `error_code`。
- Replay Model Provider 和集群内测试模型地址只存在于 `m4e2e` build tag；生产镜像构建与扫描会拒绝该实现。
- Action Executor 异步产生的 Connector Enrollment 安装命令使用 PostgreSQL AES-GCM 密文保存，并通过 Execution 一次性结果接口由原发起人领取。
- Automation 每次创建或更新都生成不可变 AutomationRevision，AutomationRun 固定绑定触发时 Revision；同时保存 ServiceAccount AuthorizationVersion 快照，并在每次运行、Preview、审批和 Commit 前重查。写操作无启用审批策略时直接拒绝。
- `ModelCall` 是模型调用、价格、Token、停止原因和 Projection Hash 的权威事实；`ModelUsage` 只由 `ModelCall` 聚合查询，不维护第二张可写事实表。
- Sandbox Session 绑定 Task、Profile Revision、TTL 和企业配额；上游创建响应丢失时按 `argus.task_id` 对账，终止结算在行锁事务中只发生一次。

## 任务

- [x] `M4-RUNTIME-01` 实现 Task/Outbox/Lease/Fence、Worker 领取、恢复和重复副作用防护。
- [x] `M4-RUN-01` 实现不可变 ConversationEvent、Message、Run、Step、ToolCall、ToolResult 和等待状态。
- [x] `M4-AGENT-01` 实现 Provider-neutral 单 Agent Loop、AgentEvent、Run Reducer、流式持久化和稳定停止原因。
- [x] `M4-CONTEXT-01` 实现 ContextAssembler：System/Tool Catalog + Typed RunCheckpoint + Active ContextSnapshot + Recent Tail。
- [x] `M4-CONTEXT-02` 实现 ToolResultProjection，主机/Pod/日志/Metrics 大结果完整保存并向模型提供安全摘要和 `result_ref`。
- [x] `M4-COMPACT-01` 实现 Token 估算、软/硬阈值、合法 Turn 切点、增量 ContextSnapshot、Source Hash 和 Compaction 计费。
- [x] `M4-COMPACT-02` 实现压缩失败、Worker 重启、重复 Source Range 和最后有效 Snapshot 的幂等恢复。
- [x] `M4-MODEL-01` 实现 AIModel、revision、健康检查、调用价格快照和部门/用户额度。
- [x] `M4-SANDBOX-01` 在 Agent 接入前补齐 OpenSandbox 服务连接、镜像、Profile、配额和活动会话治理 API；继续由 Helm 管理部署，不回填到 Setup 向导。
- [x] `M4-TOOL-01` 实现 Tool Registry、权限投影、查询 Tool 和 Tool Result 安全投影。
- [x] `M4-TOOL-02` 默认顺序执行 Tool，仅允许显式 `parallel_safe` 的无副作用查询并行；截断/不完整 ToolCall 不执行。
- [x] `M4-ACTION-01` 扩展 M3 PendingAction 公共/内部存储和私有 Token 分流，增加 `.preview/.commit` Tool 配对与通用状态迁移；不得重建或并行维护第二套资源 Action。
- [x] `M4-ACTION-02` 实现 UserConfirmation、ApprovalRequest、ActionBinding、Execution 和 Action Executor。
- [x] `M4-AUTH-01` 在 Preview、确认、审批、Commit、恢复时重新检查 DataScope、标签/资源版本和 AuthorizationVersion。
- [x] `M4-IDEMPOTENCY-01` 实现双击、网络重试、服务重启、过期、取消和 ResultUnknown 对账。
- [x] `M4-AUTOMATION-01` 实现绑定 ServiceAccount、Tool 和 DataScope 的最小定时 Automation。
- [x] `M4-WEB-01` Chatbox、任务、PendingAction、审批页面切到真实 SSE/API。
- [x] `M4-CONTRACT-01` 建立所有变更 Tool 的自动发布门禁。

## 测试

- 模型 Tool 列表中没有 `.commit`；浏览器/模型/普通 ToolResult 搜索不到私有 Token/参数。
- 双击确认只产生一个 Execution；Commit 响应丢失后查询原 Execution。
- Preview 后撤权、改标签、改资源版本或企业停用，Commit 拒绝并要求重新 Preview。
- Approval 不能补齐基础权限，创建人不能满足非本人审批。
- Worker Pod 删除、Redis 清空和 Lease 过期后 Run 可恢复且危险副作用不重复。
- 大 ToolResult 先确定性投影；多轮压缩不删除原事件，ToolCall/ToolResult 不被拆开，Projection Hash 和 Source Range 可复现。
- ContextSnapshot 搜索不到 Secret、私有 Token、PendingAction 私有参数或 RemoteAccessTicket；摘要不能恢复已撤销 Tool/DataScope。
- 模型以 `length`、`max_tokens` 或 `incomplete` 停止时，即使已经出现部分 ToolCall 也不得执行。
- 多条 Approval Policy 形成独立 Requirement Snapshot，全部满足后才能创建 Execution。
- Agent Preview 创建的 PendingAction、Execution 和 Verify Task 必须保持同一可信 Run 绑定，浏览器和模型不能自报 `run_id`。
- AutomationRun 必须使用自身绑定的不可变 Revision；Automation 更新不能改变已经创建的 Run，审批拒绝或 Execution 终态必须同步收敛 AutomationRun。
- `result_unknown` 对账使用独立 ConnectorCommand 终态事实；未知期间不重放 Host 变更，命令成功后 PendingAction、Execution 和 AutomationRun 一次性收敛。
- 异步 Enrollment 一次性结果验证原发起人、AuthorizationVersion、过期、单次消费和同一 Idempotency-Key 重放。
- Sandbox 同时校验并发配额与月度秒数配额；重复终止或恢复对账不会重复结算 Usage。
- 用户额度耗尽时公开 Run 必须返回 `stop_reason=quota_exceeded` 和 `error_code=MODEL_QUOTA_EXCEEDED`。

## 自动化证据

- `make contract-check contract-breaking`
- `go test ./...`、`go vet ./...`
- `pnpm typecheck lint test build check:bundle check:real-build e2e`
- `make check-production-artifacts`
- `make e2e-m4-k8s`

只有上述门禁和临时 Namespace 流程全部通过后，任务复选框才可标记完成。

截至 2026-08-17，上述门禁已全部通过。最终 `make e2e-m4-k8s` 成功运行号为 `20260817144832-31660`，脱敏证据位于 `artifacts/m4-e2e/20260817144832-31660`；测试覆盖双策略审批、可信 Run 绑定、AutomationRevision、ResultUnknown 不重放、模型额度耗尽、Sandbox 配额、Redis 停止与 Server 恢复和 real Playwright，结束后临时 Namespace、PVC 和 Lease 已清理。

## 退出标准

- 用户可在 Chatbox 查询资源并完成至少一个真实变更的 Preview → Confirm/Approval → Commit → Verify。
- 全链路有稳定状态、错误、审计和恢复语义。
- 长会话可以在模型上下文限制内持续运行，Compaction 成本可计量，失败可恢复且不静默丢失历史。
- Automation 使用当前 ServiceAccount 权限且不能消费人工会话票据。
- Agent/Worker 只能通过已治理的 Sandbox Profile 创建运行环境；real 模式平台 Sandbox 页面不再返回 `CLIENT_OPERATION_UNAVAILABLE`。

## 不包含

- 企业自定义 Card 发布。
- 人工终端和完整遥测。
- 通用子 Agent、跨 Conversation 长期记忆和自动选择另一个压缩模型。
