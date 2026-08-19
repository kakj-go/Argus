#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PHASE=${ARGUS_E2E_PHASE:-m2}
KUBE_CONTEXT=${ARGUS_E2E_KUBE_CONTEXT:-$(kubectl config current-context)}
RUN_ID=${ARGUS_E2E_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}
RELEASE_ID="${PHASE}-${RUN_ID}"
SYSTEM_NS="argus-${PHASE}-system-${RUN_ID}"
SANDBOX_NS="argus-${PHASE}-sandbox-${RUN_ID}"
OBSERVABILITY_NS="argus-${PHASE}-observability-${RUN_ID}"
LEASE_NAME="argus-${PHASE}-e2e-lock"
BACKEND_IMAGE="argus/argus-backend:${PHASE}-${RUN_ID}"
WEB_IMAGE="argus/argus-web:${PHASE}-${RUN_ID}"
OTELCOL_IMAGE="argus/argus-otelcol:${PHASE}-${RUN_ID}"
API_PORT=${ARGUS_E2E_API_PORT:-4180}
ENTERPRISE_PORT=${ARGUS_E2E_ENTERPRISE_PORT:-4173}
PLATFORM_PORT=${ARGUS_E2E_PLATFORM_PORT:-4174}
SETUP_PORT=${ARGUS_E2E_SETUP_PORT:-4175}
CARD_PORT=${ARGUS_E2E_CARD_PORT:-4176}
REMOTE_PORT=${ARGUS_E2E_REMOTE_PORT:-4195}
CONNECTOR_GATEWAY_ADDRESS=${ARGUS_E2E_CONNECTOR_GATEWAY_ADDRESS:-grpcs://argus-connector-gateway.${SYSTEM_NS}.svc:9443}
API_URL="http://127.0.0.1:${API_PORT}/api/v1"
PLATFORM_ORIGIN="http://127.0.0.1:${PLATFORM_PORT}"
ENTERPRISE_ORIGIN="http://127.0.0.1:${ENTERPRISE_PORT}"
SETUP_ORIGIN="http://127.0.0.1:${SETUP_PORT}"
ARTIFACT_DIR="${ROOT_DIR}/artifacts/${PHASE}-e2e/${RUN_ID}"
WORK_DIR=$(mktemp -d)
PLATFORM_JAR="${WORK_DIR}/platform.cookies"
ENTERPRISE_JAR="${WORK_DIR}/enterprise.cookies"
OTHER_ENTERPRISE_JAR="${WORK_DIR}/other-enterprise.cookies"
PF_PIDS=()
API_PF_PID=""
WEB_PF_PID=""
LEASE_ACQUIRED=false
NAMESPACES_CREATED=false
RESPONSE_FILE=""
LAST_REQUEST_NAME="none"
M7_HELM_ARGS=()

mkdir -p "$ARTIFACT_DIR"

log() { printf '[%s-e2e] %s\n' "$PHASE" "$*"; }
fail() { printf '[%s-e2e] ERROR: %s\n' "$PHASE" "$*" >&2; exit 1; }

retry() {
  attempts=$1
  shift
  count=1
  until "$@"; do
    if [[ $count -ge $attempts ]]; then
      return 1
    fi
    log "command failed; retrying (${count}/${attempts})"
    count=$((count + 1))
    sleep $((count * 2))
  done
}

k() { kubectl --context "$KUBE_CONTEXT" "$@"; }

redact() {
  sed -E '/password|token|secret|csrf|authorization|cookie/Id'
}

diagnostics() {
  if [[ "$NAMESPACES_CREATED" != true ]]; then
    return
  fi
  {
    k -n "$SYSTEM_NS" get all,pvc,configmap,jobs -o wide || true
    k -n "$SYSTEM_NS" get events --sort-by=.lastTimestamp || true
  } 2>&1 | redact >"${ARTIFACT_DIR}/cluster.txt"
  k -n "$SYSTEM_NS" logs -l app.kubernetes.io/part-of=argus --all-containers=true --prefix=true --tail=1000 2>&1 \
    | redact >"${ARTIFACT_DIR}/argus.log" || true
  if [[ "$PHASE" == "m3" || "$PHASE" == "m4" || "$PHASE" == "m5" || "$PHASE" == "m6" || "$PHASE" == "m7" ]]; then
    k -n "$SYSTEM_NS" logs -l app.kubernetes.io/part-of=argus-m3-e2e --all-containers=true --prefix=true --tail=1000 2>&1 \
      | redact >"${ARTIFACT_DIR}/m3-workloads.log" || true
  fi
  if declare -F diagnostics_m4 >/dev/null; then
    diagnostics_m4
  fi
  if declare -F diagnostics_m5 >/dev/null; then
    diagnostics_m5
  fi
  if declare -F diagnostics_m6 >/dev/null; then diagnostics_m6; fi
  if declare -F diagnostics_m7 >/dev/null; then diagnostics_m7; fi
  k -n "$SYSTEM_NS" logs statefulset/argus-postgresql --tail=300 2>&1 \
    | redact >"${ARTIFACT_DIR}/postgresql.log" || true
  k -n "$SYSTEM_NS" logs statefulset/argus-redis --tail=300 2>&1 \
    | redact >"${ARTIFACT_DIR}/redis.log" || true
}

cleanup() {
  status=$?
  set +e
  diagnostics
  unset POSTGRES_PASSWORD REDIS_PASSWORD
  if declare -F cleanup_m3 >/dev/null; then
    cleanup_m3
  fi
  if declare -F cleanup_m4 >/dev/null; then
    cleanup_m4
  fi
  if declare -F cleanup_m5 >/dev/null; then
    cleanup_m5
  fi
  if declare -F cleanup_m6 >/dev/null; then cleanup_m6; fi
  if declare -F cleanup_m7 >/dev/null; then cleanup_m7; fi
  if declare -F cleanup_cert_manager_dependency >/dev/null; then
    cleanup_cert_manager_dependency
  fi
  for pid in "${PF_PIDS[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  done
  if [[ "$NAMESPACES_CREATED" == true ]]; then
    k delete namespace "$SYSTEM_NS" "$SANDBOX_NS" "$OBSERVABILITY_NS" --wait=false >/dev/null 2>&1 || true
    for ns in "$SYSTEM_NS" "$SANDBOX_NS" "$OBSERVABILITY_NS"; do
      k wait --for=delete "namespace/${ns}" --timeout=180s >/dev/null 2>&1 || true
    done
  fi
  if [[ "$LEASE_ACQUIRED" == true ]]; then
    k -n default delete lease "$LEASE_NAME" --ignore-not-found=true >/dev/null 2>&1 || true
  fi
  {
    for ns in "$SYSTEM_NS" "$SANDBOX_NS" "$OBSERVABILITY_NS"; do
      if k get namespace "$ns" >/dev/null 2>&1; then
        printf 'namespace|%s|residual\n' "$ns"
      else
        printf 'namespace|%s|deleted\n' "$ns"
      fi
    done
    if k get pvc -A -o custom-columns=NS:.metadata.namespace,NAME:.metadata.name --no-headers 2>/dev/null | rg -q "$RUN_ID"; then
      printf 'pvc|%s|residual\n' "$RUN_ID"
    else
      printf 'pvc|%s|deleted\n' "$RUN_ID"
    fi
    if k -n default get lease "$LEASE_NAME" >/dev/null 2>&1; then
      printf 'lease|%s|residual\n' "$LEASE_NAME"
    else
      printf 'lease|%s|deleted\n' "$LEASE_NAME"
    fi
  } >"${ARTIFACT_DIR}/cleanup.txt"
  if [[ "$KUBE_CONTEXT" == "docker-desktop" ]]; then
    node=$(k get nodes -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    if [[ -n "$node" ]]; then
      docker exec "$node" ctr -n k8s.io images remove "docker.io/${BACKEND_IMAGE}" "docker.io/${WEB_IMAGE}" "docker.io/${OTELCOL_IMAGE}" >/dev/null 2>&1 || true
    fi
  fi
  docker image rm "$BACKEND_IMAGE" "$WEB_IMAGE" "$OTELCOL_IMAGE" >/dev/null 2>&1 || true
  rm -rf "$WORK_DIR"
  if [[ $status -ne 0 ]]; then
    printf '[%s-e2e] failed after request %s; redacted diagnostics: %s\n' "$PHASE" "$LAST_REQUEST_NAME" "$ARTIFACT_DIR" >&2
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

require_commands() {
  for command in kubectl helm docker jq curl nc pnpm openssl go; do
    command -v "$command" >/dev/null 2>&1 || fail "required command is missing: ${command}"
  done
  k cluster-info >/dev/null
}

assert_port_free() {
  port=$1
  if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
    fail "port ${port} is already in use"
  fi
}

acquire_lease() {
  if k -n default get lease "$LEASE_NAME" >/dev/null 2>&1; then
    fail "another M2 Kubernetes E2E run holds ${LEASE_NAME}"
  fi
  cat <<EOF | k create -f - >/dev/null
apiVersion: coordination.k8s.io/v1
kind: Lease
metadata:
  name: ${LEASE_NAME}
  namespace: default
spec:
  holderIdentity: ${RUN_ID}
  leaseDurationSeconds: 7200
EOF
  LEASE_ACQUIRED=true
}

create_namespaces() {
  for ns in "$SYSTEM_NS" "$SANDBOX_NS" "$OBSERVABILITY_NS"; do
    k create namespace "$ns" >/dev/null
    k label namespace "$ns" "argus.io/release-id=${RELEASE_ID}" "argus.io/e2e=${PHASE}" >/dev/null
  done
  NAMESPACES_CREATED=true
}

build_images() {
  log "building backend image ${BACKEND_IMAGE}"
  backend_args=(--build-arg GO_BUILD_TAGS=)
  if [[ "$PHASE" == "m4" || "$PHASE" == "m5" || "$PHASE" == "m7" ]]; then
    backend_args=(--build-arg GO_BUILD_TAGS=m4e2e)
  fi
  retry 3 docker build --quiet -f deploy/docker/backend.Dockerfile -t "$BACKEND_IMAGE" "${backend_args[@]}" . >/dev/null
  log "building real-mode web image ${WEB_IMAGE}"
  retry 3 docker build --quiet -f deploy/docker/web.Dockerfile -t "$WEB_IMAGE" \
    --build-arg VITE_API_MODE=real \
    --build-arg "VITE_API_BASE_URL=http://127.0.0.1:${API_PORT}" \
    --build-arg "VITE_CARD_ORIGIN=http://127.0.0.1:${CARD_PORT}" \
    --build-arg "VITE_PLATFORM_URL=http://127.0.0.1:${PLATFORM_PORT}/login" \
    --build-arg "VITE_DIRECT_EGRESS_ADDRESSES=${ARGUS_E2E_DIRECT_EGRESS_DISPLAY:-127.0.0.1}" . >/dev/null
  if [[ "$PHASE" == "m7" ]]; then
    log "building locked Linux arm64 Collector distribution and image ${OTELCOL_IMAGE}"
    make otelcol-linux-arm64 >/dev/null
    retry 3 docker build --quiet --platform linux/arm64 -f deploy/docker/otelcol.Dockerfile -t "$OTELCOL_IMAGE" \
      --build-arg TARGETOS=linux --build-arg TARGETARCH=arm64 . >/dev/null
  fi
  images=("$BACKEND_IMAGE" "$WEB_IMAGE")
  if [[ "$PHASE" == "m7" ]]; then images+=("$OTELCOL_IMAGE"); fi
  case "$KUBE_CONTEXT" in
    kind-*) kind load docker-image --name "${KUBE_CONTEXT#kind-}" "${images[@]}" ;;
    minikube) minikube image load "${images[@]}" ;;
    docker-desktop)
      node=$(k get nodes -o jsonpath='{.items[0].metadata.name}')
      docker save "${images[@]}" | docker exec -i "$node" ctr -n k8s.io images import - >/dev/null
      ;;
  esac
}

