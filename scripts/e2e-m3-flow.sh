#!/usr/bin/env bash

M3_CONNECTOR_PIDS=()
M3_TARGET_IMAGE="argus/argus-e2e-ssh:m3-${RUN_ID}"
M3_GATEWAY_PORT=${ARGUS_E2E_CONNECTOR_GATEWAY_PORT:-4193}
M3_SSH_PORT=${ARGUS_E2E_SSH_PORT:-4222}
M3_DIRECT_SSH_PORT=2222

prepare_m3_dependencies() {
  log "building M3 SSH target image ${M3_TARGET_IMAGE}"
  retry 3 docker build --quiet -f deploy/docker/e2e-ssh.Dockerfile -t "$M3_TARGET_IMAGE" . >/dev/null
  case "$KUBE_CONTEXT" in
    kind-*) kind load docker-image --name "${KUBE_CONTEXT#kind-}" "$M3_TARGET_IMAGE" ;;
    minikube) minikube image load "$M3_TARGET_IMAGE" ;;
    docker-desktop)
      local node
      node=$(k get nodes -o jsonpath='{.items[0].metadata.name}')
      docker save "$M3_TARGET_IMAGE" | docker exec -i "$node" ctr -n k8s.io images import - >/dev/null
      ;;
  esac
}

cleanup_m3() {
  local pid
  for pid in "${M3_CONNECTOR_PIDS[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  done
  docker image rm "$M3_TARGET_IMAGE" >/dev/null 2>&1 || true
}

m3_mutation_headers() {
  printf '%s\n' "Origin: ${ENTERPRISE_ORIGIN}" "X-CSRF-Token: ${ENTERPRISE_CSRF}"
}

m3_psql() {
  k -n "$SYSTEM_NS" exec statefulset/argus-postgresql -- env PGPASSWORD="$POSTGRES_PASSWORD" \
    psql -v ON_ERROR_STOP=1 -At -U argus -d argus -c "$1"
}

m3_confirm() {
  local name=$1 action_ref=$2
  request "$name" 200 POST "/enterprise/pending-actions/${action_ref}/confirm" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" \
    --header "Idempotency-Key: ${name}-${RUN_ID}"
  jq -e '.pending_action.status == "succeeded"' "$RESPONSE_FILE" >/dev/null
}

m3_wait_connection_test() {
  local name=$1 id=$2 status
  for _ in $(seq 1 120); do
    request "$name" 200 GET "/enterprise/connection-tests/${id}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    status=$(jq -er '.status' "$RESPONSE_FILE")
    if [[ "$status" == "succeeded" ]]; then
      return
    fi
    if [[ "$status" != "queued" && "$status" != "running" ]]; then
      jq -c '{status,error_code,checks}' "$RESPONSE_FILE" >&2
      fail "${name} (${id}) ended as ${status}"
    fi
    sleep 1
  done
  fail "${name} (${id}) timed out"
}

m3_install_target() {
  cat <<EOF | k apply -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata: {name: argus-m3-ssh-target, namespace: ${SYSTEM_NS}}
spec:
  replicas: 1
  selector: {matchLabels: {app.kubernetes.io/name: argus-m3-ssh-target}}
  template:
    metadata: {labels: {app.kubernetes.io/name: argus-m3-ssh-target, app.kubernetes.io/part-of: argus-m3-e2e}}
    spec:
      containers:
        - name: ssh
          image: ${M3_TARGET_IMAGE}
          imagePullPolicy: Never
          env: [{name: ARGUS_E2E_SSH_PASSWORD, value: "M3-e2e-ssh-password"}]
          ports: [{name: ssh, containerPort: 2222}]
          readinessProbe: {tcpSocket: {port: ssh}, initialDelaySeconds: 1, periodSeconds: 1}
          resources: {requests: {cpu: 10m, memory: 16Mi}, limits: {cpu: 100m, memory: 64Mi}}
---
apiVersion: v1
kind: Service
metadata: {name: argus-m3-ssh-target, namespace: ${SYSTEM_NS}}
spec:
  selector: {app.kubernetes.io/name: argus-m3-ssh-target}
  ports: [{name: ssh, port: 22, targetPort: ssh}]
---
apiVersion: v1
kind: ServiceAccount
metadata: {name: argus-m3-kubernetes-connector, namespace: ${SYSTEM_NS}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: argus-m3-kubernetes-connector-${RUN_ID}}
rules:
  - apiGroups: [""]
    resources: [namespaces, nodes, pods, pods/log, services]
    verbs: [get, list]
  - apiGroups: [apps]
    resources: [deployments, statefulsets, daemonsets]
    verbs: [get, list]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: argus-m3-kubernetes-connector-${RUN_ID}}
subjects: [{kind: ServiceAccount, name: argus-m3-kubernetes-connector, namespace: ${SYSTEM_NS}}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: argus-m3-kubernetes-connector-${RUN_ID}}
EOF
  k -n "$SYSTEM_NS" rollout status deployment/argus-m3-ssh-target --timeout=180s >/dev/null
  local direct_patch
  direct_patch=$(jq -nc --arg image "$M3_TARGET_IMAGE" '{spec:{template:{spec:{initContainers:[{
    name:"argus-m3-direct-address",image:$image,imagePullPolicy:"Never",
    command:["/sbin/ip","addr","add","8.8.8.8/32","dev","lo"],
    securityContext:{runAsNonRoot:false,runAsUser:0,allowPrivilegeEscalation:false,capabilities:{add:["NET_ADMIN"],drop:["ALL"]}}
  }],containers:[{
    name:"argus-m3-direct-target",image:$image,imagePullPolicy:"Never",
    env:[{name:"ARGUS_E2E_SSH_PASSWORD",value:"M3-e2e-ssh-password"}],
    ports:[{name:"e2e-direct-ssh",containerPort:2222}],
    securityContext:{runAsNonRoot:true,runAsUser:65532,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}},
    resources:{requests:{cpu:"10m",memory:"16Mi"},limits:{cpu:"100m",memory:"64Mi"}}
  }]}}}}')
  k -n "$SYSTEM_NS" patch deployment argus-direct-executor --type=strategic --patch "$direct_patch" >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-direct-executor --timeout=180s >/dev/null
  k -n "$SYSTEM_NS" port-forward service/argus-m3-ssh-target "${M3_SSH_PORT}:22" >"${ARTIFACT_DIR}/port-forward-ssh.log" 2>&1 &
  PF_PIDS+=("$!")
  for _ in $(seq 1 30); do
    nc -z 127.0.0.1 "$M3_SSH_PORT" >/dev/null 2>&1 && return
    sleep 1
  done
  fail "M3 SSH target port-forward did not become ready"
}

