# Task 03：Tool 自带模板与通用 Template Host

## 目标

让每个 Tool 自己保存展示模板，并在调用结果中返回 `template + presentation data`。前端只保留一个通用、安全、版本化的 Template Host。Agent、Tool Discovery 和 Context Compaction 不感知模板内容。

本 Task 不保留 Card Catalog、模板组件库、Slot Binding、Render Plan 或用户创建模板能力。

## 交付内容

### P5-P01：Tool Result 双受众 Envelope

- [ ] 固化 `model_result` 与 `presentation` 的版本化 JSON Schema。
- [ ] 两个投影共享 `tool_call_id/category/name/version/status`。
- [ ] Agent Event Sink 只把 `model_result` 转为原生 ToolResult Message。
- [ ] Conversation SSE 使用独立 `tool_presentation` 事件发送界面投影。
- [ ] PostgreSQL 保存 Presentation Metadata；Template Source 和大 Data 作为不可变 Artifact 保存一次。
- [ ] 历史消息按当次 Template Hash/Artifact 渲染，不读取 Tool 最新模板替换历史结果。
- [ ] Template Source 在 ModelCall Prompt、Compaction、Assistant Text 和普通 Tool Trace 中不可检出。

### P5-P02：Tool 内模板资产

- [ ] Native Tool Manifest 声明模板 Asset、Runtime Version、Template Version 和预期 Hash。
- [ ] 使用 `go:embed` 或等价构建机制把模板随 Tool 发布物编译。
- [ ] 启动时校验模板大小、编码、Runtime Version、CSP 能力和 Hash。
- [ ] Tool Handler 只产生业务结果；同包 Presentation Builder 将结果转换为 UI Data。
- [ ] 不需要富界面的 Tool 可以没有模板并降级为普通 Tool Trace。
- [ ] Template 未压缩源码上限 256 KiB；禁止运行时下载模板或依赖未锁定远程资源。
- [ ] 前端未来可以按 Hash 缓存模板，但服务端 Tool 仍是唯一来源。
- [ ] 定义 MCP Presentation Extension；MCP Server 必须在自己的 Tool Result 中内联返回模板和 Data，Argus 不提供外部模板绑定。
- [ ] 未返回 Presentation Extension 的 MCP Tool 使用普通文本/结构化 Trace，不影响调用成功。

建议目录：

```text
internal/tools/<category>/<tool>/
├── manifest.go
├── handler.go
├── projector.go
├── presentation.go
├── templates/*.html
└── contract_test.go
```

### P5-P03：Template Runtime Protocol

- [ ] 定义 `argus-template/v1` Host/iframe 消息 Schema。
- [ ] Host 创建独立 Origin 或严格 sandbox iframe，并完成 nonce + `MessageChannel` 握手。
- [ ] 模板只获得 `presentation data`、Locale、Color Scheme、Design Tokens 和允许的公开 Ref。
- [ ] Bridge 消息包含 Version、Sequence、Request ID 和大小限制。
- [ ] 默认 CSP 禁止网络、宿主导航、弹窗、下载、表单外送、Cookie 和存储访问。
- [ ] 禁止 `eval`、动态代码下载和从 Tool Data 构造可执行脚本。
- [ ] iframe 销毁后拒绝迟到消息，错误 Origin、Nonce、Sequence 和 Schema 全部 fail closed。

### P5-P04：前端通用 Host

- [ ] 用一个 `ToolPresentationFrame` 替换业务 Card Frame。
- [ ] Frame 只认识 Runtime Protocol，不包含 metric/log/trace/host 等 Tool 分支。
- [ ] 注入 `@argus/design-tokens` 的颜色、字体、间距、圆角和状态语义，模板禁止硬编码全局主题。
- [ ] 处理 Loading、Runtime Error、Rejected、Collapsed/Expanded 和无模板文本降级状态。
- [ ] 使用 `ResizeObserver` 和受限 Bridge 自动更新高度，设置最大折叠高度。
- [ ] 提供键盘焦点、屏幕阅读器标题、错误说明和 Reduced Motion 基线。
- [ ] 前端正式桌面 Web 验收，不增加移动端特有逻辑。

