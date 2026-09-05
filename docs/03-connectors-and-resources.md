# Connector、堡垒机、主机与 Kubernetes 资源管理

> Host 与 Kubernetes Cluster 只接受显式资源授权。Host 授权同时约束 Telemetry、Agent 和远程访问；Kubernetes Cluster 授权覆盖 Namespace、Pod、Workload 等下级对象。labels 不参与授权判断。

## 1. Connector 定位

“代理”在 Argus 中统一定义为 Connector。Connector 安装在一台主机上，主动向 Argus 建立出站 mTLS 长连接；当用户用一次性安装命令注册 Connector 时，该主机在产品中成为堡垒机，并形成一个可容纳内网成员主机的 Bastion Scope。

Connector 与 Model Agent 不同，也不等同于 OpenTelemetry Collector。Connector 负责：

- 管理安装它的堡垒机本机。
- 通过 SSH 或 WinRM 管理同一 Bastion Scope 中的内网主机。
- 转发到私有 Kubernetes API Server。
- 执行经过授权的命令和安装任务。
- 通过 Artifact Tunnel 传输平台批准的不可变安装包。
- 为人工用户提供 SSH Web Terminal 等远程访问隧道。
- 后续承载 RDP、SFTP、端口转发和 Connector 高可用能力。

Connector 可以使所在主机承担“类似传统堡垒机”的资源访问和审计职责。第一版中文产品文案将这类 Host 统一称为“堡垒机”，英文称为 “Bastion”；只有在进程、证书、心跳和技术诊断信息中显示 Connector，避免把普通 Model Agent、OTLP 收集器或 SaaS Worker 称为代理。

### 1.1 M3 实现边界

M3 已实现 Secret/Credential、Host/Kubernetes、Bastion Scope、Connector 注册/证书/心跳、类型化只读命令、连接测试和资源专用 PendingAction。M3 ConnectorCommand 只允许 Connection Probe、Kubernetes Read、Credential Lease 和 Uninstall；任意 Shell、文件写入与 Collector/Telemetry 均不属于 M3。M6 已在独立协议面增加 Remote Access Frame，Collector/Telemetry 仍由 M7 实现。

### 1.2 M6 实现边界

M6 已实现 RemoteAccessGrant/Policy、AccessRequest/Lease、一次性 Ticket、SSH PTY、HTTPS WinRS PowerShell 行模式、跨 Gateway peer 路由和加密录像。Remote Access 继续与 Agent、Card、PendingAction 和 Execution 隔离；M8 已补齐本地 MFA、Step-up、Break Glass 和录像备份恢复，Production 不可变保留与真实 Windows Server 兼容矩阵仍由独立 Validation 清单阻断。

Connector PKI 由 cert-manager 签发。安装器复用兼容版本的 cert-manager/trust-manager，否则安装锁定版本。所有 Argus 证书引用同一个全局 `ClusterIssuer`，但 Connector Gateway 服务端与每个 Connector 客户端分别使用独立私钥、用途化 URI SAN 和单一 EKU。Gateway 缺少 Issuer、公共 Trust Bundle 或权限时必须 fail closed；Bundle 以 epoch/SHA-256/CA 指纹更新并由节点 ACK。实例卸载不得删除被其他安装复用的 CRD。完整轮换和修复流程见 [全链路 PKI、TLS 与 Trust Bundle](./18-pki-and-tls.md)。

堡垒机还可以安装 `argus-otelcol` 并启用 Edge Gateway 模式，从而成为遥测中间机；此时用户看到的是一台同时具备堡垒访问与遥测网关角色的主机，但底层仍是两个独立进程：

```text
堡垒机 Host
├── argus-connector
│   ├── 控制、命令和远程会话隧道
│   └── Collector 安装、修复与配置下发
└── argus-otelcol
    ├── 采集堡垒机本机
    ├── 可选接收成员主机 OTLP
    └── 持久队列并推送 Argus
```

OTLP 数据不得经过 Connector 控制通道或人工远程会话流。

## 2. 核心对象和生命周期

### 2.1 Bastion Scope

Bastion Scope 表示一个稳定的堡垒机管辖范围：

```text
BastionScope
├── enterprise_id
├── name
├── connector_host_id
├── active_connector_id
├── member_host_ids
├── default_telemetry_route
└── status
```

Bastion Scope 与 Connector 实例分开保存。用户归类标签保存在根 Host 和成员 Host，不在 Scope 上再维护一套 `tags`。Connector 可能重装、轮换证书或被替换；这些操作不能改变 Scope ID，也不能让成员主机丢失分组和授权关系。第一版一个 Scope 只有一个活动 Connector，Schema 预留备用 Connector/Connector Pool，但界面不能在调度能力未实现时暗示已经具备自动高可用。

一个通过堡垒机接入的主机第一版只能属于一个 Bastion Scope。资源标签或 DataAuthorizationGrant 不能复用为 Bastion Scope；标签表达归类，显式授权表达可见资源，Bastion Scope 表达网络接入路径。一个 Bastion Scope 可以为同一企业内不同标签的目标 Host 提供路由，但不会把堡垒机 Host 的 DataAuthorizationGrant 传播给成员 Host。

### 2.2 主机连接模式

第一版主机连接模式固定为：

| 模式 | 创建方式 | 执行路径 | 是否属于 Bastion Scope |
| --- | --- | --- | --- |
| `connector_local` | 手动命令或平台 SSH 代安装 Connector 并完成注册 | Connector 管理本机 | 是，且为根堡垒机 |
| `via_bastion` | 填写内网地址、Credential 并选择堡垒机 | Argus → Connector → SSH/WinRM | 是，且为成员主机 |
| `direct_ssh`/`direct_winrm` | 填写目标地址、Credential，不选择堡垒机 | Argus Direct Executor → 目标主机 | 否 |
| `self_enrolled` | 平台生成一次性安装命令，用户在目标机器执行 | 无入站执行路径；bootstrap 自装并激活 | 否 |

“经堡垒机”和“直连”是显式接入路径，不根据 IP 字符串自动推导。服务端必须解析 DNS/IP、校验实际目的地址；Direct Executor 可以连接其部署网络可达的公网或私网目标，但必须拒绝环回、链路本地、云元数据和配置的禁用网段。

