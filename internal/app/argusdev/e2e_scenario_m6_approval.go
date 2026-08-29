package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type m6Approver struct {
	clientName string
	userID     string
	csrf       string
	roleID     string
}

func (a *App) verifyM6MFAResume(ctx context.Context, env *E2EEnvironment, hostID, accountID string) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	if _, err := a.postgresQuery(ctx, env, "UPDATE sessions SET step_up_expires_at=NULL, amr=ARRAY['password']::text[] WHERE audience='enterprise' AND user_id='"+env.State.Values["admin_user_id"]+"' AND revoked_at IS NULL;"); err != nil {
		return fmt.Errorf("clear M6 step-up state: %w", err)
	}
	request, err := client.JSON(ctx, "m6-awaiting-mfa-request", "enterprise", http.MethodPost,
		"/enterprise/remote-access-requests", http.StatusCreated,
		map[string]any{"host_id": hostID, "managed_account_id": accountID, "protocol": "ssh", "action": "terminal", "reason": "M6 E2E MFA resume"},
		enterpriseHeaders(env, "m6-awaiting-mfa-request"))
	if err != nil {
		return err
	}
	if request["status"] != "awaiting_mfa" {
		return fmt.Errorf("M6 MFA request status is %v, want awaiting_mfa", request["status"])
	}
	requestID, err := stringField(request, "id")
	if err != nil {
		return err
	}
	if err := a.stepUpEnterprise(ctx, env); err != nil {
		return err
	}
	resumed, err := client.JSON(ctx, "m6-resume-after-mfa", "enterprise", http.MethodPost,
		"/enterprise/remote-access-requests/"+requestID+"/resume", http.StatusOK, nil,
		enterpriseHeaders(env, "m6-resume-after-mfa"))
	if err != nil {
		return err
	}
	if resumed["status"] != "authorized" {
		return fmt.Errorf("M6 resumed request status is %v, want authorized", resumed["status"])
	}
	return nil
}

