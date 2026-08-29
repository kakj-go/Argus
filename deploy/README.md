# Argus Kubernetes Deployment

`deploy/` contains the install contract, locked dependency versions, image builds and six Helm releases used by `argusctl`.

## Layout

```text
deploy/
├── docker/                 backend, web and patched MinIO images
├── helm/                   six Argus-owned Helm charts
├── profiles/               Evaluation and Production install configs
├── schemas/                ArgusInstallConfig v1alpha1 JSON Schema
└── versions.lock.yaml      tested build and middleware versions
```

The release order is:

```text
argus-foundation
argus-data-operators
argus-data
argus-sandbox
argus-platform
argus-telemetry-pipeline
```

Strimzi, Altinity and OpenSandbox upstream charts are installed by `argusctl` between the Argus-owned releases. Stage state is stored in `<release-id>-install-status` in the system namespace.

## Evaluation

Evaluation targets a disposable single-node cluster. It deploys one replica of every Argus runtime role, PostgreSQL, Redis, MinIO, Strimzi/Kafka, Altinity/ClickHouse, Keeper, OpenSandbox and the OTel ClickHouse writer. The three web applications continue to use the built-in mock API; the backend deployment validates process roles, configuration, health and lifecycle, not completed domain APIs.

Create a run-specific config from `profiles/evaluation.yaml`; do not reuse the default namespace names for concurrent runs. A normal local flow is:

```bash
go run ./cmd/argusctl preflight --config deploy/.cache/evaluation-<run-id>.yaml
go run ./cmd/argusctl plan --config deploy/.cache/evaluation-<run-id>.yaml
go run ./cmd/argusctl images build --config deploy/.cache/evaluation-<run-id>.yaml --platform linux/arm64
go run ./cmd/argusctl images load --config deploy/.cache/evaluation-<run-id>.yaml
go run ./cmd/argusctl install --config deploy/.cache/evaluation-<run-id>.yaml
go run ./cmd/argusctl status --config deploy/.cache/evaluation-<run-id>.yaml --output json
go run ./cmd/argusctl verify --config deploy/.cache/evaluation-<run-id>.yaml --output json
```

`images build` starts a run-owned local registry container. `images load` uses a privileged, run-labelled DaemonSet to import the exact images into the node containerd `k8s.io` namespace. Evaluation workloads use `imagePullPolicy: Never`.

For iterative local development after the first install, `make dev-upgrade` chains the light-weight update path in one step: `images build` → `images load` → rollout restart of every Argus deployment → wait for readiness. It skips the full `install` flow (Helm stages, secrets, migrations). The install config defaults to `deploy/.cache/argus-install-dev.yaml`; override with `DEV_CONFIG=...` (also `DEV_SYSTEM_NAMESPACE`, `DEV_OBSERVABILITY_NAMESPACE`, `KUBECTL` for another kube context).

Portals are exposed through the ingress with mandatory TLS; map the install-config hosts to the ingress load-balancer address (for example in `/etc/hosts` on Docker Desktop):

- Enterprise (terminal WSS is same-origin: `wss://argus.dev/v1/sessions`): `https://argus.dev`
- Platform and first-time setup: `https://platform.argus.dev`
- Card Runtime (internal): `https://cards.argus.dev`
- Connector mTLS: `grpcs://connector.argus.dev:9443` (dedicated LoadBalancer service)

With `tls.mode: cert-manager-selfsigned`, trust the generated CA (available in secret `argus-web-tls`, key `ca.crt`; one multi-SAN certificate covers all three hosts) before first browser access.

首次安装成功时，`argusctl install` 会在最终摘要中只显示一次包含 Setup Token Fragment 的 Platform 初始化链接。初始化者直接打开该链接，无需手工输入 Token；Platform 会立即从地址栏移除 Fragment，Token 只在当前页面内存中保留。链接遗失或过期时，在系统仍未初始化的前提下运行：

```bash
go run ./cmd/argusctl setup-token rotate --config deploy/.cache/evaluation-<run-id>.yaml
```

## Production Profile

`profiles/production.yaml` renders HA replicas, PDB, HPA, topology spread, two portal hosts and the isolated Card Runtime host. Production installation is intentionally blocked with:

- `POSTGRES_HA_ADR_REQUIRED`
- `SANDBOX_RUNTIME_ADR_REQUIRED`

The profile is available for schema validation, linting and rendering only. It must not be described as production-ready until both ADRs are resolved and the resulting topology passes HA, backup/restore and hardened sandbox validation.

## Cleanup

Evaluation cleanup is destructive and requires explicit confirmation:

```bash
go run ./cmd/argusctl uninstall \
  --config deploy/.cache/evaluation-<run-id>.yaml \
  --delete-data \
  --delete-owned-crds \
  --yes
```

Before removal, diagnostics are written to `artifacts/k8s-e2e/<release-id>/uninstall/`. Cleanup removes the run namespaces, run-owned releases and CRDs, loader DaemonSet, imported Argus images and local registry container. Production defaults retain data and shared cluster resources.

See [service and Kubernetes design](../docs/10-service-components-and-kubernetes-deployment.md), [current implementation status](../docs/13-current-implementation-and-kubernetes-rollout.md), and [PostgreSQL deployment decision](../docs/14-postgresql-deployment-decision.md).