Host 可以位于共享运行时包，但该包只能包含安全协议和通用壳，不得形成业务模板或组件 Catalog。

### P5-P05：动作 Bridge

- [ ] 第一版只允许 `resize/open_resource/open_result/confirm_action/cancel_action`。
- [ ] `confirm_action/cancel_action` 只携带 `action_ref + request_id`。
- [ ] 宿主使用当前登录身份调用固定 PendingAction API，Template 不获得 API Client 或 Authorization Header。
- [ ] 服务端重新检查动作主体、状态、AuthorizationVersion、资源版本和幂等键。
- [ ] Template 不获得 Commit Tool、`argus__token`、冻结参数、Approval 内部状态或一次性结果正文。
- [ ] 不开放任意 `tools/call`、任意 HTTP Request 或动态 Query Binding。

### P5-P06：首批模板迁移

至少迁移：

- [ ] `host.list/get`：主机摘要和状态表格。
- [ ] `k8s` 查询：工作负载/Pod 状态摘要。
- [ ] `metric` 查询：时间序列和异常区间。
- [ ] `log` 查询：时间、级别和代表日志行。
- [ ] `trace` 查询：调用链和慢 Span 摘要。
- [ ] `connector` 查询：连接状态和版本摘要。
- [ ] 一个变更 Preview Tool：公开计划、风险、影响范围和确认/取消动作。

每个模板的数据结构由同 Tool 的 Presentation Builder 决定，不经过平台字段 Slot 映射。

### P5-P07：历史与缓存

- [ ] Presentation Artifact 内容寻址并记录 SHA-256、Media Type、Runtime Version 和 Tool Version。
- [ ] 同一模板可以跨结果去重存储，但 Tool Result 始终保存明确 Hash/Version 引用。
- [ ] SSE 第一版可以总是内联模板；前端缓存命中优化不得改变结果语义。
- [ ] Artifact 缺失或校验失败时显示安全错误占位，不使用 Tool 当前模板重放旧结果。
- [ ] 企业停用或权限撤销后，历史 Presentation 按现有会话授权重新校验；模板本身不能恢复已撤销数据。

## 安全测试

- [ ] 模板尝试读取 `window.parent.document`、Cookie、localStorage 和 IndexedDB 均失败。
- [ ] 模板尝试 `fetch/WebSocket/EventSource/sendBeacon` 均被 CSP/Host 阻止。
- [ ] 错误 Origin、Nonce、乱序、重复、大消息和销毁后消息被拒绝。
- [ ] Template Data 中的 HTML/脚本字符串不能突破渲染边界。
- [ ] 模板不能自行调用 Tool、Commit、PendingAction 以外的 API。
- [ ] Browser DOM、Network、Console、SSE Fixture 和模型请求中搜索不到私有 Token/参数。
- [ ] Template Hash 不一致、Runtime Version 不支持和超大小时 fail closed。

## 浏览器与 E2E

- [ ] Light/Dark、中文/英文、空数据、错误、部分数据、大数据八类场景。
- [ ] 六类查询模板和一个 Preview 模板真实渲染。
- [ ] 自动高度、折叠/展开、刷新页面和历史回放。
- [ ] Preview Template 确认、取消、双击、过期和审批等待。
- [ ] 权限撤销后旧页面刷新不继续展示已撤销数据。
- [ ] 不存在交互卡片设置页、创建命令或模板选择入口。

## 完成标准

1. 所有模板资产属于具体 Tool 包并随 Tool Result 返回。
2. Agent 请求和上下文中没有 Template Source。
3. 前端只有一个无业务语义的 Template Host。
4. Query 与 Preview Tool 都能通过同一 Envelope 渲染。
5. PendingAction 安全链保持不变，Template 只是展示和公开动作入口。
6. Card Slot、Binding、Render Plan 和模板 Catalog 不再参与任何正式运行路径。
