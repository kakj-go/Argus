# PlanV4：主机网络接入模式扩展

## 目标

PlanV4 将主机接入从「双向可达 + 堡垒机成员」扩展为覆盖单向网络、受限端口与自助安装环境的完整体系。用户创建主机或堡垒机时先选择接入模式，第二步再填写该模式需要的信息，平台随后执行连接验证、资源预览、自动安装或生成一次性命令。

`network-scenario-wizard.html` 仅保留为早期视觉探索记录，其中“左右栏同时显示场景与表单”“场景④暂缓”等内容已经过期，不再作为交互或验收依据。正式交互基线以 [Task 07](./task-07-onboarding-experience-refactor.md) 为准。

## 最终覆盖矩阵

| 场景 | 网络条件 | 接入方式 | 遥测回传 | 当前状态 |
| --- | --- | --- | --- | --- |
| ① 双向可达 | Argus ⇄ 主机互通 | `direct_ssh` | `direct_argus + direct` | 已完成并验收 |
| ② 只进不出 | Argus→主机 ✓，主机→Argus ✗ | `direct_ssh` | `direct_argus + executor_tunnel` | 已完成并验收 |
| ③ 成员连不上堡垒机端口 | 堡垒机可出站、可 SSH 成员；成员→堡垒机 OTLP 端口 ✗ | `via_bastion` | `bastion_gateway + bastion_tunnel` | 已完成并验收 |
| ⑤ 只出不进 | Argus→主机 ✗，主机→Argus ✓ | `self_enrolled` | `direct_argus + direct` | 已完成并验收 |
| 标准堡垒机成员 | 堡垒机与成员互通 | `via_bastion` | `bastion_gateway + direct` | 已完成并验收 |
| 堡垒机 A | 堡垒机可出站；不要求 Argus 可 SSH | 手动执行一次性命令 | Connector 出站直连 | 已完成并验收 |
| 堡垒机 B | 双向可达 | Executor 经 SSH 代安装 | Connector 出站直连 | 已完成并验收 |
| 堡垒机 C | Argus 可 SSH；堡垒机无出站 | Executor 代安装 + 控制隧道 | Connector 经反向隧道回连 | 已完成并验收 |

场景编号沿用讨论稿（①②③⑤）。原场景④在 2026-08-31 经 [Task 06](./task-06-bastion-install-modes.md) 重新决策：不再建设双层隧道，而是复用 Executor 的 SSH remote forward 承载 Connector 控制通道，形成堡垒机模式 C。堡垒机既无出站、Argus 也无法 SSH 的组合仍然无解，不在界面中保留禁用占位。

## 架构决策摘要

1. **传输与路由正交**：route kind 决定身份、凭证和逻辑上游，transport 只决定字节物理路径；不新增组合式 route kind。
2. **反向隧道使用统一原语**：场景②由 Direct Executor 发起，场景③由堡垒机 Connector 发起，堡垒机 C 的控制隧道由 Direct Executor 发起；隧道只转发端到端加密流量。
3. **自助注册复用既有 enrollment 信任链**：预分配资源 ID、一次性令牌、冻结计划和签名产物，不引入第二套注册协议。
4. **权威事实落 PostgreSQL**：令牌、隧道与安装 operation 均可在执行器重启后重建；Redis 只承担缓存与唤醒。
5. **向导是领域状态机，不是静态布局**：主机与堡垒机共享 `选择模式 → 填写信息 → 验证/确认 → 结果` 状态机；第一步不得出现业务表单，第二步不得保留完整模式列表。
6. **按用户意图区分命令动作**：一次性结果领取、待注册令牌轮换、self-enrolled 卸载命令与 Connector 替换分别建模；不得为了复用界面而混合风险和前件。

## 文档

