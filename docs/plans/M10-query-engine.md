# M10 单进程 Query Engine

## 当前交付

- 新增 `internal/telemetry/queryengine` Coordinator 边界。
- PromQL 接入锁定的 Prometheus v0.314.0 Engine 和 `storage.Queryable` ClickHouse Adapter。
- 新增 KQL 核心 Parser/Compiler，字段名和 SQL 标识符均走白名单。
- 新增 SkyWalking GraphQL 只读 Schema、Mutation/introspection 拒绝和复杂度限制。
- SkyWalking Trace Schema 固定在 `internal/telemetry/queryengine/skywalking/schema/trace.graphql`，由 `graph-gophers/graphql-go` 在进程初始化时按 SDL 构建一次；每个请求只执行 Document 解析、安全校验和 Resolver 调用，不动态拼装 Schema。
- 新增 `TenantTableRouter`，Writer 与 PromQL/Trace/KQL 适配器共享安全表名生成。
- 新增租户表创建、校验、删除的 `TenantSchemaManager`。
- Writer 按可信 Enterprise Identity 路由写入租户表，并且只读取 PostgreSQL `enterprise_telemetry_tables.status=ready`；Writer 不持有也不调用 ClickHouse DDL 权限，所有建表/删除只由 Query 内的 Schema Manager 执行。
- protobuf 已新增 `ExecuteQueryV2` oneof RPC，支持 PromQL、KQL 和 SkyWalking GraphQL 三种内部请求。
- MCP 已切换为 `telemetry.promql.query`、`telemetry.kql.query` 和 `telemetry.skywalking.trace`。
- Query 进程启动时为 active Enterprise 建立并验证六张租户物理表；运行期间定时对账，disabled Enterprise 先标记 deleting 后删除租户表。
- 企业创建和重新启用在事务提交后通过内部 mTLS `EnsureTenantSchema` RPC 同步完成六表创建、严格校验和 readiness 写入；禁用企业通过 `DropTenantSchema` 同步进入 deleting 并删除表。周期对账只承担启动恢复与自愈。
- `VerifyTenant` 同时校验六张表的 `ReplacingMergeTree` 引擎、完整列类型、排序键和 TTL，不再以“表存在”代替 Schema readiness。
- Query 进程将 ClickHouse 查询连接与 TenantSchemaManager DDL 连接隔离：PromQL/KQL/GraphQL 只持有只读账号，租户建表/删除使用独立 migration 账号；两者不允许在代码中混用。
- Writer 在租户状态不是 active 时将记录作为永久错误进入 DLQ，不会因 Kafka backlog 重新创建已删除的租户表。
- PromQL Storage Adapter 已读取 float、stale marker 和经典 Histogram 的 count/sum/bucket，并将其合成为 Prometheus 可消费的 `_count`、`_sum`、`_bucket` 序列。
- PromQL Storage Adapter 已保留 Summary 的 quantile/count/sum 结构，并将 OTLP Exponential Histogram 转换为 Prometheus `FloatHistogram` 迭代器；Instant/Range 模式由 HTTP、gRPC 和 MCP 显式传递。
- PromQL Storage Adapter 已先选择 Series、再读取 Samples，避免全局样本 `LIMIT` 截断；样本按时间和 ingest key 稳定排序并去重，Series、Samples、结果字节预算分开限制。Delta Monotonic Sum 在未完成累计转换前进入 DLQ，不伪装成可查询的累计 Counter。
- PromQL 结果在关闭上游 Query 前深拷贝 scalar/string/vector/matrix 与 Histogram，避免 Prometheus 全局 point pool 被后续查询复用后改写已返回结果。
- 物理租户表不再保留 `enterprise_id` 列；`resource_id` 是租户内唯一授权裁剪字段，Trace Span Edge 也携带 `resource_id`。
- HTTP 和 MCP 查询入口会先将请求资源 ID 与服务端 Data Scope 做完整授权校验，存在未授权资源时返回 `QUERY_SCOPE_DENIED`，不会静默裁剪或返回 partial 成功结果。
- Coordinator 统一执行结果字节预算、日志正文/Trace attributes-events-links 脱敏和查询审计；敏感字段权限通过带签名的 gRPC Scope 传递，不能由查询文本覆盖。
- HTTP、gRPC、MCP 三个入口均可设置 `max_result_bytes`；未设置时统一采用 8 MiB 默认值，超限返回 `QUERY_BUDGET_EXCEEDED`，不返回标记成功的截断结果。
- PromQL 的 `MaxSeries` 已贯穿 OpenAPI、protobuf、HTTP、gRPC、MCP 和 Prometheus Storage Adapter；默认 100,000、硬上限 1,000,000。gRPC 零值使用服务端默认预算，超过任一硬上限稳定返回 `ResourceExhausted: QUERY_BUDGET_EXCEEDED`。
- KQL 已支持字段存在性、wildcard、`parse json`、`parse logfmt`、受限 `parse pattern "...<field>..."`、where、unwrap、stats count、sort 和 limit。
- SkyWalking GraphQL 已使用 AST 校验只读 Query，支持命名 Fragment、Fragment Spread 和 Inline Fragment；Fragment 展开后统一执行深度/字段预算，并拒绝重复、未定义、循环 Fragment、introspection、Mutation、Subscription 和 Schema/Type definition。
- SkyWalking GraphQL 已增加 durationMin/durationMax、受控 start-time order、关系扩展预算和结果 JSON 字节预算；Span attributes/events/links 经过统一结果投影。
- SkyWalking GraphQL 首批条件已补齐 serviceInstanceName、结构化 tags、全量匹配 total 计数，并支持受限的 Trace `edges` Parent/Child 投影；关系读取只走租户 `trace_span_edges` 表并受 MaxRelationExpansions 限制。
- Query Audit 已接入 PostgreSQL tamper-evident audit chain，动作固定为 `telemetry.query.execute`；只持久化表达式哈希、计划哈希、授权版本、资源数量和执行统计，不持久化原始 PromQL/KQL/GraphQL。审计事务对 PostgreSQL serialization failure 和 deadlock 执行最多 5 次有界退避重试，永久错误立即返回；最终写入失败记录结构化错误但不阻断查询结果。
- Web 已直接消费 Prometheus Engine 的 vector/matrix 结果，并将原生 label/value/values 结构投影为统一时间序列组件；Query Builder 与 DSL 编辑器共享三种原生协议入口。
- 外部 HTTP 与 MCP 不再返回统一 `argus.telemetry_result/v2`：Metrics 返回 Prometheus `status/data.resultType/data.result/warnings` 并附加 `argus_meta`，Logs 返回 `argus.kql_result/v1`，Trace 返回 GraphQL `data/errors/extensions.argus`。旧统一 Query JSON Schema 及其生成客户端已删除。

