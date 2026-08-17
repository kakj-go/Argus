# M6：人工远程访问闭环

## 目标

交付与 Agent/Automation 完全隔离的人工 SSH/WinRM 访问能力，具备最小授权、短期票据、录像、撤权和强制终止。

## 前置条件

- M2 RoleBinding/DataScope/AuthorizationVersion 完成。
- M3 Connector、ManagedAccount、Credential Broker 和 Direct Executor 完成。
- M4 Approval、审计和持久化状态机完成。

M3 ConnectorCommand 未包含 Remote Access Frame，M3 Direct Executor 也只执行连接探测和 Kubernetes 读取；本里程碑才增加人工会话 Listener、Ticket、PTY/Runspace 和录像链路。

## 任务

- [ ] `M6-GRANT-01` 实现 RemoteAccessGrant 的显式 Host ID/标签选择器、ManagedAccount、协议、动作和有效期。
- [ ] `M6-LEASE-01` 实现 AccessRequest/AccessLease，与单次 ActionApproval 分离。
- [ ] `M6-SESSION-01` 实现 RemoteAccessSession 状态机、理由、MFA/JIT/审批和会话策略快照。
- [ ] `M6-TICKET-01` 实现绑定浏览器 Session/User/Enterprise/Host/Account/Action/AuthorizationVersion 的一次性短期票据。
- [ ] `M6-SSH-01` 实现 Connector/Direct SSH PTY、Host Key 校验、窗口调整和闲置/最长时长。
- [ ] `M6-WINRM-01` 实现受审计 PowerShell Runspace，UI 不伪装完整 PTY。
- [ ] `M6-GATEWAY-01` 实现 Gateway 多副本路由、Drain、背压和活动会话终止。
- [ ] `M6-RECORD-01` 实现结构化命令事件、会话录像分片、Artifact 上传、完整性和读取授权。
- [ ] `M6-REVOKE-01` 实现用户/Grant/DataScope/标签/企业变化对未使用票据、等待会话和活动会话的撤权。
- [ ] `M6-WEB-01` 引入 xterm.js，完成终端、会话状态、审批、终止和录像查看。

## 测试

- 没有 Grant、DataScope 或 ManagedAccount 权限时不能建立会话。
- 票据重放、跨浏览器/用户/Host/协议使用和过期均失败。
- AI、Card、Automation、OpenSandbox 无法创建或消费人工会话票据。
- 撤销 Grant 或授权敏感标签后，未使用票据立即失效，活动会话按策略终止并审计。
- Gateway Pod 删除后会话行为符合协议，录像不只保存在 Pod 本地。

## 退出标准

- 授权用户可通过 SSH 或 WinRM 完成可录像、可终止的人工会话。
- 文件上传、下载、剪贴板、分享和端口转发默认关闭且独立授权。
- Remote Access 与自动化 Execution 在接口、票据、队列和审计上完全分离。

## 不包含

- RDP、SFTP 和通用端口转发。
- 复杂逐命令审批。