install_dependencies() {
  POSTGRES_PASSWORD=$(openssl rand -base64 24 | tr -d '=+/\n' | cut -c1-24)
  REDIS_PASSWORD=$(openssl rand -base64 24 | tr -d '=+/\n' | cut -c1-24)
  SETUP_TOKEN=$(openssl rand -base64 32 | tr -d '=+/\n')
  IDEMPOTENCY_KEY=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')
  CURSOR_KEY=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')
  PENDING_ACTION_KEY=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')
  SECRET_KEK=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')
  SECRET_KEK_KEYRING=$(jq -nc --arg key "$SECRET_KEK" '{current_version:1,keys:{"1":$key}}')
  OBJECT_STORE_ACCESS=$(openssl rand -hex 12)
  OBJECT_STORE_SECRET=$(openssl rand -hex 24)
  SETUP_EXPIRES=$(date -u -v+24H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+24 hours' +%Y-%m-%dT%H:%M:%SZ)

  k -n "$SYSTEM_NS" create secret generic argus-data-credentials \
    --from-literal=postgresql-password="$POSTGRES_PASSWORD" \
    --from-literal=redis-password="$REDIS_PASSWORD" >/dev/null
  k -n "$SYSTEM_NS" create secret generic argus-generated-secrets \
    --from-literal=setup-token="$SETUP_TOKEN" \
    --from-literal=setup-token-expires-at="$SETUP_EXPIRES" >/dev/null

  cat <<EOF | k apply -f - >/dev/null
apiVersion: v1
kind: Service
metadata: {name: argus-postgresql, namespace: ${SYSTEM_NS}}
spec:
  selector: {app.kubernetes.io/name: argus-postgresql}
  ports: [{name: postgresql, port: 5432, targetPort: 5432}]
---
apiVersion: apps/v1
kind: StatefulSet
metadata: {name: argus-postgresql, namespace: ${SYSTEM_NS}}
spec:
  serviceName: argus-postgresql
  replicas: 1
  selector: {matchLabels: {app.kubernetes.io/name: argus-postgresql}}
  template:
    metadata: {labels: {app.kubernetes.io/name: argus-postgresql, app.kubernetes.io/part-of: argus}}
    spec:
      securityContext: {fsGroup: 999}
      containers:
        - name: postgresql
          image: postgres:18.6-alpine
          env:
            - {name: POSTGRES_DB, value: argus}
            - {name: POSTGRES_USER, value: argus}
            - name: POSTGRES_PASSWORD
              valueFrom: {secretKeyRef: {name: argus-data-credentials, key: postgresql-password}}
            - {name: PGDATA, value: /var/lib/postgresql/data/pgdata}
          ports: [{name: postgresql, containerPort: 5432}]
          readinessProbe: {exec: {command: [sh, -c, "pg_isready -U argus -d argus"]}, initialDelaySeconds: 2, periodSeconds: 2}
          resources: {requests: {cpu: 100m, memory: 256Mi}, limits: {cpu: "1", memory: 1Gi}}
          volumeMounts: [{name: data, mountPath: /var/lib/postgresql/data}]
  volumeClaimTemplates:
    - metadata: {name: data}
      spec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 1Gi}}}
