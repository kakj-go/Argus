package argusctl

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilversion "k8s.io/apimachinery/pkg/util/version"
)

func (a *App) install(ctx context.Context, cfg *InstallConfig) error {
	if cfg.Spec.Profile == "production" {
		return fmt.Errorf("production install blocked: POSTGRES_HA_ADR_REQUIRED; SANDBOX_RUNTIME_ADR_REQUIRED")
	}
	if _, err := a.buildPreflight(ctx, cfg); err != nil {
		return err
	}
	root, err := findRepoRoot(filepath.Dir(cfg.path))
	if err != nil {
		return err
	}
	clients, err := clientsFor(cfg.Spec.KubeContext)
	if err != nil {
		return err
	}
	helm := helmManager{contextName: cfg.Spec.KubeContext, cacheDir: filepath.Join(root, "deploy", ".cache", "charts"), log: a.stderr}

	foundation, err := loadLocalChart(root, "argus-foundation")
	if err != nil {
		return err
	}
	if err := helm.installOrUpgrade(ctx, cfg.Spec.ReleaseID+"-foundation", "default", foundation, foundationValues(cfg)); err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "foundation", "complete", "namespaces and baseline policies installed"); err != nil {
		return err
	}
	if err := ensureCertManager(ctx, clients, helm); err != nil {
		return err
	}

	if err := clients.setStage(ctx, cfg, "data-operators", "running", "installing Strimzi and Altinity operators"); err != nil {
		return err
	}
	strimzi, err := helm.loadRemoteChart(ctx, "strimzi-1.1.0", strimziChartURL)
	if err != nil {
		return err
	}
	strimziValues := map[string]any{
		"watchAnyNamespace": false,
		"watchNamespaces":   []any{cfg.Spec.Namespaces.Observability},
		"replicas":          1,
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "1", "memory": "768Mi"},
		},
	}
	if err := helm.installOrUpgrade(ctx, cfg.upstreamReleaseName("st"), cfg.Spec.Namespaces.Observability, strimzi, strimziValues); err != nil {
		return err
	}
	altinity, err := helm.loadRemoteChart(ctx, "altinity-0.27.3", altinityChartURL)
	if err != nil {
		return err
	}
	altinityValues := map[string]any{
		"operator": map[string]any{"resources": map[string]any{
			"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "1", "memory": "768Mi"},
		}},
	}
	if err := helm.installOrUpgrade(ctx, cfg.upstreamReleaseName("ch"), cfg.Spec.Namespaces.Observability, altinity, altinityValues); err != nil {
		return err
	}
	if err := a.markOwnedCRDs(ctx, cfg); err != nil {
		return err
	}
	if err := waitForDeployment(ctx, clients, cfg.Spec.Namespaces.Observability, "strimzi-cluster-operator", 10*time.Minute); err != nil {
		return err
	}
	if err := waitForDeploymentByLabel(ctx, clients, cfg.Spec.Namespaces.Observability, "app.kubernetes.io/name=altinity-clickhouse-operator", 10*time.Minute); err != nil {
		return err
	}
	operatorMarker, err := loadLocalChart(root, "argus-data-operators")
	if err != nil {
		return err
	}
	if err := helm.installOrUpgrade(ctx, cfg.Spec.ReleaseID+"-data-operators", cfg.Spec.Namespaces.Observability, operatorMarker, map[string]any{"releaseId": cfg.Spec.ReleaseID}); err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "data-operators", "complete", "operators ready"); err != nil {
		return err
	}

	credentials, err := ensureCredentials(ctx, clients, cfg)
	if err != nil {
		return err
	}
	dataChart, err := loadLocalChart(root, "argus-data")
	if err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "data", "running", "installing PostgreSQL, Redis, MinIO, Kafka, Keeper and ClickHouse"); err != nil {
		return err
	}
	if err := helm.installOrUpgrade(ctx, cfg.Spec.ReleaseID+"-data", cfg.Spec.Namespaces.System, dataChart, dataValues(cfg, credentials)); err != nil {
		return err
	}
	if err := waitForData(ctx, clients, cfg); err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "data", "complete", "data services ready"); err != nil {
		return err
	}

	if err := clients.setStage(ctx, cfg, "sandbox", "running", "installing OpenSandbox 0.2.0"); err != nil {
		return err
	}
	openSandbox, err := helm.loadOpenSandboxChart(ctx)
	if err != nil {
		return err
	}
	generatedSecret := cfg.Spec.ReleaseID + "-generated-secrets"
	apiKey, err := ensureSecretValue(ctx, clients, cfg.Spec.Namespaces.Sandbox, generatedSecret, "opensandbox-api-key", 32)
	if err != nil {
		return err
	}
	if err := helm.installOrUpgrade(ctx, cfg.upstreamReleaseName("os"), cfg.Spec.Namespaces.Sandbox, openSandbox, openSandboxValues(cfg, generatedSecret)); err != nil {
		return err
	}
	if err := a.markOwnedCRDs(ctx, cfg); err != nil {
		return err
	}
	sandboxMarker, err := loadLocalChart(root, "argus-sandbox")
	if err != nil {
		return err
	}
	if err := helm.installOrUpgrade(ctx, cfg.Spec.ReleaseID+"-sandbox", cfg.Spec.Namespaces.Sandbox, sandboxMarker, sandboxValues(cfg, apiKey)); err != nil {
		return err
	}
	if err := waitForDeployment(ctx, clients, cfg.Spec.Namespaces.Sandbox, "opensandbox-controller-manager", 10*time.Minute); err != nil {
		return err
	}
	if err := waitForDeployment(ctx, clients, cfg.Spec.Namespaces.Sandbox, "opensandbox-server", 10*time.Minute); err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "sandbox", "complete", "OpenSandbox ready with shared-runtime degradation"); err != nil {
		return err
	}

	setupSecret := cfg.Spec.ReleaseID + "-generated-secrets"
	if err := ensureSetupToken(ctx, clients, cfg.Spec.Namespaces.System, setupSecret); err != nil {
		return err
	}
	idempotencyKey, err := ensureSecretValue(ctx, clients, cfg.Spec.Namespaces.System, setupSecret, "idempotency-encryption-key", 32)
	if err != nil {
		return err
	}
	cursorSigningKey, err := ensureSecretValue(ctx, clients, cfg.Spec.Namespaces.System, setupSecret, "cursor-signing-key", 32)
	if err != nil {
		return err
	}
	pendingActionKey, err := ensureSecretValue(ctx, clients, cfg.Spec.Namespaces.System, setupSecret, "pending-action-encryption-key", 32)
	if err != nil {
		return err
	}
	secretKEK, err := ensureSecretValue(ctx, clients, cfg.Spec.Namespaces.System, setupSecret, "secret-kek-v1", 32)
	if err != nil {
		return err
	}
	platformChart, err := loadLocalChart(root, "argus-platform")
	if err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "platform", "running", "installing migrations and platform roles"); err != nil {
		return err
	}
	if err := helm.installOrUpgrade(ctx, cfg.Spec.ReleaseID+"-platform", cfg.Spec.Namespaces.System, platformChart, platformValues(cfg, credentials, setupSecret, idempotencyKey, cursorSigningKey, pendingActionKey, secretKEK)); err != nil {
		return err
	}
	if err := waitForPlatform(ctx, clients, cfg); err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "platform", "complete", "platform roles and PostgreSQL schema ready"); err != nil {
		return err
	}

	telemetryChart, err := loadLocalChart(root, "argus-telemetry-pipeline")
	if err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "telemetry-pipeline", "running", "installing ClickHouse migration and OTel writer"); err != nil {
		return err
	}
	if err := helm.installOrUpgrade(ctx, cfg.Spec.ReleaseID+"-telemetry-pipeline", cfg.Spec.Namespaces.Observability, telemetryChart, telemetryValues(cfg)); err != nil {
		return err
	}
	if err := waitForDeployment(ctx, clients, cfg.Spec.Namespaces.Observability, "otel-clickhouse-writer", 10*time.Minute); err != nil {
		return err
	}
	if err := clients.setStage(ctx, cfg, "complete", "complete", "all Evaluation stages installed"); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "Argus Evaluation %s installed successfully\n", cfg.Spec.ReleaseID)
	return nil
}

