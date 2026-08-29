# PlanV3 Task 03：会话网关、审计与生产可靠性

## 目标

在 Task 02 已落实 SessionProfile 不可变快照、录像模式和命令审计模式的基础上，继续完善终端、Gateway、Connector、Direct Executor 的生产安全边界和故障恢复能力。

## 当前边界

Task 02 已把 SessionProfile 快照写入 Session，并在 Session/Gateway 运行时执行录像与命令审计的 `required/optional/disabled` 基础语义。Task 03 不重复实现这部分状态机，重点放在生产可靠性、故障恢复和尚未交付的高级通道能力。

## 任务清单

- [x] P3-SESSION-01 将 SessionProfile 快照写入 RemoteAccessSession，连接期间不再读取可变配置。
- [x] P3-SESSION-02 对最大时长、空闲超时、并发限制和连接窗口统一服务端强制。
- [x] P3-CHANNEL-01 对剪贴板、文件上传/下载、端口转发和会话分享建立显式能力快照与前端风险提示；未实现能力由服务端保持安全拒绝。命令审计三种模式已完成。
- [x] P3-TICKET-01 保持一次性 opaque Ticket、HTTP Session/用户/企业/目标/协议/Lease/版本/Fence 绑定。
- [x] P3-GATEWAY-01 保持 Gateway peer mTLS、跨副本路由、Drain、Connector epoch 和 Direct Executor 网络边界。
- [x] P3-RECORD-01 完成录像 required/optional/disabled 的基础会话语义。
- [x] P3-AUDIT-BASE 完成申请决策、审批、Session、命令哈希、终止、撤权和配置变更的基础审计链。
- [x] P3-RECORD-02 已完成 Worker 留存过期扫描、失效 Session 收敛、optional 录像恢复、审计和 Outbox；对象存储 HA、不可变保留、分片恢复和跨故障域恢复属于尚未关闭的 Production Validation。
- [x] P3-AUDIT-02 完成录像元数据与事件读取审计、留存截止时间展示和敏感字段不出现在 API 响应；生产归档/WORM 仍留待后续 Production Validation。
- [x] P3-HA-01 已在 Evaluation 环境验证 Gateway、Server、Worker 横向扩展、Redis 清空、Worker 全量 Pod 重启和 PostgreSQL Pod 恢复路径；生产多故障域 HA 仍由 Production Validation 管理。
- [x] P3-DR-01 已完成人工/Evaluation 灾备测试：录像索引与加密材料随 PostgreSQL/OpenBao 备份固化，ObjectStore 与 ClickHouse 数据完成加密归档、全新 Namespace 恢复和恢复后校验；生产级 PITR、跨故障域灾备、WORM/Object Lock 和多节点 HA 仍待 Production Validation。
- [x] P3-SECURITY-01 已完成运行态审计字段白名单、Trace ID、敏感字段不落审计、单元测试和 Evaluation 服务日志敏感信息扫描；生产制品扫描、渗透测试和独立安全审计仍待 Production Validation。

## 可靠性不变量

- 浏览器、Agent、Card 和 Sandbox 永远不能获得人工会话 Ticket。
- Gateway 不得成为任意 TCP 代理。
- Gateway 是空闲超时唯一权威；输入、resize 和服务端输出刷新业务活动，协议 ping 只保活且不延长空闲计时。达到冻结 Session Profile 的 `idle_timeout_seconds` 后必须以 `expired/idle_timeout` 收敛 Session。
- 录像策略要求 required 时无法可靠记录就不能建立或继续会话；optional 允许降级但必须审计并发出 Outbox，disabled 不启动录像通道。
- Gateway Drain 不得遗失已落盘录像分片和终止审计。
- Redis 只做加速；清空 Redis 不得丢失 Grant、Lease、Session 或审计事实。
- 授权版本变化必须在 Ticket 握手和活动会话心跳中重新校验。

## 退出标准

开发/Evaluation 退出要求 SSH PTY、WinRS、录像、终止、撤权、跨 Gateway、Redis 清空、对象存储中断、Pod 重启和 Drain 场景通过真实 E2E；该范围已由 `20260826-planv3-final9` 满足。

Production Validation 继续要求生产配置下的 NetworkPolicy 执行、PDB、ServiceAccount、mTLS、日志/制品扫描、WORM/Object Lock、跨故障域灾备和生产数据服务 HA。Evaluation 通过不得自动关闭这些门禁。

## 第三阶段实现记录

第三阶段已完成远程会话控制台所需的活动/历史/录像 API 与桌面端页面、录像读取审计、四类治理配置 Tab、生命周期与引用保护 UI，以及 Docker Desktop 破坏性重置保护脚本。Worker 已使用 PostgreSQL 扫描收敛失效 Session，并恢复 optional 模式下缺失的录像元数据；required/optional/disabled 命令审计、运行态审计详情、幂等写入和企业管理员边界已补齐。

2026-08-26 的真实 Kubernetes Evaluation 运行 `20260826-planv3-final9` 已通过 SSH PTY、HTTPS WinRS、录像、Ticket 重放拒绝、required ObjectStore fail closed、Redis 清空、跨 Gateway 路由、Gateway Pod 删除与 Drain、Server/Worker 双副本、Worker 全量 Pod 重启、PostgreSQL Pod 恢复及运行事实不变检查。`argusctl verify` 同时确认 PostgreSQL、Redis、MinIO、Kafka、ClickHouse 和工作负载健康；清理后未残留运行 Namespace、PVC、Lease、image-loader DaemonSet 或 Registry 容器。已扫描服务日志，未发现已知密码、Setup Token、Authorization、Cookie、Ticket 或终端明文。证据位于 `artifacts/m6-e2e/20260826-planv3-final9/`。

该结果关闭第三阶段开发/Evaluation 的 HA、录像恢复、故障恢复和人工灾备验收。`P3-DR-01` 现标记为人工/Evaluation 完成，证据为 `artifacts/m8-e2e/fv-20260824-m8-final13/`，其中包含备份日志、恢复后验证和 MinIO object round-trip 记录。

该标记不代表生产级灾备认证。WORM/Object Lock、PITR、跨故障域灾备、生产 PostgreSQL/ObjectStore HA、生产 NetworkPolicy 执行、外部 Egress Gateway、强化 Sandbox Runtime 和真实 Windows 兼容性仍待 Production Validation。

### 空闲超时测试补充（2026-08-28）

`businessActivity` 已覆盖 input、resize、服务端 output 刷新业务活动，以及 ping 不刷新业务活动；前端不再运行 15 分钟清理定时器。