| 文件 | 内容 |
| --- | --- |
| [01-network-access-architecture.md](./01-network-access-architecture.md) | 场景模型、连接模式、路由×传输矩阵、隧道原语、身份不变量与 UI 信息架构 |
| [task-01-domain-and-contracts.md](./task-01-domain-and-contracts.md) | self_enrolled、transport、隧道与令牌的迁移和契约 |
| [task-02-self-enrolled-host.md](./task-02-self-enrolled-host.md) | 场景⑤自助注册完整链路 |
| [task-03-network-wizard-console.md](./task-03-network-wizard-console.md) | 网络场景组件和控制台基础实现；交互结论由 Task 07 收口 |
| [task-04-reverse-tunnel-transport.md](./task-04-reverse-tunnel-transport.md) | 场景②③的隧道注册表、监督器与配置渲染 |
| [task-05-e2e-and-docs.md](./task-05-e2e-and-docs.md) | 全场景 E2E、浏览器交互和文档验收门禁 |
| [task-06-bastion-install-modes.md](./task-06-bastion-install-modes.md) | 堡垒机 A/B/C 三种安装模式及实现盘点 |
| [task-07-onboarding-experience-refactor.md](./task-07-onboarding-experience-refactor.md) | 主机与堡垒机统一向导、命令生命周期和待注册状态重构 |

## 架构边界变化

- `hosts.connection_mode` 增加 `self_enrolled`；`telemetry_routes` 增加 transport 维度；新增隧道、主机注册令牌与 Connector 安装 operation 权威事实。
- Direct Executor 从纯短任务执行扩展为可重建的长驻隧道监督者，但不得持有数据库之外的唯一状态。
- 待注册主机与堡垒机新增各自的 enrollment token 轮换动作；self-enrolled 卸载命令独立建模；`bastion.connector.replace` 只用于已有 Connector 的真实替换与 fencing。
- 前端接入流程从两个独立页面实现收敛为共享向导壳和领域模式目录；业务字段、验证和提交策略仍留在 hosts 领域模块。

## 实施原则

1. Contract first：动作语义、状态和错误码先固化，再改页面与 mock。
2. 身份优先：transport 和界面模式不得改变证书覆盖、企业/资源身份判定与 fail closed 规则。
3. 安全能力随功能交付：审计、配额、撤销、幂等、一次性结果认领和失败语义必须同步完成。
4. 真实步骤：步骤条、可见内容、键盘焦点和提交行为必须由同一个状态机驱动，禁止静态步骤条。
5. 不保留开发期兼容层：当前项目尚未发布，本轮可以直接删除旧单屏向导、旧 action 名称、旧 mock/localStorage 结构和无效文案；不做双 UI、双接口或旧数据迁移分支。
6. 复用统一组件库：通用向导原语进入 `@argus/ui`，领域编排留在 Enterprise hosts 模块；单文件 < 2000 行。
7. E2E 使用临时 Namespace，测试结束零残留；桌面 Web 为正式验收范围。

## 实施结果

1. Task 01～04 的领域、注册、一次性结果与遥测隧道已按无兼容迁移重写并通过契约、SQLC、单元和真实网络验证。
2. Task 06 已交付 Connector 双架构签名产物、A/B/C 安装、持久化 operation 与独立长期控制隧道。
3. Task 07 已删除旧命令端点和旧单屏交互，主机五模式与堡垒机 A/B/C 均使用共享状态机；第一步只选模式，第二步才填写信息。
4. Task 05 已完成 mock/real Playwright 与 Kubernetes 专项验收，运行证据见下节和 [Task 05](./task-05-e2e-and-docs.md)。

## 完成状态（2026-09-01）

PlanV4 Task 01～07 已全部完成。最终真实 Kubernetes 运行号为 `20260901-planv4-final41`，脱敏证据目录为 `artifacts/p4-e2e/20260901-planv4-final41/verify`。该运行完成了 self-enroll、堡垒机 A/B/C、Executor/Connector 遥测隧道、replacement fencing、跨副本接管、三信号推通、real Playwright、最终 19 项安装校验与零残留清理。

实施中确认并修正了两处原有架构偏差：模式 C 的长期控制隧道已从短期安装 operation 和进程级全局状态中拆出，成为 PostgreSQL 权威、可租约接管的 `connector_control_tunnels`；B/C Execution 不再在 operation 启动时提前成功，而是保持 `result_unknown` 并由 Reconciler 按真实安装终态收敛。修正后，PlanV4 的 `route kind × transport`、统一 Pending Action、一次性领取、身份边界和服务职责无需推翻。

Docker Desktop Evaluation 集群不能证明 CNI 对 NetworkPolicy 的实际执行，因此该项在预检中保留 `unverified/degraded` 提示；production 策略模板、精确端口、配额和部署门禁已由静态检查与 production artifact 检查覆盖。唯一明确不支持的网络组合仍是“堡垒机无出站且 Argus 也无法 SSH”。
