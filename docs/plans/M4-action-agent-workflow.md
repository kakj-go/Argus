# M4：确定性执行、Agent 与 Tool 闭环

## 目标

交付可恢复、可审批、不可被模型或浏览器绕过的查询与变更执行链路，并让 Chatbox 使用真实 Model/Tool/Run。

## 前置条件

- M2 授权闭环完成。
- M3 至少提供 Host/Kubernetes 查询和一个可安全测试的变更能力。

## 任务

- [ ] `M4-RUNTIME-01` 实现 Task/Outbox/Lease/Fence、Worker 领取、恢复和重复副作用防护。
- [ ] `M4-RUN-01` 实现不可变 ConversationEvent、Message、Run、Step、ToolCall、ToolResult 和等待状态。
- [ ] `M4-AGENT-01` 实现 Provider-neutral 单 Agent Loop、AgentEvent、Run Reducer、流式持久化和稳定停止原因。
- [ ] `M4-CONTEXT-01` 实现 ContextAssembler：System/Tool Catalog + Typed RunCheckpoint + Active ContextSnapshot + Recent Tail。
- [ ] `M4-CONTEXT-02` 实现 ToolResultProjection，主机/Pod/日志/Metrics 大结果完整保存并向模型提供安全摘要和 `result_ref`。
- [ ] `M4-COMPACT-01` 实现 Token 估算、软/硬阈值、合法 Turn 切点、增量 ContextSnapshot、Source Hash 和 Compaction 计费。
- [ ] `M4-COMPACT-02` 实现压缩失败、Worker 重启、重复 Source Range 和最后有效 Snapshot 的幂等恢复。
- [ ] `M4-MODEL-01` 实现 AIModel、revision、健康检查、调用价格快照和部门/用户额度。
- [ ] `M4-SANDBOX-01` 在 Agent 接入前补齐 OpenSandbox 服务连接、镜像、Profile、配额和活动会话治理 API；继续由 Helm 管理部署，不回填到 Setup 向导。
- [ ] `M4-TOOL-01` 实现 Tool Registry、权限投影、查询 Tool 和 Tool Result 安全投影。
- [ ] `M4-TOOL-02` 默认顺序执行 Tool，仅允许显式 `parallel_safe` 的无副作用查询并行；截断/不完整 ToolCall 不执行。
- [ ] `M4-ACTION-01` 实现 `.preview/.commit` 配对校验、PendingAction 公共/内部存储和私有 Token 分流。
- [ ] `M4-ACTION-02` 实现 UserConfirmation、ApprovalRequest、ActionBinding、Execution 和 Action Executor。
- [ ] `M4-AUTH-01` 在 Preview、确认、审批、Commit、恢复时重新检查 DataScope、标签/资源版本和 AuthorizationVersion。
- [ ] `M4-IDEMPOTENCY-01` 实现双击、网络重试、服务重启、过期、取消和 ResultUnknown 对账。
- [ ] `M4-AUTOMATION-01` 实现绑定 ServiceAccount、Tool 和 DataScope 的最小定时 Automation。
- [ ] `M4-WEB-01` Chatbox、任务、PendingAction、审批页面切到真实 SSE/API。
- [ ] `M4-CONTRACT-01` 建立所有变更 Tool 的自动发布门禁。

## 测试

- 模型 Tool 列表中没有 `.commit`；浏览器/模型/普通 ToolResult 搜索不到私有 Token/参数。
- 双击确认只产生一个 Execution；Commit 响应丢失后查询原 Execution。
- Preview 后撤权、改标签、改资源版本或企业停用，Commit 拒绝并要求重新 Preview。
- Approval 不能补齐基础权限，创建人不能满足非本人审批。
- Worker Pod 删除、Redis 清空和 Lease 过期后 Run 可恢复且危险副作用不重复。
- 大 ToolResult 先确定性投影；多轮压缩不删除原事件，ToolCall/ToolResult 不被拆开，Projection Hash 和 Source Range 可复现。
- ContextSnapshot 搜索不到 Secret、私有 Token、PendingAction 私有参数或 RemoteAccessTicket；摘要不能恢复已撤销 Tool/DataScope。

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
