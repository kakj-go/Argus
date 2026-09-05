# Task 04：可选 OpenSandbox、四个基础工具与 MCP 执行器

## 目标

把 OpenSandbox 从生命周期管理扩展为 Agent Workspace Runtime。`read/bash/edit/write` 和 stdio MCP 只能在 Sandbox 内执行；OpenSandbox 未配置或不可用时，这些工具从能力快照中消失，Agent、Native Tool 和未来 Remote MCP 继续运行。

## 当前差距

[`internal/integration/opensandbox/client.go`](../../internal/integration/opensandbox/client.go) 当前只有 Health、Create、Get、List、Renew 和 Delete。 [`internal/sandbox/service.go`](../../internal/sandbox/service.go) 只根据 Task/Profile 创建并对账 Session，没有命令、文件、进程、stdin/stdout 或 Workspace 重附着能力。

因此不能把四个基础工具直接注册到当前 Runner；必须先建立 Sandbox Tool Proxy 和 Sandbox 内 Supervisor。

## 交付内容

### P5-S01：OpenSandbox 可选配置

- [ ] Helm/Config Schema 允许显式关闭或不配置 OpenSandbox。
- [ ] Agent Worker Readiness 不依赖 OpenSandbox；Sandbox 专用能力单独报告状态和原因。
- [ ] 未配置时不创建 Sandbox Backend/Profile 默认对象，也不持续产生错误重试任务。
- [ ] 已配置但不健康时只暂停 Sandbox 类调用，不影响 Native Tool、模型调用和远程 MCP。
- [ ] Platform 管理页显示 `not_configured/unhealthy/ready`，企业工作台不获得 Backend/Profile 选择权。
- [ ] 不允许通过用户请求临时配置 Endpoint、镜像或网络。

### P5-S02：Agent Workspace Profile

- [ ] 增加固定 `agent_workspace` Task Kind/Profile 契约。
- [ ] Profile 固定批准镜像 Digest、CPU、内存、临时磁盘、PID、网络和 TTL。
- [ ] Sandbox 绑定 `enterprise_id + conversation_id + workspace_revision`，而不是一次 Tool Call 一个 Session。
- [ ] 新 Run 可以重附着未过期 Workspace；并发 Run 通过 Lease/Fence 避免互相破坏。
- [ ] Workspace 过期或失效后创建新 Revision；需长期保留的文件显式保存 Artifact。
- [ ] 配额统计区分 Workspace Session、活跃秒数、命令和 stdio MCP 进程资源。

### P5-S03：Sandbox Tool Proxy

- [ ] 定义与具体 OpenSandbox SDK 解耦的 `WorkspaceRuntime` 接口。
- [ ] 实现 `Ensure/Exec/Read/Write/Edit/StartProcess/Send/Cancel/Terminate`。
- [ ] 所有调用带企业、会话、Run、ToolCall、Workspace Revision、Deadline 和预算。
- [ ] 实现路径规范化、Root 限制、Symlink/Traversal 防护和文件大小限制。
- [ ] 实现 stdout/stderr 分离、输出截断、退出码、超时和取消。
- [ ] 实现进程树回收和 Workspace 终止，不依赖 Worker 本地 PID 作为唯一事实。
- [ ] OpenSandbox API 不支持所需能力时，在批准镜像中运行受控 Supervisor，并通过隔离内部通道通信。

### P5-S04：四个基础工具

- [ ] `read`：只读 Workspace Root 下文件，支持行范围和大小上限。
- [ ] `write`：原子创建/覆盖文件，父目录策略明确，拒绝设备/特殊文件。
- [ ] `edit`：基于精确旧文本或补丁应用，匹配歧义时拒绝，不静默选择。
- [ ] `bash`：参数化 Shell 启动、固定工作目录、超时、输出上限和取消。
- [ ] 四个工具全部通过 SandboxBuiltinExecutor，不接触 Worker 宿主 `os/exec` 或文件系统。
- [ ] Tool Result 使用原生 Tool Message；大输出进入 Artifact/Projection。
- [ ] Snapshot 非 `ready` 时四个 Schema 不进入模型请求。

### P5-S05：Sandbox Supervisor

- [ ] Supervisor 使用版本化内部协议并仅监听 Sandbox 隔离网络/Channel。
- [ ] 管理命令、文件操作和长驻 stdio MCP 子进程。
- [ ] 保存可重建的进程元数据；Worker 重启后可以对账或明确终止失联进程。
- [ ] stdout 采用背压和上限，stderr 作为独立受控诊断，不混入 MCP JSON-RPC。
- [ ] Workspace 结束时终止全部进程、删除临时 Secret 和临时文件。
- [ ] Supervisor 镜像与 Agent Workspace 镜像使用批准 Digest 和供应链清单。

### P5-S06：stdio MCP

