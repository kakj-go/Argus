# PlanV4 Task 07：主机与堡垒机接入体验重构

## 状态

已完成。主机五模式与堡垒机 A/B/C 已统一为共享向导状态机；动作语义、服务端 onboarding 投影、一次性结果生命周期、mock、i18n、可访问性和浏览器验收均已收口。

## 背景

2026-08-31 对文档、代码和真实 Demo 的联合复核确认：PlanV4 已有网络场景和安装能力，但前端把早期“左侧场景 + 右侧说明/表单”的静态 Demo 当成了正式向导。普通主机步骤条没有真实状态，堡垒机第二步仍可切换模式，B/C 沿用模式 A 的文案与结果；待注册堡垒机命令错误复用了 Connector replacement，self-enrolled 主机重新生成命令则绕过 Pending Action 直接返回明文。

本任务不修补现有布局，而是从共享状态机和动作语义重构新增主机、添加堡垒机及待注册操作。项目尚未发布，不考虑旧 UI、旧 mock/localStorage、旧 action 名称和历史测试的兼容。

## 目标

1. 主机与堡垒机统一采用“第一步选择接入模式，第二步填写信息”的真实向导。
2. 第三步根据模式进入连接测试与预览，或确认并生成一次性命令。
3. 手动命令、平台代安装和控制隧道拥有不同且完整的结果态。
4. 命令领取、待注册令牌轮换、self-enrolled 卸载命令与已有 Connector 替换成为独立动作。
5. 共享结构进入统一组件/领域边界，删除静态步骤条、空 form 和重复页面编排。

## 统一状态机

| 状态 | 页面职责 | 允许的主要动作 |
| --- | --- | --- |
| `select_mode` | 仅展示模式卡、适用条件、需要准备和完成方式 | 选择模式、下一步、取消 |
| `details` | 显示已选模式摘要和对应字段；不保留完整模式列表 | 上一步、更改模式、保存输入、继续 |
| `verify` | SSH/WinRM 模式执行 ConnectionTest，展示诊断、冻结事实与资源预览 | 返回修改、重新测试、确认执行 |
| `confirm_command` | 无入站路径的模式展示前置条件、冻结计划与资源预览 | 返回修改、确认并生成命令 |
| `installing` | B/C 展示安装 operation、控制隧道和 Connector 收敛进度 | 关闭后后台继续、查看任务、失败重试 |
| `command_result` | A/self_enrolled 只展示一键安装命令、复制状态和有效期，不提供命令模式切换 | 复制、确认已保存并关闭 |
| `completed` | 展示最终资源和下一步 | 打开详情、关闭 |

步骤条只对应用户可返回的输入/验证阶段；`installing`、`command_result`、`completed` 是提交后的结果状态，不伪装成输入步骤。self_enrolled 与堡垒机 A 的第三步名称为“确认并生成命令”，其他模式为“测试与确认”。

## 模式目录与字段

### 普通主机

| 模式 | 第二步字段 | 第三步 | 结果 |
| --- | --- | --- | --- |
| ① 双向可达 | 名称、地址、协议、端口、系统、环境、账号、凭据、标签 | ConnectionTest + Preview | 创建/安装状态 |
| ② 只进不出 | 同① | ConnectionTest + 隧道前件 Preview | 创建/安装与隧道状态 |
| ⑤ 只出不进/自助安装 | 名称、架构、环境、标签 | 冻结计划 + 确认 | 一次性安装命令 |
| 标准堡垒机成员 | 所属堡垒机 + ①的目标字段 | Connector 路径测试 + Preview | 创建/安装状态 |
| 受限端口堡垒机成员 | 所属堡垒机 + ①的目标字段 | Connector 路径/隧道前件测试 + Preview | 创建/安装与隧道状态 |

### 堡垒机

