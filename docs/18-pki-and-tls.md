# Argus 全链路 PKI、TLS 与 Trust Bundle

本文是 Argus 自有 HTTPS、gRPC 和 mTLS 链路的当前权威设计与运维基线。此次重构面向全新部署，不提供旧配置、旧数据库、旧协议或已安装节点的兼容路径。

## 1. 目标与边界

Argus 自有链路必须同时满足：

- 除显式配置的自签名“首次引导脚本下载”外，证书链、主机名/SNI、SAN、EKU、有效期和身份状态均严格校验；运行期没有 `-k`、`--insecure` 或 `InsecureSkipVerify`，也不会在校验失败后自动降级。
- 私有 CA 或 Argus managed CA 不需要写入客户主机的系统信任库。安装命令携带公共 Trust Bundle，运行期保存到 Argus 专用目录。
- 稳态只有一个 cert-manager `ClusterIssuer`。每个服务、Connector、Collector 和服务间客户端仍使用独立叶证书、私钥、SAN 与单一 EKU。
- 叶证书续期和正常根 CA 轮换不要求重装二进制。错过根 CA 重叠期的离线节点使用修复命令更新 Bundle 并重新注册身份。

下列信任不混入 Argus Bundle：Kubernetes API、kubelet、PostgreSQL、Redis、第三方模型服务、Webhook、外部密钥服务及客户私有镜像仓库。Kubernetes 镜像拉取由 kubelet/container runtime 与客户的 registry 配置负责；Pod 启动后的 Argus 通信由本文负责。

浏览器是单独边界。使用私有 CA 时，Connector/Collector 可以通过专用 Bundle 正常工作，但访问 Enterprise/Platform 门户的浏览器仍需由客户通过企业终端策略或浏览器信任该 CA；也可以使用受浏览器信任的公开 CA，但不得与另一套内部 Argus Issuer 混用。

## 2. 单 Issuer、多叶证书

单 `ClusterIssuer` 降低安装和轮换复杂度，但不表示共享叶证书或私钥：

| 身份 | 证书用途 | 关键隔离 |
| --- | --- | --- |
| Enterprise、Platform、Cards、Artifact | `serverAuth` | 各自 Secret 与 DNS SAN |
| Connector Gateway、Telemetry、Direct Executor | `serverAuth` | 各自 Secret、DNS/URI SAN |
| Connector、Collector | `clientAuth` | 每实例密钥、用途化 URI SAN、企业与角色绑定 |
| 服务间客户端 | `clientAuth` | 每调用方 Secret 与 URI SAN |

同一叶证书不得同时具有 `serverAuth` 和 `clientAuth`，每条服务间调用用途也使用唯一 URI SAN；即使调用者来自同一个 Deployment，Direct Executor、Gateway peer、Telemetry 等不同凭据也不能复用同一个身份 URI。mTLS 授权除验证公共 CA 外，还查询证书身份记录并核对 URI SAN、角色、企业、序列号、指纹、EKU 和吊销状态。因此，一个 Connector 客户端证书不能仅因为链到同一根 CA 就冒充 Collector 或服务端。

cert-manager 管理的服务间客户端证书由 PKI Controller 只读取对应 Secret 的 `tls.crt`，校验它链到当前 Bundle、具有唯一 `clientAuth` 和声明的 `spiffe://argus.io/services/...` URI 后登记到身份注册表。续期时新序列先登记、旧序列进入短重叠窗口；Direct Executor、Telemetry Query 和 Gateway Peer 在每次 TLS 握手时查询活动序列并核对 URI 与证书指纹，未知或已吊销叶证书即使链到同一 CA 也会被拒绝。Server、Worker 和 Connector Gateway 使用不同客户端 Secret，不能共享调用方私钥。

单 CA 的代价是根私钥泄露或 Issuer 误配置具有全局故障半径。缓解措施是 managed 根 Secret 冻结、CA 私钥不进入 Bundle、不下发到目标节点、叶私钥分离、短期客户端身份、证书身份注册表、严格轮换状态机和审计。需要密码学层面的完全独立故障域时，应部署独立 Argus 实例；本版本不支持一个实例内的公网 Ingress CA 与私有 mTLS CA 双 Issuer 模式。

## 3. 安装配置

Managed 模式由 Argus 生成十年期 ECDSA P-256 根 CA：

```yaml
spec:
  pki:
    mode: managed
    bootstrapTLSMode: insecure-first-fetch
    rotation:
      overlap: 168h
```

客户 Issuer 模式只引用 Ready 的 `ClusterIssuer` 并提供公共链，不接收 CA 私钥：

