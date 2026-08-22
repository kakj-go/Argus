# M9 PromQL / LogQL / TraceQL 查询语言（已归档）

> 本文只保留历史决策。M9 的统一 Query IR、LogQL 和 TraceQL 运行路径已被 M10 单进程 PromQL/KQL/SkyWalking GraphQL Engine 完整替换，不再属于当前代码、契约或发布门禁。

## 历史目标

M9 曾计划将三种 DSL 统一转换为 Argus Query IR，再编译成 ClickHouse SQL：

```text
PromQL / LogQL / TraceQL
→ Parser / AST
→ Argus Query IR
→ ClickHouse Planner
→ 参数化 SQL
```

实践表明，PromQL 的 vector matching、subquery、staleness、histogram 和 counter reset 等语义不适合由通用 SQL IR 近似复刻。日志与 Trace 也不需要为了形式统一而共享同一语义 AST。

## 被 M10 替代的内容

- PromQL 改为锁定版本的 Prometheus Engine，通过 ClickHouse `storage.Queryable` 提供事实样本。
- LogQL 改为 Argus KQL 核心过滤语法与受控 Pipeline。
- TraceQL 改为固定只读的 SkyWalking 风格 GraphQL Schema。
- 共享 Query IR、`querylang/{promql,logql,traceql}` Adapter、统一 v2 Query Envelope 和 M9 E2E 脚本均已删除。
- 当前唯一查询发布门禁是 M10 的语言门禁与 `make e2e-m10-query-k8s`。

## 历史验证记录

M9 最后一次真实 Kubernetes 验证运行号为 `20260821015219-30521`。该记录只证明当时的历史实现，不再作为当前版本的发布证据。

当前架构、兼容范围和验收结果见 [M10 单进程 Query Engine](./M10-query-engine.md) 与 [Telemetry Query Engine ADR](../adr/telemetry-query-engine.md)。
