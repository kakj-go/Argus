#!/usr/bin/env bash

prepare_m7_dependencies() {
  M7_STRIMZI_RELEASE="m7s-${RUN_ID##*-}"
  M7_ALTINITY_RELEASE="m7a-${RUN_ID##*-}"
  M7_DATA_RELEASE="m7d-${RUN_ID##*-}"
  M7_PIPELINE_RELEASE="m7p-${RUN_ID##*-}"
  M7_INSTALLED_STRIMZI=false
  M7_INSTALLED_ALTINITY=false
  TELEMETRY_GRPC_PORT=${ARGUS_E2E_TELEMETRY_GRPC_PORT:-4317}
  TELEMETRY_QUERY_PORT=${ARGUS_E2E_TELEMETRY_QUERY_PORT:-4247}
	M7_COLLECTOR_NAMESPACE=argus-telemetry
	M7_CREATED_COLLECTOR_NAMESPACE=false
	M7_ARTIFACT_IMAGE="argus/argus-m7-artifacts:${RUN_ID}"
	if k get namespace "$M7_COLLECTOR_NAMESPACE" >/dev/null 2>&1; then
		fail "managed-cluster namespace ${M7_COLLECTOR_NAMESPACE} already exists"
	fi
	local artifact_file artifact_sha artifact_size signing_public signature windows_artifact_file windows_artifact_size windows_signature
	artifact_file="${WORK_DIR}/argus-otelcol-linux-arm64.tar.gz"
	cp build/otelcol/artifacts/argus-otelcol-linux-arm64.tar.gz "$artifact_file"
  artifact_sha=$(shasum -a 256 "$artifact_file" | awk '{print $1}')
  artifact_size=$(wc -c <"$artifact_file" | tr -d ' ')
	windows_artifact_file="${WORK_DIR}/argus-otelcol-windows-amd64.zip"
	cp build/otelcol/artifacts/argus-otelcol-windows-amd64.zip "$windows_artifact_file"
	M7_WINDOWS_ARTIFACT_SHA=$(shasum -a 256 "$windows_artifact_file" | awk '{print $1}')
	windows_artifact_size=$(wc -c <"$windows_artifact_file" | tr -d ' ')
  printf '%s' "$artifact_sha" | xxd -r -p >"${WORK_DIR}/artifact.digest"
  openssl genpkey -algorithm ED25519 -out "${WORK_DIR}/artifact-signing.pem" >/dev/null 2>&1
  signing_public=$(openssl pkey -in "${WORK_DIR}/artifact-signing.pem" -pubout -outform DER 2>/dev/null | tail -c 32 | openssl base64 -A | tr -d '=')
  signature=$(openssl pkeyutl -sign -rawin -inkey "${WORK_DIR}/artifact-signing.pem" -in "${WORK_DIR}/artifact.digest" | openssl base64 -A | tr -d '=')
	printf '%s' "$M7_WINDOWS_ARTIFACT_SHA" | xxd -r -p >"${WORK_DIR}/windows-artifact.digest"
	windows_signature=$(openssl pkeyutl -sign -rawin -inkey "${WORK_DIR}/artifact-signing.pem" -in "${WORK_DIR}/windows-artifact.digest" | openssl base64 -A | tr -d '=')
	openssl ecparam -name prime256v1 -genkey -noout -out "${WORK_DIR}/artifact-ca.key"
	openssl req -x509 -new -sha256 -key "${WORK_DIR}/artifact-ca.key" -days 1 -subj "/CN=argus-m7-artifact-ca" \
		-addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign" -out "${WORK_DIR}/artifact-ca.crt"
	openssl ecparam -name prime256v1 -genkey -noout -out "${WORK_DIR}/artifact-server.key"
	openssl req -new -sha256 -key "${WORK_DIR}/artifact-server.key" -subj "/CN=argus-m7-artifacts" -out "${WORK_DIR}/artifact-server.csr"
	printf '%s\n' 'subjectAltName=IP:127.0.0.1' 'extendedKeyUsage=serverAuth' >"${WORK_DIR}/artifact-server.ext"
	openssl x509 -req -sha256 -in "${WORK_DIR}/artifact-server.csr" -CA "${WORK_DIR}/artifact-ca.crt" -CAkey "${WORK_DIR}/artifact-ca.key" \
		-CAcreateserial -days 1 -extfile "${WORK_DIR}/artifact-server.ext" -out "${WORK_DIR}/artifact-server.crt"
	k -n "$SYSTEM_NS" create secret generic argus-m7-artifact-tls \
		--from-file=tls.crt="${WORK_DIR}/artifact-server.crt" --from-file=tls.key="${WORK_DIR}/artifact-server.key" \
		--from-file=ca.crt="${WORK_DIR}/artifact-ca.crt" >/dev/null
	log "building signed Collector Artifact HTTPS image ${M7_ARTIFACT_IMAGE}"
	retry 3 docker build --quiet -f deploy/docker/e2e-artifact-server.Dockerfile -t "$M7_ARTIFACT_IMAGE" . >/dev/null
	case "$KUBE_CONTEXT" in
		kind-*) kind load docker-image --name "${KUBE_CONTEXT#kind-}" "$M7_ARTIFACT_IMAGE" ;;
		minikube) minikube image load "$M7_ARTIFACT_IMAGE" ;;
		docker-desktop)
			local node
			node=$(k get nodes -o jsonpath='{.items[0].metadata.name}')
			docker save "$M7_ARTIFACT_IMAGE" | docker exec -i "$node" ctr -n k8s.io images import - >/dev/null
			;;
	esac
	M7_HELM_ARGS=(
    --set-string runtime.otelcolVersion=0.1.0-m7
	    --set-string runtime.otelcolLinuxArm64Uri=https://127.0.0.1:8443/m7/linux-arm64.tar.gz
    --set-string runtime.otelcolLinuxArm64Sha256="$artifact_sha"
    --set-string runtime.otelcolLinuxArm64Signature="$signature"
    --set-string runtime.otelcolLinuxArm64ByteSize="$artifact_size"
    --set-string runtime.otelcolWindowsAmd64Uri=https://artifacts.argus.invalid/m7/windows-amd64.zip
    --set-string runtime.otelcolWindowsAmd64Sha256="$M7_WINDOWS_ARTIFACT_SHA"
    --set-string runtime.otelcolWindowsAmd64Signature="$windows_signature"
    --set-string runtime.otelcolWindowsAmd64ByteSize="$windows_artifact_size"
    --set-string runtime.otelcolSigningKeyId=argus-m7-e2e
    --set-string runtime.otelcolSigningPublicKey="$signing_public"
	    --set-string runtime.otelcolKubernetesImage="$OTELCOL_IMAGE"
		--set-string runtime.otelcolArtifactCABundleSecretName=argus-m7-artifact-tls
	--set-string runtime.telemetryIssuerName="${RELEASE_ID}-telemetry-ca"
	--set-string runtime.telemetryEnrollmentEndpoint="https://argus-telemetry-ingest.${OBSERVABILITY_NS}.svc:4318/v1/identity/enroll"
	--set-string runtime.telemetryIngestGrpcEndpoint="grpcs://argus-telemetry-ingest.${OBSERVABILITY_NS}.svc:4317"
	--set-string runtime.telemetryIngestHttpEndpoint="https://argus-telemetry-ingest.${OBSERVABILITY_NS}.svc:4318"
    --set-string runtime.telemetryClickhouseMigrationPassword="$OBJECT_STORE_ACCESS"
    --set-string runtime.telemetryClickhouseWriterPassword="$OBJECT_STORE_SECRET"
    --set-string runtime.telemetryClickhouseQueryPassword="$POSTGRES_PASSWORD"
  )

  log "installing locked namespaced Strimzi 1.1.0 for M7"
  retry 3 helm upgrade --install "$M7_STRIMZI_RELEASE" \
    https://github.com/strimzi/strimzi-kafka-operator/releases/download/1.1.0/strimzi-kafka-operator-helm-3-chart-1.1.0.tgz \
    --kube-context "$KUBE_CONTEXT" --namespace "$OBSERVABILITY_NS" --wait --timeout 10m \
    --set watchAnyNamespace=false --set-json "watchNamespaces=[\"${OBSERVABILITY_NS}\"]" >/dev/null
  M7_INSTALLED_STRIMZI=true

  log "installing locked namespaced Altinity ClickHouse Operator 0.27.3 for M7"
  retry 3 helm upgrade --install "$M7_ALTINITY_RELEASE" \
    https://github.com/Altinity/clickhouse-operator/releases/download/release-0.27.3/altinity-clickhouse-operator-0.27.3.tgz \
    --kube-context "$KUBE_CONTEXT" --namespace "$OBSERVABILITY_NS" --wait --timeout 10m >/dev/null
  M7_INSTALLED_ALTINITY=true

  k -n "$OBSERVABILITY_NS" create secret generic argus-clickhouse-credentials \
    --from-literal=password="$OBJECT_STORE_ACCESS" >/dev/null
  k -n "$OBSERVABILITY_NS" create secret generic argus-telemetry-clickhouse \
    --from-literal=migration-password="$OBJECT_STORE_ACCESS" \
    --from-literal=writer-password="$OBJECT_STORE_SECRET" \
    --from-literal=query-password="$POSTGRES_PASSWORD" >/dev/null
  helm upgrade --install "$M7_DATA_RELEASE" deploy/helm/argus-data \
    --kube-context "$KUBE_CONTEXT" --namespace "$SYSTEM_NS" --wait --timeout 15m \
    --set-string releaseId="$RELEASE_ID" \
    --set-string namespaces.system="$SYSTEM_NS" \
    --set-string namespaces.observability="$OBSERVABILITY_NS" \
    --set components.credentials=false --set components.postgresql=false --set components.redis=false --set components.minio=false \
    --set components.kafka=true --set components.keeper=true --set components.clickhouse=true \
    --set-string persistence.kafka=1Gi --set-string persistence.keeper=1Gi --set-string persistence.clickhouse=2Gi >/dev/null
  k -n "$OBSERVABILITY_NS" wait kafka/argus-kafka --for=condition=Ready --timeout=15m >/dev/null
  k -n "$OBSERVABILITY_NS" rollout status statefulset/argus-keeper --timeout=5m >/dev/null
  for _ in $(seq 1 180); do
    k -n "$OBSERVABILITY_NS" get service argus-clickhouse-client >/dev/null 2>&1 && \
      k -n "$OBSERVABILITY_NS" get pods -l clickhouse.altinity.com/chi=argus-clickhouse -o json | jq -e '.items | length > 0 and all(.[]; .status.phase == "Running")' >/dev/null 2>&1 && break
    sleep 5
  done
  k -n "$OBSERVABILITY_NS" get pods -l clickhouse.altinity.com/chi=argus-clickhouse -o json | \
    jq -e '.items | length > 0 and all(.[]; .status.phase == "Running")' >/dev/null

  log "installing M7 Kafka topics and ClickHouse schema"
  helm upgrade --install "$M7_PIPELINE_RELEASE" deploy/helm/argus-telemetry-pipeline \
    --kube-context "$KUBE_CONTEXT" --namespace "$OBSERVABILITY_NS" --wait --wait-for-jobs --timeout 10m \
    --set-string releaseId="$RELEASE_ID" --set-string namespace="$OBSERVABILITY_NS" >/dev/null
  for topic in otlp-metrics otlp-logs otlp-traces otlp-metrics-dlq otlp-logs-dlq otlp-traces-dlq; do
    k -n "$OBSERVABILITY_NS" wait "kafkatopic/${topic}" --for=condition=Ready --timeout=5m >/dev/null
  done
}

