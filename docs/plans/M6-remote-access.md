# M6：人工远程访问闭环

## 目标

交付与 Agent、Card、PendingAction 和 Execution 完全隔离的人工远程访问闭环：

`RemoteAccessGrant → AccessRequest/Approval → AccessLease → Session/Ticket → SSH/WinRS → 录像/终止/撤权`

M6 只达到 Evaluation 完成标准。M8 已补齐本地 TOTP/Step-up、OpenBao 与录像备份恢复；Production 容量、Object Lock、真实 Windows 兼容矩阵和安全审计仍由独立 Production Validation 阻断。

## 已完成任务

- [x] `M6-GRANT-01` 实现 user/department Grant、显式 Host ID/标签选择器、ManagedAccount、协议、动作、有效期和版本化管理。
- [x] `M6-LEASE-01` 实现独立 RemoteAccessPolicy、AccessRequest、多策略 Requirement Snapshot、审批决定和 15 分钟 AccessLease。
- [x] `M6-SESSION-01` 实现 Session 状态机、并发限制、连接窗口、闲置/最长时长、终止和 AuthorizationVersion 复核。
- [x] `M6-TICKET-01` 实现 256 位 opaque Ticket，数据库只保存 SHA-256 Hash，并绑定 HTTP Session、用户、企业、Host、账号、协议、Lease、授权版本和 Fence。
- [x] `M6-SSH-01` 实现 Connector/Direct SSH 完整 PTY、Host Key 校验、窗口调整、输入输出、心跳和强制关闭。
- [x] `M6-WINRM-01` 实现 HTTPS 443/5986 WinRS PowerShell 行模式；不宣称完整 PTY、ConPTY 或 PSRP。
- [x] `M6-GATEWAY-01` 实现外部 WSS `9445`、内部 peer mTLS `9446`、Kubernetes Pod owner 解析、跨副本路由、Redis/PostgreSQL 恢复和 30 秒 Drain。
- [x] `M6-RECORD-01` 实现 asciicast v2 NDJSON `i/o/r/m` 事件、AES-256-GCM 分片、DEK 包裹、SHA-256 Hash Chain、授权增量读取和 90 天默认保留。
- [x] `M6-REVOKE-01` 实现 Grant、Policy、DataScope、标签、ManagedAccount、用户和企业变化后的票据/Lease/Session 撤权。
- [x] `M6-WEB-01` 使用 `@xterm/xterm` 和 fit addon 实现 SSH 终端、WinRS 行模式、Grant/Policy、审批、终止与录像播放器；命中 `REMOTE_ACCESS_MFA_REQUIRED` 时通过正式 Step-up 对话框获取 fresh MFA proof，成功后自动重试原 AccessRequest。
- [x] `M6-DEPLOY-01` 增加 Remote WSS Service/Ingress/Certificate、ObjectStore、Origin、并发限制、NetworkPolicy、PDB、Gateway peer RBAC 和独立 Direct Executor 客户端身份。
- [x] `M6-E2E-01` 增加 `go run ./cmd/argus-dev e2e run --suite m6`，覆盖真实 SSH、TLS WinRS 模拟器、跨 Gateway、Drain、Redis 清空、MinIO 中断、录像、撤权和 real Playwright。

## 已冻结实现语义

- AccessLease 有效 15 分钟，可重复创建 Session；每次创建都重新校验权限、DataScope、Grant、Policy 和 AuthorizationVersion。
- Ticket 有效 60 秒且只能消费一次；URL 不携带 Ticket，浏览器只在终端组件内存中持有。
- 登录阶段的 MFA 只证明登录会话，不能替代远程访问策略要求的 fresh Step-up；浏览器必须调用 `/enterprise/auth/step-up`，不得直接绕过或由 Playwright 代调 API。
- SSH 是完整 PTY。WinRM 路径固定为 HTTPS WinRS PowerShell 行输入输出，不提供完整 PTY/PSRP。
- Gateway 停止新连接后等待 30 秒，再终止剩余会话；显式跟踪升级后的 WebSocket，Session 和 Recording 落盘完成后才关闭 peer gRPC 和 PostgreSQL。
- Gateway peer 通过 Kubernetes API 获取 Ready owner Pod IP；ServiceAccount 只有同 Namespace Pod `get` 权限，peer 使用固定 mTLS 服务身份。
- ObjectStore 单次调用有 3 秒边界；连续不可用 30 秒或缓冲超过 4 MiB 时会话 fail closed。
- 录像只在内存中短暂缓冲，密文分片写入 S3-compatible ObjectStore；PostgreSQL 保存索引、Hash Chain 和命令哈希，不保存终端明文。
- 剪贴板、文件、分享和端口转发均关闭；RemoteAccessTicket 永不提供给 Agent、Card 或 Sandbox。

## 验收证据

2026-08-18 的旧 Shell Harness 最终成功运行号为 `20260818072400-79219`，脱敏诊断位于 `artifacts/m6-e2e/20260818072400-79219`。

该运行验证：

- M2 三门户真实身份和 M3 资源/Connector 基线。
- Direct SSH PTY、Direct HTTPS WinRS PowerShell 行模式、Ticket 重放拒绝和加密录像读取。
- 非 owner Gateway WSS → owner Gateway peer → Connector 的跨副本路由，Redis 清空后的 PostgreSQL fallback，以及真实 Pod 删除后的 `gateway_drain`。
- AuthorizationVersion 变化后旧 AccessLease 返回 `AUTHORIZATION_VERSION_STALE`，重新申请后才可创建 Session。
- MinIO 连续中断超过 30 秒时返回 `REMOTE_ACCESS_RECORDING_UNAVAILABLE`，PVC 恢复后 Bucket 和密文事实保留。
- `zh-CN/en-US × light/dark` 下 Grant、审批、SSH/WinRS、终止和录像页面无严重/致命 a11y 违规。
- Redis 停止后已有 Session 与审计仍由 PostgreSQL 恢复，新登录 fail closed；恢复后撤权继续生效。

退出清理确认三个临时 Namespace、PVC 和集群 Lease 均已删除。

同日最终全量门禁通过：`go run ./cmd/argus-dev contracts check`、`go run ./cmd/argus-dev contracts breaking`、`go test ./...`、`go vet -stdmethods=false ./...`、`pnpm typecheck lint test build check:bundle check:real-build e2e`、`go run ./cmd/argus-dev check production-artifacts` 和 `git diff --check`。Remote Access sqlc 查询按治理、授权和运行时拆分，项目内 Go/TypeScript/TSX 单文件均低于 2000 行。

## 不包含

- MFA、Step-up、Break Glass 已由 M8 本地加固实现，不属于 M6 自身状态机。
- RDP、SFTP、剪贴板、文件传输、分享和通用端口转发。
- Production 录像不可变保留、跨故障域备份恢复和真实 Windows Server 兼容矩阵。
- Production 容量基线、渗透测试和安全审计。

以上 Production 项目继续由独立 Production Validation 清单管理，不影响 M8 的 `local_hardening_complete` 判断。
