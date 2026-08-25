# PlanV3-01：远程访问治理总体设计

## 1. 设计目标

远程访问必须同时满足：

- 资源范围明确，不能因管理员身份隐式获得生产 Shell。
- 配置对象可复用，审批和会话限制不复制到每条授权中。
- 管理员可以解释一次访问为什么被允许、拒绝或要求审批。
- 授权、策略、审批、Lease、Session、录像和审计可关联追踪。
- 用户、角色、DataScope、主机标签、托管账号和配置版本变化可以实时撤权。
- Gateway、Connector、Direct Executor 横向扩展和故障恢复不改变安全语义。

## 2. 四域模型

### 2.1 访问授权 Grant

Grant 是基础资格，不直接表达审批和会话控制：

~~~text
RemoteAccessGrant
├── enterprise_id
├── subject_type / subject_id
├── explicit_host_ids[]
├── host_label_selector
├── managed_account_ids[]
├── protocols[]
├── actions[]
├── valid_from / valid_until
├── status / version
└── created_by / timestamps
~~~

空 host_ids 且空选择器表示无授权，不能解释为全部主机。Grant 的动作必须显式列出；允许 Terminal 不得隐式开启文件、剪贴板、端口转发或会话分享。

### 2.2 访问规则 Rule

Rule 负责匹配本次访问并产生决策义务：

~~~text
RemoteAccessRule
├── enterprise_id
├── name / description
├── priority
├── subject_selector
├── host_selector
├── managed_account_selector
├── protocols[] / actions[]
├── source_cidr[]
├── time_windows[]
├── effect: allow | deny | require_mfa | require_approval | notify
├── approval_workflow_id
├── session_profile_id
├── enabled / version
└── audit metadata
~~~

第一版可以先支持用户/部门、主机标签、托管账号、协议、来源 CIDR 和时间窗口；任意 DSL、任意 CEL 和用户 SQL 不在范围内。

规则计算约束：

1. 明确命中的 deny 优先于所有 allow。
2. 没有有效 Grant 时直接拒绝，Rule 不能越权补齐 Grant。
3. 所有命中的 require_mfa 和 require_approval 义务都必须合并。
4. 多个 Session Profile 取最严格的限制，不能取最宽松值。
5. 同优先级规则按稳定 ID 排序，结果必须可复现。

### 2.3 审批流程 Workflow

~~~text
ApprovalWorkflow
├── enterprise_id
├── name / description
├── approver_role_ids[]
├── minimum_approvals
├── separation_of_duties
├── approval_timeout_seconds
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
→ DataScope 校验
→ Host/ManagedAccount 状态和归属校验
→ Grant 匹配
→ Rule 匹配和 Deny 优先
→ MFA/审批义务合并
→ Session Profile 取最严格值
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

## 5. 撤权语义

以下变化必须递增 AuthorizationVersion，并通过 Outbox 通知：

- 用户、部门、角色或 RoleBinding 变化。
- DataScope 或主机标签变化。
- Grant、Rule、Workflow、Session Profile 停用或版本变化。
- ManagedAccount、Host 或企业停用。

撤权时：

- 未完成申请标记 invalidated。
- 未使用 Ticket 立即失败。
- Lease 标记 revoked。
- 活动 Session 按配置终止并写入原因。
- 录像和审计保留，不因撤权删除。

## 6. 生命周期和引用

配置对象采用 draft → enabled → disabled → archived；已被申请、Lease、Session 或审计引用的对象不物理删除。未被引用的草稿可删除。停用页面必须提供恢复/归档操作，并展示影响范围。

停用 Rule 只影响命中该 Rule 的新申请和按版本绑定的活动会话；停用 Grant 必须撤销其关联 Lease/Session；停用 Workflow 不得修改已有 Requirement Snapshot；停用 Session Profile 不得改变已有 Session。

## 7. 复用现有 M6

保留并复用：

- argus-connector-gateway 外部 WSS 和内部 peer mTLS。
- Connector / Direct Executor 的 SSH PTY 和 WinRS 行模式。
- AccessLease、一次性 Ticket、AuthorizationVersion 和 Fence。
- AES-GCM 分片录像、Hash Chain、ObjectStore 和远程终止。

需要重构的输入边界：

- 当前 RemoteAccessPolicy 拆为 Rule、Workflow、SessionProfile。
- Handler 不再直接拼接混合 Policy 字段。
- RemoteAccessDecisionService 成为申请和会话创建的唯一授权入口。

## 8. 非目标

第一阶段不实现 RDP/SFTP、任意端口代理、任意策略 DSL、用户自定义脚本、跨企业授权、通用 ABAC/ReBAC 和 AI 获取人工会话 Ticket。
