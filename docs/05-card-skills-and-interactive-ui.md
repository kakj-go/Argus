# Card Skill 与交互式 UI

## 1. 目标

Card Skill 是一种由 AI 创建、选择和绑定数据的沙箱化会话微应用。它既可以是简单展示卡片，也可以是包含图表、表单、过滤、局部状态和确认按钮的完整交互界面。

必须保持三方解耦：

```text
Tool Result：只表达业务数据
Card Skill：只表达 UI 能力、Slot 和权限
Render Plan：由 AI 动态连接两者
```

不存在平台硬编码的：

```text
host.list → host-list-card
pod.list → pod-table-card
```

## 2. Card Skill 包结构

```text
host-overview/
├── card.json
├── SKILL.md
├── index.html
├── input.schema.json
├── actions.schema.json
└── demo.json
```

- `card.json`：名称、描述、版本、入口、权限和兼容信息。
- `SKILL.md`：告诉 Model Agent/Presentation Agent 何时使用以及需要哪些语义数据。
- `index.html`：允许包含 HTML、CSS 和 JavaScript 的 UI。
- `input.schema.json`：数据 Slot 的输入类型。
- `actions.schema.json`：Action Slot 的定义。
- `demo.json`：预览和验证用示例数据。

示例 Manifest：

```json
{
  "id": "host-overview",
  "name": "主机概览卡片",
  "description": "展示主机状态、资源使用率和告警",
  "version": "1.0.0",
  "entry": "index.html",
  "permissions": {
    "network": [],
    "clipboard": false,
    "downloads": false,
    "open_links": false,
    "direct_tool_calls": false
  }
}
```

## 3. 数据 Slot 和 Action Slot

HTML 使用显式标记，设置界面据此识别可配置区域：

```html
<article class="host-card">
  <h3 data-slot="title" data-type="string"></h3>
  <div data-slot="host_name" data-type="string"></div>
  <div data-slot="cpu_usage" data-type="number"></div>
  <section data-slot="alerts" data-type="array"></section>

  <button data-action-slot="cancel">取消</button>
  <button data-action-slot="confirm">确认</button>
</article>
```

导入普通 HTML 时，可以由 AI 或解析器自动提出 Slot 建议，但正式保存的 Card Skill 应具有明确 Manifest，不能在每次渲染时重新猜测。

设置界面允许为每个数据区域配置来源策略：

```json
{
  "slot": "cpu_usage",
  "type": "number",
  "required": true,
  "source_policy": {
    "mode": "tool_allowlist",
    "tools": ["prometheus.query", "host.metrics"]
  }
}
```

可支持：

- `any_tool`：任意 Tool Result。
- `tool_allowlist`：只允许指定 Tool。
- `tool_denylist`：排除指定 Tool。
- `user_input`：只允许用户输入。
- `ai_generated`：允许模型生成。
- `literal`：允许固定值。
- `composite`：允许多个 Tool Result 组合。

如果 Slot 限定为 `tool_allowlist`，就不能接受模型复制出来的 literal，否则来源限制可以被绕过。

## 4. Render Plan

AI 调用 `card.render` 时传递字段引用：

```json
{
  "card_id": "host-overview",
  "card_version": "1.0.0",
  "bindings": {
    "title": {
      "source": "ai_generated",
      "value": "生产环境主机状态"
    },
    "host_name": {
      "source": "tool_result",
      "call_id": "call_host_get_01",
      "path": "$.data.name"
    },
    "cpu_usage": {
      "source": "tool_result",
      "call_id": "call_metrics_02",
      "path": "$.data.cpu.usage"
    },
    "alerts": {
      "source": "tool_result",
      "call_id": "call_alerts_03",
      "path": "$.data.items[*]",
      "map": {
        "level": "$.severity",
        "message": "$.summary"
      }
    }
  }
}
```

Card Skill 可以组合多个 Tool 的不同字段。平台负责确定性地解析、映射、过滤、排序和校验；AI 负责决定使用什么数据以及如何展示。

## 5. Action Slot

Card Skill 只声明一个可绑定动作，不硬编码业务 Tool：