func (a *App) verifyM6ApprovalFlow(ctx context.Context, env *E2EEnvironment, profileID, hostID, accountID string) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	admin, err := client.JSON(ctx, "m6-admin-user", "enterprise", http.MethodGet,
		"/enterprise/users/"+env.State.Values["admin_user_id"], http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	departmentID, err := stringField(admin, "department_id")
	if err != nil {
		return err
	}
	approvers := make([]m6Approver, 0, 2)
	for index := 1; index <= 2; index++ {
		approver, createErr := a.createM6Approver(ctx, env, departmentID, index)
		if createErr != nil {
			return createErr
		}
		approvers = append(approvers, approver)
	}
	roleIDs := []string{approvers[0].roleID, approvers[1].roleID}
	workflow, err := client.JSON(ctx, "m6-approval-workflow", "enterprise", http.MethodPost, "/enterprise/approval-workflows", http.StatusCreated,
		map[string]any{"name": "m6-two-person-approval", "description": "M6 E2E two-person approval", "approver_role_ids": roleIDs,
			"minimum_approvals": 2, "separation_of_duties": true, "approval_timeout_seconds": 600, "escalation_after_seconds": 300,
			"timeout_effect": "reject", "escalation_role_ids": []string{}, "status": "draft"}, enterpriseHeaders(env, "m6-approval-workflow"))
	if err != nil {
		return err
	}
	workflowID, _ := stringField(workflow, "id")
	if _, err := client.JSON(ctx, "m6-enable-approval-workflow", "enterprise", http.MethodPost,
		"/enterprise/approval-workflows/"+workflowID+"/enable?expected_version=1", http.StatusOK, nil, enterpriseHeaders(env, "m6-enable-approval-workflow")); err != nil {
		return err
	}
	rule, err := client.JSON(ctx, "m6-approval-rule", "enterprise", http.MethodPost, "/enterprise/remote-access-rules", http.StatusCreated,
		map[string]any{"name": "m6-temporary-two-person-rule", "description": "M6 E2E approval rule", "priority": 10, "protocols": []string{"ssh"},
			"actions": []string{"terminal"}, "source_cidrs": []string{}, "time_windows": []any{}, "effects": []string{"require_approval"},
			"approval_workflow_id": workflowID, "session_profile_id": profileID, "status": "draft"}, enterpriseHeaders(env, "m6-approval-rule"))
	if err != nil {
		return err
	}
	ruleID, _ := stringField(rule, "id")
	if _, err := client.JSON(ctx, "m6-enable-approval-rule", "enterprise", http.MethodPost,
		"/enterprise/remote-access-rules/"+ruleID+"/enable?expected_version=1", http.StatusOK, nil, enterpriseHeaders(env, "m6-enable-approval-rule")); err != nil {
		return err
	}
	request, err := client.JSON(ctx, "m6-approval-request", "enterprise", http.MethodPost, "/enterprise/remote-access-requests", http.StatusCreated,
		map[string]any{"host_id": hostID, "managed_account_id": accountID, "protocol": "ssh", "action": "terminal", "reason": "M6 E2E two-person approval"}, enterpriseHeaders(env, "m6-approval-request"))
	if err != nil {
		return err
	}
	if request["status"] != "awaiting_approval" {
		return fmt.Errorf("M6 approval request status is %v", request["status"])
	}
	requestID, _ := stringField(request, "id")
	requirements, _ := request["requirements"].([]any)
	if len(requirements) != 1 {
		return fmt.Errorf("M6 approval request returned %d requirements", len(requirements))
	}
	requirement, _ := requirements[0].(map[string]any)
	requirementID, err := stringField(requirement, "id")
	if err != nil {
		return err
	}
	decisionBody := map[string]any{"requirement_id": requirementID, "decision": "approve", "comment": "M6 E2E approval"}
	if _, err := client.JSON(ctx, "m6-self-approval-denied", "enterprise", http.MethodPost,
		"/enterprise/remote-access-requests/"+requestID+"/decisions", http.StatusForbidden, decisionBody, enterpriseHeaders(env, "m6-self-approval-denied")); err != nil {
		return err
	}
	for index, approver := range approvers {
		decided, decideErr := client.JSON(ctx, fmt.Sprintf("m6-approval-%d", index+1), approver.clientName, http.MethodPost,
			"/enterprise/remote-access-requests/"+requestID+"/decisions", http.StatusOK, decisionBody,
			map[string]string{"Origin": env.EnterpriseOrigin(), "X-CSRF-Token": approver.csrf, "Idempotency-Key": fmt.Sprintf("m6-approval-%d-%s", index+1, env.Options.RunID)})
		if decideErr != nil {
			return decideErr
		}
		expected := "awaiting_approval"
		if index == len(approvers)-1 {
			expected = "authorized"
		}
		if decided["status"] != expected {
			return fmt.Errorf("M6 approval %d request status is %v", index+1, decided["status"])
		}
	}
	leaseID, err := a.findM6Lease(ctx, env, requestID)
	if err != nil || leaseID == "" {
		return fmt.Errorf("M6 approved request did not issue a lease: %w", err)
	}
	if _, err := client.JSON(ctx, "m6-disable-approval-rule", "enterprise", http.MethodPost,
		"/enterprise/remote-access-rules/"+ruleID+"/disable?expected_version=2", http.StatusOK, nil, enterpriseHeaders(env, "m6-disable-approval-rule")); err != nil {
		return err
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	if err := a.waitForM6LeaseRevocation(ctx, env, leaseID); err != nil {
		return err
	}
	return nil
}

func (a *App) createM6Approver(ctx context.Context, env *E2EEnvironment, departmentID string, index int) (m6Approver, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return m6Approver{}, err
	}
	role, err := client.JSON(ctx, fmt.Sprintf("m6-approver-role-%d", index), "enterprise", http.MethodPost, "/enterprise/roles", http.StatusCreated,
		map[string]any{"name": fmt.Sprintf("M6 Approver %d", index), "description": "M6 E2E remote access approver", "permissions": []string{"remote_access.session.approve"}},
		enterpriseHeaders(env, fmt.Sprintf("m6-approver-role-%d", index)))
	if err != nil {
		return m6Approver{}, err
	}
	roleID, _ := stringField(role, "id")
	username := fmt.Sprintf("m6-approver-%d", index)
	created, err := client.JSON(ctx, fmt.Sprintf("m6-approver-user-%d", index), "enterprise", http.MethodPost, "/enterprise/users", http.StatusCreated,
		map[string]any{"username": username, "display_name": fmt.Sprintf("M6 Approver %d", index), "department_id": departmentID, "role_ids": []string{roleID}},
		enterpriseHeaders(env, fmt.Sprintf("m6-approver-user-%d", index)))
	if err != nil {
		return m6Approver{}, err
	}
	userID, _ := stringField(created, "user", "id")
	temporaryPassword, _ := stringField(created, "temporary_password")
	clientName := fmt.Sprintf("m6-approver-%d", index)
	login, err := client.JSON(ctx, clientName+"-login", clientName, http.MethodPost, "/enterprise/auth/login", http.StatusOK,
		map[string]any{"username": username, "password": temporaryPassword}, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return m6Approver{}, err
	}
	challengeID, _ := stringField(login, "password_change_challenge", "challenge_id")
	password := fmt.Sprintf("M6!Approver%d#R8kW3pQ2x", index)
	completed, err := client.JSON(ctx, clientName+"-password-change", clientName, http.MethodPost, "/enterprise/auth/complete-password-change", http.StatusOK,
		map[string]any{"challenge_id": challengeID, "temporary_password": temporaryPassword, "new_password": password}, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return m6Approver{}, err
	}
	csrf, err := stringField(completed, "csrf_token")
	if err != nil {
		return m6Approver{}, err
	}
	return m6Approver{clientName: clientName, userID: userID, csrf: csrf, roleID: roleID}, nil
}

func (a *App) waitForM6LeaseRevocation(ctx context.Context, env *E2EEnvironment, leaseID string) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		leases, listErr := client.JSON(ctx, "m6-revoked-lease-"+leaseID, "enterprise", http.MethodGet,
			"/enterprise/remote-access-leases", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
		if listErr != nil {
			return listErr
		}
		for _, lease := range objectItems(leases) {
			if lease["id"] == leaseID && lease["revoked"] == true {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("M6 lease %s was not revoked after disabling its approval rule", leaseID)
}
