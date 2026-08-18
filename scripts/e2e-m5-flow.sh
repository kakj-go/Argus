#!/usr/bin/env bash

prepare_m5_dependencies() { :; }
cleanup_m5() { :; }

diagnostics_m5() {
  m4_psql "
    SELECT 'cards|' || id || '|' || source || '|' || slug || '|' || lifecycle || '|' || enabled || '|' || availability || '|' || latest_revision || '|' || version FROM interactive_cards ORDER BY source, slug;
    SELECT 'versions|' || card_id || '|' || revision || '|' || status || '|' || encode(content_hash,'hex') FROM card_versions ORDER BY card_id, revision;
    SELECT 'validation|' || id || '|' || status || '|' || array_to_string(passed_scenarios, ',') FROM card_validation_runs ORDER BY created_at;
    SELECT 'instances|' || id || '|' || card_id || '|' || card_version_id || '|' || status FROM card_instances ORDER BY created_at;
    SELECT 'presentations|' || id || '|' || card_instance_id || '|' || authorization_version || '|' || partial FROM card_presentations ORDER BY created_at;
    SELECT 'query_bindings|' || binding_ref || '|' || status || '|' || authorization_version FROM card_query_bindings ORDER BY created_at;
    SELECT 'action_bindings|' || binding_ref || '|' || status || '|' || binding_source FROM action_bindings WHERE binding_source='card' ORDER BY created_at;
  " 2>&1 | redact >"${ARTIFACT_DIR}/m5-card-state.txt" || true
}

m5_run_browser_validation() {
  local revision=$1
  log "running real Card Runtime validation for enterprise Card revision ${revision}"
  ARGUS_E2E_EXTERNAL=1 ARGUS_M5_E2E=1 \
    ARGUS_M5_ENTERPRISE_USERNAME="$ENTERPRISE_USERNAME" ARGUS_M5_ENTERPRISE_PASSWORD="$ENTERPRISE_PASSWORD" \
    ARGUS_M5_CARD_NAME="$M5_CARD_NAME" ARGUS_M5_REVISION="$revision" \
    ARGUS_E2E_ARTIFACTS="$ARTIFACT_DIR/playwright-m5-r${revision}" \
    pnpm --filter @argus/enterprise exec playwright test e2e/m5-real.spec.ts --workers=1
}

m5_wait_card_instance() {
  local run_id=$1 expected_source=$2 instance_id
  for _ in $(seq 1 90); do
    instance_id=$(m4_psql "SELECT instance.id FROM card_instances instance JOIN interactive_cards card ON card.id=instance.card_id WHERE instance.run_id='${run_id}' AND card.source='${expected_source}' ORDER BY instance.created_at DESC LIMIT 1;" 2>/dev/null || true)
    if [[ -n "$instance_id" ]]; then
      printf '%s' "$instance_id"
      return
    fi
    sleep 1
  done
  fail "M5 ${expected_source} CardInstance was not created for Run ${run_id}"
}