```yaml
spec:
  pki:
    mode: existing-cluster-issuer
    bootstrapTLSMode: strict
    issuerRef:
      name: customer-argus-ca
      group: cert-manager.io
    caBundle:
      file: ./customer-ca.pem
    rotation:
      overlap: 168h
```

`caBundle.file` 相对安装配置所在目录解析；也可与 `inlinePEM` 二选一。安装器拒绝过期、尚未生效、重复、非 CA、缺少签名约束或包含非证书数据的 Bundle，并分别申请 `serverAuth`/`clientAuth` 探测证书，确认两者均链到该 Bundle 后才继续。

`bootstrapTLSMode` 控制目标机第一次取得引导脚本的方式：`managed` 默认 `insecure-first-fetch`，用于目标系统尚不信任 Argus 自签名 CA 的情况；`existing-cluster-issuer` 默认 `strict`，适用于目标系统已经信任服务端证书链。客户私有 Issuer 是否已进入每台目标机的系统信任库无法由控制面可靠推断，因此允许管理员显式选择。两种模式不会在运行时互相回退：`strict` 校验失败即失败，`insecure-first-fetch` 也只影响第一个脚本请求。

argusctl 安装或复用锁定兼容版本的 cert-manager 与 trust-manager。trust-manager 把 `argus-trust-bundle` 只同步到带 Argus 标签的控制面命名空间。客户端只挂载公共 Bundle，不从任何服务端 TLS Secret 读取 `ca.crt`，也不会接触 CA 或其他服务私钥。

服务端叶证书默认 90 天并提前 15 天续期；Connector、Collector 和服务间客户端身份默认 24 小时并提前 8 小时轮换。
根轮换重叠期不得短于 32 小时：它高于外部客户端身份的最长 24 小时寿命并保留安全余量；生产默认仍为 168 小时。

## 4. 安装指令与 SSH

API 返回结构化 `InstallInstructionSet`，分别提供 Linux 系统、Linux 用户或 Kubernetes 作用域。每个作用域只返回一个 `command`，页面不再提供一行、交互式和自动化三种命令模式或模式 Tab。Host 与手工 Connector 的命令把带一次性令牌请求头的动态引导脚本下载到权限受限的临时文件，再执行该文件；Kubernetes 使用等价的单命令临时脚本执行。指令同时携带 `download_tls_mode`（仅动态下载入口）、Bundle epoch/hash、安装器 SHA-256 和能力警告。

短入口分别由 `/api/v1/connectors/bootstrap-script` 和 `/v1/host-bootstrap-script` 提供。令牌放在 `X-Argus-Enrollment-Token` 请求头而不是 URL；响应使用 `no-store`，下载动作进入审计，但不消费令牌。真正的 enrollment/卸载交换仍使用原有原子单次消费语义，因此网络重试不会提前耗尽授权。

当 `download_tls_mode=insecure-first-fetch` 时，只有上述第一个 `curl` 带 `--insecure`。这解决“目标机还不信任自签名 CA，因而无法先下载包含 CA 的脚本”的引导闭环，但意味着该次请求可能被中间人替换并执行；它是管理员明确选择的快速接入权衡，不等价于端到端可信引导。需要完整传输身份保证时应让目标机预置信任 CA，或使用受公开 CA 保护的入口并配置 `strict`。

安全引导顺序固定为：

1. 一行命令把动态引导脚本写入权限受限的临时文件；自签名快速模式只在这一步跳过证书校验，严格模式正常验证系统信任链。
2. 引导脚本在另一个权限受限的临时目录写入其内嵌的公共 CA Bundle。
3. 使用 `curl --cacert`、HTTPS-only 和 TLS 1.2+ 下载安装器，同时验证证书链、主机名和 SNI。
4. 校验固定的安装器 SHA-256，成功后才执行脚本。
5. 安装器使用同一 Bundle 获取冻结 Manifest、交换一次性令牌并下载 Artifact。
6. 二进制继续校验 SHA-256、大小和 Ed25519 签名。
7. 仅在上述校验完成后，系统模式才以 root 运行或请求 sudo；用户模式不提权。

禁止 `curl | bash`。没有 sudo 时系统模式明确失败并提示切换用户模式；用户模式使用 XDG 目录和 `systemd --user`，未启用 linger 时只保证登录会话存续。需要内核数据、系统目录或特殊 capability 的 Profile 不允许用户模式。

SSH 远程安装先严格验证主机密钥，再把安装计划冻结的版本化 Bundle 写入 `/etc/argus-connector/server-ca.pem`，复制已校验安装器和冻结 Manifest。Connector enrollment 必须显式传入该 CA 文件，不能回退系统根；目标不能直连 Argus 时使用受管隧道，TCP 可以拨到 loopback，但 HTTP Host、TLS SNI 与证书主机名始终保持原始 Argus 域名。

