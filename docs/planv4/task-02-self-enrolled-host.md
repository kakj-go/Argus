# PlanV4 Task 02：自助注册主机（场景⑤）

## 状态

已完成。注册、bootstrap、签名安装、独立卸载、一次性领取、命令轮换和服务端 onboarding 投影均已收敛；旧明文命令端点已删除。真实受限网络、二次消费、撤权、卸载与零残留由运行 `20260901-planv4-final41` 验收。

## 目标

交付「只出不进」主机的完整闭环：平台生成一次性安装命令 → 用户在目标执行 → bootstrap 自装 Collector 并注册 → `direct_argus` 直推。远程终端、AI 生产操作与在线探测显式不支持并 fail closed。

## 前置条件

Task 01 契约与迁移完成。

## 任务清单

- [x] P4-SELF-01 Host 领域服务：`self_enrolled` 创建流程（预分配 host_id、平台/arch、标签、Profile 集合与 distribution 版本冻结）；免 ConnectionTest 分支；`pending → active` 状态迁移由 enrollment 成功触发。
- [x] P4-SELF-02 令牌服务：生成/撤销/原子消费/幂等重试（同设备指纹返回首次结果，他设备 409）；令牌绑定企业、host_id、冻结计划哈希、24h 有效期与单次使用；审计只记令牌 ID 与摘要。
- [x] P4-SELF-03 bootstrap 交换端点 `GET /host-install/{token}`：校验令牌与设备指纹，返回冻结计划、config bundle（复用 `configbundle.Render`，route 固定 `direct_argus + direct`）、产物清单（URI/sha256/签名/字节数）与 enrollment/ingest 端点；限流与审计；响应不含任何长期凭证。
- [x] P4-SELF-04 安装脚本 `deploy/scripts/host-install.sh`：默认返回下载动态引导脚本并执行的一行式入口；自签名部署仅首次请求可显式 `--insecure`，引导脚本内嵌 CA 后恢复严格 TLS；拉取 bootstrap → sha256 + ed25519 双校验（缺少签名、公钥或不匹配均 fail closed）→ 原子落盘与加固 systemd unit → 首次 enrollment → 幂等收敛。卸载使用独立短期授权和完成回调。
- [x] P4-SELF-05 enrollment 集成：复用 argusidentity enrollment 端点与既有一次性 enrollment token；enrollment 成功回写 host 自报地址/hostname、转 `active`、记录 `executor_kind=bootstrap` 的 operation 结果。
- [x] P4-SELF-06 在线语义：hostprobe 查询排除 `self_enrolled`（`internal/hostprobe`）；在线徽章走 collector last_seen，阈值与堡垒机成员一致；离线告警进入既有 Collector 自监控。
- [x] P4-SELF-07 操作边界：`configure/upgrade/repair` 对 self_enrolled 返回 `HOST_OPERATION_UNSUPPORTED_FOR_SELF_ENROLLED` 并引导「生成新命令」；卸载走新令牌命令；远程会话创建 fail closed。
- [x] P4-SELF-08 审计与权限：创建、令牌生成/消费/撤销、bootstrap 拉取、enrollment 激活、卸载全部入审计；RBAC 沿用 host.create/host.read 与 DataAuthorizationGrant，self_enrolled 主机同样受显式资源授权约束。

## 关键设计

- **命令一次性展示**：创建或轮换经 Pending Action 执行后，把加密短时结果写入既有 `execution_one_time_results`；原发起人通过统一 claim 接口原子领取，明文只进入当前内存结果态，关闭即清，不入 URL/localStorage/Query Cache/普通 DTO。异步审批后由 `one_time_result_available` 恢复领取入口。
- **一键安装**：结果只返回带一次性令牌请求头的 `command`，不再提供交互式或自动化命令变体；`GET /v1/host-bootstrap-script` 按冻结 token 计划动态生成完整脚本、`no-store` 返回且不消费 token。自签名快速模式只对这一次下载使用 `--insecure`，其余请求仍使用版本化 Trust Bundle；严格模式不会失败后自动降级。
- **冻结计划即测试**：self_enrolled 没有 ConnectionTest；令牌生成时的计划哈希承担同等「冻结事实」角色，重跑命令按哈希比对拒绝偏离。
- **Windows**：矩阵保持 `validation_pending`；脚本与端点按 linux amd64/arm64 先行，不做 Windows 承诺。

## 收口结果

- [x] P4-SELF-R01 将直接返回明文的 `POST /enterprise/hosts/{id}/install-command` 拆为 `host.enrollment.rotate` 与 self-enrolled 卸载命令两个明确 Pending Action；执行结果复用统一 claim 接口，随后删除旧端点和生成客户端方法，不保留兼容层。
- [x] P4-SELF-R02 按 Task 07 接入统一“选择模式 → 填写信息 → 确认命令 → 一次性结果”状态机，并覆盖未领取、已领取、过期、轮换待审批和审批后领取。
- [x] P4-SELF-R03 完成 Task 05 的真实受限出站 E2E、撤权、二次消费、卸载和零残留验证。

## 退出标准

P4-SELF-R01～03 完成；真实 E2E（Task 05）证明：只能出站的目标（出站仅 artifacts 域名 + telemetry 端点）经一条命令完成安装注册并推通三信号；同令牌二次消费 409；撤权后令牌与 Leaf 证书失效；卸载命令清理干净；审计链完整。单文件 < 2000 行，全部测试与契约门禁通过。

## 实施回顾（2026-09-01）

Bootstrap 首次交换使用数据库原子消费，同设备重试返回同一加密结果、其他设备返回 `TOKEN_ALREADY_CONSUMED`；Host 只在 Collector enrollment 成功后激活并回填自报信息。安装和卸载使用不同 token 表及端点，领取明文只存在于当前组件内存。Kubernetes 专项运行已验证受限出站安装、三信号、撤权、二次消费、卸载完成回调和清理。
