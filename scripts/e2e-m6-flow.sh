#!/usr/bin/env bash

prepare_m6_dependencies() {
  local tls_dir="${WORK_DIR}/m6-winrs-tls"
  mkdir -p "$tls_dir"
  openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
    -subj '/CN=Argus M6 E2E WinRS CA' \
    -addext 'basicConstraints=critical,CA:TRUE' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -keyout "${tls_dir}/ca.key" -out "${tls_dir}/ca.crt" >/dev/null 2>&1
  openssl req -newkey rsa:2048 -nodes -subj '/CN=8.8.8.8' \
    -keyout "${tls_dir}/tls.key" -out "${tls_dir}/tls.csr" >/dev/null 2>&1
  printf '%s\n' 'subjectAltName=IP:8.8.8.8' 'extendedKeyUsage=serverAuth' >"${tls_dir}/server.ext"
  openssl x509 -req -days 2 -in "${tls_dir}/tls.csr" -CA "${tls_dir}/ca.crt" -CAkey "${tls_dir}/ca.key" \
    -CAcreateserial -extfile "${tls_dir}/server.ext" -out "${tls_dir}/tls.crt" >/dev/null 2>&1
  k -n "$SYSTEM_NS" create secret generic argus-m6-winrs-tls \
    --from-file=ca.crt="${tls_dir}/ca.crt" --from-file=tls.crt="${tls_dir}/tls.crt" --from-file=tls.key="${tls_dir}/tls.key" >/dev/null
}
cleanup_m6() { :; }

m6_install_winrs_target() {
  local patch
  patch=$(jq -nc --arg image "$M3_TARGET_IMAGE" '{spec:{template:{spec:{containers:[
    {name:"argus-direct-executor",env:[{name:"SSL_CERT_FILE",value:"/etc/argus/e2e-winrs/ca.crt"}],volumeMounts:[{name:"m6-winrs-tls",mountPath:"/etc/argus/e2e-winrs",readOnly:true}]},
    {name:"argus-m6-winrs-target",image:$image,imagePullPolicy:"Never",command:["/usr/local/bin/argus-e2e-winrs"],
      env:[{name:"ARGUS_E2E_WINRS_PASSWORD",value:"M6-e2e-winrs-password"}],ports:[{name:"e2e-winrs",containerPort:5986}],
      readinessProbe:{tcpSocket:{port:"e2e-winrs"},initialDelaySeconds:1,periodSeconds:1},
      securityContext:{runAsNonRoot:true,runAsUser:65532,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}},
      resources:{requests:{cpu:"10m",memory:"16Mi"},limits:{cpu:"100m",memory:"64Mi"}},volumeMounts:[{name:"m6-winrs-tls",mountPath:"/tls",readOnly:true}]}
  ],volumes:[{name:"m6-winrs-tls",secret:{secretName:"argus-m6-winrs-tls"}}]}}}}')
  k -n "$SYSTEM_NS" patch deployment argus-direct-executor --type=strategic --patch "$patch" >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-direct-executor --timeout=180s >/dev/null
}

diagnostics_m6() {
  m3_psql "
    SELECT 'grant|' || id || '|' || subject_type || '|' || enabled || '|' || version FROM remote_access_grants ORDER BY created_at;
    SELECT 'request|' || id || '|' || status || '|' || protocol FROM remote_access_requests ORDER BY created_at;
    SELECT 'lease|' || id || '|' || coalesce(revoked_at::text,'active') FROM remote_access_leases ORDER BY issued_at;
    SELECT 'session|' || id || '|' || status || '|' || session_fence || '|' || coalesce(termination_reason,'') FROM remote_access_sessions ORDER BY created_at;
    SELECT 'recording|' || id || '|' || status || '|' || chunk_count || '|' || event_count FROM remote_access_recordings ORDER BY created_at;
    SELECT 'credential_lease|' || id || '|' || status || '|' || recipient_type FROM credential_leases WHERE target_resource_type='remote_access_session' ORDER BY created_at;
    SELECT 'host|' || id || '|' || status || '|' || connection_mode || '|' || labels::text FROM hosts WHERE id IN ('${M3_BASTION_HOST_ID:-00000000-0000-0000-0000-000000000000}','${M3_DIRECT_HOST_ID:-00000000-0000-0000-0000-000000000000}');
    SELECT 'account|' || id || '|' || host_id || '|' || status || '|' || array_to_string(allowed_protocols, ',') FROM managed_accounts WHERE host_id IN ('${M3_BASTION_HOST_ID:-00000000-0000-0000-0000-000000000000}','${M3_DIRECT_HOST_ID:-00000000-0000-0000-0000-000000000000}');
    SELECT 'connector_session|' || connector_id || '|' || gateway_instance_id || '|' || connection_epoch FROM connector_sessions ORDER BY connected_at;
  " 2>&1 | redact >"${ARTIFACT_DIR}/m6-remote-access-state.txt" || true
}