---
apiVersion: v1
kind: Service
metadata: {name: argus-redis, namespace: ${SYSTEM_NS}}
spec:
  selector: {app.kubernetes.io/name: argus-redis}
  ports: [{name: redis, port: 6379, targetPort: 6379}]
---
apiVersion: apps/v1
kind: StatefulSet
metadata: {name: argus-redis, namespace: ${SYSTEM_NS}}
spec:
  serviceName: argus-redis
  replicas: 1
  selector: {matchLabels: {app.kubernetes.io/name: argus-redis}}
  template:
    metadata: {labels: {app.kubernetes.io/name: argus-redis, app.kubernetes.io/part-of: argus}}
    spec:
      securityContext: {fsGroup: 999}
      containers:
        - name: redis
          image: redis:8.10.0-alpine
          command: [sh, -c]
          args: ['exec redis-server --appendonly yes --requirepass "$REDIS_PASSWORD"']
          env:
            - name: REDIS_PASSWORD
              valueFrom: {secretKeyRef: {name: argus-data-credentials, key: redis-password}}
          ports: [{name: redis, containerPort: 6379}]
          readinessProbe: {exec: {command: [sh, -c, 'redis-cli -a "$REDIS_PASSWORD" ping | grep PONG']}, initialDelaySeconds: 2, periodSeconds: 2}
          resources: {requests: {cpu: 25m, memory: 64Mi}, limits: {cpu: 500m, memory: 256Mi}}
          volumeMounts: [{name: data, mountPath: /data}]
  volumeClaimTemplates:
    - metadata: {name: data}
      spec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 256Mi}}}
---
apiVersion: v1
kind: Service
metadata: {name: argus-minio, namespace: ${SYSTEM_NS}}
spec:
  selector: {app.kubernetes.io/name: argus-minio}
  ports: [{name: api, port: 9000, targetPort: 9000}]
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: data-argus-minio, namespace: ${SYSTEM_NS}}
spec:
  accessModes: [ReadWriteOnce]
  resources: {requests: {storage: 1Gi}}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: argus-minio, namespace: ${SYSTEM_NS}}
