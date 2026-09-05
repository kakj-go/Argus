# Task 06：端到端验收与文档收口

## 目标

用单元、契约、浏览器和临时 Kubernetes Namespace E2E 证明 PlanV5 的完整链路，并把主文档从旧 Card/全量 Tool/OpenSandbox 强依赖架构更新为新基线。完成后 PlanV5 不再只是增量计划，而是项目当前权威设计。

## 测试矩阵

### P5-E01：Agent 与 Context

- [ ] 无 Tool 的普通对话。
- [ ] Search → Describe → Invoke 单轮和多轮调用。
- [ ] 一个 Turn 内多个元工具调用的顺序和原生 ToolCall/ToolResult 配对。
- [ ] 两个以上 Run 的 Conversation 历史连续恢复。
- [ ] Soft/Hard Compaction、多次增量摘要、Worker 重启和 Redis 清空。
- [ ] Tool Result Projection 后再压缩，Template Source 永不进入模型输入或摘要。
- [ ] Chat Completions 与 Responses 两种协议一致。
- [ ] Catalog 500/1000 Tool 时核心 Tool Schema Token 保持常数级。
- [ ] 显式激活一个 Skill 只增加该 Skill Instruction，不加载全量 Skill Catalog，也不改变核心 Tool Schema。

### P5-E02：Tool Discovery 与权限

- [ ] 七个分类的检索、详情、调用和分页。
- [ ] 未知分类、未知 Tool、非法参数和超预算。
- [ ] RBAC、ServiceAccount Tool Allowlist 和 explicit resource authorization。
- [ ] Describe 后撤权、资源版本变化和 AuthorizationVersion 变化。
- [ ] Commit Tool 通过搜索、详情、调用和猜名均不可访问。
- [ ] Catalog Reload、Manifest 顺序变化和 Redis 清空保持确定性。
- [ ] Native、Remote MCP、Sandbox Builtin 和 stdio MCP 使用同一 Gateway 门禁。

### P5-E03：Template Runtime

- [ ] Host、Kubernetes、Metric、Log、Trace、Connector 和 Preview 模板真实渲染。
- [ ] Light/Dark、中文/英文、空、错误、Partial 和大数据。
- [ ] Template Hash/Version、历史刷新、缓存命中与 Artifact 缺失。
- [ ] 错误 Origin/Nonce/Sequence、重复/乱序/超大消息和销毁后消息。
- [ ] DOM、Cookie、Storage、Network、任意 API 和任意 Tool 逃逸全部失败。
- [ ] PendingAction 确认、取消、双击、过期、审批、执行成功/失败/ResultUnknown。
- [ ] 浏览器和模型输入中搜索不到私有 Token、冻结参数和生产凭据。

### P5-E04：OpenSandbox 降级

至少运行两套安装矩阵：

| 模式            | OpenSandbox | 预期                                                               |
| --------------- | ----------- | ------------------------------------------------------------------ |
| `agent-lite`    | 未配置      | Agent、三个元工具、Native Tool 正常；四基础工具和 stdio MCP 不可见 |
| `agent-sandbox` | 配置且健康  | 七工具可见；Workspace 文件和 stdio MCP 正常                        |

故障注入：

- [ ] Sandbox Backend 启动前不可达。
- [ ] Agent 运行中 Sandbox API 中断。
- [ ] Profile 删除/停用和企业配额耗尽。
- [ ] Workspace 过期、Supervisor 崩溃和 Worker Pod 删除。
- [ ] stdio MCP 非法 stdout、Hang、崩溃、超大消息和取消。
- [ ] 所有故障都不触发 Worker 宿主执行 fallback。
- [ ] Sandbox 故障不使普通 Agent/Native Tool Readiness 失败。

### P5-E05：Remote MCP Adapter

- [ ] Streamable HTTP JSON 响应。
- [ ] Streamable HTTP SSE 响应和断线取消。
- [ ] Session ID、重连、超时和服务端通知。
- [ ] TLS、认证、非法 Origin、DNS Rebinding、SSRF 和私网地址策略。
- [ ] 明确开启时的旧 HTTP+SSE 兼容；默认不自动降低安全策略。
- [ ] Remote Result 的模型/Presentation 双投影和大小限制。

## Kubernetes E2E

### P5-E06：临时 Namespace 流程

按照项目约束使用一次性 Namespace：

```text
doctor capability check
→ acquire global e2e lease
→ optionally scale down normal test services
→ create unique namespace
→ install agent-lite profile and run suite
→ upgrade/reinstall agent-sandbox profile
→ run sandbox/mcp/fault suite
→ collect sanitized evidence
→ delete sandbox workspaces/processes/artifacts
→ uninstall release and delete namespace/PVC/lease
→ verify zero residue
```

