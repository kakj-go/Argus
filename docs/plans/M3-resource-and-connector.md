# M3：资源、Secret 与 Connector 闭环

## 目标

让企业管理员不依赖 AI 即可接入和管理带标签的 Host/Kubernetes、Secret/Credential、堡垒机 Connector 和受控公网 Direct 资源。

## 前置条件

- M2 身份、DataScope、审计和 Session 已完成。

## 任务

- [ ] `M3-LABEL-01` 实现 Host/Kubernetes `labels` 存储、索引、过滤、分组、批量选择和 `argus.io/*` 保护。
- [ ] `M3-LABEL-02` 实现授权敏感标签变更影响预览、Preview/Commit 和 AuthorizationVersion 失效。
- [ ] `M3-SECRET-01` 实现 SecretRef、Envelope Encryption、Credential、ManagedAccount 和 Broker 使用边界。
- [ ] `M3-HOST-01` 实现 Host CRUD、资源版本、连接模式和连接测试。
- [ ] `M3-K8S-01` 实现 KubernetesCluster CRUD、kubeconfig SecretRef、连接测试和基础资源只读查询。
- [ ] `M3-CONNECTOR-01` 实现一次性注册、CSR/mTLS、证书轮换/吊销、心跳和 connection_epoch。
- [ ] `M3-BASTION-01` 实现稳定 BastionScope、根 Host、成员关系、迁移和删除状态机。
- [ ] `M3-GATEWAY-01` 实现 Connector Gateway Session Registry、Drain 和跨副本路由。
- [ ] `M3-COMMAND-01` 实现持久化 ConnectorCommand、幂等、过期、ResultUnknown 和对账。
- [ ] `M3-DIRECT-01` 实现独立 Direct Executor Deployment、固定出口、DNS 前后校验、Host Key 和 SSRF/私网阻断。
- [ ] `M3-WEB-01` 将主机/Kubernetes/Secret/Connector 页面接入真实 API，使用统一标签控件。

## 测试

- 标签校验、保留命名空间、索引过滤和 DataScope 一致性。
- Connector 重注册、证书撤销、Gateway 切换、重复命令和断连对账。
- Direct Executor 拒绝环回、RFC1918、链路本地、云元数据、内部 DNS 和重定向私网。
- Secret 原值不进入列表、日志、Tool Result 或前端持久化。
- 临时 Namespace 接入测试 Connector/Host/Kubernetes 并在结束后清理。

## 退出标准

- 管理后台可真实完成堡垒机、内网 Host、公网 Direct Host 和 Kubernetes 接入。
- 所有列表/详情/批量操作遵守 DataScope。
- 标签修改可审计并正确触发授权失效。

## 不包含

- 人工 Web Terminal。
- Agent 驱动的变更执行。
- 完整 Collector/Telemetry 链路。
