package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (a *App) verifyM4ReadAutomation(ctx context.Context, env *E2EEnvironment, automationID string) error {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := client.JSONArray(ctx, "m4-automation-runs", "enterprise", http.MethodGet, "/enterprise/automations/"+automationID+"/runs", http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
		if err != nil {
			return err
		}
		for _, run := range runs {
			if run["status"] == "succeeded" && nonEmptyResultRef(run["result_ref"]) {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("M4 read-only Automation did not persist an Artifact")
}

func nonEmptyResultRef(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return false
	}
}

func (a *App) verifyM4AutomationRevisionAndResultUnknown(ctx context.Context, env *E2EEnvironment, accountID, hostID string) error {
	client, _ := scenarioHTTP(env)
	versionText, err := a.postgresQuery(ctx, env, "SELECT resource_version FROM hosts WHERE id='"+hostID+"';")
	if err != nil {
		return err
	}
	var hostVersion int64
	if _, err := fmt.Sscan(strings.TrimSpace(versionText), &hostVersion); err != nil {
		return err
	}
	created, err := client.JSON(ctx, "m4-write-automation-create", "enterprise", http.MethodPost, "/enterprise/automations", http.StatusCreated,
		map[string]any{"name": "M4 governed host update", "service_account_id": accountID, "tool_id": "host.update.preview", "tool_input": map[string]any{"host_id": hostID, "expected_version": hostVersion, "labels": map[string]string{"environment": "prod", "team": "m4", "release": "automation-v1"}}, "cron": "* * * * *", "timezone": "UTC"}, enterpriseHeaders(env, "m4-write-automation"))
	if err != nil {
		return err
	}
	automationID, _ := stringField(created, "id")
	automationVersion, _ := numberField(created, "version")
	runID := uuid.NewString()
	taskID := uuid.NewString()
	insert := "INSERT INTO runtime_tasks (id,enterprise_id,queue,payload,available_at) VALUES ('" + taskID + "','" + env.State.Values["enterprise_id"] + "','automation',jsonb_build_object('run_id','" + runID + "','enterprise_id','" + env.State.Values["enterprise_id"] + "'),now()+interval '10 minutes');" +
		"INSERT INTO automation_runs (id,automation_id,enterprise_id,automation_revision,scheduled_for,status,task_id) VALUES ('" + runID + "','" + automationID + "','" + env.State.Values["enterprise_id"] + "',1,now(),'pending','" + taskID + "');"
	if _, err := a.postgresQuery(ctx, env, insert); err != nil {
		return err
	}
	updated, err := client.JSON(ctx, "m4-write-automation-update", "enterprise", http.MethodPut, "/enterprise/automations/"+automationID, http.StatusOK,
		map[string]any{"name": "M4 governed host update revision 2", "service_account_id": accountID, "tool_id": "host.update.preview", "tool_input": map[string]any{"host_id": hostID, "expected_version": hostVersion, "labels": map[string]string{"environment": "prod", "team": "m4", "release": "automation-v2"}}, "cron": "* * * * *", "timezone": "UTC", "expected_version": automationVersion}, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	updatedVersion, _ := numberField(updated, "version")
	if updatedVersion != 2 {
		return fmt.Errorf("M4 Automation update produced version %d", updatedVersion)
	}
	if _, err := a.postgresQuery(ctx, env, "UPDATE runtime_tasks SET available_at=now(),updated_at=now() WHERE id='"+taskID+"';"); err != nil {
		return err
	}
	actionRef, err := a.waitM4AutomationApproval(ctx, env, automationID, runID)
	if err != nil {
		return err
	}
	boundLabel, err := a.postgresQuery(ctx, env, "SELECT immutable_plan->'input'->'Labels'->>'release' FROM pending_action_plans plan JOIN pending_actions action ON action.id=plan.pending_action_id WHERE action.action_ref='"+actionRef+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(boundLabel) != "automation-v1" {
		return fmt.Errorf("M4 AutomationRun used mutable Revision 2 input")
	}
	approvalID, err := a.postgresQuery(ctx, env, "SELECT request.id FROM approval_requests request JOIN pending_actions action ON action.id=request.pending_action_id WHERE action.action_ref='"+actionRef+"';")
	if err != nil {
		return err
	}
	approved, err := client.JSON(ctx, "m4-automation-approve", "m4-approver", http.MethodPost, "/enterprise/approval-requests/"+strings.TrimSpace(approvalID)+"/decisions", http.StatusOK,
		map[string]any{"decision": "approved", "reason": "governed Automation approval"}, map[string]string{"Origin": enterpriseOrigin, "X-CSRF-Token": env.State.Values["m4_approver_csrf"], "Idempotency-Key": "m4-automation-approve-" + env.Options.RunID})
	if err != nil {
		return err
	}
	if approved["status"] != "approved" {
		return fmt.Errorf("M4 Automation approval ended as %v", approved["status"])
	}
	executionID, err := a.waitExecutionForAction(ctx, env, actionRef, 2*time.Minute)
	if err != nil {
		return err
	}
	if err := a.waitPostgresValue(ctx, env, "SELECT status FROM automation_runs WHERE id='"+runID+"';", "succeeded", time.Minute); err != nil {
		return err
	}
	label, err := a.postgresQuery(ctx, env, "SELECT labels->>'release' FROM hosts WHERE id='"+hostID+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(label) != "automation-v1" {
		return fmt.Errorf("M4 Automation Revision 1 commit did not persist")
	}
	if err := a.verifyM4ResultUnknown(ctx, env, hostID, actionRef, runID, executionID); err != nil {
		return err
	}
	disabled, err := client.JSON(ctx, "m4-write-automation-disable", "enterprise", http.MethodPost, "/enterprise/automations/"+automationID+"/disable?expected_version=2", http.StatusOK, nil, enterpriseHeaders(env, ""))
	if err != nil {
		return err
	}
	if disabled["status"] != "disabled" {
		return fmt.Errorf("M4 governed Automation was not disabled")
	}
	return nil
}

func (a *App) waitM4AutomationApproval(ctx context.Context, env *E2EEnvironment, automationID, runID string) (string, error) {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := client.JSONArray(ctx, "m4-write-automation-runs", "enterprise", http.MethodGet, "/enterprise/automations/"+automationID+"/runs", http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
		if err != nil {
			return "", err
		}
		for _, run := range runs {
			revision, _ := numberField(run, "automation_revision")
			if run["id"] == runID && run["status"] == "waiting_approval" && revision == 1 {
				return stringField(run, "pending_action_ref")
			}
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("M4 write Automation did not reach approval on Revision 1")
}

func (a *App) verifyM4ResultUnknown(ctx context.Context, env *E2EEnvironment, hostID, actionRef, runID, executionID string) error {
	clusterID := uuid.NewString()
	connectorID := uuid.NewString()
	commandID := uuid.NewString()
	operationRef := "m4-reconcile-" + env.Options.RunID
	setup := "INSERT INTO kubernetes_clusters (id,enterprise_id,name,api_server,connection_mode,environment,labels,labels_hash,connection_status,status) VALUES ('" + clusterID + "','" + env.State.Values["enterprise_id"] + "','m4-reconcile-cluster','https://kubernetes.default.svc','in_cluster','development','{}',decode(repeat('00',32),'hex'),'disconnected','active');" +
		"INSERT INTO connectors (id,enterprise_id,role,name,kubernetes_cluster_id,instance_id,device_fingerprint_hash,public_key_hash,status,connection_epoch,certificate_expires_at) VALUES ('" + connectorID + "','" + env.State.Values["enterprise_id"] + "','kubernetes','m4-reconcile-connector','" + clusterID + "','" + operationRef + "',decode(repeat('00',32),'hex'),decode(repeat('11',32),'hex'),'offline',1,now()+interval '1 day');" +
		"UPDATE kubernetes_clusters SET connector_id='" + connectorID + "',updated_at=now() WHERE id='" + clusterID + "';" +
		"INSERT INTO connector_commands (id,command_id,enterprise_id,connector_id,connection_epoch,operation_ref,command_type,payload_schema_version,payload,payload_hash,idempotency_key,status,result,expires_at) VALUES ('" + commandID + "','" + operationRef + "','" + env.State.Values["enterprise_id"] + "','" + connectorID + "',1,'" + operationRef + "','connector_uninstall','argus.connector_command/v1','{}',decode(repeat('00',32),'hex'),'" + operationRef + "','result_unknown','{}',now()+interval '10 minutes');"
	if _, err := a.postgresQuery(ctx, env, setup); err != nil {
		return err
	}
	resourceVersion, err := a.postgresQuery(ctx, env, "SELECT resource_version FROM hosts WHERE id='"+hostID+"';")
	if err != nil {
		return err
	}
	reset := "UPDATE executions SET status='result_unknown',connector_command_id='" + commandID + "',error_code='EXECUTION_RESULT_UNKNOWN',completed_at=NULL,updated_at=now() WHERE id='" + executionID + "';" +
		"UPDATE pending_actions SET status='executing',updated_at=now() WHERE action_ref='" + actionRef + "';" +
		"UPDATE automation_runs SET status='running',updated_at=now() WHERE id='" + runID + "';"
	if _, err := a.postgresQuery(ctx, env, reset); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	status, err := a.postgresQuery(ctx, env, "SELECT status FROM executions WHERE id='"+executionID+"';")
	if err != nil {
		return err
	}
	currentVersion, err := a.postgresQuery(ctx, env, "SELECT resource_version FROM hosts WHERE id='"+hostID+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "result_unknown" || strings.TrimSpace(currentVersion) != strings.TrimSpace(resourceVersion) {
		return fmt.Errorf("M4 ResultUnknown reconciler replayed an unresolved side effect")
	}
	if _, err := a.postgresQuery(ctx, env, "UPDATE connector_commands SET status='succeeded',completed_at=now(),updated_at=now() WHERE id='"+commandID+"';"); err != nil {
		return err
	}
	if _, err := a.waitExecutionForAction(ctx, env, actionRef, 2*time.Minute); err != nil {
		return err
	}
	if err := a.waitPostgresValue(ctx, env, "SELECT status FROM automation_runs WHERE id='"+runID+"';", "succeeded", time.Minute); err != nil {
		return err
	}
	finalVersion, err := a.postgresQuery(ctx, env, "SELECT resource_version FROM hosts WHERE id='"+hostID+"';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(finalVersion) != strings.TrimSpace(resourceVersion) {
		return fmt.Errorf("M4 resolved ResultUnknown repeated the Host mutation")
	}
	return nil
}
