package argusdev

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (a *App) runM5Scenario(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	if _, err := client.JSON(ctx, "m5-reset-quota", "enterprise", http.MethodPost, "/enterprise/model-quotas", http.StatusOK,
		map[string]any{"model_id": env.State.Values["m4_model_id"], "subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "monthly_amount": 1000}, enterpriseHeaders(env, "m5-quota")); err != nil {
		return err
	}
	systemCardID, err := a.verifyM5SystemCatalog(ctx, env)
	if err != nil {
		return err
	}
	conversationID := env.State.Values["m4_conversation_id"]
	created, err := client.JSON(ctx, "m5-card-create-message", "enterprise", http.MethodPost, "/conversations/"+conversationID+"/messages", http.StatusAccepted,
		map[string]any{"content": "Create a safe enterprise host inventory Card using the host.list schema.", "command": map[string]any{"type": "interactive_card.create"}}, enterpriseHeaders(env, "m5-card-create"))
	if err != nil {
		return err
	}
	runID, _ := stringField(created, "run", "run_id")
	if err := a.waitRunTerminal(ctx, env, runID); err != nil {
		return err
	}
	cards, err := client.JSON(ctx, "m5-card-list", "enterprise", http.MethodGet, "/enterprise/interactive-cards", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	card, err := findItem(objectItems(cards), func(item map[string]any) bool {
		return item["source"] == "enterprise" && item["slug"] == "m5-enterprise-host-list"
	})
	if err != nil {
		return err
	}
	cardID, _ := stringField(card, "id")
	cardName, _ := stringField(card, "name")
	if err := a.runPlaywright(ctx, env, "e2e/m5-real.spec.ts", map[string]string{
		"ARGUS_M5_E2E": "1", "ARGUS_M5_ENTERPRISE_USERNAME": env.State.Values["enterprise_username"], "ARGUS_M5_ENTERPRISE_PASSWORD": env.State.Values["enterprise_password"],
		"ARGUS_M5_CARD_NAME": cardName, "ARGUS_M5_REVISION": "1",
	}); err != nil {
		return err
	}
	activeR1, err := client.JSON(ctx, "m5-card-active-r1", "enterprise", http.MethodGet, "/enterprise/interactive-cards/"+cardID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	if activeR1["enabled"] != true || activeR1["active_revision"] != float64(1) || activeR1["latest_revision"] != float64(1) {
		return fmt.Errorf("M5 revision 1 was not active after browser validation")
	}
	revised, err := client.JSON(ctx, "m5-card-revise-message", "enterprise", http.MethodPost, "/conversations/"+conversationID+"/messages", http.StatusAccepted,
		map[string]any{"content": "Revise this Card as a detail presentation while preserving its safe host.list binding.", "command": map[string]any{"type": "interactive_card.revise", "card_id": cardID, "expected_revision": 1}}, enterpriseHeaders(env, "m5-card-revise"))
	if err != nil {
		return err
	}
	reviseRunID, _ := stringField(revised, "run", "run_id")
	if err := a.waitRunTerminal(ctx, env, reviseRunID); err != nil {
		return err
	}
	if err := a.runPlaywright(ctx, env, "e2e/m5-real.spec.ts", map[string]string{
		"ARGUS_M5_E2E": "1", "ARGUS_M5_ENTERPRISE_USERNAME": env.State.Values["enterprise_username"], "ARGUS_M5_ENTERPRISE_PASSWORD": env.State.Values["enterprise_password"],
		"ARGUS_M5_CARD_NAME": cardName, "ARGUS_M5_REVISION": "2",
	}); err != nil {
		return err
	}
	activeR2, err := client.JSON(ctx, "m5-card-active-r2", "enterprise", http.MethodGet, "/enterprise/interactive-cards/"+cardID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	if activeR2["enabled"] != true || activeR2["active_revision"] != float64(2) || activeR2["latest_revision"] != float64(2) {
		return fmt.Errorf("M5 revision 2 was not active after browser validation")
	}
	if err := a.verifyM5QueryBindingInvalidation(ctx, env, systemCardID); err != nil {
		return err
	}
	if err := a.verifyM5ActionBinding(ctx, env); err != nil {
		return err
	}
	current, err := client.JSON(ctx, "m5-card-current", "enterprise", http.MethodGet, "/enterprise/interactive-cards/"+cardID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	version, err := numberField(current, "version")
	if err != nil {
		return err
	}
	rolledBack, err := client.JSON(ctx, "m5-card-rollback", "enterprise", http.MethodPost, "/enterprise/interactive-cards/"+cardID+"/rollback", http.StatusOK,
		map[string]any{"expected_version": version, "revision": 1}, enterpriseHeaders(env, "m5-card-rollback"))
	if err != nil {
		return err
	}
	active, err := numberField(rolledBack, "active_revision")
	if err != nil || active != 1 {
		return fmt.Errorf("M5 Card rollback did not activate revision 1")
	}
	replayed, err := client.JSON(ctx, "m5-card-rollback-replay", "enterprise", http.MethodPost, "/enterprise/interactive-cards/"+cardID+"/rollback", http.StatusOK,
		map[string]any{"expected_version": version, "revision": 1}, enterpriseHeaders(env, "m5-card-rollback"))
	if err != nil {
		return err
	}
	rolledBackVersion, _ := numberField(rolledBack, "version")
	replayedVersion, _ := numberField(replayed, "version")
	if replayedVersion != rolledBackVersion {
		return fmt.Errorf("M5 rollback idempotency replay changed the Card version")
	}
	if err := a.verifyM5Recovery(ctx, env, cardID); err != nil {
		return err
	}
	env.State.Values["m5_card_id"] = cardID
	return nil
}

func (a *App) verifyM5ActionBinding(ctx context.Context, env *E2EEnvironment) error {
	client, _ := scenarioHTTP(env)
	hostID := env.State.Values["m4_host_id"]
	versionText, err := a.postgresQuery(ctx, env, "SELECT resource_version FROM hosts WHERE id='"+hostID+"';")
	if err != nil {
		return err
	}
	var version int64
	if _, err := fmt.Sscan(strings.TrimSpace(versionText), &version); err != nil {
		return err
	}
	toolInput, err := json.Marshal(map[string]any{"host_id": hostID, "expected_version": version, "labels": map[string]string{"environment": "prod", "team": "m4", "release": "m5-card-action"}})
	if err != nil {
		return err
	}
	message, err := client.JSON(ctx, "m5-action-message", "enterprise", http.MethodPost, "/conversations/"+env.State.Values["m4_conversation_id"]+"/messages", http.StatusAccepted,
		map[string]any{"content": "Call host.update.preview with tool_input_b64: " + base64.RawURLEncoding.EncodeToString(toolInput)}, enterpriseHeaders(env, "m5-action-message"))
	if err != nil {
		return err
	}
	runID, err := stringField(message, "run", "run_id")
	if err != nil {
		return err
	}
	env.State.Values["m5_action_run_id"] = runID
	if err := a.waitRunStatus(ctx, env, runID, "waiting_input", 2*time.Minute); err != nil {
		return err
	}
	actionRef, err := a.postgresQuery(ctx, env, "SELECT action_ref FROM pending_actions WHERE run_id='"+runID+"';")
	if err != nil {
		return err
	}
	actionRef = strings.TrimSpace(actionRef)
	instanceID, err := a.waitM5CardInstanceBySource(ctx, env, runID, "system")
	if err != nil {
		return err
	}
	presentation, err := client.JSON(ctx, "m5-action-presentation", "enterprise", http.MethodPost, "/enterprise/card-instances/"+instanceID+"/presentations", http.StatusCreated,
		map[string]any{"locale": "zh-CN", "color_scheme": "dark"}, enterpriseHeaders(env, "m5-action-presentation"))
	if err != nil {
		return err
	}
	bindingID, err := stringField(presentation, "render_plan", "action_binding_ids", "confirm")
	if err != nil {
		return err
	}
	invokeHeaders := enterpriseHeaders(env, "m5-card-confirm")
	confirmed, err := client.JSON(ctx, "m5-card-confirm", "enterprise", http.MethodPost, "/enterprise/card-action-bindings/"+bindingID+"/invoke", http.StatusOK, nil, invokeHeaders)
	if err != nil {
		return err
	}
	status, _ := nestedString(confirmed, "pending_action", "status")
	approvalID, err := stringField(confirmed, "approval_request", "approval_request_id")
	if status != "awaiting_approval" || err != nil {
		return fmt.Errorf("M5 Card Action did not enter approval")
	}
	if _, err := client.JSON(ctx, "m5-card-confirm-replay", "enterprise", http.MethodPost, "/enterprise/card-action-bindings/"+bindingID+"/invoke", http.StatusOK, nil, invokeHeaders); err != nil {
		return err
	}
	consumed, err := client.JSON(ctx, "m5-card-confirm-consumed", "enterprise", http.MethodPost, "/enterprise/card-action-bindings/"+bindingID+"/invoke", http.StatusConflict, nil, enterpriseHeaders(env, "m5-card-confirm-second"))
	if err != nil {
		return err
	}
	if consumed["code"] != "CARD_BINDING_CONSUMED" && consumed["code"] != "ACTION_INVALIDATED" {
		return fmt.Errorf("M5 consumed Card Action Binding returned %v", consumed["code"])
	}
	approved, err := client.JSON(ctx, "m5-approve", "m4-approver", http.MethodPost, "/enterprise/approval-requests/"+approvalID+"/decisions", http.StatusOK,
		map[string]any{"decision": "approved", "reason": "independent M5 Card approval"}, map[string]string{"Origin": env.EnterpriseOrigin(), "X-CSRF-Token": env.State.Values["m4_approver_csrf"], "Idempotency-Key": "m5-approve-" + env.Options.RunID})
	if err != nil {
		return err
	}
	if approved["status"] != "approved" {
		return fmt.Errorf("M5 Card Action approval ended as %v", approved["status"])
	}
	if _, err := a.waitExecutionForAction(ctx, env, actionRef, 2*time.Minute); err != nil {
		return err
	}
	if err := a.waitRunStatus(ctx, env, runID, "succeeded", 2*time.Minute); err != nil {
		return err
	}
	label, err := a.postgresQuery(ctx, env, "SELECT labels->>'release' FROM hosts WHERE id='"+hostID+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(label) != "m5-card-action" {
		return fmt.Errorf("M5 Card Action did not complete through the governed executor")
	}
	return nil
}

func (a *App) verifyM5SystemCatalog(ctx context.Context, env *E2EEnvironment) (string, error) {
	before, err := a.postgresQuery(ctx, env, "SELECT string_agg(id::text || ':' || version::text || ':' || coalesce(active_version_id::text,''), ',' ORDER BY id) FROM interactive_cards WHERE source='system';")
	if err != nil {
		return "", err
	}
	if _, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-server", "argus-server", "/usr/local/bin/argus-card-catalog-sync"); err != nil {
		return "", err
	}
	after, err := a.postgresQuery(ctx, env, "SELECT string_agg(id::text || ':' || version::text || ':' || coalesce(active_version_id::text,''), ',' ORDER BY id) FROM interactive_cards WHERE source='system';")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(before) != strings.TrimSpace(after) {
		return "", fmt.Errorf("M5 system Card catalog sync changed an already synchronized catalog")
	}
	client, _ := scenarioHTTP(env)
	cards, err := client.JSON(ctx, "m5-system-cards", "enterprise", http.MethodGet, "/enterprise/interactive-cards", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return "", err
	}
	var systemCards []map[string]any
	var hostList map[string]any
	for _, card := range objectItems(cards) {
		if card["source"] != "system" {
			continue
		}
		systemCards = append(systemCards, card)
		if card["slug"] == "host-list" {
			hostList = card
		}
	}
	if len(systemCards) != 5 || hostList == nil {
		return "", fmt.Errorf("M5 expected five system Cards including host-list")
	}
	cardID, err := stringField(hostList, "id")
	if err != nil {
		return "", err
	}
	version, err := numberField(hostList, "version")
	if err != nil {
		return "", err
	}
	denied, err := client.JSON(ctx, "m5-system-readonly", "enterprise", http.MethodPost, "/enterprise/interactive-cards/"+cardID+"/disable", http.StatusForbidden,
		map[string]any{"expected_version": version}, enterpriseHeaders(env, "m5-system-readonly"))
	if err != nil {
		return "", err
	}
	if denied["code"] != "CARD_SOURCE_READ_ONLY" {
		return "", fmt.Errorf("M5 system Card mutation returned %v", denied["code"])
	}
	return cardID, nil
}

func (a *App) verifyM5QueryBindingInvalidation(ctx context.Context, env *E2EEnvironment, systemCardID string) error {
	client, _ := scenarioHTTP(env)
	message, err := client.JSON(ctx, "m5-system-render-message", "enterprise", http.MethodPost, "/conversations/"+env.State.Values["m4_conversation_id"]+"/messages", http.StatusAccepted,
		map[string]any{"content": "Call host.list and present a table card."}, enterpriseHeaders(env, "m5-system-render"))
	if err != nil {
		return err
	}
	runID, err := stringField(message, "run", "run_id")
	if err != nil {
		return err
	}
	if err := a.waitRunTerminal(ctx, env, runID); err != nil {
		return err
	}
	instanceID, err := a.waitM5CardInstance(ctx, env, runID, systemCardID)
	if err != nil {
		return err
	}
	env.State.Values["m5_system_instance_id"] = instanceID
	presentation, err := client.JSON(ctx, "m5-system-presentation", "enterprise", http.MethodPost, "/enterprise/card-instances/"+instanceID+"/presentations", http.StatusCreated,
		map[string]any{"locale": "zh-CN", "color_scheme": "light"}, enterpriseHeaders(env, "m5-presentation"))
	if err != nil {
		return err
	}
	bindingID, err := stringField(presentation, "render_plan", "query_binding_ids", "refresh")
	if err != nil {
		return err
	}
	invoked, err := client.JSON(ctx, "m5-query-invoke", "enterprise", http.MethodPost, "/enterprise/card-query-bindings/"+bindingID+"/invoke", http.StatusOK, nil, enterpriseHeaders(env, "m5-query"))
	if err != nil {
		return err
	}
	if invoked["status"] != "succeeded" {
		return fmt.Errorf("M5 Query Binding did not succeed")
	}
	if _, err := client.JSON(ctx, "m5-query-replay", "enterprise", http.MethodPost, "/enterprise/card-query-bindings/"+bindingID+"/invoke", http.StatusOK, nil, enterpriseHeaders(env, "m5-query")); err != nil {
		return err
	}
	if _, err := client.JSON(ctx, "m5-auth-change-binding", "enterprise", http.MethodPost, "/enterprise/role-bindings", http.StatusCreated,
		map[string]any{"subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "role_id": env.State.Values["m4_resource_viewer_role_id"]}, enterpriseHeaders(env, "m5-auth-binding")); err != nil {
		return err
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	// Role bindings invalidate the authorization version for every enterprise user.
	if err := a.refreshM4ApproverLogin(ctx, env); err != nil {
		return err
	}
	invalidated, err := client.JSON(ctx, "m5-old-query-invalidated", "enterprise", http.MethodPost, "/enterprise/card-query-bindings/"+bindingID+"/invoke", http.StatusForbidden, nil, enterpriseHeaders(env, "m5-query-stale"))
	if err != nil {
		return err
	}
	if invalidated["code"] != "CARD_PRESENTATION_INVALIDATED" {
		return fmt.Errorf("M5 stale Query Binding returned %v", invalidated["code"])
	}
	rematerialized, err := client.JSON(ctx, "m5-rematerialized-presentation", "enterprise", http.MethodPost, "/enterprise/card-instances/"+instanceID+"/presentations", http.StatusCreated,
		map[string]any{"locale": "en-US", "color_scheme": "dark"}, enterpriseHeaders(env, "m5-rematerialize"))
	if err != nil {
		return err
	}
	refreshedID, err := stringField(rematerialized, "render_plan", "query_binding_ids", "refresh")
	if err != nil {
		return err
	}
	if refreshedID == bindingID {
		return fmt.Errorf("M5 rematerialization reused an invalidated Query Binding")
	}
	return nil
}

func (a *App) waitM5CardInstance(ctx context.Context, env *E2EEnvironment, runID, cardID string) (string, error) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		value, err := a.postgresQuery(ctx, env, "SELECT id FROM card_instances WHERE run_id='"+runID+"' AND card_id='"+cardID+"' ORDER BY created_at DESC LIMIT 1;")
		if err != nil {
			return "", err
		}
		if id := strings.TrimSpace(value); id != "" {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", fmt.Errorf("M5 CardInstance for Run %s did not materialize", runID)
}

func (a *App) waitM5CardInstanceBySource(ctx context.Context, env *E2EEnvironment, runID, source string) (string, error) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		value, err := a.postgresQuery(ctx, env, "SELECT instance.id FROM card_instances instance JOIN interactive_cards card ON card.id=instance.card_id WHERE instance.run_id='"+runID+"' AND card.source='"+source+"' ORDER BY instance.created_at DESC LIMIT 1;")
		if err != nil {
			return "", err
		}
		if id := strings.TrimSpace(value); id != "" {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", fmt.Errorf("M5 %s CardInstance for Run %s did not materialize", source, runID)
}

func (a *App) verifyM5Recovery(ctx context.Context, env *E2EEnvironment, cardID string) error {
	password, err := dataCredentialValue(ctx, env, "redis-password")
	if err != nil {
		return err
	}
	if _, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-redis", "redis", "redis-cli", "-a", password, "FLUSHALL"); err != nil {
		return err
	}
	if err := env.Kube.DeletePods(ctx, env.SystemNS, "app.kubernetes.io/name=argus-server"); err != nil {
		return err
	}
	if err := env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-server", 5*time.Minute); err != nil {
		return err
	}
	if err := waitHTTPSReady(ctx, env.Endpoints.HTTPClient(), env.Endpoints.PlatformOrigin+"/readyz", "ready", 2*time.Minute); err != nil {
		return fmt.Errorf("M5 API through ingress did not recover: %w", err)
	}
	client, _ := scenarioHTTP(env)
	card, err := client.JSON(ctx, "m5-recovered-card", "enterprise", http.MethodGet, "/enterprise/interactive-cards/"+cardID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	activeRevision, _ := numberField(card, "active_revision")
	latestRevision, _ := numberField(card, "latest_revision")
	if activeRevision != 1 || latestRevision != 2 {
		return fmt.Errorf("M5 Card revisions did not survive Redis and API recovery")
	}
	instanceID := env.State.Values["m5_system_instance_id"]
	if instanceID == "" {
		return fmt.Errorf("M5 system CardInstance is unavailable for recovery")
	}
	if _, err := client.JSON(ctx, "m5-recovered-presentation", "enterprise", http.MethodPost, "/enterprise/card-instances/"+instanceID+"/presentations", http.StatusCreated,
		map[string]any{"locale": "zh-CN", "color_scheme": "light"}, enterpriseHeaders(env, "m5-recovered-presentation")); err != nil {
		return err
	}
	count, err := a.postgresQuery(ctx, env, "SELECT count(*) FROM card_instances WHERE run_id='"+env.State.Values["m5_action_run_id"]+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(count) != "1" {
		return fmt.Errorf("M5 recovery duplicated the action CardInstance")
	}
	return nil
}
