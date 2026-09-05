# Task 05：删除 Card Skill 与旧 Card 领域

## 目标

在 Tool 自带模板和 Template Host 已覆盖正式会话路径后，直接删除 Card Skill、Card Catalog、Card 管理、Card Runtime 业务协议和相关数据结构。不保留兼容 API、双写、双渲染或旧 Card 数据迁移。

PendingAction、Approval、Execution、Action Executor 和一次性 Token 不属于 Card 领域，必须保留并继续通过 Tool Preview 模板展示。

## 删除前门禁

- [ ] Agent Loop 已不引用 `internal/card`，也不处理 Card Command。
- [ ] Model Catalog 中不存在 `card.render`。
- [ ] 查询 Tool 至少覆盖 Host、Kubernetes、Metric、Log、Trace 和 Connector 自带模板。
- [ ] Preview Tool 自带模板可以确认/取消公开 `action_ref`。
- [ ] 历史 Tool Presentation 使用新的 Artifact/Envelope 路径。
- [ ] 前端工作台无任何正式路径依赖 Card Catalog/Instance/Binding API。

门禁未满足时只修正新路径，不通过保留旧 Card 分支解决。

## 交付内容

### P5-C01：后端引用盘点

- [ ] 使用静态依赖搜索列出所有 `internal/card`、`card.render`、Card DTO、SQL 和 Runtime Task 引用。
- [ ] 区分可删除的 Card 业务逻辑与可复用的 iframe 安全原语。
- [ ] 确认 PendingAction/Execution 没有数据库外键或运行依赖指向 CardInstance/Presentation。
- [ ] 确认 M5 E2E/Fixture、系统 Catalog Seed 和构建脚本不再由其他阶段调用。
- [ ] 在删除清单中记录生成文件的源契约，禁止只删除生成代码而保留生成入口。

### P5-C02：Agent 与服务端删除

- [ ] 删除 `internal/agent/card_command.go` 及测试。
- [ ] 删除 Loop 的 Card Service 依赖和 App 装配。
- [ ] 删除 `internal/card/` 业务包。
- [ ] 删除 `card.render` Tool、Card Selection、Render Plan 和 Presentation 物化。
- [ ] 删除 Card 系统目录同步、Card Draft 创建/修订、验证和启停任务。
- [ ] 删除 Card HTTP Handler、路由、权限和审计动作。
- [ ] 删除 Card 专用错误码；仍被 Template Runtime 使用的安全错误迁移到新命名空间。

### P5-C03：API 与 Schema 删除

- [ ] 删除 `api/openapi/generation/card.yaml`、`cardapi.yaml` 及引用。
- [ ] 删除 Card Path/Component、`api/schemas/card/*` 和 Card Bridge Rules。
- [ ] 重新生成 OpenAPI Bundles 和 Go/TypeScript Client，确保生成物没有孤立 Card 类型。
- [ ] 删除 Card API Client Adapter、Mock 和权限项。
- [ ] 新 Tool Presentation Schema 只保留 Template Runtime、Result Envelope 和公开动作引用。
- [ ] 契约索引和生成脚本不再期待 Card Bundle。

### P5-C04：PostgreSQL 清理

- [ ] 删除 `internal/storage/postgres/queries/card.sql` 和生成查询。
- [ ] 从权威迁移基线删除 Card Catalog、Version、Demo、Slot、Binding、Instance 和 Presentation 表。
- [ ] 删除 Card Seed、Revision Sync 和相关 Trigger/Constraint/Index。
- [ ] 清理 Conversation/ToolResult 中旧 Card 外键和事件类型。
- [ ] 保留 `pending_actions`、`approvals`、`executions`、私有 Token Record 和审计表。
- [ ] 使用全新数据库运行全部迁移；项目未发布，不提供旧 Card 数据转换或双读。

若现有后续迁移引用 Card 表，必须同步重写开发期迁移链，不能留下创建后立即删除的废弃 Schema。

### P5-C05：前端删除

- [ ] 删除交互卡片设置页、路由、导航和 i18n。
- [ ] 删除会话 `/创建交互卡片` 命令、权限判断和 Draft 入口。
- [ ] 删除自定义/内置卡片列表、Demo、Slot 高亮和 Binding 抽屉。
- [ ] 删除 Card Mock/localStorage Seed 和 Real Card API Adapter。
- [ ] 删除 `sandbox-card-frame` 和 Card Contract，调用点迁移到 `ToolPresentationFrame`。
- [ ] 删除 `@argus/card-host` 中 Slot/Binding/Card 业务协议；可复用安全原语移动到新 Runtime 后删除旧包。
- [ ] 删除 `web/apps/card-runtime` 的 Card 业务构建；新的隔离 Template Runtime 使用独立名称和最小协议。
- [ ] 删除构建产物引用和陈旧测试 Snapshot，不手工维护 `dist` 中的旧 Card Asset。

### P5-C06：部署与配置清理

- [ ] 删除 Card Runtime 专用 Config、Service、Ingress/Origin、Seed Job 和权限。
- [ ] 如果新 Template Runtime 仍需独立 Origin，使用新的明确配置名，不能沿用 Card Catalog 开关语义。
- [ ] CSP、NetworkPolicy 和资源限制按 Template Host 重新生成。
- [ ] 安装器、Doctor 和 Release Manifest 不再检查 Card Catalog/Seed。
- [ ] OpenSandbox 用途描述删除“Card 构建”，因为模板随 Tool 发布，不在 Sandbox 中生成。

### P5-C07：事件与审计收口

- [ ] 删除 `card_action_result`、Card Draft/Enable/Render 等只属于旧系统的事件。
- [ ] 用户确认继续记录为 PendingAction/UserConfirmation/Execution Event。
- [ ] Tool Presentation 加载和 Bridge 拒绝作为技术观测事件，不成为业务状态事实。
- [ ] 审计页面不再显示 Card 管理动作；保留 Tool Invoke、PendingAction 和用户确认审计。

## 明确保留的能力

```text
Tool Preview
PendingAction
Approval Request
Execution
Action Executor
hidden Commit Tool
argus__token private record
one-time result claim
Tool Result Store / Artifact
Conversation Event / SSE
Template iframe security boundary
```

这些能力必须改用 Tool Result Presentation，而不是被误删或降级为前端直接调用 Commit。

## 验证

- [ ] `rg` 搜索不到正式代码中的 `card.render`、InteractiveCard、CardVersion、CardInstance、CardPresentation、SlotBinding 和 Card Draft 命令。
- [ ] OpenAPI、JSON Schema、Go/TypeScript 生成无差异并且不再产出 Card Client。
- [ ] 全新 PostgreSQL Schema 中不存在 Card 表、Index、Trigger 或 Seed。
- [ ] Enterprise 构建和 Playwright 不再请求 Card API。
- [ ] Preview/Confirm/Approval/Execution 全链路继续通过。
- [ ] Template iframe 安全测试继续通过，证明删除的是 Card 业务域而不是隔离边界。
- [ ] Helm Render、离线发布清单和安装 Doctor 不再引用旧 Card 组件。

## 完成标准

1. Card Skill、Card 领域、Card API、Card 数据库对象和 Card 管理 UI 已物理删除。
2. 不存在兼容层、Feature Flag、双写或双渲染。
3. 所有正式富展示均来自 Tool 自带 Template。
4. 所有写操作仍使用后端 PendingAction/Approval/Execution。
5. 干净构建、契约生成、数据库迁移和前端 E2E 通过。
