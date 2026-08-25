# PlanV3 Task 01：领域模型与契约

## 目标

建立四个配置域的独立领域对象、数据库表、OpenAPI DTO、错误码、版本和引用约束；不改变 M6 Gateway 的连接协议。

## 任务清单

- [ ] P3-DOMAIN-01 定义 RemoteAccessRule、ApprovalWorkflow、SessionProfile Go 类型和状态枚举。
- [ ] P3-DOMAIN-02 为三类对象增加 PostgreSQL migration，统一 enterprise_id、version、状态、审计元数据和唯一名称约束。
- [ ] P3-DOMAIN-03 增加 Rule → Workflow/Profile 的企业内引用校验和停用影响查询。
- [ ] P3-DOMAIN-04 将旧 remote_access_policies 转换为 Rule + Workflow + SessionProfile；保留旧 ID 映射和迁移审计。
- [ ] P3-CONTRACT-01 固化 OpenAPI 列表、详情、新建、更新、启用、停用、恢复、归档和引用查询。
- [ ] P3-CONTRACT-02 固化规则模拟请求/响应和稳定 reason code。
- [ ] P3-CONTRACT-03 生成 Go/TypeScript 客户端，执行 lint、生成漂移和 breaking check。
- [ ] P3-CONTRACT-04 增加配置版本、ETag/expected_version 和幂等键约束。
- [ ] P3-CONTRACT-05 增加领域单元测试、跨企业引用测试、空范围 fail-closed 测试和状态转换测试。

## 数据不变量

- Rule 的 Workflow/Profile 引用必须属于同一企业。
- Rule 的 deny 不得配置需要审批或 Session Profile 的执行参数。
- Workflow 的 minimum_approvals 不得超过可用审批主体上限。
- Profile 的所有时长必须在服务端边界内，idle_timeout <= max_session_duration。
- 已被运行态引用的对象不能物理删除。
- 任何配置对象停用都产生审计事件和版本递增。

## 退出标准

契约生成通过，迁移可在全新和已有 Evaluation 数据上完成，历史 M6 申请/Lease/Session/录像仍能读取；所有单文件低于 2000 行；go test ./...、契约检查和 git diff --check 通过。
