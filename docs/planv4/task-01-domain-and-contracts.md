# PlanV4 Task 01：领域模型与契约

## 状态

已完成。`00028～00031` 以全新数据库为基线重写，旧 API/DTO/默认值不提供兼容；OpenAPI、protobuf、SQLC 和 TypeScript/Go 生成物已在 2026-09-01 最终门禁中验证为当前版本。

## 目标

为 `self_enrolled` 连接模式、`transport` 传输维度、隧道状态与自助安装令牌建立数据库迁移、OpenAPI 契约、错误码和生成物；不改变现有路由身份语义与 Connector 协议面。

## 任务清单

- [x] P4-DOMAIN-01 迁移 `00028`：`hosts.connection_mode` 增加 `self_enrolled`；放宽 `address`（0..512）与 `port`（0..65535，仅 self_enrolled 允许 0）CHECK；复合 CHECK 增加 `self_enrolled AND bastion_scope_id IS NULL AND connector_id IS NULL` 分支。基线：`migrations/postgresql/00002_m3_resources_connectors.sql:147-165`。
- [x] P4-DOMAIN-02 迁移 `00028`：新表 `host_enrollment_tokens`（令牌哈希、企业、预分配 host_id、冻结计划哈希、`active→consumed/revoked/expired` 状态机、remaining_uses、expires_at、设备指纹摘要、审计元数据），语义对齐 Bastion 注册令牌（docs/03 §3.4）。
- [x] P4-DOMAIN-03 迁移 `00029`：`telemetry_routes` 增加无默认值的必填 `transport`（`direct|executor_tunnel|bastion_tunnel`）与 `loopback_port`；合法组合 CHECK（`kubernetes_gateway` 恒为 direct；非 direct transport 要求 loopback_port 非空）。调用方和执行命令缺少 transport 时 fail closed。
- [x] P4-DOMAIN-04 迁移 `00029`：新表 `telemetry_tunnels`（结构见 PlanV4 §4.2；`UNIQUE(collector_id)`、epoch/fence/lease 列对齐 `telemetry_collector_operations` 现有模式）；`telemetry_collector_operations.executor_kind` CHECK 增加 `bootstrap`。
- [x] P4-DOMAIN-05 Credential Lease 用途增加 `tunnel`：绑定 host/collector/tunnel epoch，可续租、撤权与 AuthorizationVersion 失效复用现有 Lease 框架。
- [x] P4-CONTRACT-01 OpenAPI：host 创建/详情增加 `connection_mode=self_enrolled` 分支（免 ConnectionTest 引用、令牌生成与一次性展示语义对齐 `in_cluster` 安装命令）；host 详情增加 transport/隧道状态投影；telemetry 路由 DTO 增加 `transport`、`loopback_port`。
- [x] P4-CONTRACT-02 OpenAPI：bootstrap 交换端点 `GET /host-install/{token}`（返回冻结计划、config bundle、产物清单与端点；限流、审计、一次性语义）与独立的 `/host-uninstall/{token}`、完成回调端点。
- [x] P4-CONTRACT-03 错误码登记（`api/contracts/error-codes.yaml`，遵守「代码返回值必须登记」门禁）：`TOKEN_ALREADY_CONSUMED` 复用；新增 `HOST_SELF_ENROLL_UNSUPPORTED_PLATFORM`、`HOST_OPERATION_UNSUPPORTED_FOR_SELF_ENROLLED`、`COLLECTOR_ROUTE_TRANSPORT_INVALID`、`COLLECTOR_LOOPBACK_PORT_CONFLICT`、`TUNNEL_FORWARD_TARGET_UNCONFIGURED`、`TUNNEL_QUOTA_EXCEEDED`。
- [x] P4-CONTRACT-04 重新生成 bundled OpenAPI 与 Go/TypeScript 客户端；执行契约 lint、生成漂移与 breaking check。
- [x] P4-CONTRACT-05 protobuf：`CollectorManagementCommand` 增加 `transport` 与 `loopback_port` 字段（仅用于执行侧校验回环端点与 server name override 的豁免逻辑），`Validate` 白名单同步（`internal/collectormanager/manager.go:122` 附近）；`configbundle.RenderInput` 增加 `Transport`/`TunnelLoopbackPort`（`internal/otelcol/configbundle/config.go:23`），渲染规则见 PlanV4 §3。
- [x] P4-DOMAIN-06 `00028` 将安装令牌与 30 分钟卸载授权拆为 `host_enrollment_tokens`、`host_uninstall_tokens`；`00030` 固化一次性结果 v2、Execution 领取状态和 operation/resource 引用。
- [x] P4-DOMAIN-07 `00031` 新增 `connector_release_versions`、阶段事件化的 `connector_install_operations` 与独立 `connector_control_tunnels`；B/C operation secret 只以加密 envelope 保存。
- [x] P4-CONTRACT-06 Host/BastionScope 的统一 `onboarding` 投影覆盖可领取、已领取、过期、审批、安装中、失败和已注册；前端不再拼接 token、Execution 与 operation 状态。

## 数据不变量

- `self_enrolled` 主机：无 bastion_scope/connector 引用；address/port 为空仅允许出现在 `onboarding` 状态；enrollment 成功前不可被任何执行器命令派发。
- 令牌消费必须走数据库条件更新（`status=active AND remaining_uses=1 AND expires_at>now()`），不得用进程内锁；明文令牌只出现在创建响应与安装命令中，库内仅存哈希。
- `transport != direct` 的 route 必须存在对应 `telemetry_tunnels` 行（status 不低于 desired）才能转 active；隧道删除必须先使 route 进入 invalidated。
- `executor_kind=bootstrap` 的 operation plan 在令牌生成时冻结，后续任何变更生成新令牌而不是修改原计划。
- 任何导出配置渲染不得出现 `tls.insecure=true`；transport!=direct 时必须携带 server name override。

## 退出标准

迁移以全新开发数据库为验收基线；契约生成、lint、sqlc 与前端 typecheck 通过，breaking check 用于验证当前契约与生成物自洽；`go test ./...`、`go vet` 通过；单文件 < 2000 行；①/标准成员的身份与路由不变量保持成立。当前版本尚未发布，不要求兼容旧 Evaluation 数据、旧开发期 API 或旧生成物；发现遗留结构时直接清理，不保留双写或转换分支。
