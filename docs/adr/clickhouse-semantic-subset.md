# ADR: ClickHouse Semantic Subset

## 决策

ClickHouse 继续作为 Metrics、Logs、Traces 的唯一事实存储。Metrics 使用 `metric_series` + `metric_samples` 两层模型；Logs 使用低基数 `stream_labels` 与高基数 `structured_metadata` 分离模型；Traces 使用 Span 明细、`trace_summary` 和 `trace_span_edges` 派生表。

Planner 先筛选 Series，再读取 Samples；Trace Parent/Child 关系使用显式边或受界限的父子查询，禁止每次请求对全量 Span 做无界自连接。所有执行设置由服务端从预算生成，用户不能传 ClickHouse 设置、表名或字段名。

`metric_samples_local` 使用 ReplacingMergeTree 与 5 分钟 Projection 组合，因此在表级固定 `deduplicate_merge_projection_mode='rebuild'`。目标 ClickHouse 26.3 默认的 `throw` 会拒绝为 ReplacingMergeTree 添加 Projection，不能依赖集群级隐式设置。

## 兼容范围

PromQL 支持 selector、matcher、基础聚合、rate/increase 和分组；LogQL 支持 stream selector、正文过滤、基础解析和 unwrap；TraceQL 支持属性过滤、Trace/Span 基础投影和直接 Parent/Child。超出静态复杂度或预算的查询失败并返回结构化错误。

Histogram、Summary 和 Exponential Histogram 当前保证原始结构入库，但 PromQL Histogram 查询函数尚未进入支持矩阵；存储完整不等于查询语义已经兼容。Trace Summary 当前也不作为任意跨批次 Trace 的强一致视图使用。