- [ ] 平台配置定义 MCP Server ID、批准命令/参数、镜像、环境变量 Secret Ref、分类映射和启用状态。
- [ ] 模型不能指定命令、包、下载 URL、镜像、Environment 或 Secret。
- [ ] Capability Probe 为 `ready` 后，Tool Catalog 才允许发现对应 `sandbox_stdio_mcp` Tool。
- [ ] 首次需要时懒启动 Server，执行 `initialize` 与 `tools/list`，并将结果和配置 Manifest 对账。
- [ ] JSON-RPC 消息严格按 stdio MCP framing；stdout 非协议内容触发 `MCP_PROTOCOL_ERROR`。
- [ ] 实现取消、超时、最大消息、并发、崩溃退避、重启次数和 Workspace 回收。
- [ ] 上游 Tool Schema 映射到 Argus Manifest；调用仍经过 Argus 权限、输入校验和 Result Projector。
- [ ] stdio MCP 的富展示只能来自 MCP Server 自身 Result 的 Presentation Extension；缺失时不绑定平台模板。
- [ ] stdio MCP 进程只能存在于 Sandbox 进程树，Worker 宿主不得启动。

### P5-S07：Remote MCP Adapter 边界

本阶段不提供企业用户配置入口，但完成可测试接口：

- [ ] 使用服务端 Streamable HTTP Client，支持 JSON 与可选 SSE 响应。
- [ ] 如需旧 HTTP+SSE，使用显式 Compatibility Mode，默认先使用 Streamable HTTP。
- [ ] Endpoint 必须来自平台配置并通过 TLS、认证、SSRF、DNS Rebinding 和私网策略检查。
- [ ] 支持 MCP Session ID、取消、超时、重连和结果大小限制。
- [ ] 浏览器、Template 和模型参数不能传入 Endpoint 或认证 Header。
- [ ] 远程 Tool 同样映射到 `category + name`，并返回统一 Tool Result Envelope。
- [ ] Remote MCP Presentation Extension 按不可信模板校验；Argus 不维护 Remote Tool 到平台模板的映射。

### P5-S08：故障降级

- [ ] 启动时未配置：Capability=`not_configured`，无错误噪声，Agent 正常。
- [ ] Backend 不健康：Capability=`unhealthy`，四工具/stdio 不可发现。
- [ ] Profile 缺失：Capability=`profile_unavailable`，平台显示配置诊断。
- [ ] 配额不足：新 Workspace 不创建，当前调用返回稳定配额错误，不回退宿主。
- [ ] 运行中断连：当前调用返回 `SANDBOX_UNAVAILABLE`，后续 Run 刷新 Snapshot。
- [ ] stdio MCP 崩溃：只影响该 Server/Workspace；风险允许时按策略重启，不重放有副作用调用。

## 安全要求

- [ ] 默认无生产 Secret、Connector Credential、RemoteAccessTicket、宿主挂载和 Kubernetes API。
- [ ] 网络默认 `none`；只有 Platform Manifest 与 Profile 同时允许才启用最小 egress。
- [ ] 禁止特权容器、Host PID/Network、Docker Socket 和任意 RuntimeClass 降级。
- [ ] Secret 以进程级短期注入，不能写入 Tool Result、日志、Artifact 或持久化 Workspace。
- [ ] 命令、文件路径、stdout/stderr 和 MCP Result 全部执行大小、分类和脱敏限制。
- [ ] Production 继续要求强化 Runtime；Evaluation 降级必须显式显示。

## 测试

- [ ] 无 OpenSandbox 配置下 Agent + Native Tool 完整 E2E。
- [ ] `read/write/edit/bash` 同 Workspace 多轮一致性与跨 Run 重附着。
- [ ] 路径逃逸、Symlink、特殊文件、超大文件和命令超时。
- [ ] Worker 重启、Redis 清空、Sandbox API 短暂中断和 Workspace 过期。
- [ ] stdio MCP initialize/list/call/cancel、非法 stdout、超大消息、崩溃和重启。
- [ ] Worker 宿主进程树和文件系统中不存在 stdio MCP/用户命令执行证据。
- [ ] Remote MCP JSON、SSE、Session、认证失败、SSRF 和超时测试。
- [ ] Sandbox 配额耗尽不影响普通 Agent Worker Readiness。
- [ ] 临时 Kubernetes Namespace 结束后没有 Sandbox、进程、PVC、Artifact Fixture 或 Lease 残留。

## 完成标准

1. OpenSandbox 是可选能力，未配置时 Agent 正常运行。
2. 四个 Pi 风格基础工具全部且只在 OpenSandbox 执行。
3. stdio MCP 全部由 Sandbox Supervisor 管理。
4. 远程 MCP 有独立服务端适配边界，不经过浏览器或 Template。
5. 任一 Sandbox 故障都不会触发 Worker 宿主 fallback。
6. 能力快照、工具可发现性和实际执行状态一致。