m3_start_gateway_forward() {
  assert_port_free "$M3_GATEWAY_PORT"
  k -n "$SYSTEM_NS" port-forward service/argus-connector-gateway "${M3_GATEWAY_PORT}:9443" >"${ARTIFACT_DIR}/port-forward-gateway.log" 2>&1 &
  PF_PIDS+=("$!")
  for _ in $(seq 1 60); do
    nc -z 127.0.0.1 "$M3_GATEWAY_PORT" >/dev/null 2>&1 && return
    sleep 1
  done
  fail "Connector Gateway port-forward did not become ready"
}

m3_enroll_local_connector() {
  local command=$1 directory=$2 name=$3 connector_id token server role tampered_dir conflict_dir
  connector_id=$(awk '{for(i=1;i<=NF;i++) if($i=="--connector-id") print $(i+1)}' <<<"$command")
  token=$(awk '{for(i=1;i<=NF;i++) if($i=="--token") print $(i+1)}' <<<"$command")
  server=$(awk '{for(i=1;i<=NF;i++) if($i=="--server") print $(i+1)}' <<<"$command")
  role=$(awk '{for(i=1;i<=NF;i++) if($i=="--role") print $(i+1)}' <<<"$command")
  [[ -n "$connector_id" && -n "$token" && -n "$server" && -n "$role" ]] || fail "invalid one-time Connector install command"
  tampered_dir="${directory}-tampered"
  if ARGUS_CONNECTOR_INSTANCE_ID="${name}-${RUN_ID}" "${WORK_DIR}/argus-connector" enroll \
    --connector-id 00000000-0000-4000-8000-000000000001 --token "$token" --server "$server" --role "$role" \
    --name "$name" --data-dir "$tampered_dir" >"${ARTIFACT_DIR}/${name}-tampered-csr.log" 2>&1; then
    fail "Connector enrollment accepted a CSR for a different Connector ID"
  fi
  grep -Fq 'HTTP 401' "${ARTIFACT_DIR}/${name}-tampered-csr.log" || fail "tampered Connector CSR did not return stable rejection"
  ARGUS_CONNECTOR_INSTANCE_ID="${name}-${RUN_ID}" "${WORK_DIR}/argus-connector" enroll --connector-id "$connector_id" --token "$token" --server "$server" --role "$role" \
    --name "$name" --data-dir "$directory" >"${ARTIFACT_DIR}/${name}-enroll.log" 2>&1
  ARGUS_CONNECTOR_INSTANCE_ID="${name}-${RUN_ID}" "${WORK_DIR}/argus-connector" enroll --connector-id "$connector_id" --token "$token" --server "$server" --role "$role" \
    --name "$name" --data-dir "$directory" >"${ARTIFACT_DIR}/${name}-enroll-retry.log" 2>&1
  conflict_dir="${directory}-conflict"
  if ARGUS_CONNECTOR_INSTANCE_ID="${name}-${RUN_ID}" "${WORK_DIR}/argus-connector" enroll --connector-id "$connector_id" --token "$token" --server "$server" --role "$role" \
    --name "$name" --data-dir "$conflict_dir" >"${ARTIFACT_DIR}/${name}-enroll-conflict.log" 2>&1; then
    fail "consumed Connector token was reusable with a different key"
  fi
  grep -Fq 'HTTP 409' "${ARTIFACT_DIR}/${name}-enroll-conflict.log" || fail "Connector key conflict did not return stable 409"
  M3_ENROLLED_CONNECTOR_ID=$connector_id
}

m3_wait_connector_online() {
  local id=$1 minimum_epoch=${2:-1}
  for _ in $(seq 1 120); do
    request connector-online 200 GET /enterprise/connectors "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    if jq -e --arg id "$id" --argjson epoch "$minimum_epoch" '.items[] | select(.id == $id and .status == "online" and .connection_epoch >= $epoch)' "$RESPONSE_FILE" >/dev/null; then
      return
    fi
    sleep 1
  done
  fail "Connector ${id} did not become online"
}

m3_wait_certificate_rotation() {
  connector_id=$1
  previous_version=$2
  for _ in $(seq 1 60); do
    request m3-kubernetes-connector-after-rotation 200 GET /enterprise/connectors "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    if jq -e --arg id "$connector_id" --argjson version "$previous_version" \
      '.items[] | select(.id == $id and .version >= ($version + 2) and .certificate_rotation_requested_at == null)' \
      "$RESPONSE_FILE" >/dev/null; then
      return
    fi
    sleep 1
  done
  fail "rotated Kubernetes Connector version did not advance or rotation remained pending"
}