```json
{
  "action_slots": {
    "confirm": {
      "type": "tool_call",
      "label": "确认"
    },
    "cancel": {
      "type": "tool_call_or_dismiss",
      "label": "取消"
    }
  }
}
```

AI 在 Render Plan 中把 Pending Action 的 `action_ref` 绑定到 Slot。`card.render` 验证后生成服务端 Action Binding。浏览器只拿到 `action_binding_id`，真实 Token 和目标 Tool 保留在 Host。

卡片 JavaScript 只能请求执行已绑定动作：

```javascript
const result = await argus.invokeAction({ actionSlot: "confirm" });
```

不能把动作改成任意 Tool 名称。

这里的 Host 明确指 `argus-server` 的 Action Executor 和 Pending Action Store，不是浏览器。浏览器、iframe 和 Host Bridge 均不得获得 `_meta.argus__token`。用户点击确认时，Card Skill 只发送 Action Slot 名称；宿主将其解析为 `action_binding_id`，再调用服务端固定端点：

```text
POST /api/card-actions/{action_binding_id}:invoke
body: { request_id }
```

请求中不包含 Commit Tool、业务参数、`action_ref` 或 `argus__token`。服务端重新认证当前用户并由 Action Executor 直接调用绑定的 Commit Tool。

### 5.1 Query Slot

主机、Kubernetes、Connector、Telemetry 等列表需要翻页、过滤和刷新。Card Skill 仍然不能直接调用任意读取 Tool，因此增加 Query Slot：

```json
{
  "query_slots": {
    "refresh": {
      "type": "bound_tool_query",
      "parameters_schema": {
        "type": "object",
        "properties": {
          "filter": {"type": "object"},
          "cursor": {"type": ["string", "null"]},
          "limit": {"type": "integer", "minimum": 1, "maximum": 200}
        },
        "additionalProperties": false
      }
    }
  }
}
```

Render Plan 把 Query Slot 绑定到一个已经通过授权和 Schema 校验的读取 Tool，并生成 `query_binding_id`。卡片可以调用：

```javascript
const page = await argus.invokeQuery({
  querySlot: "refresh",
  parameters: {filter, cursor, limit: 50}
});
```

Host Bridge 只发送绑定 ID 和经过 Slot Schema 校验的过滤参数。服务端重新执行权限和数据范围检查；卡片不能更换 Tool、enterprise_id、资源范围或排序表达式。

Query Slot 与 Action Slot 的区别：

| 项目 | Query Slot | Action Slot |
| --- | --- | --- |
| 用途 | 查询、过滤、翻页、刷新、读取详情 | 确认、取消和受控变更 |
| 是否需要 Preview | 否 | 变更必须已有 Pending Action |
| 浏览器标识 | `query_binding_id` | `action_binding_id` |
| 参数 | 白名单过滤、游标、排序 | 不允许提交业务参数 |
| 模型参与 | 首次 Render Plan；后续交互不参与 | 首次绑定；用户点击后不参与 |

## 6. `/` 命令引用

Chatbox 输入 `/` 时展示用户可用的 Card Skill：

```text
/card 主机概览
/card K8s Pod 列表
/card 告警时间线
/card 操作确认
/card-create 创建新卡片
/card-save 保存上一张动态卡片
```

选中后应在消息上附加结构化引用，而不是只插入普通文本：

```json
{
  "text": "查询 10.0.0.12 的运行情况",
  "references": [
    {
      "type": "card_skill",
      "card_id": "host-overview",
      "version": "1.0.0",
      "selection": "pinned"
    }
  ]
}
```

选择策略：

- `automatic`：Presentation Agent 自动选择或生成。
- `preferred`：优先使用用户引用的卡片，无法匹配时允许降级。
- `pinned`：必须使用指定版本；失败时明确返回原因。

Agent 只加载已选择或候选 Card Skill 的描述和 Schema，不需要把整个卡片库的 HTML 全部放入模型上下文。

## 7. AI 动态生成

建议提供：

```text
card.skill.create.preview
card.skill.create.commit
card.skill.validate
card.skill.render_demo
card.skill.update.preview
card.skill.update.commit
card.skill.publish.preview
card.skill.publish.commit
card.skill.list
card.render
```

