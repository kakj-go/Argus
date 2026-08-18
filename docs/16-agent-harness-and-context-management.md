# Agent Harness 与上下文管理

## 1. 目标与模板选择

Argus 第一版 Agent Harness 采用小内核设计，核心循环参考 Pi `agent-core`，上下文压缩参考 Pi Coding Agent 的 Compaction 模块，但不复制完整 Coding Agent、终端 UI、文件追踪或 Session Tree。

参考范围：

- Agent Loop：模型流式响应、ToolCall、ToolResult、下一轮推理和事件输出。
- Context Hook：在调用 ModelProvider 前独立组装、裁剪和压缩上下文。
- Compaction：按 Token 预算触发、保留最近内容、在合法 Turn 边界切分并增量更新摘要。
- Provider Adapter：内部使用统一 Message/Event，调用模型时才转换成供应商格式。

Argus 不复用 Pi 的内存 Session 作为事实来源。Conversation、Run、Step、ToolCall、ToolResult、ContextSnapshot、PendingAction、Execution 和 ModelCall 的权威状态全部保存在 PostgreSQL；Redis 只用于通知和短期加速。ModelUsage 是从 ModelCall 聚合出的查询投影，不是另一份可写事实。

第一版只实现单 Model Agent。Presentation/Card Render 作为同一 Run 内受限的声明式步骤实现，不启动拥有独立权限的子 Agent；通用子 Agent 调度延后。

## 2. 核心边界

Agent Harness 只负责：

- 根据持久化 RunState 组装当前模型上下文。
- 调用固定 `model_id + model_revision`。
- 流式接收模型输出并生成持久化 AgentEvent。
- 校验和调度当前用户可发现的 Tool。
- 强制校验 Tool 权限、ServiceAccount `allowed_tool_ids`、输入 Schema、执行模式和结果大小。
- 根据 ToolResult 继续推理或进入等待/结束状态。
- 在达到上下文预算时生成 ContextSnapshot。

Agent Harness 不负责：

- 决定企业、DataScope、Tool、Signal 或字段权限。
- 保存 Secret、`argus__token`、PendingAction 私有参数或人工远程会话票据。
- 直接调用 Commit Tool。
- 把模型摘要当作授权、审批、执行计划或业务状态事实。
- 用内存对话维持唯一 Run 状态。

## 3. 三层上下文模型

Argus 必须分离完整历史、结构化状态和模型投影：

```text
ConversationEvent Ledger
    完整、不可变、可审计的会话与 Tool 事件
              ↓
RunState / RunCheckpoint
    当前目标、步骤、目标资源、等待事项和执行状态
              ↓
ModelContextProjection
    本轮实际发送给模型的最小安全上下文
```

### 3.1 ConversationEvent Ledger

至少记录：

```text
user_message
assistant_message
model_usage
tool_call_requested
tool_call_started
tool_call_result
pending_action_created
user_confirmation
approval_update
execution_update
card_action_result
run_state_changed
context_compacted
```

事件只追加，不因上下文压缩删除或覆盖。大正文可以保存到 Artifact Store，事件保存内容哈希、`result_ref`、安全摘要和数据分级。

### 3.2 RunState 与 RunCheckpoint

RunState 是服务端结构化事实，至少包含：

```text
run_id / conversation_id / enterprise_id
model_id / model_revision / locale
status / current_step_id
stop_reason / error_code
goal / user_constraints
plan / completed_step_ids / next_step
open_questions / waiting_reason
target_resource_refs
active_public_pending_action_refs
tool_call_refs / execution_refs
last_error_codes
authorization_version / resource_scope_snapshot_ref
version
```

其中权限、资源、PendingAction、Execution 和版本字段由服务端领域状态生成；模型可以建议 Goal/Plan/NextStep，但不能修改权限和执行事实。

### 3.3 ModelContextProjection

ModelContextProjection 是一次模型请求的派生对象，不是历史事实。它由 ContextAssembler 生成，包含：

1. 系统指令和固定安全边界。
2. 当前 Locale、模型能力和 Run 预算。
3. 服务端生成的 Typed Run Checkpoint。
4. 最新有效 ContextSnapshot 的 Narrative Summary。
5. 未被压缩的最近完整 Turn。
6. 当前用户输入。
7. 当前权限投影后的 Tool Catalog。

每次请求以 ModelCall 保存 Projection Hash、组成来源、估算/实际 Token、价格快照、模型 Revision、ContextSnapshot ID、延迟和停止原因，便于复现和审计，但不保存 Secret 或私有 Token。

