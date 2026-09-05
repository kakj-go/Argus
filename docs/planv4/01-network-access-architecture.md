# PlanV4 总体架构：网络接入模式与遥测传输

> 2026-09-01 状态：已实现并通过运行 `20260901-planv4-final41` 验收。模式 C 的长期控制隧道是独立 PostgreSQL desired fact，不依附安装 operation；B/C Execution 由 operation 真实终态收敛。

## 1. 场景模型

接入能力由两个正交方向决定：**控制面**（谁能 SSH/WinRM 到目标执行安装与管理）与**数据面**（OTLP 如何回流）。以「Argus 侧执行者可达目标？」×「目标可出站到 Argus？」得到四象限：

| | 目标可出站 | 目标无出站 |
| --- | --- | --- |
| 执行者可达目标 | ① `direct_ssh` + 直推（现有） | ② `direct_ssh` + `executor_tunnel`（本计划） |
| 执行者不可达目标 | ⑤ `self_enrolled` 自助注册（本计划）；堡垒机自身即此模式的现有形态 | 仅当存在「可达成员 + 可出站」堡垒机：③ `via_bastion`（现有）+ `bastion_tunnel`（本计划） |

成员侧的判定简化为一个问题：**成员能否连上堡垒机的 OTLP 监听端口**。能则标准 `bastion_gateway`；仅 SSH 可达（堡垒机→成员方向）则 `bastion_tunnel`。「双向完全不通」无路径，产品诚实拒绝。

## 2. 连接模式

### 2.1 模式矩阵

| 模式 | 创建方式 | 执行路径 | Bastion Scope | 远程终端 |
| --- | --- | --- | --- | --- |
| `connector_local` | Connector 安装命令注册（现有） | Connector 管本机 | 是，根堡垒机 | 支持 |
| `via_bastion` | 选堡垒机 + 内网地址（现有） | Argus → Connector → SSH/WinRM | 是，成员 | 支持 |
| `direct_ssh`/`direct_winrm` | 地址 + 凭证（现有） | Direct Executor → 目标 | 否 | 支持 |
| `self_enrolled`（新） | 平台生成一次性安装命令，用户在目标执行 | 无入站执行路径；bootstrap 自装 | 否 | **第一版不支持** |

`self_enrolled` 的约束与现有三值不同：`bastion_scope_id` 必须为空；`address`/`port` 允许为空，首次 enrollment 成功后由目标自报地址/hostname 回填；`platform` 限定 `linux`（Windows 维持 `validation_pending` 矩阵）。

### 2.2 `self_enrolled` 生命周期

```text
用户创建 pending Host（预分配 host_id + 冻结安装计划）
→ 生成一次性自助安装令牌（24h 有效、单次消费、绑定企业与 host_id）
→ 用户在目标执行内嵌 CA、固定摘要、先下载验签再执行的安全安装命令
→ bootstrap 拉取冻结计划 + 产物清单，校验签名后安装 systemd 服务
→ argusidentity enrollment 注册并取得 Leaf 证书
→ Host 转 active，开始 direct_argus 推送
→ 变更/升级 = 轮换 enrollment 令牌并生成新安装命令；卸载使用独立 30 分钟授权和完成回调
```

- 令牌语义照抄 Bastion 注册（docs/03 §3.4）：数据库条件更新原子消费；同设备幂等重试返回首次结果；他设备 `409 TOKEN_ALREADY_CONSUMED`；撤销/过期/未知令牌分别返回登记错误码。
- 计划冻结纪律与 ConnectionTest 等价：令牌生成时冻结 distribution 版本、Profile 集合、`direct_argus` 路由与 transport=direct；重跑命令只做幂等收敛，不接受任何偏离计划的参数。
- `last_seen` 是唯一在线事实（同堡垒机成员先例）；hostprobe 对 `self_enrolled` 主机不探测（不可直达）。
- 远程会话、AI 生产操作对 `self_enrolled` 主机 fail closed，返回显式错误码；不提供「装 Connector 变堡垒机」的隐式迁移。

