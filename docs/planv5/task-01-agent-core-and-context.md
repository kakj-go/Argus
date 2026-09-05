# Task 01：Agent Core 与上下文重构

## 目标

把现有 Agent Harness 收敛为接近 Pi Agent 的单循环：模型输出原生 ToolCall，Agent 执行核心工具并追加原生 ToolResult，达到预算后在完整 Turn 边界压缩。移除业务 Tool 启发式选择和 Card 特殊分支，为后续三个元工具提供稳定内核。

本 Task 不实现业务 Tool Search，也不实现 OpenSandbox 命令执行；它先固定 Provider-neutral 消息、跨 Run Conversation Context、Capability Snapshot 和 Compaction 边界。

## 当前问题

1. [`internal/agent/loop.go`](../../internal/agent/loop.go) 从完整 Registry 生成 Tool 列表，再使用英文关键词打分选择最多八个 Tool。
2. `loop.context` 生成了 Projection，但模型请求仍使用原 `messages`，Projection 没有成为实际 Provider 输入。
3. 当前会话消息查询以 Run 为边界；同一 Conversation 的后续用户消息创建新 Run 后，历史无法完整拼接。
4. Provider-neutral `Message` 只有普通 Role/Content，ToolCall/ToolResult 被降级成文本。
5. Agent Loop 直接依赖 Card Service，并拥有 Card 创建命令和特殊 Prompt 路径。

## 交付内容

### P5-A01：Provider-neutral 原生消息

- [ ] 将内部消息建模为 `system | user | assistant | tool`，保留 Provider-neutral ToolCall ID、名称、JSON 参数和 ToolResult。
- [ ] OpenAI Compatible `chat_completions` 与 `responses` Adapter 分别映射到供应商原生协议。
- [ ] 流式 Tool 参数只在完成、JSON 合法、Schema 通过且 Stop Reason 允许时执行。
- [ ] 不再把 ToolResult 包装成虚构用户消息，不把 ToolCall 序列化进普通 Assistant 文本。
- [ ] Provider Adapter 对未知内容块 fail closed，并保存安全诊断。

建议内部类型：

```go
type Message struct {
    Role       Role
    Content    []ContentBlock
    ToolCalls  []ToolCall
    ToolCallID string
}
```

内部类型不能直接复用任一 Provider SDK DTO，避免 ContextAssembler 与供应商协议耦合。

### P5-A02：跨 Run Conversation Context

- [ ] 增加按 `enterprise_id + conversation_id + event_id range` 读取 Conversation Event 的查询。
- [ ] ContextAssembler 从最后一个有效 Snapshot 的切点开始读取事件，而不是只读取当前 Run。
- [ ] 当前 Run 的用户输入、Assistant Delta、ToolCall 和 ToolResult 继续保存真实 Run ID，Context 只在读取时跨 Run 聚合。
- [ ] 已取消、失败和超时 Run 的可见消息仍按明确事件规则进入历史；内部错误正文和私有数据不进入模型。
- [ ] 用户确认、审批、Execution 等服务端事实只以公开投影进入 Context。

### P5-A03：小内核 Agent Loop

- [ ] 删除 `selectTurnTools` 和按业务名称/英文关键词维护的默认工具集合。
- [ ] 删除 `Loop.Cards`、Card Command 分支和 `card.render` 特殊处理。
- [ ] Agent Loop 只接收 `CoreToolSet`，不直接依赖业务 Tool Registry。
- [ ] 每轮按顺序完成 `assemble → model → tool batch → append → continue`。
- [ ] 默认顺序执行；只有核心执行器明确证明无副作用且无依赖时才允许并行。
- [ ] 保留 Run Lease/Fence、取消、最大 Turn、额度预留、幂等和恢复机制。
- [ ] 模型没有 ToolCall 时正常完成 Run；模型调用不可执行的 Tool 名称时返回受控 Tool Error，不进入任意 Registry 回退。

### P5-A04：Capability Snapshot

