# M7：OpenTelemetry 遥测闭环

## 目标

交付从 Host/Kubernetes Collector 到 Kafka、ClickHouse 和统一 Query Service 的可信 Metrics/Logs/Traces 链路。

## 前置条件

- M3 资源、Connector 和 Secret/Credential Broker 完成；M3 不提供 Collector 安装或遥测命令。
- M4 Preview/Commit、Execution 和授权快照完成。
- M2 DataScope 可解析为授权 Resource IDs。

M7 Collector 安装写操作复用 M4 的 Preview → Confirmation/Approval → Action Executor → Execution，并沿用 `result_unknown` 对账；Telemetry Query Tool 复用 M4 Artifact/ToolResultProjection 上限与授权读取，不新增第二套 Agent Result Store。

## 任务

- [x] `M7-PROFILE-01` 实现版本化 Distribution、CollectionProfile、ConfigRevision 和 CollectionClaim。
- [x] `M7-INSTALL-01` 实现 Host Collector 与 Kubernetes DaemonSet/Gateway 的 Preview/Commit、Artifact 校验、安装、回滚和修复。
- [x] `M7-ROUTE-01` 第一批实现 `direct_argus` 和 `bastion_gateway`；独立 TelemetryGroup 延后。
- [x] `M7-IDENTITY-01` 实现 Collector/Resource 凭证、轮换和可信 `EnterpriseId + ResourceId + CollectorId` 覆盖。
- [x] `M7-INGEST-01` 实现 OTLP gRPC/HTTP、mTLS 身份解析、Redis fail-closed 限流、大小/属性限制、可信身份覆盖和 Kafka ACK。
- [x] `M7-KAFKA-01` 实现 Signal/DLQ Topic、ACL、至少一次语义、Offset 门禁和受控 DLQ 重放。
- [x] `M7-WRITER-01` 使用最小 Go Writer 实现 OTLP 解码、ClickHouse 写入、Offset 和 DLQ，不承担查询或授权。
- [x] `M7-CH-01` 实现 Metrics/Logs/Traces Local/Distributed Schema、Migration、TTL、分片和 Projection。
- [x] `M7-QUERY-01` 实现版本化 Query Schema、Enterprise/Resource 强制过滤、Signal 权限、字段脱敏、预算和 `partial`。
- [x] `M7-NODE-01` 实现 KubernetesNodeHostBinding 与 CollectionClaim 冲突/迁移规则。
- [x] `M7-WEB-01` 引入 ECharts，完成基础 Metrics/Logs/Traces 查询、Collector 状态和路由页面。
- [x] `M7-TOOL-01` 实现 Agent/Card 共用的 Metrics/Logs/Traces Query Tool 和安全投影。

## 2026-08-19 实施状态

M7 的 Linux arm64 Host 与 Kubernetes Evaluation 遥测闭环已经完成。控制面、三种 `argus-telemetry` 运行模式、独立 Telemetry PKI、锁定 OCB Distribution、Host/Kubernetes 安装执行器、Kafka/ClickHouse 数据面以及 Web/Agent/Card Query 均已进入真实路径。

Kubernetes Agent 与 Gateway 使用各自本地生成的私钥/CSR 和短期证书完成 mTLS；Gateway 只接受内嵌 Collector ID、证书序列与外层可信身份一致的同 Collector 转发，Bastion 下游仍要求独立 Route/Gateway/Leaf 关系。NodeBinding 的人工确认哈希只绑定 Node UID、Node Name、Provider ID、Machine ID 和 System UUID 等稳定强身份，IP 只参与候选匹配，不因短暂 IPv4/IPv6 集合波动误撤权；强身份漂移仍使 Binding 失效。Kubelet 采集使用宿主 kubelet 证书链和最小 `nodes/stats` RBAC，保持 `insecure_skip_verify: false`。

`make e2e-m7-k8s` 于 2026-08-19 以最终运行号 `20260819140437-21054` 通过：覆盖 Linux arm64 Collector 构建与安装、Kubernetes Agent/Gateway mTLS、NodeBinding 保持与漂移、真实 Metrics/Logs/Traces、Kafka backlog、永久坏记录 DLQ 隔离与受控 replay、Redis outage 持久队列恢复、Ingest/Writer/Query Pod 删除恢复、Telemetry Card 激活、M2-M5 real Playwright 和 M7 `zh-CN/en-US × light/dark` real/a11y 流程。`bastion_gateway` 为硬门禁，验证 Leaf mTLS 身份经 Edge Gateway 覆盖后，Metrics、Logs 和 Traces 均进入 Ingest、Kafka、Writer、ClickHouse 和对应授权 Query；Gateway 下游管线禁止跨 Collector batch，避免不同可信主体被合并为一个 OTLP 请求。Query 安全矩阵覆盖跨企业拒绝、DataScope `partial`、预算、敏感字段脱敏、AuthorizationVersion 失效以及 Web/Agent/Card 投影一致性。脱敏证据位于 `artifacts/m7-e2e/20260819140437-21054`；三个临时 Namespace、运行相关 PVC 和集群 Lease均已删除。

实现还固定了 canonical Operation Plan Hash、Kafka `IdempotentWrite`、严格 Artifact TLS 和 Query Redis 并发门禁。M8 本地 Profile 只同步 Linux arm64 Distribution；Windows amd64 保持不可选择，Linux amd64、真实 Windows、生产容量/HA 和长期 PKI 演练进入 Production Validation 清单。M8 本地范围补齐 OpenBao、最小 PostgreSQL Login、备份恢复和发布证据。

Evaluation 阶段不增加第五个自研控制服务。Ingest 和 Writer 通过各自的窄领域 Adapter 读取或结算 PostgreSQL 中的 Telemetry 控制事实。M8 `local-hardening` 已为两者签发独立 Login Role：Ingest 只读 Collector/Certificate/Route，Writer 只读写 Retention、Usage 与 DLQ；数据库级权限不允许访问身份、资源或 Action 表。

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
- Linux arm64 Host 与 Kubernetes 的临时 Namespace E2E 可重复通过并无条件清理。

## 不包含

- 任意 SQL、任意 Collector YAML、Profiles 信号和高级尾采样。
- 独立 TelemetryGroup 可在 `direct_argus/bastion_gateway` 稳定后延后。
- Windows amd64 在 WinRM 管理 Adapter、Windows Service 生命周期和实体兼容验证完成前保持不可选择；这些工作与 Linux amd64 支持矩阵一起进入 Production Validation。