`card.skill.render_demo` 只在隔离沙箱中使用 Demo 数据渲染，不写入发布目录。创建、更新和发布属于持久变更，必须遵守统一 `.preview/.commit` 和私有 `_meta.argus__token` 协议；个人保存可以使用较低风险策略，但仍不能绕过 Preview/Commit。

工作流：

```text
AI 生成 Card Skill
→ card.skill.validate
→ 返回结构化安全或 Schema 错误
→ AI 修改
→ 使用 Demo 数据在沙箱预览
→ 当前消息临时使用
→ 用户选择是否保存或发布
```

动态卡片信任等级：

| 等级 | 范围 | 要求 |
| --- | --- | --- |
| ephemeral | 当前消息 | 最严格沙箱，自动过期 |
| personal | 当前用户 | 用户主动保存 |
| tenant | 当前租户 | 管理员审核发布 |
| system | 平台内置 | 平台签名和发布流程 |

## 8. 自由 UI 与安全运行时

AI 可以使用 HTML、CSS、JavaScript、SVG、Canvas、动画、图表、Tabs、搜索和局部状态。平台可以注入主题变量、图标和可选 UI Runtime，提升生成效果，但不强制 Card Skill 使用固定组件库。

静态检测无法证明任意 JavaScript 安全，因此必须同时实施：

### 8.1 保存前检测

检查外部脚本、动态代码执行、网络请求、iframe、表单提交、存储、剪贴板、设备权限、混淆代码和明显资源消耗。失败时返回可供 AI 修复的结构化错误。

### 8.2 独立沙箱

使用独立来源 iframe，默认只允许脚本执行，不启用 `allow-same-origin`、表单、弹窗、顶层导航和下载。

建议默认 CSP：

```text
default-src 'none';
connect-src 'none';
img-src data: blob:;
style-src 'unsafe-inline';
script-src 'nonce-<runtime-nonce>';
frame-src 'none';
object-src 'none';
base-uri 'none';
form-action 'none';
```

### 8.3 Host Bridge 白名单

Card Skill 只访问明确授予的能力，例如：

- 读取已绑定数据。
- 执行已绑定 Action。
- 调整卡片高度。
- 展示宿主通知。
- 请求打开经过策略校验的链接。

真实凭证、Pending Action Token 和宿主 DOM 永不进入卡片环境。

Host Bridge 使用由宿主创建并传入 iframe 的独占 `MessagePort` 作为能力通道。由于不启用 `allow-same-origin` 的 iframe 使用 opaque origin，不能单独依赖 `postMessage` 的 origin 判断。每条 Bridge 消息必须包含并校验 Card Instance、通道 nonce、消息序号和 Schema 版本；消息重复、越序、超频或实例不匹配时拒绝并记录审计。

### 8.4 运行时限制

- CPU 和执行时间预算。
- DOM 节点和内存上限。
- 消息频率限制。
- 卡片尺寸和全屏策略。
- 崩溃隔离与熔断。
- 明确标识 AI 生成或租户自定义内容，降低钓鱼风险。

## 9. 渲染错误与 AI 自修复

`card.render` 对 Slot、Tool 来源、字段类型、Action、权限和模板进行校验。错误以结构化 Tool Result 返回：

```json
{
  "success": false,
  "error": {
    "code": "SLOT_SOURCE_TOOL_NOT_ALLOWED",
    "slot": "cpu_usage",
    "actual_tool": "host.execute_command",
    "allowed_tools": ["prometheus.query", "host.metrics"],
    "repair": "replace_binding_or_call_allowed_tool"
  }
}
```

Model Agent 可以更换字段、调用允许的 Tool、删除非必填 Slot 或换卡片。应设置有限重试次数，避免自动修复死循环。

## 10. 第一版通用 UI 组件

当前仓库尚未包含可枚举的前端组件实现，因此这里固定第一版 Card Runtime 需要提供的逻辑组件和交互契约，具体框架可以选择 React、Web Components 或编译后的原生 HTML。