m3_deploy_kubernetes_connector() {
  local connector_id=$1 directory=$2
  local service_endpoint="grpcs://argus-connector-gateway.${SYSTEM_NS}.svc:9443"
  jq --arg endpoint "$service_endpoint" '.gateway_endpoint = $endpoint' "${directory}/identity.json" >"${directory}/identity.pod.json"
  k -n "$SYSTEM_NS" create secret generic argus-m3-kubernetes-connector-identity \
    --from-file=identity.json="${directory}/identity.pod.json" \
    --from-file=connector-key.pem="${directory}/connector-key.pem" \
    --from-file=connector-cert.pem="${directory}/connector-cert.pem" \
    --from-file=connector-ca.pem="${directory}/connector-ca.pem" >/dev/null
  cat <<EOF | k apply -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata: {name: argus-m3-kubernetes-connector, namespace: ${SYSTEM_NS}}
spec:
  replicas: 1
  selector: {matchLabels: {app.kubernetes.io/name: argus-m3-kubernetes-connector}}
  template:
    metadata: {labels: {app.kubernetes.io/name: argus-m3-kubernetes-connector, app.kubernetes.io/part-of: argus-m3-e2e}}
    spec:
      serviceAccountName: argus-m3-kubernetes-connector
      initContainers:
        - name: identity
          image: busybox:1.37.0
          command: [sh, -c, "cp /identity/* /data/ && chown -R 65532:65532 /data && chmod 700 /data && chmod 600 /data/*"]
          volumeMounts: [{name: identity, mountPath: /identity}, {name: data, mountPath: /data}]
      containers:
        - name: connector
          image: ${BACKEND_IMAGE}
          imagePullPolicy: Never
          command: [/usr/local/bin/argus-connector]
          args: [run, --data-dir=/var/lib/argus-connector]
          volumeMounts: [{name: data, mountPath: /var/lib/argus-connector}]
          resources: {requests: {cpu: 10m, memory: 32Mi}, limits: {cpu: 250m, memory: 128Mi}}
      volumes:
        - name: identity
          secret: {secretName: argus-m3-kubernetes-connector-identity, defaultMode: 256}
        - name: data
          emptyDir: {}
EOF
  k -n "$SYSTEM_NS" rollout status deployment/argus-m3-kubernetes-connector --timeout=180s >/dev/null
  m3_wait_connector_online "$connector_id"
}

