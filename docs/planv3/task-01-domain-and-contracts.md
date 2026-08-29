# PlanV3 Task 01：领域模型与契约

## 目标

建立四个配置域的独立领域对象、数据库表、OpenAPI DTO、错误码、版本和引用约束；不改变 M6 Gateway 的连接协议。

## 任务清单

- [x] P3-DOMAIN-01 定义 RemoteAccessRule、ApprovalWorkflow、SessionProfile Go 类型和状态枚举。
- [x] P3-DOMAIN-02 为三类对象增加 PostgreSQL migration，统一 enterprise_id、version、状态、审计元数据和唯一名称约束。
- [x] P3-DOMAIN-03 增加 Rule → Workflow/Profile 的企业内引用校验和停用影响查询。
- [x] P3-DOMAIN-04 将旧 remote_access_policies 转换为 Rule + Workflow + SessionProfile；Task 01 期间提供兼容投影，Task 02 直接切换后删除。
- [x] P3-CONTRACT-01 固化 OpenAPI 列表、详情、新建、更新、启用、停用、恢复、归档和引用查询。
- [x] P3-CONTRACT-02 规则模拟请求/响应已在第二阶段决策服务中实现；第一阶段不切换最终授权决策的边界保持不变。
- [x] P3-CONTRACT-03 生成 Go/TypeScript 客户端，执行 lint、生成漂移和 breaking check。
- [x] P3-CONTRACT-04 增加配置版本、expected_version、CSRF 和幂等键约束。
- [x] P3-CONTRACT-05 增加领域校验、跨企业引用、状态转换、签名分页和契约门禁；治理表集成断言已加入，真实数据库验收在未配置 `ARGUS_TEST_DATABASE_URL` 时跳过。

## 数据不变量

- Rule 的 Workflow/Profile 引用必须属于同一企业。
- Rule 的 deny 不得配置需要审批或 Session Profile 的执行参数。
- Rule 可以仅引用 Session Profile；effect 和 Session Profile 不能同时为空。
- Workflow 的 minimum_approvals 不得超过可用审批主体上限。
- Workflow 的审批角色和升级角色必须是非空、唯一、同企业且 active 的角色；角色不能同时出现在两组中。
- Profile 的所有时长必须在服务端边界内，idle_timeout <= max_session_duration。
- 已被运行态引用的对象不能物理删除。
- 任何配置对象停用都产生审计事件和版本递增。

## 第一阶段契约语义

- Rule 使用 `effects[]`。`require_mfa`、`require_approval`、`notify` 可以组合；`deny` 必须独占，且不能携带 Workflow/Profile 引用。正向访问资格来自 Grant，不再提供冗余的 `allow` effect。
- Rule 的 `require_approval` 必须引用同企业的 ApprovalWorkflow；SessionProfile 同理。跨企业或不存在的引用 fail closed。
- 三类对象统一使用 `draft → enabled → disabled → archived` 生命周期、单调递增 `version` 和更新时必填 `expected_version`。归档对象只能恢复为 draft，不能直接重新启用。
- 时间窗口的线格式由 OpenAPI 保证长度，领域层严格校验 `HH:MM`、星期、IANA 时区以及结束时间晚于开始时间；CIDR、协议、会话时长和审批人数同样由领域层再次校验。
- 已有运行态引用只允许停用、恢复或归档，不提供物理删除；引用查询先校验企业归属。
- `remote_access_policies` 只曾作为 Task 01 的迁移来源和短期兼容投影；Task 02 已清理开发运行数据并删除旧表、旧字段和旧权限。
- 新运行态只接受 Grant/Rule/Workflow/SessionProfile 来源快照，不再创建或读取 policy-only 快照。

## 退出标准

契约生成通过，迁移可在全新和已有 Evaluation 数据上完成，历史 M6 申请/Lease/Session/录像仍能读取；所有单文件低于 2000 行；go test ./...、契约检查和 git diff --check 通过。

## 2026-08-26 实施回顾

### 已完成

- 三类治理对象、领域校验、状态转换、版本控制、OpenAPI、生成客户端、权限和引用查询已经落地。
- 旧 Policy 到 Rule/Workflow/Session Profile 的迁移投影曾用于 Task 01 过渡；Task 02 已完成直接切换和最终清理。
- 新对象创建强制从 `draft` 开始；归档对象不可直接编辑，`disabled → enabled` 和 `archived → draft` 分别通过启用、恢复接口完成。
- Rule、Workflow、Session Profile 列表已接入企业主体绑定的签名游标；迁移会回填历史申请快照中的新对象 ID 与版本。
- `go test ./...`、`go vet`、契约生成/check/breaking、sqlc、企业端 typecheck/build/test 和 `git diff --check` 已通过。

### 后续状态

Task 02 已删除旧 Policy，接入统一决策、`awaiting_mfa`/`resume`、不可变运行态快照、多 Requirement 审批、Worker、模拟 API 和撤权闭环。真实 PostgreSQL、SSH/WinRS 与 Kubernetes 场景已由 `20260826-planv3-final9` Evaluation 运行及独立 PostgreSQL 删除迁移验证覆盖；生产级 HA、灾备和兼容性仍由 Production Validation 管理。
