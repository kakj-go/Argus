package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type p4Scenario struct {
	CredentialID    string
	DistributionID  string
	HostProfiles    []string
	SelfTarget      p4Target
	ReplayTarget    p4Target
	CommandTarget   p4Target
	DirectTarget    p4Target
	TunnelTarget    p4Target
	MemberTarget    p4Target
	TunnelInstall   p4InstallResult
	RootCollectorID string
}

func (a *App) runP4Scenario(ctx context.Context, env *E2EEnvironment) error {
	for _, workload := range []string{"argus-telemetry-ingest", "argus-telemetry-writer", "argus-telemetry-query"} {
		if err := env.Kube.WaitDeployment(ctx, env.ObservNS, workload, 5*time.Minute); err != nil {
			return err
		}
	}
	if err := a.patchP4DirectExecutor(ctx, env); err != nil {
		return err
	}
	credentialID, err := a.prepareP4EnterpriseAccess(ctx, env)
	if err != nil {
		return err
	}
	distributionID, profiles, _, err := a.verifyM7Catalog(ctx, env)
	if err != nil {
		return err
	}
	scenario := &p4Scenario{CredentialID: credentialID, DistributionID: distributionID, HostProfiles: profiles}
	targets := []struct {
		name    string
		ip      string
		network p4TargetNetwork
		set     func(p4Target)
	}{
		{"self", "198.51.100.20", p4NetworkOpen, func(value p4Target) { scenario.SelfTarget = value }},
		{"token-replay", "198.51.100.21", p4NetworkOpen, func(value p4Target) { scenario.ReplayTarget = value }},
		{"bastion-command", "198.51.100.22", p4NetworkOpen, func(value p4Target) { scenario.CommandTarget = value }},
		{"bastion-direct", "198.51.100.23", p4NetworkOpen, func(value p4Target) { scenario.DirectTarget = value }},
		{"bastion-tunnel", "198.51.100.24", p4NetworkRootTunnel, func(value p4Target) { scenario.TunnelTarget = value }},
		{"bastion-member", "198.51.100.25", p4NetworkMember, func(value p4Target) { scenario.MemberTarget = value }},
	}
	for _, item := range targets {
		target, createErr := a.createP4Target(ctx, env, item.name, item.ip, item.network)
		if createErr != nil {
			return createErr
		}
		item.set(target)
	}
	for _, target := range []p4Target{scenario.SelfTarget, scenario.CommandTarget, scenario.DirectTarget, scenario.TunnelTarget} {
		if _, err = a.execP4Target(ctx, env, target, "/bin/bash", "-lc",
			"set +e; curl -sS --connect-timeout 5 -o /dev/null "+env.EnterpriseOrigin()+"; code=$?; [ \"$code\" -eq 60 ]"); err != nil {
			return fmt.Errorf("P4 target %s unexpectedly trusted the Argus managed CA or did not reach its TLS endpoint: %w", target.Name, err)
		}
	}
	if err = a.runP4SelfEnrollment(ctx, env, scenario); err != nil {
		return fmt.Errorf("p4-self-enroll: %w", err)
	}
	if err = a.runP4CommandBastion(ctx, env, scenario); err != nil {
		return fmt.Errorf("p4-bastion-command: %w", err)
	}
	if err = a.runP4DirectBastion(ctx, env, scenario); err != nil {
		return fmt.Errorf("p4-bastion-direct-install: %w", err)
	}
	if err = a.runP4TunnelBastion(ctx, env, scenario); err != nil {
		return fmt.Errorf("p4-bastion-control-tunnel: %w", err)
	}
	if err = a.runP4MemberTunnel(ctx, env, scenario); err != nil {
		return fmt.Errorf("p4-bastion-tunnel: %w", err)
	}
	if err = a.verifyP4TunnelAudits(ctx, env); err != nil {
		return err
	}
	if err = a.verifyP4NoSensitivePersistence(ctx, env); err != nil {
		return err
	}
	return a.runPlaywright(ctx, env, "e2e/p4-real.spec.ts", map[string]string{
		"ARGUS_P4_E2E": "1", "ARGUS_P4_ENTERPRISE_USERNAME": env.State.Values["enterprise_username"],
		"ARGUS_P4_ENTERPRISE_PASSWORD": env.State.Values["enterprise_password"],
	})
}