cleanup_m7() {
	docker image rm "${M7_ARTIFACT_IMAGE:-missing}" >/dev/null 2>&1 || true
	if [[ "${M7_CREATED_COLLECTOR_NAMESPACE:-false}" == true ]]; then
		collector_id=$(m3_psql "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='${M3_CLUSTER_ID:-00000000-0000-0000-0000-000000000000}' ORDER BY created_at DESC LIMIT 1;" 2>/dev/null || true)
		k delete namespace "$M7_COLLECTOR_NAMESPACE" --wait=false >/dev/null 2>&1 || true
		if [[ -n "$collector_id" ]]; then
			k delete clusterrole,clusterrolebinding -l "argus.io/collector-id=${collector_id}" --ignore-not-found=true >/dev/null 2>&1 || true
		fi
		k wait --for=delete "namespace/${M7_COLLECTOR_NAMESPACE}" --timeout=180s >/dev/null 2>&1 || true
	fi
  k delete clusterissuer "${RELEASE_ID}-telemetry-ca" "${RELEASE_ID}-telemetry-selfsigned" --ignore-not-found=true >/dev/null 2>&1 || true
  k -n "${CERT_MANAGER_NAMESPACE:-cert-manager}" delete certificate "${RELEASE_ID}-telemetry-root-ca" --ignore-not-found=true >/dev/null 2>&1 || true
  k -n "${CERT_MANAGER_NAMESPACE:-cert-manager}" delete secret "${RELEASE_ID}-telemetry-root-ca" --ignore-not-found=true >/dev/null 2>&1 || true
  helm --kube-context "$KUBE_CONTEXT" uninstall "${M7_PIPELINE_RELEASE:-missing}" --namespace "$OBSERVABILITY_NS" >/dev/null 2>&1 || true
  helm --kube-context "$KUBE_CONTEXT" uninstall "${M7_DATA_RELEASE:-missing}" --namespace "$SYSTEM_NS" >/dev/null 2>&1 || true
  if ! k -n "$OBSERVABILITY_NS" wait --for=delete clickhouseinstallation/argus-clickhouse --timeout=180s >/dev/null 2>&1; then
    k -n "$OBSERVABILITY_NS" patch clickhouseinstallation argus-clickhouse --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
  fi
  if ! k -n "$OBSERVABILITY_NS" wait --for=delete kafkatopic --all --timeout=180s >/dev/null 2>&1; then
    for topic in $(k -n "$OBSERVABILITY_NS" get kafkatopic -o name 2>/dev/null); do
      k -n "$OBSERVABILITY_NS" patch "$topic" --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
    done
  fi
  if [[ "${M7_INSTALLED_ALTINITY:-false}" == true ]]; then
    helm --kube-context "$KUBE_CONTEXT" uninstall "$M7_ALTINITY_RELEASE" --namespace "$OBSERVABILITY_NS" >/dev/null 2>&1 || true
  fi
  if [[ "${M7_INSTALLED_STRIMZI:-false}" == true ]]; then
    helm --kube-context "$KUBE_CONTEXT" uninstall "$M7_STRIMZI_RELEASE" --namespace "$OBSERVABILITY_NS" >/dev/null 2>&1 || true
  fi
}

