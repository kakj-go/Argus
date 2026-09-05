package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

type p4InstallResult struct {
	ExecutionID string
	OperationID string
	ScopeID     string
	ConnectorID string
	HostID      string
	Operation   map[string]any
}

func (a *App) createP4ConnectionTest(ctx context.Context, env *E2EEnvironment, name, address, credentialID string, scopeID string) (string, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return "", err
	}
	mode := "direct_ssh"
	body := map[string]any{
		"address": address, "port": 22, "platform": "linux", "connection_mode": mode,
		"credential_id": credentialID, "username": "root",
	}
	if scopeID != "" {
		body["connection_mode"] = "via_bastion"
		body["bastion_scope_id"] = scopeID
	}
	test, err := client.JSON(ctx, name+"-connection-test", "enterprise", http.MethodPost,
		"/enterprise/hosts/connection-tests", http.StatusAccepted, body, enterpriseHeaders(env, name+"-connection-test"))
	if err != nil {
		return "", err
	}
	id, err := stringField(test, "id")
	if err != nil {
		return "", err
	}
	if err = a.waitConnectionTest(ctx, env, id); err != nil {
		return "", err
	}
	return id, nil
}

func (a *App) confirmP4ConnectorOperation(ctx context.Context, env *E2EEnvironment, name, actionRef, installMode string) (p4InstallResult, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return p4InstallResult{}, err
	}
	confirmed, err := client.JSON(ctx, name+"-confirm", "enterprise", http.MethodPost,
		"/enterprise/pending-actions/"+actionRef+"/confirm", http.StatusOK, nil, enterpriseHeaders(env, name+"-confirm"))
	if err != nil {
		return p4InstallResult{}, err
	}
	executionID, err := stringField(confirmed, "execution", "execution_id")
	if err != nil {
		return p4InstallResult{}, fmt.Errorf("%s did not create an execution: %w", name, err)
	}

	result := p4InstallResult{ExecutionID: executionID}
	deadline := time.Now().Add(2 * time.Minute)
	sawResultUnknown := false
	for time.Now().Before(deadline) {
		value, queryErr := a.postgresQuery(ctx, env,
			"SELECT coalesce(connector_install_operation_id::text,'') || '|' || status FROM executions WHERE id='"+executionID+"';")
		if queryErr != nil {
			return p4InstallResult{}, queryErr
		}
		parts := strings.SplitN(strings.TrimSpace(value), "|", 2)
		if len(parts) == 2 {
			if parts[1] == "result_unknown" {
				sawResultUnknown = true
			}
			if parts[0] != "" {
				result.OperationID = parts[0]
				break
			}
			if parts[1] == "failed" || parts[1] == "cancelled" {
				return p4InstallResult{}, fmt.Errorf("%s execution %s ended as %s before operation creation", name, executionID, parts[1])
			}
		}
		if err = waitP4Tick(ctx); err != nil {
			return p4InstallResult{}, err
		}
	}
	if result.OperationID == "" {
		return p4InstallResult{}, fmt.Errorf("%s execution did not expose a Connector install operation", name)
	}
	if !sawResultUnknown {
		return p4InstallResult{}, fmt.Errorf("%s execution skipped the required result_unknown state", name)
	}
	oneTimeCount, err := a.postgresQuery(ctx, env,
		"SELECT count(*) FROM execution_one_time_results WHERE execution_id='"+executionID+"';")
	if err != nil {
		return p4InstallResult{}, err
	}
	if strings.TrimSpace(oneTimeCount) != "0" {
		return p4InstallResult{}, fmt.Errorf("%s exposed a one-time command for a platform install", name)
	}

	deadline = time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		operation, getErr := client.JSON(ctx, name+"-operation", "enterprise", http.MethodGet,
			"/enterprise/connector-install-operations/"+result.OperationID, http.StatusOK, nil,
			map[string]string{"Origin": env.EnterpriseOrigin()})
		if getErr != nil {
			return p4InstallResult{}, getErr
		}
		result.Operation = operation
		status, _ := operation["status"].(string)
		switch status {
		case "succeeded":
			if operation["stage"] != "completed" || operation["install_mode"] != installMode {
				return p4InstallResult{}, fmt.Errorf("%s operation completed with an invalid projection: %#v", name, operation)
			}
			result.ScopeID, _ = operation["bastion_scope_id"].(string)
			result.ConnectorID, _ = operation["connector_id"].(string)
			result.HostID, _ = operation["host_id"].(string)
			if result.ScopeID == "" || result.ConnectorID == "" || result.HostID == "" || operation["connector_online_at"] == nil {
				return p4InstallResult{}, fmt.Errorf("%s operation omitted its converged identities", name)
			}
			if installMode == "direct_install_tunnel" && operation["control_tunnel_status"] != "established" {
				return p4InstallResult{}, fmt.Errorf("%s mode C operation completed without an established control tunnel", name)
			}
			if err = validateP4OperationTimeline(operation, installMode); err != nil {
				return p4InstallResult{}, fmt.Errorf("%s: %w", name, err)
			}
			if err = a.waitPostgresValue(ctx, env, "SELECT status FROM executions WHERE id='"+executionID+"';", "succeeded", 2*time.Minute); err != nil {
				return p4InstallResult{}, err
			}
			return result, nil
		case "failed", "expired", "cancelled":
			code, _ := operation["error_code"].(string)
			return p4InstallResult{}, fmt.Errorf("%s operation ended as %s (%s)", name, status, code)
		}
		if err = waitP4Tick(ctx); err != nil {
			return p4InstallResult{}, err
		}
	}
	return p4InstallResult{}, fmt.Errorf("%s operation %s timed out", name, result.OperationID)
}