产品界面中的五种普通主机模式不是五个新的 `connection_mode`。双向可达与只进不出都使用 Direct 模式，通过 Telemetry Route 的 `transport=direct|executor_tunnel` 区分遥测物理路径；标准堡垒机成员与受限端口成员都使用 `via_bastion`，通过 `transport=direct|bastion_tunnel` 区分成员到 Gateway 的路径；只出不进使用 `self_enrolled`。

### 2.3 Remote Access Session

RemoteAccessSession 表示一次人工远程登录：

```text
RemoteAccessSession
├── enterprise_id / user_id / host_id
├── connection_mode / connector_id / bastion_scope_id
├── managed_account_id / credential_ref
├── remote_access_grant_id / access_lease_id
├── protocol / granted_actions / authorization_version
├── reason / approval_ref / policy_snapshot
├── ticket_hash / ticket_expires_at
├── started_at / last_activity_at / ended_at
├── recording_ref
└── status
```

状态至少包括：

```text
Requested
→ AwaitingApproval（按策略）
→ Authorized
→ Connecting
→ Active
→ Terminated / Failed / Expired / ConnectionLost
```

Remote Access Session 是人工作业通道，不是 MCP Tool Commit。AI、交互卡片和 OpenSandbox 不得创建或消费交互式会话票据，也不能通过浏览器终端间接执行生产操作。

## 3. 堡垒机安装、注册和界面创建

具有 `bastion_scope.create` 的企业用户在“主机”页面点击“添加堡垒机”，第一步选择安装模式，第二步再填写对应信息。三种正式模式为：

| 模式 | 网络前件 | 第二步信息 | 确认方式 | 用户结果 |
| --- | --- | --- | --- | --- |
| A 手动命令安装 | 堡垒机可出站；不要求 Argus 可 SSH | 名称、环境、标签 | 冻结 Scope/Host/Connector 计划 | 一次性安装命令 |
| B 平台 SSH 代安装 | Argus 可 SSH，堡垒机可出站 | A 的字段 + 地址、端口、账号、Credential | ConnectionTest + 安装 Preview | 安装 operation 进度与完成状态 |
| C 平台代安装 + 控制隧道 | Argus 可 SSH，堡垒机无出站 | 同 B | ConnectionTest + 安装/控制隧道 Preview | 安装、控制隧道和 Connector 回连进度 |

堡垒机既无出站、Argus 也无法 SSH 时没有可执行路径，界面不保留一个看似可选的禁用模式。模式 C 由 Direct Executor 在同一 SSH 连接上建立 `127.0.0.1:8443 → enrollment Web/API` 与 `127.0.0.1:9443 → Connector Gateway` remote forward；它只解决安装、enrollment 和 Connector 控制长连接，堡垒机及成员 Collector 的遥测仍独立选择 Telemetry Route 与 transport。

确认后才预分配 Bastion Scope、根 Host 和 Connector 身份。模式 A 创建 registration token，并把安装命令作为 Execution 加密一次性结果；模式 B/C 创建 `connector_install_operations` 冻结计划，由 Direct Executor 安装、启动并等待 enrollment/online。B/C 的内部 enrollment token 不返回浏览器，也不显示为用户命令。关闭安装进度界面不取消 operation，用户可以从待注册 Scope 或任务记录恢复。

Preview 必须同时冻结并校验活动 Connector 发行版，而不能只校验表单。模式 A 要求发行清单、安装脚本及 Linux amd64/arm64 两个签名对象都已存在；B/C 要求 ConnectionTest 探测到的架构对象已存在。缺少目录记录或对象返回 `CONNECTOR_ARTIFACT_UNAVAILABLE`，在创建确认卡片前 fail closed；Confirm/Commit 只能使用 Preview 冻结的 release ID 与 manifest hash，不得重新选择“当前活动版本”。确认卡片本身属于危险操作语义，正常顺序仍是“Preview 卡片 → 用户确认 → Execution 加密一次性命令 → 原发起人领取”，不能在确认前泄露 token 或命令。

```mermaid
sequenceDiagram
    participant U as 用户
    participant S as Argus 服务端
    participant E as Direct Executor
    participant C as 目标 Connector

    U->>S: 选择 A/B/C 并填写第二步信息
    opt B/C
        S->>E: 执行并冻结 SSH ConnectionTest
        E-->>S: 地址、Host Key、Credential 版本与诊断
    end
    S-->>U: Scope/Host/安装或隧道 Preview
    U->>S: Confirm Pending Action
    alt A 手动命令
        S->>S: 创建 pending Scope、令牌和加密一次性结果
        U->>S: 原子领取安装命令
        U->>C: 在堡垒机执行命令
    else B/C 平台代安装
        S->>E: 创建并认领 connector_install operation
        E->>C: SSH 安装并启动 Connector
        opt C 控制隧道
            E->>C: 维持 8443/9443 remote forward
        end
    end
    C->>S: enrollment、CSR 与设备信息
    S->>S: 原子消费令牌并激活 Scope/root Host
    S-->>C: 签发 Connector 身份与证书
    C->>S: mTLS 控制长连接（A/B 直连，C 经隧道）
```

一次性注册令牌必须绑定：

- `enterprise_id`。
- 根堡垒机 Host 的预分配 ID、初始 `labels` 和 DataAuthorizationGrant 校验摘要。
- `bastion_scope_id`。
- 创建用户和安装策略。
- 允许的 Connector 名称、平台或标签限制。
- 允许注册次数和有效期。

安装命令不能包含永久访问令牌。Connector 必须在本地生成私钥和 CSR；私钥不得出现在安装命令或服务端。服务端证书绑定企业、Connector、Host、设备公钥、序列号和允许能力，并支持按设备吊销与短窗口重叠轮换。

### 3.1 编辑、待注册轮换与 Connector 替换

“编辑堡垒机”编辑的是稳定的 Bastion Scope，元数据字段为名称、环境和标签。根堡垒机 Host 的注册身份由 enrollment 事实产生；模式 B/C 的安装地址、账号和 Credential 属于冻结 operation 输入，不作为 Scope 元数据长期回填。Scope 元数据保存后，服务端同步更新根 Host 与当前 Connector 的展示信息，但不得改变 Scope ID、Host ID 或成员主机关系；标签变化不改变 DataAuthorizationGrant，也不递增 AuthorizationVersion。

安装相关操作按资源状态分开：