diagnostics_m7() {
  local collector_namespace=${M7_COLLECTOR_NAMESPACE:-}
  {
    k -n "$OBSERVABILITY_NS" get all,pvc,jobs,kafkatopic,kafkauser -o wide || true
    k -n "$OBSERVABILITY_NS" get events --sort-by=.lastTimestamp || true
  } 2>&1 | redact >"${ARTIFACT_DIR}/m7-observability.txt"
  k -n "$OBSERVABILITY_NS" logs -l app.kubernetes.io/part-of=argus --all-containers=true --prefix=true --tail=1000 2>&1 \
    | redact >"${ARTIFACT_DIR}/m7-telemetry.log" || true
	if [[ -n "$collector_namespace" ]] && k get namespace "$collector_namespace" >/dev/null 2>&1; then
		{
			k -n "$collector_namespace" get all,configmap,secret -o wide || true
			k -n "$collector_namespace" get events --sort-by=.lastTimestamp || true
			k -n "$collector_namespace" describe pods || true
		} 2>&1 | redact >"${ARTIFACT_DIR}/m7-managed-cluster.txt"
		k -n "$collector_namespace" logs -l app.kubernetes.io/part-of=argus --all-containers=true --prefix=true --tail=1000 2>&1 \
			| redact >"${ARTIFACT_DIR}/m7-collector.log" || true
		k -n "$collector_namespace" logs -l job-name=argus-m7-otlp-base --all-containers=true --prefix=true --tail=1000 2>&1 \
			| redact >"${ARTIFACT_DIR}/m7-generator.log" || true
		k -n "$collector_namespace" logs -l app.kubernetes.io/name=argus-otelcol-gateway --all-containers=true --prefix=true --previous --tail=1000 2>&1 \
			| redact >"${ARTIFACT_DIR}/m7-collector-previous.log" || true
	fi
  k -n "$SYSTEM_NS" logs job/argus-telemetry-catalog-sync --all-containers=true --prefix=true --tail=1000 2>&1 \
    | redact >"${ARTIFACT_DIR}/m7-catalog-sync.log" || true
  k -n "$OBSERVABILITY_NS" logs job/argus-clickhouse-telemetry-migration --all-containers=true --prefix=true --tail=1000 2>&1 \
    | redact >"${ARTIFACT_DIR}/m7-clickhouse-migration.log" || true
  m3_psql "
    SELECT 'distribution|' || name || '|' || version || '|' || support_status FROM collector_distribution_versions ORDER BY name;
    SELECT 'collector|' || id || '|' || resource_type || '|' || status || '|' || desired_revision || '|' || effective_revision FROM collector_instances ORDER BY created_at;
    SELECT 'operation|' || id || '|' || operation || '|' || status || '|' || attempts FROM telemetry_collector_operations ORDER BY created_at;
		SELECT 'connector_command|' || command_id || '|' || command_type || '|' || status || '|' || COALESCE(error_code, '') FROM connector_commands WHERE command_type='collector_management' ORDER BY created_at;
		SELECT 'binding|' || id || '|' || kubernetes_cluster_id || '|' || node_uid || '|' || node_name || '|' || status || '|' || version FROM kubernetes_node_host_bindings ORDER BY created_at;
		SELECT 'claim|' || id || '|' || physical_resource_ref || '|' || claim_type || '|' || signal || '|' || ownership || '|' || status FROM collection_claims ORDER BY created_at;
    SELECT 'dlq|' || id || '|' || signal || '|' || status || '|' || topic || '|' || source_offset FROM telemetry_dlq_records ORDER BY first_seen_at;
  " 2>&1 | redact >"${ARTIFACT_DIR}/m7-control-plane.txt" || true
}

run_m7_playwright() {
  log "running Playwright against real M7 telemetry data"
  ARGUS_E2E_EXTERNAL=1 ARGUS_M7_E2E=1 \
    ARGUS_M7_ENTERPRISE_USERNAME="$ENTERPRISE_USERNAME" ARGUS_M7_ENTERPRISE_PASSWORD="$ENTERPRISE_PASSWORD" \
	    ARGUS_M7_CLUSTER_ID="$M3_CLUSTER_ID" \
		ARGUS_M7_HOST_ID="$M7_HOST_ID" \
    ARGUS_E2E_ARTIFACTS="$ARTIFACT_DIR/playwright-m7" \
    pnpm --filter @argus/enterprise exec playwright test e2e/m7-real.spec.ts --workers=1
}

m7_wait_host_collector() {
	local expected=$1 status
	for _ in $(seq 1 180); do
		request m7-host-collector-status 200 GET "/enterprise/hosts/${M7_HOST_ID}/collector" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
		status=$(jq -er '.status' "$RESPONSE_FILE")
		[[ "$status" == "$expected" ]] && return
		if [[ "$status" == "degraded" || "$status" == "result_unknown" ]]; then
			fail "M7 Host Collector entered ${status}"
		fi
		sleep 2
	done
	fail "M7 Host Collector did not reach ${expected}"
}

m7_apply_host_collector_action() {
	local action=$1 distribution_id=$2 profile_ids=$3 collector_version action_ref
	request "m7-host-collector-${action}-current" 200 GET "/enterprise/hosts/${M7_HOST_ID}/collector" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	collector_version=$(jq -er '.version' "$RESPONSE_FILE")
	request "m7-host-collector-${action}-preview" 201 POST "/enterprise/hosts/${M7_HOST_ID}/collector/actions/preview-${action}" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg distribution "$distribution_id" --argjson profiles "$profile_ids" --argjson version "$collector_version" \
			'{distribution_version_id:$distribution,profile_ids:$profiles,route_kind:"direct_argus",expected_version:$version}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" \
		--header "Idempotency-Key: m7-host-collector-${action}-${RUN_ID}"
	action_ref=$(jq -er '.action_ref' "$RESPONSE_FILE")
	m3_confirm "m7-host-collector-${action}-confirm" "$action_ref"
	if [[ "$action" == "uninstall" ]]; then
		m7_wait_host_collector uninstalled
	else
		m7_wait_host_collector converged
	fi
}

