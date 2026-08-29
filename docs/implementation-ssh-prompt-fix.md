# SSH 终端提示符修复 - 当前实现

> 本文按 2026-08-29 的 Terminal Dock 与 ready 语义重构更新。此前“等待首个输出 3 秒后发送 ready”的方案已废弃；2026-08-28 版本中“Connector 在 SSH 建立前即返回、Gateway 提前发送 server_ready”的语义亦已修正。

## 问题与原因

SSH PTY 的提示符、当前用户、主机名、工作目录和 ANSI 光标都由远端 shell 输出。旧前端把输出当作按行文本处理，且连接建立时可能早于组件挂载，导致首个提示符丢失或被前端伪造。2026-08-28 之前还存在第二个问题：Connector 路径下 `RemoteAccessHub.Open` 只把 open 帧入队就返回，浏览器在 SSH 真正拨号、shell 启动之前就收到 `server_ready`，表现为“已连接 + 黑屏 + 无法输入”。

## 当前方案

1. **就绪语义与远端 shell 进度对齐**：Direct Executor 在 `RequestPty`+`Shell()` 成功并启动输出读取后才发送 `Ready`；Connector 路径 `RemoteAccessHub.Open` 在入队 open 帧后等待 Connector 回传首个 `state: active`（SSH shell 已启动 / WinRS shell 已创建）才返回，Gateway 此时才发送 `server_ready`。等待上限 15 秒（`HandshakeTimeout` 可配），失败、超时或 Connector 断连按会话不可用处理并向浏览器返回明确错误，不再出现假“已连接”。
2. 握手失败清理时会向 Connector 发送 `RemoteAccessClose` 终止远端会话；已移除 stream 的迟到上行帧由 `Deliver` 静默丢弃，不会判伤整条 Connector 连接。
3. Gateway 按顺序转发原始 output frame；首个 chunk 与后续 chunk 使用同一条通道。
4. `TerminalSessionProvider` 在应用根部保存会话和输出缓冲，页面路由或 Dock 收起不会卸载连接。
5. `TerminalEmulator` 仅在 `sessionId + mode` 变化时创建 xterm，初始化时先写入当前缓冲，后续以 `write()` 增量写入；`autoFocusKey` 变化（Dock 展开、切换会话标签）时重新 focus。
6. PTY 不使用 `writeln()`、`clear()` 重放或前端伪造的 `$` 提示符；远端 ANSI 控制序列和光标由 xterm 原样解释。Dock 头部展示 `account@host` 附加信息，但不伪造提示符。
7. xterm 挂载后执行 `fit()` 和 `focus()`，ResizeObserver 只调整尺寸并发送 resize，不清空内容。
8. 独立终端窗口（弹出窗口）方案推迟，本期 Dock 支持底部/左侧/右侧三向停靠，位置与尺寸持久化在 `argus.terminalDock`。
9. 浏览器断开不终结会话：Gateway 将后端驻留（`SessionParks`）并缓冲输出，重接时回放缓冲；会话仅因显式终止、空闲/最长超时或租约过期落终态（详见 `docs/plans/M6-remote-access.md` 的 M6-SESSION-01）。

## 涉及代码

- `internal/directexecutor/rpc_remote.go`
- `internal/connector/remote_hub.go`（`Open` 等待 ACTIVE、迟到帧丢弃）
- `internal/app/connector/remote_session.go`
- `internal/remoteaccess/websocket_gateway.go`
- `web/packages/api-client/src/contexts/terminal-session-context.tsx`
- `web/packages/ui/src/terminal.tsx`
- `web/apps/enterprise/src/components/hosts/terminal-dock.tsx`
- `web/apps/enterprise/src/store/ui.ts`（`argus.terminalDock` 偏好）
- `tests/e2e/sshserver/main.go`（PTY ECHO/ICRNL 行为模拟）

## 验证要求

- 单测验证 xterm 单实例、原始 chunk 增量写入和晚挂载缓冲。
- Gateway 测试验证输入、resize、输出刷新业务空闲时间，而 ping 不刷新。
- Hub 单测（`internal/connector/remote_hub_test.go`）验证等待 ACTIVE、终态失败、超时与 Connector 断连四个分支。
- 真实 E2E（`web/apps/enterprise/e2e/m6-real.spec.ts` SSH PTY 用例）验证首个 prompt 与 banner 可见、键盘输入 `whoami` 有回显输出、三向停靠切换、Dock 收起/展开后缓冲与会话保持。