| 模式 | 第二步字段 | 第三步 | 结果 |
| --- | --- | --- | --- |
| A 手动命令安装 | 名称、环境、标签 | 前置条件 + Scope Preview | 一次性安装命令 |
| B 平台 SSH 代安装 | 名称、环境、标签、地址、端口、账号、凭据 | ConnectionTest + Scope/安装 Preview | 安装进度 → 完成 |
| C 平台代安装 + 控制隧道 | 同 B | ConnectionTest + Scope/隧道 Preview | 安装/隧道进度 → 完成 |

## 导航与数据规则

- 打开向导时不默认提交任何模式；可以视觉标记“推荐”，但用户必须完成一次明确选择才能进入第二步。
- 从第二步返回第一步时保留公共字段：名称、环境、标签。
- 地址、端口、账号、凭据、Scope、架构等模式专属字段只在兼容模式间保留；切到不兼容模式时清除。已输入凭据引用被清除前，如会造成可见输入丢失，需明确提示。
- 第二步顶部显示紧凑摘要，例如“已选择：平台 SSH 代安装 · 更改”，点击“更改”返回第一步。
- 所有输入必须属于真实 `<form>`；Enter、提交按钮和错误处理走同一个 submit 路径。
- 连接测试失败停留在 `verify`，按服务端诊断提供“返回并改选模式”入口；不得自动替用户改变模式。
- 关闭未提交向导直接清理本地草稿；当前版本不持久化向导草稿，也不迁移旧 localStorage。

## 第一步内容原则

- 卡片主标题使用用户动作与网络条件，例如“平台 SSH 代安装”“主机自助安装”；协议枚举只出现在技术详情。
- 每张卡只保留三类决策信息：适用条件、需要准备、平台会做什么。
- 所有可用卡片不重复显示“已支持”；只对“推荐”“不可用/规划中”显示有决策意义的徽章。
- 拓扑图用于解释方向，不承担字段说明；内部端口、route kind、transport 和执行器名称放入可展开的“技术细节”。
- 第一步不得出现名称、地址、凭据、标签、命令和提交预览按钮。

## 命令与待注册生命周期

### 动作边界

| 用户意图 | 领域动作 | 风险 | 行为 |
| --- | --- | --- | --- |
| 初次创建命令型资源 | 资源 create Pending Action | write | 创建资源并产生一次性结果 |
| 领取已完成但未领取的结果 | claim one-time result | read-once/write-audit | 原子领取加密保存的一次性结果，不生成新令牌 |
| 待注册主机/堡垒机命令丢失或过期 | enrollment token rotate | write，策略可审批 | 吊销旧未消费令牌并生成新令牌，不执行 Connector fencing |
| 已注册 self-enrolled 主机卸载 | `host.uninstall.command` | dangerous | 生成独立、短期、一次性卸载授权；不复用 enrollment token |
| 已有 Connector 更换机器/身份 | `bastion.connector.replace` | dangerous | fence 旧 Connector、轮换身份并创建替换 operation |

新增待注册主机与堡垒机 token 轮换契约，名称可采用 `host.enrollment.rotate`、`bastion.enrollment.rotate`；self-enrolled 卸载命令使用独立 action。最终命名在 Contract first 阶段固化。服务端必须拒绝对存在 active Connector 的 Scope 调用 enrollment rotate，并拒绝对纯 pending Scope 误用 replacement；旧 `POST /enterprise/hosts/{id}/install-command` 与待注册 Scope 的 replacement 调用直接删除，不保留兼容端点或客户端方法。

### 待注册卡片状态

| 状态 | 主文案 | 主操作 |
| --- | --- | --- |
| 一次性结果可领取 | 安装命令已生成，等待领取 | 领取安装命令 |
| 结果已领取，等待注册 | 请在目标机器执行已领取命令 | 重新生成安装命令 |
| 轮换待审批 | 新命令申请等待审批 | 查看审批 |
| 审批通过且可领取 | 新命令已生成，等待领取 | 领取安装命令 |
| 令牌过期 | 安装命令已过期 | 重新生成安装命令 |
| B/C 安装中 | 平台正在安装 Connector | 查看安装进度 |
| 安装失败 | 安装未完成，展示可操作原因 | 重试/重新测试 |
| 已注册 | Connector 在线或离线状态 | 打开详情 |

