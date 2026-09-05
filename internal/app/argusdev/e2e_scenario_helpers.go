package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func scenarioHTTP(env *E2EEnvironment) (*ScenarioHTTP, error) {
	if env.State == nil || env.State.HTTP == nil {
		return nil, fmt.Errorf("M2 scenario HTTP state is unavailable")
	}
	return env.State.HTTP, nil
}

func enterpriseHeaders(env *E2EEnvironment, idempotency string) map[string]string {
	headers := map[string]string{"Origin": env.EnterpriseOrigin(), "X-CSRF-Token": env.State.Values["enterprise_csrf"]}
	if idempotency != "" {
		headers["Idempotency-Key"] = idempotency + "-" + env.Options.RunID
	}
	return headers
}

func (a *App) refreshEnterpriseLogin(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	client.Reset("enterprise")
	login, err := client.JSON(ctx, "enterprise-login-refresh", "enterprise", http.MethodPost, "/enterprise/auth/login", http.StatusOK, map[string]any{
		"username": env.State.Values["enterprise_username"], "password": env.State.Values["enterprise_password"],
	}, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	challenge, err := stringField(login, "mfa_challenge", "challenge_id")
	if err != nil {
		return err
	}
	code, err := waitForNextTOTP(env.State.Values["enterprise_mfa_secret"], env.State.Values["enterprise_mfa_last"])
	if err != nil {
		return err
	}
	completed, err := client.JSON(ctx, "enterprise-mfa-refresh", "enterprise", http.MethodPost, "/enterprise/auth/mfa/complete", http.StatusOK,
		map[string]any{"challenge_id": challenge, "code": code}, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	csrf, err := stringField(completed, "csrf_token")
	if err != nil {
		return err
	}
	env.State.Values["enterprise_csrf"] = csrf
	env.State.Values["enterprise_mfa_last"] = code
	return nil
}

func (a *App) stepUpEnterprise(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	code, err := waitForNextTOTP(env.State.Values["enterprise_mfa_secret"], env.State.Values["enterprise_mfa_last"])
	if err != nil {
		return err
	}
	if _, err := client.JSON(ctx, "enterprise-step-up-refresh", "enterprise", http.MethodPost, "/enterprise/auth/step-up", http.StatusOK,
		map[string]any{"code": code}, enterpriseHeaders(env, "")); err != nil {
		return err
	}
	env.State.Values["enterprise_mfa_last"] = code
	return nil
}

func objectItems(value map[string]any) []map[string]any {
	items, _ := value["items"].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func findItem(items []map[string]any, predicate func(map[string]any) bool) (map[string]any, error) {
	for _, item := range items {
		if predicate(item) {
			return item, nil
		}
	}
	return nil, fmt.Errorf("expected item was not found")
}

func (a *App) waitConnectionTest(ctx context.Context, env *E2EEnvironment, id string) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		result, err := client.JSON(ctx, "connection-test-"+id, "enterprise", http.MethodGet, "/enterprise/connection-tests/"+id, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
		if err != nil {
			return err
		}
		status, _ := result["status"].(string)
		switch status {
		case "succeeded":
			return nil
		case "failed", "cancelled", "expired":
			return fmt.Errorf("connection test %s ended as %s", id, status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("connection test %s timed out", id)
}

func (a *App) postgresQuery(ctx context.Context, env *E2EEnvironment, query string) (string, error) {
	password := env.State.Values["postgres_password"]
	if password == "" {
		value, err := dataCredentialValue(ctx, env, "postgresql-password")
		if err != nil {
			return "", err
		}
		password = value
		env.State.Values["postgres_password"] = value
	}
	return env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-postgresql", "postgresql",
		"env", "PGPASSWORD="+password, "psql", "-v", "ON_ERROR_STOP=1", "-At", "-U", "argus", "-d", "argus", "-c", query)
}

func dataCredentialValue(ctx context.Context, env *E2EEnvironment, key string) (string, error) {
	return env.Kube.SecretValue(ctx, env.SystemNS, "argus-data-credentials", key)
}

func (a *App) confirmPendingAction(ctx context.Context, env *E2EEnvironment, name, actionRef string) (map[string]any, error) {
	client, err := scenarioHTTP(env)
	if err != nil {
		return nil, err
	}
	confirmed, err := client.JSON(ctx, name, "enterprise", http.MethodPost, "/enterprise/pending-actions/"+actionRef+"/confirm", http.StatusOK, nil, enterpriseHeaders(env, name))
	if err != nil {
		return nil, err
	}
	executionID, err := stringField(confirmed, "execution", "execution_id")
	if err != nil {
		if status, _ := nestedString(confirmed, "pending_action", "status"); status == "succeeded" {
			return confirmed, nil
		}
		return nil, err
	}
	if status, _ := nestedString(confirmed, "execution", "status"); status != "succeeded" {
		deadline := time.Now().Add(5 * time.Minute)
		succeeded := false
		for time.Now().Before(deadline) {
			outcome, queryErr := a.postgresQuery(ctx, env, "SELECT status || '|' || coalesce(error_code,'') FROM executions WHERE id='"+executionID+"';")
			if queryErr != nil {
				return nil, queryErr
			}
			status, errorCode, _ := strings.Cut(strings.TrimSpace(outcome), "|")
			if status == "succeeded" {
				succeeded = true
				break
			}
			if status == "failed" || status == "cancelled" {
				return nil, fmt.Errorf("execution %s ended as %s (%s)", executionID, status, errorCode)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		if !succeeded {
			return nil, fmt.Errorf("execution %s timed out", executionID)
		}
	}
	resourceID, err := a.postgresQuery(ctx, env, "SELECT coalesce(result_resource_id::text,'') FROM pending_actions WHERE action_ref='"+actionRef+"';")
	if err != nil {
		return nil, err
	}
	result := map[string]any{"pending_action": map[string]any{"status": "succeeded"}, "execution": map[string]any{"execution_id": executionID}}
	if strings.TrimSpace(resourceID) != "" {
		result["resource_ref"] = map[string]any{"resource_id": strings.TrimSpace(resourceID)}
	}
	count, err := a.postgresQuery(ctx, env, "SELECT count(*) FROM execution_one_time_results WHERE execution_id='"+executionID+"';")
	if err == nil && strings.TrimSpace(count) == "1" {
		if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
			return nil, err
		}
		claimed, claimErr := client.JSON(ctx, name+"-one-time-result", "enterprise", http.MethodPost, "/enterprise/executions/"+executionID+"/one-time-result", http.StatusOK, nil, enterpriseHeaders(env, name+"-one-time-result"))
		if claimErr != nil {
			return nil, claimErr
		}
		result["one_time_result"] = claimed
	}
	return result, nil
}

func nestedString(value map[string]any, path ...string) (string, bool) {
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current = object[key]
	}
	result, ok := current.(string)
	return result, ok
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func (a *App) waitPostgresValue(ctx context.Context, env *E2EEnvironment, query, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		value, err := a.postgresQuery(ctx, env, query)
		if err != nil {
			return err
		}
		last = strings.TrimSpace(value)
		if last == expected {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for %q; last value was %q", expected, last)
}

func (a *App) waitExecutionForAction(ctx context.Context, env *E2EEnvironment, actionRef string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value, err := a.postgresQuery(ctx, env, "SELECT execution.id::text || '|' || execution.status || '|' || COALESCE(execution.error_code, '') || '|' || COALESCE(action.error_code, '') FROM executions execution JOIN pending_actions action ON action.id=execution.pending_action_id WHERE action.action_ref='"+actionRef+"' ORDER BY execution.created_at DESC LIMIT 1;")
		if err != nil {
			return "", err
		}
		parts := strings.SplitN(strings.TrimSpace(value), "|", 4)
		if len(parts) == 4 {
			switch parts[1] {
			case "succeeded":
				return parts[0], nil
			case "failed", "cancelled":
				return "", fmt.Errorf("execution %s ended as %s (execution_error=%s action_error=%s)", parts[0], parts[1], parts[2], parts[3])
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", fmt.Errorf("execution for action %s timed out", actionRef)
}

func (a *App) waitRunStatus(ctx context.Context, env *E2EEnvironment, runID, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := a.postgresQuery(ctx, env, "SELECT status FROM runs WHERE id='"+runID+"';")
		if err != nil {
			return err
		}
		current := strings.TrimSpace(status)
		if current == expected {
			return nil
		}
		if current == "failed" || current == "cancelled" {
			return fmt.Errorf("run %s ended as %s", runID, current)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("run %s did not reach %s", runID, expected)
}

func bytesContainFold(data, needle []byte) bool {
	return strings.Contains(strings.ToLower(string(data)), strings.ToLower(string(needle)))
}