- Scope 尚无 active Connector 时，命令丢失或过期调用 `bastion.enrollment.rotate`。该动作只撤销旧未消费 token、创建新 token 和加密一次性结果，不增加 fencing generation，也不伪装成 Connector replacement。
- Scope 已有 active Connector，需要迁移、隔离或更换机器时调用 `bastion.connector.replace`。该危险动作冻结当前 Connector/epoch，增加 fencing generation、吊销旧证书并创建替换 operation；旧机器恢复后不能重新成为活动 Connector。
- B/C 自动安装失败时按 `connector_install_operations` 语义查看诊断、重新测试或幂等重试；不能退化为向用户展示内部 enrollment token。
- 主动卸载继续使用 §3.2 的独立类型化命令，不能复用 enrollment rotate 或 replacement。

以上写动作都进入 PendingAction。领取已执行完成但尚未领取的一次性结果使用统一 claim 接口，不创建新 token。旧的“显示命令”入口不得调用 Connector replacement；不存在 active Connector 的 Scope 必须由服务端拒绝 replacement，而不是只靠前端隐藏按钮。

替换注册完成时保持 `bastion_scope_id` 与根堡垒机 `host_id` 不变，签发新的 Connector 身份和证书。命令轮换、替换与领取均记录独立审计事件；审计只保存 token/result ID、状态和摘要，不保存命令或 token 明文。

### 3.2 主动卸载与离线检测

在线堡垒机提供独立的“卸载堡垒机”操作。点击后由 PendingAction 冻结当前 `connector_id + bastion_scope_id + connection_epoch + fencing generation`，Confirm 后派发类型化 `connector_uninstall` 命令。Connector 必须先上报同时证明 `identity_removed = true` 和 `service_stopped = true` 的成功结果；Gateway 持久化 Connector/Scope 最终状态并 ACK 覆盖该结果序号后，客户端才删除本地身份并退出。不能先删除本地身份再尝试上报，否则服务端无法区分正常卸载与网络故障。

卸载期间 Scope 进入 `uninstalling`，阻止新命令、任务和 Remote Access Session；已有会话按策略排空或终止。服务端确认后 Connector 进入 `uninstalled`，Scope 进入 `uninstalled`，但保留 Scope、根 Host、成员主机、权限和监控配置。此时页面重新进入 A/B/C 安装模式选择，以便用手动命令或平台代安装恢复承载同一批成员主机的堡垒机。

Gateway 按心跳维护在线事实。推荐心跳周期 30 秒，连续 3 次未收到心跳进入 `suspected_offline`，持续 5 分钟仍未恢复才在控制面标记为 `offline`；阈值必须可配置。短暂抖动不能直接开放替换。离线状态发起重新安装属于 `bastion.connector.replace`，等同于管理员确认隔离旧实例，必须执行 fencing 并写入审计。

有效 Connector 重连并取得新的 `connection_epoch` 时，服务端必须在同一恢复路径把对应 Bastion Scope 恢复为 `active`，或把 KubernetesCluster 恢复为 `connected`。Redis Registry 丢失、Gateway Drain 或短暂断连不能让资源状态永久停留在 `suspected_offline`。

### 3.3 删除 Bastion Scope

“卸载”只移除当前 Connector 实例；“删除”才移除 Bastion Scope。删除按钮只在 Connector 已离线或 Scope 已卸载时显示，在线和卸载中状态不得显示。服务端不能只依赖前端门禁，删除时必须再次校验：

- `member_host_ids` 为空，所有成员主机已迁移到其他 Scope 或改为其他连接模式。
- 不存在活动 Remote Access Session、任务或待执行命令。
- 当前 Scope 已 `uninstalled`，或 Connector 已被 fencing 且 Scope 为 `offline`；不能仍有活动 Connector 或有效证书。

如果仍有成员主机，点击删除后弹窗必须列出成员数量并提示“请先将全部主机移出该堡垒机”，确认按钮保持禁用。满足门禁后再次确认才逻辑删除 Scope 与根堡垒机 Host，并撤销历史 Connector 的活动凭据和未消费命令；根 Host 保留 `deleted_at` 墓碑，审计记录和历史引用继续保留，不做级联物理删除。

Scope 与根堡垒机 Host 必须作为一个生命周期单元维护。创建 Scope 的事务必须同时创建 `connection_mode = connector_local` 的根 Host，并在提交前把其 ID 写入 `bastion_scopes.connector_host_id`；即使 Connector 尚未注册，也不能把该关联推迟到 enrollment。删除顺序固定为先逻辑删除根 Host，再逻辑删除 Scope，数据库删除条件还要阻止存在有效根 Host 的 Scope 被单独删除。升级时需要修复历史孤儿根 Host并回填缺失关联。

名称唯一性继续由数据库的条件唯一索引保证，Scope 与 Host 的索引都只约束未删除记录；软删除墓碑不参与冲突，因此删除完成后允许复用原名称。不能通过移除唯一索引规避生命周期错误，否则并发创建会产生同名有效资源。创建预览必须同时检查两个有效名称空间；若名称仍被有效资源占用，稳定返回 HTTP `409` / `RESOURCE_NAME_CONFLICT`，该错误属于永久业务冲突，Worker 不得重试。只有根 Host 与 Scope 都完成逻辑删除后，重建同名资源才会成功。

### 3.4 一次性令牌互斥与幂等

注册令牌生命周期固定为 `active → consumed`，或从 `active` 进入 `revoked / expired`。用途区分首次注册 `initial_registration`、待注册轮换 `enrollment_rotation` 与已有实例替换 `connector_replacement`；前两者要求不存在 active Connector，后者要求存在被 fencing 的 active Connector。服务端必须使用数据库条件更新或行锁原子消费令牌，消费条件至少包含 `status = active`、`remaining_uses = 1` 和 `expires_at > now()`，不能用进程内锁代替持久化互斥。

注册 token 的有效期与 Execution 一次性结果的领取期是两个概念。安装命令在服务端生成后立即写入 AES-GCM 加密的 `execution_one_time_results`，公开 Execution 只投影 `one_time_result_available`；原发起人通过统一幂等接口原子领取。结果过期或已经用新 Idempotency-Key 领取时不能恢复明文，用户需要显式发起 enrollment rotation；claim 本身不得生成新 token。

多台机器执行同一安装命令时遵循以下规则：

