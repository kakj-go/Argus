# M8：Production 就绪与全链路闭环

## 目标

把已完成的业务闭环提升到可生产发布：具备 HA、备份恢复、容量基线、供应链安全、故障演练和可重复的全链路 E2E。

## 前置条件

- M2 至 M7 的发布范围全部达到各自退出标准。
- PostgreSQL HA/备份和 Sandbox Runtime ADR 已决策。

## 任务

- [ ] `M8-ADR-01` 完成 PostgreSQL Operator/HA/PITR、Sandbox Runtime、Card Origin、Remote Access 录像格式 ADR。
- [ ] `M8-HA-01` 为无状态服务配置 HPA/PDB/TopologySpread/Drain，为状态组件配置副本与反亲和。
- [ ] `M8-BACKUP-01` 实现 PostgreSQL、Artifact、ClickHouse 和必要配置的备份、校验、保留与恢复流程。
- [ ] `M8-UPGRADE-01` 实现 `argusctl upgrade`、兼容 Schema 顺序、失败回滚和版本矩阵。
- [ ] `M8-SUPPLY-01` 生成 SBOM、镜像/Chart 签名、漏洞扫描、License 门禁和离线制品清单。
- [ ] `M8-SECURITY-01` 完成渗透测试、Secret/Token 泄漏扫描、NetworkPolicy 和最小权限审计。
- [ ] `M8-CRYPTO-01` 接入 Production 外部 KMS/HSM，演练 Secret KEK 与 Connector CA 根轮换、旧证书吊销和恢复。
- [ ] `M8-EGRESS-01` 在目标生产网络验证 Direct Executor NAT/Egress Gateway、声明出口地址、deny CIDR 和故障时 fail closed。
- [ ] `M8-IDENTITY-01` 为平台超级管理员实现强制 MFA、恢复码、凭证轮换和账号恢复演练。
- [ ] `M8-IDENTITY-02` 为 critical 操作实现 Step-up Authentication，并把未配置平台 MFA 设为 Production Profile 安装与发布硬阻断。
- [ ] `M8-CAPACITY-01` 完成 API、Connector、Remote Access、Kafka、Writer、ClickHouse 和 Query 容量 Benchmark。
- [ ] `M8-FAILURE-01` 演练 Redis 清空、Pod/Node 故障、Gateway Drain、Worker 接管、Kafka 积压、ClickHouse Replica 故障和网络分区。
- [ ] `M8-E2E-01` 建立唯一 Run ID、集群 Lease、常驻服务缩容/恢复、诊断导出和无条件清理框架。
- [ ] `M8-E2E-02` 自动化执行总计划第 7 节全部闭环场景。
- [ ] `M8-E2E-03` 增加长会话、大 ToolResult、多轮 Compaction、Compactor/Worker 故障接管、Provider 切换边界和 ContextSnapshot 恢复测试。
- [ ] `M8-SLO-01` 定义服务 SLI/SLO、告警、Runbook、RPO/RTO 和支持边界。
- [ ] `M8-RELEASE-01` 完成 Production Profile 阻断项清单、发布说明和已知限制。

## 测试

- 从空集群安装、初始化、业务接入、执行、Card、远程访问、遥测到卸载/恢复全链路通过。
- 从备份恢复到新 Namespace，业务状态、审计索引、录像引用和遥测查询达到 RPO/RTO。
- 每种故障均验证无跨企业泄漏、无重复危险执行、无不可恢复唯一状态。
- 验证平台超级管理员未完成 MFA 时 Production Profile fail closed；恢复码、MFA 重置和 Step-up 均有高优先级平台审计。
- 验证外部 KMS/HSM 不可用、CA 根轮换和固定出口漂移时 Production 连接路径 fail closed，且不存在回退到集群内明文密钥或任意出口。
- 长会话压缩后原始 Event、ToolResult 和审计可追溯，恢复上下文不包含已撤销权限或私有 Token。
- E2E 成功和故意失败两种情况下都清理 Namespace/PVC/Topic/Bucket 并恢复常驻副本。

## 退出标准

- Production Profile 不再有未决硬阻断 ADR。
- 所有启用的平台超级管理员均完成 MFA，恢复与 Step-up 流程通过自动化和人工演练。
- 安全、容量、恢复和升级证据可随 Release 归档。
- 第一版范围内的端到端用户闭环可重复部署、验证、升级和恢复。

## 不包含

- 第二版产品能力扩张。
- 为赶发布绕过未完成安全或恢复门禁。
