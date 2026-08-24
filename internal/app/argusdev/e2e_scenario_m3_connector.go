package argusdev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type connectorEnrollmentCommand struct {
	ConnectorID string
	Token       string
	Server      string
	Role        string
}

func parseConnectorEnrollmentCommand(command string) (connectorEnrollmentCommand, error) {
	fields := strings.Fields(command)
	if len(fields) < 2 || fields[0] != "argus-connector" || fields[1] != "enroll" {
		return connectorEnrollmentCommand{}, fmt.Errorf("invalid Connector enrollment command")
	}
	result := connectorEnrollmentCommand{}
	seen := map[string]bool{}
	for index := 2; index < len(fields); index += 2 {
		if index+1 >= len(fields) || !strings.HasPrefix(fields[index], "--") || seen[fields[index]] {
			return connectorEnrollmentCommand{}, fmt.Errorf("invalid Connector enrollment command")
		}
		seen[fields[index]] = true
		switch fields[index] {
		case "--connector-id":
			result.ConnectorID = fields[index+1]
		case "--token":
			result.Token = fields[index+1]
		case "--server":
			result.Server = fields[index+1]
		case "--role":
			result.Role = fields[index+1]
		default:
			return connectorEnrollmentCommand{}, fmt.Errorf("invalid Connector enrollment command")
		}
	}
	if result.ConnectorID == "" || result.Token == "" || result.Server == "" || (result.Role != "bastion" && result.Role != "kubernetes") {
		return connectorEnrollmentCommand{}, fmt.Errorf("invalid Connector enrollment command")
	}
	return result, nil
}

func (a *App) startM3BastionConnector(ctx context.Context, env *E2EEnvironment) error {
	command, err := parseConnectorEnrollmentCommand(env.State.Values["m3_bastion_install_command"])
	if err != nil {
		return err
	}
	binary, err := a.buildM3Connector(ctx, env)
	if err != nil {
		return err
	}
	dataDir := filepath.Join(env.WorkDir, "m3-bastion-connector")
	server := "http://127.0.0.1:4180"
	baseArgs := []string{"enroll", "--connector-id", command.ConnectorID, "--token", command.Token, "--server", server, "--role", command.Role, "--name", "m3-bastion", "--data-dir"}
	instanceEnv := map[string]string{"ARGUS_CONNECTOR_INSTANCE_ID": "m3-bastion-" + env.Options.RunID}
	var tampered bytes.Buffer
	tamperedArgs := append(append([]string{}, baseArgs[:2]...), "00000000-0000-4000-8000-000000000001")
	tamperedArgs = append(tamperedArgs, baseArgs[3:]...)
	tamperedArgs = append(tamperedArgs, dataDir+"-tampered")
	if err := a.runner.RunIO(ctx, instanceEnv, nil, &tampered, &tampered, binary, tamperedArgs...); err == nil || !strings.Contains(tampered.String(), "HTTP 401") {
		return fmt.Errorf("Connector enrollment did not reject a tampered CSR identity")
	}
	if err := writePrivate(filepath.Join(env.Options.Artifacts, "m3-bastion-tampered-enroll.log"), redactDiagnostic(tampered.Bytes())); err != nil {
		return err
	}
	validArgs := append(append([]string{}, baseArgs...), dataDir)
	if err := a.runner.Run(ctx, instanceEnv, binary, validArgs...); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, instanceEnv, binary, validArgs...); err != nil {
		return fmt.Errorf("Connector idempotent enrollment retry: %w", err)
	}
	var conflict bytes.Buffer
	conflictArgs := append(append([]string{}, baseArgs...), dataDir+"-conflict")
	if err := a.runner.RunIO(ctx, instanceEnv, nil, &conflict, &conflict, binary, conflictArgs...); err == nil || !strings.Contains(conflict.String(), "HTTP 409") {
		return fmt.Errorf("consumed Connector token was reusable with a different key")
	}
	if err := writePrivate(filepath.Join(env.Options.Artifacts, "m3-bastion-conflict-enroll.log"), redactDiagnostic(conflict.Bytes())); err != nil {
		return err
	}
	if err := rewriteConnectorGateway(filepath.Join(dataDir, "identity.json"), "grpcs://127.0.0.1:4193"); err != nil {
		return err
	}
	logFile, err := openArtifact(filepath.Join(env.Options.Artifacts, "m3-bastion-connector.log"))
	if err != nil {
		return err
	}
	process, err := a.runner.Start(instanceEnv, logFile, logFile, binary, "run", "--data-dir", dataDir)
	if err != nil {
		_ = logFile.Close()
		return err
	}
	env.Processes = append(env.Processes, process)
	env.State.Values["m3_bastion_connector_id"] = command.ConnectorID
	if err := a.waitM3ConnectorOnline(ctx, env, command.ConnectorID, 1); err != nil {
		return err
	}
	client, _ := scenarioHTTP(env)
	scope, err := client.JSON(ctx, "m3-bastion-active", "enterprise", http.MethodGet, "/enterprise/bastion-scopes/"+env.State.Values["m3_bastion_scope_id"], http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
	if err != nil {
		return err
	}
	rootHostID, err := stringField(scope, "connector_host_id")
	if err != nil {
		return err
	}
	env.State.Values["m3_bastion_root_host_id"] = rootHostID
	return nil
}