func foundationValues(cfg *InstallConfig) map[string]any {
	return map[string]any{"releaseId": cfg.Spec.ReleaseID, "namespaces": namespacesValues(cfg)}
}

func namespacesValues(cfg *InstallConfig) map[string]any {
	return map[string]any{"system": cfg.Spec.Namespaces.System, "sandbox": cfg.Spec.Namespaces.Sandbox, "observability": cfg.Spec.Namespaces.Observability}
}

func dataValues(cfg *InstallConfig, secrets map[string]string) map[string]any {
	return map[string]any{
		"releaseId":    cfg.Spec.ReleaseID,
		"namespaces":   map[string]any{"system": cfg.Spec.Namespaces.System, "observability": cfg.Spec.Namespaces.Observability},
		"storageClass": cfg.Spec.StorageClass,
		"images": map[string]any{
			"registry": cfg.Spec.Images.Registry, "tag": cfg.Spec.Images.Tag, "pullPolicy": cfg.Spec.Images.PullPolicy,
			"postgresql": "postgres:18.6-alpine", "redis": "redis:8.10.0-alpine", "minio": cfg.Image("minio"),
			"clickhouse": "clickhouse/clickhouse-server:26.3.17.110-alpine",
		},
		"persistence": map[string]any{
			"postgresql": cfg.Spec.Persistence.PostgreSQL, "redis": cfg.Spec.Persistence.Redis, "minio": cfg.Spec.Persistence.MinIO,
			"kafka": cfg.Spec.Persistence.Kafka, "clickhouse": cfg.Spec.Persistence.ClickHouse, "keeper": cfg.Spec.Persistence.Keeper,
		},
		"secrets": map[string]any{
			"postgresqlPassword": secrets["postgresql-password"], "redisPassword": secrets["redis-password"],
			"minioRootUser": secrets["minio-root-user"], "minioRootPassword": secrets["minio-root-password"],
			"clickhousePassword": secrets["clickhouse-password"],
		},
	}
}

