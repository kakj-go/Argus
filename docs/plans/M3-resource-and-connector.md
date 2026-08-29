# M3：资源、Secret 与 Connector 闭环

## 目标

让企业管理员不依赖 AI 即可完成：

`Secret/Credential → Host/Kubernetes → Bastion/Connector → 连接测试 → Preview/Confirm → explicit resource authorization 过滤与撤权`

完成状态只代表 Evaluation 资源接入闭环，不代表 Production 安全就绪。

## 固定边界

- M3 提供资源管理专用的最小 PendingAction：`prepared → awaiting_confirmation → executing → succeeded | failed | invalidated | expired | cancelled`。
- M4 在现有 PendingAction/Plan/Token 存储和确认机制上增加 Approval、Execution、Agent、Tool 与 Worker 编排，不重建 M3 基座。
- ConnectorCommand 在 M3 只允许 Connection Probe、Kubernetes Read、Credential Lease 和 Uninstall；禁止任意 Shell、文件写入和 Remote Access Frame。
- Remote Access 属于 M6；Collector、CollectionClaim 与 Telemetry 属于 M7。M3 real 页面不把这些 provisional 操作伪装成可用能力。
- PostgreSQL 保存资源、命令、证书、操作和审计事实；Redis 只保存在线 Registry、跨副本 Pub/Sub 派发提示与路由加速，丢失后由心跳和 PostgreSQL 轮询恢复。
- Direct Executor 使用独立 Deployment、ServiceAccount、NetworkPolicy 和固定出口；Server 通过独立 CA 的内部 mTLS gRPC 发送低延迟派发提示，PostgreSQL 队列负责事实与恢复。
- Connector PKI 使用 cert-manager。`argusctl` 复用同 major/minor 且 patch 不低于锁定基线的实例，否则安装锁定版本；Gateway 使用独立 ServiceAccount 和最小 CertificateRequest RBAC，卸载不得误删共享 CRD。
- Host/Kubernetes 的地址、端口、连接模式或 Bastion Scope 等网络路径变化必须引用与新路径完全匹配的成功 ConnectionTest；Confirm 必须再次校验同一冻结测试。
- Connector 主动卸载使用 PendingAction 和类型化 `connector_uninstall` 命令；客户端只有在 Gateway ACK 覆盖成功结果序号后才能删除本地身份并退出。无法证明本地清理结果的 reconcile 保持 `result_unknown`。
- M8 本地范围已补齐 MFA、OpenBao Transit 与备份恢复；Production KMS/CA HA、灾备和固定出口证明继续由 Production Validation 阻断。

## 任务

- [x] `M3-CONTRACT-01` 冻结 Host、Kubernetes、Secret、Credential、ManagedAccount、BastionScope、Connector、ConnectionTest、ConnectorCommand 和资源 PendingAction 契约，生成 Go Strict Server 与 TypeScript DTO。
- [x] `M3-DATA-01` 增加 M3 Goose Migration、sqlc 查询、企业/状态/Scope/标签索引和资源版本约束。
- [x] `M3-LABEL-01` 实现 Host/Kubernetes `labels` 存储、索引、过滤、分组、批量选择和 `argus.io/*` 保护。
- [x] `M3-LABEL-02` 保留资源标签校验、展示和筛选；动态标签授权、影响预览和标签驱动的 AuthorizationVersion 失效已移除。
- [x] `M3-SECRET-01` 实现独立 DEK 的 AES-256-GCM Envelope Encryption、版本化 KEK、Credential、ManagedAccount 和最多五分钟的一次性 Lease。
- [x] `M3-ACTION-01` 实现资源专用 PendingAction、私有 Plan/Token、幂等 Confirm/Cancel 和敏感字段扫描。
- [x] `M3-HOST-01` 实现 Host 列表/详情、资源版本、三种连接模式、连接测试和 Preview/Confirm 变更。
- [x] `M3-K8S-01` 实现 Kubernetes 三种接入模式、连接测试、Namespace/Node/Pod/Workload/Service 有界读取和 1 MiB Pod Logs。
- [x] `M3-CONNECTOR-01` 实现 Header Enrollment Token、CSR/mTLS、证书轮换/吊销、心跳、fencing 和 `connection_epoch`。
- [x] `M3-BASTION-01` 实现稳定 BastionScope、预分配根 Host、单次安装结果、Replacement、成员关系和删除门禁。
- [x] `M3-GATEWAY-01` 实现 Connector Gateway Registry、跨副本路由、Redis 重建和 Drain。
- [x] `M3-COMMAND-01` 实现类型化 ConnectorCommand、幂等、过期、DeliveryUnknown/ResultUnknown 和对账。
- [x] `M3-DIRECT-01` 实现 Direct Executor 内部 mTLS RPC、持久队列恢复、固定 IP Dial、DNS 前后校验、重定向拒绝、Host Key 与高风险地址/禁用网段阻断。
- [x] `M3-DEPLOY-01` 增加 Connector/Direct Executor PKI、Trust Bundle、RBAC、Service、NetworkPolicy、出口配置和 cert-manager 版本锁。
- [x] `M3-WEB-01` 将 Host、Kubernetes、Secret 和 Connector 页面接入 real API，使用生成 DTO、统一 Labels 控件和 Preview/Confirm 流程。
- [x] `M3-E2E-01` 在临时 Namespace 完成 Connector、Bastion、经堡垒机/直连 Host、Kubernetes、撤权、Redis 清空、Gateway/Server 重启和 real Playwright，并无条件清理。