func (a *App) startM3KubernetesConnector(ctx context.Context, env *E2EEnvironment) error {
	command, err := parseConnectorEnrollmentCommand(env.State.Values["m3_in_cluster_install_command"])
	if err != nil {
		return err
	}
	binary, err := a.buildM3Connector(ctx, env)
	if err != nil {
		return err
	}
	dataDir := filepath.Join(env.WorkDir, "m3-kubernetes-connector")
	instanceEnv := map[string]string{"ARGUS_CONNECTOR_INSTANCE_ID": "m3-kubernetes-" + env.Options.RunID}
	if err := a.runner.Run(ctx, instanceEnv, binary, "enroll", "--connector-id", command.ConnectorID, "--token", command.Token,
		"--server", "http://127.0.0.1:4180", "--role", command.Role, "--name", "m3-in-cluster", "--data-dir", dataDir); err != nil {
		return err
	}
	gateway := "grpcs://argus-connector-gateway." + env.SystemNS + ".svc:9443"
	if err := rewriteConnectorGateway(filepath.Join(dataDir, "identity.json"), gateway); err != nil {
		return err
	}
	identity := map[string][]byte{}
	for _, name := range []string{"identity.json", "connector-key.pem", "connector-cert.pem", "connector-ca.pem"} {
		value, readErr := os.ReadFile(filepath.Join(dataDir, name))
		if readErr != nil {
			return readErr
		}
		identity[name] = value
	}
	labels := map[string]string{"app.kubernetes.io/name": "argus-e2e-kubernetes-connector", "app.kubernetes.io/part-of": "argus-e2e", "argus.io/release-id": env.ReleaseID}
	secretName := "argus-e2e-kubernetes-connector-identity"
	if _, err := env.Kube.Client.CoreV1().Secrets(env.SystemNS).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: env.SystemNS, Labels: labels}, Data: identity,
	}, metav1.CreateOptions{}); err != nil {
		return err
	}
	serviceAccountName := "argus-e2e-kubernetes-connector-runtime"
	if _, err := env.Kube.Client.CoreV1().ServiceAccounts(env.SystemNS).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName, Namespace: env.SystemNS, Labels: labels},
	}, metav1.CreateOptions{}); err != nil {
		return err
	}
	rbacName := kubernetesNameForDev(env.ReleaseID + "-kubernetes-connector")
	rules := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"namespaces", "nodes", "pods", "services", "endpoints", "configmaps", "secrets", "serviceaccounts", "resourcequotas", "replicationcontrollers"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{""}, Resources: []string{"nodes/proxy", "nodes/stats"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "statefulsets", "daemonsets", "replicasets"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{"batch"}, Resources: []string{"jobs", "cronjobs"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"autoscaling"}, Resources: []string{"horizontalpodautoscalers"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"rbac.authorization.k8s.io"}, Resources: []string{"clusterroles", "clusterrolebindings"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
	}
	if _, err := env.Kube.Client.RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: rbacName, Labels: labels}, Rules: rules,
	}, metav1.CreateOptions{}); err != nil {
		return err
	}
	if _, err := env.Kube.Client.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: rbacName, Labels: labels},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: serviceAccountName, Namespace: env.SystemNS}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: rbacName},
	}, metav1.CreateOptions{}); err != nil {
		_ = env.Kube.Client.RbacV1().ClusterRoles().Delete(ctx, rbacName, metav1.DeleteOptions{})
		return err
	}
	env.ManagedClusterRBAC = append(env.ManagedClusterRBAC, rbacName)
	mode := int32(0o444)
	replicas := int32(1)
	runAsUser := int64(65532)
	runAsGroup := int64(65532)
	fsGroup := int64(65532)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "argus-e2e-kubernetes-connector", Namespace: env.SystemNS, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": labels["app.kubernetes.io/name"]}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{
				ServiceAccountName: serviceAccountName,
				SecurityContext:    &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: &runAsUser, RunAsGroup: &runAsGroup, FSGroup: &fsGroup},
				InitContainers: []corev1.Container{{Name: "prepare-identity", Image: env.State.FixtureImages["ssh"], ImagePullPolicy: corev1.PullNever,
					Command: []string{"/bin/sh", "-c"}, Args: []string{"cp /identity-source/* /identity/ && chown -R 65532:65532 /identity && chmod 700 /identity && chmod 600 /identity/*"},
					SecurityContext: &corev1.SecurityContext{RunAsNonRoot: boolPointer(false), RunAsUser: int64PointerValue(0), RunAsGroup: int64PointerValue(0)},
					VolumeMounts:    []corev1.VolumeMount{{Name: "identity-source", MountPath: "/identity-source", ReadOnly: true}, {Name: "identity", MountPath: "/identity"}}}},
				Containers: []corev1.Container{{Name: "connector", Image: env.State.FixtureImages["backend"], ImagePullPolicy: corev1.PullNever,
					Command: []string{"/usr/local/bin/argus-connector"}, Args: []string{"run", "--data-dir", "/var/lib/argus-connector"},
					VolumeMounts: []corev1.VolumeMount{{Name: "identity", MountPath: "/var/lib/argus-connector"}}}},
				Volumes: []corev1.Volume{{Name: "identity-source", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, DefaultMode: &mode}}},
					{Name: "identity", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
			}},
		},
	}
	if _, err := env.Kube.Client.AppsV1().Deployments(env.SystemNS).Create(ctx, deployment, metav1.CreateOptions{}); err != nil {
		return err
	}
	if err := env.Kube.WaitDeployment(ctx, env.SystemNS, deployment.Name, 5*time.Minute); err != nil {
		return err
	}
	env.State.Values["m3_kubernetes_connector_id"] = command.ConnectorID
	return a.waitM3ConnectorOnline(ctx, env, command.ConnectorID, 1)
}