| 组件 | 用途 | 关键约束 |
| --- | --- | --- |
| `StatusBadge` | 在线、离线、健康、失败等短状态 | 颜色必须同时配文字或图标 |
| `MetricValue` | CPU、内存、延迟、数量 | 必须声明单位和时间范围 |
| `DescriptionList` | 资源详情字段 | Secret 字段不允许绑定 |
| `DataTable` | 主机、Pod、Collector 等列表 | 支持列定义、空状态和服务端游标 |
| `FilterBar` | 白名单过滤条件 | 远程过滤只能触发 Query Slot |
| `Pagination` | 下一页、上一页、每页数量 | 使用服务端签名 Cursor |
| `Tabs` | 概览、配置、事件、日志 | 不隐藏高风险确认信息 |
| `Timeline` | Run、安装和事件历史 | 使用服务端时间并显示状态来源 |
| `DiffViewer` | 配置、YAML、权限和文件变化 | 高风险字段必须完整展示或明确省略原因 |
| `ProgressSteps` | 安装、升级、执行进度 | 显示未知、回滚和部分成功状态 |
| `LogViewer` | 受限日志展示 | 限制行数、字节数并默认脱敏 |
| `ActionSummary` | 风险、审批和确认摘要 | 显示资源、动作、过期和计划哈希摘要 |
| `EmptyErrorState` | 空列表、权限不足、查询错误 | 区分无数据与无权限，不能泄漏资源存在性 |

组件只提供一致的交互和可访问性，不成为权限边界。Card Skill 仍可以使用自由 HTML/CSS/JavaScript，但列表、过滤、详情和确认类系统卡片优先复用这些经过测试的组件。

## 11. 第一版业务卡片目录

### 11.1 通用系统卡片

| Card Skill | 用途 | 主要 Slot |
| --- | --- | --- |
| `resource-list` | 通用资源列表 | columns、items、page、applied_filter |
| `resource-detail` | 通用详情 | identity、status、properties、events |
| `pending-action-confirm` | 普通变更确认 | preview、risk、expires_at、confirm/cancel |
| `critical-action-approval` | 高危操作确认和审批 | impact、approvers、separation_of_duty、expires_at |
| `execution-progress` | 长任务进度 | steps、current_step、rollback、result |
| `operation-result` | 最终结果 | status、changed_resources、audit_ref |
| `configuration-diff` | 配置前后差异 | before、after、masked_fields、warnings |

### 11.2 Connector 和主机卡片

| Card Skill | 能力 |
| --- | --- |
| `connector-list` | 名称、在线状态、版本、标签、最后心跳和管理资源数 |
| `connector-detail` | 设备身份、能力、证书状态、连接代次和最近命令 |
| `connector-enrollment` | 一次性安装命令、有效期和允许注册次数 |
| `host-list` | 服务端过滤、排序、游标翻页和批量选择 |
| `host-detail` | 连接、系统、所有者、标签、Telemetry 和事件详情 |
| `host-connection-test` | DNS、网络、认证和目标系统检查结果 |
| `host-create-confirm` | 新增主机的不可变预览和确认 |
| `host-command-confirm` | 命令、目标范围、超时、风险和审批确认 |

### 11.3 Kubernetes 卡片

| Card Skill | 能力 |
| --- | --- |
| `kubernetes-cluster-list` | 集群状态、环境、Connector、版本和最后检查 |
| `kubernetes-cluster-detail` | API Server、认证摘要、Namespace、节点和接入状态 |
| `kubernetes-workload-list` | Deployment、StatefulSet、DaemonSet 统一列表 |
| `kubernetes-pod-list` | Namespace、Phase、Node、Owner、重启次数和标签过滤 |
| `kubernetes-pod-detail` | 容器、Condition、资源、事件和最近日志入口 |
| `kubernetes-object-detail` | 受限 YAML 摘要、Owner Reference 和事件 |
| `kubernetes-change-confirm` | Apply、Restart、Scale 等变更的对象 Diff 和确认 |

### 11.4 Telemetry 和安装卡片

