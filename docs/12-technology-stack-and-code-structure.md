# 第一版技术栈与代码结构

## 1. 目标

本文固定 Argus 第一版的前端、后端、协议、数据访问、测试和仓库结构基线。技术选型服务于现有架构边界：四个自研服务端程序、统一领域服务、PostgreSQL 权威状态、Connector 控制链路、独立遥测写入/查询链路以及受限 Card Runtime。

第一版先形成一个仓库和一套版本锁定清单。前后端框架、SDK、Operator、Chart 和镜像均固定版本或 Digest，不在构建和安装时解析 `latest`。

## 2. 前端技术栈

Argus 主应用统一使用：

| 范围 | 第一版选择 | 约束 |
| --- | --- | --- |
| 语言与框架 | React + TypeScript | TypeScript 开启 strict；平台门户、企业门户和初始化页共享类型与组件 |
| 构建 | Vite | 产出静态资源，可嵌入 `argus-server` 或由同一 Release 的静态服务器托管 |
| 包管理 | pnpm workspace | 统一锁文件，不允许各应用复制并独立修改同名组件 |
| 路由 | TanStack Router | 路由必须区分 setup、platform 和 enterprise 身份上下文 |
| 服务端状态 | TanStack Query | API 缓存、游标分页、失效和重试；不能作为业务状态事实来源 |
| 本地 UI 状态 | Zustand | 只保存草稿、布局和临时交互状态，不保存权限、Pending Action 或 Run 唯一状态 |
| UI 基础 | Radix UI + Tailwind CSS + CVA | 所有颜色、字号、间距、圆角和状态样式来自统一 Design Token |
| 表格 | TanStack Table + TanStack Virtual | 远程过滤、排序和翻页必须走服务端 Query Binding |
| 表单 | React Hook Form + Zod | 前端校验只改善交互，服务端仍执行最终 Schema 和业务校验 |
| 图表 | Apache ECharts | 用于 Metrics、Trace、拓扑和时间序列；查询必须经过 Telemetry Query |
| 远程命令行 | xterm.js（SSH PTY / WinRM PowerShell Runspace） | 只渲染 Remote Access Gateway 转发的人工会话；WinRM 不伪装成完整 PTY，命令行不得暴露 Credential 或允许 AI/Card 获取会话票据 |
| 国际化 | i18next | 第一版必须完整支持 `zh-CN` 与 `en-US`；文案使用稳定 Key，不得散落在不可检索的组件常量中 |
| 实时更新 | SSE 为主、WebSocket 为辅 | 模型输出、Run 和 Card 状态使用可恢复游标；断线后重新校验 Session、固定企业、DataScope 和 AuthorizationVersion |

不使用 Next.js 作为第一版主框架。Argus 是登录后的控制平面，不依赖 SEO 或服务端页面渲染；Vite 静态构建可以减少运行时和部署复杂度。

引入顺序固定为“核心栈先锁定，功能到达时再启用对应库”：React/TypeScript/Vite/pnpm、Router、Query、Zustand、Radix/Tailwind/CVA 和 i18next 属于前端基座；TanStack Table/Virtual 在资源大列表阶段启用，React Hook Form/Zod 在真实写表单阶段启用，ECharts 在遥测阶段启用，xterm.js 在远程访问阶段启用，MSW 与 axe-core 在真实 API 和可访问性门禁阶段启用。不得为了“技术栈完整”在尚无业务使用点时提前铺空封装。

### 2.1 UI 包边界

```text
web/
├── apps/
│   ├── setup/
│   ├── enterprise/
│   ├── platform/
│   └── card-runtime/
└── packages/
    ├── ui/
    ├── design-tokens/
    ├── api-client/
    ├── auth/
    ├── card-host/
    └── observability/
```

- `setup` 只承载首次初始化。
- `platform` 只承载平台超级管理员能力。
- `enterprise` 包含 Chatbox 和企业管理后台。
- `card-runtime` 只承载独立 Origin 的框架无关 Card iframe 运行时，不接入门户认证状态或业务路由。
- `ui` 是唯一通用组件实现，业务应用不得维护平行组件库。
- `api-client` 由 OpenAPI 生成基础类型，在其上提供领域 Port、mock/real Adapter 以及 HTTP/SSE/WebSocket Transport；客户端上下文不能替代服务端资源归属检查。三个门户必须显式设置 `VITE_API_MODE=mock|real`，未知模式、real 缺少 Base URL 或调用尚未冻结的领域操作都 fail closed，禁止隐式回退 mock。
- `card-host` 只实现 iframe 生命周期、Manifest/RenderPlan 校验、Host Bridge 和受控 Action/Query 调用；`card-runtime` 负责独立 Origin 内的 CSP 和 Card 文档执行，两者共同消费生成的 Bridge 契约。

