# PostgreSQL 部署决策

## 状态

- Evaluation：已决策并实现。
- Production：ADR 未决，安装被硬阻断。
- 决策日期：2026-08-15。

## 背景

PostgreSQL 保存 Argus 唯一业务事实，包括身份、授权、运行状态、Pending Action 和审计索引。Redis、Kafka、ClickHouse 与对象存储都不能替代 PostgreSQL 的权威状态。因此，Evaluation 可以接受较低可用性来验证安装基座，但 Production 必须同时解决高可用、备份恢复、升级和故障切换。

## Evaluation 决策

Evaluation 使用 Argus 自有 `argus-data` Chart 部署单实例 PostgreSQL StatefulSet：

- 镜像固定为 `postgres:18.6-alpine`。
- 默认 PVC 为 `2Gi`，使用 `ArgusInstallConfig.spec.storageClass`。
- 凭证由安装器生成并保存到 Kubernetes Secret。
- PostgreSQL Migration 只创建 Schema Version 和 Installation Check 表，不声明完整业务 Schema 已实现。
- `argusctl verify` 执行事务写入、读取和删除。
- 临时 E2E 会删除 PostgreSQL Pod 并在 StatefulSet 恢复后读取重建前数据，以验证 PVC 绑定和挂载。
- Evaluation Namespace 删除时允许显式删除数据；它不提供 HA、PITR 或生产恢复保证。

该模式只用于开发、演示和可安装性验证。单实例故障期间控制面不可用是 Evaluation 接受的降级，不得外推为 Production SLO。

## Production 阻断项

Production Profile 目前只提供容量、PDB/HPA、拓扑与入口模板。`argusctl install` 对 Production 返回 `POSTGRES_HA_ADR_REQUIRED`，直到后续 ADR 至少确定：

1. PostgreSQL Operator/发行方案及其版本、CRD 和升级责任。
2. 同步复制、故障切换、反亲和、拓扑分布和最小节点条件。
3. WAL/PITR、全量备份、加密、对象存储目标和保留周期。
4. RPO/RTO、恢复演练、备份校验和灾难切换流程。
5. Migration、连接池、只读/读写端点、凭证轮换和证书生命周期。
6. Operator 与 Argus Release/CRD 的共享、升级和卸载所有权。
7. 容量扩展、存储扩容、Major Version 升级和回滚策略。

在这些内容决策并通过多节点 HA 与恢复测试之前，Production 只允许 lint、schema validate 和 render，不能执行安装。

## 影响

- Evaluation 与 Production 保持相同 PostgreSQL 协议和应用配置边界，但不承诺相同可用性实现。
- 第一版安装 Schema 不提供关闭 PostgreSQL 或切换外部托管 PostgreSQL 的开关。
- 后续 Production ADR 可以替换 `argus-data` 中的 PostgreSQL 实现，但不能改变“PostgreSQL 是唯一业务事实存储”的架构不变量。
