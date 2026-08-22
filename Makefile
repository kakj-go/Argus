.PHONY: fmt test vet run-server contract-lint contract-generate contract-check contract-breaking query-parser-check query-promql-conformance query-kql-check query-skywalking-graphql-check query-tenant-schema-check check-production-artifacts migrate sqlc otelcol-linux-arm64 otelcol-windows-amd64 otelcol-distributions e2e-m2-k8s e2e-m3-k8s e2e-m4-k8s e2e-m5-k8s e2e-m6-k8s e2e-m7-k8s e2e-m8-k8s e2e-m10-query-k8s release-local

fmt:
	rg --files cmd internal -g '*.go' | xargs gofmt -w

test:
	go test ./...

vet:
	# Prometheus chunkenc.Iterator intentionally defines Seek(int64) ValueType;
	# disable stdmethods' io.Seeker signature heuristic for this interface.
	go vet -stdmethods=false ./...

run-server:
	go run ./cmd/argus-server

contract-lint:
	pnpm exec redocly lint api/openapi/argus.yaml api/openapi/generation/*.yaml
	go tool buf lint api/proto
	go test ./tests/contract -skip '^TestContractCompatibility$$'

contract-generate:
	rm -rf internal/gen/openapi web/packages/api-client/src/generated
	rm -f api/openapi/generated/*.bundle.yaml api/openapi/generated/*.bundle.json
	mkdir -p api/openapi/generated internal/gen/openapi web/packages/api-client/src/generated
	pnpm exec redocly bundle api/openapi/argus.yaml --output api/openapi/generated/argus.bundle.json --ext json
	node scripts/minify-json.mjs api/openapi/generated/argus.bundle.json
	@set -e; for domain in common identity authorization labels action card agent stream setup m8api platform enterpriseidentity enterpriseauthz machine audit secretapi hostapi kubernetesapi connectionapi actionapi connectorapi conversationapi modelapi workflowapi automationapi sandboxapi cardapi remoteaccessapi telemetryapi; do \
		pnpm exec redocly bundle api/openapi/generation/$$domain.yaml --output api/openapi/generated/$$domain.bundle.yaml --ext yaml; \
		mkdir -p internal/gen/openapi/$$domain; \
		go tool oapi-codegen -generate types,skip-prune -package $$domain -o internal/gen/openapi/$$domain/types.gen.go api/openapi/generated/$$domain.bundle.yaml; \
		node scripts/generate-openapi-types.mjs api/openapi/generated/$$domain.bundle.yaml web/packages/api-client/src/generated/$$domain.ts; \
	done
	@set -e; for domain in setup m8api platform enterpriseidentity enterpriseauthz machine audit secretapi hostapi kubernetesapi connectionapi actionapi connectorapi conversationapi modelapi workflowapi automationapi sandboxapi cardapi remoteaccessapi telemetryapi; do \
		go tool oapi-codegen -generate chi-server,strict-server -package $$domain -o internal/gen/openapi/$$domain/server.gen.go api/openapi/generated/$$domain.bundle.yaml; \
	done
	@set -e; for domain in enterpriseauthz secretapi hostapi kubernetesapi connectionapi actionapi connectorapi sandboxapi cardapi remoteaccessapi telemetryapi; do \
		node scripts/split-generated-go-server.mjs internal/gen/openapi/$$domain/server.gen.go; \
	done
	node scripts/generate-contract-index.mjs web/packages/api-client/src/generated/contracts.ts common identity authorization labels action card agent stream setup m8api platform enterpriseidentity enterpriseauthz machine audit secretapi hostapi kubernetesapi connectionapi actionapi connectorapi conversationapi modelapi workflowapi automationapi sandboxapi cardapi remoteaccessapi telemetryapi
	go tool buf generate api/proto --template api/proto/buf.gen.yaml

sqlc:
	go tool sqlc generate

migrate:
	go run ./cmd/argus-migrate up

otelcol-linux-arm64:
	rm -rf build/otelcol/dist/linux-arm64 build/otelcol/artifacts/argus-otelcol-linux-arm64.tar.gz
	mkdir -p build/otelcol/dist build/otelcol/artifacts
	go run go.opentelemetry.io/collector/cmd/builder@v0.133.0 --skip-compilation --config build/otelcol/builder-linux-arm64.yaml
	cd build/otelcol/dist/linux-arm64 && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o argus-otelcol .
	tar -czf build/otelcol/artifacts/argus-otelcol-linux-arm64.tar.gz -C build/otelcol/dist/linux-arm64 argus-otelcol

otelcol-windows-amd64:
	rm -rf build/otelcol/dist/windows-amd64 build/otelcol/artifacts/argus-otelcol-windows-amd64.zip
	mkdir -p build/otelcol/dist build/otelcol/artifacts
	go run go.opentelemetry.io/collector/cmd/builder@v0.133.0 --skip-compilation --config build/otelcol/builder-windows-amd64.yaml
	cd build/otelcol/dist/windows-amd64 && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o argus-otelcol.exe .
	cd build/otelcol/dist/windows-amd64 && zip -q ../../artifacts/argus-otelcol-windows-amd64.zip argus-otelcol.exe ../../install-windows.ps1

otelcol-distributions: otelcol-linux-arm64 otelcol-windows-amd64

e2e-m2-k8s:
	./scripts/e2e-m2-k8s.sh

e2e-m3-k8s:
	./scripts/e2e-m3-k8s.sh

e2e-m4-k8s:
	./scripts/e2e-m4-k8s.sh

e2e-m5-k8s:
	./scripts/e2e-m5-k8s.sh

e2e-m6-k8s:
	./scripts/e2e-m6-k8s.sh

e2e-m7-k8s:
	./scripts/e2e-m7-k8s.sh

e2e-m8-k8s:
	./scripts/e2e-m8-k8s.sh

e2e-m10-query-k8s:
	./scripts/e2e-m10-query-k8s.sh

release-local:
	./scripts/release-local.sh

contract-check: contract-lint
	./scripts/check-generated-contracts.sh

contract-breaking:
	go test ./tests/contract -run TestContractCompatibility
	@if git cat-file -e origin/main:api/proto/buf.yaml 2>/dev/null; then \
		go tool buf breaking api/proto --against '.git#branch=origin/main,subdir=api/proto'; \
	else \
		echo 'origin/main has no protobuf baseline; this merge establishes it'; \
	fi

query-parser-check:
	./scripts/check-query-parser-lock.sh

query-promql-conformance:
	go test ./internal/telemetry/queryengine/promql -count=1
	go test ./internal/telemetry -run 'TestPromQLClickHouse' -count=1

query-kql-check:
	go test ./internal/telemetry/queryengine/kql -count=1

query-skywalking-graphql-check:
	go test ./internal/telemetry/queryengine/skywalking -count=1

query-tenant-schema-check:
	go test ./internal/telemetry -run 'TestTenant|TestTelemetrySchemaV3' -count=1

check-production-artifacts:
	./scripts/check-production-artifacts.sh
