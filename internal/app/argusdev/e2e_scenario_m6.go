package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func (a *App) runM6Scenario(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	// M3 creates label-addressable resources and advances the authorization
	// version. Start M6 with a session carrying the current version.
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	secret, err := client.JSON(ctx, "m6-winrs-secret", "enterprise", http.MethodPost, "/enterprise/secrets", http.StatusCreated,
		map[string]any{"name": "m6-winrs-password", "type": "winrm_password", "description": "M6 E2E WinRS credential", "value": "M6-e2e-winrs-password"}, enterpriseHeaders(env, "m6-winrs-secret"))
	if err != nil {
		return err
	}
	secretID, _ := stringField(secret, "id")
	credential, err := client.JSON(ctx, "m6-winrs-credential", "enterprise", http.MethodPost, "/enterprise/credentials", http.StatusCreated,
		map[string]any{"name": "m6-winrs", "protocol": "winrm", "username": "argus", "secret_id": secretID}, enterpriseHeaders(env, "m6-winrs-credential"))
	if err != nil {
		return err
	}
	credentialID, _ := stringField(credential, "id")
	test, err := client.JSON(ctx, "m6-winrs-test", "enterprise", http.MethodPost, "/enterprise/hosts/connection-tests", http.StatusAccepted,
		map[string]any{"address": "8.8.8.8", "port": 5986, "platform": "windows", "connection_mode": "direct_winrm", "credential_id": credentialID, "username": "argus"}, enterpriseHeaders(env, "m6-winrs-test"))
	if err != nil {
		return err
	}
	testID, _ := stringField(test, "id")
	if err := a.waitConnectionTest(ctx, env, testID); err != nil {
		return err
	}
	preview, err := client.JSON(ctx, "m6-winrs-preview", "enterprise", http.MethodPost, "/enterprise/hosts/actions/preview-create", http.StatusCreated,
		map[string]any{"name": "m6-winrs-host", "address": "8.8.8.8", "port": 5986, "platform": "windows", "connection_mode": "direct_winrm", "credential_id": credentialID, "username": "argus", "environment": "production", "labels": map[string]string{"team": "m3", "route": "direct", "terminal": "winrs"}, "connection_test_id": testID}, enterpriseHeaders(env, "m6-winrs-host"))
	if err != nil {
		return err
	}
	actionRef, _ := stringField(preview, "action_ref")
	confirmed, err := a.confirmPendingAction(ctx, env, "m6-winrs-confirm", actionRef)
	if err != nil {
		return err
	}
	winrsHostID, _ := stringField(confirmed, "resource_ref", "resource_id")
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	accounts, err := client.JSON(ctx, "m6-managed-accounts", "enterprise", http.MethodGet, "/enterprise/managed-accounts", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	winrsAccount, err := findItem(objectItems(accounts), func(item map[string]any) bool { return item["host_id"] == winrsHostID })
	if err != nil {
		return fmt.Errorf("M6 managed account for WinRS Host %s: %w", winrsHostID, err)
	}
	winrsAccountID, _ := stringField(winrsAccount, "id")
	sshHostID := env.State.Values["m3_direct_host_id"]
	sshAccount, err := findItem(objectItems(accounts), func(item map[string]any) bool { return item["host_id"] == sshHostID })
	if err != nil {
		return fmt.Errorf("M6 managed account for direct SSH Host %s: %w", sshHostID, err)
	}
	sshAccountID, _ := stringField(sshAccount, "id")
	profile, err := client.JSON(ctx, "m6-session-profile", "enterprise", http.MethodPost, "/enterprise/session-profiles", http.StatusCreated,
		map[string]any{"name": "m6-required-recording", "description": "M6 E2E immutable session controls", "max_session_seconds": 3600,
			"idle_timeout_seconds": 600, "recording_mode": "required", "command_audit_mode": "required", "clipboard_mode": "disabled",
			"file_upload_mode": "disabled", "file_download_mode": "disabled", "port_forward_mode": "disabled", "session_share_mode": "disabled",
			"retention_days": 90, "status": "draft"}, enterpriseHeaders(env, "m6-session-profile"))
	if err != nil {
		return err
	}
	profileID, _ := stringField(profile, "id")
	if _, err := client.JSON(ctx, "m6-enable-session-profile", "enterprise", http.MethodPost,
		"/enterprise/session-profiles/"+profileID+"/enable?expected_version=1", http.StatusOK, nil, enterpriseHeaders(env, "m6-enable-session-profile")); err != nil {
		return err
	}
	rule, err := client.JSON(ctx, "m6-access-rule", "enterprise", http.MethodPost, "/enterprise/remote-access-rules", http.StatusCreated,
		map[string]any{"name": "m6-require-mfa", "description": "M6 E2E MFA rule", "priority": 100, "protocols": []string{"ssh", "winrs"},
			"actions": []string{"terminal"}, "source_cidrs": []string{}, "time_windows": []any{}, "effects": []string{"require_mfa"},
			"session_profile_id": profileID, "status": "draft"}, enterpriseHeaders(env, "m6-access-rule"))
	if err != nil {
		return err
	}
	ruleID, _ := stringField(rule, "id")
	validFrom := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	validUntil := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	grantIDs := map[string]string{}
	for _, grant := range []struct {
		name, host, account, protocol string
	}{
		{"m6-ssh-grant", sshHostID, sshAccountID, "ssh"},
		{"m6-winrs-grant", winrsHostID, winrsAccountID, "winrs"},
	} {
		created, err := client.JSON(ctx, grant.name, "enterprise", http.MethodPost, "/enterprise/remote-access-grants", http.StatusCreated,
			map[string]any{"subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "host_ids": []string{grant.host}, "managed_account_ids": []string{grant.account}, "protocols": []string{grant.protocol}, "actions": []string{"terminal"}, "valid_from": validFrom, "valid_until": validUntil, "status": "draft"}, enterpriseHeaders(env, grant.name))
		if err != nil {
			return err
		}
		grantID, _ := stringField(created, "id")
		if _, err := client.JSON(ctx, "m6-enable-"+grant.protocol+"-grant", "enterprise", http.MethodPost,
			"/enterprise/remote-access-grants/"+grantID+"/enable?expected_version=1", http.StatusOK, nil,
			enterpriseHeaders(env, "m6-enable-"+grant.protocol+"-grant")); err != nil {
			return err
		}
		grantIDs[grant.protocol] = grantID
	}
	if winrsHostID == "" || winrsAccountID == "" {
		return fmt.Errorf("M6 WinRS topology is incomplete")
	}
	baselineRequest, err := client.JSON(ctx, "m6-grant-without-rule-request", "enterprise", http.MethodPost,
		"/enterprise/remote-access-requests", http.StatusCreated,
		map[string]any{"host_id": sshHostID, "managed_account_id": sshAccountID, "protocol": "ssh", "action": "terminal", "reason": "M6 E2E grant baseline without rules"},
		enterpriseHeaders(env, "m6-grant-without-rule-request"))
	if err != nil {
		return err
	}
	if baselineRequest["status"] != "authorized" {
		return fmt.Errorf("M6 grant-only request status is %v, want authorized", baselineRequest["status"])
	}
	baselineRequestID, err := stringField(baselineRequest, "id")
	if err != nil {
		return err
	}
	if leaseID, leaseErr := a.findM6Lease(ctx, env, baselineRequestID); leaseErr != nil || leaseID == "" {
		return fmt.Errorf("M6 grant-only request did not issue a lease: %w", leaseErr)
	}
	if _, err := client.JSON(ctx, "m6-enable-access-rule", "enterprise", http.MethodPost,
		"/enterprise/remote-access-rules/"+ruleID+"/enable?expected_version=1", http.StatusOK, nil, enterpriseHeaders(env, "m6-enable-access-rule")); err != nil {
		return err
	}
	if err := a.verifyM6MFAResume(ctx, env, sshHostID, sshAccountID); err != nil {
		return err
	}
	if err := a.verifyM6ApprovalFlow(ctx, env, profileID, sshHostID, sshAccountID); err != nil {
		return err
	}
	if err := a.stepUpEnterprise(ctx, env); err != nil {
		return err
	}
	sshLeaseID, err := a.verifyM6RemoteSession(ctx, env, m6RemoteCase{name: "ssh", hostID: sshHostID, accountID: sshAccountID, protocol: "ssh", command: "echo argus-e2e-ok", expect: "argus-e2e-ok"})
	if err != nil {
		return err
	}
	if _, err := a.verifyM6RemoteSession(ctx, env, m6RemoteCase{name: "winrs", hostID: winrsHostID, accountID: winrsAccountID, protocol: "winrs", command: "whoami", expect: `argus\m6-e2e`}); err != nil {
		return err
	}
	if err := a.verifyM6TerminatedTicket(ctx, env, sshLeaseID); err != nil {
		return err
	}
	if err := a.verifyM6ObjectStoreFailClosed(ctx, env, sshLeaseID); err != nil {
		return err
	}
	if err := a.verifyM6CrossGatewayDrain(ctx, env); err != nil {
		return err
	}
	if err := a.verifyM6ControlPlaneRecovery(ctx, env); err != nil {
		return err
	}
	if err := a.runPlaywright(ctx, env, "e2e/m6-real.spec.ts", map[string]string{
		"ARGUS_M6_E2E": "1", "ARGUS_M6_ENTERPRISE_USERNAME": env.State.Values["enterprise_username"], "ARGUS_M6_ENTERPRISE_PASSWORD": env.State.Values["enterprise_password"],
		"ARGUS_M6_SSH_HOST_ID": sshHostID, "ARGUS_M6_WINRS_HOST_ID": winrsHostID,
	}); err != nil {
		return err
	}
	for protocol, grantID := range grantIDs {
		if _, err := client.JSON(ctx, "m6-disable-"+protocol+"-grant", "enterprise", http.MethodPost,
			"/enterprise/remote-access-grants/"+grantID+"/disable?expected_version=2", http.StatusOK, nil,
			enterpriseHeaders(env, "m6-disable-"+protocol)); err != nil {
			return err
		}
		if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
			return err
		}
	}
	return nil
}
