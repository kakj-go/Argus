#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

RUN_ID=${ARGUS_E2E_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}
ARTIFACT_DIR="${ROOT_DIR}/artifacts/m8-e2e/${RUN_ID}"
SOURCE_CONFIG="${ARTIFACT_DIR}/source.yaml"
RESTORE_CONFIG="${ARTIFACT_DIR}/restore.yaml"
WORK_DIR=$(mktemp -d)
API_PORT=${ARGUS_M8_API_PORT:-4280}
API_URL="http://127.0.0.1:${API_PORT}/api/v1"
PLATFORM_ORIGIN=http://localhost:4174
ENTERPRISE_ORIGIN=http://localhost:4173
SETUP_ORIGIN=http://localhost:4175
PLATFORM_JAR="${WORK_DIR}/platform.cookies"
ENTERPRISE_JAR="${WORK_DIR}/enterprise.cookies"
PLATFORM_RECOVERY="${WORK_DIR}/platform-recovery.json"
ENTERPRISE_RECOVERY="${WORK_DIR}/enterprise-recovery.json"
PF_PID=""
E2E_COMPLETED=false
mkdir -p "$ARTIFACT_DIR"
chmod 700 "$ARTIFACT_DIR"

cleanup() {
  status=$?
  if [[ $status -eq 0 && "$E2E_COMPLETED" != true ]]; then status=1; fi
  set +e
  if [[ -n "$PF_PID" ]]; then kill "$PF_PID" >/dev/null 2>&1 || true; wait "$PF_PID" >/dev/null 2>&1 || true; fi
  for config in "$RESTORE_CONFIG" "$SOURCE_CONFIG"; do
    if [[ -f "$config" ]]; then
      go run ./cmd/argusctl uninstall --config "$config" --delete-data --delete-owned-crds --yes >>"$ARTIFACT_DIR/cleanup.log" 2>&1 || true
    fi
  done
  kubectl --context docker-desktop -n kube-system delete lease argus-m8-e2e --ignore-not-found=true >>"$ARTIFACT_DIR/cleanup.log" 2>&1 || true
  rm -rf "$WORK_DIR"
  exit "$status"
}
trap cleanup EXIT

request() {
  local name=$1 expected=$2 method=$3 path=$4 jar=$5 body=$6 status
  shift 6
  local response="${WORK_DIR}/${name}.json"
  local -a args=(--noproxy '*' --silent --show-error --connect-timeout 5 --max-time 30 --output "$response" --write-out '%{http_code}' --request "$method")
  if [[ "$jar" != "-" ]]; then args+=(--cookie "$jar" --cookie-jar "$jar"); fi
  if [[ "$body" != "-" ]]; then args+=(--header 'Content-Type: application/json' --data "$body"); fi
  status=$(curl "${args[@]}" "$@" "${API_URL}${path}")
  if [[ "$status" != "$expected" ]]; then
    jq 'del(.temporary_password, .password_change_challenge, .mfa_challenge, .csrf_token, .authenticated_session.csrf_token, .enrollment_id, .secret, .otpauth_uri, .codes)' "$response" \
      >"${ARTIFACT_DIR}/${name}-failure.json" 2>/dev/null || true
    echo "${name}: expected HTTP ${expected}, got ${status}" >&2
    return 1
  fi
  RESPONSE_FILE=$response
}

start_api_port_forward() {
  local namespace=$1
  if [[ -n "$PF_PID" ]]; then kill "$PF_PID" >/dev/null 2>&1 || true; wait "$PF_PID" >/dev/null 2>&1 || true; fi
  kubectl --context docker-desktop -n "$namespace" port-forward service/argus-server "${API_PORT}:8080" >"${ARTIFACT_DIR}/port-forward-${namespace}.log" 2>&1 &
  PF_PID=$!
  for _ in $(seq 1 120); do
    curl --noproxy '*' --silent --fail --max-time 2 "http://127.0.0.1:${API_PORT}/readyz" >/dev/null 2>&1 && return
    sleep 1
  done
  echo "Argus API did not become ready in ${namespace}" >&2
  return 1
}

