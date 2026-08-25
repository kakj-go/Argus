# PlanV3 Task 02：授权决策、申请与撤权

## 目标

把 Grant、DataScope、Rule、Workflow、SessionProfile 合并为唯一、可解释、可复现的远程访问决策，并贯通申请、MFA、审批、Lease 和撤权。

## 任务清单

- [ ] P3-DECISION-01 实现 RemoteAccessDecisionService.Evaluate，固定校验顺序和 Deny 优先规则。
- [ ] P3-DECISION-02 实现多 Rule 命中合并、审批要求合并和 Session Profile 最严格值合并。
- [ ] P3-DECISION-03 输出脱敏解释结果、reason code、命中对象摘要、AuthorizationVersion 和 SnapshotHash。
- [ ] P3-REQUEST-01 改造 AccessRequest 创建，保存 Grant/Rule/Workflow/Profile 快照和来源版本。
- [ ] P3-REQUEST-02 接入 fresh Step-up；未完成 MFA 时 fail closed，不接受前端伪造证明。
- [ ] P3-APPROVAL-01 实现多 Requirement 审批、职责分离、审批超时、拒绝和升级。
- [ ] P3-LEASE-01 只允许通过已满足的快照签发 Lease，创建 Session 时再次 Evaluate。
- [ ] P3-REVOKE-01 接入 Grant/Rule/Workflow/Profile、DataScope、Host、账号和用户变化后的 Outbox 撤权。
- [ ] P3-SIMULATE-01 提供规则模拟 API，和实际申请共用同一 DecisionService。
- [ ] P3-AUDIT-01 记录决策输入摘要、命中对象、结果、版本、快照哈希和拒绝原因，不记录密码、Ticket 或 Secret 原值。

## 关键安全场景

1. 没有 Grant 时，任何 Rule 都不能放行。
2. Grant 有效但命中 Deny 时直接拒绝。
3. 多条 Rule 的 MFA、审批和会话限制必须全部合并。
4. 申请人不能满足职责分离审批。
5. Workflow 修改不影响已有申请快照。
6. Grant 停用后旧 Lease 和活动 Session 按撤权策略失效。
7. Rule 停用不会扩大权限；新申请不再命中旧版本。
8. Redis 清空后 PostgreSQL 仍能恢复权威授权状态。

## 退出标准

决策服务通过单元、集成和属性测试；真实 API 能完成“Grant → Rule → MFA/Approval → Lease → Session”；旧 M6 E2E 继续通过，并新增多规则、撤权、超时和模拟器 E2E。