- [ ] 为每个 Run 保存不可变的 `core_tool_schema_revision`、`catalog_revision` 和 Sandbox Capability Snapshot。
- [ ] 始终注入三个元工具的 Schema。
- [ ] 只有 Snapshot 为 `sandbox.ready` 时注入 `read/bash/edit/write`。
- [ ] 同一次流式 ModelCall 中不动态改变 Tool Schema。
- [ ] Run 恢复时重新校验安全状态；若 Sandbox 已失效，旧调用返回受控错误，下一次新 Run 使用新 Snapshot。
- [ ] Snapshot 只记录能力和版本，不保存 Endpoint、API Key、Secret 或 Profile 私密配置。

本 Task 可以先用假的 Sandbox Capability Provider 完成结构，真实 Probe 在 Task 04 接入。

### P5-A05：ContextAssembler 与 Compaction

- [ ] 修正模型请求，确保实际使用 ContextAssembler 输出的消息集合。
- [ ] 预算分别记录 System、核心 Tool Schema、Snapshot、Recent Tail 和当前输入 Token。
- [ ] 先执行确定性 Tool Result Projection，再判断是否压缩历史。
- [ ] Compaction 只在完整 Turn、ToolCall/ToolResult Batch、PendingAction/Execution Event Group 边界切分。
- [ ] 使用“上一摘要 + 新增待压缩事件”增量生成下一摘要。
- [ ] 服务端从 PostgreSQL 生成活动 `action_ref`、Execution 状态、资源引用和稳定错误事实，不依赖模型摘要维持。
- [ ] Template、完整 UI Data、Secret、私有 Token、RemoteAccessTicket 和 Sandbox 凭据不进入 Compaction 输入。
- [ ] 当前显式激活 Skill 的 Version/Hash/Instruction 作为独立上下文块保留，不被摘要改写或从历史 Tool Result 恢复。
- [ ] Compaction 失败保留原始 Event，使用最后有效 Snapshot 重试；仍超限时返回稳定错误。

### P5-A06：持久化与可观测性

- [ ] ModelCall 保存实际 Projection Hash、Event Range、Snapshot ID、Core Tool Schema Revision 和 Capability Snapshot Hash。
- [ ] ToolCall/ToolResult 保存 Provider Tool Call ID 与 Argus Invocation ID 的映射。
- [ ] Conversation SSE 继续投影流式文本，但完成状态只在事务提交后发布。
- [ ] 记录输入/输出/缓存 Token、Compaction Token、工具数量和投影大小。
- [ ] 日志和 Trace 不记录完整 Prompt、文件正文、Template 源码或敏感 Tool Result。

## 代码边界

主要修改范围：

```text
internal/agent/
internal/conversation/
internal/integration/modelprovider/
internal/storage/postgres/queries/runtime.sql
internal/storage/postgres/queries/conversation.sql
internal/runtime/
```

`internal/agent` 不得重新引入对 `internal/card`、具体 Host/Kubernetes/Telemetry Tool 或 OpenSandbox Client 的依赖。

## 测试

- [ ] 无 Tool、单 Tool、连续多 Tool、多轮 Tool 的原生消息顺序测试。
- [ ] Chat Completions 与 Responses Adapter 生成等价内部语义。
- [ ] Tool 参数截断、非法 JSON、内容过滤、取消和未知 Tool 均不执行。
- [ ] 两条用户消息创建两个 Run 后，第二个 Run 可以读取第一 Run 的 Assistant 和 Tool 历史。
- [ ] ContextProjection 确实成为 Provider Request，而不是只用于 Token 统计。
- [ ] Compaction 不拆分 Tool Batch、PendingAction 或 Execution 事件组。
- [ ] Redis 清空和 Worker 重启后可以从 PostgreSQL Snapshot/Event 恢复。
- [ ] Template 标记、私有 Token 和 Secret Fixture 在模型请求与摘要中不可检出。
- [ ] 同一 Event Range、Snapshot 和 Capability Snapshot 生成稳定 Projection Hash。

## 完成标准

1. Agent 模型可见工具不再由业务关键词选择器生成。
2. Provider 收到的是 ContextAssembler 最终投影和原生 Tool 消息。
3. Conversation Context 可以跨 Run 连续恢复。
4. Card 创建和 `card.render` 不再位于 Agent Loop。
5. 三工具/七工具 Capability 结构已经固定，Task 02 和 Task 04 可以只实现对应执行器。
6. 单元、数据库集成和双 Provider Adapter 测试通过。