待注册卡片不得写“执行下面的命令”却不显示命令；不得显示原始 i18n key、tool 名、Scope ID 或内部 diff。危险 replacement 只能从已有 Connector 的维护入口发起。

## 前端架构

- `@argus/ui`：复用并按需扩展 `Dialog`、`Wizard`、步骤语义、`ScenarioCard`、结果/进度原语；不包含主机或堡垒机业务枚举。
- Enterprise hosts 领域：建立 `components/hosts/onboarding/`，包含模式目录、共享向导控制器、主机步骤、堡垒机步骤、验证/结果适配器。
- API client：暴露明确的 create、test、preview、claim、rotate、replace 方法；禁止以一个含糊方法覆盖不同风险动作。
- i18n：模式和向导步骤进入 hosts 模块；跨主机、Kubernetes、审批与 Chat 的 Pending Action 由公开 `action_type` 驱动统一 `pendingActions` Presenter，安装 operation/stage/event/tunnel 状态统一映射。zh/en 键集合、已知 action_type 穷举测试和 JSX 用户文案静态检查共同阻止原始 key、服务端文本与枚举泄漏。
- 删除旧 `add-host-wizard.tsx`/`add-bastion-dialog.tsx` 中的静态步骤条、隐藏空 form、重复双栏编排和废弃 feature flag；允许直接拆文件和改调用方，不保留兼容组件。

## 任务清单

- [x] P4-UX-01 Contract first：固化 host/bastion pending enrollment rotate、self-enrolled 卸载命令、one-time result availability/claim、适用前件、风险、错误码和审计事件；删除直接返回命令与误用 replacement 的旧路径，重新生成客户端。
- [x] P4-UX-02 扩展共享 Wizard/Dialog 组合，使当前、完成、返回、动态第三步、结果态、焦点和真实 form 语义一致。
- [x] P4-UX-03 建立 hosts onboarding 模式目录和状态机，集中定义字段兼容、保留/清理、验证和结果路由。
- [x] P4-UX-04 重构普通主机五模式：第一步纯选择、第二步表单、第三步动态验证/命令确认。
- [x] P4-UX-05 重构堡垒机 A/B/C：动态模式说明、第二步字段、B/C 测试预览和安装/隧道进度结果。
- [x] P4-UX-06 重构待注册卡片和动作：领取、轮换、审批、过期、安装中、失败与已注册状态。
- [x] P4-UX-07 修复 i18n、真实 form、Enter、焦点、读屏步骤、错误关联、桌面视口与双滚动问题。
- [x] P4-UX-08 更新 mock engine/seed，直接删除旧 action 和旧 localStorage 结构，不做迁移兼容。
- [x] P4-UX-09 重写 Playwright：禁止第一步字段、验证第二步字段、Back/Change、动态第三步、A/B/C 结果和命令生命周期。
- [x] P4-UX-10 删除旧组件分支、废弃文案/常量/样式和已固化错误行为的测试；更新 Task 03/05/06 与主文档实施证据。

## 实施顺序

1. P4-UX-01 动作契约与状态投影。
2. P4-UX-02～03 共享状态机和组件边界。
3. P4-UX-04～06 主机、堡垒机和待注册卡片。
4. P4-UX-07～08 清理语义、i18n、mock 和旧实现。
5. P4-UX-09～10 浏览器测试、真实 E2E 和文档收口。

## 退出标准

