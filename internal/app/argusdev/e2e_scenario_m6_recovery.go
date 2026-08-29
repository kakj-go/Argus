package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (a *App) verifyM6ControlPlaneRecovery(ctx context.Context, env *E2EEnvironment) error {
	before, err := a.m6RuntimeFactCounts(ctx, env)
	if err != nil {
		return err
	}

	for _, deployment := range []string{"argus-server", "argus-worker"} {
		if err := env.Kube.ScaleDeployment(ctx, env.SystemNS, deployment, 2); err != nil {
			return fmt.Errorf("scale %s for M6 recovery: %w", deployment, err)
		}
		if err := env.Kube.WaitDeployment(ctx, env.SystemNS, deployment, 5*time.Minute); err != nil {
			return err
		}
	}

	if err := env.Kube.DeletePods(ctx, env.SystemNS, "app.kubernetes.io/name=argus-worker"); err != nil {
		return fmt.Errorf("restart M6 workers: %w", err)
	}
	if err := env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-worker", 5*time.Minute); err != nil {
		return err
	}

	if err := env.Kube.DeletePods(ctx, env.SystemNS, "app.kubernetes.io/name=argus-postgresql"); err != nil {
		return fmt.Errorf("restart M6 PostgreSQL: %w", err)
	}
	if err := env.Kube.WaitStatefulSet(ctx, env.SystemNS, "argus-postgresql", 1, 5*time.Minute); err != nil {
		return err
	}
	for _, deployment := range []string{"argus-server", "argus-worker"} {
		if err := env.Kube.WaitDeployment(ctx, env.SystemNS, deployment, 5*time.Minute); err != nil {
			return err
		}
	}

	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return fmt.Errorf("refresh M6 login after PostgreSQL recovery: %w", err)
	}
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	for name, path := range map[string]string{
		"grants":     "/enterprise/remote-access-grants",
		"requests":   "/enterprise/remote-access-requests?scope=mine",
		"leases":     "/enterprise/remote-access-leases",
		"sessions":   "/enterprise/remote-access-sessions",
		"recordings": "/enterprise/remote-access-recordings",
	} {
		if _, err := client.JSON(ctx, "m6-recovery-"+name, "enterprise", http.MethodGet, path, http.StatusOK, nil,
			map[string]string{"Origin": env.EnterpriseOrigin()}); err != nil {
			return err
		}
	}

	after, err := a.m6RuntimeFactCounts(ctx, env)
	if err != nil {
		return err
	}
	for table, expected := range before {
		if after[table] != expected {
			return fmt.Errorf("M6 %s facts changed across Worker/PostgreSQL recovery: before=%s after=%s", table, expected, after[table])
		}
	}
	return nil
}

func (a *App) m6RuntimeFactCounts(ctx context.Context, env *E2EEnvironment) (map[string]string, error) {
	result := map[string]string{}
	for _, table := range []string{
		"remote_access_grants",
		"remote_access_requests",
		"remote_access_leases",
		"remote_access_sessions",
		"remote_access_recordings",
	} {
		value, err := a.postgresQuery(ctx, env, "SELECT count(*) FROM "+table+" WHERE enterprise_id='"+env.State.Values["enterprise_id"]+"';")
		if err != nil {
			return nil, fmt.Errorf("count M6 %s facts: %w", table, err)
		}
		result[table] = strings.TrimSpace(value)
	}
	return result, nil
}
