#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="argus-web:m1-smoke"
container="argus-web-m1-smoke-$$"
rendered="$(mktemp)"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker image rm -f "$image" >/dev/null 2>&1 || true
  rm -f "$rendered"
}
trap cleanup EXIT

cd "$root"
helm lint deploy/helm/argus-platform \
  --set-string runtime.postgresqlPassword=m1-smoke \
  --set-string runtime.redisPassword=m1-smoke \
  --set-string runtime.idempotencyEncryptionKey=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA \
  --set-string runtime.cursorSigningKey=BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB \
  --set-string runtime.pendingActionEncryptionKey=CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC \
  --set-json 'runtime.secretKEKKeyring={"current_version":1,"keys":{"1":"DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"}}'
helm template argus deploy/helm/argus-platform \
  --set profile=production \
  --set-string runtime.postgresqlPassword=m1-smoke \
  --set-string runtime.redisPassword=m1-smoke \
  --set-string runtime.idempotencyEncryptionKey=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA \
  --set-string runtime.cursorSigningKey=BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB \
  --set-string runtime.pendingActionEncryptionKey=CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC \
  --set-json 'runtime.secretKEKKeyring={"current_version":1,"keys":{"1":"DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"}}' >"$rendered"
rg -q 'host: cards\.argus\.example\.com' "$rendered"
rg -q 'name: cards' "$rendered"
rg -q 'containerPort: 8083' "$rendered"
rg -q 'ARGUS_DIRECT_EXECUTOR_CLIENT_NAMES: argus-server,argus-connector-gateway' "$rendered"
rg -q 'secretName: argus-server-direct-executor-client-tls' "$rendered"
rg -q 'secretName: argus-connector-gateway-direct-executor-client-tls' "$rendered"
rg -q 'name: argus-connector-gateway-remote-access' "$rendered"

docker build \
  --file deploy/docker/web.Dockerfile \
  --tag "$image" \
  --build-arg VITE_API_MODE=real \
  --build-arg VITE_API_BASE_URL=https://api.argus.invalid \
  --build-arg VITE_CARD_ORIGIN=https://cards.argus.invalid \
  --build-arg VITE_PLATFORM_URL=https://platform.argus.invalid \
  --build-arg VITE_DIRECT_EGRESS_ADDRESSES=198.51.100.10 \
  .

docker run --detach --name "$container" \
  --publish 127.0.0.1::8080 \
  --publish 127.0.0.1::8081 \
  --publish 127.0.0.1::8082 \
  --publish 127.0.0.1::8083 \
  "$image" >/dev/null

for port in 8080 8081 8082 8083; do
  host_port="$(docker port "$container" "$port/tcp" | awk -F: 'NR == 1 {print $NF}')"
  for _ in $(seq 1 30); do
    if curl --fail --silent "http://127.0.0.1:${host_port}/healthz" >/dev/null; then
      break
    fi
    sleep 1
  done
  curl --fail --silent "http://127.0.0.1:${host_port}/healthz" | rg -q '^ok$'
done

enterprise_port="$(docker port "$container" 8080/tcp | awk -F: 'NR == 1 {print $NF}')"
platform_port="$(docker port "$container" 8081/tcp | awk -F: 'NR == 1 {print $NF}')"
setup_port="$(docker port "$container" 8082/tcp | awk -F: 'NR == 1 {print $NF}')"
cards_port="$(docker port "$container" 8083/tcp | awk -F: 'NR == 1 {print $NF}')"

curl --fail --silent "http://127.0.0.1:${enterprise_port}/hosts/example" | rg -q '<div id="root"></div>'
curl --fail --silent "http://127.0.0.1:${platform_port}/enterprises/example" | rg -q '<div id="root"></div>'
curl --fail --silent "http://127.0.0.1:${setup_port}/setup/review" | rg -q '<div id="root"></div>'
curl --fail --silent "http://127.0.0.1:${cards_port}/runtime" | rg -q '<main id="card-root"></main>'

echo "Web image, Nginx deep links, and Helm card entrypoint smoke passed"