func validateP4OperationTimeline(operation map[string]any, mode string) error {
	want := []string{"queued", "ssh_connecting", "artifact_verifying", "artifact_transferring", "service_installing"}
	if mode == "direct_install_tunnel" {
		want = append(want, "control_tunnel_establishing")
	}
	want = append(want, "enrolling", "waiting_connector_online", "completed")
	events, _ := operation["events"].([]any)
	observed := make([]string, 0, len(events))
	for _, raw := range events {
		event, _ := raw.(map[string]any)
		stage, _ := event["stage"].(string)
		status, _ := event["status"].(string)
		if stage != "" && (status == "started" || status == "succeeded") && !slices.Contains(observed, stage) {
			observed = append(observed, stage)
		}
	}
	index := 0
	for _, stage := range observed {
		if index < len(want) && stage == want[index] {
			index++
		}
	}
	if index != len(want) {
		return fmt.Errorf("Connector install timeline = %v, want ordered stages %v", observed, want)
	}
	return nil
}

func waitP4Tick(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
		return nil
	}
}

func (a *App) verifyP4NoSensitivePersistence(ctx context.Context, env *E2EEnvironment) error {
	checks := []struct {
		name  string
		query string
	}{
		{"pending action plan", `SELECT count(*) FROM pending_action_plans
WHERE immutable_plan::text ILIKE '%"enrollment_token"%'
   OR immutable_plan::text ILIKE '%--token%'
   OR immutable_plan::text ILIKE '%curl -fsSL%';`},
		{"Connector install plan", `SELECT count(*) FROM connector_install_operations
WHERE plan::text ILIKE '%"enrollment_token"%'
   OR plan::text ILIKE '%--token%';`},
		{"audit details", `SELECT count(*) FROM audit_events
WHERE details::text ILIKE '%"enrollment_token"%'
   OR details::text ILIKE '%--token%'
   OR details::text ILIKE '%curl -fsSL%';`},
		{"expired credential lease", `SELECT count(*) FROM credential_leases
WHERE status = 'active' AND expires_at <= now();`},
		{"invalid one-time envelope", `SELECT count(*) FROM execution_one_time_results
WHERE result_kind NOT IN ('host_install_command','host_uninstall_command','connector_install_command')
   OR octet_length(nonce) = 0 OR octet_length(ciphertext) = 0;`},
		{"plaintext operation secret", `SELECT count(*) FROM connector_install_operation_secrets
WHERE octet_length(nonce) = 0 OR octet_length(ciphertext) = 0;`},
	}
	for _, check := range checks {
		value, err := a.postgresQuery(ctx, env, check.query)
		if err != nil {
			return fmt.Errorf("P4 sensitive persistence check %s: %w", check.name, err)
		}
		if strings.TrimSpace(value) != "0" {
			return fmt.Errorf("P4 sensitive persistence check %s found %s violations", check.name, strings.TrimSpace(value))
		}
	}
	return nil
}

func (a *App) waitP4HostCollector(ctx context.Context, env *E2EEnvironment, hostID, expected string) (map[string]any, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		collector, getErr := client.JSON(ctx, "p4-host-collector-"+hostID, "enterprise", http.MethodGet,
			"/enterprise/hosts/"+hostID+"/collector", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
		if getErr != nil {
			return nil, getErr
		}
		status, _ := collector["status"].(string)
		if status == expected {
			return collector, nil
		}
		if status == "degraded" || status == "result_unknown" {
			return nil, fmt.Errorf("Collector for host %s entered %s", hostID, status)
		}
		if err = waitP4Tick(ctx); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("Collector for host %s did not reach %s", hostID, expected)
}

func (a *App) applyP4HostCollector(ctx context.Context, env *E2EEnvironment, name, hostID, distributionID string, profiles []string,
	routeKind, transport, gatewayCollectorID string, loopbackPort int) (map[string]any, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"distribution_version_id": distributionID, "profile_ids": profiles, "route_kind": routeKind,
		"transport": transport,
	}
	if loopbackPort > 0 {
		body["loopback_port"] = loopbackPort
	}
	if gatewayCollectorID != "" {
		body["gateway_collector_id"] = gatewayCollectorID
	}
	preview, err := client.JSON(ctx, name+"-collector-preview", "enterprise", http.MethodPost,
		"/enterprise/hosts/"+hostID+"/collector/actions/preview-install", http.StatusCreated,
		body, enterpriseHeaders(env, name+"-collector-preview"))
	if err != nil {
		return nil, err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return nil, err
	}
	if _, err = a.confirmPendingAction(ctx, env, name+"-collector", actionRef); err != nil {
		return nil, err
	}
	return a.waitP4HostCollector(ctx, env, hostID, "converged")
}