- 第一台通过校验并原子消费令牌的机器获得 Connector 身份。
- 不同设备再次提交同一令牌时返回 HTTP `409`、错误码 `TOKEN_ALREADY_CONSUMED`，并说明该命令已由其他机器使用；待注册 Scope 需要发起 enrollment rotation，已有 active Connector 则必须进入显式 replacement，不得静默成功或自动替换当前 Connector。
- 同一设备以相同设备指纹和注册请求重试时返回首次注册结果，记为 `idempotent_retry`，不得创建第二个 Connector 或重复切换。
- 被新命令替代的旧命令返回 `TOKEN_REVOKED`；过期命令返回 `TOKEN_EXPIRED`；未知命令返回 `TOKEN_MISSING`。
- 日志和审计只保存令牌 ID、用途、状态、消费时间与设备指纹摘要，不记录明文令牌。

```mermaid
stateDiagram-v2
    [*] --> Active: initial / enrollment_rotation / connector_replacement
    Active --> Consumed: 首台设备原子注册成功
    Active --> Revoked: 轮换未消费 token 或执行 replacement
    Active --> Expired: 到达 expires_at
    Consumed --> Consumed: 同设备幂等重试
    Consumed --> Conflict: 其他设备再次使用
    Revoked --> Rejected
    Expired --> Rejected
```

## 4. Connector 通道和多副本路由

Connector 主动连接服务端，以穿越 NAT 和防火墙：

```text
Connector
→ TLS/mTLS
→ argus-connector-gateway
→ Session Registry
→ Command / Tunnel / Remote Session Dispatcher
```

协议必须划分独立逻辑流并分别限流：

- Control Channel：心跳、能力协商、命令控制和状态。
- Command Data：命令输出、日志和结果。
- Artifact Tunnel：批准 Artifact 的分块传输。
- Remote Session Stream：经短期票据授权的人工终端输入输出。

Collector OTLP 不属于以上任何流。远程会话流量压力不得阻塞心跳和控制命令；Artifact 和终端流量分别设置带宽、并发和优先级。

每次长连接建立时 Gateway 分配单调递增的 `connection_epoch`，Session Registry 保存：

```text
connector_id
enterprise_id
bastion_scope_id
gateway_instance_id
connection_epoch
capabilities
connected_at / last_heartbeat_at
draining
```

PostgreSQL 保存最后连接事实、命令和 RemoteAccessSession，Redis 保存带 TTL 的在线 Registry 和跨 Gateway Pub/Sub 派发提示。命令总是先进入 PostgreSQL 权威队列；Pub/Sub 丢失时由 Gateway/Connector 的 PostgreSQL 轮询兜底。Redis 清空后 Connector 心跳重建 Registry，不能丢失命令或会话审计事实。

### 4.1 Gateway 多副本路由

```mermaid
sequenceDiagram
    participant W as argus-worker / remote client gateway
    participant G2 as Gateway B
    participant R as Redis Session Registry
    participant G1 as Gateway A
    participant C as Connector

    C->>G1: mTLS 长连接 epoch=42
    G1->>R: connector -> Gateway A, epoch=42
    W->>G2: command_id 或 remote_session_id
    G2->>R: 查询当前连接所有者
    R-->>G2: Gateway A, epoch=42
    G2->>G1: 内部 mTLS 转发 + epoch
    G1->>G1: 校验仍持有 epoch=42
    G1->>C: 下发命令或打开会话逻辑流
```

内部转发必须认证服务身份并校验企业、Bastion Scope、Connector、`connection_epoch` 和会话票据。旧 Gateway 或旧连接不能向新连接写入数据。Gateway Drain 时停止接收新远程会话和新命令，允许现有会话在限定窗口结束或生成明确的中断事件，再通知 Connector 重连。

### 4.2 Connector Command 状态机

```mermaid
stateDiagram-v2
    [*] --> Queued
    Queued --> Expired: 派发前超过 expires_at
    Queued --> Dispatched
    Dispatched --> Acknowledged
    Acknowledged --> Running
    Running --> Succeeded
    Running --> Failed
    Running --> TimedOut
    Dispatched --> DeliveryUnknown: Gateway 或 Connector 断连
    Running --> ResultUnknown: 执行中断连
    DeliveryUnknown --> Queued: 证明未接收且允许重试
    ResultUnknown --> Succeeded: 重连上报成功
    ResultUnknown --> Failed: 对账确认失败
```

Command 保存 command_id、execution_id、企业、Connector、连接代次、目标资源、计划哈希、幂等键、有效期、Fence Token 和结果摘要。`DeliveryUnknown`/`ResultUnknown` 不能当作普通失败自动重试，必须先向 Connector 本地幂等日志或目标系统对账。卸载命令的普通 reconcile 不携带能够证明本地身份和服务均已清理的类型化结果，因此必须保持 `ResultUnknown`，不能仅凭“曾执行过”提升为成功。

后台 sweeper 收敛超过有效期的命令：未派发命令进入 `Expired`，已派发/确认/运行命令进入 `TimedOut`，同时撤销关联 Credential Lease 并让 ConnectionTest 收敛为 `expired/failed`。Gateway 的内存 inflight 槽位必须按同一过期时间释放，不能让超时命令耗尽单连接并发额度。

## 5. Direct Executor

没有选择堡垒机的主机使用受控 Direct Executor。第一版 Direct Executor 作为 `argus-worker` 的受隔离运行角色或独立 Worker Pool，不增加新的业务程序；它不能由普通 HTTP Handler 直接调用任意 Socket。

Direct Executor 必须具备：

- 固定、可展示给用户加入防火墙白名单的出口 IP。
- 仅允许 SSH/WinRM 等版本化协议与端口白名单。
- DNS 解析前后校验，允许 Executor 部署网络可达的私网地址，禁止环回、链路本地、广播、云元数据和配置的禁用网段。
- 防 DNS Rebinding；连接时再次校验最终 IP。
- SSH Host Key/证书校验和首次信任的显式预览。
- 企业、用户、目标、Credential、用途、时长和并发绑定。
- 与 Agent/模型调用隔离的网络策略和 Worker Queue。
- 所有连接、命令和文件操作审计。

