package argusctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VerifyCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type VerifyReport struct {
	ReleaseID    string            `json:"releaseId"`
	Profile      string            `json:"profile"`
	VerifiedAt   string            `json:"verifiedAt"`
	Passed       bool              `json:"passed"`
	Checks       []VerifyCheck     `json:"checks"`
	Degradations []string          `json:"degradations"`
	Images       map[string]string `json:"images"`
	Artifacts    string            `json:"artifacts"`
}

func (a *App) verify(ctx context.Context, cfg *InstallConfig, output, artifactPath string) (returnErr error) {
	root, err := findRepoRoot(filepath.Dir(cfg.path))
	if err != nil {
		return err
	}
	if artifactPath == "" {
		artifactPath = filepath.Join(root, "artifacts", "k8s-e2e", cfg.Spec.ReleaseID)
	}
	report := VerifyReport{
		ReleaseID: cfg.Spec.ReleaseID, Profile: cfg.Spec.Profile, VerifiedAt: time.Now().UTC().Format(time.RFC3339), Passed: true,
		Degradations: []string{"NETWORK_POLICY_ENFORCEMENT_UNVERIFIED", "SHARED_CONTAINER_SANDBOX_RUNTIME"},
		Images:       map[string]string{}, Artifacts: artifactPath,
	}
	defer func() {
		if artifactErr := a.collectArtifacts(context.Background(), cfg, artifactPath, &report); artifactErr != nil && returnErr == nil {
			returnErr = artifactErr
		}
	}()

	clients, err := clientsFor(cfg.Spec.KubeContext)
	if err != nil {
		return err
	}
	add := func(name string, err error) {
		check := VerifyCheck{Name: name, Status: "pass", Message: "ok"}
		if err != nil {
			check.Status = "fail"
			check.Message = err.Error()
			report.Passed = false
		}
		report.Checks = append(report.Checks, check)
	}

	statusBuffer := &strings.Builder{}
	statusApp := *a
	statusApp.stdout = statusBuffer
	statusErr := statusApp.status(ctx, cfg, "json")
	if statusErr == nil {
		var statusReport StatusReport
		if err := json.Unmarshal([]byte(statusBuffer.String()), &statusReport); err != nil {
			statusErr = fmt.Errorf("decode status report: %w", err)
		} else if !statusReport.Ready {
			statusErr = fmt.Errorf("one or more workloads are not ready")
		}
	}
	add("workload-readiness", statusErr)

	for _, service := range []struct {
		namespace, name string
		port            int
	}{
		{cfg.Spec.Namespaces.System, "argus-server", 8080},
		{cfg.Spec.Namespaces.System, "argus-connector-gateway", 8081},
		{cfg.Spec.Namespaces.System, "argus-telemetry-ingest", 8081},
		{cfg.Spec.Namespaces.System, "argus-telemetry-query", 8081},
	} {
		add("health/"+service.name, a.httpProbe(ctx, cfg, service.namespace, service.name, service.port))
	}
	workerDeployments := append(expectedWorkerDeployments(cfg.Spec.Profile), "argus-direct-executor")
	for _, deployment := range workerDeployments {
		add("health/"+deployment, a.podHealthProbe(ctx, cfg, clients, cfg.Spec.Namespaces.System, deployment, 8081))
	}

	postgresSQL := `set -eu; export PGPASSWORD="$POSTGRES_PASSWORD"; psql -U argus -d argus -v ON_ERROR_STOP=1 -Atc "BEGIN; INSERT INTO argus_installation_checks(id,value) VALUES ('argus-e2e','postgres-ok') ON CONFLICT (id) DO UPDATE SET value=EXCLUDED.value; COMMIT; SELECT value FROM argus_installation_checks WHERE id='argus-e2e'; DELETE FROM argus_installation_checks WHERE id='argus-e2e';" | grep postgres-ok`
	add("postgresql-write-read", a.execBySelector(ctx, cfg, clients, cfg.Spec.Namespaces.System, "app.kubernetes.io/name=argus-postgresql", "postgresql", postgresSQL))
	redisCommand := `set -eu; redis-cli -a "$REDIS_PASSWORD" SET argus:e2e redis-ok >/dev/null; test "$(redis-cli -a "$REDIS_PASSWORD" GET argus:e2e)" = redis-ok; redis-cli -a "$REDIS_PASSWORD" DEL argus:e2e >/dev/null; test -z "$(redis-cli -a "$REDIS_PASSWORD" GET argus:e2e)"`
	add("redis-write-read", a.execBySelector(ctx, cfg, clients, cfg.Spec.Namespaces.System, "app.kubernetes.io/name=argus-redis", "redis", redisCommand))
	add("minio-object-roundtrip", a.runSmokePod(ctx, cfg, clients, cfg.Spec.Namespaces.System, "minio", minioSmokePod(cfg)))
	add("kafka-produce-consume", a.kafkaSmoke(ctx, cfg, clients))
	add("clickhouse-write-read", a.runSmokePod(ctx, cfg, clients, cfg.Spec.Namespaces.Observability, "clickhouse", clickHouseSmokePod(cfg)))
	add("opensandbox-lifecycle", a.openSandboxSmoke(ctx, cfg, clients))

	for _, image := range []string{"argus-backend", "argus-web", "minio"} {
		imageReference := cfg.Image(image)
		if cfg.Spec.Images.Mode == "local-registry" {
			imageReference = localRegistryReference(cfg, image)
		}
		inspect, inspectErr := a.runner.quiet(ctx, "docker", "manifest", "inspect", "--insecure", imageReference)
		if inspectErr != nil {
			report.Images[image] = "unavailable: " + inspectErr.Error()
			continue
		}
		var manifest struct {
			Config struct {
				Digest string `json:"digest"`
			} `json:"config"`
		}
		if json.Unmarshal([]byte(inspect), &manifest) == nil && manifest.Config.Digest != "" {
			report.Images[image] = manifest.Config.Digest
		} else {
			report.Images[image] = "manifest-present"
		}
	}
	sort.Slice(report.Checks, func(i, j int) bool { return report.Checks[i].Name < report.Checks[j].Name })
	if err := writeOutput(a.stdout, output, report, func(w io.Writer) {
		_, _ = fmt.Fprintf(w, "Argus %s verification passed=%t\n", report.ReleaseID, report.Passed)
		for _, check := range report.Checks {
			_, _ = fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
		}
		_, _ = fmt.Fprintf(w, "Artifacts: %s\n", report.Artifacts)
	}); err != nil {
		return err
	}
	if !report.Passed {
		return fmt.Errorf("one or more verification checks failed")
	}
	return nil
}

