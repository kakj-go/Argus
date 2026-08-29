# M6：人工远程访问闭环

## 目标

交付与 Agent、Card、PendingAction 和 Execution 完全隔离的人工远程访问闭环：

`RemoteAccessGrant → RemoteAccessRule/ApprovalWorkflow/SessionProfile → AccessRequest/Approval → AccessLease → Session/Ticket → SSH/WinRS → 录像/终止/撤权`

M6 只达到 Evaluation 完成标准。M8 已补齐本地 TOTP/Step-up、OpenBao 与录像备份恢复；Production 容量、Object Lock、真实 Windows 兼容矩阵和安全审计仍由独立 Production Validation 阻断。

## 已完成任务

- [x] `M6-GRANT-01` 实现 user/department Grant、显式 Host ID、ManagedAccount、协议、动作、有效期和版本化管理；资源范围由 DataAuthorizationGrant 提供，主机标签仅用于展示和筛选。
- [x] `M6-LEASE-01` 实现 AccessRequest、多 Workflow Requirement Snapshot、审批决定和 15 分钟 AccessLease；PlanV3 Task 02 已将旧 Policy 输入替换为 Rule/Workflow/SessionProfile 统一决策。
- [x] `M6-SESSION-01` 实现 Session 状态机、并发限制、连接窗口、闲置/最长时长、终止和 AuthorizationVersion 复核。2026-08-29 起浏览器断开（页面刷新/关闭）不再终结会话：Gateway 将 PTY 后端驻留在实例内注册表（`SessionParks`），继续录像并把输出写入有界回放缓冲（256KiB），会话对持有有效票据的同一用户可重接（重接票据仅允许 `authorized`/`active` 状态、同一登录会话）；只有显式终止、空闲超时（无输入且无输出）、最长时长或租约过期才落终态。多副本下驻留路由当前限制为单实例（Evaluation 单副本），跨实例重接返回 REMOTE_ACCESS_CONNECTION_LOST 并由租约到期兜底。
- [x] `M6-TICKET-01` 实现 256 位 opaque Ticket，数据库只保存 SHA-256 Hash，并绑定 HTTP Session、用户、企业、Host、账号、协议、Lease、授权版本和 Fence。
- [x] `M6-SSH-01` 实现 Connector/Direct SSH 完整 PTY、Host Key 校验、窗口调整、输入输出、心跳和强制关闭。
- [x] `M6-WINRM-01` 实现 HTTPS 443/5986 WinRS PowerShell 行模式；不宣称完整 PTY、ConPTY 或 PSRP。
- [x] `M6-GATEWAY-01` 实现外部 WSS `9445`、内部 peer mTLS `9446`、Kubernetes Pod owner 解析、跨副本路由、Redis/PostgreSQL 恢复和 30 秒 Drain。
- [x] `M6-RECORD-01` 实现 asciicast v2 NDJSON `i/o/r/m` 事件、AES-256-GCM 分片、DEK 包裹、SHA-256 Hash Chain、授权增量读取和 90 天默认保留。
- [x] `M6-REVOKE-01` 实现 Grant、Rule、Workflow、SessionProfile、explicit resource authorization、标签、ManagedAccount、用户和企业变化后的 Request/Lease/Session 撤权。
- [x] `M6-WEB-01` 使用 `@xterm/xterm` 和 fit addon 实现 SSH 终端、WinRS 行模式、Grant、审批、终止与录像播放器；PlanV3 Task 02 将 MFA 请求固化为 `awaiting_mfa`，完成正式 Step-up 后通过 `resume` 继续申请。
- [x] `M6-DEPLOY-01` 增加 Remote WSS Service/Ingress/Certificate、ObjectStore、Origin、并发限制、NetworkPolicy、PDB、Gateway peer RBAC 和独立 Direct Executor 客户端身份。
- [x] `M6-E2E-01` 增加 `go run ./cmd/argus-dev e2e run --suite m6`，覆盖真实 SSH、TLS WinRS 模拟器、跨 Gateway、Drain、Redis 清空、MinIO 中断、录像、撤权和 real Playwright；PlanV3 第三阶段新增的治理控制台、统一审批中心和远程会话中心使用独立 Playwright 场景验收。

- [x] 远程终端改为同源独立桌面窗口：主页面在用户手势期间预打开窗口，Ticket 仅通过 `postMessage` 传递；窗口内显示 connecting/connected/disconnected 状态和 WebSocket 错误，终止操作与主页面同步。（该方案已被 Terminal Dock 取代并推迟，当前实现见 `docs/implementation-ssh-prompt-fix.md` 与 `docs/planv3/task-04-enterprise-console-and-e2e.md` 的 Dock 契约。）
- [x] 终止接口对 `terminating` 和终态会话幂等返回，Gateway 使用最新 Session Fence 完成收尾；Worker 对超过 2 分钟未收敛的 `terminating` 会话执行失败终态兜底并记录审计。