- [ ] 测试前检查 Kubernetes Context、RuntimeClass、StorageClass、磁盘和镜像架构。
- [ ] 只有本次测试拥有的普通测试服务可以暂停；不得改动未知或生产 Namespace。
- [ ] Cleanup 在成功、失败、超时和中断路径都运行。
- [ ] 证据只保存 Hash、状态、计数和脱敏截图，不保存 Secret、文件正文或私有 Tool Result。
- [ ] 最终扫描 Namespace、PVC、Cluster RBAC、Sandbox Session、进程、Artifact Fixture 和 Lease 零残留。

## 性能与容量

### P5-E07：预算门禁

- [ ] 初始三工具和七工具的 Schema Token 建立固定基线。
- [ ] 500/1000 Tool Catalog 不增加初始 Tool Schema Token，只影响 Search Index 内存和 Search 延迟。
- [ ] Search P95、Describe P95、Native Invoke P95、Sandbox 冷/热启动和 Template 首帧建立预算。
- [ ] Template 256 KiB、Presentation 1 MiB、Model Projection 64 KiB 和完整结果 4 MiB 上限全部有边界测试。
- [ ] SSE 背压、浏览器慢消费和 Tool Result Artifact 路径有容量测试。
- [ ] Sandbox Workspace/进程配额和 MCP 并发有企业隔离测试。

## 文档更新

### P5-E08：权威文档收口

- [ ] `docs/README.md`：术语和决策改为 Tool Discovery、Tool Presentation、可选 Sandbox。
- [ ] `docs/00-decisions-and-invariants.md`：删除 Card Skill/Render Plan，全量 Tool 注入和 OpenSandbox 硬依赖不变量。
- [ ] `docs/01-product-and-architecture.md`：更新总体图、服务职责和依赖矩阵。
- [ ] `docs/04-agent-mcp-and-action-workflow.md`：改写三个元工具、双投影和 Preview Template 流程。
- [ ] `docs/05-interactive-cards-and-interactive-ui.md`：删除旧内容或重命名为 Tool Presentation Runtime。
- [ ] `docs/06-security-and-mvp-roadmap.md`：更新模板、Sandbox 和 MCP 威胁模型。
- [ ] `docs/08-model-and-sandbox-management.md`：增加 Agent Workspace/Profile 和可选能力语义。
- [ ] `docs/10-service-components-and-kubernetes-deployment.md`：增加 `agent-lite/agent-sandbox` 安装矩阵和 Readiness。
- [ ] `docs/12-technology-stack-and-code-structure.md`：更新包结构，删除 Card 领域和组件库。
- [ ] `docs/13-current-implementation-and-kubernetes-rollout.md`：更新实现盘点和测试证据。
- [ ] `docs/15-end-to-end-implementation-plan.md`：加入 PlanV5 E2E 与零残留门禁。
- [ ] `docs/16-agent-harness-and-context-management.md`：按小内核、原生 Tool Message 和跨 Run Context 重写。
- [ ] M4/M5 阶段文档标明被 PlanV5 哪些决策替换，避免继续作为运行时基线。

### P5-E09：契约与运维文档

- [ ] OpenAPI/JSON Schema 记录元工具输入、Tool Result Envelope、Template Runtime 和稳定错误。
- [ ] 平台运维手册说明 OpenSandbox 未配置、故障、Profile/配额和 MCP 诊断。
- [ ] Tool 开发手册说明 Manifest、Projection、Template、风险和发布门禁。
- [ ] Template 开发手册说明 CSP、Design Tokens、Bridge、大小和可访问性。
- [ ] MCP 接入手册区分分类与传输，并明确 stdio/Remote 的安全边界。
- [ ] Skill 接入手册说明显式激活、Context Hash、三个元工具复用以及“可执行逻辑必须成为 Tool”。
- [ ] 迁移记录明确旧 Card 数据和 API 不兼容且不提供转换。

## 最终删除检查

- [ ] 正式代码不存在 `card.render`。
- [ ] Agent 无业务 Tool 关键词选择器。
- [ ] 模型请求不包含业务 Tool 完整 Catalog。
- [ ] 企业门户不存在 Card 创建、内置卡片、自定义卡片和 Binding 页面。
- [ ] 数据库不存在 Card Catalog/Version/Instance/Presentation/Slot/Binding。
- [ ] Worker 宿主不执行 `bash/read/write/edit` 或 stdio MCP。
- [ ] OpenSandbox 未配置时 Agent 仍通过完整 `agent-lite` E2E。
- [ ] 主文档不再把旧 Card 架构描述为当前基线。

## 完成标准

1. 单元、契约、数据库集成、前端 Playwright 和 Kubernetes E2E 全部通过。
2. `agent-lite` 与 `agent-sandbox` 两种模式均有真实证据和零残留清理记录。
3. Tool Catalog 规模测试证明模型初始 Schema Token 不随业务 Tool 数量线性增长。
4. Template 与 Sandbox 安全逃逸测试全部 fail closed。
5. Card 领域已删除，PendingAction 安全闭环保持完整。
6. 主文档全部切换到 PlanV5 基线，不存在互相冲突的当前架构说明。