func (a *App) httpProbe(ctx context.Context, cfg *InstallConfig, namespace, service string, port int) error {
	name := kubernetesName("argus-probe-" + cfg.Spec.ReleaseID + "-" + service)
	command := fmt.Sprintf("wget -qO- http://%s:%d/healthz | grep -q ok; wget -qO- http://%s:%d/readyz | grep -q ready", service, port, service, port)
	manifest := genericSmokePod(name, namespace, "busybox:1.37.0", nil, command)
	return a.runPodManifest(ctx, cfg, namespace, name, manifest)
}

func (a *App) podHealthProbe(ctx context.Context, cfg *InstallConfig, clients *kubeClients, namespace, deployment string, port int) error {
	pods, err := clients.typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=" + deployment})
	if err != nil || len(pods.Items) == 0 {
		return fmt.Errorf("no pod for %s", deployment)
	}
	ip := pods.Items[0].Status.PodIP
	name := kubernetesName("argus-probe-" + cfg.Spec.ReleaseID + "-" + deployment)
	command := fmt.Sprintf("wget -qO- http://%s:%d/healthz | grep -q ok; wget -qO- http://%s:%d/readyz | grep -q ready", ip, port, ip, port)
	return a.runPodManifest(ctx, cfg, namespace, name, genericSmokePod(name, namespace, "busybox:1.37.0", nil, command))
}

func (a *App) execBySelector(ctx context.Context, cfg *InstallConfig, clients *kubeClients, namespace, selector, container, command string) error {
	pods, err := clients.typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil || len(pods.Items) == 0 {
		return fmt.Errorf("no pod matching %s", selector)
	}
	_, err = a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", namespace, "exec", pods.Items[0].Name, "--container", container, "--", "sh", "-ec", command)
	return err
}

