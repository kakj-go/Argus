#!/usr/bin/env bash

# M10 reuses the proven telemetry workload setup from M7, but routes every
# query assertion through the native per-language HTTP contracts. The legacy
# workload still supplies signal/query/time fields; this adapter deliberately
# discards its old envelope fields and constructs a new protocol body.
eval "$(declare -f request | sed '1s/^request /request_m10_legacy /')"
eval "$(declare -f run_m7_api_flow | sed '1s/^run_m7_api_flow /run_m10_base_api_flow /')"

m7_query_payload() {
  local signal=$1 language=$2 query=$3 from=$4 to=$5 limit=$6 first_id=$7 second_id=${8:-}
  jq -nc --arg signal "$signal" --arg query "$query" --arg from "$from" --arg to "$to" \
    --arg first "$first_id" --arg second "$second_id" --argjson limit "$limit" \
    '{query_signal:$signal,query:$query,resource_ids:([$first,$second] | map(select(length > 0))),
      time_range:{from:$from,to:$to},step_seconds:60,budget:{max_scan_bytes:268435456,max_rows:$limit,max_samples:5000000,max_series:100000,timeout_ms:10000}}'
}

m7_log_query() {
  local value=$1
  value=${value//\"/\\\"}
  printf 'service_name = argus-m7-e2e AND body : "%s"' "$value"
}

m7_log_stream_query() {
  printf 'service_name = argus-m7-e2e'
}

m7_trace_query() {
  local value=$1
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  printf 'query { queryBasicTracesByName(serviceName: "argus-m7-e2e", operationName: "%s", pageSize: 100) { total traces { traceId rootService rootOperation spans { spanId operationName } } } }' "$value"
}

request() {
  local name=$1 expected=$2 method=$3 path=$4 jar=$5 body=$6
  shift 6
  if [[ "$path" != "/enterprise/telemetry/query" ]]; then
    request_m10_legacy "$name" "$expected" "$method" "$path" "$jar" "$body" "$@"
    return
  fi
  local signal new_path new_body step
  signal=$(jq -r '.query_signal // empty' <<<"$body")
  step=$(jq -r '.step_seconds // 0' <<<"$body")
  case "$signal" in
    metrics)
      if [[ "$step" -gt 0 ]]; then
        new_path=/enterprise/metrics/query_range
        new_body=$(jq '(.budget.max_rows = (.budget.max_rows // 50000)) | {query,resource_ids,time_range,step_seconds,budget}' <<<"$body")
      else
        new_path=/enterprise/metrics/query
        new_body=$(jq '(.budget.max_rows = (.budget.max_rows // 50000)) | {query,resource_ids,time_range,budget}' <<<"$body")
      fi
      ;;
    logs)
      new_path=/enterprise/logs/query
      new_body=$(jq '(.budget.max_rows = (.budget.max_rows // 50000)) | {query,pipeline,resource_ids,time_range,budget}' <<<"$body")
      ;;
    traces)
      new_path=/enterprise/traces/graphql
      new_body=$(jq '(.budget.max_rows = (.budget.max_rows // 50000)) | {query,operation_name,variables,resource_ids,time_range,budget}' <<<"$body")
      ;;
    *)
      request_m10_legacy "$name" "$expected" "$method" "$path" "$jar" "$body" "$@"
      return
      ;;
  esac
  request_m10_legacy "$name" "$expected" "$method" "$new_path" "$jar" "$new_body" "$@"
}

m7_assert_query_v2() {
  local language=$1 result_type=$2
  case "$language" in
    promql)
      jq -e --arg result_type "$result_type" \
        '.status == "success" and
         (if $result_type == "vector" then (.data.resultType == "vector" or .data.resultType == "matrix") else .data.resultType == $result_type end) and
         (.data.result | type == "array") and (.warnings | type == "array") and
         (.argus_meta.partial | type == "boolean") and
         (.argus_meta.scanned_bytes | type == "number") and (.argus_meta.elapsed_ms | type == "number") and
         (.argus_meta.plan_hash | type == "string" and length == 64)' "$RESPONSE_FILE" >/dev/null
      return
      ;;
    logql|kql)
      jq -e --arg result_type "$result_type" \
        '.schema_version == "argus.kql_result/v1" and .result_type == $result_type and
         (.data | type == "array") and (.warnings | type == "array") and (.partial | type == "boolean") and
         (.meta.scanned_bytes | type == "number") and (.meta.elapsed_ms | type == "number") and
         (.meta.plan_hash | type == "string" and length == 64)' "$RESPONSE_FILE" >/dev/null
      return
      ;;
    traceql|skywalking_graphql)
      jq -e '(.data | type == "object") and
        (.extensions.argus.partial | type == "boolean") and
        (.extensions.argus.scanned_bytes | type == "number") and (.extensions.argus.elapsed_ms | type == "number") and
        (.extensions.argus.plan_hash | type == "string" and length == 64)' "$RESPONSE_FILE" >/dev/null
      return
      ;;
  esac
  fail "unsupported M10 query assertion language ${language}"
}

