# 交互卡片与渲染运行时

## 1. 产品边界

交互卡片（`InteractiveCard`）是用于展示 Tool Result 的沙箱化 HTML/CSS/JavaScript UI。业务 Tool 只返回结构化数据；项目内置的“渲染交互卡片”Skill 根据 Tool Output Schema、用户意图和卡片目录生成 Render Plan。模型不能把 Tool 调用硬编码进 HTML，也不能绕过服务端权限、数据裁剪或 Action Binding。

卡片来源只有两种：

- `system`：随产品发布，代码、绑定、Demo、状态均只读。
- `enterprise`：企业超级管理员通过会话中的“创建交互卡片”命令创建和维护。

不存在个人卡片、临时卡片、审核发布或在会话中引用既有卡片的流程。

## 2. 创建与可见性

- 只有企业超级管理员输入 `/` 时显示“创建交互卡片”；普通用户和部门管理员手动输入 `/` 也不打开卡片目录。
- 创建命令随消息以 `interactive_card.create` 发送。AI 完成生成后保存企业草稿、自动执行验证，但始终保持禁用。
- 回复提供卡片详情入口。管理员在交互卡片列表中检查真实预览、Demo 和 Slot Binding。
- 企业超级管理员可查看企业卡片全部状态；其他企业用户只可查看已启用企业卡片和系统卡片。

## 3. 数据结构

`InteractiveCard` 至少包含：

- `id`、`enterprise_id`（系统卡片为空）、`source`、名称、说明、版本和 Revision。
- `lifecycle`：`draft | active | deprecated`；新建固定为 `draft`。
- `enabled`：企业卡片默认 `false`；系统卡片由发布物固定为 `true`。
- 框架无关的 HTML/CSS/JavaScript 模板和 `data-slot` 标记。
- Data Slot、Action Slot、Query Slot、权限声明、Demo 场景和验证结果。
- Slot Binding、创建人、创建时间和更新时间。
- 版本化 Manifest：入口内容哈希、允许的资源/CSP 能力、Bridge 版本、Data/Query/Action Slot、支持语言和主题。

系统卡片不可更新、删除、停用、修改绑定或 Demo。企业卡片的代码、Slot、绑定和 Demo 变更会增加 Revision，并重新进入未验证、禁用状态。

## 4. Slot Binding

管理员在预览中点击 `data-slot` 区域后配置绑定。配置页面读取只读 Tool Schema Catalog，不为查看字段而执行生产 Tool。

每个 `SlotBinding` 保存：

- Slot 名称和模式 `strict | preferred`。
- Tool 名称、Tool Output Schema 版本、JSON 字段路径。
- 可选数组映射，包括 item path、label path、value path。

`strict` 要求指定 Tool 和 Schema 版本完全匹配；`preferred` 允许内置渲染 Skill 在类型兼容时选择等价字段。无论哪种模式，运行时 Render Plan 都必须引用真实 `tool_call_id + path`；Binding Preset 不能变成 HTML 内的固定 Tool 调用。

## 5. 验证与启用门禁

卡片启用前必须同时满足：

1. Manifest、按卡片生成的 CSP、外部资源、动态代码执行、Bridge 能力和资源预算安全检查通过。
2. 所有必填、非 `ai_generated` Slot 均有类型兼容且 Schema 版本有效的绑定。
3. 默认、空数据、错误、大数据、浅色、深色、中文、英文 Demo 场景全部渲染通过。
4. iframe 完成 Origin/nonce 握手，业务消息只通过 `MessageChannel/MessagePort` 传输，并通过自动高度回报、消息序号、事件白名单和销毁检查。

验证失败时返回稳定错误码、Slot 和场景，不允许启用。启用后，内置“渲染交互卡片”Skill 才能发现该企业卡片。停用立即阻止新 Render Plan 选择，但历史消息继续使用已保存版本或渲染快照。

## 6. 列表与预览

交互卡片页分为“自定义卡片”和“内置卡片”两个 Tab。列表采用分隔式单行布局，每行展示元数据、状态、操作和与会话相同的 Sandbox iframe：

- 使用 Demo 数据渲染，进入可视区域后才加载。
- iframe 通过版本化 Bridge 回报内容高度；宿主限制最大折叠高度，超出后提供完整展开。
- 浅色、深色和中英文预览使用宿主上下文，不允许卡片自行读取主应用状态。
- 点击 Slot 仅发送 Slot 标识和几何信息，宿主负责高亮与打开绑定抽屉。

## 7. 运行时

内置“渲染交互卡片”Skill 的候选集仅包含系统卡片和当前企业已启用卡片。服务端依次执行权限裁剪、字段脱敏、Schema 校验、绑定解析和 Render Plan 校验，再把最小数据集交给 Card Host。

Card iframe 使用独立 Origin 或严格 `sandbox`，Host 根据 Manifest 生成最小 CSP。全局 `postMessage` 只允许完成一次带明确 `targetOrigin` 的握手，随后把 `MessagePort` 移交给 iframe；所有业务消息使用版本化 Schema、nonce、单调序号和大小限制。

浏览器和 iframe 只能获得解析后的数据、`query_binding_id` 或 `action_binding_id`。Secret、任意 Tool 名称、PendingAction 私有参数、Commit Token、`argus__token`、生产凭证和人工远程会话票据永不进入卡片或公开 DTO。

## 8. 测试要求

必须覆盖管理员命令创建、默认禁用、系统卡片只读、严格/优先绑定、Schema 类型错误、必填绑定缺失、全部 Demo 门禁、启用后自动选择，以及 iframe 懒加载、自适应高度、完整展开和 Slot 点击定位。安全测试还必须覆盖 CSP 逃逸、错误 Origin、重复/乱序消息、伪造 Binding ID、销毁后消息、消息大小上限，以及浏览器网络与 DOM 中搜索不到私有 Token/参数。