Direct Executor 只管理其部署网络实际可达的目标，不能宣称 SaaS Worker 可以自动穿透客户内网；需要访问特定私网时，部署方必须把 Executor 放在具备受控路由的位置，或改用 Bastion Scope。若直连主机后来安装 Connector 并注册为堡垒机，它将创建新的 Bastion Scope；原 Host 的身份迁移、Credential 和历史审计必须使用显式预览。

Server 与 Direct Executor 之间使用独立内部 CA 的 mTLS gRPC。Server 只发送绑定 `connection_test_id` 的低延迟派发提示，Executor 从 PostgreSQL 原子 Claim 已冻结计划；PostgreSQL 扫描负责 RPC 丢失和 Pod 重启恢复，因此 Handler 不能把 RPC 当作唯一队列或传入任意目标参数。Direct Kubernetes Reader 同样固定首次解析的目标 IP，每次请求前后重新校验 DNS，并拒绝 HTTP Redirect 和跳过 TLS 校验。

## 6. 添加和管理主机

### 6.0 主机与堡垒机统一新增向导

主机和堡垒机共用 `select_mode → details → verify|confirm_command → installing|command_result|completed` 领域状态机。步骤条只覆盖用户可以返回编辑的输入和验证阶段；安装进度、一次性命令和完成页是提交后的结果状态，不伪装成可编辑步骤。

- 第一步只显示模式卡、适用网络条件、需要准备和平台行为，不渲染名称、地址、Credential、标签或命令表单。
- 第二步只显示已选模式摘要与该模式字段；“更改模式”返回第一步，不在表单旁保留完整模式列表。
- 第三步中，SSH/WinRM 路径执行 ConnectionTest 并展示冻结事实与 Preview；`self_enrolled` 和堡垒机 A 展示冻结计划并明确“确认并生成命令”。
- B/C 和其他自动安装路径提交后进入可恢复 operation 进度；A/`self_enrolled` 进入一次性结果领取与展示。
- 步骤、可见 DOM、焦点、Enter 提交和错误定位由同一状态驱动。表单必须使用真实 `<form>`，不得使用静态步骤条、隐藏空表单或场景与表单同屏的伪步骤。

普通主机五种产品模式的字段和确认分支固定为：

| 产品模式 | 第二步字段 | 第三步与结果 |
| --- | --- | --- |
| 双向可达 | 名称、地址、协议、端口、系统、环境、账号、Credential、标签 | ConnectionTest + Preview → 创建/安装状态 |
| 只进不出 | 同双向可达 | ConnectionTest + `executor_tunnel` 前件 Preview → 安装/隧道状态 |
| 只出不进/自助安装 | 名称、架构、环境、标签 | 冻结计划 + 确认 → 一次性安装命令 |
| 标准堡垒机成员 | 所属 Bastion Scope + 地址、协议、端口、系统、环境、账号、Credential、标签 | Connector 路径测试 + Preview → 创建/安装状态 |
| 受限端口堡垒机成员 | 同标准成员 | Connector 路径与 `bastion_tunnel` 前件测试 → 安装/隧道状态 |

从第二步返回第一步时保留名称、环境、标签等公共字段。地址、账号、Credential、Scope 和架构只在兼容模式间保留；切换到不兼容模式时清除，清除用户已经选择的 Credential 前需要明确提示。当前版本不持久化未提交草稿，也不迁移旧 localStorage 向导数据。

### 6.1 通过堡垒机添加内网主机

用户填写：

```text
connection_mode = via_bastion
bastion_scope_id / connector_id
private_address / hostname
port
protocol = ssh | winrm
credential_ref
labels
```

流程固定为：

```text
第一步选择「标准堡垒机成员」或「受限端口堡垒机成员」
→ 第二步选择 Bastion Scope 并填写内网地址和 Secret 表单
→ Connector 测试 DNS、路由、Host Key 和认证
→ 第三步展示资源、连接路径与可选隧道预览
→ 用户确认
→ 创建 Host 并加入 Bastion Scope
```

成员主机在主机页面显示在所属堡垒机框内，其连接路径明确为“Argus → 堡垒机 → 内网地址”。移动到另一个 Scope 会改变网络路径和凭证使用边界，必须 Preview/Commit，不能只修改前端分组。

### 6.1a 添加自助注册主机（self_enrolled，PlanV4）

“只出不进”主机（Argus 不可直达、目标可出站访问 Argus）的固定流程：

```text
第一步选择「只出不进/自助安装」
→ 第二步填写名称/架构/环境/标签（当前仅支持 Linux，免 ConnectionTest）
→ Preview/Confirm 创建 onboarding 主机
→ Execution 产生加密一次性结果，原发起人原子领取并在当前结果态展示
→ 用户在目标机器执行结构化安全引导命令（内嵌公共 Bundle，`curl --cacert` 下载并固定 SHA-256 后才执行安装器）
→ 首次 bootstrap 激活主机、回填自报地址并开始 direct_argus 推送
→ 命令丢失/过期 = `host.enrollment.rotate`；已注册主机卸载 = 独立 `host.uninstall.command`
```

自助安装 Preview 还必须校验目标架构的 Collector 对象和同一存储桶根下的 `install/host.sh`。例如产物位于 `/argus-collector-artifacts/argus-otelcol/...` 时，脚本固定为 `/argus-collector-artifacts/install/host.sh`，不能错误退回域名根路径。目录元数据存在但对象缺失时返回 `COLLECTOR_ARTIFACT_UNAVAILABLE`，不生成一个必然下载失败的确认卡片或安装命令。

远程终端与远程运维对该模式 fail closed；在线状态以 Collector enrollment/心跳形成的 `last_seen` 投影为准。轮换只撤销旧未消费 enrollment token，不执行 Connector fencing；卸载命令使用独立的短期一次性授权。旧 `POST /enterprise/hosts/{id}/install-command` 同时生成安装/卸载明文的开发期接口删除，不保留兼容端点。

### 6.2 添加直连独立主机

用户填写：

```text
connection_mode = direct_ssh | direct_winrm
address / hostname
port
credential_ref
labels
```

流程固定为：

```text
第一步选择「双向可达」或「只进不出」
→ 第二步填写目标与 Credential
→ 解析并校验目标地址
→ 显示 Argus 固定出口 IP
→ Direct Executor 测试 Host Key、端口和认证
→ 第三步展示资源、连接预览与可选 `executor_tunnel` 前件
→ 用户确认
→ 创建独立 Host
```