## 3. 路由 × 传输矩阵

`telemetry_routes.kind`（身份与逻辑上游）与新增 `telemetry_routes.transport`（物理路径）组合：

| 组合 | 场景 | Collector 出口端点 | TLS 校验 | 身份链 |
| --- | --- | --- | --- | --- |
| `direct_argus` + `direct` | ①⑤ | 平台 ingest 公网端点 | 现有 | Leaf ↔ Ingest（现有） |
| `direct_argus` + `executor_tunnel` | ② | `127.0.0.1:<loopback_port>` | `server_name_override` = ingest 域名 + 现有 CA | Leaf ↔ Ingest，**Executor 只见密文** |
| `bastion_gateway` + `direct` | 标准成员 | 堡垒机 Gateway 端点 | 现有 Gateway mTLS | Gateway 签发/覆盖（现有） |
| `bastion_gateway` + `bastion_tunnel` | ③ | `127.0.0.1:<loopback_port>` | `server_name_override` = Gateway server name | 同上，仅传输变化 |
| `kubernetes_gateway` + `direct` | K8s | 现有 | 现有 | 现有（transport 恒为 direct） |

规则：

- `transport != direct` 时出口端点恒为本机回环；渲染必须同时写入 server name override 与原 CA，禁止 `insecure` 回退。
- loopback 端口必须由 Preview 探测并显式冻结到 route/plan；缺失、越界、占用或与 Tunnel 行不一致时 fail closed，不使用隐式端口默认值。
- 回环监听可被目标本机其他进程连接，但无 Leaf 私钥无法通过 mTLS；unix socket 转发（streamlocal）作为后续加固项，不阻塞本计划。
- 推送选择矩阵（docs/09 §5.2）在 kind 规则之上叠加 transport 可选性：`self_enrolled` 主机只允许 `direct_argus` + `direct`；堡垒机本机不得对自己开隧道（无意义）；跨 Scope/跨 Group 规则不变。

## 4. 隧道原语

### 4.1 统一语义

```text
发起者（能主动 SSH 到目标的一方）
  ├─ 场景② Direct Executor ──SSH──▶ 目标主机
  │      remote forward: 目标 127.0.0.1:<port> ─▶ Executor 拨号平台内部 ingest（svc 直连，不走 ingress 回环）
  └─ 场景③ 堡垒机 Connector ──SSH──▶ 成员主机
         remote forward: 成员 127.0.0.1:<port> ─▶ 堡垒机本机 Gateway listener
```

- 复用现有 SSH 底座：pinned host key、目标认证、DNS/禁用网段校验与 `ApplySSH` 同源；不新开协议面。
- 隧道承载的是 Collector → 上游的端到端 OTLP TLS 流；发起者是**可用性信任方**，场景② 中 Executor 连密文内容都不可见（机密性仍由 Leaf↔Ingest mTLS 保证）。场景③ 中堡垒机本来就在信任域内终结成员 TLS，语义与现有 `bastion_gateway` 一致。
- **OTLP 永不进入 Connector 控制通道或远程会话流**的不变量不变；隧道是独立 SSH 传输，不是 Connector 协议复用。

### 4.2 状态与监督

权威事实在 PostgreSQL（`telemetry_tunnels`）：

```text
telemetry_tunnels
├── enterprise_id / host_id / collector_id
├── initiator = direct_executor | connector
├── transport = executor_tunnel | bastion_tunnel
├── loopback_port / forward_target（冻结快照）
├── status = desired | establishing | established | degraded | down | removed
├── epoch（单调递增，重建即 +1）
├── lease_owner / fence / lease_expires_at
├── last_established_at / last_heartbeat_at / last_drop_reason
└── byte counters 摘要（限速与容量观测）
```