spec:
  replicas: 1
  selector: {matchLabels: {app.kubernetes.io/name: argus-minio}}
  template:
    metadata: {labels: {app.kubernetes.io/name: argus-minio, app.kubernetes.io/part-of: argus}}
    spec:
      containers:
        - name: minio
          image: minio/minio:RELEASE.2025-04-22T22-12-26Z
          args: [server, /data]
          env:
            - {name: MINIO_ROOT_USER, value: "${OBJECT_STORE_ACCESS}"}
            - {name: MINIO_ROOT_PASSWORD, value: "${OBJECT_STORE_SECRET}"}
          ports: [{name: api, containerPort: 9000}]
          readinessProbe: {httpGet: {path: /minio/health/ready, port: api}, initialDelaySeconds: 2, periodSeconds: 2}
          volumeMounts: [{name: data, mountPath: /data}]
      volumes: [{name: data, persistentVolumeClaim: {claimName: data-argus-minio}}]
---
apiVersion: batch/v1
kind: Job
metadata: {name: argus-minio-bucket, namespace: ${SYSTEM_NS}}
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: create-bucket
          image: minio/mc:RELEASE.2025-04-16T18-13-26Z
          command: [sh, -c]
          args: ['until mc alias set argus http://argus-minio:9000 "\$MINIO_ROOT_USER" "\$MINIO_ROOT_PASSWORD"; do sleep 2; done; mc mb --ignore-existing argus/argus-remote-recordings']
          env:
            - {name: MINIO_ROOT_USER, value: "${OBJECT_STORE_ACCESS}"}
            - {name: MINIO_ROOT_PASSWORD, value: "${OBJECT_STORE_SECRET}"}
EOF
  k -n "$SYSTEM_NS" rollout status statefulset/argus-postgresql --timeout=300s
  k -n "$SYSTEM_NS" rollout status statefulset/argus-redis --timeout=300s
  k -n "$SYSTEM_NS" rollout status deployment/argus-minio --timeout=300s
  k -n "$SYSTEM_NS" wait --for=condition=complete job/argus-minio-bucket --timeout=180s
}

install_argus() {
	local kubernetes_api_ip kubernetes_api_endpoint kubernetes_api_cidr kubernetes_api_endpoint_cidr telemetry_catalog_enabled
	telemetry_catalog_enabled=false
	if [[ "$PHASE" == "m7" ]]; then telemetry_catalog_enabled=true; fi
  kubernetes_api_ip=$(k -n default get service kubernetes -o jsonpath='{.spec.clusterIP}')
  kubernetes_api_endpoint=$(k -n default get endpoints kubernetes -o jsonpath='{.subsets[0].addresses[0].ip}')
  if [[ "$kubernetes_api_ip" == *:* ]]; then kubernetes_api_cidr="${kubernetes_api_ip}/128"; else kubernetes_api_cidr="${kubernetes_api_ip}/32"; fi
  if [[ "$kubernetes_api_endpoint" == *:* ]]; then kubernetes_api_endpoint_cidr="${kubernetes_api_endpoint}/128"; else kubernetes_api_endpoint_cidr="${kubernetes_api_endpoint}/32"; fi
  helm upgrade --install "${RELEASE_ID}-platform" deploy/helm/argus-platform \
    --kube-context "$KUBE_CONTEXT" --namespace "$SYSTEM_NS" --wait --wait-for-jobs --timeout 10m \
    --set-string releaseId="$RELEASE_ID" \
    --set-string namespaces.system="$SYSTEM_NS" \
    --set-string namespaces.sandbox="$SANDBOX_NS" \
    --set-string namespaces.observability="$OBSERVABILITY_NS" \
    --set-string images.backend="$BACKEND_IMAGE" \
    --set-string images.web="$WEB_IMAGE" \
    --set-string images.pullPolicy=Never \
    --set-string setupTokenSecretName=argus-generated-secrets \
    --set-string runtime.postgresqlPassword="$POSTGRES_PASSWORD" \
    --set-string runtime.redisPassword="$REDIS_PASSWORD" \
    --set-string runtime.idempotencyEncryptionKey="$IDEMPOTENCY_KEY" \
    --set-string runtime.cursorSigningKey="$CURSOR_KEY" \
    --set-string runtime.pendingActionEncryptionKey="$PENDING_ACTION_KEY" \
    --set-json runtime.secretKEKKeyring="$SECRET_KEK_KEYRING" \
    --set-string runtime.connectorEnrollmentURL="http://localhost:${API_PORT}" \
    --set-string runtime.connectorGatewayAddress="$CONNECTOR_GATEWAY_ADDRESS" \
    --set-json "runtime.kubernetesApiCidrs=[\"${kubernetes_api_cidr}\",\"${kubernetes_api_endpoint_cidr}\"]" \
    --set-json "runtime.allowedOrigins=[\"${ENTERPRISE_ORIGIN}\",\"${PLATFORM_ORIGIN}\",\"${SETUP_ORIGIN}\"]" \
    --set-string runtime.remoteOrigin="http://127.0.0.1:${REMOTE_PORT}" \
    --set-string runtime.objectStoreUrl="http://argus-minio.${SYSTEM_NS}.svc:9000" \
    --set-string runtime.objectStoreAccessKey="$OBJECT_STORE_ACCESS" \
    --set-string runtime.objectStoreSecretKey="$OBJECT_STORE_SECRET" \
    --set-string runtime.secureCookies=false \
	--set-string runtime.telemetryToolCatalogEnabled="$telemetry_catalog_enabled" \
    "${M7_HELM_ARGS[@]}" >/dev/null
  k -n "$SYSTEM_NS" get job argus-postgresql-migration -o jsonpath='{.status.succeeded}' | grep -q '^1$'
}

