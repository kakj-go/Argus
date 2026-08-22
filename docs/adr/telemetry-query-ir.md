# ADR: Telemetry Query IR（历史）

## 状态

已废弃，适用于 M9 历史设计；M10 已改为每种语言使用独立 Engine 边界。

## 决策

PromQL、KQL 和 SkyWalking GraphQL 不再统一转换为 `internal/telemetry/queryir`。PromQL 进入 Prometheus Engine 的 `storage.Queryable`，KQL 和 GraphQL 各自拥有受控 Parser/Resolver，再进入 ClickHouse 适配器。

该设计曾要求 PromQL、LogQL 和 TraceQL 统一转换为 Argus IR；M10 已废弃这条路径。当前 PromQL 使用锁定版本的 Prometheus Engine，日志使用 KQL，Trace 使用固定 SkyWalking GraphQL Schema。历史文档仅用于解释为什么不允许把用户文本直接拼进 SQL。

查询文本永远不能直接进入 SQL；表名、字段名和函数名只能来自编译器常量。权限在 Planner 阶段注入，不能通过 DSL 的 matcher 或 SQL `OR` 绕过。

## 结果

统一结果元数据由 Query Coordinator 维护，语言协议保留各自结果模型；Web、Agent、Card 只消费经过统一授权投影和敏感字段脱敏的结果。

## 取舍

历史实现只实现核心生产子集；现行 M10 的不支持特性仍必须显式返回错误，不静默近似执行。
