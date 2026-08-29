# PlanV3 Task 02：统一决策与远程访问申请闭环

## 目标

把 Grant、explicit resource authorization、Rule、Workflow、SessionProfile 合并为唯一、可解释、可复现的远程访问决策，并贯通：

```text
Grant → Rule → Workflow → SessionProfile
      → AccessRequest → MFA/审批 → Lease → Session → 撤权
```

## 实施状态（2026-08-26）

Task 02 后端实现已经完成。`RemoteAccessDecisionService.Evaluate` 是申请创建、申请恢复、规则模拟和 Session 创建前二次评估的唯一授权入口；旧 `RemoteAccessPolicy` API、领域类型、SQL、权限和表已经删除，不保留双轨或回退路径。

本阶段已经落地：

- `AccessIntent`、`AccessDecision`、`DecisionOutcome`、reason code 和稳定 SHA-256 `SnapshotHash`。
- Request、Lease、Session 的不可变决策快照、来源对象 `ID + version` 和快照哈希。
- `awaiting_mfa` 状态及 `POST /enterprise/remote-access-requests/{id}/resume`。
- 多 Workflow Requirement、审批人数、职责分离、有效角色绑定、重复审批拒绝、超时和升级。
- Lease 签发前的状态、Requirement、有效期、AuthorizationVersion 和快照完整性检查。
- Session 创建前重新评估 Grant、Rule、explicit resource authorization、Host、ManagedAccount 和用户授权版本，并使用申请时固化的 SessionProfile。
- Grant、Rule、Workflow、SessionProfile、explicit resource authorization/RBAC、Host、ManagedAccount、用户和企业变化后的运行态撤权。
- Worker 每 30 秒通过 PostgreSQL 事务和 `FOR UPDATE SKIP LOCKED` 扫描 MFA 请求过期、审批升级和审批超时。
- `POST /enterprise/remote-access-rules/simulate`，与真实申请共用同一决策服务，只返回脱敏解释。
- 允许、等待和拒绝决策都记录 outcome、reason code、来源版本、AuthorizationVersion 和 `SnapshotHash`；命中 `notify` effect 时写入 `remote_access.decision.notify` Outbox。
- SessionProfile 的录像与命令审计 `required/optional/disabled` 模式已由 Session/Gateway 运行时执行，而不是只保存在配置或快照中。

开发迁移 `00017_remove_remote_access_policy.sql` 会清空旧 M6 Request/Lease/Session 运行数据并删除 Policy 表和兼容字段。该迁移有意不可逆，符合项目仍处于开发阶段的约束。

## 决策契约

### AccessIntent

`AccessIntent` 不包含密码、Ticket、Secret 或凭证原值。仓储适配器先从权威存储校验企业、用户、explicit resource authorization、Host 和 ManagedAccount，再加载有效 Grant、Rule、Workflow 和 SessionProfile。来源 IP、评估时间、Step-up 状态和 AuthorizationVersion 都进入决策输入；缺少必要事实或配置解析失败时 fail closed。

### AccessDecision

稳定字段包括：

- `Outcome`：`allowed`、`denied`、`awaiting_mfa`、`awaiting_approval`。
- `MatchedGrantSnapshots`、`MatchedRuleSnapshots`：稳定排序的 `ID + version`。
- `ApprovalRequirements`：Workflow 版本、审批角色、最少人数、职责分离、截止时间语义、超时结果、升级角色和来源 Rule。
- `SessionProfile`：合并后的不可变会话限制及来源 Profile 版本。
- `AuthorizationVersion`、reason code、脱敏解释和 `SnapshotHash`。

哈希输入采用固定字段顺序，不包含解释文案和 Secret。相同事实集合不受数据库返回顺序影响；Request、Lease、Session 每次读取快照时都重新计算并校验哈希，篡改或缺失直接拒绝。

### Rule 与义务合并

