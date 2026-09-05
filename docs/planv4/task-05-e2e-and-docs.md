# PlanV4 Task 05：E2E 与文档同步

## 状态

已完成。最终 Kubernetes 运行号：`20260901-planv4-final41`；脱敏 verify 证据：`artifacts/p4-e2e/20260901-planv4-final41/verify`。

## 目标

为 PlanV4 全部网络场景、堡垒机 A/B/C 和统一接入向导建立真实证据，并完成设计文档同步；本任务是 PlanV4 的统一验收出口。

## Kubernetes E2E

- [x] P4-E2E-01 `p4-self-enroll`：临时 Namespace 内以受限出站 Pod 模拟“只出不进”主机 → 向导建卡 → 生成命令 → bootstrap 安装 → 三信号推通；同令牌二次消费、撤权、卸载和零残留全部验证。
- [x] P4-E2E-02 `p4-executor-tunnel`：目标仅允许 Executor SSH 入站、禁止目标直连 ingest → 隧道建立 → Collector 经回环端口推通 → Executor Pod 删除后重建 → 队列与审计验证。
- [x] P4-E2E-03 `p4-bastion-tunnel`：堡垒机 + 成员模拟成员无法访问堡垒机 OTLP 端口 → `bastion_gateway + bastion_tunnel` 推通 → 隧道断开/恢复、状态徽章与审计验证。
- [x] P4-E2E-04 `p4-bastion-direct-install`：模式 B 经 SSH ConnectionTest、预览、代安装、自动 enrollment、在线收敛；用户界面不得显示 enrollment token 或安装命令。
- [x] P4-E2E-05 `p4-bastion-control-tunnel`：模式 C 在堡垒机无出站条件下完成代安装、控制隧道建立和 Connector 回连；Executor 重启后隧道重建，界面区分隧道故障和 Connector 离线。
- [x] P4-E2E-06 回归：`argus-dev doctor e2e` 与受影响套件通过；成功和失败路径均清理 Namespace/PVC/Lease/临时凭据。

## 浏览器交互验收

- [x] P4-WEB-01 主机五种模式：第一步只有模式卡，点击下一步后第二步才出现字段；上一步保留公共字段，切换模式按规则清理不兼容敏感字段。
- [x] P4-WEB-02 堡垒机 A/B/C：模式说明、前置条件、字段、CTA、第三步和结果态分别正确；B/C 不出现“一次性命令”文案。
- [x] P4-WEB-03 真实步骤语义：当前/完成状态、Back/Next、Enter 提交、焦点移动、错误定位和关闭恢复均由状态机驱动；禁止静态步骤条。
- [x] P4-WEB-04 命令生命周期：初次一次性结果、未领取结果、已领取/丢失后的令牌轮换、轮换待审批、审批后领取、过期和已注册状态全部覆盖。
- [x] P4-WEB-05 动作边界：待注册 token 轮换不显示 Connector replacement；已有 Connector 替换明确显示 fencing 影响并按危险操作策略处理。
- [x] P4-WEB-06 待注册卡片及 Pending Action：公开 `action_type` 驱动统一双语展示，确认卡、审批和 Chat 无原始 i18n key、服务端 title/summary/diff、内部 tool/resource ID 或 operation/event 状态枚举；状态、下一步、过期时间和审批入口可理解。
- [x] P4-WEB-07 zh-CN/en-US × light/dark、桌面常用视口和 axe 门禁通过；不存在必须依赖两个独立长滚动区才能完成的步骤。
- [x] P4-WEB-08 安全断言：一次性命令不出现在 URL、localStorage、sessionStorage、普通 Query Cache、日志和资源 DTO。

## 文档同步任务