m7_query_host_signals() {
	local from to marker=host-systemd
	from=$(date -u -v-10M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '-10 minutes' +%Y-%m-%dT%H:%M:%SZ)
	to=$(date -u -v+1M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 minute' +%Y-%m-%dT%H:%M:%SZ)
	k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-direct-executor -- \
		/usr/local/bin/argus-telemetry-e2e --endpoint=127.0.0.1:4317 --resource-id="$M7_HOST_ID" --marker="$marker"
	m7_wait_metric_for_resource() {
		local metric_name=$1 resource_id=$2
		for _ in $(seq 1 120); do
			request "m7-host-metric" 200 POST /enterprise/telemetry/query/metrics "$ENTERPRISE_JAR" \
				"$(jq -nc --arg id "$resource_id" --arg from "$from" --arg to "$to" --arg metric "$metric_name" \
					'{resource_ids:[$id],from:$from,to:$to,metric_name:$metric,aggregation:"avg",step_seconds:60,limit:100}')" \
				--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
			jq -e --arg metric "$metric_name" 'any(.series[]; .metric_name == $metric and (.points | length > 0))' "$RESPONSE_FILE" >/dev/null 2>&1 && return
			sleep 2
		done
		fail "M7 Host metric ${metric_name} did not converge"
	}
	m7_wait_metric_for_resource "argus.m7.e2e.gauge.${marker}" "$M7_HOST_ID"
	request m7-host-logs-real 200 POST /enterprise/telemetry/query/logs "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M7_HOST_ID" --arg from "$from" --arg to "$to" --arg text "argus m7 e2e log.${marker}" \
			'{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",text:$text,limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e --arg body "argus m7 e2e log.${marker}" 'any(.records[]; .body == $body)' "$RESPONSE_FILE" >/dev/null
	request m7-host-traces-real 200 POST /enterprise/telemetry/query/traces "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M7_HOST_ID" --arg from "$from" --arg to "$to" --arg operation "argus-m7-e2e.${marker}" \
			'{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",operation:$operation,limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e --arg operation "argus-m7-e2e.${marker}" 'any(.traces[]; .root_span_name == $operation)' "$RESPONSE_FILE" >/dev/null
}

m7_run_generator() {
	local marker=$1 marker_name job_name collector_id
	marker_name=${marker:-base}
	job_name="argus-m7-otlp-${marker_name//[^a-zA-Z0-9-]/-}"
	collector_id=$(m3_psql "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='${M3_CLUSTER_ID}' ORDER BY created_at DESC LIMIT 1;")
	[[ -n "$collector_id" ]] || fail "Kubernetes Collector identity is unavailable for the mTLS generator"
	k -n "$M7_COLLECTOR_NAMESPACE" delete job "$job_name" --ignore-not-found=true >/dev/null
	cat <<EOF | k -n "$M7_COLLECTOR_NAMESPACE" apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata: {name: ${job_name}}
spec:
  backoffLimit: 3
  template:
    spec:
      restartPolicy: Never
      securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532}
      containers:
        - name: generator
          image: ${BACKEND_IMAGE}
          imagePullPolicy: Never
          command: [/usr/local/bin/argus-telemetry-e2e]
          args:
            - --endpoint=argus-otelcol-gateway.${M7_COLLECTOR_NAMESPACE}.svc.cluster.local:4317
            - --resource-id=${M3_CLUSTER_ID}
            - --marker=${marker}
            - --tls-ca=/var/run/argus-telemetry-client/ca.pem
            - --tls-cert=/var/run/argus-telemetry-client/client.pem
            - --tls-key=/var/run/argus-telemetry-client/client-key.pem
            - --tls-server-name=collector-${collector_id}.argus.telemetry
          volumeMounts:
            - {name: telemetry-identity, mountPath: /var/run/argus-telemetry-client, readOnly: true}
      volumes:
        - name: telemetry-identity
          secret: {secretName: argus-otelcol-identity, defaultMode: 288}
EOF
	k -n "$M7_COLLECTOR_NAMESPACE" wait --for=condition=complete "job/${job_name}" --timeout=3m >/dev/null
}

m7_verify_bastion_gateway_path() {
	local gateway_collector_id leaf_collector_id job_name=argus-m7-bastion-gateway from to
	gateway_collector_id=$(m3_psql "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='${M3_CLUSTER_ID}' ORDER BY created_at DESC LIMIT 1;")
	leaf_collector_id=$(m3_psql "SELECT id FROM collector_instances WHERE resource_type='host' AND resource_id='${M7_HOST_ID}' ORDER BY created_at DESC LIMIT 1;")
	[[ -n "$gateway_collector_id" && -n "$leaf_collector_id" && -n "${M3_BASTION_ROOT_HOST_ID:-}" && -n "${M3_BASTION_CLUSTER_ID:-}" ]] || \
		fail "M7 bastion_gateway identities or M3 Bastion resources are unavailable"

	for file in client.pem client-key.pem ca.pem; do
		k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-m7-systemd-host -- \
			cat "/var/lib/argus-otelcol/identity/${file}" >"${WORK_DIR}/m7-leaf-${file}"
	done
	k -n "$M7_COLLECTOR_NAMESPACE" delete secret argus-m7-bastion-leaf-identity --ignore-not-found=true >/dev/null
	k -n "$M7_COLLECTOR_NAMESPACE" create secret generic argus-m7-bastion-leaf-identity \
		--from-file=client.pem="${WORK_DIR}/m7-leaf-client.pem" \
		--from-file=client-key.pem="${WORK_DIR}/m7-leaf-client-key.pem" \
		--from-file=ca.pem="${WORK_DIR}/m7-leaf-ca.pem" >/dev/null
	rm -f "${WORK_DIR}/m7-leaf-client.pem" "${WORK_DIR}/m7-leaf-client-key.pem" "${WORK_DIR}/m7-leaf-ca.pem"

	# Rebind the two already enrolled Evaluation identities to a real Bastion
	# Scope pair. The Gateway process and Leaf certificate remain unchanged;
	# Ingest must resolve the authoritative relationship from PostgreSQL.
	m3_psql "
		UPDATE collector_instances SET resource_type='host',resource_id='${M3_BASTION_ROOT_HOST_ID}',role='edge_gateway',updated_at=now() WHERE id='${gateway_collector_id}';
		UPDATE collector_instances SET resource_type='kubernetes_cluster',resource_id='${M3_BASTION_CLUSTER_ID}',role='leaf',updated_at=now() WHERE id='${leaf_collector_id}';
		UPDATE telemetry_routes SET kind='bastion_gateway',gateway_collector_id='${gateway_collector_id}',status='active',updated_at=now() WHERE collector_id='${leaf_collector_id}';" >/dev/null

	k -n "$M7_COLLECTOR_NAMESPACE" delete job "$job_name" --ignore-not-found=true >/dev/null
	cat <<EOF | k -n "$M7_COLLECTOR_NAMESPACE" apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata: {name: ${job_name}}
spec:
  backoffLimit: 2
  template:
    spec:
      restartPolicy: Never
      securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532}
      containers:
        - name: generator
          image: ${BACKEND_IMAGE}
          imagePullPolicy: Never
          command: [/usr/local/bin/argus-telemetry-e2e]
          args:
            - --endpoint=argus-otelcol-gateway.${M7_COLLECTOR_NAMESPACE}.svc.cluster.local:4317
            - --resource-id=${M3_BASTION_CLUSTER_ID}
            - --marker=bastion-gateway
            - --tls-ca=/var/run/argus-telemetry-client/ca.pem
            - --tls-cert=/var/run/argus-telemetry-client/client.pem
            - --tls-key=/var/run/argus-telemetry-client/client-key.pem
            - --tls-server-name=collector-${gateway_collector_id}.argus.telemetry
          volumeMounts:
            - {name: telemetry-identity, mountPath: /var/run/argus-telemetry-client, readOnly: true}
      volumes:
        - name: telemetry-identity
          secret: {secretName: argus-m7-bastion-leaf-identity, defaultMode: 288}
