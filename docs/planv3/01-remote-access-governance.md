# PlanV3-01：远程访问治理总体设计

> 远程访问只复用 Host 显式授权结果，不再解析 Host 或主体标签过滤条件。标签仅用于页面筛选和展示；ManagedAccount、协议、动作、审批和有效期约束保持不变。

## 1. 设计目标

远程访问必须同时满足：

- 资源范围明确，不能因管理员身份隐式获得生产 Shell。
- 配置对象可复用，审批和会话限制不复制到每条授权中。
- 管理员可以解释一次访问为什么被允许、拒绝或要求审批。
- 授权、策略、审批、Lease、Session、录像和审计可关联追踪。
- 用户、角色、DataAuthorizationGrant、托管账号和配置版本变化可以实时撤权；主机标签变化不改变授权。
- Gateway、Connector、Direct Executor 横向扩展和故障恢复不改变安全语义。

## 2. 四域模型

### 2.1 访问授权 Grant

Grant 是基础资格，不直接表达审批和会话控制：

~~~text
RemoteAccessGrant
├── enterprise_id
├── subject_type / subject_id
├── explicit_host_ids[]
├── managed_account_ids[]
├── protocols[]
├── actions[]
├── valid_from / valid_until
├── status / version
└── created_by / timestamps
~~~

空 host_ids 表示无授权，不能解释为全部主机。Grant 的动作必须显式列出；允许 Terminal 不得隐式开启文件、剪贴板、端口转发或会话分享。

### 2.2 访问规则 Rule

Rule 负责匹配本次访问并产生决策义务：

~~~text
RemoteAccessRule
├── enterprise_id
├── name / description
├── priority
├── protocols[] / actions[]
├── source_cidr[]
├── time_windows[]
├── effects[]: deny | require_mfa | require_approval | notify
├── approval_workflow_id
├── session_profile_id
├── enabled / version
└── audit metadata
~~~

第一版只支持用户/部门、明确 Host、托管账号、协议、来源 CIDR 和时间窗口；任意标签授权 DSL、任意 CEL 和用户 SQL 不在范围内。

规则计算约束：

1. 有效 Grant 提供正向基础访问资格；没有匹配 Rule 时允许访问并使用系统安全默认 Session Profile。
2. 明确命中的 deny 优先于所有其它结果。
3. 没有有效 Grant 时直接拒绝，Rule 不能越权补齐或扩大 Grant。
4. 所有命中的 require_mfa、require_approval 和 notify 义务都必须合并。
5. 多个 Session Profile 取最严格的限制，不能取最宽松值。
6. 同优先级规则按稳定 ID 排序，结果必须可复现。

`effects[]` 是同一匹配条件下的组合义务：`require_mfa`、`require_approval`、`notify` 可以同时出现；`deny` 必须独占，且不能携带 Workflow/Profile 引用。Rule 可以使用空 `effects[]` 只附加 Session Profile，但 effect 和 Session Profile 不能同时为空。

### 2.3 审批流程 Workflow

~~~text
ApprovalWorkflow
├── enterprise_id
├── name / description
├── approver_role_ids[]
├── minimum_approvals
├── separation_of_duties
├── approval_timeout_seconds
├── escalation_after_seconds
├── timeout_effect: reject | expire
├── escalation_role_ids[]
├── enabled / version
└── audit metadata
~~~

流程只描述审批要求，不保存某一次申请的决定。申请创建时保存不可变 Requirement Snapshot；后续修改流程不影响历史申请。

### 2.4 会话策略 Session Profile

~~~text
SessionProfile
├── enterprise_id
├── name / description
├── max_session_seconds
├── idle_timeout_seconds
├── recording_mode: required | optional | disabled
├── command_audit_mode
├── clipboard_mode
├── file_upload_mode
├── file_download_mode
├── port_forward_mode
├── session_share_mode
├── retention_days
├── enabled / version
└── audit metadata
~~~

第一版默认值必须安全：录像 required、剪贴板/file/port-forward/share disabled；任何放宽都需要明确配置、权限和审计。

## 3. 统一授权决策

所有入口调用统一服务：

~~~go
type AccessIntent struct {
    EnterpriseID, UserID, DepartmentID uuid.UUID
    HostID, ManagedAccountID uuid.UUID
    Protocol, Action string
    SourceIP netip.Addr
    At time.Time
}

type AccessDecision struct {
    Outcome DecisionOutcome
    MatchedGrantIDs []uuid.UUID
    MatchedRuleIDs []uuid.UUID
    ApprovalRequirements []RequirementSnapshot
    SessionProfile SessionProfileSnapshot
    AuthorizationVersion int64
    ReasonCodes []string
    SnapshotHash [32]byte
}
~~~

决策顺序固定为：

~~~text
身份/企业校验
→ DataAuthorizationGrant 校验
→ Host/ManagedAccount 状态和归属校验
→ Grant 匹配
→ Rule 匹配和 Deny 优先
→ MFA/审批义务合并
→ Session Profile 取最严格值；无匹配 Rule/Profile 时使用系统安全默认值
→ 生成快照、版本和审计摘要
~~~

浏览器不能自己合并 Grant、Rule、Workflow 或 Profile；模拟器也必须调用同一服务，只返回脱敏的解释结果。

## 4. 运行时状态机

~~~text
AccessRequest:
requested → awaiting_mfa → awaiting_approval → authorized
                         ↘ rejected / expired / invalidated

