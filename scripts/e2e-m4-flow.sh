#!/usr/bin/env bash

M4_REPLAY_IMAGE="argus/argus-replay-model:m4-${RUN_ID}"
M4_REPLAY_HOST="argus-replay-model.${SANDBOX_NS}.svc"
APPROVER_JAR="${WORK_DIR}/approver.cookies"

prepare_m4_dependencies() {
  log "building M4 Replay Model/OpenSandbox lifecycle image ${M4_REPLAY_IMAGE}"
  retry 3 docker build --quiet -f deploy/docker/replay-model.Dockerfile -t "$M4_REPLAY_IMAGE" . >/dev/null
  case "$KUBE_CONTEXT" in
    kind-*) kind load docker-image --name "${KUBE_CONTEXT#kind-}" "$M4_REPLAY_IMAGE" ;;
    minikube) minikube image load "$M4_REPLAY_IMAGE" ;;
    docker-desktop)
      local node
      node=$(k get nodes -o jsonpath='{.items[0].metadata.name}')
      docker save "$M4_REPLAY_IMAGE" | docker exec -i "$node" ctr -n k8s.io images import - >/dev/null
      ;;
  esac

  openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
    -subj "/CN=${M4_REPLAY_HOST}" \
    -addext "subjectAltName=DNS:argus-replay-model,DNS:${M4_REPLAY_HOST}" \
    -keyout "${WORK_DIR}/m4-replay.key" -out "${WORK_DIR}/m4-replay.crt" >/dev/null 2>&1
  k -n "$SANDBOX_NS" create secret tls argus-replay-model-tls \
    --cert "${WORK_DIR}/m4-replay.crt" --key "${WORK_DIR}/m4-replay.key" >/dev/null
  cat <<EOF | k apply -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata: {name: argus-replay-model, namespace: ${SANDBOX_NS}}
spec:
  replicas: 1
  selector: {matchLabels: {app.kubernetes.io/name: argus-replay-model}}
  template:
    metadata: {labels: {app.kubernetes.io/name: argus-replay-model, app.kubernetes.io/part-of: argus-m4-e2e}}
    spec:
      containers:
        - name: replay
          image: ${M4_REPLAY_IMAGE}
          imagePullPolicy: Never
          env:
            - {name: ARGUS_REPLAY_ADDRESS, value: ":8080"}
            - {name: ARGUS_REPLAY_PLAINTEXT_ADDRESS, value: ":8081"}
            - {name: ARGUS_REPLAY_TLS_CERT, value: /tls/tls.crt}
            - {name: ARGUS_REPLAY_TLS_KEY, value: /tls/tls.key}
          ports: [{name: model, containerPort: 8080}, {name: sandbox, containerPort: 8081}]
          volumeMounts: [{name: tls, mountPath: /tls, readOnly: true}]
          readinessProbe: {httpGet: {path: /healthz, port: sandbox}, initialDelaySeconds: 1, periodSeconds: 1}
          resources: {requests: {cpu: 10m, memory: 24Mi}, limits: {cpu: 250m, memory: 128Mi}}
      volumes: [{name: tls, secret: {secretName: argus-replay-model-tls}}]
---
apiVersion: v1
kind: Service
metadata: {name: argus-replay-model, namespace: ${SANDBOX_NS}}
spec:
  selector: {app.kubernetes.io/name: argus-replay-model}
  ports:
    - {name: https-model, port: 443, targetPort: model}
    - {name: http-sandbox, port: 8081, targetPort: sandbox}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: argus-m4-e2e-model-egress, namespace: ${SYSTEM_NS}}
spec:
  podSelector:
    matchExpressions:
      - key: app.kubernetes.io/name
        operator: In
        values: [argus-worker]
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector:
            matchLabels: {kubernetes.io/metadata.name: ${SANDBOX_NS}}
          podSelector:
            matchLabels: {app.kubernetes.io/name: argus-replay-model}
      ports: [{protocol: TCP, port: 8080}]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: argus-m4-e2e-sandbox-egress, namespace: ${SYSTEM_NS}}
spec:
  podSelector:
    matchLabels: {app.kubernetes.io/name: argus-worker}
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector:
            matchLabels: {kubernetes.io/metadata.name: ${SANDBOX_NS}}
          podSelector:
            matchLabels: {app.kubernetes.io/name: argus-replay-model}
      ports: [{protocol: TCP, port: 8081}]
EOF
  k -n "$SANDBOX_NS" rollout status deployment/argus-replay-model --timeout=180s >/dev/null
}