## 实现证据

- 契约：`api/openapi/components/m3.yaml`、`api/openapi/paths/m3.yaml`、`api/proto/argus/connector/v1/commands.proto`、`api/proto/argus/directexecutor/v1/direct_executor.proto`。
- 领域：`internal/{resource,secret,connector,directexecutor,kubernetesreader}`。
- 数据：`migrations/postgresql/00002_m3_resources_connectors.sql`、`internal/storage/postgres/queries/{resources,secrets,connectors}.sql`。
- 部署：`deploy/helm/argus-platform/templates/{connector-pki,direct-executor-pki,m3-network-policies}.yaml`。
- 前端：`web/packages/api-client/src/generated/*api.ts` 与 Enterprise Host/Kubernetes/Secret/ManagedAccount 页面；凭据管理按密钥、连接凭证、托管账号三个 Tab 分区，并由页面头部提供统一创建入口。
- E2E：`internal/app/argusdev/e2e_scenario_m3*.go` 负责集群与业务编排，`web/apps/enterprise/e2e/m3-real.spec.ts` 保留 Playwright 浏览器验证。
- 生命周期：Connector 重连恢复 Bastion Scope/Kubernetes 连接状态；Scope 删除只允许已卸载或已 fencing 的离线状态，并逻辑删除根 Host，保留审计与历史引用。
- 前端一次性结果：`in_cluster` 确认后展示一次安装命令，关闭抽屉后清除，不进入 URL、storage 或日志。

## 测试

- Labels、explicit resource authorization、PendingAction 幂等、Secret 加密/轮换和敏感字段深度扫描。
- Connector Token 竞争、CSR/设备幂等、证书轮换/吊销、旧 epoch、Gateway 跨副本派发、Drain、Redis 清空、命令超时收敛和未知结果对账。
- Direct Executor 与 Kubernetes Reader 允许部署网络可达的 RFC1918、IPv6 ULA 和内部 DNS，拒绝环回、链路本地、云元数据、配置的禁用网段、DNS rebinding、重定向、错误出口、Host Key 变化和 TLS 绕过。
- Kubernetes 覆盖三种接入模式、Namespace explicit resource authorization、跨企业拒绝、列表边界和 Pod Logs 大小限制。
- 全量门禁：`go run ./cmd/argus-dev contracts check`、`go run ./cmd/argus-dev contracts breaking`、`go test ./...`、`go vet -stdmethods=false ./...`、`pnpm typecheck/lint/test/build/check:bundle/check:real-build/e2e`。
- 临时集群门禁：`go run ./cmd/argus-dev e2e run --suite m3`，成功或失败均导出脱敏诊断并删除 Namespace/PVC/Lease。

## 验收记录

- 2026-08-17：契约、Go、前端类型检查/lint/单测/构建、Bundle、real-build 和 mock Playwright 全量门禁通过；mock Playwright 为 32 passed，real 场景按环境开关隔离。
- 2026-08-17：旧 Shell Harness 通过 M2 双 Audience 的 3 条 real Playwright 与 M3 的 6 条 real Playwright，并验证 ConnectionTest 冻结计划、Host 跨 Scope 迁移、Connector 证书轮换与 ACK 后卸载、双 Gateway 跨副本派发、Bastion Replacement/fencing/删除、三种 Kubernetes 接入、explicit resource authorization 撤权、Secret 轮换失效、Redis 清空、Gateway/Server 重启恢复和敏感值扫描。
- 成功运行号为 `20260817060430-49810`，脱敏诊断位于本地 `artifacts/m3-e2e/20260817060430-49810`；验收结束后 M3 临时 Namespace、PVC 和 Lease 均为零残留。

## 退出标准

- 管理后台可真实完成 Secret、堡垒机、经堡垒机 Host、直连 Host 和 Kubernetes 接入。
- 所有列表、详情和 Kubernetes 查询遵守企业边界与 explicit resource authorization；标签变化正确触发撤权。
- 浏览器、日志、审计、命令和 Redis 中不存在 Secret 原值。
- 全量门禁和 M3 临时 Namespace E2E 已通过，以上任务均有代码、测试、部署和清理证据。

## 不包含

- Agent、MCP Commit Tool、Approval 和通用 Execution。
- 人工 Web Terminal、RemoteAccessTicket 和 Remote Access Frame。
- Collector、CollectionClaim、Telemetry Ingest/Query。
- Production KMS/HSM、CA 根轮换、MFA、HA 和灾备。