run_m6_playwright() {
  log "running M6 real Playwright remote access and accessibility matrix"
  ARGUS_E2E_EXTERNAL=1 ARGUS_M6_E2E=1 \
    ARGUS_M6_ENTERPRISE_USERNAME="$ENTERPRISE_USERNAME" ARGUS_M6_ENTERPRISE_PASSWORD="$ENTERPRISE_PASSWORD" \
    ARGUS_M6_USER_ID="$ADMIN_USER_ID" ARGUS_M6_SSH_HOST_ID="$M3_DIRECT_HOST_ID" \
    ARGUS_M6_WINRS_HOST_ID="$M6_WINRS_HOST_ID" ARGUS_M6_WINRS_ACCOUNT_ID="$M6_WINRS_ACCOUNT_ID" \
    ARGUS_E2E_ARTIFACTS="$ARTIFACT_DIR/playwright-m6" \
    pnpm --filter @argus/enterprise exec playwright test e2e/m6-real.spec.ts --workers=1
}

m6_cross_gateway_flow() {
  log "verifying cross-Gateway Connector routing, PostgreSQL fallback, and Drain"
  request m6-bastion-access-scope 201 POST /enterprise/data-scopes "$ENTERPRISE_JAR" \
    "$(jq -nc --arg host "$M3_BASTION_HOST_ID" '{name:"M6 remote access bootstrap",resource_types:["host"],explicit_resource_ids:[$host]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-bastion-access-scope-${RUN_ID}"
  M6_BASTION_ACCESS_SCOPE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-bastion-access-binding 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
    "$(jq -nc --arg subject "$ADMIN_USER_ID" --arg role "$M3_RESOURCE_ADMIN_ROLE_ID" --arg scope "$M6_BASTION_ACCESS_SCOPE_ID" '{subject_type:"user",subject_id:$subject,role_id:$role,data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-bastion-access-binding-${RUN_ID}"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  request m6-bastion-host-current 200 GET "/enterprise/hosts/${M3_BASTION_HOST_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_BASTION_HOST_VERSION=$(jq -er '.resource_version' "$RESPONSE_FILE")
  request m6-bastion-host-reauthorize 201 POST "/enterprise/hosts/${M3_BASTION_HOST_ID}/actions/preview-update" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M6_BASTION_HOST_VERSION" '{labels:{team:"m3",route:"bastion"},expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-bastion-reauthorize-${RUN_ID}"
  M6_BASTION_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m6-bastion-host-reauthorize-confirm "$M6_BASTION_ACTION_REF"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  k -n "$SYSTEM_NS" scale deployment/argus-connector-gateway --replicas=2 >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-connector-gateway --timeout=180s >/dev/null
  local connector_owner peer peer_ip route client_name client_url client_pid status
  connector_owner=""
  for _ in $(seq 1 30); do
    connector_owner=$(m3_psql "SELECT gateway_instance_id FROM connector_sessions WHERE connector_id='${M3_SECOND_BASTION_CONNECTOR_ID}' ORDER BY connected_at DESC LIMIT 1;")
    if [[ -n "$connector_owner" ]] && k -n "$SYSTEM_NS" get pod "$connector_owner" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null | grep -q True; then
      break
    fi
    sleep 1
  done
  peer=$(k -n "$SYSTEM_NS" get pods -l app.kubernetes.io/name=argus-connector-gateway \
    -o jsonpath='{range .items[?(@.status.phase=="Running")]}{.metadata.name}{"\n"}{end}' | awk -v owner="$connector_owner" '$0 != owner {print; exit}')
  [[ -n "$connector_owner" && -n "$peer" && "$peer" != "$connector_owner" ]] || fail "could not select a non-owner Gateway Pod"
  peer_ip=$(k -n "$SYSTEM_NS" get pod "$peer" -o jsonpath='{.status.podIP}')
  [[ -n "$peer_ip" ]] || fail "non-owner Gateway Pod has no address"

  request m6-bastion-accounts 200 GET /enterprise/managed-accounts "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_BASTION_ACCOUNT_ID=$(jq -er --arg host "$M3_BASTION_HOST_ID" '.items[] | select(.host_id == $host) | .id' "$RESPONSE_FILE")
  request m6-bastion-grant 201 POST /enterprise/remote-access-grants "$ENTERPRISE_JAR" \
    "$(jq -nc --arg user "$ADMIN_USER_ID" --arg host "$M3_BASTION_HOST_ID" --arg account "$M6_BASTION_ACCOUNT_ID" '{subject_type:"user",subject_id:$user,host_ids:[$host],managed_account_ids:[$account],protocols:["ssh"],actions:["terminal"],valid_from:(now|todate),valid_until:((now+3600)|todate),enabled:true}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-bastion-grant-${RUN_ID}"
  M6_BASTION_GRANT_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-bastion-request 201 POST /enterprise/remote-access-requests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg host "$M3_BASTION_HOST_ID" --arg account "$M6_BASTION_ACCOUNT_ID" '{host_id:$host,managed_account_id:$account,protocol:"ssh",action:"terminal",reason:"M6 cross-Gateway Drain"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-bastion-request-${RUN_ID}"
  M6_BASTION_REQUEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-bastion-leases 200 GET /enterprise/remote-access-leases "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_BASTION_LEASE_ID=$(jq -er --arg request "$M6_BASTION_REQUEST_ID" '.items[] | select(.request_id == $request and .revoked == false) | .id' "$RESPONSE_FILE")
  request m6-bastion-session 201 POST /enterprise/remote-access-sessions "$ENTERPRISE_JAR" \
    "$(jq -nc --arg lease "$M6_BASTION_LEASE_ID" '{lease_id:$lease,terminal_cols:100,terminal_rows:30}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-bastion-session-${RUN_ID}"
  M6_BASTION_SESSION_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-bastion-ticket 201 POST "/enterprise/remote-access-sessions/${M6_BASTION_SESSION_ID}/tickets" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-bastion-ticket-${RUN_ID}"
  M6_BASTION_TICKET=$(jq -er '.ticket' "$RESPONSE_FILE")
  M6_BASTION_WSS_URL=$(jq -er '.websocket_url' "$RESPONSE_FILE")

  k -n "$SYSTEM_NS" exec statefulset/argus-redis -- redis-cli -a "$REDIS_PASSWORD" FLUSHALL >/dev/null 2>&1
  client_name="argus-m6-remote-client"
  client_url="ws://${peer_ip}:9445/v1/sessions/${M6_BASTION_SESSION_ID}"
  k -n "$SYSTEM_NS" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${client_name}
  labels:
    app.kubernetes.io/part-of: argus-m6-e2e
spec:
  restartPolicy: Never
  containers:
    - name: remoteclient
      image: ${M3_TARGET_IMAGE}
      imagePullPolicy: Never
      command: ["/usr/local/bin/argus-e2e-remoteclient"]
      args:
        - --url
        - ${client_url}
        - --origin
        - ${ENTERPRISE_ORIGIN}
        - --command
        - stream
        - --expect-status
        - terminated
        - --expect-reason
        - gateway_drain
        - --timeout
        - 90s
      stdin: true
      stdinOnce: true
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        allowPrivilegeEscalation: false
        capabilities: {drop: ["ALL"]}
      resources:
        requests: {cpu: 10m, memory: 16Mi}
        limits: {cpu: 100m, memory: 64Mi}
EOF
  k -n "$SYSTEM_NS" wait --for=condition=Ready "pod/${client_name}" --timeout=60s >/dev/null
  printf '%s\n' "$M6_BASTION_TICKET" | k -n "$SYSTEM_NS" attach -i "pod/${client_name}" \
    >"${ARTIFACT_DIR}/m6-cross-gateway-drain.log" 2>&1 &
  client_pid=$!
  unset M6_BASTION_TICKET
  route=""
  for _ in $(seq 1 30); do
    route=$(m3_psql "SELECT gateway_instance FROM remote_access_routes WHERE session_id='${M6_BASTION_SESSION_ID}';")
    [[ -n "$route" ]] && break
    sleep 1
  done
  [[ "$route" == "$peer" && "$route" != "$connector_owner" ]] || fail "remote session did not use the non-owner Gateway: owner=${connector_owner} route=${route} peer=${peer}"
  k -n "$SYSTEM_NS" delete pod "$peer" --wait=false >/dev/null
  wait "$client_pid" || fail "Gateway Drain did not terminate the cross-pod remote session cleanly"
  k -n "$SYSTEM_NS" delete pod "$client_name" --wait=true >/dev/null
  for _ in $(seq 1 30); do
    status=$(m3_psql "SELECT status || '|' || coalesce(termination_reason,'') FROM remote_access_sessions WHERE id='${M6_BASTION_SESSION_ID}';")
    [[ "$status" == "terminated|gateway_drain" ]] && break
    sleep 1
  done
  [[ "$status" == "terminated|gateway_drain" ]] || fail "Gateway Drain state did not persist: ${status}"
  k -n "$SYSTEM_NS" rollout status deployment/argus-connector-gateway --timeout=180s >/dev/null

  start_remote_port_forward port-forward-remote-recovered.log
}

