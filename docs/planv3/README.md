# PlanV3：企业级远程访问治理

## 目标

PlanV3 将现有 M6 的人工远程访问闭环，从“授权 + 混合策略”演进为可解释、可审计、可撤权、可扩展的企业级远程访问治理体系。目标参考 JumpServer 的资产授权、访问控制、审批工单、会话审计分层，但保留 Argus 已完成的 Connector、Direct Executor、Gateway、短期 Ticket、Lease、录像和 AuthorizationVersion 能力。

最终运行链路：

~~~text
访问授权 Grant（基础资格）
  + 可选访问规则 Rule
      → 可选审批流程 Workflow
      → 可选会话策略 Session Profile
  → AccessRequest
  → AccessLease
  → RemoteAccessSession
  → Gateway/Connector/Direct Executor
  → 录像、命令审计、撤权和归档
~~~

## 产品信息架构

组织与权限 → 远程访问下提供四个配置 Tab：

~~~text
访问授权 | 访问规则 | 审批流程 | 会话策略
~~~

运行态数据不放入配置 Tab。访问申请与审批统一进入现有审批中心；远程会话和录像作为独立运行中心：

~~~text
执行治理 → 审批中心
  ├── 操作审批
  └── 远程访问
      ├── 待我审批
      ├── 我发起的（访问申请）
      └── 已处理

远程会话
  ├── 活动会话
  ├── 历史会话
  └── 会话录像
~~~

不新增独立的“访问申请”一级菜单。申请人从主机详情发起申请，申请记录在“审批中心 → 远程访问 → 我发起的”中跟踪；审批人从同一中心的“待我审批”处理。现有“待审批”页面建议更名为“审批中心”，因为它同时承载待办、我发起的和已处理记录。

四个配置域的职责必须保持单向清晰：

| 模块 | 主要问题 | 不负责的内容 |
| --- | --- | --- |
| 访问授权 Grant | 谁可以访问哪些主机、账号、协议和动作 | 审批人、MFA、会话时长 |
| 访问规则 Rule | 本次访问匹配什么条件、需要什么附加要求 | 保存审批决定、传输终端数据 |
| 审批流程 Workflow | 谁审批、几人审批、是否职责分离 | 决定基础资源范围 |
| 会话策略 Session Profile | 进入会话后能做什么、持续多久、如何录像 | 决定用户是否有基础访问资格 |

## 文档

| 文件 | 内容 |
| --- | --- |
| [01-remote-access-governance.md](./01-remote-access-governance.md) | 总体架构、领域边界、运行时决策、数据模型、API 和安全不变量 |
| [task-01-domain-and-contracts.md](./task-01-domain-and-contracts.md) | Grant/Rule/Workflow/Session Profile 领域模型、迁移和契约任务 |
| [task-02-decision-and-request-flow.md](./task-02-decision-and-request-flow.md) | 统一授权决策、申请、审批、Lease、撤权和版本快照 |
| [task-03-session-gateway-and-audit.md](./task-03-session-gateway-and-audit.md) | 会话网关、凭证隔离、录像、命令审计、HA 和故障恢复 |
| [task-04-enterprise-console-and-e2e.md](./task-04-enterprise-console-and-e2e.md) | 已并入第三阶段的四个 Tab、运行态页面、E2E 和生产验收门禁 |

## 架构边界变化

旧 RemoteAccessPolicy 同时承载规则匹配、审批要求和会话限制。PlanV3 已将其拆为 RemoteAccessRule、ApprovalWorkflow 和 SessionProfile 三个独立领域对象；RemoteAccessGrant 保留为基础资源授权对象。Task 02 已删除旧 Policy API、代码、权限和数据库表。Task 04 已并入第三阶段，与会话 Gateway、审计和远程会话中心作为同一条交付链路实施。