服务端允许直连 RFC1918、IPv6 ULA 和内部 DNS 目标，但不得接受环回、链路本地、云元数据、配置的禁用网段，或 DNS 重绑定后超出冻结地址集合的目标。

ConnectionTest 是独立的异步只读资源。服务端创建时冻结目标地址、Credential/Secret 版本、Connector epoch、DNS/IP、Host Key 或 TLS 摘要和过期时间；Host/Kubernetes Preview 只能引用与当前输入完全匹配且未过期的成功测试。创建后的地址、端口、连接模式、Bastion Scope 或 API Server 等网络路径变化同样必须先创建新测试，纯名称、描述等元数据变化才可复用原路径。Confirm 再次校验冻结计划、操作者、DataAuthorizationGrant、资源版本、AuthorizationVersion 和网络身份，不能用一个目标的成功结果提交另一个目标。Secret 轮换、Connector 换代或授权变化会立即使相关测试和 Credential Lease 失效。

### 6.3 Host 与 Credential 分离

```text
Host
├── enterprise_id
├── address / hostname / port
├── platform
├── connection_mode
├── bastion_scope_id（via_bastion 时必填）
├── connector_id（connector_local/via_bastion 时使用）
├── labels: Record<string, string>
├── labels_version / resource_version
└── connection_status
```

Host 不直接保存用户名、密码或私钥。目标登录账号保存为 `ManagedAccount`，并通过 `credential_ref` 引用 Secret：

```text
ManagedAccount
├── enterprise_id / host_id
├── username / privilege_level
├── credential_ref
├── allowed_protocols
└── status
```

同一个 Host 可以配置多个 Managed Account。账号管理、凭证代用和 Secret 原值读取必须分别授权；普通远程访问用户只获得 Credential Broker 的短期代用包，不能读取原值。聊天中不得鼓励用户发送密码；疑似凭证在进入模型前切换到受控 Secret 表单。若凭证已发送给外部模型，系统不能声称可靠撤回，应阻止继续回显、记录安全事件并建议轮换。

### 6.4 Remote Access Grant

拥有 `remote_access.request` 或 `remote_access.session.create` 只表示具备使用远程访问功能的能力，不自动获得企业内全部 Host 和账号。创建会话还必须命中 DataAuthorizationGrant 和有效的 RemoteAccessGrant：

```text
RemoteAccessGrant
├── enterprise_id
├── subject: user | department
├── explicit_host_ids[]
├── managed_account_ids
├── protocols
├── actions
├── valid_from / valid_until
├── policy_id / status
└── version
```

Grant 至少包含显式 Host ID 或经过校验的 Host 标签过滤条件；空范围不代表企业内全部主机。标签变化不会改变 Grant 命中集合，也不会递增 AuthorizationVersion。

第一版只必须支持 `connect`；`clipboard`、`upload`、`download`、`session_share` 和 `port_forward` 是独立动作并默认关闭。需要临时访问时，Access Request 审批后生成短期 AccessLease 或激活受限 Grant；批准访问不能自动批准某个 AI/MCP 生产变更。

## 7. 人工远程登录和堡垒隧道

成员主机的 SSH Web Terminal 使用：

```text
Browser
→ argus-server 校验企业、DataAuthorizationGrant、Host、ManagedAccount、Grant、MFA/JIT/审批和授权版本
→ 签发短期票据与 Credential Package
→ argus-connector-gateway Remote Access 入口
→ 当前 Connector 长连接的 Remote Session Stream
→ Connector 使用短期 Credential Package 发起 SSH
→ 内网主机
```

直连独立主机使用：

```text
Browser
→ argus-server 校验企业、DataAuthorizationGrant、Host、ManagedAccount、Grant、MFA/JIT/审批和授权版本
→ 签发短期票据与 Credential Package
→ Remote Access 入口
→ 受控 Direct Executor
→ 目标主机
```

所有已经完成连接测试并处于可管理状态的 Host 都显示统一的“打开命令行”入口，但底层能力随连接模式变化：

| Host 路径                              | 命令行实现                                             | 第一版界面语义                                   |
| -------------------------------------- | ------------------------------------------------------ | ------------------------------------------------ |
| `connector_local`                      | Connector 在堡垒机本机创建受限 PTY/ConPTY              | 本机终端                                         |
| `via_bastion` + SSH                    | Connector 使用短期 Credential Package 建立远端 PTY     | SSH 终端                                         |
| `direct_ssh`                           | Direct Executor 建立远端 PTY                           | SSH 终端                                         |
| `via_bastion` + WinRM / `direct_winrm` | Connector/Direct Executor 通过 HTTPS WinRS 建立持久 Shell | PowerShell 行模式，不宣称具备完整 PTY、PSRP 或桌面语义 |

第一版提供 SSH Web Terminal 和 HTTPS WinRS PowerShell 行模式；RDP、SFTP 和通用端口转发延后。会话创建前展示目标、完整路径、登录身份、协议能力、最长时长、录像、剪贴板、文件传输和审批策略。默认规则：

- 票据短期、一次性、绑定浏览器 Session、用户、企业、Host、ManagedAccount、协议、动作、DataAuthorizationGrant 摘要、AuthorizationVersion 和 RemoteAccessSession。
- 平台超级管理员无权进入企业远程会话。
- AI、Card 和 OpenSandbox 不获得票据。
- 录像按 asciicast v2 NDJSON 保存 `i/o/r/m` 事件，AES-256-GCM 密文分片写入 Artifact Store，PostgreSQL 保存索引和 SHA-256 Hash Chain；Gateway Pod 不保存唯一录像。
- M6 Evaluation 遇到 `step_up_mfa` obligation 时 fail closed；M8 本地加固通过 AuthenticationAssuranceService 提供 TOTP Step-up，Production 仍需独立环境验证。
- 剪贴板和文件传输默认关闭，开启时分别授权和审计。
- 管理员可以终止活动会话，但不能静默接管用户身份。
- 用户禁用、RoleBinding/DataAuthorizationGrant/RemoteAccessGrant/Policy 撤销、显式授权关系变化或企业停用时，未使用票据立即失效，等待连接的会话被拒绝，活动会话立即终止或进入有上限的安全结束窗口并审计。

