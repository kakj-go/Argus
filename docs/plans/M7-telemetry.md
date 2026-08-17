# M7：OpenTelemetry 遥测闭环

## 目标

交付从 Host/Kubernetes Collector 到 Kafka、ClickHouse 和统一 Query Service 的可信 Metrics/Logs/Traces 链路。

## 前置条件

- M3 资源、Connector 和 Secret/Credential Broker 完成；M3 不提供 Collector 安装或遥测命令。
- M4 Preview/Commit、Execution 和授权快照完成。
- M2 DataScope 可解析为授权 Resource IDs。

## 任务

- [ ] `M7-PROFILE-01` 实现版本化 Distribution、CollectionProfile、ConfigRevision 和 CollectionClaim。
- [ ] `M7-INSTALL-01` 实现 Host Collector 与 Kubernetes DaemonSet/Gateway 的 Preview/Commit、Artifact 校验、安装、回滚和修复。
- [ ] `M7-ROUTE-01` 第一批实现 `direct_argus` 和 `bastion_gateway`；独立 TelemetryGroup 在容量允许时再启用。
- [ ] `M7-IDENTITY-01` 实现 Collector/Resource 凭证、轮换和可信 `EnterpriseId + ResourceId + CollectorId` 覆盖。
- [ ] `M7-INGEST-01` 实现 OTLP gRPC/HTTP、认证、限流、大小/属性/高基数限制、脱敏和 Kafka ACK。
- [ ] `M7-KAFKA-01` 实现 Signal Topic、ACL、至少一次语义、Offset 门禁和 DLQ/重放。
- [ ] `M7-WRITER-01` 完成标准 Writer Gate；不满足可靠性/Schema 时实现最小 Writer。
- [ ] `M7-CH-01` 实现 Metrics/Logs/Traces Local/Distributed Schema、Migration、TTL、分片和 Projection。
- [ ] `M7-QUERY-01` 实现版本化 Query Schema、Enterprise/Resource 强制过滤、Signal 权限、字段脱敏、预算和 `partial`。
- [ ] `M7-NODE-01` 实现 KubernetesNodeHostBinding 与 CollectionClaim 冲突/迁移规则。
- [ ] `M7-WEB-01` 引入 ECharts，完成基础 Metrics/Logs/Traces 查询、Collector 状态和路由页面。
- [ ] `M7-TOOL-01` 实现 Agent/Card 共用的 Metrics/Logs/Traces Query Tool 和安全投影。

## 测试

- 客户端伪造 Enterprise/Resource/Collector 属性被覆盖或拒绝。
- 跨企业、超出 DataScope、跨 Signal 和敏感字段查询被拒绝。
- Kafka 重试、Writer 重启、DLQ 跳过/重放不破坏可解释的去重语义。
- Host Collector/DaemonSet 同一 Claim 冲突在 Commit 前阻止或形成有期限迁移。
- Query Service 的 Web/Agent/Card 结果一致，未授权资源不泄露名称或属性。
- 临时 Namespace 完成 OTLP 写入、Kafka、ClickHouse、Query 和清理。

## 退出标准

- 真实资源的 Metrics/Logs/Traces 可摄入、查询、脱敏和审计。
- Redis 清空、Ingest/Writer/Query Pod 删除后系统按既定语义恢复。
- 查询预算、保留期和基本成本指标可观测。

## 不包含

- 任意 SQL、任意 Collector YAML、Profiles 信号和高级尾采样。
- 独立 TelemetryGroup 可在 `direct_argus/bastion_gateway` 稳定后延后。
- M3 real 页面中的 Collector provisional 控件不代表本里程碑已经交付；只有本里程碑 E2E 通过后才可启用真实入口。