start_port_forwards() {
  start_api_port_forward
  kubectl --context "$KUBE_CONTEXT" -n "$SYSTEM_NS" port-forward service/argus-web \
    "${ENTERPRISE_PORT}:8080" "${PLATFORM_PORT}:8081" "${SETUP_PORT}:8082" "${CARD_PORT}:8083" \
    >"${ARTIFACT_DIR}/port-forward-web.log" 2>&1 &
  WEB_PF_PID=$!
  PF_PIDS+=("$WEB_PF_PID")
  start_remote_port_forward port-forward-remote.log
  for url in "http://127.0.0.1:${API_PORT}/readyz" "${ENTERPRISE_ORIGIN}/healthz" "${PLATFORM_ORIGIN}/healthz" "${SETUP_ORIGIN}/healthz" "http://127.0.0.1:${CARD_PORT}/healthz"; do
    for _ in $(seq 1 120); do
      curl --noproxy '*' --silent --fail --max-time 3 "$url" >/dev/null 2>&1 && break
      sleep 1
    done
    curl --noproxy '*' --silent --fail --max-time 3 "$url" >/dev/null || fail "endpoint did not become ready: ${url}"
  done
}

start_remote_port_forward() {
  local log_name=${1:-port-forward-remote.log}
  if [[ -n "${REMOTE_PF_PID:-}" ]]; then
    kill "$REMOTE_PF_PID" >/dev/null 2>&1 || true
    wait "$REMOTE_PF_PID" >/dev/null 2>&1 || true
    REMOTE_PF_PID=""
  fi
  assert_port_free "$REMOTE_PORT"
  kubectl --context "$KUBE_CONTEXT" -n "$SYSTEM_NS" port-forward service/argus-connector-gateway "${REMOTE_PORT}:9445" >"${ARTIFACT_DIR}/${log_name}" 2>&1 &
  REMOTE_PF_PID=$!
  PF_PIDS+=("$REMOTE_PF_PID")
  for _ in $(seq 1 30); do
    nc -z 127.0.0.1 "$REMOTE_PORT" >/dev/null 2>&1 && return
    sleep 1
  done
  fail "Remote Access WSS port-forward did not become ready"
}

start_api_port_forward() {
  kubectl --context "$KUBE_CONTEXT" -n "$SYSTEM_NS" port-forward service/argus-server "${API_PORT}:8080" >>"${ARTIFACT_DIR}/port-forward-api.log" 2>&1 &
  API_PF_PID=$!
  PF_PIDS+=("$API_PF_PID")
}

request() {
  local name=$1 expected=$2 method=$3 path=$4 jar=$5 body=$6 status
  local -a args
  shift 6
  LAST_REQUEST_NAME=$name
  RESPONSE_FILE="${WORK_DIR}/${name}.json"
  args=(--noproxy '*' --silent --show-error --connect-timeout 5 --max-time 30 --output "$RESPONSE_FILE" --write-out '%{http_code}' --request "$method")
  if [[ "$jar" != "-" ]]; then
    args+=(--cookie "$jar" --cookie-jar "$jar")
  fi
  if [[ "$body" != "-" ]]; then
    args+=(--header 'Content-Type: application/json' --data "$body")
  fi
  status=$(curl "${args[@]}" "$@" "${API_URL}${path}")
  if [[ "$status" != "$expected" ]]; then
    jq -c '{code: .code, message_key: .message_key}' "$RESPONSE_FILE" >&2 2>/dev/null || true
    if [[ -n "${ARTIFACT_DIR:-}" && -f "$RESPONSE_FILE" ]]; then
      jq 'del(.temporary_password, .password_change_challenge.temporary_password, .authenticated_session.csrf_token, .csrf_token)' "$RESPONSE_FILE" \
        >"${ARTIFACT_DIR}/${name}-response.json" 2>/dev/null || cp "$RESPONSE_FILE" "${ARTIFACT_DIR}/${name}-response.json"
    fi
    fail "${name}: expected HTTP ${expected}, got ${status}"
  fi
}

platform_login() {
  request platform-login 200 POST /platform/auth/login "$PLATFORM_JAR" \
    "$(jq -nc --arg username "$PLATFORM_USERNAME" --arg password "$PLATFORM_PASSWORD" '{username:$username,password:$password}')" \
    --header "Origin: ${PLATFORM_ORIGIN}"
  PLATFORM_CSRF=$(jq -er '.authenticated_session.csrf_token' "$RESPONSE_FILE")
}

enterprise_login() {
  request enterprise-login 200 POST /enterprise/auth/login "$ENTERPRISE_JAR" \
    "$(jq -nc --arg username "$ENTERPRISE_USERNAME" --arg password "$ENTERPRISE_PASSWORD" '{username:$username,password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"
  ENTERPRISE_CSRF=$(jq -er '.authenticated_session.csrf_token' "$RESPONSE_FILE")
}

