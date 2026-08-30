.PHONY: fmt test vet run-server contract-lint contract-generate contract-check contract-breaking query-parser-check query-promql-conformance query-kql-check query-skywalking-graphql-check query-tenant-schema-check check-production-artifacts migrate sqlc otelcol-linux-arm64 otelcol-windows-amd64 otelcol-distributions otelcol-image-publish otelcol-artifacts-publish e2e-m2-k8s e2e-m3-k8s e2e-m4-k8s e2e-m5-k8s e2e-m6-k8s e2e-m7-k8s e2e-m8-k8s e2e-m10-query-k8s release-local dev-upgrade

# 本地迭代一键构建升级：images build -> images load -> 滚动重启全部 Argus 工作负载。
# 与 argusctl install 的 restartLocalRegistryWorkloads 保持同一份 Deployment 清单
# （evaluation profile）；改动集群或命名空间时用变量覆盖。
DEV_CONFIG ?= deploy/.cache/argus-install-dev.yaml
DEV_SYSTEM_NAMESPACE ?= argus-system
DEV_OBSERVABILITY_NAMESPACE ?= argus-observability
DEV_PLATFORM_DEPLOYMENTS ?= argus-web argus-server argus-worker argus-direct-executor argus-connector-gateway
DEV_TELEMETRY_DEPLOYMENTS ?= argus-telemetry-ingest argus-telemetry-writer argus-telemetry-query
KUBECTL ?= kubectl

dev-upgrade:
	@test -f $(DEV_CONFIG) || { echo "install config $(DEV_CONFIG) not found; generate it before running dev-upgrade"; exit 1; }
	go run ./cmd/argusctl images build --config $(DEV_CONFIG) --platform linux/arm64
	go run ./cmd/argusctl images load --config $(DEV_CONFIG)
	$(KUBECTL) -n $(DEV_SYSTEM_NAMESPACE) rollout restart deployment $(DEV_PLATFORM_DEPLOYMENTS)
	$(KUBECTL) -n $(DEV_OBSERVABILITY_NAMESPACE) rollout restart deployment $(DEV_TELEMETRY_DEPLOYMENTS)
	@for d in $(DEV_PLATFORM_DEPLOYMENTS); do $(KUBECTL) -n $(DEV_SYSTEM_NAMESPACE) rollout status deployment/$$d --timeout=180s || exit 1; done
	@for d in $(DEV_TELEMETRY_DEPLOYMENTS); do $(KUBECTL) -n $(DEV_OBSERVABILITY_NAMESPACE) rollout status deployment/$$d --timeout=180s || exit 1; done
	@echo "dev upgrade complete: images rebuilt, loaded, and all Argus workloads rolled out"

fmt:
	go run ./cmd/argus-dev repo fmt

test:
	go run ./cmd/argus-dev repo test

vet:
	go run ./cmd/argus-dev repo vet

run-server:
	go run ./cmd/argus-dev repo run-server

contract-lint:
	go run ./cmd/argus-dev contracts lint

contract-generate:
	go run ./cmd/argus-dev contracts generate

sqlc:
	go run ./cmd/argus-dev repo sqlc

migrate:
	go run ./cmd/argus-dev repo migrate

otelcol-linux-arm64:
	go run ./cmd/argus-dev collector build linux-arm64

otelcol-linux-amd64:
	go run ./cmd/argus-dev collector build linux-amd64

otelcol-windows-amd64:
	go run ./cmd/argus-dev collector build windows-amd64

otelcol-distributions:
	go run ./cmd/argus-dev collector build all

otelcol-image-publish:
	go run ./cmd/argus-dev collector publish-image --push

otelcol-artifacts-publish:
	go run ./cmd/argus-dev collector publish-artifacts

e2e-m2-k8s:
	go run ./cmd/argus-dev e2e run --suite m2

e2e-m3-k8s:
	go run ./cmd/argus-dev e2e run --suite m3

e2e-m4-k8s:
	go run ./cmd/argus-dev e2e run --suite m4

e2e-m5-k8s:
	go run ./cmd/argus-dev e2e run --suite m5

e2e-m6-k8s:
	go run ./cmd/argus-dev e2e run --suite m6

e2e-m7-k8s:
	go run ./cmd/argus-dev e2e run --suite m7

e2e-m8-k8s:
	go run ./cmd/argus-dev e2e run --suite m8

e2e-m10-query-k8s:
	go run ./cmd/argus-dev e2e run --suite m10-query

release-local:
	go run ./cmd/argus-dev release local

contract-check:
	go run ./cmd/argus-dev contracts check

contract-breaking:
	go run ./cmd/argus-dev contracts breaking

query-parser-check:
	go run ./cmd/argus-dev check query-parsers

query-promql-conformance:
	go run ./cmd/argus-dev query promql

query-kql-check:
	go run ./cmd/argus-dev query kql

query-skywalking-graphql-check:
	go run ./cmd/argus-dev query skywalking

query-tenant-schema-check:
	go run ./cmd/argus-dev query tenant-schema

check-production-artifacts:
	go run ./cmd/argus-dev check production-artifacts