## 4. Agent Loop

第一版循环保持简单：

```text
load RunState
→ assemble safe context
→ reserve quota and call model
→ persist streamed assistant message/events
→ validate tool calls
→ execute allowed tools
→ persist projected tool results
→ continue / wait / finish
```

伪代码：

```go
for run.Status == Running {
    projection := contextAssembler.Build(run)
    response := modelProvider.Stream(run.ModelRevision, projection)
    calls := eventSink.PersistResponse(response)

    if len(calls) == 0 {
        return run.Complete()
    }

    results := toolExecutor.Execute(run, calls)
    eventSink.PersistToolResults(results)
    run = runReducer.Apply(results)
}
```

Agent Loop 不直接写多个领域表。它通过 RunService、ToolGateway、ActionService 和 EventSink 完成条件更新，所有状态迁移带 Run Version/Fence Token。

## 5. Tool 调度

Tool Registry 为每个 Tool 保存：

```text
risk: read | write | dangerous | critical
agent_visibility: visible | hidden
execution_mode: sequential | parallel_safe
result_projection_schema
input/output schema version
required_permissions
```

调度规则：

- 默认 `sequential`。
- 只有无副作用、互不依赖且显式声明 `parallel_safe` 的查询 Tool 可以并行。
- Preview 会创建 PendingAction，默认顺序执行。
- Commit Tool 对 Model Agent 永远 `hidden`，只允许 Action Executor 调用。
- 模型输出被长度限制截断、参数 JSON 不完整或 Schema 校验失败时，不执行 Tool。
- 一轮 ToolCall 数、并行数、总耗时、结果字节和 Token 都受 Run Budget 限制。

## 6. ToolResult Projection

主机列表、Pod、日志、Metrics 和诊断结果通常比自然语言历史更快耗尽上下文，因此必须先进行 ToolResult Projection，再考虑整段会话压缩。

```text
完整 ToolResult
├── PostgreSQL / Artifact Store：完整结果、哈希、来源和权限快照
└── Model Projection：结构化摘要、样本、统计、resource_refs、result_ref、partial
```

优先使用 Tool 自身的版本化 Projection Schema 做确定性裁剪：

- 列表保留总数、过滤条件、关键字段、最多 50 项样本和分页/结果引用。
- Pod Logs 模型投影最多 32 KiB，保留时间范围、代表样本、截断原因和 `result_ref`。
- Metrics 保留聚合、异常区间、缺失/部分数据标记和 Query Ref。
- Secret、凭证、敏感字段和未授权资源在 Projection 前移除。

任一完整 Tool Result 最多 4 MiB 并保存到 Artifact；模型投影总量最多 64 KiB，同时记录稳定 Projection Hash、`projected_bytes` 和 `resource_refs`。相同规范化输入必须得到相同投影与哈希。

只有无法稳定结构化归纳的结果才允许调用模型生成 Narrative Summary。模型生成的摘要必须带来源引用，不能替代完整 ToolResult。

## 7. ContextSnapshot

ContextSnapshot 保存一次可重建的压缩结果：

```text
ContextSnapshot
├── id / enterprise_id / conversation_id / run_id
├── source_from_event_id / source_through_event_id
├── first_kept_event_id
├── typed_checkpoint
├── narrative_summary
├── compaction_model_id / compaction_model_revision
├── compaction_prompt_version
├── estimated_tokens_before / actual_tokens_after
├── source_hash / snapshot_hash
├── status / error_code
└── created_at
```

`typed_checkpoint` 由服务端从 Run、Step、ToolCall、PendingAction 和 Execution 生成；`narrative_summary` 才由模型生成。摘要至少覆盖：

```text
Goal
User Constraints
Current Plan
Completed Work
Current Step
Open Questions
Key Decisions
Important Tool Findings
Target Resource References
Active Public PendingAction References
Recent Errors
Next Steps
```

ContextSnapshot 是派生记录。旧 Snapshot 可以保留用于调试和审计，但任意时刻只有一个 Active Snapshot；原始 Event Ledger 永不删除。

## 8. Token 预算与触发

每个 AIModel Revision 保存或探测：

```text
context_window_tokens
max_output_tokens
supports_tool_calling
supports_structured_output
provider_compaction_capability
```

硬触发条件：

```text
estimated_input_tokens >
context_window_tokens - reserved_output_tokens - safety_margin_tokens
```

