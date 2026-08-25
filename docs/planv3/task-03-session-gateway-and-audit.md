# PlanV3 Task 03：会话网关、审计与生产可靠性

## 目标

将 SessionProfile 的限制真正落实到终端、Gateway、Connector、Direct Executor、录像和审计，达到生产环境的安全边界和故障恢复要求。

## 任务清单

- [ ] P3-SESSION-01 将 SessionProfile 快照写入 RemoteAccessSession，连接期间不再读取可变配置。
- [ ] P3-SESSION-02 对最大时长、空闲超时、并发限制和连接窗口统一服务端强制。
- [ ] P3-CHANNEL-01 对剪贴板、文件上传/下载、端口转发、会话分享和命令审计建立显式能力检查；未实现能力保持关闭。
- [ ] P3-TICKET-01 保持一次性 opaque Ticket、HTTP Session/用户/企业/目标/协议/Lease/版本/Fence 绑定。
- [ ] P3-GATEWAY-01 保持 Gateway peer mTLS、跨副本路由、Drain、Connector epoch 和 Direct Executor 网络边界。
- [ ] P3-RECORD-01 完善录像 required/optional/disabled 语义；required 时对象存储不可用必须 fail closed。
- [ ] P3-AUDIT-02 增加申请、审批、Session、命令哈希、录像读取、终止、撤权和配置变更审计。
- [ ] P3-HA-01 验证 Gateway、Server、Worker 横向扩展，Redis 故障和 PostgreSQL 恢复路径。
- [ ] P3-DR-01 固化录像索引、加密材料、ObjectStore、数据库备份和恢复演练。
- [ ] P3-SECURITY-01 完成 Ticket/Secret/Cookie/Authorization/录像内容的日志脱敏和生产制品扫描。

## 可靠性不变量

- 浏览器、Agent、Card 和 Sandbox 永远不能获得人工会话 Ticket。
- Gateway 不得成为任意 TCP 代理。
- 录像策略要求 required 时无法可靠记录就不能建立或继续会话。
- Gateway Drain 不得遗失已落盘录像分片和终止审计。
- Redis 只做加速；清空 Redis 不得丢失 Grant、Lease、Session 或审计事实。
- 授权版本变化必须在 Ticket 握手和活动会话心跳中重新校验。

## 退出标准

SSH PTY、WinRS、录像、终止、撤权、跨 Gateway、Redis 清空、对象存储中断、Pod 重启和 Drain 场景均通过真实 E2E；生产配置下 NetworkPolicy、PDB、ServiceAccount、mTLS 和日志扫描门禁通过。