EOF
	k -n "$M7_COLLECTOR_NAMESPACE" wait --for=condition=complete "job/${job_name}" --timeout=3m >/dev/null

	from=$(date -u -v-10M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '-10 minutes' +%Y-%m-%dT%H:%M:%SZ)
	to=$(date -u -v+1M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 minute' +%Y-%m-%dT%H:%M:%SZ)
	for _ in $(seq 1 120); do
		request m7-bastion-metrics 200 POST /enterprise/telemetry/query/metrics "$ENTERPRISE_JAR" \
			"$(jq -nc --arg id "$M3_BASTION_CLUSTER_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,metric_name:"argus.m7.e2e.gauge.bastion-gateway",aggregation:"avg",step_seconds:60,limit:100}')" \
			--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
		jq -e 'any(.series[]; .metric_name == "argus.m7.e2e.gauge.bastion-gateway")' "$RESPONSE_FILE" >/dev/null 2>&1 && break
		sleep 2
	done
	jq -e 'any(.series[]; .metric_name == "argus.m7.e2e.gauge.bastion-gateway") and .meta.partial == false' "$RESPONSE_FILE" >/dev/null \
		|| fail "bastion_gateway Metric did not reach the authorized downstream resource"
	for _ in $(seq 1 120); do
		request m7-bastion-logs 200 POST /enterprise/telemetry/query/logs "$ENTERPRISE_JAR" \
			"$(jq -nc --arg id "$M3_BASTION_CLUSTER_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",text:"argus m7 e2e log.bastion-gateway",limit:100}')" \
			--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
		jq -e 'any(.records[]; .body == "argus m7 e2e log.bastion-gateway")' "$RESPONSE_FILE" >/dev/null 2>&1 && break
		sleep 2
	done
	jq -e 'any(.records[]; .body == "argus m7 e2e log.bastion-gateway") and .meta.partial == false' "$RESPONSE_FILE" >/dev/null \
		|| fail "bastion_gateway Log did not reach the authorized downstream resource"
	for _ in $(seq 1 120); do
		request m7-bastion-traces 200 POST /enterprise/telemetry/query/traces "$ENTERPRISE_JAR" \
			"$(jq -nc --arg id "$M3_BASTION_CLUSTER_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",operation:"argus-m7-e2e.bastion-gateway",limit:100}')" \
			--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
		jq -e 'any(.traces[]; .root_span_name == "argus-m7-e2e.bastion-gateway")' "$RESPONSE_FILE" >/dev/null 2>&1 && break
		sleep 2
	done
	jq -e 'any(.traces[]; .root_span_name == "argus-m7-e2e.bastion-gateway") and .meta.partial == false' "$RESPONSE_FILE" >/dev/null \
		|| fail "bastion_gateway Trace did not reach the authorized downstream resource"

	m3_psql "
		UPDATE collector_instances SET resource_type='kubernetes_cluster',resource_id='${M3_CLUSTER_ID}',role='daemonset',updated_at=now() WHERE id='${gateway_collector_id}';
		UPDATE collector_instances SET resource_type='host',resource_id='${M7_HOST_ID}',role='direct',updated_at=now() WHERE id='${leaf_collector_id}';
		UPDATE telemetry_routes SET kind='direct_argus',gateway_collector_id=NULL,status='active',updated_at=now() WHERE collector_id='${leaf_collector_id}';" >/dev/null
}

m7_verify_query_security() {
	local from to sensitive_body='Authorization: Bearer m7-redaction-fixture'
	from=$(date -u -v-15M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '-15 minutes' +%Y-%m-%dT%H:%M:%SZ)
	to=$(date -u -v+1M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 minute' +%Y-%m-%dT%H:%M:%SZ)

	request m7-query-cross-enterprise 403 POST /enterprise/telemetry/query/metrics "$OTHER_ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M7_HOST_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,metric_name:"system.cpu.utilization",aggregation:"avg",step_seconds:60,limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}"

	request m7-query-partial 200 POST /enterprise/telemetry/query/metrics "$ENTERPRISE_JAR" \
		"$(jq -nc --arg allowed "$M7_HOST_ID" --arg denied "$M3_BASTION_HOST_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$allowed,$denied],from:$from,to:$to,metric_name:"system.cpu.utilization",aggregation:"avg",step_seconds:60,limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e --arg allowed "$M7_HOST_ID" --arg denied "$M3_BASTION_HOST_ID" \
		'.meta.partial == true and (.meta.partial_reasons | index("unauthorized_resources")) != null and all(.series[]; .resource_id == $allowed and .resource_id != $denied)' \
		"$RESPONSE_FILE" >/dev/null || fail "Telemetry Query did not safely report DataScope partial results"

	k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-direct-executor -- \
		/usr/local/bin/argus-telemetry-e2e --endpoint=127.0.0.1:4317 --resource-id="$M7_HOST_ID" --marker=limit-a
	k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-direct-executor -- \
		/usr/local/bin/argus-telemetry-e2e --endpoint=127.0.0.1:4317 --resource-id="$M7_HOST_ID" --marker=limit-b --log-body="$sensitive_body"
	for _ in $(seq 1 120); do
		request m7-query-sensitive-raw 200 POST /enterprise/telemetry/query/logs "$ENTERPRISE_JAR" \
			"$(jq -nc --arg id "$M7_HOST_ID" --arg from "$from" --arg to "$to" --arg body "$sensitive_body" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",text:$body,limit:10}')" \
			--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
		jq -e --arg body "$sensitive_body" 'any(.records[]; .body == $body)' "$RESPONSE_FILE" >/dev/null 2>&1 && break
		sleep 2
	done
	jq -e --arg body "$sensitive_body" 'any(.records[]; .body == $body)' "$RESPONSE_FILE" >/dev/null \
		|| fail "sensitive telemetry fixture did not reach Query"
	request m7-query-limit 200 POST /enterprise/telemetry/query/logs "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M7_HOST_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",text:"",limit:1}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e '.records | length == 1' "$RESPONSE_FILE" >/dev/null || fail "Telemetry Query did not enforce its result-row limit"

	m3_psql "
		DELETE FROM role_permissions WHERE permission_id='telemetry.sensitive_fields.read' AND role_id=(SELECT id FROM roles WHERE enterprise_id='${ENTERPRISE_ID}' AND identity_key='enterprise_admin');
		UPDATE enterprise_users SET authorization_version=authorization_version+1,updated_at=now() WHERE id='${ADMIN_USER_ID}' AND enterprise_id='${ENTERPRISE_ID}';" >/dev/null
	request m7-query-stale-session 409 POST /enterprise/telemetry/query/logs "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M7_HOST_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",text:"",limit:10}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e '.code == "AUTHORIZATION_VERSION_STALE"' "$RESPONSE_FILE" >/dev/null
	rm -f "$ENTERPRISE_JAR"
	enterprise_login
	request m7-query-sensitive-redacted 200 POST /enterprise/telemetry/query/logs "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M7_HOST_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",text:"",limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e --arg body "$sensitive_body" 'all(.records[]; .body != $body) and any(.records[]; .body == "[redacted by telemetry field policy]")' "$RESPONSE_FILE" >/dev/null \
		|| fail "telemetry.sensitive_fields.read removal did not redact governed log bodies"

	m3_psql "
		INSERT INTO role_permissions (role_id,permission_id) SELECT id,'telemetry.sensitive_fields.read' FROM roles WHERE enterprise_id='${ENTERPRISE_ID}' AND identity_key='enterprise_admin' ON CONFLICT DO NOTHING;
		UPDATE enterprise_users SET authorization_version=authorization_version+1,updated_at=now() WHERE id='${ADMIN_USER_ID}' AND enterprise_id='${ENTERPRISE_ID}';" >/dev/null
	rm -f "$ENTERPRISE_JAR"
	enterprise_login
}

m7_wait_metric() {
	local metric_name=$1 from to
	from=$(date -u -v-15M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '-15 minutes' +%Y-%m-%dT%H:%M:%SZ)
	to=$(date -u -v+1M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 minute' +%Y-%m-%dT%H:%M:%SZ)
	for _ in $(seq 1 120); do
		request "m7-metric-${metric_name//./-}" 200 POST /enterprise/telemetry/query/metrics "$ENTERPRISE_JAR" \
			"$(jq -nc --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" --arg metric "$metric_name" '{resource_ids:[$id],from:$from,to:$to,metric_name:$metric,aggregation:"avg",step_seconds:60,limit:100}')" \
			--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
		if jq -e --arg metric "$metric_name" 'any(.series[]; .metric_name == $metric and (.points | length > 0))' "$RESPONSE_FILE" >/dev/null 2>&1; then
			return
		fi
		sleep 2
	done
	fail "M7 metric ${metric_name} did not converge"
}

