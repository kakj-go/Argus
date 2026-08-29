package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) runM8Scenario(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	secret, err := client.JSON(ctx, "m8-openbao-secret", "enterprise", http.MethodPost, "/enterprise/secrets", http.StatusCreated,
		map[string]any{"name": "m8-openbao-secret-" + env.Options.RunID, "type": "ssh_password", "description": "M8 OpenBao envelope validation", "value": "M8-openbao-secret-value"}, enterpriseHeaders(env, "m8-openbao-secret"))
	if err != nil {
		return err
	}
	secretID, _ := stringField(secret, "id")
	credential, err := client.JSON(ctx, "m8-openbao-credential", "enterprise", http.MethodPost, "/enterprise/credentials", http.StatusCreated,
		map[string]any{"name": "m8-openbao-credential-" + env.Options.RunID, "protocol": "ssh", "username": "argus", "secret_id": secretID}, enterpriseHeaders(env, "m8-openbao-credential"))
	if err != nil {
		return err
	}
	credentialID, _ := stringField(credential, "id")
	breakGlass, err := client.JSON(ctx, "m8-break-glass", "enterprise", http.MethodPost, "/enterprise/break-glass-sessions", http.StatusCreated,
		map[string]any{"reason": "local recovery validation", "ticket_ref": "M8-LOCAL-001"}, enterpriseHeaders(env, "m8-break-glass"))
	if err != nil {
		return err
	}
	breakGlassID, _ := stringField(breakGlass, "id")
	if _, err := client.JSON(ctx, "m8-break-glass-revoke", "enterprise", http.MethodPost, "/enterprise/break-glass-sessions/"+breakGlassID+"/revoke", http.StatusNoContent, nil, enterpriseHeaders(env, "")); err != nil {
		return err
	}
	env.State.Values["m8_secret_id"] = secretID
	env.State.Values["m8_credential_id"] = credentialID

	if err := a.injectM8Failures(ctx, env); err != nil {
		return err
	}
	if err := a.invokeArgusctl(ctx, env, "verify", "--config", env.ConfigPath, "--artifacts", filepath.Join(env.Options.Artifacts, "post-failure")); err != nil {
		return err
	}
	backupOutput, err := a.runner.Output(ctx, nil, env.Argusctl, "backup", "create", "--config", env.ConfigPath, "--artifacts", filepath.Join(env.Options.Artifacts, "backups"))
	if err != nil {
		return err
	}
	backup, keyFile := outputValue(backupOutput, "backup"), outputValue(backupOutput, "key_file")
	if backup == "" || keyFile == "" {
		return fmt.Errorf("argusctl backup create did not return backup and key_file")
	}
	if err := writePrivate(filepath.Join(env.Options.Artifacts, "backup.log"), []byte(backupOutput+"\n")); err != nil {
		return err
	}
	if err := a.invokeArgusctl(ctx, env, "backup", "verify", "--config", env.ConfigPath, "--backup", backup, "--key-file", keyFile); err != nil {
		return err
	}

	a.stopE2ELocalProcesses(env)
	if env.fixtureReady {
		if err := a.cleanupE2EFixtures(ctx, env); err != nil {
			return err
		}
		env.fixtureAttempted = false
		env.fixtureReady = false
	}
	if err := a.invokeArgusctl(ctx, env, "uninstall", "--config", env.ConfigPath, "--delete-data", "--delete-owned-crds", "--yes"); err != nil {
		return err
	}
	env.installed = false
	env.installAttempted = false

	env.ReleaseID = releaseIDForDev("m8r-" + env.Options.RunID)
	env.SystemNS = kubernetesNameForDev(env.ReleaseID + "-system")
	env.SandboxNS = kubernetesNameForDev(env.ReleaseID + "-sandbox")
	env.ObservNS = kubernetesNameForDev(env.ReleaseID + "-observability")
	restoreConfig, err := a.writeE2EConfig(env, "local-hardening")
	if err != nil {
		return err
	}
	env.ConfigPath = restoreConfig
	env.imagesAttempted = true
	if err := a.prepareE2EImages(ctx, env); err != nil {
		return err
	}
	if err := a.invokeArgusctl(ctx, env, "restore", "plan", "--config", restoreConfig, "--backup", backup, "--key-file", keyFile); err != nil {
		return err
	}
	env.installAttempted = true
	if err := a.invokeArgusctl(ctx, env, "restore", "apply", "--config", restoreConfig, "--backup", backup, "--key-file", keyFile); err != nil {
		return err
	}
	env.installed = true
	if err := a.invokeArgusctl(ctx, env, "restore", "verify", "--config", restoreConfig, "--backup", backup, "--key-file", keyFile); err != nil {
		return err
	}
	if err := a.invokeArgusctl(ctx, env, "verify", "--config", restoreConfig, "--artifacts", filepath.Join(env.Options.Artifacts, "restored")); err != nil {
		return err
	}
	if err := a.resolveE2EAccess(ctx, env); err != nil {
		return err
	}
	env.State.HTTP = NewDomainScenarioHTTP(env)
	return a.verifyM8RestoredIdentity(ctx, env)
}

