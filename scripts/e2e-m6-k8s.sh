#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export ARGUS_E2E_PHASE=m6
export ARGUS_E2E_CONNECTOR_GATEWAY_ADDRESS="grpcs://localhost:${ARGUS_E2E_CONNECTOR_GATEWAY_PORT:-4193}"
exec "${ROOT_DIR}/scripts/e2e-m2-k8s.sh" "$@"