PlanV3 Task 01 已在 M6 底座之上增加 Rule、ApprovalWorkflow、SessionProfile 三个独立治理对象及其迁移、API、权限和版本契约。Task 02 已完成直接切换：旧 `RemoteAccessPolicy` API、代码、权限和表全部删除，统一决策服务贯通 Request、MFA/审批、Lease、Session 二次评估和撤权；Workflow 支持可配置升级阈值，拒绝决策和 `notify` effect 分别进入审计与 Outbox。

## 已冻结实现语义

- AccessLease 有效 15 分钟，可重复创建 Session；每次创建都通过统一决策服务重新校验权限、explicit resource authorization、Grant、Rule/Workflow/Profile 来源版本和 AuthorizationVersion，并使用冻结 Profile 快照。
- RemoteAccessGrant 是正向基础访问资格；RemoteAccessRule 是可选限制层。没有匹配 Rule 时申请可直接授权，并使用录像和命令审计 required、高级通道 disabled 的系统安全默认 Profile。
- Ticket 有效 60 秒且只能消费一次；URL 不携带 Ticket，浏览器只在终端组件内存中持有。
- 登录阶段的 MFA 只证明登录会话，不能替代远程访问策略要求的 fresh Step-up；浏览器必须调用 `/enterprise/auth/step-up`，不得直接绕过或由 Playwright 代调 API。
- SSH 是完整 PTY。WinRM 路径固定为 HTTPS WinRS PowerShell 行输入输出，不提供完整 PTY/PSRP。
- Gateway 停止新连接后等待 30 秒，再终止剩余会话；显式跟踪升级后的 WebSocket，Session 和 Recording 落盘完成后才关闭 peer gRPC 和 PostgreSQL。
- Gateway peer 通过 Kubernetes API 获取 Ready owner Pod IP；ServiceAccount 只有同 Namespace Pod `get` 权限，peer 使用固定 mTLS 服务身份。
- ObjectStore 单次调用有 3 秒边界；required 录像连续不可用 30 秒或缓冲超过 4 MiB 时会话 fail closed，optional 模式记录降级并继续，disabled 模式不启动录像通道。
- 录像只在内存中短暂缓冲，密文分片写入 S3-compatible ObjectStore；PostgreSQL 保存索引、Hash Chain 和按 Profile 模式控制的命令哈希，不保存终端明文。命令审计 required 写入失败会终止会话，optional 降级，disabled 不写命令事件。
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

2026-08-26 已更新 M6 场景种子，新增 MFA Rule、两人审批 Workflow、职责分离、自批拒绝、审批达标签发 Lease，以及停用命中 Rule 后 Lease 被撤销的断言。随后在显式确认重置的 `docker-desktop` Evaluation 集群完成 `20260826-planv3-final9`：M2 real Playwright 3/3、M3 6/6、M6 5/5 全部通过，并覆盖 MFA `awaiting_mfa → resume`、双人审批、Lease、SSH/WinRS、录像、Ticket 重放拒绝、Rule 撤权、required ObjectStore fail closed、Redis 清空、跨 Gateway、Gateway 删除/Drain、Server/Worker 双副本、Worker 全量 Pod 重启和 PostgreSQL Pod 恢复。恢复后 Grant、Request、Lease、Session、Recording 事实数量保持不变，`argusctl verify passed=true`；最终未残留运行 Namespace、PVC、Lease、image-loader DaemonSet 或 Registry 容器。证据位于 `artifacts/m6-e2e/20260826-planv3-final9/`。2026-08-27 又增加了 Grant 已启用但尚未启用任何 Rule 时，申请直接进入 `authorized` 并签发 Lease 的回归断言。

该运行关闭 PlanV3 第三阶段开发/Evaluation 的真实集群门禁；结合 `artifacts/m8-e2e/fv-20260824-m8-final13/` 的人工灾备演练，PlanV3 的开发、Evaluation 和人工测试项均可标记完成。但这不代表 Production 认证；WORM/Object Lock、PITR、跨故障域灾备、生产 PostgreSQL/ObjectStore HA、生产 NetworkPolicy 执行、外部 Egress Gateway、强化 Sandbox Runtime 和真实 Windows 兼容性仍未宣称完成。

同日已在一次性 PostgreSQL 18 容器中完成旧 Policy 正式删除迁移验证：旧表、旧兼容字段和旧权限均已确认不存在，开发运行态数据清空后 down/up 重入成功；当前正式集群数据未被修改。

## 不包含

- MFA、Step-up、Break Glass 已由 M8 本地加固实现，不属于 M6 自身状态机。
- RDP、SFTP、剪贴板、文件传输、分享和通用端口转发。
- Production 录像不可变保留、跨故障域备份恢复和真实 Windows Server 兼容矩阵。
- Production 容量基线、渗透测试和安全审计。

以上 Production 项目继续由独立 Production Validation 清单管理，不影响 M8 的 `local_hardening_complete` 判断。