### 2.2 主题与国际化契约

三个门户共享 `ThemeProvider`、`LocaleProvider`、语义 Design Token 和语言持久化规则，不允许各应用自行维护另一套浅色/深色变量或语言枚举。

主题规则：

- 第一版支持 `light`、`dark`，用户偏好还可以保存为 `system` 并跟随操作系统；实际渲染总会解析为 `light` 或 `dark`。
- 颜色、阴影、边框、文字层级、图表色板和状态色必须引用语义 Token，例如 `--bg-surface`、`--text-primary`、`--danger`，业务组件不得用固定深色背景假装支持主题。
- 用户偏好保存在用户 Profile；登录前或网络不可用时可以使用 `localStorage` 作为启动缓存。服务端 Profile 是跨设备同步的权威值。
- 主题切换不能只改变背景色；组件必须在两种主题下满足对比度、焦点可见性、禁用态和状态“颜色 + 文字/图标”约束。

语言规则：

- 第一版支持 `zh-CN`、`en-US`，默认回退为 `zh-CN`。优先级为用户 Profile → 企业默认语言 → `Accept-Language` → `zh-CN`。
- 路由、导航、表单、校验、空状态、错误、通知、实时事件和 交互卡片 均使用稳定 `message_key` 与参数渲染，不拼接依赖中文语序的句子。
- 资源名、用户输入、代码、日志原文、ID、枚举和 Tool 字段不翻译；日期、数字、单位和相对时间通过 `Intl` 按当前 Locale 与用户时区格式化。
- API 客户端在普通 HTTP、SSE 建连和必要的 WebSocket 握手中发送 `Accept-Language`。服务端返回 `Content-Language`，并在语言不受支持时回退而不是改变业务错误码。
- 后端错误响应使用稳定 `code`、`message_key`、`params` 和可选本地化 `message`。领域层、审计和 Outbox 不能把某一种语言的 `message` 当作唯一事实。
- 模型和 Agent Run 固化会话 Locale；Tool 名称、JSON Schema、枚举和机器可读结果保持语言无关，展示层再按 Locale 生成说明。

国际化资源按门户和领域拆分并接受缺失 Key 检查。新增或修改用户可见功能时，中文与英文资源、浅色与深色状态以及键盘/读屏标签必须在同一个变更中完成。

### 2.3 Card Runtime

主应用使用 React，但 交互卡片 保持框架无关，使用标准 HTML、CSS、JavaScript 和 JSON Schema。Card iframe 不加载主应用 React 上下文、状态容器或 Query Client。

```text
React 主应用
└── Card Host
    └── 独立来源 iframe
        └── HTML/CSS/JavaScript 交互卡片
```

Card Host 根据版本化 Manifest 生成最小 CSP，并使用 `MessageChannel/MessagePort`、通道 nonce、消息序号、Origin 校验和版本化消息 Schema。全局 `window.postMessage` 只允许完成一次受限握手，不得以 `targetOrigin='*'` 传输业务消息。浏览器和 Card 只能获得 `query_binding_id` 或 `action_binding_id`，不能获得 Secret、Commit Tool、PendingAction 私有参数或 `argus__token`。

Card Host 在独立的 `host.context` 消息中传递 `locale`、解析后的 `theme/color_scheme` 和白名单语义 Token。Card iframe 不能读取宿主 DOM 或任意 CSS；语言或主题变化由 Host Bridge 推送新 Context，卡片应原地更新而不是重建业务 Action Binding。

M1 Runtime 对 Card 脚本暴露的唯一浏览器对象是 `window.argusCard`，提供 Binding ID 级 `query/action`、最小 `data/context`、更新订阅和高度回报。独立 Origin iframe 使用 `allow-scripts allow-same-origin`：后者只用于保留 Card 自身 Origin 以支持精确 `targetOrigin`，不得把 Card Runtime 与任一门户部署为同源。

交互卡片 Manifest 必须声明 `schema_version`、入口内容哈希、允许资源、Data/Query/Action Slot、Bridge 能力、`supported_locales`、`default_locale` 和主题能力。系统卡片必须完整支持 `zh-CN/en-US` 与 `light/dark`；企业卡片缺少当前语言时可以回退到声明的默认语言，但宿主必须明确标识回退，不得静默显示错误语义。第一版不存在个人卡片。

## 3. 后端技术栈