- 没有有效 Grant 时直接拒绝，Rule 不能补齐基础资格。
- 有效 Grant 是正向访问基线；没有匹配 Rule 时直接允许，并冻结系统安全默认 SessionProfile。
- 只评估同企业、`enabled`、协议/动作和 selector/CIDR/时间窗口匹配的 Rule。
- 任意 `deny` 覆盖全部允许结果，且不受 priority 影响。
- 多 Rule 的 MFA、审批和通知全部合并；同一 Workflow 在一次申请中去重。
- 同一 Workflow 的最少审批人数取最大值、职责分离取 OR、审批超时取最短值、`reject` 优先于 `expire`、升级角色取并集。
- 多 SessionProfile 的时长取最小值，录像和命令审计按 `required > optional > disabled`，交互能力只要一个来源禁用即禁用，留存期取最大值。

## 请求、MFA 与审批

`CreateRequest` 只调用统一决策服务。拒绝结果返回稳定错误且不创建可继续流转的申请；其他结果持久化不可变快照并进入：

```text
awaiting_mfa → awaiting_approval → authorized
             ↘ authorized
```

`resume` 只允许申请人恢复自己的 `awaiting_mfa` 申请，重新读取当前 HTTP Session 的服务端 Step-up 状态并再次执行完整决策。接口不接受 MFA proof 字段；未满足 fresh Step-up 时仍保持 `awaiting_mfa`。规则或来源版本发生变化时，新快照替换旧等待快照；Grant 不再匹配或 AuthorizationVersion 变化时申请失效。

每个 Workflow 生成独立 Requirement。审批人必须在当前时刻拥有快照角色的有效绑定；`separation_of_duties=true` 时申请人不能自批。`minimum_approvals` 达标后 Requirement 才变为 `satisfied`，所有 Requirement 满足后才把 Request 置为 `authorized` 并在同一事务签发 Lease。

Workflow 通过 `escalation_after_seconds` 显式配置单次升级阈值，该值至少为 30 秒且必须早于 `approval_timeout_seconds`。申请创建时 Requirement 冻结绝对时间 `escalation_at`；Worker 到时写入 `escalated_at`、Outbox 和系统审计，不修改审批快照或人数。截止时间到达后，`reject` 将 Request 置为 `rejected`，`expire` 将其置为 `expired`。

## Lease 与 Session

Lease 只从 `authorized`、未过期、所有 Requirement 已满足且快照完整的 Request 签发，并保存完整决策快照、Profile 快照和哈希。AuthorizationVersion 不一致时拒绝签发。

创建 Session 时重新执行统一决策；当前 Grant/Rule/Profile 版本必须与申请快照一致，当前决策不能变成拒绝或重新要求 MFA。Session 的时长、空闲超时、录像、命令审计、剪贴板、文件、端口转发、分享和留存全部来自冻结 Profile 快照，不读取可变配置。

Session/Gateway 已执行冻结 Profile 的基础模式语义：`recording_mode=required` 时录像初始化、打开或持久化失败会阻止或终止 Session；`optional` 失败时记录降级审计和 Outbox，但不终止 Session；`disabled` 不创建录像元数据也不启动录像通道。命令审计同样按 `required/optional/disabled` 处理，required 写入失败会终止 Session，optional 降级，disabled 不持久化命令事件。剪贴板、文件、端口转发和分享等未交付通道继续保持关闭。

## 撤权与 Worker

授权敏感变化通过同一运行态撤权组件处理：

- 递增受影响用户的 AuthorizationVersion。
- 将未完成或仍可使用的 Request 标记为 `invalidated`。
- 撤销未过期 Lease。
- 增加 Session fence、将活动 Session 置为失效并写入终止 Outbox。
- 写入撤权摘要 Outbox；各配置或资源变更入口继续写自身审计事件。

Rule、Workflow 和 SessionProfile 通过决策 JSON 快照查询引用；Grant、Host 和 ManagedAccount 使用规范化列查询。RBAC/explicit resource authorization、用户和企业变化按受影响用户集合撤权。Redis 仅承载终止通知和加速，PostgreSQL 始终是授权事实来源。

