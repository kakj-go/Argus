# Task 02：Tool Discovery 与统一 Tool Gateway

## 目标

用 `tool.search`、`tool.describe`、`tool.invoke` 三个稳定元工具替换完整业务 Tool Schema 注入。所有 Native Tool、未来 Remote MCP 和 Sandbox stdio MCP 都通过同一个 Catalog、权限门禁、Schema 校验、执行预算和结果投影边界。

## 交付内容

### P5-T01：版本化分类契约

- [ ] 建立 `ToolCategory` 版本化枚举，第一批包含 `host/k8s/metric/trace/log/connector/workflow`。
- [ ] 分类是业务领域，不携带执行器、租户、Endpoint 或风险语义。
- [ ] 新分类必须更新 JSON Schema、Go/TypeScript 生成类型、文档和契约测试。
- [ ] 未知分类稳定返回 `TOOL_CATEGORY_UNKNOWN`，不回退到全局模糊检索。
- [ ] 同名工具可以存在于不同分类，但同一分类内名称唯一。

### P5-T02：Tool Manifest 与 Catalog

- [ ] 定义代码内 `ToolManifest`：分类、名称、版本、标题、描述、关键词、Schema、风险、权限、执行器、Projection 和 Presentation。
- [ ] Catalog 启动时校验名称规范、分类、版本、Schema、重复项、执行器和模板资产。
- [ ] Catalog Revision 根据规范化 Manifest 集合确定性计算。
- [ ] Commit Tool 注册到内部隐藏目录，永不进入模型 Search/Describe。
- [ ] Platform/Enterprise 启用状态、用户权限和 Capability Snapshot 作为查询投影，不修改原始 Manifest。
- [ ] Catalog 不把 Endpoint、stdio 命令、镜像、环境变量或 Secret 暴露给 Agent。

### P5-T03：`tool.search`

- [ ] 实现必填 `category`，可选 `query/limit/cursor` 的固定输入 Schema。
- [ ] 在名称、标题、描述和关键词上执行确定性相关性排序。
- [ ] 搜索前应用企业、用户、ServiceAccount、Tool Visibility 和 Capability Snapshot 过滤。
- [ ] 默认 10 项，最大 20 项；返回稳定 Cursor 和 Catalog Revision。
- [ ] 结果只含名称、标题、短摘要、风险、是否需要确认和版本，不含完整 Schema。
- [ ] 同一输入和权限/能力快照产生相同规范化结果与 Projection Hash。
- [ ] Catalog 即使有 500～1000 个 Tool，Search 输出仍受固定上限约束。

### P5-T04：`tool.describe`

- [ ] 输入必须同时包含 `category + name`。
- [ ] 返回完整描述、Input Schema、Output 语义、风险、权限前件、PendingAction 语义、版本和少量示例。
- [ ] 不返回 Template Source、Executor 地址、MCP Server 配置或 Secret。
- [ ] 对无权发现和实际不存在的 Tool 使用同一 `TOOL_NOT_FOUND`，避免存在性泄漏。
- [ ] 描述内容按 Manifest Version 缓存；权限和 Capability 改变时重新投影。
- [ ] Schema 输出使用规范化字段顺序，避免无意义变化破坏 Prompt Cache。

### P5-T05：`tool.invoke`

- [ ] 输入固定为 `category + name + arguments`，不允许模型指定版本、执行器、Endpoint 或 Profile。
- [ ] Gateway 在执行前重新解析 Manifest，并验证分类、名称、启用状态、权限、资源授权、Schema、风险和预算。
- [ ] Gateway 注入可信 `enterprise_id/user_id/run_id/tool_call_id/invocation_id/authorization_version`，忽略模型同名字段。
- [ ] 只读 Tool 根据 Manifest 执行；变更 Tool 只能调用 Preview，Commit 目录对模型隐藏。
- [ ] 执行超时、取消、并发和重试策略由 Manifest 风险级别与执行器共同约束。
- [ ] Tool 执行完成后统一进入 Result Projector，不允许 Handler 直接写模型消息或浏览器 SSE。
- [ ] 幂等键绑定真实 Tool 身份、规范化参数、调用主体和 Run，不使用模型自由文本。
- [ ] Agent Policy 要求首次调用某 Tool Version 前先 Describe；Gateway 仍独立验证，不把 Describe 历史当作授权票据。

### P5-T06：执行器接口

统一接口至少包含：

```go
type Executor interface {
    Invoke(context.Context, Invocation) (RawResult, error)
}
```

