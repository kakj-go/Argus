package argusdev

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func (a *App) createM3BastionKubernetes(ctx context.Context, env *E2EEnvironment) (string, error) {
	serviceAccountName := "argus-e2e-kubernetes-connector"
	if _, err := env.Kube.Client.CoreV1().ServiceAccounts(env.SystemNS).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName, Namespace: env.SystemNS, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}},
	}, metav1.CreateOptions{}); err != nil {
		return "", err
	}
	rbacName := kubernetesNameForDev(env.ReleaseID + "-connector")
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"namespaces", "nodes", "pods", "pods/log", "services", "endpoints", "resourcequotas"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "statefulsets", "daemonsets", "replicasets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
	}
	if _, err := env.Kube.Client.RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}}, Rules: rules}, metav1.CreateOptions{}); err != nil {
		return "", err
	}
	if _, err := env.Kube.Client.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: rbacName, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: serviceAccountName, Namespace: env.SystemNS}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: rbacName},
	}, metav1.CreateOptions{}); err != nil {
		return "", err
	}
	env.ManagedClusterRBAC = append(env.ManagedClusterRBAC, rbacName)
	expiration := int64(time.Hour / time.Second)
	token, err := env.Kube.Client.CoreV1().ServiceAccounts(env.SystemNS).CreateToken(ctx, serviceAccountName,
		&authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expiration}}, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	caData := env.Kube.Config.CAData
	if len(caData) == 0 && env.Kube.Config.CAFile != "" {
		caData, err = os.ReadFile(env.Kube.Config.CAFile)
		if err != nil {
			return "", err
		}
	}
	kubeconfig, err := yaml.Marshal(map[string]any{
		"apiVersion": "v1", "kind": "Config", "current-context": "argus-e2e",
		"clusters": []any{map[string]any{"name": "argus-e2e", "cluster": map[string]any{"server": env.Kube.Config.Host, "certificate-authority-data": base64.StdEncoding.EncodeToString(caData)}}},
		"contexts": []any{map[string]any{"name": "argus-e2e", "context": map[string]any{"cluster": "argus-e2e", "user": "argus-e2e", "namespace": env.SystemNS}}},
		"users":    []any{map[string]any{"name": "argus-e2e", "user": map[string]any{"token": token.Status.Token}}},
	})
	if err != nil {
		return "", err
	}
	client, _ := scenarioHTTP(env)
	secret, err := client.JSON(ctx, "m3-kubeconfig-secret", "enterprise", http.MethodPost, "/enterprise/secrets", http.StatusCreated,
		map[string]any{"name": "m3-kubeconfig", "type": "kubeconfig", "description": "M3 E2E Kubernetes access", "value": string(kubeconfig)}, enterpriseHeaders(env, "m3-kubeconfig-secret"))
	if err != nil {
		return "", err
	}
	secretID, err := stringField(secret, "id")
	if err != nil {
		return "", err
	}
	credential, err := client.JSON(ctx, "m3-kubeconfig-credential", "enterprise", http.MethodPost, "/enterprise/credentials", http.StatusCreated,
		map[string]any{"name": "m3-kubernetes", "protocol": "kubernetes", "secret_id": secretID}, enterpriseHeaders(env, "m3-kubeconfig-credential"))
	if err != nil {
		return "", err
	}
	credentialID, err := stringField(credential, "id")
	if err != nil {
		return "", err
	}
	denied, err := client.JSON(ctx, "m3-kubernetes-loopback-direct-denied", "enterprise", http.MethodPost, "/enterprise/kubernetes-clusters/connection-tests", http.StatusForbidden,
		map[string]any{"api_server": "https://127.0.0.1:6443", "connection_mode": "direct", "credential_id": credentialID}, enterpriseHeaders(env, "m3-kubernetes-loopback-direct-denied"))
	if err != nil {
		return "", err
	}
	if denied["code"] != "DIRECT_TARGET_DENIED" {
		return "", fmt.Errorf("loopback Kubernetes direct target returned %v", denied["code"])
	}
	test, err := client.JSON(ctx, "m3-bastion-kubernetes-test", "enterprise", http.MethodPost, "/enterprise/kubernetes-clusters/connection-tests", http.StatusAccepted,
		map[string]any{"api_server": env.Kube.Config.Host, "connection_mode": "via_bastion", "bastion_scope_id": env.State.Values["m3_bastion_scope_id"], "credential_id": credentialID}, enterpriseHeaders(env, "m3-bastion-kubernetes-test"))
	if err != nil {
		return "", err
	}
	testID, err := stringField(test, "id")
	if err != nil {
		return "", err
	}
	if err := a.waitConnectionTest(ctx, env, testID); err != nil {
		return "", err
	}
	preview, err := client.JSON(ctx, "m3-bastion-kubernetes-preview", "enterprise", http.MethodPost, "/enterprise/kubernetes-clusters/actions/preview-create", http.StatusCreated,
		map[string]any{"name": "m3-via-bastion", "api_server": env.Kube.Config.Host, "connection_mode": "via_bastion", "bastion_scope_id": env.State.Values["m3_bastion_scope_id"], "credential_id": credentialID, "default_namespace": "default", "environment": "production", "labels": map[string]string{"team": "m3", "route": "bastion"}, "connection_test_id": testID}, enterpriseHeaders(env, "m3-bastion-kubernetes"))
	if err != nil {
		return "", err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return "", err
	}
	confirmed, err := a.confirmPendingAction(ctx, env, "m3-bastion-kubernetes-confirm", actionRef)
	if err != nil {
		return "", err
	}
	return stringField(confirmed, "resource_ref", "resource_id")
}