run_api_flow() {
  PLATFORM_USERNAME=platform-admin
  PLATFORM_PASSWORD='N7!qP4@vL9#sT2$x'
  ENTERPRISE_USERNAME=enterprise-admin
  ENTERPRISE_PASSWORD='R8!kW3@zM6#pQ2$x'

  request setup-status 200 GET /setup/status - -
  jq -e '.state == "uninitialized"' "$RESPONSE_FILE" >/dev/null
  request setup-initialize 201 POST /setup/initialize - \
    "$(jq -nc --arg password "$PLATFORM_PASSWORD" '{platform_name:"Argus M2 E2E",default_locale:"zh-CN",timezone:"Asia/Shanghai",external_url:"http://127.0.0.1:4174",super_admin:{username:"platform-admin",display_name:"Platform Admin",email:"platform@example.test",password:$password}}')" \
    --header "Origin: ${SETUP_ORIGIN}" --header "X-Argus-Setup-Token: ${SETUP_TOKEN}" --header "Idempotency-Key: setup-${RUN_ID}"
  request setup-locked 200 GET /setup/status - -
  jq -e '.state == "initialized"' "$RESPONSE_FILE" >/dev/null

  platform_login
  request platform-session 200 GET /platform/auth/session "$PLATFORM_JAR" - --header "Origin: ${PLATFORM_ORIGIN}"
  jq -e '.session.audience == "platform" and (.csrf_token | length) >= 32' "$RESPONSE_FILE" >/dev/null
  request wrong-enterprise-audience 401 GET /enterprise/auth/session "$PLATFORM_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"

  request enterprise-create 201 POST /platform/enterprises "$PLATFORM_JAR" \
    '{"name":"Acme Evaluation","code":"acme-eval","timezone":"Asia/Shanghai","default_locale":"zh-CN"}' \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: enterprise-${RUN_ID}"
  ENTERPRISE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  ENTERPRISE_VERSION=$(jq -er '.version' "$RESPONSE_FILE")

  request admin-create 201 POST /platform/enterprise-admins "$PLATFORM_JAR" \
    "$(jq -nc --arg id "$ENTERPRISE_ID" --arg username "$ENTERPRISE_USERNAME" '{enterprise_id:$id,username:$username,display_name:"Enterprise Admin",email:"enterprise@example.test"}')" \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: admin-${RUN_ID}"
  TEMPORARY_PASSWORD=$(jq -er '.temporary_password' "$RESPONSE_FILE")
  ADMIN_USER_ID=$(jq -er '.user.id' "$RESPONSE_FILE")

  request enterprise-temp-login 200 POST /enterprise/auth/login "$ENTERPRISE_JAR" \
    "$(jq -nc --arg username "$ENTERPRISE_USERNAME" --arg password "$TEMPORARY_PASSWORD" '{username:$username,password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"
  CHALLENGE_ID=$(jq -er '.password_change_challenge.challenge_id' "$RESPONSE_FILE")
  request enterprise-password-change 200 POST /enterprise/auth/complete-password-change "$ENTERPRISE_JAR" \
    "$(jq -nc --arg challenge "$CHALLENGE_ID" --arg temporary "$TEMPORARY_PASSWORD" --arg password "$ENTERPRISE_PASSWORD" '{challenge_id:$challenge,temporary_password:$temporary,new_password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"
  ENTERPRISE_CSRF=$(jq -er '.csrf_token' "$RESPONSE_FILE")
  unset TEMPORARY_PASSWORD CHALLENGE_ID
  request wrong-platform-audience 401 GET /platform/auth/session "$ENTERPRISE_JAR" - --header "Origin: ${PLATFORM_ORIGIN}"

  request departments-initial 200 GET '/enterprise/departments?limit=1' "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  DEFAULT_DEPARTMENT_ID=$(jq -er '.items[0].id' "$RESPONSE_FILE")
  request department-create 201 POST /enterprise/departments "$ENTERPRISE_JAR" '{"name":"Operations","description":"M2 E2E"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: department-${RUN_ID}"
  request departments-cursor 200 GET '/enterprise/departments?limit=1' "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  OLD_CURSOR=$(jq -er '.page.next_cursor' "$RESPONSE_FILE")

  request scope-create 201 POST /enterprise/data-scopes "$ENTERPRISE_JAR" \
    '{"name":"Production hosts","resource_types":["host"],"explicit_resource_ids":["host-explicit"],"label_selector":{"schema_version":"argus.label_selector/v1","requirements":[{"key":"environment","operator":"eq","values":["prod"]}]}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: scope-${RUN_ID}"
  SCOPE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request role-create 201 POST /enterprise/roles "$ENTERPRISE_JAR" \
    '{"name":"M2 Machine Reader","permissions":["department.read","audit.read"]}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: role-${RUN_ID}"
  ROLE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  ROLE_VERSION=$(jq -er '.version' "$RESPONSE_FILE")

  request service-account-create 201 POST /enterprise/service-accounts "$ENTERPRISE_JAR" \
    "$(jq -nc --arg scope "$SCOPE_ID" '{name:"m2-automation",description:"M2 E2E",allowed_tool_ids:["inventory.read"],data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: service-account-${RUN_ID}"
  SERVICE_ACCOUNT_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request binding-create 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
    "$(jq -nc --arg subject "$SERVICE_ACCOUNT_ID" --arg role "$ROLE_ID" --arg scope "$SCOPE_ID" '{subject_type:"service_account",subject_id:$subject,role_id:$role,data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: binding-${RUN_ID}"

  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request stale-cursor 409 GET "/enterprise/departments?limit=1&cursor=$(jq -rn --arg v "$OLD_CURSOR" '$v|@uri')" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.code == "AUTHORIZATION_VERSION_STALE"' "$RESPONSE_FILE" >/dev/null

  API_KEY_IDEMPOTENCY="api-key-${RUN_ID}"
  request api-key-create 201 POST "/enterprise/service-accounts/${SERVICE_ACCOUNT_ID}/api-keys" "$ENTERPRISE_JAR" '{"name":"m2-key"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: ${API_KEY_IDEMPOTENCY}"
  API_KEY=$(jq -er '.secret' "$RESPONSE_FILE")
  API_KEY_ID=$(jq -er '.api_key.id' "$RESPONSE_FILE")
  request api-key-replay 201 POST "/enterprise/service-accounts/${SERVICE_ACCOUNT_ID}/api-keys" "$ENTERPRISE_JAR" '{"name":"m2-key"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: ${API_KEY_IDEMPOTENCY}"
  [[ "$(jq -r '.secret' "$RESPONSE_FILE")" == "$API_KEY" ]] || fail "API key idempotency did not replay the one-time secret"
  API_KEY_VERSION=$(jq -er '.api_key.version' "$RESPONSE_FILE")
  request api-key-auth 200 GET /enterprise/departments - - --header "Authorization: Bearer ${API_KEY}"
  request api-key-cookie-mix 403 GET /enterprise/departments "$ENTERPRISE_JAR" - --header "Authorization: Bearer ${API_KEY}"
  request api-key-rotate 200 POST "/enterprise/api-keys/${API_KEY_ID}/rotate?expected_version=${API_KEY_VERSION}" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: api-key-rotate-${RUN_ID}"
  ROTATED_API_KEY=$(jq -er '.secret' "$RESPONSE_FILE")
  ROTATED_API_KEY_ID=$(jq -er '.api_key.id' "$RESPONSE_FILE")
  ROTATED_API_KEY_VERSION=$(jq -er '.api_key.version' "$RESPONSE_FILE")
  request api-key-rotated-old-revoked 403 GET /enterprise/departments - - --header "Authorization: Bearer ${API_KEY}"
  request api-key-rotated-auth 200 GET /enterprise/departments - - --header "Authorization: Bearer ${ROTATED_API_KEY}"
  request api-key-revoke 204 POST "/enterprise/api-keys/${ROTATED_API_KEY_ID}/revoke?expected_version=${ROTATED_API_KEY_VERSION}" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  request api-key-revoked-auth 403 GET /enterprise/departments - - --header "Authorization: Bearer ${ROTATED_API_KEY}"
  request api-key-authz-create 201 POST "/enterprise/service-accounts/${SERVICE_ACCOUNT_ID}/api-keys" "$ENTERPRISE_JAR" '{"name":"authorization-version-check"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: api-key-authz-${RUN_ID}"
  API_KEY=$(jq -er '.secret' "$RESPONSE_FILE")

  request cross-enterprise-create 201 POST /platform/enterprises "$PLATFORM_JAR" \
    '{"name":"Other Evaluation","code":"other-eval","timezone":"UTC","default_locale":"en-US"}' \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: other-enterprise-${RUN_ID}"
  OTHER_ENTERPRISE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request cross-admin-create 201 POST /platform/enterprise-admins "$PLATFORM_JAR" \
    "$(jq -nc --arg id "$OTHER_ENTERPRISE_ID" '{enterprise_id:$id,username:"other-admin",display_name:"Other Admin"}')" \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: other-admin-${RUN_ID}"
  OTHER_USER_ID=$(jq -er '.user.id' "$RESPONSE_FILE")
  OTHER_TEMPORARY_PASSWORD=$(jq -er '.temporary_password' "$RESPONSE_FILE")
  OTHER_ENTERPRISE_PASSWORD='V9!mR4@kT7#pL2$x'
  request other-enterprise-temp-login 200 POST /enterprise/auth/login "$OTHER_ENTERPRISE_JAR" \
    "$(jq -nc --arg password "$OTHER_TEMPORARY_PASSWORD" '{username:"other-admin",password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"
  OTHER_CHALLENGE_ID=$(jq -er '.password_change_challenge.challenge_id' "$RESPONSE_FILE")
  request other-enterprise-password-change 200 POST /enterprise/auth/complete-password-change "$OTHER_ENTERPRISE_JAR" \
    "$(jq -nc --arg challenge "$OTHER_CHALLENGE_ID" --arg temporary "$OTHER_TEMPORARY_PASSWORD" --arg password "$OTHER_ENTERPRISE_PASSWORD" \
      '{challenge_id:$challenge,temporary_password:$temporary,new_password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"
  OTHER_ENTERPRISE_CSRF=$(jq -er '.csrf_token' "$RESPONSE_FILE")
  unset OTHER_TEMPORARY_PASSWORD OTHER_CHALLENGE_ID
  request cross-enterprise-id 404 GET "/enterprise/users/${OTHER_USER_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request nonexistent-id 404 GET /enterprise/users/00000000-0000-0000-0000-000000000001 "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"

  request role-update 200 PUT "/enterprise/roles/${ROLE_ID}" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$ROLE_VERSION" '{description:"version invalidation",permissions:["department.read","audit.read"],expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  request stale-api-key 403 GET /enterprise/departments - - --header "Authorization: Bearer ${API_KEY}"
  jq -e '.code == "AUTHORIZATION_VERSION_STALE"' "$RESPONSE_FILE" >/dev/null
  unset API_KEY

  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request suspend-api-key-create 201 POST "/enterprise/service-accounts/${SERVICE_ACCOUNT_ID}/api-keys" "$ENTERPRISE_JAR" '{"name":"suspension-check"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: suspend-key-${RUN_ID}"
  SUSPEND_API_KEY=$(jq -er '.secret' "$RESPONSE_FILE")
  request enterprise-audit 200 GET /enterprise/audit-events "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e --arg id "$ENTERPRISE_ID" 'all(.items[]; .domain == "enterprise" and .enterprise_id == $id)' "$RESPONSE_FILE" >/dev/null
  if grep -Eiq 'password|setup.token|csrf|api.key.secret|temporary.credential' "$RESPONSE_FILE"; then
    fail "enterprise audit response contains a sensitive field"
  fi
  request platform-audit 200 GET /platform/audit-events "$PLATFORM_JAR" - --header "Origin: ${PLATFORM_ORIGIN}"
  jq -e 'all(.items[]; .domain == "platform" and (.enterprise_id == null))' "$RESPONSE_FILE" >/dev/null

  run_real_playwright

  if declare -F run_m3_api_flow >/dev/null; then
    run_m3_api_flow
  fi
  if declare -F run_m4_api_flow >/dev/null; then
    run_m4_api_flow
  fi
  if declare -F run_m5_api_flow >/dev/null; then
    run_m5_api_flow
  fi
  if declare -F run_m6_api_flow >/dev/null; then run_m6_api_flow; fi
  if declare -F run_m7_api_flow >/dev/null; then run_m7_api_flow; fi

  log "stopping Redis to verify PostgreSQL authority"
  k -n "$SYSTEM_NS" scale statefulset/argus-redis --replicas=0 >/dev/null
  k -n "$SYSTEM_NS" wait --for=delete pod/argus-redis-0 --timeout=120s >/dev/null
  request redis-existing-session 200 GET /enterprise/auth/session "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request redis-login-fails-closed 503 POST /enterprise/auth/login - \
    "$(jq -nc --arg username "$ENTERPRISE_USERNAME" --arg password "$ENTERPRISE_PASSWORD" '{username:$username,password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"

  log "restarting argus-server while Redis is unavailable"
  k -n "$SYSTEM_NS" rollout restart deployment/argus-server >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-server --timeout=300s >/dev/null
  kill "$API_PF_PID" >/dev/null 2>&1 || true
  wait "$API_PF_PID" >/dev/null 2>&1 || true
  start_api_port_forward
  for _ in $(seq 1 60); do curl --noproxy '*' --silent --fail --max-time 3 "http://127.0.0.1:${API_PORT}/readyz" >/dev/null 2>&1 && break; sleep 1; done
  curl --noproxy '*' --silent --fail --max-time 3 "http://127.0.0.1:${API_PORT}/readyz" >/dev/null || fail "API port-forward did not recover in Redis-degraded mode"
  request degraded-restart-session 200 GET /enterprise/auth/session "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request degraded-restart-audit 200 GET /enterprise/audit-events "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request degraded-restart-login-fails-closed 503 POST /enterprise/auth/login - \
    "$(jq -nc --arg username "$ENTERPRISE_USERNAME" --arg password "$ENTERPRISE_PASSWORD" '{username:$username,password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"

  k -n "$SYSTEM_NS" scale statefulset/argus-redis --replicas=1 >/dev/null
  k -n "$SYSTEM_NS" rollout status statefulset/argus-redis --timeout=300s >/dev/null
  for _ in $(seq 1 60); do
    if curl --noproxy '*' --silent --fail --max-time 3 "http://127.0.0.1:${API_PORT}/readyz" | jq -e '.dependencies.redis == "ready"' >/dev/null 2>&1; then break; fi
    sleep 1
  done
  curl --noproxy '*' --silent --fail --max-time 3 "http://127.0.0.1:${API_PORT}/readyz" | jq -e '.dependencies.redis == "ready"' >/dev/null || fail "Redis did not recover in readiness state"
  request restart-session 200 GET /enterprise/auth/session "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request restart-audit 200 GET /enterprise/audit-events "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"

  request enterprise-suspend 200 POST "/platform/enterprises/${ENTERPRISE_ID}/suspend?expected_version=${ENTERPRISE_VERSION}" "$PLATFORM_JAR" - \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}"
  request revoked-session 401 GET /enterprise/auth/session "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.code == "ENTERPRISE_SUSPENDED" or .code == "SESSION_REVOKED"' "$RESPONSE_FILE" >/dev/null
  request disabled-api-key 403 GET /enterprise/departments - - --header "Authorization: Bearer ${SUSPEND_API_KEY}"

  unset PLATFORM_PASSWORD ENTERPRISE_PASSWORD OTHER_ENTERPRISE_PASSWORD PLATFORM_CSRF ENTERPRISE_CSRF OTHER_ENTERPRISE_CSRF SUSPEND_API_KEY SETUP_TOKEN IDEMPOTENCY_KEY CURSOR_KEY PENDING_ACTION_KEY SECRET_KEK SECRET_KEK_KEYRING
}