run_m6_api_flow() {
  log "running M6 Grant, Lease, Ticket, SSH PTY, recording, and revocation flow"
  m6_install_winrs_target
  M6_OLD_SSH_HOST_KEY=$(m3_psql "SELECT pinned_host_key FROM hosts WHERE id='${M3_DIRECT_HOST_ID}';")
  request m6-ssh-host-key-test 202 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M3_CREDENTIAL_ID" --argjson port "$M3_DIRECT_SSH_PORT" '{address:"8.8.8.8",port:$port,platform:"linux",connection_mode:"direct_ssh",credential_id:$credential,username:"argus"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-ssh-host-key-test-${RUN_ID}"
  M6_SSH_HOST_KEY_TEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  m3_wait_connection_test m6-ssh-host-key-test-result "$M6_SSH_HOST_KEY_TEST_ID"
  M6_NEW_SSH_HOST_KEY=$(jq -er '.host_key_fingerprint' "$RESPONSE_FILE")
  [[ "$M6_NEW_SSH_HOST_KEY" != "$M6_OLD_SSH_HOST_KEY" ]] || fail "Direct SSH test target did not rotate its Host Key"
  request m6-ssh-host-current 200 GET "/enterprise/hosts/${M3_DIRECT_HOST_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_SSH_HOST_VERSION=$(jq -er '.resource_version' "$RESPONSE_FILE")
  request m6-ssh-host-key-preview 201 POST "/enterprise/hosts/${M3_DIRECT_HOST_ID}/actions/preview-update" "$ENTERPRISE_JAR" \
    "$(jq -nc --arg test "$M6_SSH_HOST_KEY_TEST_ID" --argjson version "$M6_SSH_HOST_VERSION" '{connection_test_id:$test,expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-ssh-host-key-preview-${RUN_ID}"
  M6_SSH_HOST_KEY_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m6-ssh-host-key-confirm "$M6_SSH_HOST_KEY_ACTION_REF"

  request m6-winrs-plaintext-denied 422 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M3_CREDENTIAL_ID" '{address:"8.8.8.8",port:5985,platform:"windows",connection_mode:"direct_winrm",credential_id:$credential,username:"argus"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-plaintext-${RUN_ID}"
  jq -e '.code == "WINRM_TLS_REQUIRED"' "$RESPONSE_FILE" >/dev/null

  request m6-winrs-secret 201 POST /enterprise/secrets "$ENTERPRISE_JAR" \
    '{"name":"m6-winrs-password","type":"winrm_password","description":"M6 E2E WinRS credential","value":"M6-e2e-winrs-password"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-secret-${RUN_ID}"
  M6_WINRS_SECRET_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-winrs-credential 201 POST /enterprise/credentials "$ENTERPRISE_JAR" \
    "$(jq -nc --arg id "$M6_WINRS_SECRET_ID" '{name:"m6-winrs",protocol:"winrm",username:"argus",secret_id:$id}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-credential-${RUN_ID}"
  M6_WINRS_CREDENTIAL_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-winrs-test 202 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M6_WINRS_CREDENTIAL_ID" '{address:"8.8.8.8",port:5986,platform:"windows",connection_mode:"direct_winrm",credential_id:$credential,username:"argus"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-test-${RUN_ID}"
  M6_WINRS_TEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  m3_wait_connection_test m6-winrs-test-result "$M6_WINRS_TEST_ID"
  request m6-winrs-preview 201 POST /enterprise/hosts/actions/preview-create "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M6_WINRS_CREDENTIAL_ID" --arg test "$M6_WINRS_TEST_ID" '{name:"m6-winrs-host",address:"8.8.8.8",port:5986,platform:"windows",connection_mode:"direct_winrm",credential_id:$credential,username:"argus",environment:"production",labels:{team:"m3",route:"direct",terminal:"winrs"},connection_test_id:$test}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-host-${RUN_ID}"
  M6_WINRS_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m6-winrs-confirm "$M6_WINRS_ACTION_REF"
  M6_WINRS_HOST_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m6-winrs-accounts 200 GET /enterprise/managed-accounts "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_WINRS_ACCOUNT_ID=$(jq -er --arg host "$M6_WINRS_HOST_ID" '.items[] | select(.host_id == $host) | .id' "$RESPONSE_FILE")

  request m6-grant 201 POST /enterprise/remote-access-grants "$ENTERPRISE_JAR" \
    "$(jq -nc --arg user "$ADMIN_USER_ID" --arg host "$M3_DIRECT_HOST_ID" --arg account "$M3_MANAGED_ACCOUNT_ID" '{subject_type:"user",subject_id:$user,host_ids:[$host],managed_account_ids:[$account],protocols:["ssh"],actions:["terminal"],valid_from:(now|todate),valid_until:((now+3600)|todate),enabled:true}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-grant-${RUN_ID}"
  M6_GRANT_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-request 201 POST /enterprise/remote-access-requests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg host "$M3_DIRECT_HOST_ID" --arg account "$M3_MANAGED_ACCOUNT_ID" '{host_id:$host,managed_account_id:$account,protocol:"ssh",action:"terminal",reason:"M6 E2E recorded PTY"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-request-${RUN_ID}"
  jq -e '.status == "authorized"' "$RESPONSE_FILE" >/dev/null
  M6_REQUEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-leases 200 GET /enterprise/remote-access-leases "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_LEASE_ID=$(jq -er --arg request "$M6_REQUEST_ID" '.items[] | select(.request_id == $request and .revoked == false) | .id' "$RESPONSE_FILE")
  request m6-session 201 POST /enterprise/remote-access-sessions "$ENTERPRISE_JAR" \
    "$(jq -nc --arg lease "$M6_LEASE_ID" '{lease_id:$lease,terminal_cols:100,terminal_rows:30}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-session-${RUN_ID}"
  M6_SESSION_ID=$(jq -er '.id' "$RESPONSE_FILE")
  M6_RECORDING_ID=$(jq -er '.recording_id' "$RESPONSE_FILE")
  request m6-ticket 201 POST "/enterprise/remote-access-sessions/${M6_SESSION_ID}/tickets" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-ticket-${RUN_ID}"
  jq -e '.protocol_version == "argus.remote_access/v1" and (.ticket | length) >= 43 and (.websocket_url | contains("ticket=") | not)' "$RESPONSE_FILE" >/dev/null
  M6_TICKET=$(jq -er '.ticket' "$RESPONSE_FILE")
  M6_WSS_URL=$(jq -er '.websocket_url' "$RESPONSE_FILE")
  if rg -F "$M6_TICKET" "$ARTIFACT_DIR" >/dev/null 2>&1; then fail "M6 Ticket leaked into diagnostics"; fi
  printf '%s\n' "$M6_TICKET" | go run ./tests/e2e/remoteclient --url "$M6_WSS_URL" --origin "$ENTERPRISE_ORIGIN" \
    >"${ARTIFACT_DIR}/m6-ssh-pty-client.log" 2>&1
  if printf '%s\n' "$M6_TICKET" | go run ./tests/e2e/remoteclient --url "$M6_WSS_URL" --origin "$ENTERPRISE_ORIGIN" \
    >"${ARTIFACT_DIR}/m6-ticket-replay.log" 2>&1; then
    fail "M6 Ticket replay unexpectedly succeeded"
  fi
  for _ in $(seq 1 30); do
    request m6-recording 200 GET "/enterprise/remote-access-recordings/${M6_RECORDING_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    [[ "$(jq -r '.status' "$RESPONSE_FILE")" == "available" ]] && break
    sleep 1
  done
  jq -e '.status == "available" and .chunk_count > 0 and .event_count >= 3' "$RESPONSE_FILE" >/dev/null
  request m6-recording-events 200 GET "/enterprise/remote-access-recordings/${M6_RECORDING_ID}/events" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '([.events[].type] | index("i") != null) and ([.events[].type] | index("o") != null) and ([.events[].type] | index("r") != null)' "$RESPONSE_FILE" >/dev/null

  request m6-winrs-grant 201 POST /enterprise/remote-access-grants "$ENTERPRISE_JAR" \
    "$(jq -nc --arg user "$ADMIN_USER_ID" --arg host "$M6_WINRS_HOST_ID" --arg account "$M6_WINRS_ACCOUNT_ID" '{subject_type:"user",subject_id:$user,host_ids:[$host],managed_account_ids:[$account],protocols:["winrs"],actions:["terminal"],valid_from:(now|todate),valid_until:((now+3600)|todate),enabled:true}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-grant-${RUN_ID}"
  M6_WINRS_GRANT_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-winrs-request 201 POST /enterprise/remote-access-requests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg host "$M6_WINRS_HOST_ID" --arg account "$M6_WINRS_ACCOUNT_ID" '{host_id:$host,managed_account_id:$account,protocol:"winrs",action:"terminal",reason:"M6 E2E WinRS PowerShell line mode"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-request-${RUN_ID}"
  M6_WINRS_REQUEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-winrs-leases 200 GET /enterprise/remote-access-leases "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_WINRS_LEASE_ID=$(jq -er --arg request "$M6_WINRS_REQUEST_ID" '.items[] | select(.request_id == $request and .revoked == false) | .id' "$RESPONSE_FILE")
  request m6-winrs-session 201 POST /enterprise/remote-access-sessions "$ENTERPRISE_JAR" \
    "$(jq -nc --arg lease "$M6_WINRS_LEASE_ID" '{lease_id:$lease,terminal_cols:100,terminal_rows:30}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-session-${RUN_ID}"
  M6_WINRS_SESSION_ID=$(jq -er '.id' "$RESPONSE_FILE")
  M6_WINRS_RECORDING_ID=$(jq -er '.recording_id' "$RESPONSE_FILE")
  request m6-winrs-ticket 201 POST "/enterprise/remote-access-sessions/${M6_WINRS_SESSION_ID}/tickets" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-winrs-ticket-${RUN_ID}"
  M6_WINRS_TICKET=$(jq -er '.ticket' "$RESPONSE_FILE")
  M6_WINRS_WSS_URL=$(jq -er '.websocket_url' "$RESPONSE_FILE")
  printf '%s\n' "$M6_WINRS_TICKET" | go run ./tests/e2e/remoteclient --url "$M6_WINRS_WSS_URL" --origin "$ENTERPRISE_ORIGIN" \
    --command whoami --expect 'argus\m6-e2e' >"${ARTIFACT_DIR}/m6-winrs-client.log" 2>&1
  for _ in $(seq 1 30); do
    request m6-winrs-recording 200 GET "/enterprise/remote-access-recordings/${M6_WINRS_RECORDING_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    [[ "$(jq -r '.status' "$RESPONSE_FILE")" == "available" ]] && break
    sleep 1
  done
  jq -e '.status == "available" and .chunk_count > 0 and .event_count >= 3' "$RESPONSE_FILE" >/dev/null
  request m6-winrs-recording-events 200 GET "/enterprise/remote-access-recordings/${M6_WINRS_RECORDING_ID}/events" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '([.events[].type] | index("i") != null) and ([.events[].type] | index("o") != null) and ([.events[].type] | index("r") != null)' "$RESPONSE_FILE" >/dev/null

  m6_cross_gateway_flow

  request m6-session-stale-lease 409 POST /enterprise/remote-access-sessions "$ENTERPRISE_JAR" \
    "$(jq -nc --arg lease "$M6_LEASE_ID" '{lease_id:$lease,terminal_cols:100,terminal_rows:30}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-session-stale-lease-${RUN_ID}"
  jq -e '.code == "AUTHORIZATION_VERSION_STALE"' "$RESPONSE_FILE" >/dev/null
  request m6-request-post-drain 201 POST /enterprise/remote-access-requests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg host "$M3_DIRECT_HOST_ID" --arg account "$M3_MANAGED_ACCOUNT_ID" '{host_id:$host,managed_account_id:$account,protocol:"ssh",action:"terminal",reason:"M6 post-Drain authorization refresh"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-request-post-drain-${RUN_ID}"
  M6_POST_DRAIN_REQUEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-leases-post-drain 200 GET /enterprise/remote-access-leases "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M6_POST_DRAIN_LEASE_ID=$(jq -er --arg request "$M6_POST_DRAIN_REQUEST_ID" '.items[] | select(.request_id == $request and .revoked == false) | .id' "$RESPONSE_FILE")

  request m6-session-terminate 201 POST /enterprise/remote-access-sessions "$ENTERPRISE_JAR" \
    "$(jq -nc --arg lease "$M6_POST_DRAIN_LEASE_ID" '{lease_id:$lease,terminal_cols:100,terminal_rows:30}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-session-terminate-${RUN_ID}"
  M6_TERMINATE_SESSION_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-ticket-terminate 201 POST "/enterprise/remote-access-sessions/${M6_TERMINATE_SESSION_ID}/tickets" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-ticket-terminate-${RUN_ID}"
  M6_TERMINATED_TICKET=$(jq -er '.ticket' "$RESPONSE_FILE")
  M6_TERMINATED_WSS_URL=$(jq -er '.websocket_url' "$RESPONSE_FILE")
  request m6-terminate 200 POST "/enterprise/remote-access-sessions/${M6_TERMINATE_SESSION_ID}/terminate" "$ENTERPRISE_JAR" '{"reason":"e2e_terminate"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-terminate-${RUN_ID}"
  if printf '%s\n' "$M6_TERMINATED_TICKET" | go run ./tests/e2e/remoteclient --url "$M6_TERMINATED_WSS_URL" --origin "$ENTERPRISE_ORIGIN" \
    >"${ARTIFACT_DIR}/m6-terminated-ticket.log" 2>&1; then
    fail "terminated M6 Session accepted an unused Ticket"
  fi

  request m6-session-object-store 201 POST /enterprise/remote-access-sessions "$ENTERPRISE_JAR" \
    "$(jq -nc --arg lease "$M6_POST_DRAIN_LEASE_ID" '{lease_id:$lease,terminal_cols:100,terminal_rows:30}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-session-object-store-${RUN_ID}"
  M6_STORE_SESSION_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m6-ticket-object-store 201 POST "/enterprise/remote-access-sessions/${M6_STORE_SESSION_ID}/tickets" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-ticket-object-store-${RUN_ID}"
  M6_STORE_TICKET=$(jq -er '.ticket' "$RESPONSE_FILE")
  M6_STORE_WSS_URL=$(jq -er '.websocket_url' "$RESPONSE_FILE")
  k -n "$SYSTEM_NS" scale deployment/argus-minio --replicas=0 >/dev/null
  k -n "$SYSTEM_NS" wait --for=delete pod -l app.kubernetes.io/name=argus-minio --timeout=120s >/dev/null
  printf '%s\n' "$M6_STORE_TICKET" | go run ./tests/e2e/remoteclient --url "$M6_STORE_WSS_URL" --origin "$ENTERPRISE_ORIGIN" \
    --command stream --expect-status failed --expect-reason REMOTE_ACCESS_RECORDING_UNAVAILABLE --timeout 90s \
    >"${ARTIFACT_DIR}/m6-object-store-fail-closed.log" 2>&1
  k -n "$SYSTEM_NS" scale deployment/argus-minio --replicas=1 >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-minio --timeout=180s >/dev/null

  run_m6_playwright

  request m6-disable-grant 204 DELETE "/enterprise/remote-access-grants/${M6_GRANT_ID}" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-disable-${RUN_ID}"
  request m6-disable-winrs-grant 204 DELETE "/enterprise/remote-access-grants/${M6_WINRS_GRANT_ID}" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-disable-winrs-${RUN_ID}"
  request m6-disable-bastion-grant 204 DELETE "/enterprise/remote-access-grants/${M6_BASTION_GRANT_ID}" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m6-disable-bastion-${RUN_ID}"
  unset M6_TICKET M6_WINRS_TICKET M6_TERMINATED_TICKET M6_STORE_TICKET
}