func (a *App) prepareP4EnterpriseAccess(ctx context.Context, env *E2EEnvironment) (string, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return "", err
	}
	secret, err := client.JSON(ctx, "p4-secret-create", "enterprise", http.MethodPost, "/enterprise/secrets", http.StatusCreated,
		map[string]any{"name": "p4-root-password", "type": "ssh_password", "description": "PlanV4 E2E write-only secret", "value": p4SSHPassword},
		enterpriseHeaders(env, "p4-secret-create"))
	if err != nil {
		return "", err
	}
	if _, exposed := secret["value"]; exposed {
		return "", fmt.Errorf("P4 Secret response exposed write-only value")
	}
	secretID, err := stringField(secret, "id")
	if err != nil {
		return "", err
	}
	credential, err := client.JSON(ctx, "p4-credential-create", "enterprise", http.MethodPost, "/enterprise/credentials", http.StatusCreated,
		map[string]any{"name": "p4-root-ssh", "protocol": "ssh", "username": "root", "secret_id": secretID},
		enterpriseHeaders(env, "p4-credential-create"))
	if err != nil {
		return "", err
	}
	credentialID, err := stringField(credential, "id")
	if err != nil {
		return "", err
	}
	roles, err := client.JSON(ctx, "p4-roles", "enterprise", http.MethodGet, "/enterprise/roles", http.StatusOK, nil,
		map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return "", err
	}
	role, err := findItem(objectItems(roles), func(item map[string]any) bool {
		return item["builtin_key"] == "resource_admin" && item["builtin"] == true
	})
	if err != nil {
		return "", err
	}
	roleID, err := stringField(role, "id")
	if err != nil {
		return "", err
	}
	if _, err = client.JSON(ctx, "p4-admin-binding", "enterprise", http.MethodPost, "/enterprise/role-bindings", http.StatusCreated,
		map[string]any{"subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "role_id": roleID},
		enterpriseHeaders(env, "p4-admin-binding")); err != nil {
		return "", err
	}
	if err = a.refreshEnterpriseLogin(ctx, env); err != nil {
		return "", err
	}
	return credentialID, nil
}

func (a *App) runP4SelfEnrollment(ctx context.Context, env *E2EEnvironment, scenario *p4Scenario) error {
	client, _ := scenarioHTTP(env)
	architecture := strings.TrimPrefix(env.ImagePlatform, "linux/")
	preview, err := client.JSON(ctx, "p4-self-preview", "enterprise", http.MethodPost,
		"/enterprise/hosts/actions/preview-create", http.StatusCreated, map[string]any{
			"name": "p4-self-enrolled", "platform": "linux", "architecture": architecture,
			"connection_mode": "self_enrolled", "environment": "production", "labels": map[string]string{"suite": "p4", "mode": "self"},
		}, enterpriseHeaders(env, "p4-self-preview"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	confirmed, err := a.confirmPendingAction(ctx, env, "p4-self-confirm", actionRef)
	if err != nil {
		return err
	}
	hostID, err := stringField(confirmed, "resource_ref", "resource_id")
	if err != nil {
		return err
	}
	result, ok := confirmed["one_time_result"].(map[string]any)
	if !ok {
		return fmt.Errorf("self enrollment did not return a one-time result")
	}
	command, err := validateP4OneTimeResult(result, "host_install_command", confirmed)
	if err != nil {
		return err
	}
	executionID, _ := result["execution_id"].(string)
	retry, err := client.JSON(ctx, "p4-self-claim-retry", "enterprise", http.MethodPost,
		"/enterprise/executions/"+executionID+"/one-time-result", http.StatusOK, nil,
		enterpriseHeaders(env, "p4-self-confirm-one-time-result"))
	if err != nil || retry["command"] != command {
		return fmt.Errorf("same-idempotency one-time result retry was not stable: %v", err)
	}
	second, err := client.JSON(ctx, "p4-self-claim-rejected", "enterprise", http.MethodPost,
		"/enterprise/executions/"+executionID+"/one-time-result", http.StatusConflict, nil,
		enterpriseHeaders(env, "p4-self-second-claim"))
	if err != nil || second["code"] != "ACTION_RESULT_ALREADY_CONSUMED" {
		return fmt.Errorf("different-idempotency one-time result claim was not rejected: %#v, %v", second, err)
	}
	if _, err = a.execP4Target(ctx, env, scenario.SelfTarget, "/bin/bash", "-lc", command); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.SelfTarget, "/bin/bash", "-lc", command); err != nil {
		return fmt.Errorf("same-device bootstrap retry failed: %w", err)
	}
	if _, err = a.execP4Target(ctx, env, scenario.ReplayTarget, "/bin/bash", "-lc", command); err == nil {
		return fmt.Errorf("consumed self-enrollment command worked on a second device")
	}
	query := "SELECT h.status || '|' || h.connection_status || '|' || c.status || '|' || r.status FROM hosts h JOIN collector_instances c ON c.resource_id=h.id AND c.resource_type='host' JOIN telemetry_routes r ON r.collector_id=c.id WHERE h.id='" + hostID + "';"
	if err = a.waitPostgresValue(ctx, env, query, "active|online|converged|active", 5*time.Minute); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.SelfTarget, "/usr/local/bin/argus-telemetry-e2e",
		"--endpoint=127.0.0.1:4317", "--resource-id="+hostID, "--marker=p4-self"); err != nil {
		return err
	}
	if err = a.verifyM7Signals(ctx, env, hostID, "p4-self"); err != nil {
		return err
	}
	host, err := client.JSON(ctx, "p4-self-host", "enterprise", http.MethodGet, "/enterprise/hosts/"+hostID, http.StatusOK, nil,
		map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	version, err := numberField(host, "resource_version")
	if err != nil {
		return err
	}
	if err = a.stepUpEnterprise(ctx, env); err != nil {
		return err
	}
	uninstallPreview, err := client.JSON(ctx, "p4-self-uninstall-preview", "enterprise", http.MethodPost,
		"/enterprise/hosts/"+hostID+"/actions/preview-uninstall-command", http.StatusCreated,
		map[string]any{"expected_version": version}, enterpriseHeaders(env, "p4-self-uninstall-preview"))
	if err != nil {
		return err
	}
	uninstallRef, err := stringField(uninstallPreview, "action_ref")
	if err != nil {
		return err
	}
	uninstall, err := a.confirmPendingAction(ctx, env, "p4-self-uninstall-confirm", uninstallRef)
	if err != nil {
		return err
	}
	uninstallResult, ok := uninstall["one_time_result"].(map[string]any)
	if !ok {
		return fmt.Errorf("self uninstall did not return a one-time command")
	}
	uninstallCommand, err := validateP4OneTimeResult(uninstallResult, "host_uninstall_command", uninstall)
	if err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.SelfTarget, "/bin/bash", "-lc", uninstallCommand); err != nil {
		return err
	}
	if _, err = a.waitP4HostCollector(ctx, env, hostID, "uninstalled"); err != nil {
		return err
	}
	files, err := a.execP4Target(ctx, env, scenario.SelfTarget, "/bin/bash", "-lc",
		"test ! -e /usr/local/bin/argus-otelcol && test ! -e /etc/argus-otelcol && test ! -e /var/lib/argus-otelcol")
	if err != nil || files != "" {
		return fmt.Errorf("self uninstall left local files: %q, %v", files, err)
	}
	return nil
}