交互式 Shell 允许人工执行改变目标状态的命令，因此不能声称每条命令都走 Tool Preview/Commit。受控 Tool 写操作使用两阶段操作；远程会话则使用会话级 JIT 授权、录像和审计。后续 RDP、SFTP、端口转发必须分别形成协议和安全 ADR。

第一版至少记录结构化命令和完整会话录像；后续 CommandPolicy 可以按用户/部门、明确 Host、ManagedAccount、命令组和优先级执行允许、拒绝、复核或告警。命令匹配不能作为唯一安全边界，高敏主机仍应使用低权限 Managed Account、MFA/JIT、短会话和最小文件能力。

### 7.1 人工命令行与后台执行任务的边界

Collector 安装、升级、配置收敛、Profile 开关和 Telemetry Route 变更会使用与人工命令行相同的底层连接基础设施，但不能使用同一个逻辑通道：

```text
人工命令行
→ RemoteAccessSession
→ Remote Access Gateway
→ Connector / Direct Executor 连接适配器

后台执行任务
→ Preview / Confirm
→ Execution / ConnectorCommand
→ Connector / Direct Executor 连接适配器
```

二者可以复用目标解析、SSH/WinRM 客户端、Host Key 校验、Credential Broker 和网络路径，但必须分离：

- 人工命令行使用用户绑定的短期会话票据、交互输入输出、会话录像和会话级授权。
- 后台命令使用服务端生成的不可变执行计划、幂等键、步骤状态、超时、重试/对账和回滚状态。
- Collector 安装、配置和验证只能通过版本化模板执行，不能把任意 Shell 字符串包装成后台任务。
- 前端必须明确标识当前处于“人工命令行”还是“平台后台任务”，不能在后台向已打开的人工终端写入命令。
- 两类操作分别计入 RemoteAccessSession 与 Execution/ConnectorCommand 审计，支持按用户、目标、会话或任务独立检索。

## 8. 主机管理界面

新增主机与堡垒机使用 §6.0 的统一状态机和共享 `@argus/ui` Wizard/Dialog 原语，业务模式目录、字段、验证和结果适配器留在 Enterprise hosts 领域。可用卡片不重复显示“已支持”；只对推荐、不可用或规划中状态显示有决策意义的徽章。协议枚举、route kind、transport 和内部端口放入可展开技术详情，不作为用户完成模式选择的前置知识。

待注册 Scope/Host 卡片按领域状态提供唯一明确的下一步：

| 状态 | 主操作 |
| --- | --- |
| 一次性结果可领取 | 领取安装命令 |
| 已领取、等待注册 | 重新生成安装命令 |
| enrollment rotation 待审批 | 查看审批 |
| 审批通过、结果可领取 | 领取安装命令 |
| token/结果过期 | 重新生成安装命令 |
| B/C 自动安装中 | 查看安装进度 |
| 自动安装失败 | 查看原因并重试或重新测试 |
| 已注册 | 打开详情 |

卡片不能写“执行下面的命令”却不显示命令，也不能暴露原始 i18n key、Tool 名、内部 Scope/Host ID 或未整理 diff。Connector replacement 只从已有 Connector 的维护入口发起，并明确展示 fencing 影响。

主机页面采用“Bastion Scope 分组卡片 + 独立主机”结构：

```text
主机                                              [添加堡垒机] [添加普通主机]

┌─ 北京生产网关 ───────────────────────────────────────────┐
│ gw-bj-01 · Connector 在线 · 成员 12 · 活动会话 2        │
│ Collector Gateway 未启用                                │
│ [gw-bj-01] [web-01] [web-02] [db-01] ...   [添加内网主机] │
└─────────────────────────────────────────────────────────┘

独立主机
[public-web-01] [public-db-01] [dev-server]
```

堡垒机不作为成员主机卡片重复嵌套在 Bastion Scope 内。Scope 外框标题栏直接显示可进入 Host 详情的堡垒机名称、环境、堡垒机身份、接入服务状态、OTLP 收集器状态，以及连接测试和编辑快捷操作；堡垒机不提供独立删除按钮。标题栏右侧显示活动远程会话、成员数和成员 OTLP 收集器汇总。成员卡片显示连接路径、远程登录、OTLP 收集器状态和 Telemetry Route；独立主机显示 Direct Executor 连通性和固定出口要求。

主机规模较大时默认折叠 Scope，只显示汇总和前若干成员；展开后使用紧凑卡片或服务端分页表格。卡片视觉分组不能替代服务端 Bastion Scope、权限和路由检查。

主机详情的“组件与采集”区域承载 Connector 和 OTLP 收集器：

```text
Connector                未安装 / 在线 / 离线 / 需升级
OTLP 收集器            未安装 / 监控中 / 配置待下发 / Gateway
```

Connector 和 Collector 不作为企业左侧一级菜单。

主机详情建议固定为：

```text
概览 | 终端与会话 | 组件与采集 | 任务与审计
```

“终端与会话”提供打开命令行、活动会话、历史录像和强制终止；“组件与采集”中的 Connector 技术信息仅在堡垒机显示，OTLP 收集器则在所有支持的 Host 上显示安装按钮。主机和堡垒机列表中的“未安装”状态直接打开安装抽屉，“监控中”直接进入对应 Host 的“组件与采集”中的收集器区域，而不是跳到全局收集器页面。堡垒机自身的 OTLP 收集器可以直接上报 Argus，也可以在路由测试通过后选择同企业内另一个已激活堡垒机作为代理，但不能选择自身：

```text
Collector 概览 | 采集能力 | 数据推送 | 配置版本 | 运行状态
```

## 9. Kubernetes 管理

支持：

- kubeconfig + Bastion Scope Connector 访问私有 API Server。
- kubeconfig 由受控 Direct Executor 访问其部署网络可达且经校验的 API Server。
- 集群内 Connector 使用 ServiceAccount。
- 后续扩展托管集群身份和临时凭证。

KubernetesCluster 保存：

```text
enterprise_id
name
api_server
connection_mode
bastion_scope_id / connector_id（私网模式）
credential_ref
default_namespace（可选）
labels: Record<string, string>
labels_version / resource_version
connection_status
```

kubeconfig 作为 Secret 处理，模型只获得 context、cluster name 和 server 地址等非敏感摘要。集群接入先测试 API、集群身份、版本和权限，再配置授权 Namespace Scope 并确认写入。

