# PlanV3 Task 04：企业控制台与端到端验收

## 目标

在组织与权限 → 远程访问下提供四个一致的配置 Tab，并让管理员能够理解、模拟、编辑、停用、恢复和审计远程访问配置。

## 页面结构

```text
组织与权限
└── 远程访问
    ├── 访问授权
    ├── 访问规则
    ├── 审批流程
    └── 会话策略
```

运行态不放入组织权限配置页：访问申请与审批统一进入“执行治理 → 审批中心”，远程会话单独作为一级菜单；企业控制台当前只验收桌面端，移动端不属于本版本目标。

```text
执行治理
├── 任务记录
└── 审批中心
    ├── 操作审批
    └── 远程访问
        ├── 待我审批
        ├── 我发起的（访问申请）
        └── 已处理

远程会话
├── 活动会话
├── 历史会话
└── 会话录像
```

不新增独立的“访问申请”一级菜单。申请从主机详情发起，在审批中心的“我发起的”中跟踪；待审批继续使用统一审批收件箱。页面标题“待审批”应调整为“审批中心”，以覆盖待办、我发起的和已处理三种视图。

## 任务清单

### 远程终端 Dock 契约

远程终端在企业后台以与页面内容切分视口的 Terminal Dock 呈现（非遮罩，类似浏览器 DevTools 的 Split 行为）：默认与页面上下各占 50%，支持鼠标/键盘拖拽调整到 20%～80%（双击分隔条恢复默认），并可在底部/左侧/右侧三向停靠之间切换，位置与尺寸持久化在 `argus.terminalDock`；独立终端窗口方案推迟。Dock 收起、页面路由切换和终端标签隐藏都不得关闭 WebSocket；标签关闭只隐藏会话，真正终止必须通过 Dock 工具栏或活动会话列表的“终止会话”操作。页面刷新或浏览器断开同样不终结会话——后端驻留 PTY 并缓冲输出，用户可从活动会话列表“进入”重接同一终端并回放缓冲；会话仅因显式终止、空闲超时（无输入且无输出）或最长时长终结。活动会话列表的“进入”入口只对本地仍持有连接的会话提供（后端仅对 `authorized` 状态会话签发 ticket，重新 attach 必然失败）。终端组件使用一次创建、增量写入的 xterm PTY，保留远端提示符、当前路径、ANSI 控制序列和光标；`server_ready` 语义与远端 shell 启动进度对齐（详见 `docs/implementation-ssh-prompt-fix.md`）。

远程 Gateway 的 WSS 端点与企业门户同源：`ARGUS_REMOTE_ORIGIN` 固定为 `https://<enterpriseHost>`，路径 `/v1/sessions` 由 Ingress 分流到 `argus-connector-gateway:9445`。本地 Docker Desktop 环境只需映射三个门户域名（`argus.dev`、`platform.argus.dev`、`cards.argus.dev`），浏览器复用页面已信任的同一张多 SAN 证书，无独立终端域名、无额外 hosts 或证书步骤。

- [x] P3-UI-01 拆分远程访问页面，治理对象使用独立组件和统一 @argus/ui。
- [x] P3-UI-02 访问授权支持新建、停用以及治理对象统一的引用保护模式。
- [x] P3-UI-03 访问规则采用“匹配范围 → 处理动作 → 审批流程 → 会话策略 → 摘要”的分步表单。
- [x] P3-UI-04 审批流程提供角色、人数、职责分离、超时、升级和引用关系管理。
- [x] P3-UI-05 会话策略提供时长、空闲、录像、命令审计、剪贴板、文件、端口转发和分享控制。
- [x] P3-UI-06 所有 Switch 提供标题、说明、当前值和高级通道风险提示。
- [x] P3-UI-07 增加规则模拟器页面，调用 Task 02 已提供的模拟 API，展示最终 outcome、reason code 和 SnapshotHash。
- [x] P3-UI-08 停用操作展示影响范围；已停用对象提供恢复或归档，不提供误导性的永久删除。
- [x] P3-UI-09 RemoteAccess Request 已接入统一审批中心远程访问页签。
- [x] P3-UI-10 增加远程会话一级菜单及活动会话、历史会话、会话录像三个 Tab，并支持录像关联。
- [x] P3-UI-11 `/approvals` 页面标题已调整为“审批中心”，保留操作审批和远程访问两个一级 Tab。
- [x] P3-I18N-01 为远程访问治理、审批和远程会话模块增加独立中英文文案，并在模块清单注册。
- [x] P3-E2E-01 已增加四 Tab、规则模拟器和远程会话中心的桌面端 mock 测试，并覆盖治理对象创建、校验、启用、停用、归档、恢复、详情、版本和模拟结果。
- [x] P3-E2E-02 真实 Kubernetes Suite 已覆盖授权、MFA、双人审批、会话、录像、撤权、故障恢复和无条件清理。
- [x] P3-A11Y-01 新增控件使用语义标签、键盘可操作按钮和 design token；`zh-CN/en-US`、亮色/暗色和正式桌面视口已通过 mock 与 real Playwright 验收。