enroll_mfa() {
  local audience=$1 jar=$2 csrf=$3 origin=$4 output=$5 enrollment secret code
  request "${audience}-mfa-enroll" 201 POST "/${audience}/account/mfa/totp/enroll" "$jar" - \
    --header "Origin: ${origin}" --header "X-CSRF-Token: ${csrf}" --header "Idempotency-Key: m8-${audience}-mfa-${RUN_ID}"
  enrollment=$(jq -er '.enrollment_id' "$RESPONSE_FILE")
  secret=$(jq -er '.secret' "$RESPONSE_FILE")
  code=$(printf '%s' "$secret" | node scripts/totp-code.mjs)
  request "${audience}-mfa-verify" 200 POST "/${audience}/account/mfa/totp/verify" "$jar" \
    "$(jq -nc --arg enrollment "$enrollment" --arg code "$code" '{enrollment_id:$enrollment,code:$code}')" \
    --header "Origin: ${origin}" --header "X-CSRF-Token: ${csrf}"
  jq -e '.codes | length == 10' "$RESPONSE_FILE" >/dev/null
  cp "$RESPONSE_FILE" "$output"
  chmod 600 "$output"
  unset enrollment secret code
}

run_local_identity_flow() {
  local namespace=$1 release=$2 setup_token temporary challenge enrollment_code break_glass_id
  PLATFORM_PASSWORD='N7!qP4@vL9#sT2$x'
  ENTERPRISE_PASSWORD='R8!kW3@zM6#pQ2$x'
  setup_token=$(kubectl --context docker-desktop -n "$namespace" get secret "${release}-generated-secrets" -o jsonpath='{.data.setup-token}' | openssl base64 -d -A)

  request setup-status 200 GET /setup/status - -
  jq -e '.state == "uninitialized"' "$RESPONSE_FILE" >/dev/null
  request setup-initialize 201 POST /setup/initialize - \
    "$(jq -nc --arg password "$PLATFORM_PASSWORD" '{platform_name:"Argus M8 Local",default_locale:"zh-CN",timezone:"Asia/Shanghai",external_url:"http://localhost:4174",super_admin:{username:"platform-admin",display_name:"Platform Admin",email:"platform@example.test",password:$password}}')" \
    --header "Origin: ${SETUP_ORIGIN}" --header "X-Argus-Setup-Token: ${setup_token}" --header "Idempotency-Key: m8-setup-${RUN_ID}"
  request platform-login 200 POST /platform/auth/login "$PLATFORM_JAR" \
    "$(jq -nc --arg password "$PLATFORM_PASSWORD" '{username:"platform-admin",password:$password}')" --header "Origin: ${PLATFORM_ORIGIN}"
  PLATFORM_CSRF=$(jq -er '.authenticated_session.csrf_token' "$RESPONSE_FILE")
  jq -e '.authenticated_session.mfa_state == "enrollment_required"' "$RESPONSE_FILE" >/dev/null
  enroll_mfa platform "$PLATFORM_JAR" "$PLATFORM_CSRF" "$PLATFORM_ORIGIN" "$PLATFORM_RECOVERY"

  request enterprise-create 201 POST /platform/enterprises "$PLATFORM_JAR" \
    '{"name":"M8 Local Enterprise","code":"m8-local","timezone":"Asia/Shanghai","default_locale":"zh-CN"}' \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: m8-enterprise-${RUN_ID}"
  ENTERPRISE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request enterprise-admin-create 201 POST /platform/enterprise-admins "$PLATFORM_JAR" \
    "$(jq -nc --arg enterprise "$ENTERPRISE_ID" '{enterprise_id:$enterprise,username:"enterprise-admin",display_name:"Enterprise Admin",email:"enterprise@example.test"}')" \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: m8-enterprise-admin-${RUN_ID}"
  temporary=$(jq -er '.temporary_password' "$RESPONSE_FILE")
  request enterprise-temp-login 200 POST /enterprise/auth/login "$ENTERPRISE_JAR" \
    "$(jq -nc --arg temporary "$temporary" '{username:"enterprise-admin",password:$temporary}')" --header "Origin: ${ENTERPRISE_ORIGIN}"
  challenge=$(jq -er '.password_change_challenge.challenge_id' "$RESPONSE_FILE")
  request enterprise-password-change 200 POST /enterprise/auth/complete-password-change "$ENTERPRISE_JAR" \
    "$(jq -nc --arg challenge "$challenge" --arg temporary "$temporary" --arg password "$ENTERPRISE_PASSWORD" '{challenge_id:$challenge,temporary_password:$temporary,new_password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"
  ENTERPRISE_CSRF=$(jq -er '.csrf_token' "$RESPONSE_FILE")
  enroll_mfa enterprise "$ENTERPRISE_JAR" "$ENTERPRISE_CSRF" "$ENTERPRISE_ORIGIN" "$ENTERPRISE_RECOVERY"
  enrollment_code=$(jq -er '.codes[0]' "$ENTERPRISE_RECOVERY")
  request enterprise-step-up 200 POST /enterprise/auth/step-up "$ENTERPRISE_JAR" \
    "$(jq -nc --arg code "$enrollment_code" '{code:$code}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  request enterprise-openbao-secret-create 201 POST /enterprise/secrets "$ENTERPRISE_JAR" \
    "$(jq -nc --arg name "m8-openbao-secret-${RUN_ID}" '{name:$name,type:"ssh_password",description:"M8 OpenBao envelope validation",value:"M8-openbao-secret-value"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m8-openbao-secret-${RUN_ID}"
  M8_SECRET_ID=$(jq -er '.id' "$RESPONSE_FILE")
  jq -e 'has("value") | not' "$RESPONSE_FILE" >/dev/null
  request enterprise-openbao-credential-create 201 POST /enterprise/credentials "$ENTERPRISE_JAR" \
    "$(jq -nc --arg id "$M8_SECRET_ID" --arg name "m8-openbao-credential-${RUN_ID}" '{name:$name,protocol:"ssh",username:"argus",secret_id:$id}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m8-openbao-credential-${RUN_ID}"
  M8_CREDENTIAL_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request break-glass-create 201 POST /enterprise/break-glass-sessions "$ENTERPRISE_JAR" \
    '{"reason":"local recovery validation","ticket_ref":"M8-LOCAL-001"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m8-break-glass-${RUN_ID}"
  break_glass_id=$(jq -er '.id' "$RESPONSE_FILE")
  request break-glass-revoke 204 POST "/enterprise/break-glass-sessions/${break_glass_id}/revoke" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  request enterprise-audit 200 GET /enterprise/audit-events "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e 'any(.items[]; .action == "identity.break_glass.created") and any(.items[]; .action == "identity.break_glass.revoked")' "$RESPONSE_FILE" >/dev/null
  unset setup_token temporary challenge enrollment_code break_glass_id
}

verify_restored_identity_flow() {
  local challenge code
  rm -f "$PLATFORM_JAR" "$ENTERPRISE_JAR"
  request restored-platform-login 200 POST /platform/auth/login "$PLATFORM_JAR" \
    "$(jq -nc --arg password "$PLATFORM_PASSWORD" '{username:"platform-admin",password:$password}')" --header "Origin: ${PLATFORM_ORIGIN}"
  jq -e '.status == "mfa_required"' "$RESPONSE_FILE" >/dev/null
  challenge=$(jq -er '.mfa_challenge.challenge_id' "$RESPONSE_FILE")
  code=$(jq -er '.codes[0]' "$PLATFORM_RECOVERY")
  request restored-platform-mfa 200 POST /platform/auth/mfa/complete "$PLATFORM_JAR" \
    "$(jq -nc --arg challenge "$challenge" --arg code "$code" '{challenge_id:$challenge,code:$code}')" --header "Origin: ${PLATFORM_ORIGIN}"
  jq -e '.mfa_state == "enabled" and (.amr | index("recovery_code") != null)' "$RESPONSE_FILE" >/dev/null

  request restored-enterprise-login 200 POST /enterprise/auth/login "$ENTERPRISE_JAR" \
    "$(jq -nc --arg password "$ENTERPRISE_PASSWORD" '{username:"enterprise-admin",password:$password}')" --header "Origin: ${ENTERPRISE_ORIGIN}"
  challenge=$(jq -er '.mfa_challenge.challenge_id' "$RESPONSE_FILE")
  code=$(jq -er '.codes[1]' "$ENTERPRISE_RECOVERY")
  request restored-enterprise-mfa 200 POST /enterprise/auth/mfa/complete "$ENTERPRISE_JAR" \
    "$(jq -nc --arg challenge "$challenge" --arg code "$code" '{challenge_id:$challenge,code:$code}')" --header "Origin: ${ENTERPRISE_ORIGIN}"
  request restored-openbao-secret 200 GET /enterprise/secrets "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e --arg id "$M8_SECRET_ID" '.items[] | select(.id == $id and .type == "ssh_password" and .current_version == 1)' "$RESPONSE_FILE" >/dev/null
  request restored-openbao-credential 200 GET /enterprise/credentials "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e --arg id "$M8_CREDENTIAL_ID" --arg secret "$M8_SECRET_ID" '.items[] | select(.id == $id and .secret_id == $secret and .protocol == "ssh")' "$RESPONSE_FILE" >/dev/null
  request restored-break-glass-history 200 GET /enterprise/break-glass-sessions "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e 'any(.[]; .ticket_ref == "M8-LOCAL-001" and .status == "revoked")' "$RESPONSE_FILE" >/dev/null
  unset challenge code
}

run_baseline() {
  local phase script log_file status phase_upper
  phase=$1
  script=$2
  log_file="${ARTIFACT_DIR}/${phase}-baseline.log"
  phase_upper=$(printf '%s' "$phase" | tr '[:lower:]' '[:upper:]')
  set +e
  ARGUS_E2E_RUN_ID="${RUN_ID}-${phase}" "$script" 2>&1 | tee "$log_file"
  status=${PIPESTATUS[0]}
  set -e
  if [[ $status -ne 0 ]] || ! grep -Fq "[${phase}-e2e] ${phase_upper} Kubernetes E2E passed;" "$log_file"; then
    echo "${phase_upper} baseline did not complete successfully" >&2
    return 1
  fi
}

if [[ "${ARGUS_M8_SKIP_BASELINES:-0}" != "1" ]]; then
  echo "running M6 baseline (includes M2/M3 and Remote Access)"
  run_baseline m6 ./scripts/e2e-m6-k8s.sh
  echo "running M7 baseline (includes M2-M5 and Telemetry)"
  run_baseline m7 ./scripts/e2e-m7-k8s.sh
fi

kubectl --context docker-desktop -n kube-system create lease argus-m8-e2e \
  --dry-run=client -o yaml | kubectl --context docker-desktop apply -f - >/dev/null

sed \
  -e "s/releaseId: argus-m8-local/releaseId: m8-${RUN_ID}/" \
  -e "s/argus-m8-local-system/m8-${RUN_ID}-system/g" \
  -e "s/argus-m8-local-sandbox/m8-${RUN_ID}-sandbox/g" \
  -e "s/argus-m8-local-observability/m8-${RUN_ID}-observability/g" \
  -e "s/platformMfaRequired: false/platformMfaRequired: true/" \
  deploy/profiles/local-hardening.yaml >"$SOURCE_CONFIG"

sed \
  -e "s/releaseId: argus-m8-local/releaseId: m8r-${RUN_ID}/" \
  -e "s/argus-m8-local-system/m8r-${RUN_ID}-system/g" \
  -e "s/argus-m8-local-sandbox/m8r-${RUN_ID}-sandbox/g" \
  -e "s/argus-m8-local-observability/m8r-${RUN_ID}-observability/g" \
  -e "s/platformMfaRequired: false/platformMfaRequired: true/" \
  deploy/profiles/local-hardening.yaml >"$RESTORE_CONFIG"

echo "installing local-hardening source environment"
go run ./cmd/argusctl install --config "$SOURCE_CONFIG" | tee "$ARTIFACT_DIR/install.log"
go run ./cmd/argusctl verify --config "$SOURCE_CONFIG" --artifacts "$ARTIFACT_DIR/pre-failure" | tee "$ARTIFACT_DIR/verify-before.log"

SYSTEM_NS="m8-${RUN_ID}-system"
OBS_NS="m8-${RUN_ID}-observability"
start_api_port_forward "$SYSTEM_NS"
run_local_identity_flow "$SYSTEM_NS" "m8-${RUN_ID}"
kubectl --context docker-desktop -n "$SYSTEM_NS" exec statefulset/argus-redis -- redis-cli FLUSHALL >/dev/null
for workload in argus-server argus-worker-action argus-connector-gateway; do
  kubectl --context docker-desktop -n "$SYSTEM_NS" delete pod -l "app.kubernetes.io/name=${workload}" --wait=false >/dev/null
done
for workload in argus-telemetry-writer argus-telemetry-query; do
  kubectl --context docker-desktop -n "$OBS_NS" delete pod -l "app.kubernetes.io/name=${workload}" --wait=false >/dev/null
done
kubectl --context docker-desktop -n "$SYSTEM_NS" scale statefulset argus-openbao --replicas=0 >/dev/null
kubectl --context docker-desktop -n "$SYSTEM_NS" scale statefulset argus-openbao --replicas=1 >/dev/null
kubectl --context docker-desktop -n "$SYSTEM_NS" rollout status statefulset/argus-openbao --timeout=5m >/dev/null
kubectl --context docker-desktop -n "$SYSTEM_NS" wait pod -l app.kubernetes.io/name=argus-server --for=condition=Ready --timeout=5m >/dev/null

go run ./cmd/argusctl verify --config "$SOURCE_CONFIG" --artifacts "$ARTIFACT_DIR/post-failure" | tee "$ARTIFACT_DIR/verify-after.log"
BACKUP_OUTPUT=$(go run ./cmd/argusctl backup create --config "$SOURCE_CONFIG" --artifacts "$ARTIFACT_DIR/backups")
printf '%s\n' "$BACKUP_OUTPUT" | tee "$ARTIFACT_DIR/backup.log"
BACKUP=$(printf '%s\n' "$BACKUP_OUTPUT" | sed -n 's/^backup=//p')
KEY_FILE=$(printf '%s\n' "$BACKUP_OUTPUT" | sed -n 's/^key_file=//p')
go run ./cmd/argusctl backup verify --config "$SOURCE_CONFIG" --backup "$BACKUP" --key-file "$KEY_FILE"

go run ./cmd/argusctl uninstall --config "$SOURCE_CONFIG" --delete-data --delete-owned-crds --yes
go run ./cmd/argusctl restore plan --config "$RESTORE_CONFIG" --backup "$BACKUP" --key-file "$KEY_FILE"
go run ./cmd/argusctl restore apply --config "$RESTORE_CONFIG" --backup "$BACKUP" --key-file "$KEY_FILE" | tee "$ARTIFACT_DIR/restore.log"
go run ./cmd/argusctl restore verify --config "$RESTORE_CONFIG" --backup "$BACKUP" --key-file "$KEY_FILE"
go run ./cmd/argusctl verify --config "$RESTORE_CONFIG" --artifacts "$ARTIFACT_DIR/restored" | tee "$ARTIFACT_DIR/verify-restored.log"
start_api_port_forward "m8r-${RUN_ID}-system"
verify_restored_identity_flow

printf '{"run_id":"%s","status":"local_hardening_complete","production_ready":false}\n' "$RUN_ID" >"$ARTIFACT_DIR/result.json"
chmod 600 "$ARTIFACT_DIR"/* 2>/dev/null || true
echo "M8 local hardening E2E passed: $RUN_ID"
E2E_COMPLETED=true