func (a *App) kafkaSmoke(ctx context.Context, cfg *InstallConfig, clients *kubeClients) error {
	pods, err := clients.typed.CoreV1().Pods(cfg.Spec.Namespaces.Observability).List(ctx, metav1.ListOptions{LabelSelector: "strimzi.io/cluster=argus-kafka,strimzi.io/component-type=kafka"})
	if err != nil || len(pods.Items) == 0 {
		pods, err = clients.typed.CoreV1().Pods(cfg.Spec.Namespaces.Observability).List(ctx, metav1.ListOptions{LabelSelector: "strimzi.io/cluster=argus-kafka"})
	}
	if err != nil || len(pods.Items) == 0 {
		return fmt.Errorf("Kafka broker pod not found")
	}
	command := `set -eu; message="argus-e2e-$(date +%s)"; printf '%s\n' "$message" | bin/kafka-console-producer.sh --bootstrap-server argus-kafka-kafka-bootstrap:9092 --topic argus-installation-check; timeout 30 bin/kafka-console-consumer.sh --bootstrap-server argus-kafka-kafka-bootstrap:9092 --topic argus-installation-check --from-beginning --max-messages 20 | grep -q "$message"`
	_, err = a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability, "exec", pods.Items[0].Name, "--container", "kafka", "--", "sh", "-ec", command)
	return err
}

func (a *App) openSandboxSmoke(ctx context.Context, cfg *InstallConfig, clients *kubeClients) error {
	name := kubernetesName("argus-opensandbox-smoke-" + cfg.Spec.ReleaseID)
	manifest := openSandboxSmokePod(cfg, name)
	return a.runPodManifest(ctx, cfg, cfg.Spec.Namespaces.Sandbox, name, manifest)
}

func (a *App) runSmokePod(ctx context.Context, cfg *InstallConfig, clients *kubeClients, namespace, suffix, manifest string) error {
	name := kubernetesName("argus-" + suffix + "-smoke-" + cfg.Spec.ReleaseID)
	return a.runPodManifest(ctx, cfg, namespace, name, manifest)
}