m7_inject_permanent_record() {
	local collector_id=$1 job_name=argus-m7-dlq-inject
	k -n "$OBSERVABILITY_NS" delete job "$job_name" --ignore-not-found=true >/dev/null
	cat <<EOF | k -n "$OBSERVABILITY_NS" apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata: {name: ${job_name}}
spec:
  backoffLimit: 1
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: inject
          image: ${BACKEND_IMAGE}
          imagePullPolicy: Never
          env:
            - name: ARGUS_E2E_KAFKA_PASSWORD
              valueFrom: {secretKeyRef: {name: argus-telemetry, key: password}}
          command: [/usr/local/bin/argus-telemetry-e2e]
          args: [--kafka-brokers=argus-kafka-kafka-bootstrap.${OBSERVABILITY_NS}.svc:9093, --kafka-username=argus-telemetry, --enterprise-id=${ENTERPRISE_ID}, --resource-id=${M3_CLUSTER_ID}, --collector-id=${collector_id}]
EOF
	k -n "$OBSERVABILITY_NS" wait --for=condition=complete "job/${job_name}" --timeout=2m >/dev/null
}

m7_replay_dlq() {
	local record_id=$1 job_name
	job_name="argus-m7-dlq-replay-${record_id//-/}"
	job_name=${job_name:0:55}
	k -n "$OBSERVABILITY_NS" delete job "$job_name" --ignore-not-found=true >/dev/null
	cat <<EOF | k -n "$OBSERVABILITY_NS" apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata: {name: ${job_name}}
spec:
  backoffLimit: 1
  template:
    spec:
      restartPolicy: Never
      serviceAccountName: argus-telemetry-writer
      containers:
        - name: replay
          image: ${BACKEND_IMAGE}
          imagePullPolicy: Never
          command: [/usr/local/bin/argus-telemetry-dlq-replay]
          args: [--record-id=${record_id}]
          env:
            - name: ARGUS_DATABASE_URL
              valueFrom: {secretKeyRef: {name: argus-telemetry-runtime, key: database-url}}
            - {name: ARGUS_KAFKA_BROKERS, value: "argus-kafka-kafka-bootstrap.${OBSERVABILITY_NS}.svc:9093"}
            - {name: ARGUS_KAFKA_USERNAME, value: argus-telemetry}
            - name: ARGUS_KAFKA_PASSWORD
              valueFrom: {secretKeyRef: {name: argus-telemetry, key: password}}
EOF
	k -n "$OBSERVABILITY_NS" wait --for=condition=complete "job/${job_name}" --timeout=2m >/dev/null
}

m7_restart_deployment() {
	local workload=$1 pod_name
	pod_name=$(k -n "$OBSERVABILITY_NS" get pods \
		-l "app.kubernetes.io/name=${workload}" \
		-o jsonpath='{.items[0].metadata.name}')
	[[ -n "$pod_name" ]] || fail "${workload} has no Pod to restart"
	k -n "$OBSERVABILITY_NS" delete "pod/${pod_name}" --wait=true --timeout=180s >/dev/null
	k -n "$OBSERVABILITY_NS" rollout status "deployment/${workload}" --timeout=300s >/dev/null
	k -n "$OBSERVABILITY_NS" wait --for=condition=Ready pod \
		-l "app.kubernetes.io/name=${workload}" --timeout=300s >/dev/null
}

