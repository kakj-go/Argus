#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${ROOT_DIR}"

make query-promql-conformance
make query-kql-check
make query-skywalking-graphql-check
make query-tenant-schema-check
go test ./internal/transport/httpapi ./internal/telemetry ./tests/contract

if [[ "${ARGUS_E2E_M10_UNIT_ONLY:-0}" == "1" ]]; then
  exit 0
fi

# M10 uses the existing disposable Kubernetes harness for infrastructure
# lifecycle and overlays the single-process query protocol checks above.
export ARGUS_E2E_PHASE=m10-query
export ARGUS_E2E_CONNECTOR_GATEWAY_ADDRESS="grpcs://localhost:${ARGUS_E2E_CONNECTOR_GATEWAY_PORT:-4193}"
exec "${ROOT_DIR}/scripts/e2e-m2-k8s.sh" "$@"