run_real_playwright() {
  log "running Playwright against four real origins"
  ARGUS_E2E_EXTERNAL=1 ARGUS_M2_E2E=1 \
    ARGUS_M2_PLATFORM_USERNAME="$PLATFORM_USERNAME" ARGUS_M2_PLATFORM_PASSWORD="$PLATFORM_PASSWORD" \
    ARGUS_M2_ENTERPRISE_USERNAME="$ENTERPRISE_USERNAME" ARGUS_M2_ENTERPRISE_PASSWORD="$ENTERPRISE_PASSWORD" \
    ARGUS_E2E_ARTIFACTS="$ARTIFACT_DIR/playwright" \
    pnpm --filter @argus/enterprise exec playwright test e2e/m2-real.spec.ts --workers=1
}

main() {
  cd "$ROOT_DIR"
  require_commands
  for port in "$API_PORT" "$ENTERPRISE_PORT" "$PLATFORM_PORT" "$SETUP_PORT" "$CARD_PORT" "$REMOTE_PORT"; do assert_port_free "$port"; done
  acquire_lease
  create_namespaces
  build_images
  install_dependencies
  if declare -F prepare_cert_manager_dependency >/dev/null; then
    prepare_cert_manager_dependency
  fi
  if declare -F prepare_m3_dependencies >/dev/null; then
    prepare_m3_dependencies
  fi
  if declare -F prepare_m4_dependencies >/dev/null; then
    prepare_m4_dependencies
  fi
  if declare -F prepare_m5_dependencies >/dev/null; then
    prepare_m5_dependencies
  fi
  if declare -F prepare_m6_dependencies >/dev/null; then prepare_m6_dependencies; fi
  if declare -F prepare_m7_dependencies >/dev/null; then prepare_m7_dependencies; fi
  install_argus
  start_port_forwards
  run_api_flow
  phase_label=$(printf '%s' "$PHASE" | tr '[:lower:]' '[:upper:]')
  log "${phase_label} Kubernetes E2E passed; diagnostics: ${ARTIFACT_DIR}"
}

if [[ "$PHASE" == "m3" || "$PHASE" == "m4" || "$PHASE" == "m5" || "$PHASE" == "m6" || "$PHASE" == "m7" ]]; then
  source "${ROOT_DIR}/scripts/e2e-cert-manager.sh"
fi
if [[ "$PHASE" == "m3" || "$PHASE" == "m5" || "$PHASE" == "m6" || "$PHASE" == "m7" ]]; then
  source "${ROOT_DIR}/scripts/e2e-m3-flow.sh"
fi
if [[ "$PHASE" == "m4" || "$PHASE" == "m5" || "$PHASE" == "m7" ]]; then
  source "${ROOT_DIR}/scripts/e2e-m4-flow.sh"
fi
if [[ "$PHASE" == "m5" || "$PHASE" == "m7" ]]; then
  source "${ROOT_DIR}/scripts/e2e-m5-flow.sh"
fi
if [[ "$PHASE" == "m6" ]]; then source "${ROOT_DIR}/scripts/e2e-m6-flow.sh"; fi
if [[ "$PHASE" == "m7" ]]; then source "${ROOT_DIR}/scripts/e2e-m7-flow.sh"; fi

main "$@"