run_m3_api_flow() {
  log "running M3 Secret, resource, Connector, DataScope, and revocation flow"
  command -v nc >/dev/null 2>&1 || fail "required command is missing: nc"
  go build -o "${WORK_DIR}/argus-connector" ./cmd/argus-connector
  m3_install_target
  k -n "$SYSTEM_NS" scale deployment/argus-connector-gateway --replicas=2 >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-connector-gateway --timeout=300s >/dev/null
  m3_start_gateway_forward

  request m3-secret-create 201 POST /enterprise/secrets "$ENTERPRISE_JAR" \
    '{"name":"m3-ssh-password","type":"ssh_password","description":"M3 E2E write-only secret","value":"M3-e2e-ssh-password"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-secret-${RUN_ID}"
  M3_SECRET_ID=$(jq -er '.id' "$RESPONSE_FILE")
  M3_SECRET_VERSION=$(jq -er '.version' "$RESPONSE_FILE")
  jq -e 'has("value") | not' "$RESPONSE_FILE" >/dev/null
  request m3-credential-create 201 POST /enterprise/credentials "$ENTERPRISE_JAR" \
    "$(jq -nc --arg id "$M3_SECRET_ID" '{name:"m3-ssh",protocol:"ssh",username:"argus",secret_id:$id}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-credential-${RUN_ID}"
  M3_CREDENTIAL_ID=$(jq -er '.id' "$RESPONSE_FILE")
  M3_KUBE_API_SERVER=$(k config view --raw --minify --flatten -o jsonpath='{.clusters[0].cluster.server}')
  M3_KUBE_CA_DATA=$(k config view --raw --minify --flatten -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
  M3_KUBE_TOKEN=$(k -n "$SYSTEM_NS" create token argus-m3-kubernetes-connector --duration=1h)
  [[ -n "$M3_KUBE_API_SERVER" && -n "$M3_KUBE_CA_DATA" && -n "$M3_KUBE_TOKEN" ]] || fail "could not build static M3 kubeconfig"
  M3_KUBECONFIG=$(jq -rn --arg server "$M3_KUBE_API_SERVER" --arg ca "$M3_KUBE_CA_DATA" --arg token "$M3_KUBE_TOKEN" --arg namespace "$SYSTEM_NS" '
    "apiVersion: v1\nkind: Config\nclusters:\n- name: m3\n  cluster:\n    server: \($server)\n    certificate-authority-data: \($ca)\ncontexts:\n- name: m3\n  context:\n    cluster: m3\n    user: m3\n    namespace: \($namespace)\ncurrent-context: m3\nusers:\n- name: m3\n  user:\n    token: \($token)"')
  unset M3_KUBE_TOKEN
  request m3-kubeconfig-secret-create 201 POST /enterprise/secrets "$ENTERPRISE_JAR" \
    "$(jq -nc --arg value "$M3_KUBECONFIG" '{name:"m3-kubeconfig",type:"kubeconfig",description:"M3 E2E Kubernetes access",value:$value}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-kubeconfig-secret-${RUN_ID}"
  M3_KUBECONFIG_SECRET_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m3-kubeconfig-credential-create 201 POST /enterprise/credentials "$ENTERPRISE_JAR" \
    "$(jq -nc --arg id "$M3_KUBECONFIG_SECRET_ID" '{name:"m3-kubernetes",protocol:"kubernetes",secret_id:$id}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-kubeconfig-credential-${RUN_ID}"
  M3_KUBERNETES_CREDENTIAL_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m3-kubernetes-direct-private-denied 403 POST /enterprise/kubernetes-clusters/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M3_KUBERNETES_CREDENTIAL_ID" '{api_server:"https://127.0.0.1:6443",connection_mode:"direct",credential_id:$credential}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-kubernetes-direct-denied-${RUN_ID}"
  jq -e '.code == "DIRECT_TARGET_DENIED"' "$RESPONSE_FILE" >/dev/null

  request m3-admin-scope-create 201 POST /enterprise/data-scopes "$ENTERPRISE_JAR" \
    '{"name":"M3 administration scope","resource_types":["host","kubernetes_cluster","kubernetes_namespace"],"explicit_resource_ids":[],"label_selector":{"schema_version":"argus.label_selector/v1","requirements":[{"key":"team","operator":"eq","values":["m3"]}]}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-admin-scope-${RUN_ID}"
  M3_ADMIN_SCOPE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  M3_ADMIN_SCOPE_VERSION=$(jq -er '.version' "$RESPONSE_FILE")
  request m3-roles 200 GET /enterprise/roles "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_RESOURCE_ADMIN_ROLE_ID=$(jq -er '.items[] | select(.builtin == true and .name == "Resource Admin") | .id' "$RESPONSE_FILE")
  request m3-admin-binding-create 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
    "$(jq -nc --arg subject "$ADMIN_USER_ID" --arg role "$M3_RESOURCE_ADMIN_ROLE_ID" --arg scope "$M3_ADMIN_SCOPE_ID" '{subject_type:"user",subject_id:$subject,role_id:$role,data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-admin-binding-${RUN_ID}"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  request m3-bastion-preview 201 POST /enterprise/bastion-scopes/actions/preview-create "$ENTERPRISE_JAR" \
    '{"name":"m3-bastion","environment":"production","labels":{"team":"m3","route":"bastion"}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-bastion-confirm "$M3_ACTION_REF"
  M3_BASTION_SCOPE_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
  M3_BASTION_COMMAND=$(jq -er '.enrollment.install_command' "$RESPONSE_FILE")
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  BASTION_DIR="${WORK_DIR}/bastion-connector"
  m3_enroll_local_connector "$M3_BASTION_COMMAND" "$BASTION_DIR" m3-bastion
  M3_BASTION_CONNECTOR_ID=$M3_ENROLLED_CONNECTOR_ID
  "${WORK_DIR}/argus-connector" run --data-dir "$BASTION_DIR" >"${ARTIFACT_DIR}/bastion-connector.log" 2>&1 &
  M3_BASTION_CONNECTOR_PID=$!
  M3_CONNECTOR_PIDS+=("$M3_BASTION_CONNECTOR_PID")
  m3_wait_connector_online "$M3_BASTION_CONNECTOR_ID"

  request m3-bastion-kubernetes-test 202 POST /enterprise/kubernetes-clusters/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg server "$M3_KUBE_API_SERVER" --arg scope "$M3_BASTION_SCOPE_ID" --arg credential "$M3_KUBERNETES_CREDENTIAL_ID" '{api_server:$server,connection_mode:"via_bastion",bastion_scope_id:$scope,credential_id:$credential}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-kubernetes-test-${RUN_ID}"
  M3_BASTION_KUBERNETES_TEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  m3_wait_connection_test m3-bastion-kubernetes-test-result "$M3_BASTION_KUBERNETES_TEST_ID"
  request m3-bastion-kubernetes-preview 201 POST /enterprise/kubernetes-clusters/actions/preview-create "$ENTERPRISE_JAR" \
    "$(jq -nc --arg server "$M3_KUBE_API_SERVER" --arg scope "$M3_BASTION_SCOPE_ID" --arg credential "$M3_KUBERNETES_CREDENTIAL_ID" --arg test "$M3_BASTION_KUBERNETES_TEST_ID" '{name:"m3-via-bastion",api_server:$server,connection_mode:"via_bastion",bastion_scope_id:$scope,credential_id:$credential,default_namespace:"default",environment:"production",labels:{team:"m3",route:"bastion"},connection_test_id:$test}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-kubernetes-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-bastion-kubernetes-confirm "$M3_ACTION_REF"
  M3_BASTION_CLUSTER_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  request m3-bastion-command-epoch 200 GET /enterprise/connectors "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_BASTION_EPOCH=$(jq -er --arg id "$M3_BASTION_CONNECTOR_ID" '.items[] | select(.id == $id) | .connection_epoch' "$RESPONSE_FILE")
  m3_psql "INSERT INTO connector_commands (id,command_id,enterprise_id,connector_id,connection_epoch,operation_ref,command_type,payload_schema_version,payload,payload_hash,idempotency_key,status,expires_at) VALUES
    (gen_random_uuid(),'m3-expired-${RUN_ID}',(SELECT enterprise_id FROM connectors WHERE id='${M3_BASTION_CONNECTOR_ID}'),'${M3_BASTION_CONNECTOR_ID}',${M3_BASTION_EPOCH},gen_random_uuid()::text,'connector_uninstall','argus.connector_command/v1','{}',decode(repeat('00',32),'hex'),'m3-expired-${RUN_ID}','queued',now()-interval '1 second'),
    (gen_random_uuid(),'m3-timeout-${RUN_ID}',(SELECT enterprise_id FROM connectors WHERE id='${M3_BASTION_CONNECTOR_ID}'),'${M3_BASTION_CONNECTOR_ID}',${M3_BASTION_EPOCH},gen_random_uuid()::text,'connector_uninstall','argus.connector_command/v1','{}',decode(repeat('00',32),'hex'),'m3-timeout-${RUN_ID}','running',now()-interval '1 second');" >/dev/null
  for _ in $(seq 1 12); do
    M3_COMMAND_STATES=$(m3_psql "SELECT string_agg(command_id || ':' || status, ',' ORDER BY command_id) FROM connector_commands WHERE command_id IN ('m3-expired-${RUN_ID}','m3-timeout-${RUN_ID}');")
    if [[ "$M3_COMMAND_STATES" == *"m3-expired-${RUN_ID}:expired"* && "$M3_COMMAND_STATES" == *"m3-timeout-${RUN_ID}:timed_out"* ]]; then
      break
    fi
    sleep 5
  done
  [[ "$M3_COMMAND_STATES" == *"m3-expired-${RUN_ID}:expired"* && "$M3_COMMAND_STATES" == *"m3-timeout-${RUN_ID}:timed_out"* ]] || fail "Connector command sweeper did not converge: ${M3_COMMAND_STATES}"

  request m3-bastion-host-test 202 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg scope "$M3_BASTION_SCOPE_ID" --arg credential "$M3_CREDENTIAL_ID" --argjson port "$M3_SSH_PORT" '{address:"127.0.0.1",port:$port,platform:"linux",connection_mode:"via_bastion",bastion_scope_id:$scope,credential_id:$credential,username:"argus"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-test-${RUN_ID}"
  M3_BASTION_TEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  m3_wait_connection_test m3-bastion-host-test-result "$M3_BASTION_TEST_ID"
  request m3-bastion-host-preview 201 POST /enterprise/hosts/actions/preview-create "$ENTERPRISE_JAR" \
    "$(jq -nc --arg scope "$M3_BASTION_SCOPE_ID" --arg credential "$M3_CREDENTIAL_ID" --arg test "$M3_BASTION_TEST_ID" --argjson port "$M3_SSH_PORT" '{name:"m3-private-host",address:"127.0.0.1",port:$port,platform:"linux",connection_mode:"via_bastion",bastion_scope_id:$scope,credential_id:$credential,username:"argus",environment:"production",labels:{team:"m3",route:"bastion"},connection_test_id:$test}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-host-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-bastion-host-confirm "$M3_ACTION_REF"
  M3_BASTION_HOST_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  request m3-direct-host-test 202 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M3_CREDENTIAL_ID" --argjson port "$M3_DIRECT_SSH_PORT" '{address:"8.8.8.8",port:$port,platform:"linux",connection_mode:"direct_ssh",credential_id:$credential,username:"argus"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-direct-test-${RUN_ID}"
  M3_DIRECT_TEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  m3_wait_connection_test m3-direct-host-test-result "$M3_DIRECT_TEST_ID"
  request m3-direct-host-test-reuse 422 POST /enterprise/hosts/actions/preview-create "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M3_CREDENTIAL_ID" --arg test "$M3_DIRECT_TEST_ID" --argjson port "$M3_DIRECT_SSH_PORT" '{name:"m3-reused-test",address:"8.8.4.4",port:$port,platform:"linux",connection_mode:"direct_ssh",credential_id:$credential,username:"argus",environment:"production",labels:{team:"m3",route:"direct"},connection_test_id:$test}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-reused-test-${RUN_ID}"
  jq -e '.code == "CONNECTION_TEST_REQUIRED"' "$RESPONSE_FILE" >/dev/null
  request m3-direct-host-preview 201 POST /enterprise/hosts/actions/preview-create "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M3_CREDENTIAL_ID" --arg test "$M3_DIRECT_TEST_ID" --argjson port "$M3_DIRECT_SSH_PORT" '{name:"m3-public-host",address:"8.8.8.8",port:$port,platform:"linux",connection_mode:"direct_ssh",credential_id:$credential,username:"argus",environment:"production",labels:{team:"m3",route:"direct"},connection_test_id:$test}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-direct-host-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-direct-host-confirm "$M3_ACTION_REF"
  M3_DIRECT_HOST_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m3-cross-enterprise-host 404 GET "/enterprise/hosts/${M3_DIRECT_HOST_ID}" "$OTHER_ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request m3-cross-enterprise-host-missing 404 GET /enterprise/hosts/00000000-0000-0000-0000-000000000001 "$OTHER_ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request m3-managed-accounts 200 GET /enterprise/managed-accounts "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_MANAGED_ACCOUNT_ID=$(jq -er --arg host "$M3_DIRECT_HOST_ID" '.items[] | select(.host_id == $host) | .id' "$RESPONSE_FILE")
  M3_MANAGED_ACCOUNT_VERSION=$(jq -er --arg id "$M3_MANAGED_ACCOUNT_ID" '.items[] | select(.id == $id) | .version' "$RESPONSE_FILE")
  request m3-managed-account-update 200 PUT "/enterprise/managed-accounts/${M3_MANAGED_ACCOUNT_ID}" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M3_MANAGED_ACCOUNT_VERSION" '{privilege_level:"standard",expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  jq -e '.privilege_level == "standard"' "$RESPONSE_FILE" >/dev/null
  request m3-cancel-preview 201 POST "/enterprise/hosts/${M3_DIRECT_HOST_ID}/actions/preview-update" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$(jq -er '.resource_ref.version' "${WORK_DIR}/m3-direct-host-confirm.json")" '{name:"m3-public-host",expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-cancel-preview-${RUN_ID}"
  M3_CANCEL_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  for attempt in first retry; do
    request "m3-cancel-${attempt}" 200 POST "/enterprise/pending-actions/${M3_CANCEL_ACTION_REF}/cancel" "$ENTERPRISE_JAR" - \
      --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-cancel-${RUN_ID}"
    jq -e '.status == "cancelled"' "$RESPONSE_FILE" >/dev/null
  done
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  request m3-in-cluster-preview 201 POST /enterprise/kubernetes-clusters/actions/preview-create "$ENTERPRISE_JAR" \
    '{"name":"m3-in-cluster","api_server":"https://kubernetes.default.svc","connection_mode":"in_cluster","default_namespace":"default","environment":"production","labels":{"team":"m3","route":"in-cluster"}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-in-cluster-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-in-cluster-confirm "$M3_ACTION_REF"
  M3_CLUSTER_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
  M3_KUBERNETES_COMMAND=$(jq -er '.enrollment.install_command' "$RESPONSE_FILE")
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m3-cross-enterprise-kubernetes 404 GET "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}" "$OTHER_ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request m3-cross-enterprise-kubernetes-missing 404 GET /enterprise/kubernetes-clusters/00000000-0000-0000-0000-000000000001 "$OTHER_ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  KUBERNETES_DIR="${WORK_DIR}/kubernetes-connector"
  m3_enroll_local_connector "$M3_KUBERNETES_COMMAND" "$KUBERNETES_DIR" m3-kubernetes
  M3_KUBERNETES_CONNECTOR_ID=$M3_ENROLLED_CONNECTOR_ID
  m3_deploy_kubernetes_connector "$M3_KUBERNETES_CONNECTOR_ID" "$KUBERNETES_DIR"
  request m3-kubernetes-connector-before-rotation 200 GET /enterprise/connectors "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_KUBERNETES_EPOCH=$(jq -er --arg id "$M3_KUBERNETES_CONNECTOR_ID" '.items[] | select(.id == $id) | .connection_epoch' "$RESPONSE_FILE")
  M3_KUBERNETES_CONNECTOR_VERSION=$(jq -er --arg id "$M3_KUBERNETES_CONNECTOR_ID" '.items[] | select(.id == $id) | .version' "$RESPONSE_FILE")
  request m3-kubernetes-certificate-rotation 202 POST "/enterprise/connectors/${M3_KUBERNETES_CONNECTOR_ID}/rotate-certificate?expected_version=${M3_KUBERNETES_CONNECTOR_VERSION}" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-certificate-rotation-${RUN_ID}"
  k -n "$SYSTEM_NS" rollout restart deployment/argus-m3-kubernetes-connector >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-m3-kubernetes-connector --timeout=180s >/dev/null
  m3_wait_connector_online "$M3_KUBERNETES_CONNECTOR_ID" "$((M3_KUBERNETES_EPOCH + 1))"
  m3_wait_certificate_rotation "$M3_KUBERNETES_CONNECTOR_ID" "$M3_KUBERNETES_CONNECTOR_VERSION"

  request m3-admin-scope-update 200 PUT "/enterprise/data-scopes/${M3_ADMIN_SCOPE_ID}" "$ENTERPRISE_JAR" \
    "$(jq -nc --arg cluster "$M3_CLUSTER_ID" --arg namespace "$SYSTEM_NS" --argjson version "$M3_ADMIN_SCOPE_VERSION" '{
      name:"M3 administration scope",resource_types:["host","kubernetes_cluster","kubernetes_namespace"],
      explicit_resource_ids:[($cluster+"/default"),($cluster+"/"+$namespace)],expected_version:$version,
      label_selector:{schema_version:"argus.label_selector/v1",requirements:[{key:"team",operator:"eq",values:["m3"]}]}
    }')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  request m3-kubernetes-namespaces 200 GET "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}/resources?resource_type=namespace&limit=50" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.items[] | select(.name == "default")' "$RESPONSE_FILE" >/dev/null
  M3_TARGET_POD=$(k -n "$SYSTEM_NS" get pod -l app.kubernetes.io/name=argus-m3-ssh-target -o jsonpath='{.items[0].metadata.name}')
  request m3-kubernetes-pod-logs 200 GET "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}/pod-logs?namespace=${SYSTEM_NS}&pod=${M3_TARGET_POD}&tail_lines=10" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '(.content | length) <= 1048576' "$RESPONSE_FILE" >/dev/null

  request m3-second-bastion-preview 201 POST /enterprise/bastion-scopes/actions/preview-create "$ENTERPRISE_JAR" \
    '{"name":"m3-bastion-2","environment":"production","labels":{"team":"m3","route":"migration-target"}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-second-bastion-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-second-bastion-confirm "$M3_ACTION_REF"
  M3_SECOND_BASTION_SCOPE_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
  M3_SECOND_BASTION_COMMAND=$(jq -er '.enrollment.install_command' "$RESPONSE_FILE")
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  SECOND_BASTION_DIR="${WORK_DIR}/bastion-connector-second"
  m3_enroll_local_connector "$M3_SECOND_BASTION_COMMAND" "$SECOND_BASTION_DIR" m3-bastion-second
  M3_SECOND_BASTION_CONNECTOR_ID=$M3_ENROLLED_CONNECTOR_ID
  "${WORK_DIR}/argus-connector" run --data-dir "$SECOND_BASTION_DIR" >"${ARTIFACT_DIR}/bastion-second-connector.log" 2>&1 &
  M3_SECOND_BASTION_CONNECTOR_PID=$!
  M3_CONNECTOR_PIDS+=("$M3_SECOND_BASTION_CONNECTOR_PID")
  m3_wait_connector_online "$M3_SECOND_BASTION_CONNECTOR_ID"

  request m3-host-migration-test 202 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg scope "$M3_SECOND_BASTION_SCOPE_ID" --arg credential "$M3_CREDENTIAL_ID" --argjson port "$M3_SSH_PORT" '{address:"127.0.0.1",port:$port,platform:"linux",connection_mode:"via_bastion",bastion_scope_id:$scope,credential_id:$credential,username:"argus"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-host-migration-test-${RUN_ID}"
  M3_HOST_MIGRATION_TEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  m3_wait_connection_test m3-host-migration-test-result "$M3_HOST_MIGRATION_TEST_ID"
  request m3-host-before-migration 200 GET "/enterprise/hosts/${M3_BASTION_HOST_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_HOST_VERSION=$(jq -er '.resource_version' "$RESPONSE_FILE")
  request m3-host-migration-preview 201 POST "/enterprise/hosts/${M3_BASTION_HOST_ID}/actions/preview-update" "$ENTERPRISE_JAR" \
    "$(jq -nc --arg scope "$M3_SECOND_BASTION_SCOPE_ID" --arg test "$M3_HOST_MIGRATION_TEST_ID" --argjson version "$M3_HOST_VERSION" '{connection_mode:"via_bastion",bastion_scope_id:$scope,connection_test_id:$test,expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-host-migration-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-host-migration-confirm "$M3_ACTION_REF"
  request m3-first-bastion-after-migration 200 GET "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.member_count == 0' "$RESPONSE_FILE" >/dev/null
  request m3-second-bastion-after-migration 200 GET "/enterprise/bastion-scopes/${M3_SECOND_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.member_count == 1' "$RESPONSE_FILE" >/dev/null

  request m3-scope-create 201 POST /enterprise/data-scopes "$ENTERPRISE_JAR" \
    '{"name":"M3 labeled resources","resource_types":["host","kubernetes_cluster"],"explicit_resource_ids":[],"label_selector":{"schema_version":"argus.label_selector/v1","requirements":[{"key":"team","operator":"eq","values":["m3"]}]}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-scope-${RUN_ID}"
  M3_SCOPE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m3-role-create 201 POST /enterprise/roles "$ENTERPRISE_JAR" \
    '{"name":"M3 Resource Reader","permissions":["host.read","kubernetes.read"]}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-role-${RUN_ID}"
  M3_ROLE_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m3-service-account-create 201 POST /enterprise/service-accounts "$ENTERPRISE_JAR" \
    "$(jq -nc --arg scope "$M3_SCOPE_ID" '{name:"m3-reader",allowed_tool_ids:["inventory.read"],data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-sa-${RUN_ID}"
  M3_SERVICE_ACCOUNT_ID=$(jq -er '.id' "$RESPONSE_FILE")
  request m3-binding-create 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
    "$(jq -nc --arg subject "$M3_SERVICE_ACCOUNT_ID" --arg role "$M3_ROLE_ID" --arg scope "$M3_SCOPE_ID" '{subject_type:"service_account",subject_id:$subject,role_id:$role,data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-binding-${RUN_ID}"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m3-api-key-create 201 POST "/enterprise/service-accounts/${M3_SERVICE_ACCOUNT_ID}/api-keys" "$ENTERPRISE_JAR" '{"name":"m3-reader"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-key-${RUN_ID}"
  M3_API_KEY=$(jq -er '.secret' "$RESPONSE_FILE")
  request m3-filtered-hosts 200 GET /enterprise/hosts - - --header "Authorization: Bearer ${M3_API_KEY}"
  jq -e '(.items | length) >= 2 and all(.items[]; .labels.team == "m3")' "$RESPONSE_FILE" >/dev/null

  request m3-host-current 200 GET "/enterprise/hosts/${M3_BASTION_HOST_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_HOST_VERSION=$(jq -er '.resource_version' "$RESPONSE_FILE")
  request m3-label-preview 201 POST "/enterprise/hosts/${M3_BASTION_HOST_ID}/actions/preview-update" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M3_HOST_VERSION" '{labels:{team:"revoked",route:"bastion"},expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-label-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-label-confirm "$M3_ACTION_REF"
  request m3-stale-api-key 403 GET /enterprise/hosts - - --header "Authorization: Bearer ${M3_API_KEY}"
  jq -e '.code == "AUTHORIZATION_VERSION_STALE"' "$RESPONSE_FILE" >/dev/null

  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  kill "$M3_BASTION_CONNECTOR_PID" >/dev/null 2>&1 || true
  wait "$M3_BASTION_CONNECTOR_PID" >/dev/null 2>&1 || true
  for _ in $(seq 1 30); do
    request m3-bastion-offline 200 GET "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    M3_BASTION_STATUS=$(jq -er '.status' "$RESPONSE_FILE")
    [[ "$M3_BASTION_STATUS" == "suspected_offline" || "$M3_BASTION_STATUS" == "offline" ]] && break
    sleep 1
  done
  [[ "$M3_BASTION_STATUS" == "suspected_offline" || "$M3_BASTION_STATUS" == "offline" ]] || fail "Bastion Scope did not follow disconnected Connector state"
  M3_BASTION_SCOPE_VERSION=$(jq -er '.resource_version' "$RESPONSE_FILE")
  M3_OLD_BASTION_CONNECTOR_ID=$M3_BASTION_CONNECTOR_ID
  request m3-bastion-replacement-preview 201 POST "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}/actions/preview-replacement" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M3_BASTION_SCOPE_VERSION" '{expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-replacement-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-bastion-replacement-confirm "$M3_ACTION_REF"
  M3_BASTION_REPLACEMENT_COMMAND=$(jq -er '.enrollment.install_command' "$RESPONSE_FILE")
  "${WORK_DIR}/argus-connector" run --data-dir "$BASTION_DIR" >"${ARTIFACT_DIR}/bastion-revoked-reconnect.log" 2>&1 &
  M3_REVOKED_CONNECTOR_PID=$!
  sleep 5
  request m3-old-bastion-fenced 200 GET /enterprise/connectors "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  if jq -e --arg id "$M3_OLD_BASTION_CONNECTOR_ID" '.items[] | select(.id == $id and .status == "online")' "$RESPONSE_FILE" >/dev/null; then
    fail "revoked Bastion Connector reconnected after replacement fencing"
  fi
  kill "$M3_REVOKED_CONNECTOR_PID" >/dev/null 2>&1 || true
  wait "$M3_REVOKED_CONNECTOR_PID" >/dev/null 2>&1 || true
  BASTION_REPLACEMENT_DIR="${WORK_DIR}/bastion-connector-replacement"
  m3_enroll_local_connector "$M3_BASTION_REPLACEMENT_COMMAND" "$BASTION_REPLACEMENT_DIR" m3-bastion-replacement
  M3_BASTION_CONNECTOR_ID=$M3_ENROLLED_CONNECTOR_ID
  "${WORK_DIR}/argus-connector" run --data-dir "$BASTION_REPLACEMENT_DIR" >"${ARTIFACT_DIR}/bastion-replacement-connector.log" 2>&1 &
  M3_BASTION_CONNECTOR_PID=$!
  M3_CONNECTOR_PIDS+=("$M3_BASTION_CONNECTOR_PID")
  m3_wait_connector_online "$M3_BASTION_CONNECTOR_ID"
  request m3-bastion-replacement-active 200 GET "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e --arg id "$M3_BASTION_CONNECTOR_ID" '.status == "active" and .active_connector_id == $id' "$RESPONSE_FILE" >/dev/null

  request m3-connectors-before-restart 200 GET /enterprise/connectors "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_BASTION_EPOCH=$(jq -er --arg id "$M3_BASTION_CONNECTOR_ID" '.items[] | select(.id == $id) | .connection_epoch' "$RESPONSE_FILE")
  M3_KUBERNETES_EPOCH=$(jq -er --arg id "$M3_KUBERNETES_CONNECTOR_ID" '.items[] | select(.id == $id) | .connection_epoch' "$RESPONSE_FILE")
  k -n "$SYSTEM_NS" exec statefulset/argus-redis -- redis-cli -a "$REDIS_PASSWORD" FLUSHALL >/dev/null 2>&1
  k -n "$SYSTEM_NS" rollout restart deployment/argus-connector-gateway >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-connector-gateway --timeout=300s >/dev/null
  m3_start_gateway_forward
  m3_wait_connector_online "$M3_BASTION_CONNECTOR_ID" "$((M3_BASTION_EPOCH + 1))"
  m3_wait_connector_online "$M3_KUBERNETES_CONNECTOR_ID" "$((M3_KUBERNETES_EPOCH + 1))"

  request m3-old-bastion-before-uninstall 200 GET "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_BASTION_ROOT_HOST_ID=$(jq -er '.connector_host_id' "$RESPONSE_FILE")
  request m3-old-connector-before-uninstall 200 GET "/enterprise/connectors/${M3_BASTION_CONNECTOR_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_BASTION_CONNECTOR_VERSION=$(jq -er '.version' "$RESPONSE_FILE")
  request m3-connector-uninstall-preview 201 POST "/enterprise/connectors/${M3_BASTION_CONNECTOR_ID}/actions/preview-uninstall" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M3_BASTION_CONNECTOR_VERSION" '{expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-connector-uninstall-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-connector-uninstall-confirm "$M3_ACTION_REF"
  for _ in $(seq 1 120); do
    request m3-connector-uninstall-state 200 GET "/enterprise/connectors/${M3_BASTION_CONNECTOR_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    M3_UNINSTALL_CONNECTOR_STATUS=$(jq -er '.status' "$RESPONSE_FILE")
    request m3-bastion-uninstall-state 200 GET "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
    M3_UNINSTALL_SCOPE_STATUS=$(jq -er '.status' "$RESPONSE_FILE")
    [[ "$M3_UNINSTALL_CONNECTOR_STATUS" == "uninstalled" && "$M3_UNINSTALL_SCOPE_STATUS" == "uninstalled" ]] && break
    sleep 1
  done
  [[ "$M3_UNINSTALL_CONNECTOR_STATUS" == "uninstalled" && "$M3_UNINSTALL_SCOPE_STATUS" == "uninstalled" ]] || \
    fail "Connector uninstall did not converge: connector=${M3_UNINSTALL_CONNECTOR_STATUS} scope=${M3_UNINSTALL_SCOPE_STATUS}"

  request m3-bastion-cluster-current 200 GET "/enterprise/kubernetes-clusters/${M3_BASTION_CLUSTER_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_BASTION_CLUSTER_VERSION=$(jq -er '.resource_version' "$RESPONSE_FILE")
  request m3-bastion-cluster-delete-preview 201 POST "/enterprise/kubernetes-clusters/${M3_BASTION_CLUSTER_ID}/actions/preview-delete" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M3_BASTION_CLUSTER_VERSION" '{expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-cluster-delete-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-bastion-cluster-delete-confirm "$M3_ACTION_REF"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m3-bastion-before-delete 200 GET "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M3_BASTION_SCOPE_VERSION=$(jq -er '.resource_version' "$RESPONSE_FILE")
  request m3-bastion-delete-preview 201 POST "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}/actions/preview-delete" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M3_BASTION_SCOPE_VERSION" '{expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-bastion-delete-${RUN_ID}"
  M3_ACTION_REF=$(jq -er '.action_ref' "$RESPONSE_FILE")
  m3_confirm m3-bastion-delete-confirm "$M3_ACTION_REF"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m3-bastion-deleted 404 GET "/enterprise/bastion-scopes/${M3_BASTION_SCOPE_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  request m3-bastion-root-host-deleted 404 GET "/enterprise/hosts/${M3_BASTION_ROOT_HOST_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  [[ "$(m3_psql "SELECT count(*) FROM hosts WHERE id='${M3_BASTION_ROOT_HOST_ID}' AND status='deleted' AND deleted_at IS NOT NULL;")" == "1" ]] || \
    fail "deleted Bastion root Host was not retained as a logical tombstone"

  request m3-secret-rotation-test 202 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
    "$(jq -nc --arg credential "$M3_CREDENTIAL_ID" --argjson port "$M3_DIRECT_SSH_PORT" '{address:"8.8.8.8",port:$port,platform:"linux",connection_mode:"direct_ssh",credential_id:$credential,username:"argus"}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-secret-rotation-test-${RUN_ID}"
  M3_ROTATION_TEST_ID=$(jq -er '.id' "$RESPONSE_FILE")
  m3_wait_connection_test m3-secret-rotation-test-result "$M3_ROTATION_TEST_ID"
  request m3-secret-rotate 200 POST "/enterprise/secrets/${M3_SECRET_ID}/rotate" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M3_SECRET_VERSION" '{value:"M3-e2e-ssh-password",expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m3-secret-rotate-${RUN_ID}"
  M3_SECRET_VERSION=$(jq -er '.version' "$RESPONSE_FILE")
  request m3-secret-rotation-test-expired 200 GET "/enterprise/connection-tests/${M3_ROTATION_TEST_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.status == "expired"' "$RESPONSE_FILE" >/dev/null
  [[ "$(m3_psql "SELECT count(*) FROM credential_leases WHERE credential_id='${M3_CREDENTIAL_ID}' AND status='active';")" == "0" ]] || \
    fail "Secret rotation left an active Credential Lease"

  request m3-audit-scan 200 GET /enterprise/audit-events "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  if grep -Fq 'M3-e2e-ssh-password' "$RESPONSE_FILE"; then
    fail "M3 audit response contains a Secret value"
  fi
  if k -n "$SYSTEM_NS" logs -l app.kubernetes.io/part-of=argus --all-containers=true --tail=2000 2>/dev/null | grep -Fq 'M3-e2e-ssh-password'; then
    fail "M3 workload logs contain a Secret value"
  fi
  if k -n "$SYSTEM_NS" exec statefulset/argus-postgresql -- env PGPASSWORD="$POSTGRES_PASSWORD" \
    pg_dump --data-only --inserts -U argus -d argus 2>/dev/null | grep -Fq 'M3-e2e-ssh-password'; then
    fail "PostgreSQL logical dump contains a Secret value in plaintext"
  fi
  if k -n "$SYSTEM_NS" exec statefulset/argus-redis -- sh -c \
    'redis-cli -a "$REDIS_PASSWORD" --scan 2>/dev/null | while read -r key; do redis-cli -a "$REDIS_PASSWORD" --raw DUMP "$key" 2>/dev/null; done' | grep -Fq 'M3-e2e-ssh-password'; then
    fail "Redis contains a Secret value in plaintext"
  fi

  ARGUS_E2E_EXTERNAL=1 ARGUS_M3_E2E=1 \
    ARGUS_M3_ENTERPRISE_USERNAME="$ENTERPRISE_USERNAME" ARGUS_M3_ENTERPRISE_PASSWORD="$ENTERPRISE_PASSWORD" \
    ARGUS_E2E_ARTIFACTS="$ARTIFACT_DIR/playwright-m3" \
    pnpm --filter @argus/enterprise exec playwright test e2e/m3-real.spec.ts --workers=1

  rm -f "$ENTERPRISE_JAR"
  enterprise_login

  unset M3_API_KEY M3_BASTION_COMMAND M3_KUBERNETES_COMMAND
}