| Card Skill | 能力 |
| --- | --- |
| `telemetry-group-list` | 模式、成员数、Gateway、健康和积压 |
| `telemetry-group-detail` | Leaf/Gateway 拓扑、路由和最后数据时间 |
| `collector-list` | 角色、版本、Profile、配置 Revision 和状态 |
| `collector-detail` | Desired/Effective Config、队列、错误和资源占用 |
| `collector-install-confirm` | Artifact、文件、服务、端口、权限、回滚和确认 |
| `collector-upgrade-confirm` | 版本差异、兼容性、批次和回滚计划 |
| `installation-progress` | 探测、传输、校验、安装、重启、验证和回滚步骤 |

模型、Sandbox、权限和审计页面可以复用通用列表、详情、Diff、进度和确认卡片，第一版不为每个 CRUD 页面创建独立卡片。

## 12. 主机列表查询协议

`host.list` 使用结构化过滤，不接受 SQL、自由表达式或前端拼接的数据库字段：

```json
{
  "filter": {
    "query": "web-01",
    "status": ["online", "degraded"],
    "platform": ["linux", "windows"],
    "connection_method": ["connector", "ssh_via_connector"],
    "connector_ids": ["conn-01"],
    "environment": ["production"],
    "owner_team_ids": ["team-sre"],
    "tags": {"region": ["cn-east"], "role": ["web"]},
    "telemetry_status": ["reporting", "stale"]
  },
  "sort": [{"field": "last_seen_at", "direction": "desc"}],
  "page": {"cursor": null, "limit": 50}
}
```

服务端处理顺序：

```text
参数 Schema 校验
→ 字段和操作符白名单
→ authorization_scope AND user_filter
→ 稳定排序（最后追加 id）
→ Cursor 查询
→ 字段脱敏
→ 返回 applied_filter 和 next_cursor
```

标准返回：

```json
{
  "data": {"items": [], "next_cursor": null, "has_more": false},
  "meta": {
    "applied_filter": {},
    "sort": [{"field": "last_seen_at", "direction": "desc"}],
    "limit": 50
  }
}
```

卡片可以在当前已授权页面内做纯展示型本地搜索，但本地搜索不改变总数，也不能代替服务端过滤。跨页过滤、排序和刷新必须调用绑定的 Query Slot。

## 13. Kubernetes 列表查询协议

Kubernetes 查询分为两级：

1. `kubernetes.cluster.list`：查询 Argus 已登记集群，支持环境、状态、Connector、标签和版本过滤。
2. `kubernetes.resource.list`：查询具体集群中的 Kubernetes 对象，必须提供 `cluster_id` 和 `resource_type`。

```json
{
  "cluster_id": "cluster-01",
  "resource_type": "pods",
  "filter": {
    "namespaces": ["production"],
    "query": "checkout",
    "label_selector": "app=checkout,tier=backend",
    "field_selector": "status.phase=Running",
    "phases": ["Running", "Pending"],
    "node_names": [],
    "owner_kinds": ["Deployment"],
    "min_restart_count": 1
  },
  "sort": [{"field": "restart_count", "direction": "desc"}],
  "page": {"cursor": null, "limit": 50}
}
```

服务端必须验证 Label/Field Selector 语法和允许字段，并先把用户有权访问的 Namespace 范围与请求过滤求交集。不能通过错误差异向用户泄漏无权限 Namespace 或对象是否存在。跨多个集群的查询由 Argus Query Planner 分解为每集群调用并设置并发、超时和部分失败状态，不能把无限制全局查询直接下发到所有 API Server。

## 14. 详情、确认和安装卡片约束

详情卡片必须显示资源身份、数据更新时间和来源 Tool Call。需要额外数据时使用绑定的 Query Slot 获取，不能把 Secret、完整 kubeconfig 或原始凭证放入 DOM。

确认卡片必须同时显示：

- 动作名称、目标资源和影响范围。
- 实际风险等级和需要的审批。
- 将创建、修改、删除或重启的对象。
- 配置/文件 Diff 或明确的摘要与省略原因。
- 不可变计划摘要、过期时间和是否允许回滚。
- 确认、取消；高危操作还需明确的增强认证或审批状态。

Collector/软件安装确认卡片还必须显示 Artifact 名称、版本、Digest、签名状态、大小、目标路径、服务变化、端口/防火墙变化、所需权限、预计中断和自动回滚步骤。确认按钮只绑定 `action_binding_id`，用户确认的正是 Preview Tool 已持久化的计划。
