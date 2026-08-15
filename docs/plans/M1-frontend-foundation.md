# M1：前端与 API 基础

## 目标

让三个前端应用建立在冻结契约和统一组件边界上，移除会阻碍真实后端接入的安全与结构欠账。

## 前置条件

- M0 的 OpenAPI、错误、PendingAction、Card 和流式契约已冻结。

## 任务

- [ ] `M1-API-01` 将 `@argus/api-client` 拆为生成契约、mock Adapter、HTTP Adapter、SSE Adapter、WebSocket Adapter。
- [ ] `M1-API-02` 生产模式缺少真实 Adapter 时 fail closed，不再静默回退 mock。
- [ ] `M1-CHAT-01` 使用 M0 生成契约替换手写 ChatMessage/ToolCallTrace，支持 Run、AgentEvent、稳定停止原因、可恢复 SSE 游标和 Context Compaction 状态展示。
- [ ] `M1-ACTION-01` 从公开类型和 localStorage 数据删除 `PendingAction.params` 与所有私有 Token/计划字段。
- [ ] `M1-AUTH-01` 把 localStorage Session 降为非权威启动缓存，接入 Cookie Session/CSRF 接口边界。
- [ ] `M1-UI-01` 盘点 setup/platform/enterprise 平行组件，通用实现迁入 `@argus/ui`。
- [ ] `M1-STYLE-01` 拆分 Enterprise 大样式文件，统一 `.argus-*` 类名和 Design Token，禁止硬编码颜色/字号/间距/圆角。
- [ ] `M1-I18N-01` 按业务模块拆 i18n 并注册，补齐中英文和缺失 Key 检查。
- [ ] `M1-CARD-01` Card Host 改为 Manifest + CSP + MessageChannel/MessagePort，不使用业务 `postMessage('*')`。
- [ ] `M1-SETUP-01` 替换 Setup 登录跳转占位，建立 Setup/Platform/Enterprise Audience 路由守卫。
- [ ] `M1-BUILD-01` 分析并拆分 Setup 大 Chunk，建立 bundle size budget。
- [ ] `M1-A11Y-01` 引入 axe-core 门禁，补齐键盘、焦点、Dialog/Drawer 和表单错误语义。
- [ ] `M1-TEST-01` 清理 Card Provider 测试 stderr，新增 Adapter 契约测试和 mock 种子隔离测试。

## 测试

- `pnpm typecheck/lint/test/build/e2e` 全部通过。
- `zh-CN/en-US × light/dark` 覆盖三个门户核心路由。
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