同时可以在达到软阈值后，于完整 Turn 结束时提前生成 Snapshot。实现要求：

- 使用 Provider Tokenizer 时优先精确计算；不可用时采用保守估算。
- 按 Token 保留最近内容，不按固定消息条数。
- 切点必须位于完整 Turn 边界，不能拆开 ToolCall/ToolResult、PendingAction 创建/确认或同一执行状态组。
- 最新用户输入、当前等待事项、未完成 ToolCall 和活动 PendingAction 不进入旧历史摘要。
- 单个最新用户输入本身超过预算时返回稳定错误并引导使用文件/Artifact，不能静默截断用户请求。
- System Prompt、Tool Schema、Typed Checkpoint、摘要和最近历史分别记录预算占用。

第一版 Compaction 使用当前 Run 固定的 `model_id + model_revision`，不自动切换廉价模型或 Fallback；压缩调用纳入该用户和部门的 Token/金额结算。

## 9. 压缩流程

```text
estimate context
→ apply deterministic ToolResult Projection
→ choose legal event boundary
→ load previous Active Snapshot（可选）
→ summarize previous summary + newly compacted events
→ rebuild typed checkpoint from PostgreSQL
→ persist new Snapshot
→ mark previous Snapshot superseded
→ assemble new context with recent tail
```

压缩必须增量进行，不能每次重新总结全部历史。新摘要输入由“上一版 Narrative Summary + 上一切点后的待压缩事件”组成，并保留 `first_kept_event_id` 和 Source Hash。

失败策略：

- Compaction 失败时不删除或修改任何原始事件。
- 优先使用最后一个有效 Snapshot 加最近 Tail 重试组装。
- 如果仍超过硬限制，Run 进入可恢复的 `waiting_system`/`failed` 状态并返回 `CONTEXT_COMPACTION_FAILED`，不能静默丢弃关键历史。
- Worker 重启后可以根据 Snapshot 状态和 Source Range 幂等重试；相同 Source Hash 不创建重复 Active Snapshot。

## 10. Provider 原生能力

OpenAI Responses Compaction、Anthropic Server-side Compaction 或 Context Editing 只能实现为 ModelProvider Adapter 的可选优化：

- 不能清空或重写 Argus ConversationEvent Ledger。
- 不能成为跨 Provider 唯一可用的恢复状态。
- Provider 返回的压缩块作为派生内容保存，并记录 Provider、模型和版本。
- 切换模型只影响后续 Run；已经开始的 Run 不因 Provider 能力不同改变事实历史。

默认行为始终是 Argus 生成 Provider-neutral ModelContextProjection。

## 11. 安全不变量

- ContextAssembler 只消费已经通过授权服务和字段脱敏的投影。
- `argus__token`、PendingAction 私有参数、Secret、Credential、RemoteAccessTicket 永不进入 Ledger 正文、Snapshot 或模型上下文。
- Narrative Summary 是不可信派生文本，不能作为 Commit、授权、审批、DataScope 或资源归属输入。
- Tool 名称和 Tool Schema 由服务端 Registry 投影，模型不能通过摘要恢复已经撤销的 Tool。
- AuthorizationVersion、资源版本或标签变化后，下一次 Model Call、Tool Call 和 Run 恢复都重新计算权限。
- Compaction Prompt 必须防止摘要把历史 Tool 输出中的指令升级为 System Instruction。

## 12. 横向扩展与恢复

- Agent Loop 在 `argus-worker` 普通 Pool 运行，无 Pod 本地唯一状态。
- 每次 Model Call 和 Tool Batch 对应可领取 Task，使用 Lease/Fence Token。
- Event、RunState 和 Snapshot 通过 PostgreSQL 条件更新提交，并在同一事务写 Outbox。
- 流式 Token 可以通过 SSE 实时投影；完成消息和状态事实必须落库后才能视为成功。
- Worker 在 Tool 执行成功、结果落库前失联时，先按 ToolCall/Execution 幂等键对账，不能直接重复执行。
- ContextAssembler 必须是确定性组件；给定相同 Run Version、Active Snapshot 和 Event Tail，应产生相同来源集合与 Projection Hash。

## 13. 可观测性与治理

每次 Model Call 和 Compaction 记录：

- Model/Revision、Provider、Run/Step、Prompt/Projection Hash。
- System、Tool Schema、Checkpoint、Summary、Recent Tail 的 Token 占用。
- 输入/输出/缓存 Token、价格快照、延迟和停止原因。
- ToolCall 数、并行度、结果原始字节和投影后字节。
- Compaction 前后 Token、覆盖 Event Range、Snapshot Revision 和失败原因。

