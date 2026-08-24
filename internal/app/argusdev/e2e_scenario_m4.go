package argusdev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (a *App) runM4Scenario(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	roles, err := client.JSON(ctx, "m4-roles", "enterprise", http.MethodGet, "/enterprise/roles", http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	resourceAdmin, err := findItem(objectItems(roles), func(item map[string]any) bool { return item["name"] == "Resource Admin" && item["builtin"] == true })
	if err != nil {
		return err
	}
	roleID, _ := stringField(resourceAdmin, "id")
	env.State.Values["m4_resource_admin_role_id"] = roleID
	resourceViewer, err := findItem(objectItems(roles), func(item map[string]any) bool { return item["name"] == "Resource Viewer" && item["builtin"] == true })
	if err != nil {
		return err
	}
	viewerRoleID, _ := stringField(resourceViewer, "id")
	env.State.Values["m4_resource_viewer_role_id"] = viewerRoleID
	resourceApprover, err := findItem(objectItems(roles), func(item map[string]any) bool { return item["name"] == "Resource Approver" && item["builtin"] == true })
	if err != nil {
		return err
	}
	approverRoleID, _ := stringField(resourceApprover, "id")
	scope, err := client.JSON(ctx, "m4-admin-scope", "enterprise", http.MethodPost, "/enterprise/data-scopes", http.StatusCreated,
		map[string]any{"name": "M4 managed resources", "resource_types": []string{"host", "kubernetes_cluster", "kubernetes_namespace"}, "explicit_resource_ids": []string{}, "label_selector": map[string]any{"schema_version": "argus.label_selector/v1", "requirements": []any{map[string]any{"key": "team", "operator": "eq", "values": []string{"m4"}}}}}, enterpriseHeaders(env, "m4-admin-scope"))
	if err != nil {
		return err
	}
	scopeID, _ := stringField(scope, "id")
	if _, err := client.JSON(ctx, "m4-admin-binding", "enterprise", http.MethodPost, "/enterprise/role-bindings", http.StatusCreated,
		map[string]any{"subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "role_id": roleID, "data_scope_ids": []string{scopeID}}, enterpriseHeaders(env, "m4-admin-binding")); err != nil {
		return err
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}

	modelBase := "https://argus-replay-model." + env.SandboxNS + ".svc/v1"
	var modelID string
	for _, protocol := range []string{"chat_completions", "responses"} {
		created, err := client.JSON(ctx, "m4-model-"+protocol, "enterprise", http.MethodPost, "/enterprise/ai-models/test-and-create", http.StatusCreated,
			map[string]any{"name": "M4 Replay " + protocol, "base_url": modelBase, "model_id": "argus-replay-" + protocol, "api_protocol": protocol, "api_key": "m4-write-only-key", "context_window_tokens": 8192, "max_output_tokens": 512, "input_price_per_million": 0.1, "output_price_per_million": 0.2}, enterpriseHeaders(env, "m4-model-"+protocol))
		if err != nil {
			return err
		}
		if compatible, _ := created["compatible"].(bool); !compatible {
			return fmt.Errorf("M4 replay model %s is incompatible", protocol)
		}
		if protocol == "chat_completions" {
			modelID, err = stringField(created, "model", "id")
			if err != nil {
				return err
			}
		}
	}
	if _, err := client.JSON(ctx, "m4-model-quota", "enterprise", http.MethodPost, "/enterprise/model-quotas", http.StatusOK,
		map[string]any{"model_id": modelID, "subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "monthly_amount": 100}, enterpriseHeaders(env, "m4-quota")); err != nil {
		return err
	}
	conversation, err := client.JSON(ctx, "m4-conversation", "enterprise", http.MethodPost, "/conversations", http.StatusCreated,
		map[string]any{"title": "M4 recovery flow", "selected_model_id": modelID}, enterpriseHeaders(env, "m4-conversation"))
	if err != nil {
		return err
	}
	conversationID, _ := stringField(conversation, "id")
	env.State.Values["m4_model_id"] = modelID
	env.State.Values["m4_conversation_id"] = conversationID
	message, err := client.JSON(ctx, "m4-message", "enterprise", http.MethodPost, "/conversations/"+conversationID+"/messages", http.StatusAccepted,
		map[string]any{"content": "Call host.list and summarize the visible hosts."}, enterpriseHeaders(env, "m4-message"))
	if err != nil {
		return err
	}
	runID, _ := stringField(message, "run", "run_id")
	if err := a.waitRunTerminal(ctx, env, runID); err != nil {
		return err
	}
	if err := a.verifyM4Compaction(ctx, env, conversationID, runID); err != nil {
		return err
	}
	if err := a.verifyM4OneTimeResult(ctx, env); err != nil {
		return err
	}

	hostID, err := a.createM4Host(ctx, env)
	if err != nil {
		return err
	}
	env.State.Values["m4_host_id"] = hostID
	host, err := client.JSON(ctx, "m4-host", "enterprise", http.MethodGet, "/enterprise/hosts/"+hostID, http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	version, err := numberField(host, "resource_version")
	if err != nil {
		return err
	}
	if err := a.verifyM4Approval(ctx, env, hostID, version, approverRoleID); err != nil {
		return err
	}

	serviceAccount, err := client.JSON(ctx, "m4-automation-account", "enterprise", http.MethodPost, "/enterprise/service-accounts", http.StatusCreated,
		map[string]any{"name": "m4-automation", "allowed_tool_ids": []string{"host.list", "host.update.preview"}, "data_scope_ids": []string{scopeID}}, enterpriseHeaders(env, "m4-automation-account"))
	if err != nil {
		return err
	}
	accountID, _ := stringField(serviceAccount, "id")
	if _, err := client.JSON(ctx, "m4-automation-binding", "enterprise", http.MethodPost, "/enterprise/role-bindings", http.StatusCreated,
		map[string]any{"subject_type": "service_account", "subject_id": accountID, "role_id": roleID, "data_scope_ids": []string{scopeID}}, enterpriseHeaders(env, "m4-automation-binding")); err != nil {
		return err
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	// Role binding changes invalidate every enterprise user's authorization
	// version, including the independent approver used later in this scenario.
	if err := a.refreshM4ApproverLogin(ctx, env); err != nil {
		return err
	}
	automation, err := client.JSON(ctx, "m4-automation", "enterprise", http.MethodPost, "/enterprise/automations", http.StatusCreated,
		map[string]any{"name": "M4 host inventory", "service_account_id": accountID, "tool_id": "host.list", "tool_input": map[string]any{}, "cron": "* * * * *", "timezone": "UTC"}, enterpriseHeaders(env, "m4-automation"))
	if err != nil {
		return err
	}
	automationID, _ := stringField(automation, "id")
	if err := a.verifyM4ReadAutomation(ctx, env, automationID); err != nil {
		return err
	}
	if err := a.verifyM4AutomationRevisionAndResultUnknown(ctx, env, accountID, hostID); err != nil {
		return err
	}
	if err := a.createM4Sandbox(ctx, env); err != nil {
		return err
	}
	if err := a.verifyM4ModelQuota(ctx, env); err != nil {
		return err
	}
	return a.runPlaywright(ctx, env, "e2e/m4-real.spec.ts", map[string]string{
		"ARGUS_M4_E2E": "1", "ARGUS_M4_ENTERPRISE_USERNAME": env.State.Values["enterprise_username"], "ARGUS_M4_ENTERPRISE_PASSWORD": env.State.Values["enterprise_password"],
		"ARGUS_M4_PLATFORM_USERNAME": env.State.Values["platform_username"], "ARGUS_M4_PLATFORM_PASSWORD": env.State.Values["platform_password"],
	})
}

func (a *App) verifyM4Approval(ctx context.Context, env *E2EEnvironment, hostID string, hostVersion int64, approverRoleID string) error {
	client, _ := scenarioHTTP(env)
	defaultDepartmentID, err := a.postgresQuery(ctx, env, "SELECT id FROM departments WHERE enterprise_id='"+env.State.Values["enterprise_id"]+"' AND is_default=true;")
	if err != nil {
		return err
	}
	defaultDepartmentID = strings.TrimSpace(defaultDepartmentID)
	if defaultDepartmentID == "" {
		return fmt.Errorf("M4 default Department is unavailable")
	}
	created, err := client.JSON(ctx, "m4-approver-create", "enterprise", http.MethodPost, "/enterprise/users", http.StatusCreated,
		map[string]any{"username": "m4-approver", "display_name": "M4 Approver", "department_id": defaultDepartmentID, "role_ids": []string{approverRoleID}}, enterpriseHeaders(env, "m4-approver"))
	if err != nil {
		return err
	}
	temporaryPassword, err := stringField(created, "temporary_password")
	if err != nil {
		return err
	}
	login, err := client.JSON(ctx, "m4-approver-login", "m4-approver", http.MethodPost, "/enterprise/auth/login", http.StatusOK,
		map[string]any{"username": "m4-approver", "password": temporaryPassword}, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	challengeID, err := stringField(login, "password_change_challenge", "challenge_id")
	if err != nil {
		return err
	}
	newPassword := "Q8!mV4@rT7#pL2$x"
	completed, err := client.JSON(ctx, "m4-approver-password", "m4-approver", http.MethodPost, "/enterprise/auth/complete-password-change", http.StatusOK,
		map[string]any{"challenge_id": challengeID, "temporary_password": temporaryPassword, "new_password": newPassword}, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	approverCSRF, err := stringField(completed, "csrf_token")
	if err != nil {
		return err
	}
	env.State.Values["m4_approver_password"] = newPassword
	env.State.Values["m4_approver_csrf"] = approverCSRF
	if _, err := client.JSON(ctx, "m4-policy", "enterprise", http.MethodPost, "/enterprise/approval-policies", http.StatusCreated,
		map[string]any{"name": "M4 host changes", "enabled": true, "tool_ids": []string{"argus.host.update.commit"}, "risks": []string{"write"}, "resource_types": []string{"host"}, "minimum_approvers": 1, "separation_of_duty": true, "approver_role_ids": []string{approverRoleID}, "expires_after_seconds": 3600, "expected_version": 0}, enterpriseHeaders(env, "m4-policy")); err != nil {
		return err
	}
	preview, err := client.JSON(ctx, "m4-host-update-preview", "enterprise", http.MethodPost, "/enterprise/hosts/"+hostID+"/actions/preview-update", http.StatusCreated,
		map[string]any{"labels": map[string]string{"environment": "prod", "team": "m4", "release": "approved"}, "expected_version": hostVersion}, enterpriseHeaders(env, "m4-host-update"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	confirmed, err := client.JSON(ctx, "m4-host-update-confirm", "enterprise", http.MethodPost, "/enterprise/pending-actions/"+actionRef+"/confirm", http.StatusOK, nil, enterpriseHeaders(env, "m4-host-update-confirm"))
	if err != nil {
		return err
	}
	status, _ := nestedString(confirmed, "pending_action", "status")
	approvalID, err := stringField(confirmed, "approval_request", "approval_request_id")
	if status != "awaiting_approval" || err != nil {
		return fmt.Errorf("M4 host update did not enter approval")
	}
	approved, err := client.JSON(ctx, "m4-approve", "m4-approver", http.MethodPost, "/enterprise/approval-requests/"+approvalID+"/decisions", http.StatusOK,
		map[string]any{"decision": "approved", "reason": "independent M4 approval"}, map[string]string{"Origin": enterpriseOrigin, "X-CSRF-Token": approverCSRF, "Idempotency-Key": "m4-approve-" + env.Options.RunID})
	if err != nil {
		return err
	}
	if approved["status"] != "approved" {
		return fmt.Errorf("M4 approval request ended as %v", approved["status"])
	}
	if _, err := a.waitExecutionForAction(ctx, env, actionRef, 2*time.Minute); err != nil {
		return err
	}
	label, err := a.postgresQuery(ctx, env, "SELECT labels->>'release' FROM hosts WHERE id='"+hostID+"' AND enterprise_id='"+env.State.Values["enterprise_id"]+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(label) != "approved" {
		return fmt.Errorf("M4 approved host update did not persist")
	}
	return a.refreshM4ApproverLogin(ctx, env)
}

func (a *App) refreshM4ApproverLogin(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	client.Reset("m4-approver")
	login, err := client.JSON(ctx, "m4-approver-login-refresh", "m4-approver", http.MethodPost, "/enterprise/auth/login", http.StatusOK,
		map[string]any{"username": "m4-approver", "password": env.State.Values["m4_approver_password"]}, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	csrf, err := stringField(login, "authenticated_session", "csrf_token")
	if err != nil {
		return err
	}
	env.State.Values["m4_approver_csrf"] = csrf
	return nil
}

func (a *App) verifyM4Compaction(ctx context.Context, env *E2EEnvironment, conversationID, runID string) error {
	client, _ := scenarioHTTP(env)
	ledger, err := client.JSON(ctx, "m4-ledger", "enterprise", http.MethodGet, "/conversations/"+conversationID+"/ledger", http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	foundToolResult := false
	foundAssistant := false
	for _, event := range objectItems(ledger) {
		switch event["event_type"] {
		case "tool_call_result":
			foundToolResult = true
		case "assistant_message":
			foundAssistant = true
		}
	}
	if !foundToolResult || !foundAssistant {
		return fmt.Errorf("M4 conversation ledger is missing tool or assistant evidence")
	}
	if _, err := client.JSON(ctx, "m4-compact", "enterprise", http.MethodPost, "/runs/"+runID+"/compact", http.StatusAccepted, nil, enterpriseHeaders(env, "m4-compact")); err != nil {
		return err
	}
	if err := a.waitPostgresValue(ctx, env, "SELECT count(*) FROM context_snapshots WHERE run_id='"+runID+"' AND status='active';", "1", 90*time.Second); err != nil {
		return fmt.Errorf("M4 manual compaction snapshot: %w", err)
	}
	if err := a.waitPostgresValue(ctx, env, "SELECT count(*) FROM model_calls WHERE run_id='"+runID+"' AND call_kind='compaction' AND status='succeeded';", "1", 90*time.Second); err != nil {
		return fmt.Errorf("M4 compaction ModelCall: %w", err)
	}
	status, err := a.postgresQuery(ctx, env, "SELECT status FROM runs WHERE id='"+runID+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "succeeded" {
		return fmt.Errorf("M4 manual compaction changed terminal Run status to %q", strings.TrimSpace(status))
	}
	return nil
}

func (a *App) verifyM4OneTimeResult(ctx context.Context, env *E2EEnvironment) error {
	client, _ := scenarioHTTP(env)
	preview, err := client.JSON(ctx, "m4-bastion-preview", "enterprise", http.MethodPost, "/enterprise/bastion-scopes/actions/preview-create", http.StatusCreated,
		map[string]any{"name": "m4-one-time-result", "environment": "production", "labels": map[string]string{"team": "m4", "route": "enrollment"}}, enterpriseHeaders(env, "m4-bastion-preview"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	confirmed, err := client.JSON(ctx, "m4-bastion-confirm", "enterprise", http.MethodPost, "/enterprise/pending-actions/"+actionRef+"/confirm", http.StatusOK, nil, enterpriseHeaders(env, "m4-bastion-confirm"))
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(confirmed)
	if bytesContainFold(encoded, []byte("install_command")) {
		return fmt.Errorf("M4 one-time Connector command leaked through confirmation response")
	}
	executionID, err := a.waitExecutionForAction(ctx, env, actionRef, 2*time.Minute)
	if err != nil {
		return err
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	execution, err := client.JSON(ctx, "m4-enrollment-execution", "enterprise", http.MethodGet, "/enterprise/executions/"+executionID, http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	encoded, _ = json.Marshal(execution)
	if execution["status"] != "succeeded" || execution["one_time_result_available"] != true || bytesContainFold(encoded, []byte("install_command")) {
		return fmt.Errorf("M4 execution exposed or lost one-time result metadata")
	}
	claimHeaders := enterpriseHeaders(env, "m4-enrollment-claim")
	claimed, err := client.JSON(ctx, "m4-enrollment-claim", "enterprise", http.MethodPost, "/enterprise/executions/"+executionID+"/one-time-result", http.StatusOK, nil, claimHeaders)
	if err != nil {
		return err
	}
	command, err := stringField(claimed, "enrollment", "install_command")
	if err != nil || claimed["schema_version"] != "argus.action_one_time_result/v1" || claimed["result_kind"] != "connector_enrollment" {
		return fmt.Errorf("M4 one-time result did not match the Connector enrollment contract")
	}
	replayed, err := client.JSON(ctx, "m4-enrollment-claim-retry", "enterprise", http.MethodPost, "/enterprise/executions/"+executionID+"/one-time-result", http.StatusOK, nil, claimHeaders)
	if err != nil {
		return err
	}
	replayedCommand, _ := stringField(replayed, "enrollment", "install_command")
	if replayedCommand != command {
		return fmt.Errorf("M4 one-time result idempotency replay changed the command")
	}
	consumed, err := client.JSON(ctx, "m4-enrollment-claim-consumed", "enterprise", http.MethodPost, "/enterprise/executions/"+executionID+"/one-time-result", http.StatusConflict, nil, enterpriseHeaders(env, "m4-enrollment-second-claim"))
	if err != nil {
		return err
	}
	if consumed["code"] != "ACTION_RESULT_ALREADY_CONSUMED" {
		return fmt.Errorf("M4 second one-time result claim returned %v", consumed["code"])
	}
	leaks, err := a.postgresQuery(ctx, env, "SELECT count(*) FROM execution_one_time_results WHERE encode(ciphertext,'escape') LIKE '%--token%';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(leaks) != "0" {
		return fmt.Errorf("M4 one-time install command leaked into PostgreSQL ciphertext")
	}
	return nil
}

func (a *App) waitRunTerminal(ctx context.Context, env *E2EEnvironment, runID string) error {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		run, err := client.JSON(ctx, "run-"+runID, "enterprise", http.MethodGet, "/runs/"+runID, http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
		if err != nil {
			return err
		}
		status, _ := run["status"].(string)
		if status == "succeeded" || status == "cancelled" {
			return nil
		}
		if status == "failed" {
			return fmt.Errorf("run %s failed", runID)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("run %s timed out", runID)
}

func (a *App) createM4Host(ctx context.Context, env *E2EEnvironment) (string, error) {
	id := uuid.NewString()
	labels := `{"environment":"prod","team":"m4"}`
	hash := sha256.Sum256([]byte(labels))
	query := fmt.Sprintf("INSERT INTO hosts (id,enterprise_id,name,hostname,address,port,platform,connection_mode,environment,labels,labels_hash,connection_status) VALUES ('%s','%s','m4-managed-host','m4-managed-host','203.0.113.10',22,'linux','direct_ssh','production','%s'::jsonb,decode('%s','hex'),'offline');", id, env.State.Values["enterprise_id"], labels, hex.EncodeToString(hash[:]))
	if _, err := a.postgresQuery(ctx, env, query); err != nil {
		return "", err
	}
	return id, nil
}

func (a *App) createM4Sandbox(ctx context.Context, env *E2EEnvironment) error {
	client, _ := scenarioHTTP(env)
	headers := map[string]string{"Origin": platformOrigin, "X-CSRF-Token": env.State.Values["platform_csrf"], "Idempotency-Key": "m4-sandbox-" + env.Options.RunID}
	backend, err := client.JSON(ctx, "m4-sandbox-backend", "platform", http.MethodPost, "/platform/sandbox/backends", http.StatusCreated,
		map[string]any{"name": "M4 OpenSandbox", "endpoint": "http://argus-replay-model." + env.SandboxNS + ".svc:8081/sandbox", "api_key": "write-only", "status": "enabled", "expected_version": 0}, headers)
	if err != nil {
		return err
	}
	if _, exposed := backend["api_key"]; exposed {
		return fmt.Errorf("M4 Sandbox backend exposed its API key")
	}
	backendID, _ := stringField(backend, "id")
	headers["Idempotency-Key"] = "m4-sandbox-test-" + env.Options.RunID
	test, err := client.JSON(ctx, "m4-sandbox-test", "platform", http.MethodPost, "/platform/sandbox/backends/"+backendID+"/test", http.StatusOK, nil, headers)
	if err != nil {
		return err
	}
	if test["health_status"] != "healthy" {
		return fmt.Errorf("M4 Sandbox backend health is %v", test["health_status"])
	}
	headers["Idempotency-Key"] = "m4-sandbox-image-" + env.Options.RunID
	image, err := client.JSON(ctx, "m4-sandbox-image", "platform", http.MethodPost, "/platform/sandbox/images", http.StatusCreated,
		map[string]any{"backend_id": backendID, "name": "M4 smoke image", "image_ref": "registry.example.test/argus/smoke", "digest": "sha256:" + strings.Repeat("0", 64), "status": "enabled", "expected_version": 0}, headers)
	if err != nil {
		return err
	}
	imageID, _ := stringField(image, "id")
	headers["Idempotency-Key"] = "m4-sandbox-profile-" + env.Options.RunID
	_, err = client.JSON(ctx, "m4-sandbox-profile", "platform", http.MethodPost, "/platform/sandbox/profiles", http.StatusCreated,
		map[string]any{"name": "M4 smoke", "backend_id": backendID, "image_id": imageID, "task_kinds": []string{"smoke"}, "cpu_millis": 100, "memory_mib": 128, "timeout_seconds": 60, "network_mode": "none", "status": "enabled", "expected_version": 0}, headers)
	if err != nil {
		return err
	}
	quotaHeaders := map[string]string{"Origin": platformOrigin, "X-CSRF-Token": env.State.Values["platform_csrf"]}
	if _, err := client.JSON(ctx, "m4-sandbox-quota", "platform", http.MethodPut, "/platform/sandbox/enterprise-quotas/"+env.State.Values["enterprise_id"], http.StatusOK,
		map[string]any{"max_concurrent_sessions": 1, "monthly_session_seconds": 600, "expected_version": 0}, quotaHeaders); err != nil {
		return err
	}
	taskID := uuid.NewString()
	if _, err := a.postgresQuery(ctx, env, "INSERT INTO runtime_tasks (id,enterprise_id,queue,payload) VALUES ('"+taskID+"','"+env.State.Values["enterprise_id"]+"','sandbox',jsonb_build_object('enterprise_id','"+env.State.Values["enterprise_id"]+"','task_id','"+taskID+"','task_kind','smoke'));"); err != nil {
		return err
	}
	var sessionID string
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		sessions, err := client.JSONArray(ctx, "m4-sandbox-sessions", "platform", http.MethodGet, "/platform/sandbox/sessions", http.StatusOK, nil, map[string]string{"Origin": platformOrigin})
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if session["task_id"] == taskID && session["status"] == "running" {
				sessionID, _ = stringField(session, "id")
				break
			}
		}
		if sessionID != "" {
			break
		}
		time.Sleep(time.Second)
	}
	if sessionID == "" {
		return fmt.Errorf("M4 Sandbox session did not start")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(1100 * time.Millisecond):
	}
	headers["Idempotency-Key"] = "m4-sandbox-terminate-" + env.Options.RunID
	terminated, err := client.JSON(ctx, "m4-sandbox-terminate", "platform", http.MethodPost, "/platform/sandbox/sessions/"+sessionID+"/terminate", http.StatusOK, nil, headers)
	if err != nil {
		return err
	}
	if terminated["status"] != "terminated" {
		return fmt.Errorf("M4 Sandbox session ended as %v", terminated["status"])
	}
	usage, err := client.JSONArray(ctx, "m4-sandbox-usage", "platform", http.MethodGet, "/platform/sandbox/usage", http.StatusOK, nil, map[string]string{"Origin": platformOrigin})
	if err != nil {
		return err
	}
	var usedSeconds int64
	for _, item := range usage {
		if item["enterprise_id"] == env.State.Values["enterprise_id"] {
			usedSeconds, _ = numberField(item, "session_seconds")
			count, _ := numberField(item, "session_count")
			if count != 1 {
				return fmt.Errorf("M4 Sandbox usage count is %d", count)
			}
		}
	}
	if usedSeconds < 1 {
		return fmt.Errorf("M4 Sandbox usage did not record session time")
	}
	if _, err := client.JSON(ctx, "m4-sandbox-quota-exhaust", "platform", http.MethodPut, "/platform/sandbox/enterprise-quotas/"+env.State.Values["enterprise_id"], http.StatusOK,
		map[string]any{"max_concurrent_sessions": 1, "monthly_session_seconds": usedSeconds, "expected_version": 1}, quotaHeaders); err != nil {
		return err
	}
	rejectedTaskID := uuid.NewString()
	if _, err := a.postgresQuery(ctx, env, "INSERT INTO runtime_tasks (id,enterprise_id,queue,payload) VALUES ('"+rejectedTaskID+"','"+env.State.Values["enterprise_id"]+"','sandbox',jsonb_build_object('enterprise_id','"+env.State.Values["enterprise_id"]+"','task_id','"+rejectedTaskID+"','task_kind','smoke'));"); err != nil {
		return err
	}
	if err := a.waitPostgresValue(ctx, env, "SELECT status || ':' || COALESCE(last_error_code,'') FROM runtime_tasks WHERE id='"+rejectedTaskID+"';", "failed:SANDBOX_QUOTA_EXCEEDED", 30*time.Second); err != nil {
		return fmt.Errorf("M4 Sandbox monthly quota: %w", err)
	}
	count, err := a.postgresQuery(ctx, env, "SELECT count(*) FROM sandbox_sessions WHERE task_id='"+rejectedTaskID+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(count) != "0" {
		return fmt.Errorf("M4 quota-rejected Sandbox task persisted a session")
	}
	return nil
}

func (a *App) verifyM4ModelQuota(ctx context.Context, env *E2EEnvironment) error {
	client, _ := scenarioHTTP(env)
	if _, err := client.JSON(ctx, "m4-user-quota-exhaust", "enterprise", http.MethodPost, "/enterprise/model-quotas", http.StatusOK,
		map[string]any{"model_id": env.State.Values["m4_model_id"], "subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "monthly_amount": 0}, enterpriseHeaders(env, "m4-quota-exhaust")); err != nil {
		return err
	}
	message, err := client.JSON(ctx, "m4-quota-message", "enterprise", http.MethodPost, "/conversations/"+env.State.Values["m4_conversation_id"]+"/messages", http.StatusAccepted,
		map[string]any{"content": "Summarize the completed governed changes without calling a tool."}, enterpriseHeaders(env, "m4-quota-message"))
	if err != nil {
		return err
	}
	runID, err := stringField(message, "run", "run_id")
	if err != nil {
		return err
	}
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		run, err := client.JSON(ctx, "m4-quota-run", "enterprise", http.MethodGet, "/runs/"+runID, http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
		if err != nil {
			return err
		}
		if run["status"] == "failed" && run["error_code"] == "MODEL_QUOTA_EXCEEDED" {
			return nil
		}
		if run["status"] == "succeeded" || run["status"] == "cancelled" {
			return fmt.Errorf("M4 quota Run ended as %v", run["status"])
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("M4 model quota exhaustion did not fail the Run deterministically")
}
