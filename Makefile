.PHONY: fmt test vet run-server contract-lint contract-generate contract-check contract-breaking contract-update-legacy-baseline

fmt:
	rg --files cmd internal -g '*.go' | xargs gofmt -w

test:
	go test ./...

vet:
	go vet ./...

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
	@set -e; for domain in common identity authorization labels action card agent stream telemetry; do \
		pnpm exec redocly bundle api/openapi/generation/$$domain.yaml --output api/openapi/generated/$$domain.bundle.yaml --ext yaml; \
		mkdir -p internal/gen/openapi/$$domain; \
		go tool oapi-codegen -generate types,skip-prune -package $$domain -o internal/gen/openapi/$$domain/types.gen.go api/openapi/generated/$$domain.bundle.yaml; \
		node scripts/generate-openapi-types.mjs api/openapi/generated/$$domain.bundle.yaml web/packages/api-client/src/generated/$$domain.ts; \
	done
	node scripts/generate-contract-index.mjs web/packages/api-client/src/generated/contracts.ts common identity authorization labels action card agent stream telemetry
	go tool buf generate api/proto --template api/proto/buf.gen.yaml

contract-check: contract-lint
	./scripts/check-generated-contracts.sh

contract-breaking:
	go test ./tests/contract -run TestContractCompatibility
	@if git cat-file -e origin/main:api/proto/buf.yaml 2>/dev/null; then \
		go tool buf breaking api/proto --against '.git#branch=origin/main,subdir=api/proto'; \
	else \
		echo 'origin/main has no protobuf baseline; this merge establishes it'; \
	fi

contract-update-legacy-baseline:
	node scripts/update-legacy-web-allowlist.mjs