| 范围 | 第一版选择 | 说明 |
| --- | --- | --- |
| 语言 | Go | 四个服务端程序、Connector 和 `argusctl` 使用同一工具链并固定 Go 版本 |
| HTTP | `net/http` + `chi` | 保持传输层轻量，领域复杂度不放入 Web 框架 |
| 外部 API | REST + OpenAPI 3.1 | 使用 `oapi-codegen` 生成 Go 类型/接口，使用 `openapi-typescript` 生成 TypeScript 契约；前端 Adapter 在生成契约之上封装 |
| 内部 RPC | gRPC + protobuf | 使用 Buf 管理 lint、生成和 breaking change；Connector 使用双向流 |
| MCP | 官方 Go MCP SDK + Argus Tool Gateway | SDK 只处理协议；权限、私有 Token 分流和 Tool 投影由 Argus 实现 |
| Agent Harness | Go 小内核，语义参考 Pi agent-core | Provider-neutral Message/Event、可插拔 ContextAssembler、顺序优先的 Tool Loop；不引入第二套 Workflow Runtime |
| PostgreSQL | `pgx` + `sqlc` | 使用显式 SQL 实现事务、条件更新、Lease、Fence Token 和 Outbox，不使用重 ORM；查询与生成文件按领域拆分并遵守 2000 行上限 |
| PostgreSQL Migration | Goose | Migration 以独立 Job 运行并持有 PostgreSQL advisory lock，普通 Server 启动不修改 Schema |
| Redis | `go-redis` | 只用于缓存、通知、限流和短期协调 |
| Kafka | `franz-go` | Ingest Producer、必要时的最小 Writer 和管理工具共用 |
| ClickHouse | `clickhouse-go/v2` | 只由 Telemetry Query、Schema Migration 和受控 Writer 使用 |
| Kubernetes | `client-go` + Helm Go SDK | 资源管理、Collector 安装和 `argusctl` 安装编排 |
| 对象存储 | MinIO Go SDK/S3 Adapter | 上层只依赖 Artifact Store 接口 |
| 可观测性 | OpenTelemetry Go + `slog` | Trace、Metric、结构化日志统一携带 enterprise/run/tool/execution 标识 |

第一版不引入 Temporal、Celery 或另一套权威工作流系统。Run、Step、Task、Lease 和 Execution 继续以 PostgreSQL 为事实来源，Redis 只降低调度延迟。

Agent Harness 不直接依赖某个 Provider SDK 的 Session/Memory 对象。`internal/agent` 维护 Agent Loop、Run Reducer 和事件协议；`internal/conversation` 维护不可变 ConversationEvent；`internal/model` 维护 AIModel/ModelCall/ContextSnapshot；`internal/integration/modelprovider` 在调用边界转换消息、流事件和可选 Provider Compaction。完整设计见[Agent Harness 与上下文管理](./16-agent-harness-and-context-management.md)。

### 3.1 后端语言协商与消息模型

`argus-server` 的 HTTP Middleware 统一解析 `Accept-Language`，把规范化后的 `zh-CN` 或 `en-US` 放入 Request Context，并设置 `Content-Language`。Handler、SSE 投影和通知渲染只能读取该 Context，不能各自解析 Header。gRPC/protobuf 内部调用传递规范化 Locale 字段，但领域状态机和持久化枚举保持语言无关。

推荐错误和事件展示结构：

```json
{
  "code": "HOST_CONNECTION_TIMEOUT",
  "message_key": "errors.host.connection_timeout",
  "params": {"host": "host-web-12", "seconds": 30},
  "message": "连接 host-web-12 超时（30 秒）"
}
```

`message` 方便非浏览器客户端直接展示，但不是程序判断依据。审计事件保存 `code/message_key/params`、操作者、资源和时间等不可变事实；查询时按请求 Locale 渲染展示文案，导出任务必须记录导出 Locale。

### 3.2 内部模块组织

代码按领域组织，不采用全局巨型 `controllers/`、`services/`、`models/` 分层：

```text
internal/
├── platform/
├── identity/
├── authorization/
├── model/
├── conversation/
├── agent/
├── mcp/
├── action/
├── card/
├── connector/
├── remoteaccess/
├── resource/
├── telemetry/
├── sandbox/
├── secret/
├── audit/
└── runtime/
```

建议在领域内进一步拆分：

```text
internal/agent/
├── loop/
├── context/
├── checkpoint/
├── events/
└── run/

internal/mcp/
├── registry/
├── gateway/
├── projection/
└── schema/
```

Agent Loop、ContextAssembler、Compactor 和 Provider Adapter 保持独立接口，避免把模型 SDK、数据库事务、Tool 执行和 Prompt 拼接写入同一个大文件。

