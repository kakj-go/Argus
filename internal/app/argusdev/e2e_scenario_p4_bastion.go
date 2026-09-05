package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *App) runP4DirectBastion(ctx context.Context, env *E2EEnvironment, scenario *p4Scenario) error {
	testID, err := a.createP4ConnectionTest(ctx, env, "p4-mode-b", scenario.DirectTarget.ExternalIP, scenario.CredentialID, "")
	if err != nil {
		return err
	}
	client, _ := scenarioHTTP(env)
	preview, err := client.JSON(ctx, "p4-mode-b-preview", "enterprise", http.MethodPost,
		"/enterprise/bastion-scopes/actions/preview-create", http.StatusCreated, map[string]any{
			"name": "p4-direct-install", "environment": "production", "labels": map[string]string{"suite": "p4", "mode": "b"},
			"install_mode": "direct_install", "address": scenario.DirectTarget.ExternalIP, "port": 22,
			"username": "root", "credential_id": scenario.CredentialID, "connection_test_id": testID,
		}, enterpriseHeaders(env, "p4-mode-b-preview"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	installed, err := a.confirmP4ConnectorOperation(ctx, env, "p4-mode-b", actionRef, "direct_install")
	if err != nil {
		return err
	}
	if err = a.waitM3ConnectorOnline(ctx, env, installed.ConnectorID, 1); err != nil {
		return err
	}
	if err = a.waitPostgresValue(ctx, env,
		"SELECT status || '|' || onboarding_mode FROM bastion_scopes WHERE id='"+installed.ScopeID+"';",
		"active|direct_install", 2*time.Minute); err != nil {
		return err
	}
	scope, err := client.JSON(ctx, "p4-mode-b-onboarding", "enterprise", http.MethodGet,
		"/enterprise/bastion-scopes/"+installed.ScopeID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	onboardingState, err := stringField(scope, "onboarding", "state")
	if err != nil {
		return err
	}
	if onboardingState != "registered" {
		return fmt.Errorf("mode B onboarding projection is %q, want registered", onboardingState)
	}
	return nil
}

func (a *App) runP4TunnelBastion(ctx context.Context, env *E2EEnvironment, scenario *p4Scenario) error {
	testID, err := a.createP4ConnectionTest(ctx, env, "p4-mode-c", scenario.TunnelTarget.ExternalIP, scenario.CredentialID, "")
	if err != nil {
		return err
	}
	client, _ := scenarioHTTP(env)
	preview, err := client.JSON(ctx, "p4-mode-c-preview", "enterprise", http.MethodPost,
		"/enterprise/bastion-scopes/actions/preview-create", http.StatusCreated, map[string]any{
			"name": "p4-control-tunnel", "environment": "production", "labels": map[string]string{"suite": "p4", "mode": "c"},
			"install_mode": "direct_install_tunnel", "address": scenario.TunnelTarget.ExternalIP, "port": 22,
			"username": "root", "credential_id": scenario.CredentialID, "connection_test_id": testID,
		}, enterpriseHeaders(env, "p4-mode-c-preview"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	installed, err := a.confirmP4ConnectorOperation(ctx, env, "p4-mode-c", actionRef, "direct_install_tunnel")
	if err != nil {
		return err
	}
	if err = a.waitM3ConnectorOnline(ctx, env, installed.ConnectorID, 1); err != nil {
		return err
	}
	if err = a.verifyP4ControlTunnelTakeover(ctx, env, installed.ConnectorID); err != nil {
		return err
	}
	replaced, err := a.replaceP4TunnelConnector(ctx, env, scenario, installed)
	if err != nil {
		return err
	}
	scenario.TunnelInstall = replaced
	collector, err := a.applyP4HostCollector(ctx, env, "p4-mode-c-root", replaced.HostID,
		scenario.DistributionID, scenario.HostProfiles, "direct_argus", "executor_tunnel", "", 14317)
	if err != nil {
		return err
	}
	collectorID, err := stringField(collector, "id")
	if err != nil {
		return err
	}
	scenario.RootCollectorID = collectorID
	if err = a.waitPostgresValue(ctx, env,
		"SELECT r.status || '|' || r.transport || '|' || t.status || '|' || t.initiator FROM telemetry_routes r JOIN telemetry_tunnels t ON t.collector_id=r.collector_id WHERE r.collector_id='"+collectorID+"';",
		"active|executor_tunnel|established|direct_executor", 2*time.Minute); err != nil {
		return err
	}
	if err = a.verifyP4ExecutorTunnelTakeover(ctx, env, scenario.TunnelTarget, collectorID, replaced.HostID); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.TunnelTarget, "/usr/local/bin/argus-telemetry-e2e",
		"--endpoint=127.0.0.1:4317", "--resource-id="+replaced.HostID, "--marker=p4-control",
		"--tls-ca=/var/lib/argus-otelcol/identity/ca.pem", "--tls-cert=/var/lib/argus-otelcol/identity/client.pem",
		"--tls-key=/var/lib/argus-otelcol/identity/client-key.pem", "--tls-server-name=collector-"+collectorID+".argus.telemetry"); err != nil {
		return err
	}
	if err = a.verifyM7Signals(ctx, env, replaced.HostID, "p4-control"); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.TunnelTarget, "/bin/bash", "-lc",
		"timeout 3 bash -c '</dev/tcp/"+env.Endpoints.IngressIP+"/443'"); err == nil {
		return fmt.Errorf("mode C target retained a direct platform callback path")
	}
	return nil
}

func (a *App) verifyP4ExecutorTunnelTakeover(ctx context.Context, env *E2EEnvironment, target p4Target, collectorID, hostID string) error {
	query := "SELECT lease_owner || '|' || epoch::text || '|' || fence::text || '|' || status FROM telemetry_tunnels WHERE collector_id='" + collectorID + "';"
	before, err := a.postgresQuery(ctx, env, query)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimSpace(before), "|")
	if len(parts) != 4 || parts[0] == "" || parts[3] != "established" {
		return fmt.Errorf("executor tunnel was not durably owned before takeover: %q", before)
	}
	oldEpoch, epochErr := strconv.ParseInt(parts[1], 10, 64)
	oldFence, fenceErr := strconv.ParseInt(parts[2], 10, 64)
	if epochErr != nil || fenceErr != nil {
		return fmt.Errorf("invalid executor tunnel epoch/fence: %q", before)
	}
	oldOwner := parts[0]
	restoreSSH, err := a.blockP4ExecutorSSH(ctx, env, target)
	if err != nil {
		return err
	}
	restored := false
	defer func() {
		if !restored {
			_ = restoreSSH()
		}
	}()
	if err = waitP4Tick(ctx); err != nil {
		return err
	}
	if err = env.Kube.Client.CoreV1().Pods(env.SystemNS).Delete(ctx, oldOwner, metav1.DeleteOptions{}); err != nil {
		return err
	}
	if err = a.waitP4TargetPortClosed(ctx, env, target, 14317, 45*time.Second); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, target, "/usr/local/bin/argus-telemetry-e2e",
		"--endpoint=127.0.0.1:4317", "--resource-id="+hostID, "--marker=p4-executor-takeover",
		"--tls-ca=/var/lib/argus-otelcol/identity/ca.pem", "--tls-cert=/var/lib/argus-otelcol/identity/client.pem",
		"--tls-key=/var/lib/argus-otelcol/identity/client-key.pem", "--tls-server-name=collector-"+collectorID+".argus.telemetry"); err != nil {
		return fmt.Errorf("inject queued signal while executor tunnel was down: %w", err)
	}
	if err = restoreSSH(); err != nil {
		return err
	}
	restored = true
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		value, queryErr := a.postgresQuery(ctx, env, query)
		if queryErr != nil {
			return queryErr
		}
		current := strings.Split(strings.TrimSpace(value), "|")
		if len(current) == 4 && current[0] != "" && current[0] != oldOwner && current[3] == "established" {
			epoch, _ := strconv.ParseInt(current[1], 10, 64)
			fence, _ := strconv.ParseInt(current[2], 10, 64)
			if epoch > oldEpoch && fence > oldFence {
				if err = env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-direct-executor", 5*time.Minute); err != nil {
					return err
				}
				return a.verifyM7Signals(ctx, env, hostID, "p4-executor-takeover")
			}
		}
		if err = waitP4Tick(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("executor tunnel was not taken over from %s with a larger epoch/fence", oldOwner)
}

func (a *App) waitP4TargetPortClosed(ctx context.Context, env *E2EEnvironment, target p4Target, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := a.execP4Target(ctx, env, target, "/bin/bash", "-lc",
			"timeout 1 bash -c '</dev/tcp/127.0.0.1/"+strconv.Itoa(port)+"'")
		if err != nil {
			return nil
		}
		if err = waitP4Tick(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("target loopback port %d stayed open during tunnel outage", port)
}

func (a *App) verifyP4ControlTunnelTakeover(ctx context.Context, env *E2EEnvironment, connectorID string) error {
	query := "SELECT lease_owner || '|' || epoch::text || '|' || fence::text || '|' || status FROM connector_control_tunnels WHERE connector_id='" + connectorID + "';"
	before, err := a.postgresQuery(ctx, env, query)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimSpace(before), "|")
	if len(parts) != 4 || parts[0] == "" || parts[3] != "established" {
		return fmt.Errorf("control tunnel was not durably owned before takeover: %q", before)
	}
	oldEpoch, epochErr := strconv.ParseInt(parts[1], 10, 64)
	oldFence, fenceErr := strconv.ParseInt(parts[2], 10, 64)
	if epochErr != nil || fenceErr != nil {
		return fmt.Errorf("invalid control tunnel epoch/fence: %q", before)
	}
	oldOwner := parts[0]
	if err = env.Kube.Client.CoreV1().Pods(env.SystemNS).Delete(ctx, oldOwner, metav1.DeleteOptions{}); err != nil {
		return err
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		value, queryErr := a.postgresQuery(ctx, env, query)
		if queryErr != nil {
			return queryErr
		}
		current := strings.Split(strings.TrimSpace(value), "|")
		if len(current) == 4 && current[0] != "" && current[0] != oldOwner && current[3] == "established" {
			epoch, _ := strconv.ParseInt(current[1], 10, 64)
			fence, _ := strconv.ParseInt(current[2], 10, 64)
			if epoch > oldEpoch && fence > oldFence {
				if err = env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-direct-executor", 5*time.Minute); err != nil {
					return err
				}
				return a.waitM3ConnectorOnline(ctx, env, connectorID, 2)
			}
		}
		if err = waitP4Tick(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("control tunnel was not taken over from %s with a larger epoch/fence", oldOwner)
}

func (a *App) replaceP4TunnelConnector(ctx context.Context, env *E2EEnvironment, scenario *p4Scenario, previous p4InstallResult) (p4InstallResult, error) {
	client, _ := scenarioHTTP(env)
	testID, err := a.createP4ConnectionTest(ctx, env, "p4-mode-c-replacement", scenario.TunnelTarget.ExternalIP, scenario.CredentialID, "")
	if err != nil {
		return p4InstallResult{}, err
	}
	scope, err := client.JSON(ctx, "p4-mode-c-scope", "enterprise", http.MethodGet,
		"/enterprise/bastion-scopes/"+previous.ScopeID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return p4InstallResult{}, err
	}
	version, err := numberField(scope, "resource_version")
	if err != nil {
		return p4InstallResult{}, err
	}
	oldTunnelID, err := a.postgresQuery(ctx, env,
		"SELECT id FROM connector_control_tunnels WHERE connector_id='"+previous.ConnectorID+"';")
	if err != nil || strings.TrimSpace(oldTunnelID) == "" {
		return p4InstallResult{}, fmt.Errorf("old control tunnel is unavailable: %v", err)
	}
	if err = a.stepUpEnterprise(ctx, env); err != nil {
		return p4InstallResult{}, err
	}
	preview, err := client.JSON(ctx, "p4-mode-c-replacement-preview", "enterprise", http.MethodPost,
		"/enterprise/bastion-scopes/"+previous.ScopeID+"/actions/preview-connector-replacement", http.StatusCreated,
		map[string]any{"expected_version": version, "address": scenario.TunnelTarget.ExternalIP, "port": 22,
			"username": "root", "credential_id": scenario.CredentialID, "connection_test_id": testID},
		enterpriseHeaders(env, "p4-mode-c-replacement-preview"))
	if err != nil {
		return p4InstallResult{}, err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return p4InstallResult{}, err
	}
	replacement, err := a.confirmP4ConnectorOperation(ctx, env, "p4-mode-c-replacement", actionRef, "direct_install_tunnel")
	if err != nil {
		return p4InstallResult{}, err
	}
	if replacement.ConnectorID == previous.ConnectorID || replacement.HostID != previous.HostID || replacement.ScopeID != previous.ScopeID {
		return p4InstallResult{}, fmt.Errorf("replacement identities did not fence only the Connector")
	}
	oldTunnelID = strings.TrimSpace(oldTunnelID)
	query := "SELECT c.status || '|' || t.status || '|' || (SELECT count(*)::text FROM credential_leases l WHERE l.operation_ref='connector_control_tunnel:" + oldTunnelID + "' AND l.status='active') FROM connectors c JOIN connector_control_tunnels t ON t.connector_id=c.id WHERE c.id='" + previous.ConnectorID + "';"
	if err = a.waitPostgresValue(ctx, env, query, "revoked|removed|0", 2*time.Minute); err != nil {
		return p4InstallResult{}, err
	}
	if err = a.waitM3ConnectorOnline(ctx, env, replacement.ConnectorID, 1); err != nil {
		return p4InstallResult{}, err
	}
	return replacement, nil
}

func (a *App) runP4MemberTunnel(ctx context.Context, env *E2EEnvironment, scenario *p4Scenario) error {
	if scenario.TunnelInstall.ScopeID == "" || scenario.RootCollectorID == "" {
		return fmt.Errorf("mode C root is unavailable")
	}
	testID, err := a.createP4ConnectionTest(ctx, env, "p4-member", scenario.MemberTarget.ExternalIP,
		scenario.CredentialID, scenario.TunnelInstall.ScopeID)
	if err != nil {
		return err
	}
	client, _ := scenarioHTTP(env)
	preview, err := client.JSON(ctx, "p4-member-preview", "enterprise", http.MethodPost,
		"/enterprise/hosts/actions/preview-create", http.StatusCreated, map[string]any{
			"name": "p4-restricted-member", "address": scenario.MemberTarget.ExternalIP, "port": 22, "platform": "linux",
			"connection_mode": "via_bastion", "credential_id": scenario.CredentialID, "username": "root",
			"bastion_scope_id": scenario.TunnelInstall.ScopeID, "connection_test_id": testID,
			"environment": "production", "labels": map[string]string{"suite": "p4", "mode": "member-tunnel"},
		}, enterpriseHeaders(env, "p4-member-preview"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	confirmed, err := a.confirmPendingAction(ctx, env, "p4-member-confirm", actionRef)
	if err != nil {
		return err
	}
	hostID, err := stringField(confirmed, "resource_ref", "resource_id")
	if err != nil {
		return err
	}
	if err = a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	collector, err := a.applyP4HostCollector(ctx, env, "p4-member", hostID, scenario.DistributionID,
		scenario.HostProfiles, "bastion_gateway", "bastion_tunnel", scenario.RootCollectorID, 14318)
	if err != nil {
		return err
	}
	collectorID, err := stringField(collector, "id")
	if err != nil {
		return err
	}
	if err = a.waitPostgresValue(ctx, env,
		"SELECT r.status || '|' || r.transport || '|' || t.status || '|' || t.initiator FROM telemetry_routes r JOIN telemetry_tunnels t ON t.collector_id=r.collector_id WHERE r.collector_id='"+collectorID+"';",
		"active|bastion_tunnel|established|connector", 3*time.Minute); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.MemberTarget, "/usr/local/bin/argus-telemetry-e2e",
		"--endpoint=127.0.0.1:4317", "--resource-id="+hostID, "--marker=p4-member"); err != nil {
		return err
	}
	if err = a.verifyM7Signals(ctx, env, hostID, "p4-member"); err != nil {
		return err
	}
	if err = a.verifyP4MemberTunnelRecovery(ctx, env, scenario, collectorID, hostID); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.MemberTarget, "/bin/bash", "-lc",
		"timeout 3 bash -c '</dev/tcp/"+scenario.TunnelTarget.ExternalIP+"/4317'"); err == nil {
		return fmt.Errorf("restricted member retained direct access to the gateway OTLP port")
	}
	return nil
}

func (a *App) verifyP4MemberTunnelRecovery(ctx context.Context, env *E2EEnvironment, scenario *p4Scenario, collectorID, hostID string) error {
	query := "SELECT lease_owner || '|' || owner_connection_epoch::text || '|' || epoch::text || '|' || fence::text || '|' || status FROM telemetry_tunnels WHERE collector_id='" + collectorID + "';"
	before, err := a.postgresQuery(ctx, env, query)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimSpace(before), "|")
	if len(parts) != 5 || parts[0] == "" || parts[4] != "established" {
		return fmt.Errorf("member tunnel was not durably owned before reconnect: %q", before)
	}
	oldConnectionEpoch, connectionErr := strconv.ParseInt(parts[1], 10, 64)
	oldEpoch, epochErr := strconv.ParseInt(parts[2], 10, 64)
	oldFence, fenceErr := strconv.ParseInt(parts[3], 10, 64)
	if connectionErr != nil || epochErr != nil || fenceErr != nil {
		return fmt.Errorf("invalid member tunnel ownership: %q", before)
	}
	stopped := false
	defer func() {
		if stopped {
			recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = a.execP4Target(recoveryCtx, env, scenario.TunnelTarget, "systemctl", "start", "argus-connector.service")
		}
	}()
	if _, err = a.execP4Target(ctx, env, scenario.TunnelTarget, "systemctl", "stop", "argus-connector.service"); err != nil {
		return err
	}
	stopped = true
	if err = a.waitPostgresValue(ctx, env,
		"SELECT status FROM telemetry_tunnels WHERE collector_id='"+collectorID+"';", "down", 2*time.Minute); err != nil {
		return err
	}
	if _, err = a.execP4Target(ctx, env, scenario.MemberTarget, "/usr/local/bin/argus-telemetry-e2e",
		"--endpoint=127.0.0.1:4317", "--resource-id="+hostID, "--marker=p4-member-reconnect"); err != nil {
		return fmt.Errorf("inject queued member signal while Connector was stopped: %w", err)
	}
	if _, err = a.execP4Target(ctx, env, scenario.TunnelTarget, "systemctl", "start", "argus-connector.service"); err != nil {
		return err
	}
	stopped = false
	if err = a.waitM3ConnectorOnline(ctx, env, scenario.TunnelInstall.ConnectorID, oldConnectionEpoch+1); err != nil {
		return err
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		value, queryErr := a.postgresQuery(ctx, env, query)
		if queryErr != nil {
			return queryErr
		}
		current := strings.Split(strings.TrimSpace(value), "|")
		if len(current) == 5 && current[0] != "" && current[0] != parts[0] && current[4] == "established" {
			connectionEpoch, _ := strconv.ParseInt(current[1], 10, 64)
			epoch, _ := strconv.ParseInt(current[2], 10, 64)
			fence, _ := strconv.ParseInt(current[3], 10, 64)
			if connectionEpoch > oldConnectionEpoch && epoch > oldEpoch && fence > oldFence {
				return a.verifyM7Signals(ctx, env, hostID, "p4-member-reconnect")
			}
		}
		if err = waitP4Tick(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("member tunnel did not reconnect on a larger connection epoch/epoch/fence")
}

func (a *App) verifyP4TunnelAudits(ctx context.Context, env *E2EEnvironment) error {
	actions := []string{
		"telemetry.tunnel.claim", "telemetry.tunnel.establish", "telemetry.tunnel.disconnect",
		"connector.control_tunnel.claim", "connector.control_tunnel.establish", "connector.control_tunnel.disconnect",
	}
	for _, action := range actions {
		query := "SELECT count(*) FROM audit_events WHERE action='" + action + "' AND details ? 'tunnel_id' AND details ? 'epoch' AND details ? 'fence' AND details ? 'lease_owner';"
		value, err := a.postgresQuery(ctx, env, query)
		if err != nil {
			return err
		}
		count, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if parseErr != nil || count < 1 {
			return fmt.Errorf("P4 tunnel audit %s has no complete persistent evidence (count=%q)", action, value)
		}
	}
	return nil
}