## 明确范围

- OpenAPI、protobuf 和 TypeScript 客户端已切换并由生成流程维护；旧统一 Query JSON Schema 已删除，`/enterprise/telemetry/query/overview` 仅保留为内部固定聚合接口。
- ClickHouse 共享遥测表只在一次性迁移脚本中执行删除；运行时建表唯一入口是 `TenantSchemaManager`。
- 企业创建/启用同步完成租户表 readiness，disabled 状态同步触发回收；启动和 30 秒周期对账用于恢复异常中断。当前平台没有独立“删除企业”HTTP 动作，因此 disabled 是遥测数据删除触发点。
- PromQL Native Histogram、Summary 已接入 Storage Adapter；QueryMeta 通过 ClickHouse driver progress callback 累积真实 scanned rows/bytes，并同时记录 Engine SamplesRead。
- KQL 仍是 Argus 受控子集，不承诺 Elasticsearch/Loki 全量兼容；pattern 只提供字面量捕获，不提供脚本和任意正则。
- SkyWalking GraphQL 仍是固定只读 Trace Schema，不实现 OAP Topology、Metrics Query 和任意关系闭包；当前 SDL 只承诺 Argus 支持的 `queryBasicTraces`、`queryBasicTracesByName`、`queryTraces`、`queryTrace` 以及 bounded spans/edges 投影。
- Kubernetes 集群环境依赖外部运行时；`go run ./cmd/argus-dev e2e run --suite m10-query` 运行真实路径，`--unit-only` 显式运行不创建集群资源的轻量门禁。

## 验证

```text
go test ./...
go run ./cmd/argus-dev repo vet
go run ./cmd/argus-dev contracts check
go run ./cmd/argus-dev query promql
go run ./cmd/argus-dev query kql
go run ./cmd/argus-dev query skywalking
go run ./cmd/argus-dev query tenant-schema
go run ./cmd/argus-dev e2e run --suite m10-query
```

截至 2026-08-22，代码、契约、Parser lock、语言门禁、全量 Go 测试和锁定版 ClickHouse `26.3.17.110-alpine` 差分测试均已通过。差分覆盖 Instant/Range、`sum by`、vector matching、`group_left`、counter reset、`_over_time`、subquery、`offset` 和 `@`，并验证真实 scanned rows/bytes；Schema 漂移测试验证缺列会阻断 readiness。三种独立 wire format、固定 SDL 和 `MaxSeries` 收口后的 Kubernetes E2E 成功运行号为 `20260822063330-17805`，覆盖 PromQL/KQL/SkyWalking GraphQL、企业同步建表与 readiness、Native Histogram/Summary、Trace spans/edges、Query Audit、租户隔离、Writer backlog/DLQ、Redis/PostgreSQL 恢复、权限/预算/脱敏和四种 locale/theme 的真实 Web 查询。运行结束后三个临时 Namespace、相关 PVC 和 E2E Lease 均为零残留。

## 状态结论

M10 实施计划已完成。旧统一 Query Envelope、旧 Query IR、旧查询 RPC、旧 M9 门禁和旧共享事实表已从运行链路移除；KQL 是明确的 Argus 受控子集，Trace 是明确的只读 SkyWalking Schema 子集，不声称完整 Elasticsearch/Loki/OAP 兼容。生产发布仍须在目标环境执行同一组门禁。