func platformValues(cfg *InstallConfig, credentials map[string]string, setupSecret, idempotencyKey, cursorSigningKey, pendingActionKey, secretKEK string) map[string]any {
	allowedOrigins := []any{"http://localhost:4173", "http://localhost:4174", "http://localhost:4175"}
	secureCookies := false
	if cfg.Spec.Profile == "production" {
		allowedOrigins = []any{
			"https://" + cfg.Spec.Exposure.EnterpriseHost,
			"https://" + cfg.Spec.Exposure.PlatformHost,
			"https://" + cfg.Spec.Exposure.SetupHost,
		}
		secureCookies = true
	}
	connectorEnrollmentURL := "http://localhost:8080"
	connectorGatewayAddress := "grpcs://localhost:9443"
	if cfg.Spec.Profile == "production" {
		connectorEnrollmentURL = "https://" + cfg.Spec.Exposure.EnterpriseHost
		connectorGatewayAddress = "grpcs://" + cfg.Spec.Exposure.ConnectorHost + ":9443"
	}
	return map[string]any{
		"releaseId": cfg.Spec.ReleaseID, "namespaces": namespacesValues(cfg), "replicas": 1, "setupTokenSecretName": setupSecret,
		"images": map[string]any{"backend": cfg.Image("argus-backend"), "web": cfg.Image("argus-web"), "pullPolicy": cfg.Spec.Images.PullPolicy, "postgresql": "postgres:18.6-alpine"},
		"runtime": map[string]any{
			"postgresqlPassword": credentials["postgresql-password"], "redisPassword": credentials["redis-password"],
			"idempotencyEncryptionKey": idempotencyKey, "cursorSigningKey": cursorSigningKey,
			"pendingActionEncryptionKey": pendingActionKey,
			"secretKEKKeyring": map[string]any{
				"current_version": 1,
				"keys":            map[string]any{"1": secretKEK},
			},
			"connectorEnrollmentURL":  connectorEnrollmentURL,
			"connectorGatewayAddress": connectorGatewayAddress,
			"allowedOrigins":          allowedOrigins, "secureCookies": secureCookies,
		},
		"production": map[string]any{"hosts": map[string]any{"enterprise": cfg.Spec.Exposure.EnterpriseHost,
			"platform": cfg.Spec.Exposure.PlatformHost, "setup": cfg.Spec.Exposure.SetupHost, "connector": cfg.Spec.Exposure.ConnectorHost}},
	}
}

