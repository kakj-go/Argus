# M8：本地安全、恢复与发布基座

## 目标与边界

M8 只在 arm64 Docker Desktop、本地临时 Kubernetes Namespace 和本地对象存储完成。完成状态固定为 `local_hardening_complete`，不代表 Production Ready。

- `local-hardening` 使用单副本 PostgreSQL、Redis、Kafka、ClickHouse、MinIO 和单节点 OpenBao。
- Production Profile 只允许 schema validate、lint 和 render；`argusctl install` 继续 fail closed。
- Linux amd64、Windows amd64、真实 WinRM、跨可用区灾备、生产固定出口、HA 和容量证明不进入退出标准。
- 本地性能与故障测试只形成回归证据，不形成生产 SLO、RPO 或 RTO 承诺。

## 已实现

- [x] `M8-IDENTITY-01` TOTP Enrollment、登录 Challenge、十个单次 Recovery Code、五分钟 Step-up 和平台超级管理员强制 Enrollment。
- [x] `M8-IDENTITY-02` MFA/密码变化撤销其他 Session、Step-up 和 Break Glass；TOTP counter 原子消费阻止重放。
- [x] `M8-BREAKGLASS-01` 企业 Break Glass 要求 Step-up、原因、工单引用、十五分钟 TTL、显式开关和高优先级审计；不扩大 RBAC/DataScope。
- [x] `M8-CRYPTO-01` 建立 `KeyWrappingProvider` 与 OpenBao Transit Adapter，Secret、模型凭证、MFA Secret、录像 DEK 和一次性结果使用版本化 Key Reference。
- [x] `M8-DEPLOY-01` 增加 `local-hardening` Profile、单节点 OpenBao Raft、幂等 Bootstrap、受限 Transit Token 和最小 NetworkPolicy；静态 KEK 仅保留 Evaluation。
- [x] `M8-DB-01` Server、Worker、Gateway、Direct Executor、Migration、Telemetry Ingest/Writer 使用独立 PostgreSQL Login；Telemetry 角色使用表级最小权限。
- [x] `M8-BACKUP-01` 增加 `argusctl backup create|verify|list`，生成分块 AES-256-GCM 归档、组件 SHA-256 清单和 `0600` 恢复密钥。
- [x] `M8-RESTORE-01` 增加 `argusctl restore plan|apply|verify`；仅允许唯一、空目标 Namespace，恢复 PostgreSQL、OpenBao Raft、MinIO、ClickHouse 和配置引用。
- [x] `M8-UPGRADE-01` 增加 `argusctl upgrade plan|apply|status|rollback`；阶段状态可恢复，Schema 前进后禁止破坏性回滚。
- [x] `M8-SUPPLY-01` 增加本地 SBOM、漏洞检查、License 门禁、离线 Hash Manifest、Ed25519 签名和 `make release-local`。
- [x] `M8-SECURITY-01` 扩展生产制品扫描，拒绝 Replay/mock、测试私钥/API Key 和安全绕过开关。
- [x] `M8-E2E-01` 增加 `make e2e-m8-k8s`，使用 Run ID、Lease、故障注入、加密备份、新 Namespace 恢复、诊断和无条件清理。

## 本地验证

常规门禁：

```text
make contract-check contract-breaking
go test ./...
go vet ./...
pnpm typecheck lint test build check:bundle check:real-build e2e
make check-production-artifacts
git diff --check
```

M8 Kubernetes 验证：

```text
make e2e-m8-k8s
```

该流程先复用 M2-M7 已完成的真实闭环证据，再安装 `local-hardening`，注入 Redis 清空、Server/Worker/Gateway/Writer/Query Pod 删除和 OpenBao 重启，随后创建加密备份、删除源 Namespace、恢复到唯一新 Namespace，并重新执行身份、授权、Card、录像和遥测验证。成功或失败都必须释放 Lease 并删除 Namespace/PVC。

## 完成规则

只有契约、Migration、Go/前端门禁、Helm render、`release-local` 和临时 Kubernetes E2E 均有本地证据后，状态才能写为：

```text
local_hardening_complete
```

该状态明确包含 `production_ready: false` 和 `production_profile_installable: false`。

## Production Validation 清单

以下内容不属于 M8 本地完成范围，未来必须在真实生产环境单独验证：

- PostgreSQL/Kafka/ClickHouse/OpenBao 多节点 HA、PITR、跨故障域恢复和容量。
- Kata/gVisor 等强化 Sandbox Runtime、真实 NAT/Egress Gateway 和出口地址证明。
- 生产 KMS/OpenBao HA、CA 根轮换、Object Lock、签名密钥托管和供应链平台集成。
- Linux amd64、Windows amd64、Windows Collector/Service 和真实 Windows Server WinRM 矩阵。
- 生产告警、值班 Runbook、SLO、RPO/RTO、渗透测试和跨集群灾备。

Production Profile 在上述清单完成前继续返回明确阻断，不因 `local_hardening_complete` 自动解除。