cleanup_m4() {
  if [[ "$KUBE_CONTEXT" == "docker-desktop" ]]; then
    local node
    node=$(k get nodes -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    [[ -z "$node" ]] || docker exec "$node" ctr -n k8s.io images remove "docker.io/${M4_REPLAY_IMAGE}" >/dev/null 2>&1 || true
  fi
  docker image rm "$M4_REPLAY_IMAGE" >/dev/null 2>&1 || true
}

diagnostics_m4() {
  k -n "$SANDBOX_NS" logs deployment/argus-replay-model --tail=1000 2>&1 | redact >"${ARTIFACT_DIR}/m4-replay.log" || true
  m4_psql "
    SELECT 'runs|' || id || '|' || status || '|' || coalesce(stop_reason, '') || '|' || coalesce(error_code, '') FROM runs ORDER BY created_at;
    SELECT 'tasks|' || id || '|' || queue || '|' || status || '|' || attempt || '|' || coalesce(last_error_code, '') FROM runtime_tasks ORDER BY created_at;
    SELECT 'steps|' || id || '|' || step_type || '|' || status || '|' || attempt FROM run_steps ORDER BY created_at;
    SELECT 'model_calls|' || id || '|' || call_kind || '|' || status || '|' || coalesce(stop_reason, '') || '|' || coalesce(error_code, '') FROM model_calls ORDER BY created_at;
    SELECT 'quota_reservations|' || id || '|' || status FROM model_quota_reservations ORDER BY created_at;
    SELECT 'tool_calls|' || id || '|' || tool_id || '|' || status || '|' || coalesce(error_code, '') FROM tool_calls ORDER BY created_at;
    SELECT 'events|' || id || '|' || sequence || '|' || event_type FROM conversation_events ORDER BY conversation_id, sequence;
    SELECT 'pending_actions|' || action_ref || '|' || status || '|' || action_type || '|' || coalesce(error_code, '') FROM pending_actions ORDER BY created_at;
    SELECT 'approval_requests|' || id || '|' || status || '|' || pending_action_id FROM approval_requests ORDER BY created_at;
    SELECT 'approval_requirements|' || approval_request_id || '|' || policy_id || '|' || status || '|' || approved_count FROM approval_requirement_snapshots ORDER BY approval_request_id, policy_id;
    SELECT 'executions|' || id || '|' || status || '|' || pending_action_id || '|' || coalesce(error_code, '') FROM executions ORDER BY created_at;
  " 2>&1 | redact >"${ARTIFACT_DIR}/m4-runtime-state.txt" || true
	if [[ -n "${RESPONSE_FILE:-}" && -f "$RESPONSE_FILE" ]]; then
		redact <"$RESPONSE_FILE" >"${ARTIFACT_DIR}/m4-last-response.json" || true
	fi
}

m4_psql() {
  k -n "$SYSTEM_NS" exec statefulset/argus-postgresql -- env PGPASSWORD="$POSTGRES_PASSWORD" \
    psql -v ON_ERROR_STOP=1 -At -U argus -d argus -c "$1"
}

m4_uuid() {
  local value
  value=$(openssl rand -hex 16)
  printf '%s-%s-4%s-a%s-%s\n' "${value:0:8}" "${value:8:4}" "${value:13:3}" "${value:17:3}" "${value:20:12}"
}

m4_wait_run() {
  local run_id=$1 status
  for _ in $(seq 1 120); do
    request m4-run-state 200 GET "/runs/${run_id}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    status=$(jq -er '.status' "$RESPONSE_FILE")
    case "$status" in
      succeeded|cancelled) return ;;
      failed) jq -c . "$RESPONSE_FILE" >&2; fail "M4 Run failed" ;;
    esac
    sleep 1
  done
  fail "M4 Run did not reach a terminal state; last status was ${status:-unknown}"
}

m4_wait_run_status() {
	local run_id=$1 expected=$2 status
	for _ in $(seq 1 120); do
		request m4-run-state 200 GET "/runs/${run_id}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
		status=$(jq -er '.status' "$RESPONSE_FILE")
		[[ "$status" == "$expected" ]] && return
		[[ "$status" == "failed" || "$status" == "cancelled" ]] && fail "M4 Run reached ${status} while waiting for ${expected}"
		sleep 1
	done
	fail "M4 Run did not reach ${expected}; last status was ${status:-unknown}"
}

m4_wait_execution() {
  local action_ref=$1 execution_id status
  for _ in $(seq 1 120); do
    m4_enterprise_get m4-executions /enterprise/executions
    execution_id=$(jq -er --arg ref "$action_ref" '.items[] | select(.action_ref == $ref) | .execution_id' "$RESPONSE_FILE" 2>/dev/null || true)
    if [[ -n "$execution_id" ]]; then
      status=$(jq -er --arg id "$execution_id" '.items[] | select(.execution_id == $id) | .status' "$RESPONSE_FILE")
      if [[ "$status" == "succeeded" ]]; then
        M4_LAST_EXECUTION_ID=$execution_id
        return
      fi
      [[ "$status" == "failed" ]] && fail "M4 Execution failed"
    fi
    sleep 1
  done
  fail "M4 Execution did not converge"
}

m4_enterprise_get() {
  local name=$1 path=$2 status
  for _ in 1 2 3; do
    LAST_REQUEST_NAME=$name
    RESPONSE_FILE="${WORK_DIR}/${name}.json"
    status=$(curl --noproxy '*' --silent --show-error --connect-timeout 5 --max-time 30 \
      --output "$RESPONSE_FILE" --write-out '%{http_code}' --request GET \
      --cookie "$ENTERPRISE_JAR" --cookie-jar "$ENTERPRISE_JAR" \
      --header "Origin: ${ENTERPRISE_ORIGIN}" "${API_URL}${path}")
    if [[ "$status" == "200" ]]; then
      return
    fi
    if [[ "$status" == "403" ]] && jq -e '.code == "AUTHORIZATION_VERSION_STALE"' "$RESPONSE_FILE" >/dev/null 2>&1; then
      rm -f "$ENTERPRISE_JAR"
      enterprise_login
      continue
    fi
    jq -c '{code: .code, message_key: .message_key}' "$RESPONSE_FILE" >&2 2>/dev/null || true
    fail "${name}: expected HTTP 200, got ${status}"
  done
  fail "${name}: authorization version remained stale after session refresh"
}