func ensureCertManager(ctx context.Context, clients *kubeClients, helm helmManager) error {
	if _, err := clients.typed.Discovery().ServerResourcesForGroupVersion("cert-manager.io/v1"); err == nil {
		deployment, getErr := clients.typed.AppsV1().Deployments("cert-manager").Get(ctx, "cert-manager", metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("cert-manager API exists but controller deployment cannot be inspected: %w", getErr)
		}
		version := certManagerDeploymentVersion(deployment)
		if !certManagerVersionCompatible(version, certManagerVersion) {
			return fmt.Errorf("existing cert-manager version %q is incompatible with locked version %s", version, certManagerVersion)
		}
		return nil
	}
	chart, err := helm.loadRemoteChart(ctx, "cert-manager-v"+certManagerVersion, certManagerURL)
	if err != nil {
		return err
	}
	if _, err := clients.typed.CoreV1().Namespaces().Get(ctx, "cert-manager", metav1.GetOptions{}); err != nil {
		_, createErr := clients.typed.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}, metav1.CreateOptions{})
		if createErr != nil {
			return fmt.Errorf("create cert-manager namespace: %w", createErr)
		}
	}
	if err := helm.installOrUpgrade(ctx, "cert-manager", "cert-manager", chart, map[string]any{"crds": map[string]any{"enabled": true}}); err != nil {
		return err
	}
	return waitForDeployment(ctx, clients, "cert-manager", "cert-manager", 10*time.Minute)
}

func certManagerDeploymentVersion(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return ""
	}
	for _, key := range []string{"app.kubernetes.io/version", "helm.sh/chart"} {
		if value := deployment.Labels[key]; value != "" {
			if key == "helm.sh/chart" {
				value = strings.TrimPrefix(value, "cert-manager-")
			}
			return strings.TrimPrefix(value, "v")
		}
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != "cert-manager-controller" && container.Name != "cert-manager" {
			continue
		}
		if index := strings.LastIndex(container.Image, ":"); index >= 0 && index+1 < len(container.Image) {
			return strings.TrimPrefix(container.Image[index+1:], "v")
		}
	}
	return ""
}

func certManagerVersionCompatible(actual, locked string) bool {
	actualVersion, err := utilversion.ParseSemantic(strings.TrimPrefix(actual, "v"))
	if err != nil {
		return false
	}
	lockedVersion, err := utilversion.ParseSemantic(strings.TrimPrefix(locked, "v"))
	if err != nil {
		return false
	}
	return actualVersion.Major() == lockedVersion.Major() && actualVersion.Minor() == lockedVersion.Minor() && actualVersion.AtLeast(lockedVersion)
}

func sandboxValues(cfg *InstallConfig, apiKey string) map[string]any {
	return map[string]any{
		"releaseId": cfg.Spec.ReleaseID, "apiKey": apiKey, "runtimeClassName": cfg.Spec.OpenSandbox.RuntimeClassName,
		"allowSharedRuntime": cfg.Spec.OpenSandbox.AllowSharedRuntime,
		"versions":           map[string]any{"chart": "0.2.0", "server": "0.2.2", "controller": "0.2.0"},
	}
}

func telemetryValues(cfg *InstallConfig) map[string]any {
	return map[string]any{
		"releaseId": cfg.Spec.ReleaseID, "namespace": cfg.Spec.Namespaces.Observability,
		"images":     map[string]any{"clickhouse": "clickhouse/clickhouse-server:26.3.17.110-alpine", "otelCollector": "otel/opentelemetry-collector-contrib:0.158.0"},
		"clickhouse": map[string]any{"endpoint": "tcp://argus-clickhouse-client:9000"},
		"kafka":      map[string]any{"brokers": "argus-kafka-kafka-bootstrap:9092"},
	}
}