1. 主机和堡垒机第一步都只能选择模式，第二步才出现信息字段；步骤条与 DOM 可见内容一致。
2. 五种主机模式与 A/B/C 均走通正确的第三步和结果态；B/C 不出现命令结果，A/self_enrolled 不伪造 ConnectionTest。
3. 待注册命令的领取、轮换、审批和过期可恢复；纯 pending Scope 不再触发 Connector replacement。
4. 所有字段属于真实 form，Enter、Back、Change、错误聚焦和键盘/读屏行为通过自动化验证。
5. 一次性明文不进入持久化浏览器状态、日志、普通 DTO 或审计；命令领取与轮换均有完整审计。
   自签名快速模式的风险提示必须说明：令牌位于一行命令中，且只有首次脚本下载跳过 TLS 校验；不得把它描述为完整安全引导。
6. mock Playwright、real Playwright、Task 05 Kubernetes E2E、typecheck/test、契约生成与 axe 门禁全部通过。
7. 旧单屏向导、静态步骤条、兼容 action/DTO、废弃 feature flag 和错误 E2E 断言从代码库删除。

## 实施回顾（2026-09-01）

- `@argus/ui` 承载 Wizard/Dialog/Scenario 通用原语，Enterprise hosts reducer 承载模式、字段兼容、校验与结果路由，业务枚举未进入共享包。
- 主机和堡垒机第一步 DOM 只包含模式卡；第二步只显示“已选择模式 · 更改”和对应字段；第三步进入 ConnectionTest/Preview 或命令确认。关闭对话框清理草稿与命令明文。
- 向导滚动内容区在四边保留共享表单焦点环的安全内边距，名称输入框和整行标签文本框聚焦时不会再被 overflow 裁掉左边或底边；Playwright 以焦点环几何边界覆盖该回归。
- Host/BastionScope 的 onboarding 投影直接驱动领取、轮换、审批、过期、安装中、失败和已注册卡片；active Connector replacement 仅位于维护入口并明确 fencing。
- `PendingActionPublic.action_type` 直接来自服务端持久化领域动作；确认卡、审批列表/详情和 Chat 共用双语 Presenter，服务端 `title/summary/diff` 只保留为审计文本。B/C 安装进度的 operation、event 和 control tunnel 状态同样不再直接显示内部枚举。
- 项目级 i18n 门禁除翻译键注册和 zh/en 键集合外，还扫描 Enterprise、Platform 与 `@argus/ui` 的用户可见 JSX 字面量；共享资源授权双栏改为由业务应用显式传入本地化标签。
- 旧 `install-command` API/DTO、含糊 replacement 调用、静态步骤条、隐藏空 form、旧 localStorage schema 和过期断言均已删除，不存在兼容分支。
- P4 mock Playwright 与运行 `20260901-planv4-final41` 的 real Playwright 均通过；四组语言/主题/桌面视口无 serious/critical axe violation，敏感命令未进入浏览器持久化状态。

## 回归修复（2026-09-02）

- Mode A 一次性命令响应在服务端边界固定输出空数组而非 `null`，命令面板仍做输入归一化；空 warning 或空 instruction 集合不再触发 React `.map` 崩溃。
- 安装结果统一为单一 `command`；原一行/交互式/自动化三模式及其 Tab 已删除，系统级与用户级作用域选择继续保留。
- Scope 创建事务在根 Host 创建后立即写入 `connector_host_id`；删除事务不再允许缺少根 Host 关联时跳过清理。迁移 `00033_bastion_root_host_lifecycle.sql` 清理历史已删除 Scope 遗留的有效根 Host、回填现存关联，并约束每个 Scope 只能有一个有效根 Host。
- 名称冲突统一为不可重试的 `RESOURCE_NAME_CONFLICT`。软删除记录继续由条件唯一索引排除，同名 Scope 删除后可重新创建；并发有效同名资源仍由数据库兜底拒绝。
- 创建确认只在 Execution 未结束时指数退避查询；Scope/Connector 列表仅在 B/C 后台安装处于 `installing` 时轮询，模式 A 命令生成/领取后及成功、失败、取消、未知结果、超时均停止，并展示稳定的双语错误。
- Go 单元/契约测试、Enterprise/Platform typecheck 与单测、Mode A 删除后同名重建的定向 Playwright 用例均通过。
