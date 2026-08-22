# ADR: 单进程 Telemetry Query Engine

## 状态

已接受，适用于 M10 Query 重构。

## 决策

遥测查询进程只保留一个进程边界，但在进程内运行三个独立语义引擎：Prometheus PromQL Engine、Argus KQL Engine 和 SkyWalking GraphQL Engine。三种语言不再强行转换为共享语义 AST。

PromQL 通过 Prometheus `promql.Engine` 和 `storage.Queryable` 执行；ClickHouse 只实现租户感知的 `Queryable/Querier/SeriesSet/Iterator` 适配层。Thanos 仅提供执行边界和查询统计参考，不引入 StoreAPI、Sidecar、Store Gateway、Replica Dedup、Query Frontend、Downsampling 或分布式 Fanout。

KQL 只允许固定字段、布尔条件、范围比较和受控 Pipeline。SkyWalking GraphQL 使用固定只读 SDL `internal/telemetry/queryengine/skywalking/schema/trace.graphql`，由 `graph-gophers/graphql-go` 在进程初始化时构建 Schema；请求只解析 Query Document。它允许有界展开的命名/内联 Fragment，拒绝 Mutation、Subscription、introspection、循环或未定义 Fragment，并在展开后限制深度、字段数、分页和关系扩展。

所有引擎共享 Scope、Budget、超时、并发、审计和结果投影，但不能绕过统一权限流程。ClickHouse 表名只能由可信 Enterprise UUID 通过 `TenantTableRouter` 生成，查询文本不得影响表名、字段名或 SQL 标识符。

## 租户存储

查询和 Writer 使用以下物理表名后缀：

```text
metric_series_<enterprise_uuid_hex>
metric_samples_<enterprise_uuid_hex>
logs_<enterprise_uuid_hex>
traces_<enterprise_uuid_hex>
trace_summary_<enterprise_uuid_hex>
trace_span_edges_<enterprise_uuid_hex>
```

`TenantSchemaManager` 负责创建、验证和删除租户表。企业创建/启用事务提交后，`argus-server` 通过内部 mTLS RPC 同步请求 Query 创建六表并写入 readiness；企业 disabled 时同步标记 deleting 并删除六表。Query 启动和周期对账只负责异常恢复与自愈。Schema 验证必须核对引擎、列类型、排序键和 TTL，不能只检查表名存在。

Query Pod 内的 ClickHouse 连接分为两类：查询 Engine 使用 `argus_telemetry_query` 只读账号；`TenantSchemaManager` 使用 `argus_telemetry_migration` 账号执行受信 DDL。Schema Manager 连接不得传入 PromQL、KQL、GraphQL 执行路径。

## 结果与失败策略

PromQL HTTP/MCP 返回 Prometheus `status/data.resultType/data.result/warnings`，并在 `argus_meta` 携带审计所需执行统计；KQL 返回独立 `argus.kql_result/v1` 日志结果；GraphQL 返回标准 `data/errors` 和 `extensions.argus`。三者不共享外部结果 Envelope。预算、超时、复杂度或权限失败时返回明确错误，不返回伪造的成功 partial 结果。

## 已接入与验证边界

PromQL Storage Adapter 已支持 float、stale marker、经典 Histogram、Summary quantile/count/sum 和 OTLP Exponential Histogram 到 `FloatHistogram` 的转换。HTTP、gRPC、MCP 三个入口显式区分 Instant/Range；HTTP/MCP 会先校验请求资源是否完全落在服务端 Data Scope 内。

PromQL 查询预算包含独立的 `MaxSamples` 与 `MaxSeries`，后者默认 100,000、硬上限 1,000,000，并贯穿所有协议入口和 ClickHouse Storage Adapter。旧统一 Query JSON Schema、统一 HTTP Envelope 和对应生成客户端不再属于契约。

锁定版 ClickHouse 的真实样本读取、Schema 漂移检查和 Prometheus 参考 Engine 差分测试已纳入门禁；Kubernetes E2E 继续作为每次发布的部署验收。

## 未覆盖

Recording/Alert Rule 管理、Thanos 特殊能力、完整 KQL、SkyWalking Topology/Metrics Query、任意 SQL 和旧查询协议不属于本 ADR 范围。
