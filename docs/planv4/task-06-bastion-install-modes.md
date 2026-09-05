# PlanV4 Task 06：堡垒机三种安装模式

> 2026-08-31 决策：原“堡垒机无出站场景排除”结论被替代。模式 C 复用 Executor SSH remote forward 承载 Connector 控制通道，不建设双层隧道新架构。

## 状态

已完成。A/B/C 契约、签名产物、正式安装脚本、持久化 operation、独立控制隧道、人工重试、replacement fencing、前端结果态和真实 E2E 均已验收。

## 三种模式

| 模式 | 堡垒机出站 | Argus 可 SSH | 安装方式 | 安装后控制通道 | 用户结果 |
| --- | --- | --- | --- | --- | --- |
| A 手动命令安装 | 必须 | 不要求 | 用户在堡垒机执行一次性命令 | Connector 出站直连 gateway:9443 | 一次性命令 |
| B 平台 SSH 代安装 | 必须 | 必须 | Executor 下载、验签、安装并 enrollment | Connector 出站直连 gateway:9443 | 安装进度与完成状态 |
| C 平台代安装 + 控制隧道 | 禁止/不可用 | 必须 | Executor 代安装并维持 remote forward | Connector→127.0.0.1:9443→隧道→Gateway | 安装/隧道进度与完成状态 |

模式 A 不以“Argus 一定不可 SSH”为前提；即使双向可达，用户仍可基于运维策略选择手动命令。堡垒机既不能出站、Argus 也无法 SSH 的组合没有可执行路径，不在界面保留禁用卡。

## 模式 B

- 表单：名称/环境/标签 + 地址/端口/账号/凭据。
- 提交：先运行 Direct ConnectionTest 并冻结地址、主机键与凭据租约，再生成 Pending Action 预览。
- 执行：预分配 Scope/Connector/root Host → Executor 执行类型化 `connector_install` → 下载与验签产物 → 写入配置与 systemd → enrollment → 等待 Connector online。
- 稳态：root Host 的 connection_mode 为 `connector_local`，Connector 出站行为与模式 A 相同。
- 页面结果：展示测试、安装、enrollment、在线收敛步骤；不得向用户返回或展示内部 enrollment token。

## 模式 C

- 表单和 ConnectionTest 与 B 相同。
- Executor 在安装前建立并监督 `127.0.0.1:9443 → connector-gateway` remote forward，Connector 的 enrollment/长连接端点由计划渲染为本机回环地址。
- mTLS 端到端不变，Executor 只能转发密文；隧道事实、epoch、fence 和 lease 落 PostgreSQL，重启后重建。
- 页面分别展示“SSH 可达、安装中、控制隧道建立、Connector 回连”状态；隧道断开与 Connector 离线不得合并成一个原因。

## 一次性命令与替换边界

- 模式 A 初次创建确认后返回一次性命令，明文只在结果态展示一次。
- 模式 A 默认展示下载动态引导脚本并执行的一行命令；`GET /api/v1/connectors/bootstrap-script` 通过请求头校验未过期 token，按 token 绑定的 Connector release 重建完整严格脚本，读取脚本不消费 enrollment token。自签名快速模式只在第一个脚本请求使用 `--insecure`，后续安装器、Manifest、Artifact 与 enrollment 继续使用内嵌 Trust Bundle 严格校验。
- 待注册 Scope 没有 active Connector 时，用户丢失或令牌过期应调用独立的 enrollment token 轮换动作；轮换只吊销未消费令牌，不得命名或呈现为 Connector replacement。
- 已有 Connector 的迁移、隔离或更换机器才调用 `bastion.connector.replace`；该动作执行 fencing、使旧 Connector 离线并按危险操作策略审批。
- 异步审批完成且一次性结果尚未领取时，待注册卡片必须显示“领取安装命令”；领取后只保留“重新生成安装命令”。
- B/C 不生成用户可见命令；失败后提供按 operation 语义重试或重新测试，不复用模式 A 的 token 轮换入口。

## 最终实现盘点（2026-09-01）

| 原任务 | 状态 | 复核结论 |
| --- | --- | --- |
| P4-BI-01 三卡 A/B/C | 已完成 | 第一步只显示 A/B/C，第二步才显示字段，第三步与结果按模式分流 |
| P4-BI-02 Connector 产物发布 | 已完成 | Linux amd64/arm64 不可变清单、SHA256 与 Ed25519 签名纳入发布和 production 检查 |
| P4-BI-03 契约/proto | 已完成 | `onboarding_mode`、类型化 install operation、事件与错误码已生成且无漂移 |
| P4-BI-04 preview/commit | 已完成 | B/C 冻结 ConnectionTest、artifact 和凭据版本，Execution 按 operation 终态收敛 |
| P4-BI-05 Executor 安装器 | 已完成 | 流式传输签名产物、原子替换、幂等 systemd 和不确定结果探测均已覆盖 |
| P4-BI-06 控制隧道 | 已完成 | 长期 desired fact 独立于 operation，支持 lease/epoch/fence、退避重建和跨副本接管 |
| P4-BI-07 严格 TLS 安全引导 | 已完成 | 模式 A 正式入口内嵌专用 CA，固定安装器摘要，下载校验后再按架构双重验签、原子安装并 enrollment |
| P4-BI-08 前端与 E2E | 已完成 | mock/real Playwright 与 P4 Kubernetes A/B/C 场景全部通过 |

## 收口结果

- [x] P4-BI-R01 完成 Connector 双架构产物发布、签名、下载与真实安装证据。
- [x] P4-BI-R02 固化 B/C operation 状态、错误码、幂等重试和 Connector online 收敛条件。
- [x] P4-BI-R03 固化模式 C 控制隧道的 epoch/fence/lease、重建和状态投影。
- [x] P4-BI-R04 新增待注册 Scope enrollment token 轮换契约，限制 `bastion.connector.replace` 的适用前件。
- [x] P4-BI-R05 完成模式 A 正式一行式安装脚本及 sha256/ed25519 验证链。
- [x] P4-BI-R06 按 Task 07 重构 A/B/C 的向导、动态说明、结果态与待注册操作。
- [x] P4-BI-R07 按 Task 05 完成 A/B/C mock、real Playwright 和 Kubernetes E2E。

## 风险与约束

- 模式 C 的可用性依赖 Executor 隧道，滚动发布和容量策略必须控制同时中断数量；重连安全由 fencing 保证。
- root Host 在安装期通过 Direct Executor 访问，稳态由 Connector 管理；Preview、审计和详情必须区分安装路径与稳态 connection_mode。
- 凭据只以引用和短期租约进入执行器，前端、Pending Action diff、审计和错误均不得出现凭据值。
- 当前处于开发阶段，可直接替换已有 action/UI/mock，不保留旧命令按钮语义或历史数据兼容分支。

## 实施回顾（2026-09-01）

B/C operation 固定经过 SSH、artifact 校验/传输、service 安装、C 控制隧道、enrollment、等待在线与完成阶段，整体 10 分钟并最多自动重试 3 次；失败后人工重试创建新 operation，冻结测试失效或主机键变化时强制返回第二步。安装 operation 完成后不再承担模式 C 的长期控制隧道事实。

专项运行 `20260901-planv4-final41` 验证 B/C 安装完成、C replacement 后旧隧道 fencing、当前 epoch 建立和 Executor Pod 漂移接管。B/C 的 UI 和公开 DTO 未出现一次性命令或 enrollment token。
