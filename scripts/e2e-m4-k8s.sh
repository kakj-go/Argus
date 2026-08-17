#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ARGUS_E2E_PHASE=m4 exec "${ROOT_DIR}/scripts/e2e-m2-k8s.sh" "$@"