- Executor/Connector 侧 Reconciler 按 `desired` 行建连：SSH keepalive（周期全局请求探活）、指数退避重连、断线事件回写 `degraded/down` 与原因。
- Executor 重启/Pod 漂移：租约过期 → 其他副本按 SKIP LOCKED 认领 → epoch+1 重建；Collector 侧端点不变，断流期间由其 `file_storage` 队列缓冲——**隧道断开 ≠ Collector 死亡**，两者必须以不同状态呈现。
- 长周期凭证：Credential Broker 新增 `tunnel` 用途可续租租约，绑定 host/collector/隧道 epoch；授权变化、AuthorizationVersion 递增或隧道删除立即失效，与远程会话同一撤权路径。
- 容量治理：每 Executor 并发隧道数上限、隧道字节速率与 Leaf 队列水位监控；超限拒绝新隧道并提示改用堡垒机 Gateway 模式。

### 4.3 安装与路由测试语义

- 隧道路由的 Preview 增加变更项：「建立 SSH 隧道（回环端口 X → forward 目标）」，并在冻结计划中包含隧道前件（凭证、pinned key、目标可达性测试）。
- 路由测试顺序固化为：先建隧道 → 安装/下发 Collector → 经隧道健康检查与首条数据验证 → 路由转 active。不能只测网络端口可达。
- 卸载/路由切换必须先降级 Collector（或确认队列排空策略），再拆隧道，避免先断管道后停进程造成队列无谓膨胀。

## 5. 身份与信任链（不变量重申）

- Leaf 证书签发、Gateway 对成员的凭证签发与覆盖、Ingest 的可信身份注入**零改动**；transport 不参与任何身份判定。
- Ingest/Gateway 继续以认证结果覆盖 `argus.enterprise.id`、`argus.resource.id`、`argus.collector.id`；回环端点不构成信任放宽。
- 自助安装命令不含任何永久凭证；令牌、计划与产物签名是唯一信任根；私钥仍在目标本地生成。

## 6. 数据模型变更汇总

| 对象 | 变更 |
| --- | --- |
| `hosts` | `connection_mode` + `self_enrolled`；address/port 约束放宽（仅 self_enrolled 允许空/0）；自报地址回填 |
| `host_enrollment_tokens`（新） | 令牌哈希、绑定企业/host_id/冻结计划哈希、状态机（active→consumed/revoked/expired）、设备指纹摘要 |
| `host_uninstall_tokens`（新） | 独立 30 分钟卸载授权、交换/完成状态与审计摘要；不复用 enrollment token |
| `execution_one_time_results`（既有能力扩展） | 复用 AES-GCM 加密、短时有效、原子领取和幂等重放语义；增加主机/堡垒机安装命令 result kind，不新增明文结果存储 |
| `telemetry_routes` | + `transport`、`loopback_port`；kind/transport 组合 CHECK |
| `telemetry_tunnels`（新） | 见 §4.2 |
| `telemetry_collector_operations` | `executor_kind` + `bootstrap`（self_enrolled 安装记录，plan 冻结于令牌生成） |
| `connector_release_versions`（新） | Linux amd64/arm64 不可变产物清单、SHA256 与 Ed25519 签名元数据 |
| `connector_install_operations`（新） | B/C 平台代安装的冻结计划、测试引用、阶段事件、失败码、重试来源与在线收敛；operation secret 只存加密 envelope |
| `connector_control_tunnels`（新） | 模式 C 长期 desired/status、epoch/fence/lease/owner、心跳、字节与断开原因；生命周期独立于安装 operation |
| Credential Lease | 用途 + `tunnel`（长周期可续租） |

## 7. UI 信息架构

### 7.1 统一向导状态机

主机与堡垒机共享 `select_mode → details → verify|confirm_command → installing|command_result|completed` 状态机，正式交互见 Task 07。