run_m5_api_flow() {
  log "running M5 Card generation, validation, selection, Binding, rollback, and recovery flow"

  request m5-reset-quota 200 POST /enterprise/model-quotas "$ENTERPRISE_JAR" \
    "$(jq -nc --arg model "$M4_MODEL_ID" --arg user "$ADMIN_USER_ID" '{model_id:$model,subject_type:"user",subject_id:$user,monthly_amount:1000}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-quota-${RUN_ID}"

  M5_CATALOG_BEFORE=$(m4_psql "SELECT string_agg(id::text || ':' || version::text || ':' || coalesce(active_version_id::text,''), ',' ORDER BY id) FROM interactive_cards WHERE source='system';")
  k -n "$SYSTEM_NS" exec deployment/argus-server -- /usr/local/bin/argus-card-catalog-sync >/dev/null
  M5_CATALOG_AFTER=$(m4_psql "SELECT string_agg(id::text || ':' || version::text || ':' || coalesce(active_version_id::text,''), ',' ORDER BY id) FROM interactive_cards WHERE source='system';")
  [[ "$M5_CATALOG_BEFORE" == "$M5_CATALOG_AFTER" ]] || fail "M5 System Card catalog sync changed an already synchronized catalog"

  request m5-system-cards 200 GET /enterprise/interactive-cards "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '([.items[] | select(.source == "system")] | length) == 5 and any(.items[]; .slug == "telemetry-overview" and .availability == "dependency_pending" and .enabled == false)' "$RESPONSE_FILE" >/dev/null
  M5_SYSTEM_CARD_ID=$(jq -er '.items[] | select(.source == "system" and .slug == "host-list") | .id' "$RESPONSE_FILE")
  M5_SYSTEM_CARD_VERSION=$(jq -er '.items[] | select(.id == $id) | .version' --arg id "$M5_SYSTEM_CARD_ID" "$RESPONSE_FILE")
  request m5-system-readonly 403 POST "/enterprise/interactive-cards/${M5_SYSTEM_CARD_ID}/disable" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M5_SYSTEM_CARD_VERSION" '{expected_version:$version}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-system-readonly-${RUN_ID}"
  jq -e '.code == "CARD_SOURCE_READ_ONLY"' "$RESPONSE_FILE" >/dev/null

  request m5-card-create-message 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
    '{"content":"Create a safe enterprise host inventory Card using the host.list schema.","command":{"type":"interactive_card.create"}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-card-create-${RUN_ID}"
  M5_CREATE_RUN_ID=$(jq -er '.run.run_id' "$RESPONSE_FILE")
  m4_wait_run "$M5_CREATE_RUN_ID"
  request m5-card-create-events 200 GET "/conversations/${M4_CONVERSATION_ID}/ledger" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e --arg run "$M5_CREATE_RUN_ID" 'any(.items[]; .run_id == $run and .event_type == "card_draft_created" and (.payload.card_id | length) > 0 and (.payload.content_hash | length) == 64) and (tostring | contains("entrypoint_html") | not)' "$RESPONSE_FILE" >/dev/null
  request m5-card-list 200 GET /enterprise/interactive-cards "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M5_CARD_ID=$(jq -er '.items[] | select(.source == "enterprise" and .slug == "m5-enterprise-host-list") | .id' "$RESPONSE_FILE")
  M5_CARD_NAME=$(jq -er --arg id "$M5_CARD_ID" '.items[] | select(.id == $id) | .name' "$RESPONSE_FILE")
  jq -e --arg id "$M5_CARD_ID" 'any(.items[]; .id == $id and .enabled == false and .lifecycle == "draft" and .latest_revision == 1)' "$RESPONSE_FILE" >/dev/null

  m5_run_browser_validation 1
  request m5-card-active-r1 200 GET "/enterprise/interactive-cards/${M5_CARD_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.enabled == true and .active_revision == 1 and .latest_revision == 1' "$RESPONSE_FILE" >/dev/null

  request m5-card-revise-message 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
    "$(jq -nc --arg card "$M5_CARD_ID" '{content:"Revise this Card as a detail presentation while preserving its safe host.list binding.",command:{type:"interactive_card.revise",card_id:$card,expected_revision:1}}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-card-revise-${RUN_ID}"
  M5_REVISE_RUN_ID=$(jq -er '.run.run_id' "$RESPONSE_FILE")
  m4_wait_run "$M5_REVISE_RUN_ID"
  request m5-card-old-active 200 GET "/enterprise/interactive-cards/${M5_CARD_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.enabled == true and .active_revision == 1 and .latest_revision == 2' "$RESPONSE_FILE" >/dev/null

  m5_run_browser_validation 2
  request m5-card-active-r2 200 GET "/enterprise/interactive-cards/${M5_CARD_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.enabled == true and .active_revision == 2 and .latest_revision == 2' "$RESPONSE_FILE" >/dev/null

  request m5-system-render-message 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
    '{"content":"Call host.list and present a table card."}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-system-render-${RUN_ID}"
  M5_SYSTEM_RENDER_RUN=$(jq -er '.run.run_id' "$RESPONSE_FILE")
  m4_wait_run "$M5_SYSTEM_RENDER_RUN"
  M5_SYSTEM_INSTANCE=$(m5_wait_card_instance "$M5_SYSTEM_RENDER_RUN" system)

  request m5-system-presentation 201 POST "/enterprise/card-instances/${M5_SYSTEM_INSTANCE}/presentations" "$ENTERPRISE_JAR" \
    '{"locale":"zh-CN","color_scheme":"light"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-presentation-${RUN_ID}"
  M5_QUERY_BINDING=$(jq -er '.render_plan.query_binding_ids.refresh' "$RESPONSE_FILE")
  jq -e '.manifest.source == "system" and .partial == false and (.initial_data.primary | type) == "array"' "$RESPONSE_FILE" >/dev/null
  request m5-query-invoke 200 POST "/enterprise/card-query-bindings/${M5_QUERY_BINDING}/invoke" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-query-${RUN_ID}"
  jq -e '.status == "succeeded" and (.data.items | type) == "array"' "$RESPONSE_FILE" >/dev/null
  request m5-query-replay 200 POST "/enterprise/card-query-bindings/${M5_QUERY_BINDING}/invoke" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-query-${RUN_ID}"

  request m5-auth-change-scope 201 POST /enterprise/data-scopes "$ENTERPRISE_JAR" \
    '{"name":"M5 authorization invalidation","resource_types":["host"],"explicit_resource_ids":[],"label_selector":{"schema_version":"argus.label_selector/v1","requirements":[{"key":"team","operator":"eq","values":["m5"]}]}}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-auth-scope-${RUN_ID}"
  M5_INVALIDATION_SCOPE=$(jq -er '.id' "$RESPONSE_FILE")
  request m5-auth-change-binding 201 POST /enterprise/role-bindings "$ENTERPRISE_JAR" \
    "$(jq -nc --arg user "$ADMIN_USER_ID" --arg role "$M4_RESOURCE_VIEWER_ROLE_ID" --arg scope "$M5_INVALIDATION_SCOPE" '{subject_type:"user",subject_id:$user,role_id:$role,data_scope_ids:[$scope]}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-auth-binding-${RUN_ID}"
  rm -f "$ENTERPRISE_JAR"
  enterprise_login
  request m5-old-query-invalidated 403 POST "/enterprise/card-query-bindings/${M5_QUERY_BINDING}/invoke" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-query-stale-${RUN_ID}"
  jq -e '.code == "CARD_PRESENTATION_INVALIDATED"' "$RESPONSE_FILE" >/dev/null
  request m5-rematerialized-presentation 201 POST "/enterprise/card-instances/${M5_SYSTEM_INSTANCE}/presentations" "$ENTERPRISE_JAR" \
    '{"locale":"en-US","color_scheme":"dark"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-rematerialize-${RUN_ID}"
  M5_REFRESHED_QUERY_BINDING=$(jq -er '.render_plan.query_binding_ids.refresh' "$RESPONSE_FILE")
  [[ "$M5_REFRESHED_QUERY_BINDING" != "$M5_QUERY_BINDING" ]] || fail "M5 rematerialization reused an invalidated Query Binding"

  request m5-enterprise-render-message 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
    '{"content":"Call host.list and present a detail card."}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-enterprise-render-${RUN_ID}"
  M5_ENTERPRISE_RENDER_RUN=$(jq -er '.run.run_id' "$RESPONSE_FILE")
  m4_wait_run "$M5_ENTERPRISE_RENDER_RUN"
  M5_ENTERPRISE_INSTANCE=$(m5_wait_card_instance "$M5_ENTERPRISE_RENDER_RUN" enterprise)
  [[ "$(m4_psql "SELECT card_id FROM card_instances WHERE id='${M5_ENTERPRISE_INSTANCE}';")" == "$M5_CARD_ID" ]] || fail "M5 detail intent did not select the more precise enterprise Card"

  M5_HOST_VERSION=$(m4_psql "SELECT resource_version FROM hosts WHERE id='${M4_HOST_ID}';")
  M5_PREVIEW_INPUT=$(jq -nc --arg host "$M4_HOST_ID" --argjson version "$M5_HOST_VERSION" '{host_id:$host,expected_version:$version,labels:{environment:"prod",team:"m4",release:"m5-card-action"}}' | base64 | tr '+/' '-_' | tr -d '=\n')
  request m5-action-message 202 POST "/conversations/${M4_CONVERSATION_ID}/messages" "$ENTERPRISE_JAR" \
    "$(jq -nc --arg input "$M5_PREVIEW_INPUT" '{content:("Call host.update.preview with tool_input_b64: "+$input)}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-action-message-${RUN_ID}"
  M5_ACTION_RUN=$(jq -er '.run.run_id' "$RESPONSE_FILE")
  m4_wait_run_status "$M5_ACTION_RUN" waiting_input
  M5_ACTION_REF=$(m4_psql "SELECT action_ref FROM pending_actions WHERE run_id='${M5_ACTION_RUN}';")
  M5_ACTION_INSTANCE=$(m5_wait_card_instance "$M5_ACTION_RUN" system)
  request m5-action-presentation 201 POST "/enterprise/card-instances/${M5_ACTION_INSTANCE}/presentations" "$ENTERPRISE_JAR" \
    '{"locale":"zh-CN","color_scheme":"dark"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-action-presentation-${RUN_ID}"
  M5_CONFIRM_BINDING=$(jq -er '.render_plan.action_binding_ids.confirm' "$RESPONSE_FILE")
  request m5-card-confirm 200 POST "/enterprise/card-action-bindings/${M5_CONFIRM_BINDING}/invoke" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-card-confirm-${RUN_ID}"
  jq -e '.pending_action.status == "awaiting_approval"' "$RESPONSE_FILE" >/dev/null
  M5_APPROVAL_ID=$(jq -er '.approval_request.approval_request_id' "$RESPONSE_FILE")
  request m5-card-confirm-replay 200 POST "/enterprise/card-action-bindings/${M5_CONFIRM_BINDING}/invoke" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-card-confirm-${RUN_ID}"
  request m5-card-confirm-consumed 409 POST "/enterprise/card-action-bindings/${M5_CONFIRM_BINDING}/invoke" "$ENTERPRISE_JAR" - \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-card-confirm-second-${RUN_ID}"
  jq -e '.code == "CARD_BINDING_CONSUMED" or .code == "ACTION_INVALIDATED"' "$RESPONSE_FILE" >/dev/null

  rm -f "$APPROVER_JAR"
  request m5-approver-login 200 POST /enterprise/auth/login "$APPROVER_JAR" \
    '{"username":"m4-approver","password":"Q8!mV4@rT7#pL2$x"}' --header "Origin: ${ENTERPRISE_ORIGIN}"
  M5_APPROVER_CSRF=$(jq -er '.authenticated_session.csrf_token' "$RESPONSE_FILE")
  request m5-approve 200 POST "/enterprise/approval-requests/${M5_APPROVAL_ID}/decisions" "$APPROVER_JAR" \
    '{"decision":"approved","reason":"independent M5 Card approval"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${M5_APPROVER_CSRF}" --header "Idempotency-Key: m5-approve-${RUN_ID}"
  m4_wait_execution "$M5_ACTION_REF"
  m4_wait_run "$M5_ACTION_RUN"
  [[ "$(m4_psql "SELECT labels->>'release' FROM hosts WHERE id='${M4_HOST_ID}';")" == "m5-card-action" ]] || fail "M5 Card Action did not complete through the M4 executor"

  request m5-card-before-rollback 200 GET "/enterprise/interactive-cards/${M5_CARD_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  M5_CARD_RESOURCE_VERSION=$(jq -er '.version' "$RESPONSE_FILE")
  request m5-card-rollback 200 POST "/enterprise/interactive-cards/${M5_CARD_ID}/rollback" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M5_CARD_RESOURCE_VERSION" '{expected_version:$version,revision:1}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-rollback-${RUN_ID}"
  jq -e '.active_revision == 1 and .latest_revision == 2 and .enabled == true' "$RESPONSE_FILE" >/dev/null
  M5_ROLLBACK_VERSION=$(jq -er '.version' "$RESPONSE_FILE")
  request m5-card-rollback-replay 200 POST "/enterprise/interactive-cards/${M5_CARD_ID}/rollback" "$ENTERPRISE_JAR" \
    "$(jq -nc --argjson version "$M5_CARD_RESOURCE_VERSION" '{expected_version:$version,revision:1}')" \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-rollback-${RUN_ID}"
  [[ "$(jq -er '.version' "$RESPONSE_FILE")" == "$M5_ROLLBACK_VERSION" ]] || fail "M5 rollback idempotency replay changed the Card version"

  k -n "$SYSTEM_NS" exec statefulset/argus-redis -- redis-cli -a "$REDIS_PASSWORD" FLUSHALL >/dev/null 2>&1
  k -n "$SYSTEM_NS" delete pod -l app.kubernetes.io/name=argus-server --wait=false >/dev/null
  k -n "$SYSTEM_NS" rollout status deployment/argus-server --timeout=300s >/dev/null
  kill "$API_PF_PID" >/dev/null 2>&1 || true
  wait "$API_PF_PID" >/dev/null 2>&1 || true
  start_api_port_forward
  for _ in $(seq 1 60); do curl --noproxy '*' --silent --fail --max-time 3 "http://127.0.0.1:${API_PORT}/readyz" >/dev/null 2>&1 && break; sleep 1; done
  request m5-recovered-card 200 GET "/enterprise/interactive-cards/${M5_CARD_ID}" "$ENTERPRISE_JAR" - --header "Origin: ${ENTERPRISE_ORIGIN}"
  jq -e '.active_revision == 1 and .latest_revision == 2' "$RESPONSE_FILE" >/dev/null
  request m5-recovered-presentation 201 POST "/enterprise/card-instances/${M5_SYSTEM_INSTANCE}/presentations" "$ENTERPRISE_JAR" \
    '{"locale":"zh-CN","color_scheme":"light"}' \
    --header "Origin: ${ENTERPRISE_ORIGIN}" --header "X-CSRF-Token: ${ENTERPRISE_CSRF}" --header "Idempotency-Key: m5-recovered-presentation-${RUN_ID}"

  [[ "$(m4_psql "SELECT count(*) FROM card_instances WHERE run_id='${M5_ACTION_RUN}';")" == "1" ]] || fail "M5 recovery duplicated a CardInstance"
  if k -n "$SYSTEM_NS" logs -l app.kubernetes.io/part-of=argus --all-containers=true --tail=5000 2>/dev/null | rg -i 'argus__token|commit_tool|private_params|remote_access_ticket'; then
    fail "M5 workload logs contain a private Card or action field"
  fi
  unset M5_APPROVER_CSRF M5_PREVIEW_INPUT
}
