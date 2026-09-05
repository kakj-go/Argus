# PlanV4 Task 04：反向隧道传输（场景②③）

## 状态

已完成。Executor/Connector 共享隧道监督原语、PostgreSQL desired/lease/epoch/fence、凭据续租与撤销、容量限制、production NetworkPolicy 和跨副本恢复均已实现并通过专项 E2E。

## 目标

实现统一反向隧道原语的两个发起者：Direct Executor（场景②，forward 目标为平台内部 ingest）与堡垒机 Connector（场景③，forward 目标为堡垒机本机 Gateway listener）；交付隧道注册表、监督器、配置渲染、网络策略、凭证租约与路由测试语义。

## 前置条件

Task 01 迁移与契约完成；Task 03 卡片门禁可放开。

## 任务清单

### Executor 侧（场景②）

- [x] P4-TUN-01 隧道 Reconciler（Direct Executor 进程内，模式对齐 `internal/hostprobe` Reconciler）：按 `telemetry_tunnels.desired` 行 SKIP LOCKED 认领 → 复用 SSH 底座（pinned host key、`tunnel` 用途 Credential Lease、DNS/禁用网段校验，与 `ApplySSH` 同源）建立 SSH 并开 remote forward（`ssh.Client.Listen` 语义，目标回环 `127.0.0.1:<loopback_port>` → Executor 拨号 forward 目标）。
- [x] P4-TUN-02 forward 目标配置：Direct Executor 配置增加 `ARGUS_TELEMETRY_INGEST_FORWARD_ENDPOINT`（集群内部 svc gRPC 端点，避免经 ingress 回环）；加入 `internal/config/direct_executor.go` 的 fail closed 校验（未配置而存在 desired 隧道时拒绝启动）。
- [x] P4-TUN-03 监督与接管：基础 keepalive、租约、epoch/fence 和重建路径已实现；补齐心跳驱动恢复、跨副本接管、Executor 重启后的全量重建证据与失败状态收敛。
- [x] P4-TUN-04 NetworkPolicy 与部署：evaluation profile 当前仍使用较宽 executor egress；补齐 production direct-executor → telemetry ingest 4317 最小放行、每副本并发上限、字节速率观测和 `TUNNEL_QUOTA_EXCEEDED` 行为。

### Connector 侧（场景③）

- [x] P4-TUN-05 Connector 隧道任务：作为 Collector 管理职责的组成部分新增长驻隧道任务（不新增协议面）——服务端下发 desired 状态（经现有类型化命令通道），Connector 对成员建 SSH + remote forward 到本机 Gateway listener（`127.0.0.1:4317`，目标同 scope 成员校验），状态经心跳/命令结果回写；fencing epoch 与现有 Connector 命令状态机一致。
- [x] P4-TUN-06 前件校验：成员隧道路由仅允许「所属 Scope 堡垒机已启用 Gateway Profile 且路由测试通过」；跨 Scope、堡垒机对自身、独立主机申请 bastion_tunnel 全部拒绝（`COLLECTOR_ROUTE_TRANSPORT_INVALID`）。

### 共用

- [x] P4-TUN-07 配置渲染：`configbundle` 支持 `transport != direct`——出口端点渲染为回环地址、强制 `server_name_override`（executor_tunnel 用 ingest 域名；bastion_tunnel 用 Gateway server name）、保留原 CA、拒绝任何 insecure 组合；`collectormanager.Validate` 接受回环端点仅当 transport 匹配且 loopback_port 与隧道行一致。
- [x] P4-TUN-08 安装/路由测试顺序：Preview 增加「建立 SSH 隧道（回环端口 X → forward 目标）」变更项与隧道前件冻结；执行顺序固定为 建隧道 → 装/配 Collector → 经隧道健康检查与首条数据验证 → route active。loopback 端口冲突（目标占用）在 Preview 探测并可固化替代端口。
- [x] P4-TUN-09 拆除顺序：卸载/切路由先停 Collector 推送并处理队列策略，再拆隧道；route invalidated 时隧道行转 removed，凭证租约同步失效。
- [x] P4-TUN-10 状态呈现：隧道 established/degraded/down 独立于 Collector 健康呈现；断开原因、epoch、重建历史进主机详情与审计；Collector 侧队列水位告警联动（隧道长时间 down 时提示磁盘队列风险）。

## 关键设计

- **Executor 只见密文**：Leaf↔Ingress/Ingest mTLS 端到端不变，隧道是纯传输；回环监听被本机其他进程连接时因无 Leaf 私钥无法通过认证。
- **unix socket 加固不阻塞**：v1 用回环 TCP；`streamlocal-forward` unix socket 作为后续加固项另行评估（含 x/crypto/ssh 支持验证）。
- **隧道 ≠ 会话**：不占用远程会话通道与票据体系；凭证租约是唯一的长期授权物，撤权链路与 AuthorizationVersion 复用现有实现。

## 退出标准

P4-TUN-03/04 完成；真实 E2E（Task 05）证明：成员→堡垒机 4317 被禁断、目标→ingest 被禁断两类 NetworkPolicy 场景下，三信号仍经隧道全通；Executor Pod 删除后隧道自动重建且 Collector 队列无丢失升级；撤权立即断隧道；审计与配额生效；`go test ./...`、`go vet`、契约与前端门禁通过；单文件 < 2000 行。


## 实施回顾（2026-09-01）

Executor 侧 Reconciler（`internal/directexecutor/tunnel.go`）与 Connector 侧成员隧道（`internal/app/connector/tunnel.go`）复用 `internal/tunnelruntime` 的 keepalive、字节计数、限速、退避和关闭语义。双方均按数据库 desired 行使用 SKIP LOCKED 认领，以 lease、epoch、fence 和 owner 守卫阻止旧副本写入；心跳重新下发 desired，授权版本或凭据撤销立即拆除。

运行 `20260901-planv4-final41` 验证 Executor Pod 删除后的 owner 接管、成员遥测隧道重建、三信号推通和零残留。Evaluation 默认配额为每副本 64 条 telemetry tunnel、32 条 control tunnel 和聚合 64 MiB/s，production 必须显式配置正数；超限返回 `TUNNEL_QUOTA_EXCEEDED`。Docker Desktop 的 CNI 不提供可验证的 NetworkPolicy enforcement，因此真实运行保留 degraded 提示，production 精确策略由 Helm/static production 门禁验证。