- [ ] `NativeExecutor` 迁移现有 Go Tool。
- [ ] 预留 `RemoteMCPExecutor`，Task 04 实现传输。
- [ ] 预留 `SandboxBuiltinExecutor` 和 `SandboxStdioMCPExecutor`，Task 04 实现执行。
- [ ] 执行器只返回 Raw Result 和受控元数据，不负责构造模型消息或操作 DOM。
- [ ] 所有上游错误先规范化为 Argus 稳定错误，再做受众投影。

### P5-T07：Result Projector

- [ ] 保留现有 4 MiB 完整结果和 64 KiB 模型投影基线。
- [ ] 每个 Tool 使用版本化确定性 Projection；大结果保存 Artifact 并返回 `result_ref`。
- [ ] 模型投影包含摘要、样本、统计、资源引用、Partial 和稳定错误。
- [ ] Presentation Projection 作为独立受众对象交给 Task 03，不进入 Agent Context。
- [ ] 原始 Tool Result、模型投影和 Presentation 使用同一 `tool_call_id`、Tool Version 和 Authorization Snapshot。

### P5-T08：现有业务 Tool 迁移

- [ ] Host Tool 迁移到 `host`。
- [ ] Kubernetes Tool 迁移到 `k8s`。
- [ ] Metrics Tool 迁移到 `metric`。
- [ ] Logs Tool 迁移到 `log`。
- [ ] Trace Tool 迁移到 `trace`。
- [ ] Connector Tool 迁移到 `connector`。
- [ ] PendingAction 查询和取消迁移到 `workflow`；Commit Tool 保留内部隐藏。
- [ ] 删除每个 Tool 面向模型的直接注册，业务 Tool 只能由三个元工具访问。

### P5-T09：后续 Skill 接口

- [ ] 定义只读 `SkillManifest` 和 `ActiveSkillContext`，包含名称、版本、描述、Instruction Hash 和允许分类提示。
- [ ] Skill 只通过显式产品命令/受信入口激活，当前不建设 Marketplace、远程安装或模糊自动选择。
- [ ] 激活 Skill 不向模型增加 Tool Schema，只向 ContextAssembler 增加一个版本化 Instruction Block。
- [ ] Skill 只能指导三个元工具的使用；可执行代码必须注册为 Tool，界面必须由目标 Tool Template 提供。
- [ ] Skill 不能扩大 Tool Visibility、权限、预算、Sandbox Capability 或 Commit 可见性。
- [ ] 删除 Card Render Skill 后，不得以通用 Skill 名义重新建立模板选择逻辑。

## 权限与缓存

Search/Describe 缓存键至少包含：

```text
catalog_revision
enterprise_id
principal_id
authorization_version
capability_snapshot_hash
category
normalized_query_or_tool_name
```

缓存只保存已裁剪公开投影；权限变化递增 AuthorizationVersion 后旧缓存不可继续使用。Redis 清空只导致重新计算，不影响 Catalog 权威定义。

## 测试

- [ ] 分类必填、未知分类、非法名称和分页边界契约测试。
- [ ] 500/1000 Tool Catalog 的 Search 正确性、确定性和固定输出大小测试。
- [ ] 无权限 Tool 在 Search、Describe 和 Invoke 均不可见。
- [ ] Describe 后权限撤销，Invoke 仍然拒绝。
- [ ] 模型伪造 executor、run_id、enterprise_id、version 和 Profile 不生效。
- [ ] Commit Tool 无法通过 Search、Describe、Invoke 或名称猜测访问。
- [ ] Native Tool 原始结果、模型投影、Presentation Ref 共享同一 ToolCall ID。
- [ ] 同一 Manifest 集合顺序变化不改变 Catalog Revision。
- [ ] 大日志、指标、Pod 列表先确定性投影，完整结果可按 `result_ref` 获取。
- [ ] Redis 清空、Worker 重启和 Catalog Reload 不改变权限语义。
- [ ] 激活 Skill 只改变 Instruction Context，不改变核心 Tool Schema、Catalog 权限或执行器。

## 完成标准

1. 模型请求不再包含任何业务 Tool 的完整 Schema。
2. 所有正式业务 Tool 只能通过 Search → Describe → Invoke 路径调用。
3. Catalog 分类和 Executor 完全正交。
4. Tool Gateway 成为权限、Schema、预算、幂等和结果投影的唯一入口。
5. Commit Tool 对模型不可发现、不可描述、不可调用。
6. Catalog 扩容不会线性增加初始模型 Tool Schema Token。
