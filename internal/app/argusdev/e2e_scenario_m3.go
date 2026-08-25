package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

func (a *App) runM3Scenario(ctx context.Context, env *E2EEnvironment) error {
	client, err := scenarioHTTP(env)
	if err != nil {
		return err
	}
	secret, err := client.JSON(ctx, "m3-secret-create", "enterprise", http.MethodPost, "/enterprise/secrets", http.StatusCreated,
		map[string]any{"name": "m3-ssh-password", "type": "ssh_password", "description": "M3 E2E write-only secret", "value": "M3-e2e-ssh-password"}, enterpriseHeaders(env, "m3-secret"))
	if err != nil {
		return err
	}
	if _, exposed := secret["value"]; exposed {
		return fmt.Errorf("M3 Secret response exposed its value")
	}
	secretID, err := stringField(secret, "id")
	if err != nil {
		return err
	}
	credential, err := client.JSON(ctx, "m3-credential-create", "enterprise", http.MethodPost, "/enterprise/credentials", http.StatusCreated,
		map[string]any{"name": "m3-ssh", "protocol": "ssh", "username": "argus", "secret_id": secretID}, enterpriseHeaders(env, "m3-credential"))
	if err != nil {
		return err
	}
	credentialID, err := stringField(credential, "id")
	if err != nil {
		return err
	}
	scope, err := client.JSON(ctx, "m3-admin-scope", "enterprise", http.MethodPost, "/enterprise/data-scopes", http.StatusCreated,
		map[string]any{"name": "M3 administration scope", "resource_types": []string{"host", "kubernetes_cluster", "kubernetes_namespace"}, "explicit_resource_ids": []string{}, "label_selector": map[string]any{"schema_version": "argus.label_selector/v1", "requirements": []any{map[string]any{"key": "team", "operator": "eq", "values": []string{"m3"}}}}}, enterpriseHeaders(env, "m3-admin-scope"))
	if err != nil {
		return err
	}
	scopeID, err := stringField(scope, "id")
	if err != nil {
		return err
	}
	roles, err := client.JSON(ctx, "m3-roles", "enterprise", http.MethodGet, "/enterprise/roles", http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	role, err := findItem(objectItems(roles), func(item map[string]any) bool {
		return item["builtin_key"] == "resource_admin" && item["builtin"] == true
	})
	if err != nil {
		return err
	}
	roleID, err := stringField(role, "id")
	if err != nil {
		return err
	}
	env.State.Values["m3_resource_admin_role_id"] = roleID
	if _, err := client.JSON(ctx, "m3-admin-binding", "enterprise", http.MethodPost, "/enterprise/role-bindings", http.StatusCreated,
		map[string]any{"subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "role_id": roleID, "data_scope_ids": []string{scopeID}}, enterpriseHeaders(env, "m3-admin-binding")); err != nil {
		return err
	}
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}

	test, err := client.JSON(ctx, "m3-direct-host-test", "enterprise", http.MethodPost, "/enterprise/hosts/connection-tests", http.StatusAccepted,
		map[string]any{"address": "10.255.255.1", "port": 2222, "platform": "linux", "connection_mode": "direct_ssh", "credential_id": credentialID, "username": "argus"}, enterpriseHeaders(env, "m3-direct-test"))
	if err != nil {
		return err
	}
	testID, err := stringField(test, "id")
	if err != nil {
		return err
	}
	if err := a.waitConnectionTest(ctx, env, testID); err != nil {
		return err
	}
	preview, err := client.JSON(ctx, "m3-direct-host-preview", "enterprise", http.MethodPost, "/enterprise/hosts/actions/preview-create", http.StatusCreated,
		map[string]any{"name": "m3-direct-host", "address": "10.255.255.1", "port": 2222, "platform": "linux", "connection_mode": "direct_ssh", "credential_id": credentialID, "username": "argus", "environment": "production", "labels": map[string]string{"team": "m3", "route": "direct"}, "connection_test_id": testID}, enterpriseHeaders(env, "m3-direct-host"))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	confirmed, err := a.confirmPendingAction(ctx, env, "m3-direct-host-confirm", actionRef)
	if err != nil {
		return err
	}
	hostID, err := stringField(confirmed, "resource_ref", "resource_id")
	if err != nil {
		return err
	}
	env.State.Values["m3_direct_host_id"] = hostID
	env.State.Values["m3_credential_id"] = credentialID
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}

	clusterID, err := a.createM3InCluster(ctx, env)
	if err != nil {
		return err
	}
	env.State.Values["m3_cluster_id"] = clusterID
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	if suiteHas(env.Options.Suite, "m7") || env.Options.Suite == "m10-query" {
		if err := a.startM3KubernetesConnector(ctx, env); err != nil {
			return err
		}
	}
	if _, err := a.createM3Bastion(ctx, env, "m3-bastion-2", "migration-target"); err != nil {
		return err
	}
	preserve := suiteHas(env.Options.Suite, "m7") || suiteHas(env.Options.Suite, "m6")
	if preserve {
		id, err := a.createM3Bastion(ctx, env, "m3-bastion", "bastion")
		if err != nil {
			return err
		}
		env.State.Values["m3_bastion_scope_id"] = id
		if err := a.startM3BastionConnector(ctx, env); err != nil {
			return err
		}
		clusterID, err := a.createM3BastionKubernetes(ctx, env)
		if err != nil {
			return err
		}
		env.State.Values["m3_bastion_cluster_id"] = clusterID
	}
	if err := a.runPlaywright(ctx, env, "e2e/m3-real.spec.ts", map[string]string{
		"ARGUS_M3_E2E": "1", "ARGUS_M3_PRESERVE_BASTION": boolString(preserve),
		"ARGUS_M3_ENTERPRISE_USERNAME": env.State.Values["enterprise_username"], "ARGUS_M3_ENTERPRISE_PASSWORD": env.State.Values["enterprise_password"],
	}); err != nil {
		return err
	}
	// The browser flow creates another in-cluster resource and advances the
	// user's authorization version. Refresh the harness session before the next
	// suite dependency reuses its cookie jar.
	return a.refreshEnterpriseLogin(ctx, env)
}

