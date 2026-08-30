package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const m7CollectorNamespace = "argus-telemetry"

func (a *App) runM7Scenario(ctx context.Context, env *E2EEnvironment) error {
	for _, workload := range []string{"argus-telemetry-ingest", "argus-telemetry-writer", "argus-telemetry-query"} {
		if err := env.Kube.WaitDeployment(ctx, env.ObservNS, workload, 5*time.Minute); err != nil {
			return err
		}
	}
	distributionID, hostProfiles, kubernetesProfiles, err := a.verifyM7Catalog(ctx, env)
	if err != nil {
		return err
	}
	if err := a.grantM7HostScope(ctx, env); err != nil {
		return err
	}
	hostID, err := a.createM7Host(ctx, env)
	if err != nil {
		return err
	}
	env.State.Values["m7_host_id"] = hostID
	// The new Host enters the M7 label-based scope and advances the admin's
	// authorization version before the first Collector action is previewed.
	if err := a.refreshEnterpriseLogin(ctx, env); err != nil {
		return err
	}
	if err := a.applyM7CollectorAction(ctx, env, "host", hostID, "install", distributionID, hostProfiles); err != nil {
		return err
	}
	if err := a.waitM7HostCollector(ctx, env, "converged"); err != nil {
		return err
	}
	if err := a.generateM7HostSignals(ctx, env, "host-systemd"); err != nil {
		return err
	}
	for _, action := range []string{"configure", "repair", "upgrade"} {
		if err := a.applyM7CollectorAction(ctx, env, "host", hostID, action, distributionID, hostProfiles); err != nil {
			return err
		}
		if err := a.waitM7HostCollector(ctx, env, "converged"); err != nil {
			return err
		}
	}
	clusterID := env.State.Values["m3_cluster_id"]
	if clusterID == "" {
		return fmt.Errorf("M7 requires the M3 Kubernetes cluster")
	}
	// 用显式 kubernetes_image 覆盖安装一次,验证按次镜像链路(preview→plan→DaemonSet)。
	otelcolImage := env.State.FixtureImages["otelcol"]
	if otelcolImage == "" {
		return fmt.Errorf("M7 requires the fixture otelcol image reference")
	}
	if err := a.applyM7CollectorActionWithImage(ctx, env, "kubernetes-cluster", clusterID, "install", distributionID, kubernetesProfiles, otelcolImage); err != nil {
		return err
	}
	env.ManagedNamespaces = append(env.ManagedNamespaces, m7CollectorNamespace)
	if err := env.Kube.WaitDaemonSet(ctx, m7CollectorNamespace, "argus-otelcol-agent", 5*time.Minute); err != nil {
		return err
	}
	daemonSet, err := env.Kube.Client.AppsV1().DaemonSets(m7CollectorNamespace).Get(ctx, "argus-otelcol-agent", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if got := daemonSet.Spec.Template.Spec.Containers[0].Image; got != otelcolImage {
		return fmt.Errorf("DaemonSet image %q does not match the explicit kubernetes_image %q", got, otelcolImage)
	}
	if err := env.Kube.WaitDeployment(ctx, m7CollectorNamespace, "argus-otelcol-gateway", 5*time.Minute); err != nil {
		return err
	}
	if _, err := env.Kube.Client.CoreV1().Secrets(m7CollectorNamespace).Get(ctx, "argus-otelcol-enrollment", metav1.GetOptions{}); err == nil {
		return fmt.Errorf("consumed Kubernetes Collector enrollment Secret was not deleted")
	}
	if err := a.runM7Generator(ctx, env, "base"); err != nil {
		return err
	}
	if err := a.verifyM7Signals(ctx, env, clusterID, "base"); err != nil {
		return err
	}
	if err := a.verifyM7BastionGateway(ctx, env); err != nil {
		return err
	}
	if err := a.verifyM7NodeBinding(ctx, env, clusterID, hostID, distributionID, kubernetesProfiles); err != nil {
		return err
	}
	if err := a.verifyM7Recovery(ctx, env, clusterID); err != nil {
		return err
	}
	if err := a.runPlaywright(ctx, env, "e2e/m7-real.spec.ts", map[string]string{
		"ARGUS_M7_E2E": "1", "ARGUS_M7_ENTERPRISE_USERNAME": env.State.Values["enterprise_username"],
		"ARGUS_M7_ENTERPRISE_PASSWORD": env.State.Values["enterprise_password"], "ARGUS_M7_CLUSTER_ID": clusterID, "ARGUS_M7_HOST_ID": hostID,
	}); err != nil {
		return err
	}
	if err := a.applyM7CollectorAction(ctx, env, "host", hostID, "uninstall", distributionID, hostProfiles); err != nil {
		return err
	}
	return a.waitM7HostCollector(ctx, env, "uninstalled")
}

func (a *App) verifyM7Catalog(ctx context.Context, env *E2EEnvironment) (string, []string, []string, error) {
	client, _ := scenarioHTTP(env)
	distributions, err := client.JSONArray(ctx, "m7-distributions", "enterprise", http.MethodGet, "/enterprise/telemetry/distributions", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return "", nil, nil, err
	}
	var distributionID string
	windowsHash := ""
	if env.CollectorArtifacts != nil {
		windowsHash = env.CollectorArtifacts.WindowsSHA256
	}
	for _, distribution := range distributions {
		status, _ := distribution["support_status"].(string)
		artifacts, _ := distribution["artifacts"].([]any)
		for _, value := range artifacts {
			artifact, _ := value.(map[string]any)
			platform, _ := artifact["platform"].(string)
			if status == "supported" && platform == "linux_arm64" {
				distributionID, _ = distribution["id"].(string)
			}
			if status == "validation_pending" && platform == "windows_amd64" && artifact["sha256"] != windowsHash {
				return "", nil, nil, fmt.Errorf("M7 Windows Catalog digest does not match the signed ZIP")
			}
		}
	}
	if distributionID == "" {
		return "", nil, nil, fmt.Errorf("M7 supported Linux arm64 Collector distribution is missing")
	}
	profiles, err := client.JSONArray(ctx, "m7-profiles", "enterprise", http.MethodGet, "/enterprise/telemetry/profiles", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return "", nil, nil, err
	}
	profileIDs := map[string]string{}
	for _, profile := range profiles {
		key, _ := profile["key"].(string)
		id, _ := profile["id"].(string)
		profileIDs[key] = id
	}
	host := collectProfileIDs(profileIDs, "host-basic", "linux-journald", "otlp-receiver")
	kubernetes := collectProfileIDs(profileIDs, "k8s-node-container", "k8s-cluster", "otlp-receiver")
	if len(host) != 3 || len(kubernetes) != 3 {
		return "", nil, nil, fmt.Errorf("M7 Collector profiles are incomplete")
	}
	cards, err := client.JSON(ctx, "m7-telemetry-card", "enterprise", http.MethodGet, "/enterprise/interactive-cards", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return "", nil, nil, err
	}
	if _, err := findItem(objectItems(cards), func(item map[string]any) bool {
		return item["slug"] == "telemetry-overview" && item["availability"] == "available" && item["enabled"] == true
	}); err != nil {
		return "", nil, nil, fmt.Errorf("M7 telemetry overview card: %w", err)
	}
	return distributionID, host, kubernetes, nil
}

func collectProfileIDs(values map[string]string, keys ...string) []string {
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if values[key] != "" {
			result = append(result, values[key])
		}
	}
	return result
}