### Dock 重构补充验收（2026-08-28）

- [x] TerminalSessionProvider 生命周期单测覆盖隐藏/恢复、显式终止和重复终止幂等。
- [x] TerminalDock 尺寸边界单测覆盖 20%～80%；鼠标拖拽使用 `.argus-app-workspace` 实际工作区计算比例。
- [x] xterm PTY 单测覆盖单实例、原始 chunk 增量 `write()` 和 ANSI/换行字节不改写。
- [x] Gateway 单测覆盖 ping 不刷新业务空闲时间；输入、resize、输出仍刷新活动时间。
- [ ] 真实 Kubernetes Playwright 仍需在配置 `ARGUS_M6_E2E=1` 的 Evaluation 环境执行 Dock 收起、路由切换和 WebSocket 保持场景；本机无该环境时不宣称通过。

## E2E 核心场景

1. 只有有效 Grant、没有匹配 Rule 时申请直接授权并使用系统安全默认 Session Profile；模拟器展示相同决策。
2. 创建 Profile-only Rule，或叠加 MFA/审批/通知 Rule，并在模拟器看到预期决策。
3. Evaluation SSH 访问命中 MFA + 双人审批，申请人无法自批；生产环境仍需独立安全与兼容性验证。
4. 审批完成后只能获得绑定目标、账号、协议和版本的 Lease。
5. 申请人在审批中心“我发起的”中查看申请进度，审批人在“待我审批”中处理，操作审批和远程访问审批使用同一收件箱模型。
6. 停用 Grant 立即撤销 Lease 并终止活动会话。
7. 停用 Workflow 不影响已有申请快照；停用 Profile 不改变已有 Session。
8. 规则版本冲突不会覆盖其他管理员的修改。
9. 活动会话可以被授权运维人员查看和终止，历史会话保留完整快照。
10. 会话录像可从历史会话打开，也可从录像列表反向定位会话；读取行为写入审计。
11. 录像读取、命令审计和配置变更均可追溯到企业用户和版本。
12. 测试失败也清理临时 Namespace、PVC、Topic、Bucket、Lease 和测试凭据。

## 退出标准

执行 `cd web/apps/enterprise && pnpm e2e`、`go run ./cmd/argus-dev e2e run --suite m6` 和 PlanV3 专项 Suite；中文/英文、亮色/暗色和正式桌面视口无严重/致命可访问性问题，后端契约、Go 测试、前端 typecheck/lint/build 和生产制品检查全部通过。移动端不属于当前产品体验或 E2E 验收范围。

## 第三阶段实现记录

2026-08-26 已在独立端口完成桌面端 mock Playwright 全量运行：38 个场景通过，33 个场景按产品或环境保护条件跳过，无失败；跳过项包括不在当前验收范围的移动端场景和只能由真实发布环境执行的场景。专项治理场景覆盖 Session Profile、Workflow 和 Rule 的创建、字段校验、启用、停用、归档、恢复、详情版本与规则模拟。

同日，受保护的 Docker Desktop Kubernetes Evaluation 环境完成真实运行 `20260826-planv3-final9`：M2 real Playwright 3/3、M3 6/6、M6 5/5 全部通过，并验证 `awaiting_mfa → resume`、双人审批、申请人自批拒绝、Lease、SSH/WinRS、录像读取、Ticket 重放拒绝、Rule 停用撤权、required ObjectStore fail closed、Redis 清空、Gateway 删除/Drain、Worker 重启和 PostgreSQL Pod 恢复。清理后未残留运行 Namespace、PVC、Lease、image-loader DaemonSet 或 Registry 容器；证据位于 `artifacts/m6-e2e/20260826-planv3-final9/`。

Task 04 已并入第三阶段，控制台与真实运行链路的开发/Evaluation 退出标准已经满足。该结果不构成 Production 认证；生产 NetworkPolicy 执行、WORM/Object Lock、跨故障域灾备、生产 PostgreSQL/ObjectStore HA、外部 Egress Gateway、强化 Sandbox Runtime 和真实 Windows 兼容性继续单独验收。