func (a *App) createM3InCluster(ctx context.Context, env *E2EEnvironment) (string, error) {
	client, _ := scenarioHTTP(env)
	preview, err := client.JSON(ctx, "m3-in-cluster-preview", "enterprise", http.MethodPost, "/enterprise/kubernetes-clusters/actions/preview-create", http.StatusCreated,
		map[string]any{"name": "m3-in-cluster", "api_server": "https://kubernetes.default.svc", "connection_mode": "in_cluster", "default_namespace": "default", "environment": "production", "labels": map[string]string{"team": "m3", "route": "in-cluster"}}, enterpriseHeaders(env, "m3-in-cluster"))
	if err != nil {
		return "", err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return "", err
	}
	result, err := a.confirmPendingAction(ctx, env, "m3-in-cluster-confirm", actionRef)
	if err != nil {
		return "", err
	}
	if enrollment, ok := result["enrollment"].(map[string]any); ok {
		if command, _ := enrollment["install_command"].(string); command != "" {
			env.State.Values["m3_in_cluster_install_command"] = command
		}
	}
	return stringField(result, "resource_ref", "resource_id")
}

func (a *App) createM3Bastion(ctx context.Context, env *E2EEnvironment, name, route string) (string, error) {
	client, _ := scenarioHTTP(env)
	preview, err := client.JSON(ctx, "m3-"+name+"-preview", "enterprise", http.MethodPost, "/enterprise/bastion-scopes/actions/preview-create", http.StatusCreated,
		map[string]any{"name": name, "environment": "production", "labels": map[string]string{"team": "m3", "route": route}}, enterpriseHeaders(env, "m3-"+name))
	if err != nil {
		return "", err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return "", err
	}
	result, err := a.confirmPendingAction(ctx, env, "m3-"+name+"-confirm", actionRef)
	if err != nil {
		return "", err
	}
	id, err := stringField(result, "resource_ref", "resource_id")
	if err != nil {
		return "", err
	}
	if enrollment, ok := result["enrollment"].(map[string]any); ok {
		if command, _ := enrollment["install_command"].(string); command != "" {
			env.State.Values[strings.ReplaceAll(name, "-", "_")+"_install_command"] = command
		}
	}
	return id, nil
}