AccessLease:
issued → active → revoked / expired

RemoteAccessSession:
requested → connecting → active → terminating → terminated
             ↘ failed / expired / connection_lost / invalidated
~~~

规则命中但不需要审批时可以跳过 awaiting_approval；需要 MFA 时必须在签发 Lease 前完成 fresh Step-up。AccessLease 默认最长 15 分钟，Ticket 默认 60 秒且一次性消费。

## 4.1 运行态信息架构

AccessRequest 不单独创建一级导航。它属于审批中心中的远程访问申请记录：

~~~text
执行治理 → 审批中心
├── 操作审批
└── 远程访问
    ├── 待我审批
    ├── 我发起的（AccessRequest）
    └── 已处理
~~~

统一审批中心的远程访问 Tab 与操作审批使用相同的范围筛选语义：`待我审批`、`我发起的`、`已处理`。中央页面只查询和跟踪申请，不提供脱离主机资源上下文的任意目标输入；申请入口仍然是“主机详情 → 终端与会话 → 建立会话”。

RemoteAccessSession 和 RemoteAccessRecording 属于独立的远程会话运行中心：

~~~text
远程会话
├── 活动会话
├── 历史会话
└── 会话录像
~~~

申请、租约和会话必须保持不同对象和详情页。一个已授权申请可以产生零个或多个会话；录像从会话详情进入，同时允许从“会话录像”列表反向打开所属会话。

## 5. 撤权语义

以下变化必须递增 AuthorizationVersion，并通过 Outbox 通知：

- 用户、部门、角色或 RoleBinding 变化。
- DataAuthorizationGrant 或主机标签变化（不影响授权）。
- Grant、Rule、Workflow、Session Profile 停用或版本变化。
- ManagedAccount、Host 或企业停用。

撤权时：

- 未完成申请标记 invalidated。
- 未使用 Ticket 立即失败。
- Lease 标记 revoked。
- 活动 Session 按配置终止并写入原因。
- 录像和审计保留，不因撤权删除。

## 6. 生命周期和引用

配置对象采用 `draft → enabled → disabled → archived`；新建只能从 draft 开始，disabled 通过启用接口回到 enabled，archived 通过恢复接口回到 draft。第一阶段统一不提供物理删除，已被申请、Lease、Session 或审计引用的对象必须长期保留；所有状态变更都递增 version 并展示影响范围。

停用 Rule 只影响命中该 Rule 的新申请和按版本绑定的活动会话；停用 Grant 必须撤销其关联 Lease/Session；停用 Workflow 不得修改已有 Requirement Snapshot；停用 Session Profile 不得改变已有 Session。

## 7. 复用现有 M6

保留并复用：

- argus-connector-gateway 外部 WSS 和内部 peer mTLS。
- Connector / Direct Executor 的 SSH PTY 和 WinRS 行模式。
- AccessLease、一次性 Ticket、AuthorizationVersion 和 Fence。
- AES-GCM 分片录像、Hash Chain、ObjectStore 和远程终止。

Task 02 已完成的输入边界：

- RemoteAccessPolicy 已拆为 Rule、Workflow、SessionProfile，并删除旧 API、代码、权限和表。
- Handler 不再拼接混合 Policy 字段。
- RemoteAccessDecisionService 已成为申请、恢复、模拟和会话创建的唯一授权入口。
- Request、Lease、Session 保存不可变决策/Profile 快照和快照哈希。
- Worker 每 30 秒处理 MFA 请求过期、达到 Requirement 冻结 `escalation_at` 的单次升级和审批超时；升级阈值来自 Workflow 的 `escalation_after_seconds`，不是服务端固定比例。
- 允许、等待和拒绝决策都保存 outcome、reason code、来源版本、AuthorizationVersion 和 `SnapshotHash` 的审计摘要；命中 `notify` effect 时写入 `remote_access.decision.notify` Outbox。
- Session/Gateway 已执行 SessionProfile 的录像与命令审计 `required/optional/disabled` 基础语义；高级通道保持显式禁用和服务端安全拒绝。Evaluation 环境已验证横向扩展、Redis 清空、Worker 重启和 PostgreSQL Pod 恢复，生产 HA、不可变保留和跨故障域灾备仍由 Production Validation 管理。

## 8. 第三阶段完成边界

Task 04 已并入第三阶段，不再作为独立执行阶段。2026-08-26 的 `20260826-planv3-final9` 真实 Kubernetes 运行已验证治理配置、统一审批、MFA、Lease、Session、SSH/WinRS、录像、撤权、Gateway/Worker 恢复和桌面端 real Playwright，证据位于 `artifacts/m6-e2e/20260826-planv3-final9/`。因此 PlanV3 的开发与 Evaluation 链路已经闭环。

人工/Evaluation 灾备测试已完成，证据位于 `artifacts/m8-e2e/fv-20260824-m8-final13/`；仍未关闭的生产边界包括 WORM/Object Lock、PITR、跨故障域灾备、生产 PostgreSQL/ObjectStore HA、生产 NetworkPolicy 执行、外部 Egress Gateway、强化 Sandbox Runtime 和真实 Windows 兼容性。这些项目不能从 Docker Desktop 或 Local Hardening 结果推导为已完成。

## 9. 非目标

第一阶段不实现 RDP/SFTP、任意端口代理、任意策略 DSL、用户自定义脚本、跨企业授权、通用 ABAC/ReBAC 和 AI 获取人工会话 Ticket。