func (a *App) injectM8Failures(ctx context.Context, env *E2EEnvironment) error {
	redisPassword, err := dataCredentialValue(ctx, env, "redis-password")
	if err != nil {
		return err
	}
	if _, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-redis", "redis", "redis-cli", "-a", redisPassword, "FLUSHALL"); err != nil {
		return err
	}
	for _, selector := range []string{"app.kubernetes.io/name=argus-server", "app.kubernetes.io/name=argus-worker-action", "app.kubernetes.io/name=argus-connector-gateway"} {
		if err := env.Kube.DeletePods(ctx, env.SystemNS, selector); err != nil {
			return err
		}
	}
	for _, selector := range []string{"app.kubernetes.io/name=argus-telemetry-writer", "app.kubernetes.io/name=argus-telemetry-query"} {
		if err := env.Kube.DeletePods(ctx, env.ObservNS, selector); err != nil {
			return err
		}
	}
	if err := env.Kube.ScaleStatefulSet(ctx, env.SystemNS, "argus-openbao", 0); err != nil {
		return err
	}
	if err := env.Kube.WaitStatefulSet(ctx, env.SystemNS, "argus-openbao", 0, 5*time.Minute); err != nil {
		return err
	}
	if err := env.Kube.ScaleStatefulSet(ctx, env.SystemNS, "argus-openbao", 1); err != nil {
		return err
	}
	if err := env.Kube.WaitStatefulSet(ctx, env.SystemNS, "argus-openbao", 1, 5*time.Minute); err != nil {
		return err
	}
	return env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-server", 5*time.Minute)
}

func (a *App) verifyM8RestoredIdentity(ctx context.Context, env *E2EEnvironment) error {
	client, _ := scenarioHTTP(env)
	for _, audience := range []string{"platform", "enterprise"} {
		origin := env.PlatformOrigin()
		if audience == "enterprise" {
			origin = env.EnterpriseOrigin()
		}
		login, err := client.JSON(ctx, "restored-"+audience+"-login", audience, http.MethodPost, "/"+audience+"/auth/login", http.StatusOK,
			map[string]any{"username": env.State.Values[audience+"_username"], "password": env.State.Values[audience+"_password"]}, map[string]string{"Origin": origin})
		if err != nil {
			return err
		}
		challenge, err := stringField(login, "mfa_challenge", "challenge_id")
		if err != nil {
			return err
		}
		codes := strings.Split(env.State.Values[audience+"_recovery_codes"], ",")
		index := 0
		if audience == "platform" {
			index = 1
		}
		if len(codes) <= index || codes[index] == "" {
			return fmt.Errorf("%s recovery code is unavailable", audience)
		}
		if _, err := client.JSON(ctx, "restored-"+audience+"-mfa", audience, http.MethodPost, "/"+audience+"/auth/mfa/complete", http.StatusOK,
			map[string]any{"challenge_id": challenge, "code": codes[index]}, map[string]string{"Origin": origin}); err != nil {
			return err
		}
	}
	secrets, err := client.JSON(ctx, "restored-openbao-secret", "enterprise", http.MethodGet, "/enterprise/secrets", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	if _, err := findItem(objectItems(secrets), func(item map[string]any) bool {
		return item["id"] == env.State.Values["m8_secret_id"] && item["type"] == "ssh_password"
	}); err != nil {
		return fmt.Errorf("restored Secret: %w", err)
	}
	credentials, err := client.JSON(ctx, "restored-openbao-credential", "enterprise", http.MethodGet, "/enterprise/credentials", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	if _, err := findItem(objectItems(credentials), func(item map[string]any) bool {
		return item["id"] == env.State.Values["m8_credential_id"] && item["secret_id"] == env.State.Values["m8_secret_id"]
	}); err != nil {
		return fmt.Errorf("restored Credential: %w", err)
	}
	return nil
}

func outputValue(output, key string) string {
	prefix := key + "="
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func (a *App) stopE2ELocalProcesses(env *E2EEnvironment) {
	for index := len(env.Processes) - 1; index >= 0; index-- {
		_ = env.Processes[index].Stop(5 * time.Second)
	}
	env.Processes = nil
}