`in_cluster` 确认会产生一次性 Connector 安装命令。Execution 公开对象只返回结果可领取状态，原发起人通过统一 claim 接口原子领取；领取后的明文只在当前结果抽屉中展示并允许复制，关闭后立即清除，不能写入 URL、localStorage、Query Cache、日志或普通 KubernetesCluster DTO。

第一版管理能力覆盖集群 CRUD、连接测试、Namespace/Node/Pod/Deployment/StatefulSet/DaemonSet/Service 查询和 Pod 日志。变更操作继续使用两阶段确认。

Kubernetes Collector 在集群详情中安装和配置，使用 DaemonSet + Gateway Deployment；集群内 Gateway 不要求部署在某台堡垒机主机上，Connector 只负责访问 Kubernetes API 和下发安装。

如果 Kubernetes Node 已经作为 Host 接入并安装 Host Collector，不能仅凭 IP 或主机名直接禁止 DaemonSet，也不能默认同时打开两套相同采集能力。

安装前先建立 Kubernetes Node 与 Host 的可信物理绑定，再按 Collection Claim 检测具体 Profile 是否重叠。两个 Collector 进程可以共存，但同一物理节点上的同一采集责任默认只能有一个主所有者；详细规则见 [OpenTelemetry 接入与监控数据链路](./09-opentelemetry-observability.md)。

## 10. Secret 使用原则

- 加密存储，业务表只保存引用。
- 解密发生在尽量靠近实际连接执行者的位置。
- MCP Tool、RemoteAccessSession 和 Direct Executor 只使用 `secret_ref`。
- Connector/Direct Executor 获取绑定执行或会话的短期使用包，而非无限期主密钥。
- 日志、Tool Result、会话 DOM、录像元数据和 Card DOM 不出现密钥。
- Secret 访问本身产生审计事件。

Credential Broker 根据 Execution 或 RemoteAccessSession、目标资源和有效期签发短期使用包。Connector/Direct Executor 只能在内存或操作系统保护的临时文件中使用，并在任务或会话结束后清理。Kubernetes 第一版由连接执行者使用短期 kubeconfig/Token 代执行 API 请求，不向 Worker、模型或浏览器返回完整 kubeconfig。

## 11. Collector 安装与 Artifact Tunnel

Collector 安装入口位于主机详情。执行路径继承 Host 的连接模式：

```text
connector_local  → 本机 Connector 安装本机 Collector
via_bastion      → 所属堡垒机 Connector 经 SSH/WinRM 安装成员 Collector
direct_ssh       → Direct Executor 经 SSH 安装 Collector
self_enrolled    → 用户执行冻结的一次性 bootstrap 命令安装 Collector
```

安装 Preview 必须展示完整执行路径、OS/架构/权限、下载或 Tunnel 模式、文件与服务变化、端口、防火墙、版本、Digest、Telemetry Route kind × transport、健康检查和回滚计划。`executor_tunnel`/`bastion_tunnel` 的执行顺序固定为建立隧道、安装/配置 Collector、经隧道验证健康与首条数据、最后把 route 转为 active；不能只做端口连通测试。

Connector Artifact Tunnel 只使用平台发布清单中的不可变 Artifact，支持分块、续传、SHA256/签名校验、带宽限制、临时文件清理和进度上报。Direct Executor 可以让目标从批准地址下载，或通过受控 Artifact 读取路径传输；两种方式都不能成为任意文件写入能力。

Collector 安装后，用户在主机详情进入“采集能力”和“数据推送”配置。Collector 组件通常编译进 Distribution，因此 UI 中的“监控插件”在领域层定义为 Collection Profile 开关和参数；如果当前 Distribution 不包含所需组件，必须先 Preview 升级。

Connector 只负责安装、控制和 Artifact；Collector 自身通过 OTLP 主动推送。堡垒机上普通 Collector 默认只采集本机，只有显式启用 Edge Gateway Profile、OTLP Listener、持久队列和 Leaf 认证后，才成为成员主机可选的遥测中间机。详细规则见 [OpenTelemetry 接入与监控数据链路](./09-opentelemetry-observability.md)。

## 12. 列表、过滤、Tool 与人工会话接口

主机和 Kubernetes 领域服务提供过滤、稳定排序、游标翻页和详情查询；卡片只展示和收集白名单条件，不能模拟跨页数据库过滤。

基础 Tool：

```text
bastion.scope.list(filter, sort, page)
bastion.scope.get(scope_id)
bastion.scope.create.preview / commit
bastion.enrollment.rotate.preview / commit
bastion.connector.replace.preview / commit

host.list(filter, sort, page)
host.get(host_id)
host.test_connection(host_id 或临时连接引用)
host.create.preview / commit
host.enrollment.rotate.preview / commit
host.uninstall.command.preview / commit

execution.one_time_result.claim(execution_id, idempotency_key)

remote_access.session.create
remote_access.session.terminate
remote_access.session.get
kubernetes.cluster.list(filter, sort, page)
kubernetes.cluster.get(cluster_id)
kubernetes.resource.list(cluster_id, resource_type, filter, sort, page)
kubernetes.resource.get(cluster_id, resource_type, namespace, name)
kubernetes.pod.logs(cluster_id, namespace, pod, container, options)
```

`execution.one_time_result.claim` 是读取已执行结果的幂等 OpenAPI 领域接口，不生成新 token，也不注册为模型可发现的 MCP Tool。上面列表中的 `remote_access.*` 同样是人工管理 UI/OpenAPI 领域接口；其他变更 Tool 继续遵守 Preview/Commit。是否需要远程会话审批由策略决定，但短期票据签发必须在最终重新授权后完成。开发期直接返回主机命令的旧接口和待注册 Scope 误用 replacement 的调用直接删除，不保留双接口或兼容客户端方法。

主机过滤覆盖名称/地址、在线状态、连接模式、Bastion Scope、Connector、平台、环境、团队、标签、Collector 和 Telemetry Route。服务端始终求用户过滤与授权 Scope 的交集。Cursor 签名绑定企业、资源类型、过滤哈希、稳定排序和有效期。

Kubernetes 资源过滤覆盖 Namespace、名称、Label Selector、允许的 Field Selector、Phase、Node、Owner 和最小重启次数。服务端先求授权 Namespace Scope 与请求过滤的交集，再访问 Kubernetes API；无权限与不存在使用不泄漏资源存在性的统一错误语义。