每个领域内部包含 domain、service、repository/port 和 adapter。HTTP Handler、MCP Tool、Worker Handler 和自动化任务只能调用领域服务，不能各自实现数据库业务规则。

## 4. 身份、权限和 Secret

- 本地密码使用 Argon2id。
- 浏览器使用 HttpOnly、Secure、SameSite Cookie，所有变更请求执行 CSRF 防护。
- Session 和撤销事实保存在 PostgreSQL，Redis 只保存热缓存和快速失效通知。
- Redis 初始连接失败时 Server 可以 degraded 启动并保留自动重连客户端；`/readyz` 只以 PostgreSQL 为必要条件。Redis 不可用期间已有 Session 继续由 PostgreSQL 校验，新登录 fail closed。
- PlatformUser 与 EnterpriseUser 使用不同身份域和 Audience；EnterpriseUser 固定一个企业，不实现 Membership 或企业切换。
- 第一版企业级 RoleBinding、DataScope、RemoteAccessGrant、ManagedAccount、AuthorizationVersion 和类型化 Policy 在 Go 领域服务中实现；标签选择器使用独立的版本化白名单语法，受限 Policy 条件可以使用 CEL-Go，二者都不接受用户 SQL。
- API Key 和 ServiceAccount 凭证只显示一次，数据库只保存哈希，并固定企业、Tool/DataScope 和 AuthorizationVersion。
- M2 真实写表单统一使用 React Hook Form + Zod，DTO 继续直接消费生成的 `snake_case` 契约；Zod 只改善交互，服务端仍执行权威校验。
- 业务对象只保存 `secret_ref`。Secret Store 使用 Envelope Encryption，并预留外部 Vault/OpenBao/云 KMS Adapter；第一版不因此引入集群外依赖。

## 5. 测试与质量门禁

前端使用 ESLint、Prettier、Vitest、Testing Library、MSW、Playwright 和 axe-core。后端使用 `gofmt`、`go vet`、`golangci-lint`、`govulncheck`、`go test -race`、Testcontainers 和 Buf breaking check。

测试分层：

```text
单元测试
→ PostgreSQL/Redis/Kafka/ClickHouse 集成测试
→ OpenAPI/protobuf/MCP/Preview-Commit 契约测试
→ Kubernetes 临时 Namespace E2E
```

E2E 至少覆盖：

- 初始化、双层管理域、平台/企业身份互斥、单企业用户和跨企业拒绝。
- RoleBinding + DataScope 的列表/详情/批量/Tool/Card 一致过滤，以及授权敏感标签变化后的缓存、Binding、游标和流式订阅失效。
- Connector 注册并创建 Bastion Scope、内网主机经堡垒机接入、公网 Direct SSH 的 SSRF/固定出口边界。
- Connector 本机/SSH/WinRM 人工命令行票据与录像；RemoteAccessGrant 限定 Host/ManagedAccount/动作；人工会话和自动化 Execution 隔离。
- Collector 沿两种执行路径安装、Telemetry Route 选择矩阵和 Metrics/Logs/Traces Profile 配置。
- Kubernetes Node/Host 绑定，以及 Host Collector 与 DaemonSet Collection Claim 的冲突、非冲突共存和到期迁移。
- 资源查询、Preview/Confirm/Commit、撤权与 AuthorizationVersion、审批不补齐基础权限、Redis 清空恢复和 Pod 重启接管。
- Agent Event 顺序、ToolCall/ToolResult 完整切点、确定性 ToolResult Projection、增量 ContextSnapshot、压缩失败恢复、Projection Hash 和私有字段不可见性。
- OTLP 写入可信 Enterprise/Resource/Collector 身份，以及跨企业、超出 DataScope、跨 Signal 和敏感字段查询拒绝。

前端与 Card E2E 还必须覆盖 `zh-CN/en-US × light/dark` 基础矩阵、偏好持久化、缺失翻译回退、语言协商和主题切换后 Action Binding 不变。测试完成后删除临时 Namespace。

## 6. 第一版仓库骨架

```text
Argus/
├── cmd/
│   ├── argus-server/
│   ├── argus-worker/
│   ├── argus-connector-gateway/
│   ├── argus-telemetry/
│   ├── argus-connector/
│   └── argusctl/
├── internal/
├── api/
│   ├── openapi/
│   └── proto/
├── migrations/
│   ├── postgresql/
│   └── clickhouse/
├── web/
├── interactive-cards/
├── deploy/
└── tests/
    ├── contract/
    ├── integration/
    └── e2e/
```

前后端单文件仍遵守项目约束，不超过 2000 行；领域、页面和组件应在接近限制前主动拆分。