- **第一步：选择接入模式**。只显示模式卡、适用条件、需要准备和平台行为；不得出现名称、地址、凭据、标签或命令表单。
- **第二步：填写信息**。只显示已选模式摘要和该模式字段；完整模式列表收起，用户通过“更改模式”返回第一步。
- **第三步：验证与确认**。SSH/WinRM 路径运行 ConnectionTest 并展示冻结事实和 Preview；self_enrolled 与堡垒机 A 展示冻结计划、前置条件和“确认并生成命令”。
- **提交后结果**。B/C 和其他自动安装路径进入 operation 进度；命令型路径进入一次性结果；结果状态不计入可返回编辑的步骤条。
- 步骤条、DOM 可见内容、焦点和 submit 行为由同一状态驱动；禁止静态步骤条、隐藏空 form 和场景/表单同屏的伪步骤。

### 7.2 模式目录

- **普通主机**：①双向可达、②只进不出、⑤只出不进/自助安装、标准堡垒机成员、受限端口堡垒机成员。
- **堡垒机**：A 手动命令安装、B 平台 SSH 代安装、C 平台代安装 + 控制隧道。A 只要求堡垒机可出站，不要求 Argus 一定不可 SSH；B/C 的结果是安装进度，不展示 enrollment token。
- 场景卡只帮助用户表达网络事实；ConnectionTest、冻结计划和服务端约束是提交事实来源。失败诊断可以建议返回第一步改选，但不得自动替用户切换。
- `direct_ssh`、route kind、transport、端口和执行器名称属于技术详情，不作为普通用户完成选择的必要知识。

### 7.3 一次性命令与待注册状态

- 初次创建命令型资源可以生成一次性结果；明文仅在服务端生成并立即加密的瞬间、原子 claim 响应和当前内存结果态短暂存在，不进入 URL、浏览器持久化、普通 DTO、日志或审计。
- “领取已有一次性结果”“轮换待注册令牌”“生成 self-enrolled 卸载命令”“替换已有 Connector”分别建模。待注册 Scope 无 active Connector 时只允许 token rotate；已有 Connector 的迁移/隔离才允许 replacement 和 fencing；卸载授权不得伪装成 enrollment rotate。
- 待注册卡片必须区分：结果可领取、已领取等待注册、轮换待审批、审批后可领取、令牌过期、自动安装中、安装失败和已注册。
- 卡片只呈现用户可理解的资源名称、状态、下一步和过期/审批信息，不暴露原始 i18n key、tool/action 名、内部资源 ID 或未整理 diff。

### 7.4 资源状态呈现

- 主机详情“数据推送”区域展示 route kind × transport 与隧道状态（established/degraded/down、最后建连时间与断开原因）。
- self_enrolled 在线状态走 collector last_seen；隧道路径分别呈现 Collector、隧道和 Connector 状态，不以一个“离线”掩盖根因。
- B/C 创建后保留安装 operation 可见性，用户关闭对话框后仍可从待注册卡片或任务记录恢复进度。

## 8. 安全不变量

1. 未知 transport、self_enrolled 的远程会话请求、令牌非原子消费、隧道前件缺失：全部 fail closed + 登记错误码。
2. 隧道建立/断开/认领/令牌生成消费/bootstrap 拉取全部入审计；审计不含令牌明文与密钥。
3. 回环监听不豁免 mTLS；任何 `insecure` 导出配置在渲染层被拒绝。
4. AuthorizationVersion 递增使隧道租约与令牌立即失效（与现有撤权链路同一实现）。
5. self_enrolled 主机不进入任何执行器命令派发路径；`configure/upgrade/repair` 操作返回显式不支持错误并引导重新生成命令。
6. 一次性结果继续复用 `execution_one_time_results`：静态加密、短时有效、原子领取、同一幂等键可重放；明文不得进入 PendingAction、Execution、资源投影、审计、日志或浏览器持久化。
7. 待注册 Scope 只允许轮换未消费 enrollment token；`bastion.connector.replace` 必须以 active Connector 为前件并执行 fencing，服务端不能依赖前端入口区分两者。
8. 安装 operation 只描述一次安装过程；完成或失败后都不能充当模式 C 长期控制隧道的 desired fact。Executor 内存只保存本副本当前持有的连接。