func validateP4OneTimeResult(result map[string]any, kind string, confirmation map[string]any) (string, error) {
	if result["schema_version"] != "argus.action_one_time_result/v2" || result["result_kind"] != kind {
		return "", fmt.Errorf("unexpected one-time result envelope: %#v", result)
	}
	executionID, _ := result["execution_id"].(string)
	confirmedID, _ := nestedString(confirmation, "execution", "execution_id")
	command, _ := result["command"].(string)
	expires, _ := result["expires_at"].(string)
	expiresAt, parseErr := time.Parse(time.RFC3339, expires)
	if executionID == "" || executionID != confirmedID || command == "" || parseErr != nil || !expiresAt.After(time.Now()) {
		return "", fmt.Errorf("incomplete one-time result envelope")
	}
	return command, nil
}

func (a *App) runP4CommandBastion(ctx context.Context, env *E2EEnvironment, scenario *p4Scenario) error {
	client, _ := scenarioHTTP(env)
	preview, err := client.JSON(ctx, "p4-command-bastion-preview", "enterprise", http.MethodPost,
		"/enterprise/bastion-scopes/actions/preview-create", http.StatusCreated, map[string]any{
			"name": "p4-command-bastion", "environment": "production", "labels": map[string]string{"suite": "p4", "mode": "a"}, "install_mode": "command",
		}, enterpriseHeaders(env, "p4-command-bastion-preview"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	confirmed, err := a.confirmPendingAction(ctx, env, "p4-command-bastion-confirm", actionRef)
	if err != nil {
		return err
	}
	scopeID, err := stringField(confirmed, "resource_ref", "resource_id")
	if err != nil {
		return err
	}
	result, ok := confirmed["one_time_result"].(map[string]any)
	if !ok {
		return fmt.Errorf("mode A did not return a one-time install command")
	}
	command, err := validateP4OneTimeResult(result, "connector_install_command", confirmed)
	if err != nil {
		return err
	}
	parsed, err := parseConnectorCommandResult(command)
	if err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.CommandTarget, "/bin/bash", "-lc", command); err != nil {
		return err
	}
	if err = a.waitM3ConnectorOnline(ctx, env, parsed.ConnectorID, 1); err != nil {
		return err
	}
	if err = a.waitPostgresValue(ctx, env,
		"SELECT status || '|' || onboarding_mode FROM bastion_scopes WHERE id='"+scopeID+"';", "active|command", 2*time.Minute); err != nil {
		return err
	}
	scope, err := client.JSON(ctx, "p4-command-bastion", "enterprise", http.MethodGet,
		"/enterprise/bastion-scopes/"+scopeID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	onboarding, ok := scope["onboarding"].(map[string]any)
	if !ok || onboarding["state"] != "registered" {
		return fmt.Errorf("mode A onboarding projection did not converge to registered: %#v", scope["onboarding"])
	}
	return nil
}