这是一次已确定架构边界的演进，实施前必须同步更新 docs/00-decisions-and-invariants.md、docs/02-identity-authorization-and-data-permission.md、docs/plans/M6-remote-access.md 和 docs/15-end-to-end-implementation-plan.md。M6 已完成的连接底座不重写，只迁移其治理输入和快照来源。

## 实施原则

1. Contract first：先固化 PostgreSQL、OpenAPI、错误码、状态机和生成代码，再接入页面。
2. 一个决策服务：HTTP、Web Terminal、OpenAPI 和未来可能的后台执行入口都调用同一个 RemoteAccessDecisionService；当前版本不提供定时无人值守入口。
3. 配置版本化：治理对象没有物理删除入口，数据库直接 `DELETE` 同样被拒绝；运行时保存匹配快照，历史解释不读取可变配置。
4. 默认拒绝：空授权范围、缺少有效 Grant、未知模式、已要求但无法完成的 MFA/审批和 required 审计存储不可用时 fail closed；没有匹配 Rule 不属于拒绝条件。
5. 最小权限：Grant、explicit resource authorization、ManagedAccount、协议、动作和有效期是基础必需条件；匹配 Rule 只叠加限制，未匹配 Rule 时使用系统安全默认 Session Profile。
6. 事实与加速分离：PostgreSQL 保存业务事实；Redis 只做缓存、通知、限流和短期协调。
7. 安全能力随功能交付：撤权、审计、幂等、错误投影、故障恢复和 E2E 不得延后到最后补齐。

## 计划状态

Task 01 的四域模型与契约、Task 02 的统一决策和运行态闭环均已实现。Task 04 已并入第三阶段；第三阶段已补齐远程访问四个治理 Tab、统一审批中心远程访问页签、远程会话一级菜单及活动/历史/录像页面，并将录像读取审计接入服务端。`RemoteAccessDecisionService.Evaluate` 仍是申请、恢复、模拟和 Session 二次评估的唯一授权入口；Request、Lease、Session 保存不可变快照。

2026-08-26，受保护的 Docker Desktop Kubernetes Evaluation 环境已完成真实 M6/PlanV3 回归，运行号为 `20260826-planv3-final9`，证据位于 `artifacts/m6-e2e/20260826-planv3-final9/`。该运行覆盖 MFA、双人审批、Lease、SSH/WinRS、录像、撤权、Redis 清空、跨 Gateway、Gateway Drain、Worker 全量 Pod 重启、PostgreSQL Pod 恢复、real Playwright 和清理；Task 01、Task 02 与第三阶段的开发/Evaluation 退出标准已经满足。

这不等同于 Production 认证。人工/Evaluation 灾备测试已由 `artifacts/m8-e2e/fv-20260824-m8-final13/` 记录并关闭 PlanV3 的人工灾备验收；WORM/Object Lock、PITR、跨故障域灾备、生产 PostgreSQL/ObjectStore HA、生产 NetworkPolicy 执行、外部 Egress Gateway、强化 Sandbox Runtime 和真实 Windows 兼容性继续由独立 Production Validation 阻断。

## 全计划审计结论

PlanV3 的 Task 01、Task 02、Task 03 和已并入第三阶段的 Task 04，其代码、契约、数据库迁移、前端、单元测试、真实 Evaluation E2E 和清理证据均已具备。规则模拟已在 Task 02 实现，不能再按 Task 01 的“后续任务”理解。

整个 PlanV3 可以标记为“开发、Evaluation 和人工测试完成”，其中 Task 03 的 `P3-DR-01` 已按人工/Evaluation 范围关闭。M8 的 `local-hardening` 演练覆盖加密备份、全新 Namespace 恢复、PostgreSQL/OpenBao/MinIO/ClickHouse 恢复和恢复后验证；这仍不等同于生产级 PITR、跨故障域灾备或 WORM/Object Lock 认证。计划状态应明确写为“开发/Evaluation/人工测试完成，生产认证未完成”。