run_m4_api_flow() {
  step_up_enterprise_session
  log "running M4 Model, Agent, approval, execution, automation, and Sandbox flow"
  local model_base="https://${M4_REPLAY_HOST}/v1"

  request m4-initial-roles 200 GET /enterprise/roles "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M4_RESOURCE_ADMIN_ROLE_ID=$(jq -er '.items[] | select(.builtin == true and .name == "Resource Admin") | .id' "$RESPONSE_FILE")
  M4_APPROVER_ROLE_ID=$(jq -er '.items[] | select(.builtin == true and .name == "Resource Approver") | .id' "$RESPONSE_FILE")
  request m4-admin-scope 201 POST /enterprise/data-scopes "$ENTERPRISE_JAR" \
    '{"name":"M4 managed resources","resource_types":["host","kubernetes_cluster","kubernetes_namespace"],"explicit_resource_ids":[],"label_selector":{"schema_version":"argus.label_selector/v1","requirements":[{"key":"team","operator":"eq","values":["m4"]}]}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-admin-scope-${RUN_ID}"
  M4_ADMIN_SCOPE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m4-admin-binding 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
    "$(jq -nc --arg subject "$ADMIN_USER_ID" --arg role "$M4_RESOURCE_ADMIN_ROLE_ID" --arg scope "$M4_ADMIN_SCOPE_ID" '{subject_type:"user",subject_id:$subject,role_id:$role,data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-admin-binding-${RUN_ID}"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  for protocol in chat_completions responses; do
    request "m4-model-${protocol}" 201 POST /enterprise/ai-models/test-and-create "$ENTERPRISE_JAR" \
      "$(jq -nc --arg protocol "$protocol" --arg base "$model_base" '{name:("M4 Replay "+$protocol),base_url:$base,model_id:("argus-replay-"+$protocol),api_protocol:$protocol,api_key:"m4-write-only-key",context_window_tokens:8192,max_output_tokens:512,input_price_per_million:0.1,output_price_per_million:0.2}')" \
      --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-model-${protocol}-${RUN_ID}"
    jq -e '.compatible == true and all(.checks[]; .status == "passed")' "$RESPONSE_FILE" >/dev/null
    [[ "$protocol" == "chat_completions" ]] && M4_MODEL_ID=$(jq -er '.model.id' "$RESPONSE_FILE")
  done

  request m4-user-quota 200 POST /enterprise/model-quotas "$ENTERPRISE_JAR" \
    "$(jq -nc --arg model "$M4_MODEL_ID" --arg user "$ADMIN_USER_ID" '{model_id:$model,subject_type:"user",subject_id:$user,monthly_amount:100}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-quota-${RUN_ID}"
  request m4-conversation 201 POST /conversations "$ENTERPRISE_JAR" \
    "$(jq -nc --arg model "$M4_MODEL_ID" '{title:"M4 recovery flow",selected_model_id:$model}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-conversation-${RUN_ID}"
  M4_CONVERSATION_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m4-message 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
    '{"content":"Call host.list and summarize the visible hosts."}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-message-${RUN_ID}"
  M4_RUN_ID=$(jq -er '.run.run_id' "$RESPONSE_FILE")
	m4_wait_run "$M4_RUN_ID"
	request m4-events 200 GET "/conversations/${M4_CONVERSATION_ID}/ledger" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	jq -e 'any(.items[]; .event_type == "tool_call_result") and any(.items[]; .event_type == "assistant_message")' "$RESPONSE_FILE" >/dev/null
	request m4-compact 202 POST "/runs/${M4_RUN_ID}/compact" "$ENTERPRISE_JAR" - \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-compact-${RUN_ID}"
	for _ in $(seq 1 90); do
		[[ "$(m4_psql "SELECT count(*) FROM context_snapshots WHERE run_id='${M4_RUN_ID}' AND status='active';")" == "1" ]] && break
		sleep 1
	done
	[[ "$(m4_psql "SELECT count(*) FROM context_snapshots WHERE run_id='${M4_RUN_ID}' AND status='active';")" == "1" ]] || fail "M4 manual Compaction did not create an active snapshot"
	[[ "$(m4_psql "SELECT count(*) FROM model_calls WHERE run_id='${M4_RUN_ID}' AND call_kind='compaction' AND status='succeeded';")" == "1" ]] || fail "M4 Compaction ModelCall evidence is missing"
	[[ "$(m4_psql "SELECT status FROM runs WHERE id='${M4_RUN_ID}';")" == "succeeded" ]] || fail "manual Compaction resurrected a terminal Run"

  request m4-bastion-preview 201 POST /enterprise/bastion-scopes/actions/preview-create "$ENTERPRISE_JAR" \
    '{"name":"m4-one-time-result","environment":"production","labels":{"team":"m4","route":"enrollment"}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-bastion-preview-${RUN_ID}"
  M4_ENROLLMENT_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  request m4-bastion-confirm 200 POST "/enterprise/pending-actions/${M4_ENROLLMENT_ACTION_REF}/confirm" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-bastion-confirm-${RUN_ID}"
  jq -e '.execution.status == "pending" and (tostring | contains("install_command") | not)' "$RESPONSE_FILE" >/dev/null
  m4_wait_execution "$M4_ENROLLMENT_ACTION_REF"
  M4_ENROLLMENT_EXECUTION_ID=$M4_LAST_EXECUTION_ID
  request m4-enrollment-execution 200 GET "/enterprise/executions/${M4_ENROLLMENT_EXECUTION_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.status == "succeeded" and .one_time_result_available == true and (tostring | contains("install_command") | not)' "$RESPONSE_FILE" >/dev/null
  request m4-enrollment-claim 200 POST "/enterprise/executions/${M4_ENROLLMENT_EXECUTION_ID}/one-time-result" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-enrollment-claim-${RUN_ID}"
  M4_INSTALL_COMMAND=$(jq -er '.enrollment.install_command' "$RESPONSE_FILE")
  jq -e '.schema_version == "argus.action_one_time_result/v1" and .result_kind == "connector_enrollment"' "$RESPONSE_FILE" >/dev/null
  request m4-enrollment-claim-retry 200 POST "/enterprise/executions/${M4_ENROLLMENT_EXECUTION_ID}/one-time-result" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-enrollment-claim-${RUN_ID}"
  [[ "$(jq -er '.enrollment.install_command' "$RESPONSE_FILE")" == "$M4_INSTALL_COMMAND" ]] || fail "one-time result idempotency replay changed the command"
  request m4-enrollment-claim-consumed 409 POST "/enterprise/executions/${M4_ENROLLMENT_EXECUTION_ID}/one-time-result" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-enrollment-second-claim-${RUN_ID}"
  jq -e '.code == "ACTION_RESULT_ALREADY_CONSUMED"' "$RESPONSE_FILE" >/dev/null
  [[ "$(m4_psql "SELECT count(*) FROM execution_one_time_results WHERE encode(ciphertext,'escape') LIKE '%--token%';")" == "0" ]] || fail "one-time install command leaked into PostgreSQL ciphertext"
  unset M4_INSTALL_COMMAND

  request m4-approver-create 201 POST /enterprise/users "$ENTERPRISE_JAR" \
    "$(jq -nc --arg department "$DEFAULT_DEPARTMENT_ID" --arg role "$M4_APPROVER_ROLE_ID" '{username:"m4-approver",display_name:"M4 Approver",department_id:$department,role_ids:[$role]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-approver-${RUN_ID}"
  M4_APPROVER_PASSWORD=$(jq -er '.temporary_password' "$RESPONSE_FILE")
  request m4-approver-login 200 POST /enterprise/auth/login "$APPROVER_JAR" \
    "$(jq -nc --arg password "$M4_APPROVER_PASSWORD" '{username:"m4-approver",password:$password}')" --header "Origin: ${ENTERPRISE_ORIGIN}"
  M4_APPROVER_CHALLENGE=$(jq -er '.password_change_challenge.challenge_id' "$RESPONSE_FILE")
  M4_APPROVER_NEW_PASSWORD='Q8!mV4@rT7#pL2$x'
  request m4-approver-password 200 POST /enterprise/auth/complete-password-change "$APPROVER_JAR" \
    "$(jq -nc --arg challenge "$M4_APPROVER_CHALLENGE" --arg temporary "$M4_APPROVER_PASSWORD" --arg password "$M4_APPROVER_NEW_PASSWORD" '{challenge_id:$challenge,temporary_password:$temporary,new_password:$password}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}"
  M4_APPROVER_CSRF=$(jq -er '.csrf_token' "$RESPONSE_FILE")
	unset M4_APPROVER_PASSWORD M4_APPROVER_CHALLENGE

  request m4-policy 201 POST /enterprise/approval-policies "$ENTERPRISE_JAR" \
    "$(jq -nc --arg role "$M4_APPROVER_ROLE_ID" '{name:"M4 host changes",enabled:true,tool_ids:["argus.host.update.commit"],risks:["write"],resource_types:["host"],minimum_approvers:1,separation_of_duty:true,approver_role_ids:[$role],expires_after_seconds:3600,expected_version:0}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-policy-${RUN_ID}"
  request m4-policy-secondary 201 POST /enterprise/approval-policies "$ENTERPRISE_JAR" \
    "$(jq -nc --arg role "$M4_APPROVER_ROLE_ID" '{name:"M4 production label changes",enabled:true,tool_ids:["argus.host.update.commit"],risks:["write"],resource_types:["host"],label_selector:{schema_version:"argus.label_selector/v1",requirements:[{key:"environment",operator:"eq",values:["prod"]}]},minimum_approvers:1,separation_of_duty:true,approver_role_ids:[$role],expires_after_seconds:1800,expected_version:0}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-policy-secondary-${RUN_ID}"

  M4_HOST_ID=$(m4_uuid)
  M4_LABELS='{"environment":"prod","team":"m4"}'
  M4_LABELS_HASH=$(printf '%s' "$M4_LABELS" | openssl dgst -sha256 | awk '{print $NF}')
  m4_psql "INSERT INTO hosts (id,enterprise_id,name,hostname,address,port,platform,connection_mode,environment,labels,labels_hash,connection_status) VALUES ('${M4_HOST_ID}','${ENTERPRISE_ID}','m4-managed-host','m4-managed-host','203.0.113.10',22,'linux','direct_ssh','production','${M4_LABELS}'::jsonb,decode('${M4_LABELS_HASH}','hex'),'offline');" >/dev/null
	M4_CHAT_TOOL_INPUT=$(jq -nc --arg host "$M4_HOST_ID" '{host_id:$host,expected_version:1,labels:{environment:"prod",team:"m4",release:"approved"}}' | base64 | tr '+/' '-_' | tr -d '=\n')
	request m4-chat-preview 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg input "$M4_CHAT_TOOL_INPUT" '{content:("Call host.update.preview with tool_input_b64: "+$input)}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-chat-preview-${RUN_ID}"
	M4_ACTION_RUN_ID=$(jq -er '.run.run_id' "$RESPONSE_FILE")
	m4_wait_run_status "$M4_ACTION_RUN_ID" waiting_input
	M4_ACTION_REF=$(m4_psql "SELECT action_ref FROM pending_actions WHERE run_id='${M4_ACTION_RUN_ID}' AND enterprise_id='${ENTERPRISE_ID}';")
	[[ -n "$M4_ACTION_REF" ]] || fail "Chat Preview did not bind a PendingAction to its Run"
	[[ "$(m4_psql "SELECT count(*) FROM conversation_events WHERE run_id='${M4_ACTION_RUN_ID}' AND event_type='pending_action_created';")" == "1" ]] || fail "Chat Preview did not persist pending_action_created"
  request m4-confirm 200 POST "/enterprise/pending-actions/${M4_ACTION_REF}/confirm" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-confirm-${RUN_ID}"
  jq -e '.pending_action.status == "awaiting_approval"' "$RESPONSE_FILE" >/dev/null
  M4_APPROVAL_ID=$(jq -er '.approval_request.approval_request_id' "$RESPONSE_FILE")
  request m4-approval-requirements 200 GET "/enterprise/approval-requests/${M4_APPROVAL_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.status == "pending" and (.requirements | length) == 2 and all(.requirements[]; .status == "pending")' "$RESPONSE_FILE" >/dev/null
	k -n "$SYSTEM_NS" delete pod -l app.kubernetes.io/name=argus-worker --wait=false >/dev/null
  k -n "$SYSTEM_NS" exec statefulset/argus-redis -- redis-cli -a "$REDIS_PASSWORD" FLUSHALL >/dev/null 2>&1
  request m4-approve 200 POST "/enterprise/approval-requests/${M4_APPROVAL_ID}/decisions" "$APPROVER_JAR" \
    '{"decision":"approved","reason":"independent M4 approval"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${M4_APPROVER_CSRF}" --header "Idempotency-Key: m4-approve-${RUN_ID}"
  jq -e '.status == "approved" and all(.requirements[]; .status == "approved")' "$RESPONSE_FILE" >/dev/null
	m4_wait_execution "$M4_ACTION_REF"
	m4_wait_run "$M4_ACTION_RUN_ID"
	[[ "$(m4_psql "SELECT labels->>'release' FROM hosts WHERE id='${M4_HOST_ID}' AND enterprise_id='${ENTERPRISE_ID}';")" == "approved" ]] || fail "M4 Chat commit did not persist the approved labels"
	[[ "$(m4_psql "SELECT count(*) FROM model_calls WHERE run_id='${M4_ACTION_RUN_ID}' AND call_kind='inference' AND status='succeeded';")" -ge "2" ]] || fail "M4 execution Verify did not resume the Agent Run"
	[[ "$(m4_psql "SELECT count(*) FROM executions execution JOIN pending_actions action ON action.id=execution.pending_action_id WHERE action.action_ref='${M4_ACTION_REF}' AND execution.run_id='${M4_ACTION_RUN_ID}';")" == "1" ]] || fail "M4 Execution lost its Run binding"

  request m4-roles-refresh 200 GET /enterprise/roles "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M4_RESOURCE_VIEWER_ROLE_ID=$(jq -er '.items[] | select(.builtin == true and .name == "Resource Viewer") | .id' "$RESPONSE_FILE")
	request m4-automation-account 201 POST /enterprise/service-accounts "$ENTERPRISE_JAR" \
		"$(jq -nc --arg scope "$M4_ADMIN_SCOPE_ID" '{name:"m4-automation",allowed_tool_ids:["host.list","host.update.preview"],data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-automation-account-${RUN_ID}"
  M4_AUTOMATION_ACCOUNT_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m4-automation-binding 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
		"$(jq -nc --arg subject "$M4_AUTOMATION_ACCOUNT_ID" --arg role "$M4_RESOURCE_ADMIN_ROLE_ID" --arg scope "$M4_ADMIN_SCOPE_ID" '{subject_type:"service_account",subject_id:$subject,role_id:$role,data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-automation-binding-${RUN_ID}"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m4-automation-create 201 POST /enterprise/automations "$ENTERPRISE_JAR" \
    "$(jq -nc --arg account "$M4_AUTOMATION_ACCOUNT_ID" '{name:"M4 host inventory",service_account_id:$account,tool_id:"host.list",tool_input:{},cron:"* * * * *",timezone:"UTC"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-automation-${RUN_ID}"
  M4_AUTOMATION_ID=$(jq -er '.id' "$RESPONSE_FILE")
  for _ in $(seq 1 90); do
    request m4-automation-runs 200 GET "/enterprise/automations/${M4_AUTOMATION_ID}/runs" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    jq -e 'any(.[]; .status == "succeeded" and (.result_ref | length) > 0)' "$RESPONSE_FILE" >/dev/null && break
    sleep 1
  done
	jq -e 'any(.[]; .status == "succeeded" and (.result_ref | length) > 0)' "$RESPONSE_FILE" >/dev/null || fail "M4 read-only Automation did not persist an Artifact"

	M4_HOST_VERSION=$(m4_psql "SELECT resource_version FROM hosts WHERE id='${M4_HOST_ID}';")
	request m4-write-automation-create 201 POST /enterprise/automations "$ENTERPRISE_JAR" \
		"$(jq -nc --arg account "$M4_AUTOMATION_ACCOUNT_ID" --arg host "$M4_HOST_ID" --argjson version "$M4_HOST_VERSION" '{name:"M4 governed host update",service_account_id:$account,tool_id:"host.update.preview",tool_input:{host_id:$host,expected_version:$version,labels:{environment:"prod",team:"m4",release:"automation-v1"}},cron:"* * * * *",timezone:"UTC"}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-write-automation-${RUN_ID}"
	M4_WRITE_AUTOMATION_ID=$(jq -er '.id' "$RESPONSE_FILE")
	M4_WRITE_AUTOMATION_VERSION=$(jq -er '.version' "$RESPONSE_FILE")
	M4_WRITE_RUN_ID=$(m4_uuid)
	M4_WRITE_TASK_ID=$(m4_uuid)
	m4_psql "INSERT INTO runtime_tasks (id,enterprise_id,queue,payload,available_at) VALUES ('${M4_WRITE_TASK_ID}','${ENTERPRISE_ID}','automation',jsonb_build_object('run_id','${M4_WRITE_RUN_ID}','enterprise_id','${ENTERPRISE_ID}'),now()+interval '10 minutes'); INSERT INTO automation_runs (id,automation_id,enterprise_id,automation_revision,scheduled_for,status,task_id) VALUES ('${M4_WRITE_RUN_ID}','${M4_WRITE_AUTOMATION_ID}','${ENTERPRISE_ID}',1,now(),'pending','${M4_WRITE_TASK_ID}');" >/dev/null
	request m4-write-automation-update 200 PUT "/enterprise/automations/${M4_WRITE_AUTOMATION_ID}" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg account "$M4_AUTOMATION_ACCOUNT_ID" --arg host "$M4_HOST_ID" --argjson version "$M4_HOST_VERSION" --argjson expected "$M4_WRITE_AUTOMATION_VERSION" '{name:"M4 governed host update revision 2",service_account_id:$account,tool_id:"host.update.preview",tool_input:{host_id:$host,expected_version:$version,labels:{environment:"prod",team:"m4",release:"automation-v2"}},cron:"* * * * *",timezone:"UTC",expected_version:$expected}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e '.version == 2' "$RESPONSE_FILE" >/dev/null
	m4_psql "UPDATE runtime_tasks SET available_at=now(),updated_at=now() WHERE id='${M4_WRITE_TASK_ID}';" >/dev/null
	for _ in $(seq 1 90); do
		request m4-write-automation-runs 200 GET "/enterprise/automations/${M4_WRITE_AUTOMATION_ID}/runs" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
		M4_AUTOMATION_ACTION_REF=$(jq -er --arg run "$M4_WRITE_RUN_ID" '.[] | select(.id == $run and .status == "waiting_approval" and .automation_revision == 1) | .pending_action_ref' "$RESPONSE_FILE" 2>/dev/null || true)
		[[ -n "$M4_AUTOMATION_ACTION_REF" ]] && break
		sleep 1
	done
	[[ -n "${M4_AUTOMATION_ACTION_REF:-}" ]] || fail "M4 write Automation did not reach approval on bound Revision 1"
	[[ "$(m4_psql "SELECT immutable_plan->'input'->'Labels'->>'release' FROM pending_action_plans plan JOIN pending_actions action ON action.id=plan.pending_action_id WHERE action.action_ref='${M4_AUTOMATION_ACTION_REF}';")" == "automation-v1" ]] || fail "AutomationRun used mutable current Automation instead of bound Revision"
	M4_AUTOMATION_APPROVAL_ID=$(m4_psql "SELECT request.id FROM approval_requests request JOIN pending_actions action ON action.id=request.pending_action_id WHERE action.action_ref='${M4_AUTOMATION_ACTION_REF}';")
	rm -f "$APPROVER_JAR"
	request m4-approver-refresh 200 POST /enterprise/auth/login "$APPROVER_JAR" \
		"$(jq -nc --arg password "$M4_APPROVER_NEW_PASSWORD" '{username:"m4-approver",password:$password}')" --header "Origin: ${ENTERPRISE_ORIGIN}"
	M4_APPROVER_CSRF=$(jq -er '.authenticated_session.csrf_token' "$RESPONSE_FILE")
	request m4-automation-approve 200 POST "/enterprise/approval-requests/${M4_AUTOMATION_APPROVAL_ID}/decisions" "$APPROVER_JAR" \
		'{"decision":"approved","reason":"governed Automation approval"}' \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${M4_APPROVER_CSRF}" --header "Idempotency-Key: m4-automation-approve-${RUN_ID}"
	m4_wait_execution "$M4_AUTOMATION_ACTION_REF"
	for _ in $(seq 1 60); do
		request m4-write-automation-finished 200 GET "/enterprise/automations/${M4_WRITE_AUTOMATION_ID}/runs" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
		jq -e --arg run "$M4_WRITE_RUN_ID" 'any(.[]; .id == $run and .status == "succeeded" and .automation_revision == 1)' "$RESPONSE_FILE" >/dev/null && break
		sleep 1
	done
	jq -e --arg run "$M4_WRITE_RUN_ID" 'any(.[]; .id == $run and .status == "succeeded" and .automation_revision == 1)' "$RESPONSE_FILE" >/dev/null || fail "AutomationRun did not converge after governed Execution"
	[[ "$(m4_psql "SELECT labels->>'release' FROM hosts WHERE id='${M4_HOST_ID}';")" == "automation-v1" ]] || fail "Automation Revision 1 commit did not persist"
	request m4-write-automation-disable 200 POST "/enterprise/automations/${M4_WRITE_AUTOMATION_ID}/disable?expected_version=2" "$ENTERPRISE_JAR" - \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e '.status == "disabled" and .version == 3' "$RESPONSE_FILE" >/dev/null

  request m4-sandbox-backend 201 POST /platform/sandbox/backends "$PLATFORM_JAR" \
    "$(jq -nc --arg endpoint "http://argus-replay-model.${SANDBOX_NS}.svc:8081/sandbox" '{name:"M4 OpenSandbox",endpoint:$endpoint,api_key:"write-only",status:"enabled",expected_version:0}')" \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: m4-sandbox-backend-${RUN_ID}"
  M4_SANDBOX_BACKEND_ID=$(jq -er '.id' "$RESPONSE_FILE")
  jq -e 'has("api_key") | not' "$RESPONSE_FILE" >/dev/null
  request m4-sandbox-test 200 POST "/platform/sandbox/backends/${M4_SANDBOX_BACKEND_ID}/test" "$PLATFORM_JAR" - \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: m4-sandbox-test-${RUN_ID}"
  jq -e '.health_status == "healthy"' "$RESPONSE_FILE" >/dev/null
  request m4-sandbox-image 201 POST /platform/sandbox/images "$PLATFORM_JAR" \
    "$(jq -nc --arg backend "$M4_SANDBOX_BACKEND_ID" --arg digest "sha256:$(printf '0%.0s' {1..64})" '{backend_id:$backend,name:"M4 smoke image",image_ref:"registry.example.test/argus/smoke",digest:$digest,status:"enabled",expected_version:0}')" \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: m4-sandbox-image-${RUN_ID}"
  M4_SANDBOX_IMAGE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m4-sandbox-profile 201 POST /platform/sandbox/profiles "$PLATFORM_JAR" \
    "$(jq -nc --arg backend "$M4_SANDBOX_BACKEND_ID" --arg image "$M4_SANDBOX_IMAGE_ID" '{name:"M4 smoke",backend_id:$backend,image_id:$image,task_kinds:["smoke"],cpu_millis:100,memory_mib:128,timeout_seconds:60,network_mode:"none",status:"enabled",expected_version:0}')" \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: m4-sandbox-profile-${RUN_ID}"
  request m4-sandbox-quota 200 PUT "/platform/sandbox/enterprise-quotas/${ENTERPRISE_ID}" "$PLATFORM_JAR" \
    '{"max_concurrent_sessions":1,"monthly_session_seconds":600,"expected_version":0}' \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}"
  M4_SANDBOX_TASK_ID=$(m4_uuid)
  m4_psql "INSERT INTO runtime_tasks (id,enterprise_id,queue,payload) VALUES ('${M4_SANDBOX_TASK_ID}','${ENTERPRISE_ID}','sandbox',jsonb_build_object('enterprise_id','${ENTERPRISE_ID}','task_id','${M4_SANDBOX_TASK_ID}','task_kind','smoke'));" >/dev/null
  for _ in $(seq 1 60); do
    request m4-sandbox-sessions 200 GET /platform/sandbox/sessions "$PLATFORM_JAR" - --header "Origin: ${PLATFORM_ORIGIN}"
    M4_SANDBOX_SESSION_ID=$(jq -er --arg task "$M4_SANDBOX_TASK_ID" '.[] | select(.task_id == $task and .status == "running") | .id' "$RESPONSE_FILE" 2>/dev/null || true)
    [[ -n "$M4_SANDBOX_SESSION_ID" ]] && break
    sleep 1
  done
  [[ -n "${M4_SANDBOX_SESSION_ID:-}" ]] || fail "M4 Sandbox session did not start"
  request m4-sandbox-terminate 200 POST "/platform/sandbox/sessions/${M4_SANDBOX_SESSION_ID}/terminate" "$PLATFORM_JAR" - \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}" --header "Idempotency-Key: m4-sandbox-terminate-${RUN_ID}"
  jq -e '.status == "terminated"' "$RESPONSE_FILE" >/dev/null
  request m4-sandbox-usage 200 GET /platform/sandbox/usage "$PLATFORM_JAR" - --header "Origin: ${PLATFORM_ORIGIN}"
  jq -e --arg enterprise "$ENTERPRISE_ID" 'any(.[]; .enterprise_id == $enterprise and .session_count == 1)' "$RESPONSE_FILE" >/dev/null
  M4_SANDBOX_USED_SECONDS=$(jq -er --arg enterprise "$ENTERPRISE_ID" '.[] | select(.enterprise_id == $enterprise) | .session_seconds' "$RESPONSE_FILE")
  request m4-sandbox-quota-exhaust 200 PUT "/platform/sandbox/enterprise-quotas/${ENTERPRISE_ID}" "$PLATFORM_JAR" \
    "$(jq -nc --argjson used "$M4_SANDBOX_USED_SECONDS" '{max_concurrent_sessions:1,monthly_session_seconds:$used,expected_version:1}')" \
    --header "Origin: ${PLATFORM_ORIGIN}" --header "X-CSRF-Token: ${PLATFORM_CSRF}"
  M4_SANDBOX_QUOTA_TASK_ID=$(m4_uuid)
  m4_psql "INSERT INTO runtime_tasks (id,enterprise_id,queue,payload) VALUES ('${M4_SANDBOX_QUOTA_TASK_ID}','${ENTERPRISE_ID}','sandbox',jsonb_build_object('enterprise_id','${ENTERPRISE_ID}','task_id','${M4_SANDBOX_QUOTA_TASK_ID}','task_kind','smoke'));" >/dev/null
  for _ in $(seq 1 30); do
    [[ "$(m4_psql "SELECT status || ':' || COALESCE(last_error_code,'') FROM runtime_tasks WHERE id='${M4_SANDBOX_QUOTA_TASK_ID}';")" == "failed:SANDBOX_QUOTA_EXCEEDED" ]] && break
    sleep 1
  done
  [[ "$(m4_psql "SELECT status || ':' || COALESCE(last_error_code,'') FROM runtime_tasks WHERE id='${M4_SANDBOX_QUOTA_TASK_ID}';")" == "failed:SANDBOX_QUOTA_EXCEEDED" ]] || fail "M4 Sandbox monthly quota did not fail closed"
	[[ "$(m4_psql "SELECT count(*) FROM sandbox_sessions WHERE task_id='${M4_SANDBOX_QUOTA_TASK_ID}';")" == "0" ]] || fail "quota-rejected Sandbox task persisted a session"

	M4_RECONCILE_CLUSTER_ID=$(m4_uuid)
	M4_RECONCILE_CONNECTOR_ID=$(m4_uuid)
	M4_RECONCILE_CONNECTION_EPOCH=1
	m4_psql "INSERT INTO kubernetes_clusters (
		id,enterprise_id,name,api_server,connection_mode,environment,labels,labels_hash,
		connection_status,status
	) VALUES (
		'${M4_RECONCILE_CLUSTER_ID}','${ENTERPRISE_ID}','m4-reconcile-cluster',
		'https://kubernetes.default.svc','in_cluster','development','{}',decode(repeat('00',32),'hex'),
		'disconnected','active'
	);
	INSERT INTO connectors (
		id,enterprise_id,role,name,kubernetes_cluster_id,instance_id,
		device_fingerprint_hash,public_key_hash,status,connection_epoch,certificate_expires_at
	) VALUES (
		'${M4_RECONCILE_CONNECTOR_ID}','${ENTERPRISE_ID}','kubernetes','m4-reconcile-connector',
		'${M4_RECONCILE_CLUSTER_ID}','m4-reconcile-${RUN_ID}',decode(repeat('00',32),'hex'),
		decode(repeat('11',32),'hex'),'offline',${M4_RECONCILE_CONNECTION_EPOCH},now()+interval '1 day'
	);
	UPDATE kubernetes_clusters SET connector_id='${M4_RECONCILE_CONNECTOR_ID}',updated_at=now()
	WHERE id='${M4_RECONCILE_CLUSTER_ID}' AND enterprise_id='${ENTERPRISE_ID}';" >/dev/null
	M4_RECONCILE_COMMAND_ID=$(m4_uuid)
	m4_psql "INSERT INTO connector_commands (
		id,command_id,enterprise_id,connector_id,connection_epoch,operation_ref,
		command_type,payload_schema_version,payload,payload_hash,idempotency_key,
		status,result,expires_at
	) VALUES (
		'${M4_RECONCILE_COMMAND_ID}','m4-reconcile-${RUN_ID}','${ENTERPRISE_ID}',
		'${M4_RECONCILE_CONNECTOR_ID}',${M4_RECONCILE_CONNECTION_EPOCH},'m4-reconcile-${RUN_ID}',
		'connector_uninstall','argus.connector_command/v1','{}',decode(repeat('00',32),'hex'),
		'm4-reconcile-${RUN_ID}','result_unknown','{}',now()+interval '10 minutes'
	);" >/dev/null
	M4_RECONCILE_RESOURCE_VERSION=$(m4_psql "SELECT resource_version FROM hosts WHERE id='${M4_HOST_ID}';")
	m4_psql "UPDATE executions SET status='result_unknown',connector_command_id='${M4_RECONCILE_COMMAND_ID}',error_code='EXECUTION_RESULT_UNKNOWN',completed_at=NULL,updated_at=now() WHERE id='${M4_LAST_EXECUTION_ID}'; UPDATE pending_actions SET status='executing',updated_at=now() WHERE action_ref='${M4_AUTOMATION_ACTION_REF}'; UPDATE automation_runs SET status='running',updated_at=now() WHERE id='${M4_WRITE_RUN_ID}';" >/dev/null
	sleep 5
	[[ "$(m4_psql "SELECT status FROM executions WHERE id='${M4_LAST_EXECUTION_ID}';")" == "result_unknown" ]] || fail "ResultUnknown reconciler replayed an unresolved side effect"
	[[ "$(m4_psql "SELECT resource_version FROM hosts WHERE id='${M4_HOST_ID}';")" == "$M4_RECONCILE_RESOURCE_VERSION" ]] || fail "ResultUnknown reconciliation repeated the Host mutation"
	m4_psql "UPDATE connector_commands SET status='succeeded',completed_at=now(),updated_at=now() WHERE id='${M4_RECONCILE_COMMAND_ID}';" >/dev/null
	m4_wait_execution "$M4_AUTOMATION_ACTION_REF"
	[[ "$(m4_psql "SELECT status FROM pending_actions WHERE action_ref='${M4_AUTOMATION_ACTION_REF}';")" == "succeeded" ]] || fail "resolved ResultUnknown did not finish the PendingAction"
	[[ "$(m4_psql "SELECT status FROM automation_runs WHERE id='${M4_WRITE_RUN_ID}';")" == "succeeded" ]] || fail "resolved ResultUnknown did not finish the AutomationRun"
	[[ "$(m4_psql "SELECT resource_version FROM hosts WHERE id='${M4_HOST_ID}';")" == "$M4_RECONCILE_RESOURCE_VERSION" ]] || fail "resolved ResultUnknown repeated the Host mutation"

	request m4-user-quota-exhaust 200 POST /enterprise/model-quotas "$ENTERPRISE_JAR" \
		"$(jq -nc --arg model "$M4_MODEL_ID" --arg user "$ADMIN_USER_ID" '{model_id:$model,subject_type:"user",subject_id:$user,monthly_amount:0}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-quota-exhaust-${RUN_ID}"
	request m4-quota-message 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
		'{"content":"Summarize the completed governed changes without calling a tool."}' \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m4-quota-message-${RUN_ID}"
	M4_QUOTA_RUN_ID=$(jq -er '.run.run_id' "$RESPONSE_FILE")
	for _ in $(seq 1 60); do
		request m4-quota-run 200 GET "/runs/${M4_QUOTA_RUN_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
		jq -e '.status == "failed" and .error_code == "MODEL_QUOTA_EXCEEDED"' "$RESPONSE_FILE" >/dev/null && break
		sleep 1
	done
	jq -e '.status == "failed" and .error_code == "MODEL_QUOTA_EXCEEDED"' "$RESPONSE_FILE" >/dev/null || fail "M4 model quota exhaustion did not fail the Run deterministically"

	if k -n "$SYSTEM_NS" logs -l app.kubernetes.io/part-of=argus --all-containers=true --tail=3000 2>/dev/null | rg -i 'm4-write-only-key|independent M4 approval'; then
    fail "M4 workload logs contain a model key or private approval input"
  fi

  ARGUS_E2E_EXTERNAL=1 ARGUS_M4_E2E=1 \
    ARGUS_M4_ENTERPRISE_USERNAME="$ENTERPRISE_USERNAME" ARGUS_M4_ENTERPRISE_PASSWORD="$ENTERPRISE_PASSWORD" \
    ARGUS_M4_PLATFORM_USERNAME="$PLATFORM_USERNAME" ARGUS_M4_PLATFORM_PASSWORD="$PLATFORM_PASSWORD" \
    ARGUS_E2E_ARTIFACTS="$ARTIFACT_DIR/playwright-m4" \
    pnpm --filter @argus/enterprise exec playwright test e2e/m4-real.spec.ts --workers=1
	unset M4_APPROVER_CSRF M4_APPROVER_NEW_PASSWORD
}