run_m7_api_flow() {
  log "verifying M7 catalog, query policy, and telemetry workloads"
  for workload in argus-telemetry-ingest argus-telemetry-writer argus-telemetry-query; do
    k -n "$OBSERVABILITY_NS" rollout status "deployment/${workload}" --timeout=300s >/dev/null
  done

  request m7-distributions 200 GET /enterprise/telemetry/distributions "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '
    any(.[]; .support_status == "supported" and any(.artifacts[]; .platform == "linux_arm64")) and
    any(.[]; .support_status == "validation_pending" and any(.artifacts[]; .platform == "windows_amd64"))
  ' "$RESPONSE_FILE" >/dev/null
	jq -e --arg sha "$M7_WINDOWS_ARTIFACT_SHA" '
		any(.[]; .support_status == "validation_pending" and any(.artifacts[]; .platform == "windows_amd64" and .sha256 == $sha))
	' "$RESPONSE_FILE" >/dev/null || fail "Windows Catalog artifact metadata does not match the ZIP"
	request m7-profiles 200 GET /enterprise/telemetry/profiles "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	jq -e 'any(.[]; .key == "host-basic") and any(.[]; .key == "k8s-node-container")' "$RESPONSE_FILE" >/dev/null
	request m7-telemetry-card 200 GET /enterprise/interactive-cards "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	jq -e 'any(.items[]; .slug == "telemetry-overview" and .availability == "available" and .enabled == true)' "$RESPONSE_FILE" >/dev/null

	local distribution_id profile_ids host_profile_ids action_ref resource_admin_role_id m7_host_scope_id
	request m7-initial-roles 200 GET /enterprise/roles "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	resource_admin_role_id=$(jq -er '.items[] | select(.builtin == true and .name == "Resource Admin") | .id' "$RESPONSE_FILE")
	request m7-host-scope 201 POST /enterprise/data-scopes "$ENTERPRISE_JAR" \
		'{"name":"M7 Linux arm64 hosts","resource_types":["host"],"explicit_resource_ids":[],"label_selector":{"schema_version":"argus.label_selector/v1","requirements":[{"key":"team","operator":"eq","values":["m7"]}]}}' \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-host-scope-${RUN_ID}"
	m7_host_scope_id=$(jq -er '.id' "$RESPONSE_FILE")
	request m7-host-binding 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
		"$(jq -nc --arg subject "$ADMIN_USER_ID" --arg role "$resource_admin_role_id" --arg scope "$m7_host_scope_id" \
			'{subject_type:"user",subject_id:$subject,role_id:$role,data_scope_ids:[$scope]}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-host-binding-${RUN_ID}"
	rm -f "$ENTERPRISE_JAR"
	enterprise_login

	request m7-distributions-for-install 200 GET /enterprise/telemetry/distributions "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	distribution_id=$(jq -er '.[] | select(.support_status == "supported" and any(.artifacts[]; .platform == "linux_arm64")) | .id' "$RESPONSE_FILE" | head -n1)
	request m7-profiles-for-install 200 GET /enterprise/telemetry/profiles "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	host_profile_ids=$(jq -c '[.[] | select(.key == "host-basic" or .key == "linux-journald" or .key == "otlp-receiver") | .id]' "$RESPONSE_FILE")
	profile_ids=$(jq -c '[.[] | select(.key == "k8s-node-container" or .key == "k8s-cluster" or .key == "otlp-receiver") | .id]' "$RESPONSE_FILE")
	[[ "$(jq 'length' <<<"$host_profile_ids")" -eq 3 ]] || fail "M7 Host profiles are incomplete"
	[[ "$(jq 'length' <<<"$profile_ids")" -ge 2 ]] || fail "M7 Kubernetes profiles are incomplete"

	request m7-systemd-host-test 202 POST /enterprise/hosts/connection-tests "$ENTERPRISE_JAR" \
		"$(jq -nc --arg credential "$M3_CREDENTIAL_ID" '{address:"8.8.8.8",port:22,platform:"linux",connection_mode:"direct_ssh",credential_id:$credential,username:"root"}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-systemd-host-test-${RUN_ID}"
	local host_test_id
	host_test_id=$(jq -er '.id' "$RESPONSE_FILE")
	m3_wait_connection_test m7-systemd-host-test-result "$host_test_id"
	request m7-systemd-host-preview 201 POST /enterprise/hosts/actions/preview-create "$ENTERPRISE_JAR" \
		"$(jq -nc --arg credential "$M3_CREDENTIAL_ID" --arg test "$host_test_id" \
			'{name:"m7-linux-arm64-systemd",address:"8.8.8.8",port:22,platform:"linux",connection_mode:"direct_ssh",credential_id:$credential,username:"root",environment:"production",labels:{team:"m7",runtime:"systemd"},connection_test_id:$test}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-systemd-host-${RUN_ID}"
	action_ref=$(jq -er '.action_ref' "$RESPONSE_FILE")
	m3_confirm m7-systemd-host-confirm "$action_ref"
	M7_HOST_ID=$(jq -er '.resource_ref.resource_id' "$RESPONSE_FILE")
	rm -f "$ENTERPRISE_JAR"
	enterprise_login
	request m7-host-collector-preview 201 POST "/enterprise/hosts/${M7_HOST_ID}/collector/actions/preview-install" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg distribution "$distribution_id" --argjson profiles "$host_profile_ids" \
			'{distribution_version_id:$distribution,profile_ids:$profiles,route_kind:"direct_argus"}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-host-collector-install-${RUN_ID}"
	action_ref=$(jq -er '.action_ref' "$RESPONSE_FILE")
	m3_confirm m7-host-collector-install-confirm "$action_ref"
	m7_wait_host_collector converged
	k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-m7-systemd-host -- systemctl is-active --quiet argus-otelcol.service
	local receiver_ready=false
	for _ in $(seq 1 60); do
		if k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-m7-systemd-host -- \
			bash -c 'exec 3<>/dev/tcp/127.0.0.1/4317' >/dev/null 2>&1; then
			receiver_ready=true
			break
		fi
		sleep 1
	done
	[[ "$receiver_ready" == true ]] || fail "M7 Host Collector OTLP receiver did not become ready"
	request m7-host-collector-current 200 GET "/enterprise/hosts/${M7_HOST_ID}/collector" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	local host_collector_id
	host_collector_id=$(jq -er '.id' "$RESPONSE_FILE")
	request m7-host-route-test 202 POST /enterprise/telemetry/routes/tests "$ENTERPRISE_JAR" \
		"$(jq -nc --arg collector "$host_collector_id" '{collector_id:$collector,route_kind:"direct_argus"}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-host-route-test-${RUN_ID}"
	jq -e '.status == "succeeded"' "$RESPONSE_FILE" >/dev/null
	m7_query_host_signals
	for lifecycle in configure repair upgrade; do
		m7_apply_host_collector_action "$lifecycle" "$distribution_id" "$host_profile_ids"
	done
	request m7-kubernetes-collector-preview 201 POST "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}/collector/actions/preview-install" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg distribution "$distribution_id" --argjson profiles "$profile_ids" '{distribution_version_id:$distribution,profile_ids:$profiles,route_kind:"direct_argus"}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-kubernetes-collector-preview-${RUN_ID}"
	action_ref=$(jq -er '.action_ref' "$RESPONSE_FILE")
	M7_CREATED_COLLECTOR_NAMESPACE=true
	m3_confirm m7-kubernetes-collector-confirm "$action_ref"
	k -n "$M7_COLLECTOR_NAMESPACE" rollout status daemonset/argus-otelcol-agent --timeout=5m >/dev/null
	k -n "$M7_COLLECTOR_NAMESPACE" rollout status deployment/argus-otelcol-gateway --timeout=5m >/dev/null
	if k -n "$M7_COLLECTOR_NAMESPACE" get secret argus-otelcol-enrollment >/dev/null 2>&1; then
		fail "consumed Kubernetes Collector enrollment Secret was not deleted"
	fi

	request m7-node-bindings 200 GET "/enterprise/telemetry/node-host-bindings?kubernetes_cluster_id=${M3_CLUSTER_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	local binding_id binding_version kubernetes_collector_version kubernetes_action_ref
	jq -e 'length > 0' "$RESPONSE_FILE" >/dev/null || fail "Kubernetes Collector converged without producing Node/Host binding evidence"
	binding_id=$(jq -er '.[0].id' "$RESPONSE_FILE")
	binding_version=$(jq -er '.[0].version' "$RESPONSE_FILE")
	request m7-node-binding-preview 201 POST "/enterprise/telemetry/node-host-bindings/${binding_id}/actions/preview-confirm" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg host "$M7_HOST_ID" --argjson version "$binding_version" '{host_id:$host,expected_version:$version}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-node-binding-${RUN_ID}"
	kubernetes_action_ref=$(jq -er '.action_ref' "$RESPONSE_FILE")
	m3_confirm m7-node-binding-confirm "$kubernetes_action_ref"

	request m7-kubernetes-collector-current 200 GET "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}/collector" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	kubernetes_collector_version=$(jq -er '.version' "$RESPONSE_FILE")
	request m7-kubernetes-collector-configure 201 POST "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}/collector/actions/preview-configure" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg distribution "$distribution_id" --argjson profiles "$profile_ids" --argjson version "$kubernetes_collector_version" \
			'{distribution_version_id:$distribution,profile_ids:$profiles,route_kind:"direct_argus",expected_version:$version}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-kubernetes-configure-verified-${RUN_ID}"
	kubernetes_action_ref=$(jq -er '.action_ref' "$RESPONSE_FILE")
	m3_confirm m7-kubernetes-configure-verified-confirm "$kubernetes_action_ref"
	request m7-node-binding-stable 200 GET "/enterprise/telemetry/node-host-bindings?kubernetes_cluster_id=${M3_CLUSTER_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	jq -e --arg id "$binding_id" --arg host "$M7_HOST_ID" 'any(.[]; .id == $id and .status == "verified" and .matched_by == "manual" and .host_id == $host)' "$RESPONSE_FILE" >/dev/null \
		|| fail "unchanged Kubernetes Node evidence did not preserve the verified manual binding"
	request m7-host-claims 200 GET "/enterprise/telemetry/collection-claims?resource_id=${M7_HOST_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	jq -e --arg collector "$(m3_psql "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='${M3_CLUSTER_ID}';")" \
		--arg ref "host:${M7_HOST_ID}" 'any(.[]; .collector_id == $collector and .physical_resource_ref == $ref and .status == "active")' "$RESPONSE_FILE" >/dev/null \
		|| fail "verified Node/Host binding did not move Kubernetes node Claims to the Host physical identity"

	m3_psql "UPDATE kubernetes_node_host_bindings SET evidence_hash=decode(repeat('00',32),'hex') WHERE id='${binding_id}';" >/dev/null
	request m7-kubernetes-collector-current-after-drift 200 GET "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}/collector" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	kubernetes_collector_version=$(jq -er '.version' "$RESPONSE_FILE")
	request m7-kubernetes-collector-configure-after-drift 201 POST "/enterprise/kubernetes-clusters/${M3_CLUSTER_ID}/collector/actions/preview-configure" "$ENTERPRISE_JAR" \
		"$(jq -nc --arg distribution "$distribution_id" --argjson profiles "$profile_ids" --argjson version "$kubernetes_collector_version" \
			'{distribution_version_id:$distribution,profile_ids:$profiles,route_kind:"direct_argus",expected_version:$version}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m7-kubernetes-configure-drift-${RUN_ID}"
	kubernetes_action_ref=$(jq -er '.action_ref' "$RESPONSE_FILE")
	m3_confirm m7-kubernetes-configure-drift-confirm "$kubernetes_action_ref"
	request m7-node-binding-drifted 200 GET "/enterprise/telemetry/node-host-bindings?kubernetes_cluster_id=${M3_CLUSTER_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	jq -e --arg id "$binding_id" 'any(.[]; .id == $id and .status == "proposed")' "$RESPONSE_FILE" >/dev/null \
		|| fail "changed Kubernetes Node evidence did not invalidate the verified binding"
	request m7-cluster-claims-after-drift 200 GET "/enterprise/telemetry/collection-claims?resource_id=${M3_CLUSTER_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
	jq -e --arg ref "kubernetes_cluster:${M3_CLUSTER_ID}" 'any(.[]; .physical_resource_ref == $ref and .status == "active")' "$RESPONSE_FILE" >/dev/null \
		|| fail "invalidated Node binding did not restore Kubernetes cluster-scoped Claims"

	m7_run_generator ""

  local from to
  from=$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%SZ)
  to=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  request m7-metrics-empty 200 POST /enterprise/telemetry/query/metrics "$ENTERPRISE_JAR" \
    "$(jq -nc --arg id "$M3_DIRECT_HOST_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,metric_name:"system.cpu.utilization",aggregation:"avg",step_seconds:60,limit:100}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  jq -e '.meta.schema_version == "argus.telemetry_result/v1" and (.series | type == "array")' "$RESPONSE_FILE" >/dev/null

	from=$(date -u -v-10M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '-10 minutes' +%Y-%m-%dT%H:%M:%SZ)
	to=$(date -u -v+1M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 minute' +%Y-%m-%dT%H:%M:%SZ)
	local signal_found=false
	for _ in $(seq 1 120); do
		request m7-metrics-real 200 POST /enterprise/telemetry/query/metrics "$ENTERPRISE_JAR" \
			"$(jq -nc --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,metric_name:"argus.m7.e2e.gauge",aggregation:"avg",step_seconds:60,limit:100}')" \
			--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
		if jq -e 'any(.series[]; .metric_name == "argus.m7.e2e.gauge" and (.points | length > 0))' "$RESPONSE_FILE" >/dev/null 2>&1; then signal_found=true; break; fi
		sleep 2
	done
	[[ "$signal_found" == true ]] || fail "M7 metric did not traverse Collector, Kafka, Writer, ClickHouse, and Query"
	request m7-logs-real 200 POST /enterprise/telemetry/query/logs "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",text:"argus m7 e2e log",limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e 'any(.records[]; .body == "argus m7 e2e log")' "$RESPONSE_FILE" >/dev/null
	request m7-traces-real 200 POST /enterprise/telemetry/query/traces "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,service_name:"argus-m7-e2e",operation:"argus-m7-e2e",limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e 'any(.traces[]; .root_span_name == "argus-m7-e2e")' "$RESPONSE_FILE" >/dev/null

	log "verifying Leaf Collector traffic through the Bastion Edge Gateway"
	m7_verify_bastion_gateway_path

	log "verifying Kafka backlog while Writer is stopped"
	k -n "$OBSERVABILITY_NS" scale deployment/argus-telemetry-writer --replicas=0 >/dev/null
	k -n "$OBSERVABILITY_NS" wait --for=delete pod -l app.kubernetes.io/name=argus-telemetry-writer --timeout=180s >/dev/null
	m7_run_generator backlog
	sleep 5
	request m7-backlog-pending 200 POST /enterprise/telemetry/query/metrics "$ENTERPRISE_JAR" \
		"$(jq -nc --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" '{resource_ids:[$id],from:$from,to:$to,metric_name:"argus.m7.e2e.gauge.backlog",aggregation:"avg",step_seconds:60,limit:100}')" \
		--header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
	jq -e '.series | length == 0' "$RESPONSE_FILE" >/dev/null
	k -n "$OBSERVABILITY_NS" scale deployment/argus-telemetry-writer --replicas=1 >/dev/null
	k -n "$OBSERVABILITY_NS" rollout status deployment/argus-telemetry-writer --timeout=300s >/dev/null
	m7_wait_metric argus.m7.e2e.gauge.backlog

	log "verifying permanent record isolation and controlled DLQ replay"
	local collector_id dlq_id
	collector_id=$(m3_psql "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='${M3_CLUSTER_ID}' ORDER BY created_at DESC LIMIT 1;")
	[[ -n "$collector_id" ]] || fail "M7 Kubernetes Collector identity was not persisted"
	m7_inject_permanent_record "$collector_id"
	for _ in $(seq 1 60); do
		dlq_id=$(m3_psql "SELECT id FROM telemetry_dlq_records WHERE status='pending' ORDER BY first_seen_at DESC LIMIT 1;" 2>/dev/null || true)
		[[ -n "$dlq_id" ]] && break
		sleep 2
	done
	[[ -n "$dlq_id" ]] || fail "M7 Writer did not isolate a permanent record in DLQ"
	m7_replay_dlq "$dlq_id"
	[[ "$(m3_psql "SELECT status FROM telemetry_dlq_records WHERE id='${dlq_id}';")" == "replayed" ]] || fail "M7 DLQ replay status did not converge"

	log "verifying Collector persistent queue after Redis outage"
	k -n "$SYSTEM_NS" scale statefulset/argus-redis --replicas=0 >/dev/null
	k -n "$SYSTEM_NS" wait --for=delete pod/argus-redis-0 --timeout=120s >/dev/null
	m7_run_generator redis-recovery
	k -n "$SYSTEM_NS" scale statefulset/argus-redis --replicas=1 >/dev/null
	k -n "$SYSTEM_NS" rollout status statefulset/argus-redis --timeout=300s >/dev/null
	m7_wait_metric argus.m7.e2e.gauge.redis-recovery
	log "verifying Query enterprise isolation, DataScope partials, budgets, redaction, and authorization-version invalidation"
	m7_verify_query_security
	run_m7_playwright
	m7_apply_host_collector_action uninstall "$distribution_id" "$host_profile_ids"
	if k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-m7-systemd-host -- systemctl is-active --quiet argus-otelcol.service; then
		fail "M7 Host Collector systemd unit remains active after uninstall"
	fi
	k -n "$SYSTEM_NS" exec deployment/argus-direct-executor -c argus-m7-systemd-host -- test ! -e /etc/systemd/system/argus-otelcol.service

  log "verifying PostgreSQL recovery after telemetry Pod deletion"
  m7_restart_deployment argus-telemetry-query
  request m7-query-after-restart 200 POST /enterprise/telemetry/query/overview "$ENTERPRISE_JAR" \
    "$(jq -nc --arg id "$M3_DIRECT_HOST_ID" '{resource_ids:[$id],lookback_seconds:3600}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"

	log "verifying telemetry services recover after Redis cache loss"
  k -n "$SYSTEM_NS" exec statefulset/argus-redis -- redis-cli -a "$REDIS_PASSWORD" FLUSHALL >/dev/null 2>&1
  m7_restart_deployment argus-telemetry-ingest
  m7_restart_deployment argus-telemetry-writer
}