func openSandboxValues(cfg *InstallConfig, generatedSecret string) map[string]any {
	config := fmt.Sprintf(`[server]
host = "0.0.0.0"
port = 80
api_key = ""

[log]
level = "INFO"

[runtime]
type = "kubernetes"
execd_image = "opensandbox/execd:v1.0.22"

[kubernetes]
kubeconfig_path = ""
namespace = %q
informer_enabled = true
informer_resync_seconds = 300
informer_watch_timeout_seconds = 60
snapshot_create_timeout_seconds = 900
workload_provider = "batchsandbox"
batchsandbox_template_file = "/etc/opensandbox/example.batchsandbox-template.yaml"

[egress]
image = "opensandbox/egress:v1.1.6"
mode = "dns+nft"
`, cfg.Spec.Namespaces.Sandbox)
	return map[string]any{
		"opensandbox-controller": map[string]any{
			"namespaceOverride": cfg.Spec.Namespaces.Sandbox,
			"controller": map[string]any{
				"replicaCount": 1,
				"image":        map[string]any{"repository": "opensandbox/controller", "tag": "v0.2.0", "pullPolicy": "IfNotPresent"},
				"resources":    map[string]any{"requests": map[string]any{"cpu": "25m", "memory": "64Mi"}, "limits": map[string]any{"cpu": "500m", "memory": "256Mi"}},
			},
		},
		"opensandbox-server": map[string]any{
			"namespaceOverride": cfg.Spec.Namespaces.Sandbox,
			"server": map[string]any{
				"replicaCount": 1, "image": map[string]any{"repository": "opensandbox/server", "tag": "v0.2.2"},
				"resources": map[string]any{"requests": map[string]any{"cpu": "100m", "memory": "256Mi"}, "limits": map[string]any{"cpu": "1", "memory": "1Gi"}},
				"env":       []any{map[string]any{"name": "OPENSANDBOX_SERVER_API_KEY", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": generatedSecret, "key": "opensandbox-api-key"}}}},
			},
			"configToml": config,
		},
	}
}

func ensureCredentials(ctx context.Context, clients *kubeClients, cfg *InstallConfig) (map[string]string, error) {
	values := map[string]string{}
	for key, size := range map[string]int{"postgresql-password": 24, "redis-password": 24, "minio-root-password": 32, "clickhouse-password": 24} {
		value, err := ensureSecretValue(ctx, clients, cfg.Spec.Namespaces.System, cfg.Spec.ReleaseID+"-generated-credentials", key, size)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	values["minio-root-user"] = "argusadmin"
	return values, nil
}

func ensureSecretValue(ctx context.Context, clients *kubeClients, namespace, name, key string, size int) (string, error) {
	secrets := clients.typed.CoreV1().Secrets(namespace)
	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if value := existing.Data[key]; len(value) > 0 {
			return string(value), nil
		}
	} else {
		existing = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app.kubernetes.io/part-of": "argus"}}, Data: map[string][]byte{}}
	}
	value, err := randomSecret(size)
	if err != nil {
		return "", err
	}
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	existing.Data[key] = []byte(value)
	if existing.ResourceVersion == "" {
		_, err = secrets.Create(ctx, existing, metav1.CreateOptions{})
	} else {
		_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	}
	if err != nil {
		return "", fmt.Errorf("persist generated secret %s/%s: %w", namespace, name, err)
	}
	return value, nil
}

func ensureSetupToken(ctx context.Context, clients *kubeClients, namespace, name string) error {
	secrets := clients.typed.CoreV1().Secrets(namespace)
	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app.kubernetes.io/part-of": "argus"}}, Data: map[string][]byte{}}
	}
	expiresAt, parseErr := time.Parse(time.RFC3339, string(secret.Data["setup-token-expires-at"]))
	if len(secret.Data["setup-token"]) > 0 && parseErr == nil && time.Now().UTC().Before(expiresAt) {
		return nil
	}
	token, err := randomSecret(32)
	if err != nil {
		return err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data["setup-token"] = []byte(token)
	secret.Data["setup-token-expires-at"] = []byte(time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339))
	if secret.ResourceVersion == "" {
		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	} else {
		_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("persist setup token secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

func randomSecret(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(buffer), "="), nil
}

func waitForData(ctx context.Context, clients *kubeClients, cfg *InstallConfig) error {
	for _, item := range []struct{ namespace, name string }{
		{cfg.Spec.Namespaces.System, "argus-postgresql"}, {cfg.Spec.Namespaces.System, "argus-redis"}, {cfg.Spec.Namespaces.System, "argus-minio"},
		{cfg.Spec.Namespaces.Observability, "argus-keeper"},
	} {
		if err := waitForStatefulSet(ctx, clients, item.namespace, item.name, 12*time.Minute); err != nil {
			return err
		}
	}
	readyCondition := func(object map[string]any) bool {
		status, _ := object["status"].(map[string]any)
		conditions, _ := status["conditions"].([]any)
		for _, raw := range conditions {
			condition, _ := raw.(map[string]any)
			if condition["type"] == "Ready" && condition["status"] == "True" {
				return true
			}
		}
		return false
	}
	if err := clients.waitResource(ctx, schema.GroupVersionResource{Group: "kafka.strimzi.io", Version: "v1", Resource: "kafkas"}, cfg.Spec.Namespaces.Observability, "argus-kafka", readyCondition, 15*time.Minute); err != nil {
		return fmt.Errorf("wait for Kafka: %w", err)
	}
	if err := clients.waitResource(ctx, schema.GroupVersionResource{Group: "clickhouse.altinity.com", Version: "v1", Resource: "clickhouseinstallations"}, cfg.Spec.Namespaces.Observability, "argus-clickhouse", func(object map[string]any) bool {
		status, _ := object["status"].(map[string]any)
		state, _ := status["status"].(string)
		return strings.EqualFold(state, "completed") || strings.EqualFold(state, "complete")
	}, 15*time.Minute); err != nil {
		return fmt.Errorf("wait for ClickHouse: %w", err)
	}
	return nil
}

func waitForPlatform(ctx context.Context, clients *kubeClients, cfg *InstallConfig) error {
	names := []string{"argus-web", "argus-server", "argus-worker", "argus-direct-executor", "argus-connector-gateway", "argus-telemetry-ingest", "argus-telemetry-query"}
	for _, name := range names {
		if err := waitForDeployment(ctx, clients, cfg.Spec.Namespaces.System, name, 10*time.Minute); err != nil {
			return err
		}
	}
	return waitForJob(ctx, clients, cfg.Spec.Namespaces.System, "argus-postgresql-migration", 10*time.Minute)
}

func waitForDeployment(ctx context.Context, clients *kubeClients, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		deployment, err := clients.typed.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for deployment %s/%s", namespace, name)
}

func waitForDeploymentByLabel(ctx context.Context, clients *kubeClients, namespace, selector string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		deployments, err := clients.typed.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err == nil && len(deployments.Items) > 0 {
			allReady := true
			for _, deployment := range deployments.Items {
				allReady = allReady && deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.AvailableReplicas == *deployment.Spec.Replicas
			}
			if allReady {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for deployment selector %s in %s", selector, namespace)
}

func waitForStatefulSet(ctx context.Context, clients *kubeClients, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		statefulSet, err := clients.typed.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && statefulSet.Status.ObservedGeneration >= statefulSet.Generation && statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for statefulset %s/%s", namespace, name)
}

func waitForJob(ctx context.Context, clients *kubeClients, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := clients.typed.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			if job.Status.Succeeded > 0 {
				return nil
			}
			if job.Status.Failed > 0 && job.Status.Failed >= *job.Spec.BackoffLimit {
				return fmt.Errorf("job %s/%s failed", namespace, name)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for job %s/%s", namespace, name)
}