治理页面至少能区分正常推理用量、Tool Result 体积和 Compaction 用量，避免上下文压缩成本被隐藏在普通对话统计中。

## 14. 第一版实现边界

第一版实现：

- 单 Agent Loop。
- 持久化 Run/Step/Task/Event。
- 顺序 Tool 和显式 `parallel_safe` 查询 Tool。
- Deterministic ToolResult Projection。
- Typed Run Checkpoint + Narrative Summary + Recent Tail。
- Token 预算、自动/手动 Compaction 和恢复。
- Provider-neutral ContextAssembler。
- OpenAI Compatible `chat_completions` 与 `responses` 双 Adapter，模型 Revision 固定协议且不自动回退。
- `agent`、`action`、`compaction`、`automation`、`sandbox` 五个 PostgreSQL Task Worker Pool。
- Worker 进程内可信 Tool Registry/Gateway；`.commit` 仅注册到隐藏 Catalog 并要求 `action_executor` 身份。
- 首批 Catalog 覆盖 Host、Connector、PendingAction、Kubernetes Cluster/Namespace/Node/Pod/Deployment/StatefulSet/DaemonSet/Service/Pod Logs 查询，以及 Host/Kubernetes create/update/delete Preview；所有输入先通过严格 Schema 和权限门禁。
- Agent 注入可信 `run_id` 并贯穿 PendingAction/Execution；Action 终态只恢复同一 Run 的 Verify Task，浏览器和模型不能自报该关联。
- MCP 内部调用将真实 Agent `run_id` 与可信 `invocation_id` 分开：前者只用于 Agent Run 归属和恢复，后者用于 Tool 调用幂等。Automation 使用 AutomationRun ID 作为 `invocation_id`，不得伪造 Agent Run。
- Automation 使用不可变 Revision，AutomationRun 固定绑定触发时版本，后续编辑不改变已创建 Run。

延后：

- 通用子 Agent、Agent 间消息和动态角色委派。
- 长期用户记忆、跨 Conversation 自动记忆和向量记忆库。
- 自动选择另一个模型执行压缩。
- 允许模型修改 System Prompt、Tool Policy 或 Context Budget。

## 15. 测试门禁

至少覆盖：

1. 无 Tool、单 Tool、多轮 Tool 和 `parallel_safe` Tool 的事件顺序。
2. 模型输出截断或 Tool 参数不完整时不执行 Tool。
3. ToolCall/ToolResult 和 PendingAction 事件不会被压缩切点拆开。
4. 大 Host/Pod/日志/Metrics 结果先确定性投影，完整结果仍可按 `result_ref` 读取。
5. 多轮 Compaction 使用旧摘要增量更新，Source Range 和 `first_kept_event_id` 正确。
6. Typed Checkpoint 与数据库 Run/Execution 状态一致，模型摘要不能覆盖结构化事实。
7. Compaction 失败、Worker 重启和 Redis 清空后原始历史不丢失并可恢复。
8. 上下文、Snapshot、日志和 SSE 搜索不到 Secret、私有参数、Commit Token 或 RemoteAccessTicket。
9. 授权变化后旧 Snapshot 不能恢复已撤销 Tool 或资源权限。
10. 同一输入状态生成稳定 Projection Hash，不同 Provider Adapter 获得等价业务语义。
11. 公开 Run 返回终止 `stop_reason` 和稳定 `error_code`；额度耗尽必须可从 API 观察为 `MODEL_QUOTA_EXCEEDED`。
12. AutomationRevision、AutomationRun、PendingAction 和 Execution 状态一致；ResultUnknown 未获得外部终态前不得重放副作用。

当前实现将 `length`、`max_tokens`、Responses `incomplete`、内容过滤、失败和取消统一视为不可执行 Tool 的停止原因。流式参数即使已部分拼接，也只能在完整 JSON、通过 Tool Schema/领域校验且停止原因允许时进入 Registry。

## 16. 参考

- [Pi Agent Loop](https://github.com/badlogic/pi-mono/blob/main/packages/agent/src/agent-loop.ts)
- [Pi Coding Agent Compaction](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/compaction/compaction.ts)
- [OpenAI Agents SDK Sessions](https://openai.github.io/openai-agents-js/guides/sessions/)
- [Anthropic Context Editing](https://platform.claude.com/docs/en/build-with-claude/context-editing)