- [x] P4-DOC-01 docs/03 已同步主机五模式、堡垒机 A/B/C、统一新增流程、一次性结果、命令轮换与 Connector 替换边界。
- [x] P4-DOC-02 docs/09 已同步 route × transport、控制隧道与遥测隧道边界、Preview、配置渲染和可靠性语义。
- [x] P4-DOC-03 docs/00 已同步身份不变量、transport 正交、一次性结果和 enrollment 轮换审计约束。
- [x] P4-DOC-04 docs/13 已记录 Task 06/07 当前实现盘点和已知限制；PlanV4 专项 E2E 完成后补充最终运行号与脱敏证据再关闭。
- [x] P4-DOC-05 docs/README.md 已加入 PlanV4 阅读入口。
- [x] P4-DOC-06 `network-scenario-wizard.html` 已标记为历史探索稿，并在页面内声明正式交互以 Task 07、堡垒机边界以 Task 06 为准。

## 验收门禁

1. Task 01～07 的未完成项全部关闭，运行号和脱敏证据记录在 docs/13 与本文件实施回顾中。
2. OpenAPI/protobuf/sqlc/前端生成物无漂移；新 action、错误码和状态都被实际返回路径与测试使用。
3. 审计抽查覆盖令牌生成/领取/轮换/撤销/消费、Connector replacement、隧道建立/断开/认领和 bootstrap 拉取，且无明文敏感值。
4. 不要求兼容开发期旧 UI、旧 mock/localStorage 数据和旧 action 名称；最终代码中不得保留双路径、兼容分支或废弃 feature flag。
5. Production 认证必须覆盖堡垒机 C；唯一明确不支持的是“堡垒机无出站且 Argus 也无法 SSH”的双向不通组合。

## 最终证据（2026-09-01）

- Kubernetes P4 套件完成 self-enroll、堡垒机 A 命令安装、B 代安装、C 控制隧道、Executor 遥测隧道、堡垒机成员遥测隧道、replacement/fencing 和 Direct Executor 跨副本接管。
- B operation 与两次 C operation 均进入 `completed`；replacement 后旧控制隧道以 `connector_replaced` 进入 `removed`，当前控制隧道在新 epoch established；两类遥测隧道均由新 owner 接管并 established。
- real Playwright `e2e/p4-real.spec.ts` 通过；最终 `argusctl verify` 19/19 通过，证书、Enterprise/Platform HTTPS、CORS、工作负载和数据服务均健康。
- mock Playwright 的 P4-WEB-01～08 全部通过，覆盖 zh-CN/en-US、light/dark、1366×768/1920×1080 与 axe。命令/token 未出现在 URL、storage、Query Cache、console 或资源 DTO。
- 后续全项目 i18n 复核补充 `PendingActionPublic.action_type` 契约投影、所有已知动作 zh/en 穷举单测、Operation/Event/Tunnel 状态映射和用户可见 JSX 字面量静态门禁；真实/Mock 模式不再依赖持久化英文或中文标题决定 UI。
- 套件在独占阶段先通过 `argus-dev doctor e2e`，随后删除临时 Helm release、Namespace、PVC、Lease、凭据和测试 CRD 后退出 0。测试结束后恢复共享开发环境中的 Strimzi、Altinity CRD、OpenSandbox 和本地 registry；恢复后的共享环境再次执行 `argusctl verify`，19/19 全部通过。共享集群按设计不再满足“dedicated E2E cluster” doctor 条件。
- Evaluation 集群的 NetworkPolicy enforcement 只能标记为 `unverified/degraded`；production NetworkPolicy、PDB、拓扑分散、配额和 artifact 检查均由 production 门禁覆盖。

## 2026-09-02 回归证据

- 新增 Mode A 删除后使用原名称重新创建的浏览器回归，覆盖危险删除审批、根 Host 同步删除与软删除名称复用；定向 Playwright 已通过。
- 新增一次性命令响应空数组契约及 UI `null` 防御测试，并覆盖 PostgreSQL 名称冲突到 `RESOURCE_NAME_CONFLICT` 的不可重试分类。临时 PostgreSQL 16 实例已实际执行 00001～00033 全部迁移、down/up，并通过根 Host 单实例约束与删除后同名 Scope/Host 重建断言。
- 本次为定向回归证据，不替代上方 `20260901-planv4-final41` 的完整 Kubernetes/real Playwright 运行记录。