m7_assert_metric_result() {
  m7_assert_query_v2 promql vector
  jq -e '.data.result | type == "array"' "$RESPONSE_FILE" >/dev/null
}

m7_assert_log_result() {
  m7_assert_query_v2 kql log_entries
  jq -e '.data | type == "array"' "$RESPONSE_FILE" >/dev/null
}

m7_assert_sensitive_log_redacted() {
  local body=$1
  jq -e --arg body "$body" \
    'all(.data[]; .body != $body) and any(.data[]; .body == "[REDACTED]")' \
    "$RESPONSE_FILE" >/dev/null
}

m7_assert_trace_result() {
  m7_assert_query_v2 skywalking_graphql traces
  jq -e '.data | type == "object"' "$RESPONSE_FILE" >/dev/null
}

run_m7_api_flow() {
  run_m10_base_api_flow

  local from to instant_body range_body trace_body
  from=$(date -u -v-10M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '-10 minutes' +%Y-%m-%dT%H:%M:%SZ)
  to=$(date -u -v+1M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '+1 minute' +%Y-%m-%dT%H:%M:%SZ)

  instant_body=$(jq -nc --arg query argus_m7_e2e_gauge --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" \
    '{query:$query,resource_ids:[$id],time_range:{from:$from,to:$to},budget:{max_scan_bytes:268435456,max_rows:100,max_samples:5000000,max_series:100000,timeout_ms:10000,max_result_bytes:8388608}}')
  request m10-promql-instant 200 POST /enterprise/metrics/query "$ENTERPRISE_JAR" "$instant_body" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  m7_assert_query_v2 promql vector
  jq -e '.data.resultType == "vector"' "$RESPONSE_FILE" >/dev/null

  range_body=$(jq -nc --arg query argus_m7_e2e_native_histogram --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" \
    '{query:$query,resource_ids:[$id],time_range:{from:$from,to:$to},step_seconds:60,budget:{max_scan_bytes:268435456,max_rows:100,max_samples:5000000,max_series:100000,timeout_ms:10000,max_result_bytes:8388608}}')
  request m10-promql-native-histogram 200 POST /enterprise/metrics/query_range "$ENTERPRISE_JAR" "$range_body" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  m7_assert_query_v2 promql matrix
  jq -e '.data.result | type == "array" and length > 0' "$RESPONSE_FILE" >/dev/null

  trace_body=$(jq -nc --arg id "$M3_CLUSTER_ID" --arg from "$from" --arg to "$to" \
    '{query:"query { queryBasicTraces(serviceName: \"argus-m7-e2e\", pageSize: 10) { total traces { traceId edges { parentSpanId childSpanId depth } } } }",resource_ids:[$id],time_range:{from:$from,to:$to},budget:{max_scan_bytes:268435456,max_rows:100,max_samples:5000000,timeout_ms:10000,max_result_bytes:8388608}}')
  request m10-trace-edges 200 POST /enterprise/traces/graphql "$ENTERPRISE_JAR" "$trace_body" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}"
  m7_assert_trace_result

  request m10-query-audit 200 GET /enterprise/audit-events "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e 'any(.items[]; .action == "telemetry.query.execute" and .result == "success" and
    (.details.expression_hash | type == "string" and length == 64) and
    (.details.plan_hash | type == "string" and length == 64) and
    (.details.language == "promql" or .details.language == "kql" or .details.language == "skywalking_graphql"))' "$RESPONSE_FILE" >/dev/null
  if jq -e 'any(.items[].details; has("query") or has("expression") or has("document"))' "$RESPONSE_FILE" >/dev/null; then
    fail "M10 Query Audit persisted raw query text"
  fi
}