Windows Collector 当前仍是 `validation_pending`，不出现在正式 `InstallInstructionSet` 中。仓库内验证脚本同样要求 Bundle SHA/CA 约束及 Artifact SHA-256/Ed25519 全部成功后才检查管理员权限和写入 Windows Service，不能作为绕过 TLS 或供应链验证的旁路。

## 5. 运行时热加载

Go 服务和客户端统一使用 `internal/tlsmaterial`：

- 按投影目录内容哈希监控证书、私钥和 CA Bundle。
- 原子验证证书链、私钥匹配、预期 DNS/URI SAN、单一 EKU、有效期和 CA 约束。
- 服务端握手动态读取最新服务证书和客户端 CA；客户端的新连接/重连动态读取最新 CA 与客户端证书。
- 加载失败保留最后一个有效快照，同时增加失败指标并记录事件；首次没有有效快照时 fail closed。

OpenTelemetry Collector 扩展位于独立 Go module，不能反向导入 Argus 主 module；它在 `internal/otelcol/argusidentity` 内使用遵循同一约束的独立适配器：固定 TLS 1.3、显式 Bundle、证书/私钥匹配、单一 `clientAuth`、URI 身份和有效期校验。该适配器与主模块一起接受 TLS 安全门禁和独立 module 测试，不允许回退到容器系统根或跳过验证。

Collector 的身份扩展把只读 Kubernetes Secret 复制到 writable `emptyDir`，后续原子轮换。Gateway 通过固定资源名 RBAC 把最后有效身份镜像回 `argus-otelcol-identity`，重启时不会退回旧证书。无法原地重载的组件使用受控滚动重启。

## 6. 客户 Kubernetes 集群

Kubernetes 安装器先在本机严格下载并校验，再使用当前 kubeconfig 完成 mTLS 注册。一次性令牌不会写入 Manifest。安装器创建：

- `argus-trust-bundle` ConfigMap 与 Connector 身份 Secret；
- 独立 ServiceAccount、只读集群发现权限和固定资源名 Bundle/身份更新权限；
- `argus-telemetry` 专用命名空间、预建的 Collector 只读 ClusterRole，以及仅限该命名空间 Argus 资源的管理 Role；
- Connector Deployment、writable 身份卷与真实 TLS readiness probe。

目标集群不安装 trust-manager。Kubernetes Connector 只更新固定名称的 Argus Bundle/身份对象；运行时不创建任意 ClusterRole。Collector Agent/Gateway 缺少有效 Bundle 或真实 TLS/mTLS 探测失败时不 Ready。

`connector_image_pull_secrets` 和 Collector 的 `image_pull_secrets` 只引用客户已创建的 Secret。Argus 不修改节点 CA、不重启 containerd/docker，也不接管私有 registry TLS；默认公共镜像和客户提供的完整内部镜像引用均可使用。

## 7. 版本化 Bundle 与根 CA 轮换

数据库以 `epoch` 保存不可变 Bundle 版本、当前/下一任 CA 指纹、SHA-256、状态、`forward | rollback` 方向、开始/移除时间，以及每个 Connector、Collector、Kubernetes Connector 和控制面进程的确认结果。状态为：

```text
stable -> preparing -> overlapping -> retiring -> stable(new epoch)
              \-> failed
```

正常轮换顺序：

1. 验证下一任 Issuer、公共链以及 server/client 探测证书。
2. 发布旧 CA + 新 CA 的 Bundle，进入 `preparing`。
3. 为每张服务端证书预签下一任 CA 叶证书，但不覆盖在线 Secret；控制面以 Kubernetes 中当前 Running、Ready 且未进入终止流程的 Pod 精确集合为门禁，Connector/Collector 以最近在线窗口为门禁，全部 ACK 后才进入 `overlapping` 并启动默认 168 小时倒计时。滚动升级遗留的旧 Pod 和历史已知离线节点仍写入新 epoch 供追踪，但不阻塞切换。
4. 重叠期内，线上服务端始终继续呈现旧 CA 叶证书，同时接受旧、新客户端身份；Connector 会在长连接心跳中检查 24 小时身份的轮换窗口，Collector 也会定期检查并换发新身份。晚恢复节点因此仍能通过旧链连接、取得双 Bundle 并换证。
5. 到达退休时间后，控制器先验证目标 Issuer 和全部暂存叶证书，再把每张服务端 Certificate 指向稳态 Issuer，并以已验证的暂存 Secret 原子替换在线 Secret。确认所有控制面叶证书均链到新 CA 后才发布仅含新 CA 的 Bundle；未确认节点随后标记 `trust_expired`。

