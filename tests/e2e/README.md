# End-to-end tests

正式部署路径保持 `argusctl + Helm`。仓库 E2E 由跨平台 Go CLI `argus-dev` 编排，在专用干净 Kubernetes Context 的临时 Namespace 中调用真实 `argusctl` 完成安装、验证和卸载；client-go 负责 Lease、Fixture、等待、日志、exec、port-forward、脱敏诊断和无条件清理。

M6/M8 的堡垒机远程访问场景额外占用本机 `127.0.0.1:4222`，将临时 Fixture Chart 的 SSH Target 转发给运行在开发主机上的真实 Connector；`doctor e2e` 会与其他本地端口一起检查该端口。

```text
go run ./cmd/argus-dev doctor e2e
go run ./cmd/argus-dev e2e run --suite m2
go run ./cmd/argus-dev e2e run --suite m7 --run-id local-m7 --artifacts artifacts/m7-e2e/local-m7
go run ./cmd/argus-dev e2e run --suite m10-query --unit-only
```

Suite 依赖：

```text
m2        = M2
m3        = M2 + M3
m4        = M2 + M4
m5        = M2 + M3 + M4 + M5
m6        = M2 + M3 + M6
m7        = M2 + M3 + M4 + M5 + M7
m10-query = M7 + M10 Query
m8        = M6 baseline + M7 baseline + Local Hardening/Backup/Restore
```

Fixture Chart 位于 `tests/e2e/helm/argus-e2e-fixtures`，只提供 SSH Target、Replay Model、WinRS Simulator、Artifact Server 和测试镜像装载，不进入正式发布包。需要场景身份、Ticket 或短期证书的 Remote Client 与 Telemetry Generator 由 client-go 按用例创建受控 Pod/Job，并随临时 Namespace 清理。Playwright 仍使用 `web/apps/enterprise/e2e/*.spec.ts`。

2026-08-24 最终运行 `fv-20260824-m8-final13` 验证了上述依赖闭包、脱敏诊断和零残留清理。完整 E2E 失败时不得保留 Secret；当卸载先删除 Service、随后端口转发返回 NotFound 时，Harness 将其作为幂等停止结果处理。

完整 E2E 需要 Docker、kubectl、兼容 Kubernetes、StorageClass、受支持节点架构、空闲本地端口、至少 25 GiB 主机磁盘，以及没有其他 Helm release 持有 Strimzi/OpenSandbox 固定 ClusterRole 的专用 Context。`doctor e2e` 和 `e2e run` 都在镜像构建前检查该所有权；当前正式部署所在集群不应作为 E2E 目标。`--kube-context`、`ARGUS_E2E_KUBE_CONTEXT` 和自动发现值按优先级决定唯一的检查与运行目标。成功或失败都会先用独立超时预算收集脱敏诊断，再用新的清理预算删除 Fixture、临时 Namespace/PVC、Lease、Cluster RBAC、测试镜像和本地子进程。

每个 E2E 镜像都写入本次 run 专属 OCI label，确保其 manifest digest 不与正式 `dev` tag 或其他运行共享。Registry 清理只接受本次运行的精确 tag，先通过 Registry V2 API 解析 digest 并枚举同仓库 tag；若发现共享 digest 或 Registry 禁止 DELETE，清理立即失败并报告残留，不会扩大到 repository 或误删正式镜像。仓库不再提供或执行 Bash E2E 脚本；Make target 仅转发到相同 `argus-dev` 命令。