func (a *App) grantM7HostScope(ctx context.Context, env *E2EEnvironment) error {
	client, _ := scenarioHTTP(env)
	roles, err := client.JSON(ctx, "m7-roles", "enterprise", http.MethodGet, "/enterprise/roles", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
	if err != nil {
		return err
	}
	role, err := findItem(objectItems(roles), func(item map[string]any) bool {
		return item["builtin_key"] == "resource_admin" && item["builtin"] == true
	})
	if err != nil {
		return err
	}
	roleID, _ := stringField(role, "id")
	if _, err := client.JSON(ctx, "m7-host-binding", "enterprise", http.MethodPost, "/enterprise/role-bindings", http.StatusCreated, map[string]any{
		"subject_type": "user", "subject_id": env.State.Values["admin_user_id"], "role_id": roleID,
	}, enterpriseHeaders(env, "m7-host-binding")); err != nil {
		return err
	}
	return a.refreshEnterpriseLogin(ctx, env)
}

func (a *App) createM7Host(ctx context.Context, env *E2EEnvironment) (string, error) {
	client, _ := scenarioHTTP(env)
	test, err := client.JSON(ctx, "m7-systemd-host-test", "enterprise", http.MethodPost, "/enterprise/hosts/connection-tests", http.StatusAccepted,
		map[string]any{"address": "8.8.8.8", "port": 22, "platform": "linux", "connection_mode": "direct_ssh", "credential_id": env.State.Values["m3_credential_id"], "username": "root"}, enterpriseHeaders(env, "m7-systemd-host-test"))
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
	preview, err := client.JSON(ctx, "m7-systemd-host-preview", "enterprise", http.MethodPost, "/enterprise/hosts/actions/preview-create", http.StatusCreated,
		map[string]any{"name": "m7-linux-arm64-systemd", "address": "8.8.8.8", "port": 22, "platform": "linux", "connection_mode": "direct_ssh", "credential_id": env.State.Values["m3_credential_id"], "username": "root", "environment": "production", "labels": map[string]string{"team": "m7", "runtime": "systemd"}, "connection_test_id": testID}, enterpriseHeaders(env, "m7-systemd-host"))
	if err != nil {
		return "", err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return "", err
	}
	confirmed, err := a.confirmPendingAction(ctx, env, "m7-systemd-host-confirm", actionRef)
	if err != nil {
		return "", err
	}
	return stringField(confirmed, "resource_ref", "resource_id")
}

func (a *App) applyM7CollectorAction(ctx context.Context, env *E2EEnvironment, resourceType, resourceID, action, distributionID string, profiles []string) error {
	return a.applyM7CollectorActionWithImage(ctx, env, resourceType, resourceID, action, distributionID, profiles, "")
}

// applyM7CollectorActionWithImage 允许 kubernetes 安装显式携带 kubernetes_image
// 覆盖(空串走服务端默认),验证按次镜像配置从 preview 到 DaemonSet 的完整链路。
func (a *App) applyM7CollectorActionWithImage(ctx context.Context, env *E2EEnvironment, resourceType, resourceID, action, distributionID string, profiles []string, kubernetesImage string) error {
	client, _ := scenarioHTTP(env)
	base := "/enterprise/hosts/" + resourceID + "/collector"
	if resourceType == "kubernetes-cluster" {
		base = "/enterprise/kubernetes-clusters/" + resourceID + "/collector"
	}
	body := map[string]any{"distribution_version_id": distributionID, "profile_ids": profiles, "route_kind": "direct_argus"}
	if kubernetesImage != "" {
		body["kubernetes_image"] = kubernetesImage
	}
	idempotencyKey := "m7-collector-" + resourceType + "-" + resourceID + "-" + action
	if action != "install" {
		current, err := client.JSON(ctx, "m7-collector-current-"+action, "enterprise", http.MethodGet, base, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
		if err != nil {
			return err
		}
		version, err := numberField(current, "version")
		if err != nil {
			return err
		}
		body["expected_version"] = version
		idempotencyKey += fmt.Sprintf("-%d", version)
	}
	preview, err := client.JSON(ctx, "m7-collector-preview-"+action, "enterprise", http.MethodPost, base+"/actions/preview-"+action, http.StatusCreated, body, enterpriseHeaders(env, idempotencyKey))
	if err != nil {
		return err
	}
	actionRef, err := stringField(preview, "action_ref")
	if err != nil {
		return err
	}
	_, err = a.confirmPendingAction(ctx, env, idempotencyKey+"-confirm", actionRef)
	return err
}

func (a *App) waitM7HostCollector(ctx context.Context, env *E2EEnvironment, expected string) error {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		current, err := client.JSON(ctx, "m7-host-collector-status", "enterprise", http.MethodGet, "/enterprise/hosts/"+env.State.Values["m7_host_id"]+"/collector", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
		if err != nil {
			return err
		}
		status, _ := current["status"].(string)
		if status == expected {
			return nil
		}
		if status == "degraded" || status == "result_unknown" {
			a.captureM7HostCollectorJournal(ctx, env)
			return fmt.Errorf("M7 Host Collector entered %s", status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("M7 Host Collector did not reach %s", expected)
}

func (a *App) generateM7HostSignals(ctx context.Context, env *E2EEnvironment, marker string) error {
	_, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-direct-executor", "argus-direct-executor",
		"/usr/local/bin/argus-telemetry-e2e", "--endpoint=127.0.0.1:4317", "--resource-id="+env.State.Values["m7_host_id"], "--marker="+marker)
	if err != nil {
		a.captureM7HostCollectorJournal(ctx, env)
	}
	return err
}

func (a *App) captureM7HostCollectorJournal(ctx context.Context, env *E2EEnvironment) {
	value, err := env.Kube.Exec(ctx, env.SystemNS, "app.kubernetes.io/name=argus-direct-executor", "argus-e2e-systemd-host",
		"journalctl", "--unit", "argus-otelcol.service", "--no-pager", "--lines", "200")
	if err != nil {
		value = err.Error()
	}
	_ = writePrivate(filepath.Join(env.Options.Artifacts, "m7-host-collector-journal.log"), redactDiagnostic([]byte(value)))
}

func (a *App) runM7Generator(ctx context.Context, env *E2EEnvironment, marker string) error {
	clusterID := env.State.Values["m3_cluster_id"]
	collectorID, err := a.postgresQuery(ctx, env, "SELECT id FROM collector_instances WHERE resource_type='kubernetes_cluster' AND resource_id='"+clusterID+"' ORDER BY created_at DESC LIMIT 1;")
	if err != nil || strings.TrimSpace(collectorID) == "" {
		return fmt.Errorf("Kubernetes Collector identity is unavailable: %w", err)
	}
	collectorID = strings.TrimSpace(collectorID)
	return a.runM7GeneratorWithIdentity(ctx, env, marker, clusterID, collectorID, "argus-otelcol-identity")
}

func (a *App) runM7GeneratorWithIdentity(ctx context.Context, env *E2EEnvironment, marker, resourceID, serverCollectorID, secretName string) error {
	defaultMode := int32(0o440)
	backoff := int32(3)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: kubernetesNameForDev("argus-m7-otlp-" + marker), Namespace: m7CollectorNamespace, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}},
		Spec: batchv1.JobSpec{BackoffLimit: &backoff, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			RestartPolicy:   corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: int64PointerValue(65532), RunAsGroup: int64PointerValue(65532), FSGroup: int64PointerValue(65532)},
			Containers: []corev1.Container{{Name: "generator", Image: env.State.FixtureImages["backend"], ImagePullPolicy: corev1.PullNever,
				Command: []string{"/usr/local/bin/argus-telemetry-e2e"}, Args: []string{
					"--endpoint=argus-otelcol-gateway." + m7CollectorNamespace + ".svc.cluster.local:4317", "--resource-id=" + resourceID, "--marker=" + marker,
					"--tls-ca=/var/run/argus-telemetry-client/ca.pem", "--tls-cert=/var/run/argus-telemetry-client/client.pem", "--tls-key=/var/run/argus-telemetry-client/client-key.pem",
					"--tls-server-name=collector-" + serverCollectorID + ".argus.telemetry",
				}, VolumeMounts: []corev1.VolumeMount{{Name: "telemetry-identity", MountPath: "/var/run/argus-telemetry-client", ReadOnly: true}}}},
			Volumes: []corev1.Volume{{Name: "telemetry-identity", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, DefaultMode: &defaultMode}}}},
		}}},
	}
	return env.Kube.RunJob(ctx, job, 3*time.Minute)
}

func boolPointer(value bool) *bool         { return &value }
func int64PointerValue(value int64) *int64 { return &value }
