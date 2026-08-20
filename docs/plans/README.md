# 分阶段任务文件

本目录把[端到端实现计划](../15-end-to-end-implementation-plan.md)拆成可执行里程碑。任务 ID 应保留到 Issue、PR、测试名称或变更说明中，便于追踪交付状态。

| 里程碑       | 文件                                                  | 主要交付                                                                                |
| ------------ | ----------------------------------------------------- | --------------------------------------------------------------------------------------- |
| M0（已完成） | [契约与文档](./M0-contract-and-documentation.md)      | 身份、labels/DataScope、API、PendingAction、Card、Agent/Stream/Telemetry 契约与生成门禁 |
| M1（已完成） | [前端基础](./M1-frontend-foundation.md)               | real/mock Adapter、UI/样式/i18n/认证/Card 前端整改                                      |
| M2（已完成） | [初始化、身份与授权](./M2-bootstrap-identity-rbac.md) | Setup、平台/企业身份、Department、RoleBinding/DataScope、Session、审计                  |
| M3（已完成） | [资源与 Connector](./M3-resource-and-connector.md)    | Host/Kubernetes labels、Secret/Credential、Bastion Scope、Connector、Direct Executor    |
| M4（已完成） | [执行、Agent 与 Tool](./M4-action-agent-workflow.md)  | Outbox/Lease/Fence、单 Agent Loop、上下文投影/压缩、Preview/Commit、Approval、Execution |
| M5（已完成） | [交互卡片](./M5-interactive-card.md)                  | Manifest、CSP、MessagePort、RenderPlan、Binding、发布门禁                               |
| M6（已完成） | [远程访问](./M6-remote-access.md)                     | Grant、Ticket、SSH PTY/HTTPS WinRS、加密录像、终止与撤权                                |
| M7（已完成） | [遥测](./M7-telemetry.md)                             | Collector、Ingest、Kafka、ClickHouse、Query 和统一权限裁剪                              |
| M8           | [本地安全与恢复](./M8-production-readiness.md)        | MFA/Step-up、OpenBao、备份恢复、升级、供应链和本地完整 E2E                              |

截至 2026-08-19，M0-M7 已完成，M8 本地安全、恢复与发布基座已经进入最终门禁验证。M8 只允许在 arm64 Docker Desktop 标记 `local_hardening_complete`；Production Profile 继续阻断。Windows/AMD64、生产 HA、容量、固定出口和跨集群灾备进入独立 Production Validation 清单。

## 状态规则

- `[ ]` 未开始。
- `[~]` 已开始但未达到验收标准。
- `[x]` 已完成并有测试或交付物证据。
- 任务不能只因代码合并标记完成；契约、测试、文档和必要的部署验证必须同时满足。
- 被阻塞任务应写明 ADR、上游任务或外部依赖，不使用模糊的“后续处理”。

## 通用完成定义

每个里程碑都必须满足：

- 相关单文件低于 2000 行，领域和前端组件边界清晰。
- OpenAPI/protobuf/JSON Schema 与实现同步，没有手写漂移。
- `typecheck`、lint、单元测试、契约测试和相关构建通过。
- 用户可见功能完成 `zh-CN/en-US`、light/dark、键盘和基础读屏验证。
- 变更路径覆盖权限、审计、错误、幂等和撤权测试。
- 需要完整依赖的流程在临时 Kubernetes Namespace 执行 E2E，并无条件清理测试资源。
- 若实现改变架构边界，同步更新 `docs/00` 和相关专题文档。
