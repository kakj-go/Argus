# PlanV3：企业级远程访问治理

## 目标

PlanV3 将现有 M6 的人工远程访问闭环，从“授权 + 混合策略”演进为可解释、可审计、可撤权、可扩展的企业级远程访问治理体系。目标参考 JumpServer 的资产授权、访问控制、审批工单、会话审计分层，但保留 Argus 已完成的 Connector、Direct Executor、Gateway、短期 Ticket、Lease、录像和 AuthorizationVersion 能力。

最终运行链路：

~~~text
访问授权 Grant
  → 访问规则 Rule
  → 审批流程 Workflow
  → 会话策略 Session Profile
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

运行态数据不放入配置 Tab，单独提供：

~~~text
访问申请 | 待审批 | 活动会话 | 历史会话 | 会话录像
~~~

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
| [task-04-enterprise-console-and-e2e.md](./task-04-enterprise-console-and-e2e.md) | 四个 Tab、规则模拟器、运行态页面、E2E 和生产验收门禁 |

## 架构边界变化

当前 RemoteAccessPolicy 同时承载规则匹配、审批要求和会话限制。PlanV3 将其拆为 RemoteAccessRule、ApprovalWorkflow 和 SessionProfile 三个独立领域对象；RemoteAccessGrant 保留为基础资源授权对象。

这是一次已确定架构边界的演进，实施前必须同步更新 docs/00-decisions-and-invariants.md、docs/02-identity-authorization-and-data-permission.md、docs/plans/M6-remote-access.md 和 docs/15-end-to-end-implementation-plan.md。M6 已完成的连接底座不重写，只迁移其治理输入和快照来源。

## 实施原则

1. Contract first：先固化 PostgreSQL、OpenAPI、错误码、状态机和生成代码，再接入页面。
2. 一个决策服务：HTTP、Web Terminal、OpenAPI 和未来可能的后台执行入口都调用同一个 RemoteAccessDecisionService；当前版本不提供定时无人值守入口。
3. 配置版本化：已被申请或会话引用的对象不可破坏性覆盖；运行时保存匹配快照。
4. 默认拒绝：空授权范围、未知模式、缺少 MFA/审批能力和审计存储不可用时 fail closed。
5. 最小权限：Grant、DataScope、ManagedAccount、Rule、Workflow 和 Session Profile 必须同时满足。
6. 事实与加速分离：PostgreSQL 保存业务事实；Redis 只做缓存、通知、限流和短期协调。
7. 安全能力随功能交付：撤权、审计、幂等、错误投影、故障恢复和 E2E 不得延后到最后补齐。

## 计划状态

当前为设计和实施计划，尚未改变 M6 现有接口和数据库。实现时按四个 Task 依次建立垂直闭环，并在每个 Task 完成后更新本目录状态和受影响的基线文档。