func (a *App) buildM3Connector(ctx context.Context, env *E2EEnvironment) (string, error) {
	binary := filepath.Join(env.WorkDir, "argus-connector")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if _, err := os.Stat(binary); err == nil {
		return binary, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := a.runner.Run(ctx, nil, "go", "build", "-trimpath", "-o", binary, "./cmd/argus-connector"); err != nil {
		return "", err
	}
	return binary, nil
}

func rewriteConnectorGateway(path, endpoint string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var identity map[string]any
	if err := json.Unmarshal(data, &identity); err != nil {
		return err
	}
	identity["gateway_endpoint"] = endpoint
	encoded, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, append(encoded, '\n'))
}

func (a *App) waitM3ConnectorOnline(ctx context.Context, env *E2EEnvironment, connectorID string, minimumEpoch int64) error {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(2 * time.Minute)
	stableEpoch := int64(0)
	stableChecks := 0
	for time.Now().Before(deadline) {
		connectors, err := client.JSON(ctx, "m3-connector-online", "enterprise", http.MethodGet, "/enterprise/connectors", http.StatusOK, nil, map[string]string{"Origin": enterpriseOrigin})
		if err != nil {
			return err
		}
		for _, item := range objectItems(connectors) {
			if item["id"] != connectorID || item["status"] != "online" {
				continue
			}
			epoch, err := numberField(item, "connection_epoch")
			if err != nil || epoch < minimumEpoch {
				continue
			}
			if epoch == stableEpoch {
				stableChecks++
			} else {
				stableEpoch, stableChecks = epoch, 1
			}
			if stableChecks >= 5 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("Connector %s did not remain online on a stable epoch", connectorID)
}