func (a *App) runPodManifest(ctx context.Context, cfg *InstallConfig, namespace, name, manifest string) error {
	_, _ = a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", namespace, "delete", "pod", name, "--ignore-not-found=true", "--wait=true")
	if _, err := a.runner.quietInput(ctx, strings.NewReader(manifest), "kubectl", "--context", cfg.Spec.KubeContext, "apply", "--filename", "-"); err != nil {
		return err
	}
	defer func() {
		_, _ = a.runner.quiet(context.Background(), "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", namespace, "delete", "pod", name, "--ignore-not-found=true", "--wait=false")
	}()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		phase, err := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", namespace, "get", "pod/"+name, "--output=jsonpath={.status.phase}")
		if err != nil {
			return err
		}
		switch strings.TrimSpace(phase) {
		case "Succeeded":
			return nil
		case "Failed":
			logs, _ := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", namespace, "logs", name)
			return fmt.Errorf("smoke pod %s failed: %s", name, strings.TrimSpace(logs))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	logs, _ := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", namespace, "logs", name)
	return fmt.Errorf("smoke pod %s did not finish within 5m: %s", name, strings.TrimSpace(logs))
}

func genericSmokePod(name, namespace, image string, envYAML []string, command string) string {
	env := ""
	if len(envYAML) > 0 {
		env = "\n      env:\n" + strings.Join(envYAML, "\n")
	}
	command = strings.ReplaceAll(command, "\n", "\n          ")
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels: {app.kubernetes.io/part-of: argus}
spec:
  restartPolicy: Never
  containers:
    - name: smoke
      image: %s%s
      command: ["/bin/sh", "-ec"]
      args:
        - |
          %s
`, name, namespace, image, env, command)
}

func minioSmokePod(cfg *InstallConfig) string {
	name := kubernetesName("argus-minio-smoke-" + cfg.Spec.ReleaseID)
	env := []string{
		"          - name: MINIO_ROOT_USER\n            valueFrom: {secretKeyRef: {name: argus-data-credentials, key: minio-root-user}}",
		"          - name: MINIO_ROOT_PASSWORD\n            valueFrom: {secretKeyRef: {name: argus-data-credentials, key: minio-root-password}}",
	}
	command := `mc alias set argus http://argus-minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"; printf argus-e2e >/tmp/value; mc mb --ignore-existing argus/argus-e2e; mc cp /tmp/value argus/argus-e2e/value; test "$(mc cat argus/argus-e2e/value)" = argus-e2e`
	return genericSmokePod(name, cfg.Spec.Namespaces.System, "minio/mc:RELEASE.2025-08-13T08-35-41Z", env, command)
}

func clickHouseSmokePod(cfg *InstallConfig) string {
	name := kubernetesName("argus-clickhouse-smoke-" + cfg.Spec.ReleaseID)
	env := []string{"          - name: CLICKHOUSE_PASSWORD\n            valueFrom: {secretKeyRef: {name: argus-clickhouse-credentials, key: password}}"}
	command := `clickhouse-client --host argus-clickhouse-client --user argus --password "$CLICKHOUSE_PASSWORD" --multiquery "CREATE TABLE IF NOT EXISTS argus.e2e_persistence (id String, value String) ENGINE=ReplacingMergeTree ORDER BY id; INSERT INTO argus.e2e_persistence VALUES ('argus-e2e','clickhouse-ok');"; clickhouse-client --host argus-clickhouse-client --user argus --password "$CLICKHOUSE_PASSWORD" --query "SELECT value FROM argus.e2e_persistence FINAL WHERE id='argus-e2e'" | grep -q clickhouse-ok`
	return genericSmokePod(name, cfg.Spec.Namespaces.Observability, "clickhouse/clickhouse-server:26.3.17.110-alpine", env, command)
}

func openSandboxSmokePod(cfg *InstallConfig, name string) string {
	env := []string{"          - name: OPENSANDBOX_API_KEY\n            valueFrom: {secretKeyRef: {name: argus-opensandbox-api, key: api-key}}"}
	// The server API is exercised through its published Python SDK, including create, exec and delete.
	command := `python - <<'PY'
import asyncio, os
from opensandbox.sandbox import Sandbox
from opensandbox.config import ConnectionConfig
async def main():
    config = ConnectionConfig(
        domain="opensandbox-server",
        api_key=os.environ["OPENSANDBOX_API_KEY"],
        use_server_proxy=True,
    )
    sandbox = await Sandbox.create("ubuntu:latest", connection_config=config)
    execution = await sandbox.commands.run("echo argus-e2e")
    assert "argus-e2e" in execution.logs.stdout[0].text
    await sandbox.kill()
asyncio.run(main())
PY`
	return genericSmokePod(name, cfg.Spec.Namespaces.Sandbox, "python:3.13-alpine", env, "pip install --no-cache-dir opensandbox==0.1.15 >/dev/null; "+command)
}

func (a *App) collectArtifacts(ctx context.Context, cfg *InstallConfig, directory string, report *VerifyReport) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if report != nil {
		data, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(filepath.Join(directory, "verify.json"), append(data, '\n'), 0o600)
	}
	commands := []struct {
		name string
		args []string
	}{
		{"pods-wide.txt", []string{"--context", cfg.Spec.KubeContext, "get", "pods", "--all-namespaces", "--selector", "app.kubernetes.io/part-of=argus", "--output", "wide"}},
		{"events.txt", []string{"--context", cfg.Spec.KubeContext, "get", "events", "--all-namespaces", "--sort-by=.lastTimestamp"}},
		{"pvc.txt", []string{"--context", cfg.Spec.KubeContext, "get", "pvc", "--all-namespaces", "--output", "wide"}},
		{"kafka.json", []string{"--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability, "get", "kafka,kafkanodepool,kafkatopic", "--output", "json"}},
		{"clickhouse.json", []string{"--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability, "get", "clickhouseinstallation", "--output", "json"}},
	}
	for _, command := range commands {
		output, _ := a.runner.quiet(ctx, "kubectl", command.args...)
		_ = os.WriteFile(filepath.Join(directory, command.name), []byte(output), 0o600)
	}
	for _, namespace := range []string{cfg.Spec.Namespaces.System, cfg.Spec.Namespaces.Sandbox, cfg.Spec.Namespaces.Observability} {
		logs, _ := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", namespace, "logs", "--selector", "app.kubernetes.io/part-of=argus", "--all-containers=true", "--prefix=true", "--tail=1000")
		_ = os.WriteFile(filepath.Join(directory, "logs-"+namespace+".txt"), []byte(logs), 0o600)
	}
	configData, _ := os.ReadFile(cfg.path)
	_ = os.WriteFile(filepath.Join(directory, "install-config.yaml"), configData, 0o600)
	return nil
}