Worker 默认每 30 秒运行，批量锁定并处理：

1. 已超过 `expires_at` 的 `requested/awaiting_mfa` Request。
2. 已达到冻结 `escalation_at` 且未升级的 Requirement。
3. 已超过 `deadline_at` 的 pending Requirement。

所有状态谓词、`escalated_at IS NULL` 和 `SKIP LOCKED` 共同保证重试、并发 Worker 和 PostgreSQL 恢复后的幂等语义。

## 任务清单

- [x] P3-DECISION-01 统一决策服务、Grant 基线授权、可选 Rule、Deny 优先和稳定 reason code。
- [x] P3-DECISION-02 多 Rule/Workflow/Profile 严格合并和稳定 SnapshotHash。
- [x] P3-REQUEST-01 Request 保存 Grant/Rule/Workflow/Profile 版本和完整快照。
- [x] P3-REQUEST-02 `awaiting_mfa`、服务端 Step-up 和 `resume`。
- [x] P3-APPROVAL-01 多 Requirement、职责分离、人数、超时和单次升级。
- [x] P3-LEASE-01 Lease 完整性检查和 Session 二次评估。
- [x] P3-REVOKE-01 配置、授权、资源、账号、用户和企业变化后的运行态撤权。
- [x] P3-SIMULATE-01 与真实申请共用决策服务的脱敏规则模拟 API。
- [x] P3-AUDIT-01 决策摘要、状态变化、超时、升级和撤权审计/Outbox。
- [x] P3-CLEANUP-01 删除 RemoteAccessPolicy API、代码、权限、生成客户端和数据库表。
- [x] P3-PROFILE-01 执行录像和命令审计的 required/optional/disabled 基础运行时语义。

## 后续边界

Task 03 继续完成 Gateway HA、对象存储可靠性、录像灾备、生产日志脱敏和剪贴板/文件/端口转发/分享等高级通道；基础 Profile 快照、录像模式和命令审计模式已在本阶段落实。Task 04 实现四个治理 Tab、审批中心和远程会话控制台；Task 02 不增加新的一级导航。

## 2026-08-26 验证记录

已通过契约 lint/generate/check/breaking、sqlc、`go test ./...`、`go vet -stdmethods=false ./...`、真实 PostgreSQL 18 全量 migration/约束/down-up 重入、API client 与 Enterprise typecheck、Enterprise build/test、全仓 Web lint、`git diff --check` 和 2000 行门禁。M6 场景种子已补充 required 录像/命令审计 Profile、MFA Rule、两人审批 Workflow、职责分离、自批拒绝、审批达标签发 Lease，以及停用命中 Rule 后 Lease 撤销断言。

已在一次性 PostgreSQL 18 容器中执行正式迁移删除验证：`00017_remove_remote_access_policy.sql` 执行后，`remote_access_policies` 表、`policy_id/policy_version/policy_snapshot_hash` 兼容字段和旧 `remote_access.policy.*` 权限均不存在，开发阶段的 Request/Lease/Session 运行数据已清空；随后完成 down/up 重入验证。该验证未触碰当前 `docker-desktop` 正式环境。

随后已在显式确认重置保护的 `docker-desktop` Evaluation 集群完成 `20260826-planv3-final9`。该运行覆盖 MFA `awaiting_mfa → resume`、双人审批、职责分离、自批拒绝、Lease、SSH/WinRS、录像、Ticket 重放拒绝、Rule 停用撤权、required ObjectStore fail closed、Redis 清空、跨 Gateway、Gateway 删除/Drain、Worker 重启和 PostgreSQL Pod 恢复；恢复前后 Grant、Request、Lease、Session、Recording 事实数量保持不变。`argusctl verify passed=true`，最终清理无运行 Namespace、PVC、Lease、image-loader DaemonSet 或 Registry 容器残留，证据位于 `artifacts/m6-e2e/20260826-planv3-final9/`。此前预检阻断记录保留为历史环境证据，不再代表当前 Task 02 状态。