`existing-cluster-issuer` 根轮换要求客户先创建一个名称不同的下一任 `ClusterIssuer`，并用指向下一任 Issuer/Bundle 的配置执行 `pki rotate`。Argus 会同步更新运行时 Connector/Collector 身份签发配置、滚动重启相关签发服务，并在替换 Pod 再次确认双 Bundle 后才启动倒计时。轮换期间暂时同时使用当前和下一任 Issuer，完成后配置中的下一任成为唯一稳态 Issuer；直接在原名称下原地替换 CA 会被拒绝，因为那样无法保证旧服务端证书贯穿整个重叠期。轮换未结束时不要另行执行会覆盖 Certificate `issuerRef` 的 Helm 升级。

正向轮换成功后，后续 `plan`、升级和灾备恢复必须继续使用指向新 Issuer 与新 Bundle 的安装配置；安全回滚完成后则改回上一任 Issuer 与上一任 Bundle。运行中的重叠状态只保存在数据库和受控 Kubernetes 资源中，不会静默改写管理员提供的配置文件。

Trust source 同时持久化 Bundle epoch，managed 根 Secret 持久化 Issuer generation。后续 `argusctl install`/升级会把这两个在线值合并回 Helm runtime values，并在确认值一致后才强制接管轮换控制器更新过的 SSA 字段；升级不得把 epoch 或 Connector/Telemetry 签发代数重置为 1。

叶证书续期只替换 Secret/本地身份并热加载，不需要重新安装。按上述流程完成根 CA 轮换也不需要重装在线软件。只有离线时间超过重叠期的节点失去共同信任，此时运行：

```text
argusctl pki repair-command --node-kind connector --node-id <id> --scope linux-system
argusctl pki repair-command --node-kind collector --node-id <id> --scope kubernetes
```

修复命令嵌入当前公共 Bundle、验证 epoch/hash，并为相同逻辑节点签发一次性修复令牌和新身份；二进制不重装。Linux 用户安装选择 `linux-user`，Kubernetes 可通过 `--target-namespace` 指定 Connector 命名空间。

操作命令：

```text
argusctl pki status
argusctl pki rotate
argusctl pki extend --duration 168h
argusctl pki abort
argusctl pki repair-command --node-kind <kind> --node-id <id> --scope <scope>
```

`abort` 不会立即删除下一任 CA。它先把服务端叶证书和运行时签发器恢复到上一任 Issuer，再把同一个双信任 epoch 的方向改为 `rollback`，重新启动至少 32 小时（默认 168 小时）的安全重叠期。客户端在该窗口内换回上一任 CA；到期后控制器才移除被放弃的 CA。距离原退休时间不足 15 分钟时必须先 `extend`，避免与自动退休竞争。该流程可重复执行并从中断点恢复。平台管理页 `/pki` 提供只读的 Bundle、方向、指纹、阶段和节点 ACK 视图，避免浏览器直接获得证书管理权限。

## 8. 故障语义与运维影响

- 单个叶证书续期失败只影响对应服务或客户端，旧证书在有效期内继续使用，并触发告警。
- 无效的 Secret/ConfigMap 更新不会覆盖最后有效 TLS 快照。
- Bundle 发布错误在任何节点换证前可回滚；已经切换叶证书后需要同时恢复 Issuer、服务端证书和 Bundle。
- 根 CA 私钥泄露、错误删除旧 CA、错误替换全局 Issuer可能影响全部 Argus 链路，这是单 Issuer 的主要风险。
- 延长重叠期提高离线节点自动恢复概率，但延长旧 CA 的信任时间；默认到期自动退休可防止轮换永久悬挂。
- 客户直接替换 CA 而不走轮换状态机，会让所有仍只信任旧 CA 的客户端同时失联，属于不支持的操作。

因此，方便部署和更换 CA 的前提不是“把一张叶证书给所有服务共用”，而是“一个受控 Issuer + 一个版本化公共 Bundle + 多张用途隔离的叶证书 + 双信任轮换”。

## 9. 验证门禁

`go run ./cmd/argus-dev check tls-security` 扫描生产 Go、Shell、PowerShell、Helm、Docker 和 Web 源码，拒绝未限定范围的 TLS 跳过开关和未经校验的下载管道；唯一例外是结构化安装指令生成器中受 `bootstrapTLSMode` 控制的首次脚本请求。生产 Helm 渲染也执行同一检查。`argusctl verify` 在稳态要求全部在线叶证书引用唯一全局 Issuer，在重叠期只允许带完整 epoch/来源元数据且具有下一任就绪替代证书的 former Issuer。配置、证书热加载、安装脚本、Kubernetes RBAC、Bundle ACK/轮换、离线修复、中英文和深浅色页面由单元、集成、Playwright 与临时 namespace E2E 覆盖。
