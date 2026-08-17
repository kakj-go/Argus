#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
IMAGE=${ARGUS_PRODUCTION_IMAGE:-argus/argus-backend:production-scan}
WORK_DIR=$(mktemp -d)
BUILT_IMAGE=false

cleanup() {
  status=$?
  if [[ -n "${CONTAINER_ID:-}" ]]; then
    docker rm "$CONTAINER_ID" >/dev/null 2>&1 || true
  fi
  if [[ "$BUILT_IMAGE" == true ]]; then
    docker image rm "$IMAGE" >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK_DIR"
  exit "$status"
}
trap cleanup EXIT

cd "$ROOT_DIR"
if [[ -z "${ARGUS_PRODUCTION_IMAGE:-}" ]]; then
  docker build --quiet -f deploy/docker/backend.Dockerfile -t "$IMAGE" . >/dev/null
  BUILT_IMAGE=true
fi

CONTAINER_ID=$(docker create "$IMAGE")
docker export "$CONTAINER_ID" >"${WORK_DIR}/rootfs.tar"
tar -tf "${WORK_DIR}/rootfs.tar" >"${WORK_DIR}/files.txt"

if rg -i '(^|/)(argus-replay-model|mock-seed)(/|$)' "${WORK_DIR}/files.txt"; then
  echo "production image contains an M4 replay or mock artifact" >&2
  exit 1
fi

mkdir "${WORK_DIR}/rootfs"
tar -xf "${WORK_DIR}/rootfs.tar" -C "${WORK_DIR}/rootfs"
for binary in "${WORK_DIR}/rootfs"/usr/local/bin/*; do
  if strings "$binary" | rg -i 'argus M4 replay model|ARGUS_REPLAY_|ARGUS_ALLOW_PRIVATE_MODEL|quota-exceeded'; then
    echo "production binary contains an E2E replay/private-endpoint marker: ${binary##*/}" >&2
    exit 1
  fi
done

if rg -n 'GO_BUILD_TAGS=.*m4e2e|tags=m4e2e' deploy/docker/backend.Dockerfile; then
  echo "production Dockerfile enables the M4 E2E build tag" >&2
  exit 1
fi

echo "Production backend image contains no Replay Provider, mock seed, or private-endpoint switch"
