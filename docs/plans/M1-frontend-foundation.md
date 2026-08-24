# M1：前端与 API 基础

## 目标

让三个前端应用建立在冻结契约和统一组件边界上，移除会阻碍真实后端接入的安全与结构欠账。

## 前置条件

- M0 的 OpenAPI、JSON Schema、protobuf、错误、PendingAction、Card 和流式契约已冻结，`@argus/api-client/contracts` 可独立编译。

## 任务

- [x] `M1-API-01` 保留 M0 的 `@argus/api-client/contracts` 生成入口，将现有根接口拆为 mock Adapter、显式 real Adapter、HTTP Transport、可恢复 SSE Transport、WebSocket Transport，并通过编译与 Port 形状测试阻止 mock/real 漂移。
- [x] `M1-API-02` Enterprise 与 Platform 必须显式选择 `mock` 或 `real`；未知模式、real 缺少 Base URL、Enterprise real 缺少 Card Origin 均 fail closed，未冻结领域操作稳定返回 `CLIENT_OPERATION_UNAVAILABLE`。
- [x] `M1-CHAT-01` M0 Agent/Stream DTO 直接使用 `snake_case`；`ChatMessage`/`ToolCallTrace` 仅作为 Enterprise 内部派生 ViewModel，旧任务展示模型命名为 `TaskViewModel`，不再与 M0 `Task` 重名；生产 Hook 通过纯 reducer 消费 `StreamEventEnvelope/AgentEvent`，支持顺序去重、AbortSignal、停止原因、PendingAction/Execution 投影和不暴露私有摘要的 Context Compaction 展示。
- [x] `M1-ACTION-01` 公开类型和 localStorage 数据已删除 `PendingAction.params` 与私有 Token/计划字段，mock 私有信息只保存在内部 Action Plan Record。
- [x] `M1-AUTH-01` localStorage 只保存 audience/user/locale/expiry 启动提示；Platform/Enterprise 使用独立 Store 和 Key，路由渲染前必须完成 `auth.me()` 恢复；HTTP Transport 已固定 Cookie、CSRF、Locale 和 Request ID 边界。
- [x] `M1-UI-01` AppShell、PortalUserMenu、AuthStatePage、IconButton、FormDrawer、DataTable 和通用表单反馈统一由 `@argus/ui` 提供，业务应用只保留导航配置和领域组件。
- [x] `M1-STYLE-01` Enterprise 样式按领域拆分，应用自有类统一 `.argus-*`，`check:styles` 禁止硬编码颜色、字号、间距和圆角，所有前端文件低于 2000 行。
- [x] `M1-I18N-01` 两个门户按模块注册 i18n，Platform 初始化模块与其他领域模块一起接受中英文 Key 对称测试。
- [x] `M1-CARD-01` Card Host 使用生成 Manifest/RenderPlan、独立 Card Origin、内容哈希、CSP、一次精确 Origin 握手和 MessagePort 业务通道；Runtime 在 CSP 下执行允许的 Card 脚本并暴露受控 `window.argusCard` API，限制 nonce、序号、大小和 Binding ID。
- [x] `M1-SETUP-01` 首次初始化并入 Platform：启动时先读取初始化状态，未初始化显示向导，成功后进入 `/login`；Platform/Enterprise Audience 继续互斥。
- [x] `M1-BUILD-01` Enterprise/Platform 页面路由懒加载，三个 Vite 门户统一 vendor 分块，`check:bundle` 固化 Chunk 和初始包预算。
- [x] `M1-A11Y-01` `@axe-core/playwright` 已进入 Enterprise、Platform、Card Runtime 三 Origin 核心页面门禁；Radix Dialog/Drawer/Menu 与共享表单覆盖 Escape、焦点恢复、键盘提交和错误语义。
- [x] `M1-TEST-01` Card Provider stderr 已清理；Adapter、SSE、mock 种子、Audience、Labels、Compaction、Card 安全和 real bundle 无 mock seed 均有自动化证据。
- [x] `M1-FORM-01` 三门户全部字段统一使用 `FieldRequirement`；必填红色星号、复合字段 group 语义、Input/Textarea/Select ARIA 和 FormDrawer 原生 submit 由 `@argus/ui` 提供。
- [x] `M1-VALIDATION-01` 写表单统一以 React Hook Form + Zod 提交；对象字段和标量 Schema 约束从 bundled OpenAPI 生成，密码继续使用独立共享契约。
- [x] `M1-ERROR-01` `/api/v1` 启用 bundled OpenAPI 请求校验；安全 `INVALID_ARGUMENT` 可回填字段或显示表单摘要并保留 request ID。
- [x] `M1-GATE-01` AST 门禁阻止缺失 requirement、手写星号、可编辑 `none`、复合字段错误语义和绕过 RHF/Zod 的写提交。

## 测试

- `pnpm typecheck/lint/test/build/e2e` 全部通过。
- `zh-CN/en-US × light/dark` 覆盖两个门户、Platform 初始化流程和 Card Runtime 核心路由。
- Chat SSE 断线恢复不重复 Message/Tool Event，Compaction 事件只展示状态而不暴露摘要私有来源或内部 Prompt。
- 生产构建搜索不到 mock 自动回退和公开 PendingAction 私有字段。
- Card 错误 Origin、伪造消息、销毁后消息和 CSP 逃逸测试通过。

## 退出标准

- 三个应用可在 mock/real Adapter 间显式切换，类型完全一致。
- 通用组件只在 `@argus/ui`，页面级样式均符合约定且单文件低于 2000 行。
- 真实后端接入不需要再次改写前端领域类型。

## 不包含

- 真实身份和业务 API。
- ECharts、xterm.js 和复杂资源表格的提前铺设。

## 完成记录

M1 于 2026-08-16 完成。当前门禁命令为：

- `go run ./cmd/argus-dev contracts check`、`go run ./cmd/argus-dev contracts breaking`、`go test ./...`、`go vet -stdmethods=false ./...`
- `pnpm typecheck`、`pnpm lint`、`pnpm test`、`pnpm build`、`pnpm check:bundle`、`pnpm e2e`
- `pnpm check:real-build`：real 构建中不得出现 mock seed 标记。
- `pnpm check:i18n`：校验两个门户及 Platform 初始化模块的语言包均已集中注册，并扫描静态 `t(...)` 调用，阻止缺失 key 或直接回显 key。
- `pnpm smoke:web`：实际构建 Web 镜像，验证 Nginx 四端口/SPA 深链和 Helm cards Host/Service。
- `pnpm e2e`：Enterprise、Platform 和 Card Runtime 三个 Origin 覆盖浏览器场景；Platform Origin 同时覆盖首次初始化和初始化后登录。mock/portable 场景覆盖表单 required/optional、Setup 邮箱可空、AI Model API Key 条件必填、Secret/Credential、产品流程、四种语言/主题组合和 Card Bridge/CSP。

M1 没有实现真实领域 CRUD Path。M2 只需在已存在的 real Adapter/Transport 上补身份与 IAM Path，不得重新引入 Project、Membership、公开 PendingAction 私有字段或隐式 mock 回退。
