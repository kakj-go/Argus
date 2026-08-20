package argusctl

import (
	"bytes"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var splitWorkerDeployments = []string{
	"argus-worker-agent",
	"argus-worker-action",
	"argus-worker-compaction",
	"argus-worker-automation",
	"argus-worker-sandbox",
}

func TestLocalHardeningInstallerValuesRenderCharts(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "local-hardening.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	credentials := localHardeningTestCredentials()
	tests := []struct {
		name      string
		chartName string
		namespace string
		values    map[string]any
	}{
		{name: "data", chartName: "argus-data", namespace: cfg.Spec.Namespaces.System, values: dataValues(cfg, credentials)},
		{name: "platform", chartName: "argus-platform", namespace: cfg.Spec.Namespaces.System, values: platformValues(cfg, credentials, "setup-secret", "idempotency", "cursor", "pending", "")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded, err := loadLocalChart(root, test.chartName)
			if err != nil {
				t.Fatal(err)
			}
			configuration := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
			install := action.NewInstall(configuration)
			install.ReleaseName = "argus-local-render"
			install.Namespace = test.namespace
			install.DryRunStrategy = action.DryRunClient
			if _, err := install.Run(loaded, test.values); err != nil {
				t.Fatalf("render %s chart with installer values: %v", test.chartName, err)
			}
		})
	}
}

func TestPlatformChartAllowsTelemetryToBeDisabled(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "local-hardening.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "")
	runtimeValues := values["runtime"].(map[string]any)
	runtimeValues["telemetryToolCatalogEnabled"] = false
	runtimeValues["otelcolLinuxArm64Uri"] = ""
	runtimeValues["otelcolLinuxArm64Sha256"] = ""
	runtimeValues["otelcolLinuxArm64Signature"] = ""
	runtimeValues["otelcolLinuxArm64ByteSize"] = 0
	runtimeValues["otelcolSigningKeyId"] = ""
	runtimeValues["otelcolSigningPublicKey"] = ""

	loaded, err := loadLocalChart(root, "argus-platform")
	if err != nil {
		t.Fatal(err)
	}
	configuration := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
	install := action.NewInstall(configuration)
	install.ReleaseName = "argus-no-telemetry-render"
	install.Namespace = cfg.Spec.Namespaces.System
	install.DryRunStrategy = action.DryRunClient
	rendered, err := install.Run(loaded, values)
	if err != nil {
		t.Fatalf("render platform chart with telemetry disabled: %v", err)
	}
	accessor, err := release.NewAccessor(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(accessor.Manifest(), "argus-telemetry-catalog-sync") ||
		strings.Contains(accessor.Manifest(), "argus-telemetry-ingest") ||
		strings.Contains(accessor.Manifest(), "argus-server-telemetry-client-tls") {
		t.Fatal("telemetry resources rendered while telemetry was disabled")
	}
}

func TestExpectedWorkerDeployments(t *testing.T) {
	tests := []struct {
		profile string
		want    []string
	}{
		{profile: "evaluation", want: []string{"argus-worker"}},
		{profile: "local-hardening", want: splitWorkerDeployments},
		{profile: "production", want: splitWorkerDeployments},
	}
	for _, test := range tests {
		t.Run(test.profile, func(t *testing.T) {
			if got := expectedWorkerDeployments(test.profile); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expectedWorkerDeployments(%q) = %v, want %v", test.profile, got, test.want)
			}
		})
	}
}

func TestPlatformWorkerTopologyByProfile(t *testing.T) {
	profiles := []string{"evaluation", "local-hardening", "production"}
	for _, profile := range profiles {
		t.Run(profile, func(t *testing.T) {
			resources := renderPlatformResources(t, profile)
			deployments := resourcesByKind(resources, "Deployment")

			directExecutor := requireResource(t, deployments, "argus-direct-executor")
			assertContainerArgs(t, directExecutor, []any{"--pool=direct-executor"})
			if profile == "evaluation" {
				worker := requireResource(t, deployments, "argus-worker")
				assertContainerArgs(t, worker, []any{"--pool=default"})
				assertContainerResources(t, worker, map[string]any{
					"limits":   map[string]any{"cpu": "2", "memory": "1Gi"},
					"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
				})
				for _, name := range splitWorkerDeployments {
					if _, ok := deployments[name]; ok {
						t.Fatalf("split Worker Deployment %s rendered for evaluation", name)
					}
				}
				return
			}

			if _, ok := deployments["argus-worker"]; ok {
				t.Fatalf("consolidated Worker Deployment rendered for %s", profile)
			}
			for index, name := range splitWorkerDeployments {
				requireResource(t, deployments, name)
				pool := strings.TrimPrefix(splitWorkerDeployments[index], "argus-worker-")
				assertContainerArgs(t, deployments[name], []any{"--pool=" + pool})
			}
		})
	}
}

func TestEvaluationWorkerNetworkPolicyHasDependencyUnion(t *testing.T) {
	resources := renderPlatformResources(t, "evaluation")
	policies := resourcesByKind(resources, "NetworkPolicy")
	worker := requireResource(t, policies, "argus-worker")

	ports := networkPolicyEgressPorts(t, worker)
	for _, port := range []int64{53, 80, 443, 5432, 6379, 8080, 9443, 9444, 9447} {
		if !ports[port] {
			t.Errorf("evaluation Worker NetworkPolicy does not allow required port %d", port)
		}
	}
	for _, target := range []string{
		"argus-postgresql",
		"argus-redis",
		"argus-direct-executor",
		"argus-connector-gateway",
		"argus-telemetry-query",
	} {
		if !networkPolicyTargetsPod(worker, target) {
			t.Errorf("evaluation Worker NetworkPolicy does not target %s", target)
		}
	}

	telemetry := requireResource(t, policies, "argus-telemetry-query")
	if !networkPolicyIngressAllowsPod(telemetry, "argus-worker") {
		t.Fatal("Telemetry Query ingress does not allow the evaluation Worker")
	}
}

func TestSplitWorkerNetworkPoliciesRemainProfileSpecific(t *testing.T) {
	for _, profile := range []string{"local-hardening", "production"} {
		t.Run(profile, func(t *testing.T) {
			policies := resourcesByKind(renderPlatformResources(t, profile), "NetworkPolicy")
			if _, ok := policies["argus-worker"]; ok {
				t.Fatal("consolidated Worker NetworkPolicy rendered for split topology")
			}
			for _, name := range splitWorkerDeployments {
				requireResource(t, policies, name)
			}
			if networkPolicyEgressPorts(t, requireResource(t, policies, "argus-worker-action"))[443] {
				t.Fatal("action Worker unexpectedly received model HTTPS egress")
			}
			if !networkPolicyEgressPorts(t, requireResource(t, policies, "argus-worker-sandbox"))[8080] {
				t.Fatal("sandbox Worker lost OpenSandbox egress")
			}
			telemetry := requireResource(t, policies, "argus-telemetry-query")
			for _, name := range []string{"argus-worker-agent", "argus-worker-automation"} {
				if !networkPolicyIngressAllowsPod(telemetry, name) {
					t.Fatalf("Telemetry Query ingress does not allow %s", name)
				}
			}
		})
	}
}

func renderPlatformResources(t *testing.T, profile string) []*unstructured.Unstructured {
	t.Helper()
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", profile+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadLocalChart(root, "argus-platform")
	if err != nil {
		t.Fatal(err)
	}
	configuration := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
	install := action.NewInstall(configuration)
	install.ReleaseName = "argus-" + profile + "-render"
	install.Namespace = cfg.Spec.Namespaces.System
	install.DryRunStrategy = action.DryRunClient
	rendered, err := install.Run(loaded, platformValues(
		cfg,
		localHardeningTestCredentials(),
		"setup-secret",
		"idempotency",
		"cursor",
		"pending",
		"0123456789abcdef0123456789abcdef",
	))
	if err != nil {
		t.Fatalf("render platform chart for %s: %v", profile, err)
	}
	accessor, err := release.NewAccessor(rendered)
	if err != nil {
		t.Fatal(err)
	}

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(accessor.Manifest()), 4096)
	var resources []*unstructured.Unstructured
	for {
		resource := &unstructured.Unstructured{}
		if err := decoder.Decode(resource); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("decode rendered manifest for %s: %v", profile, err)
		}
		if resource.GetKind() != "" {
			resources = append(resources, resource)
		}
	}
	return resources
}

func resourcesByKind(resources []*unstructured.Unstructured, kind string) map[string]*unstructured.Unstructured {
	result := make(map[string]*unstructured.Unstructured)
	for _, resource := range resources {
		if resource.GetKind() == kind {
			result[resource.GetName()] = resource
		}
	}
	return result
}

func requireResource(t *testing.T, resources map[string]*unstructured.Unstructured, name string) *unstructured.Unstructured {
	t.Helper()
	resource, ok := resources[name]
	if !ok {
		t.Fatalf("resource %s was not rendered", name)
	}
	return resource
}

func assertContainerArgs(t *testing.T, deployment *unstructured.Unstructured, want []any) {
	t.Helper()
	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) != 1 {
		t.Fatalf("read containers for %s: found=%v err=%v", deployment.GetName(), found, err)
	}
	container := containers[0].(map[string]any)
	if got := container["args"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s args = %v, want %v", deployment.GetName(), got, want)
	}
}

func assertContainerResources(t *testing.T, deployment *unstructured.Unstructured, want map[string]any) {
	t.Helper()
	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) != 1 {
		t.Fatalf("read containers for %s: found=%v err=%v", deployment.GetName(), found, err)
	}
	container := containers[0].(map[string]any)
	if got := container["resources"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s resources = %#v, want %#v", deployment.GetName(), got, want)
	}
}

func networkPolicyEgressPorts(t *testing.T, policy *unstructured.Unstructured) map[int64]bool {
	t.Helper()
	egress, found, err := unstructured.NestedSlice(policy.Object, "spec", "egress")
	if err != nil || !found {
		t.Fatalf("read egress for %s: found=%v err=%v", policy.GetName(), found, err)
	}
	ports := make(map[int64]bool)
	for _, rawRule := range egress {
		rule := rawRule.(map[string]any)
		for _, rawPort := range rule["ports"].([]any) {
			port := rawPort.(map[string]any)["port"]
			switch value := port.(type) {
			case int64:
				ports[value] = true
			case float64:
				ports[int64(value)] = true
			}
		}
	}
	return ports
}

func networkPolicyTargetsPod(policy *unstructured.Unstructured, name string) bool {
	egress, _, _ := unstructured.NestedSlice(policy.Object, "spec", "egress")
	for _, rawRule := range egress {
		rule := rawRule.(map[string]any)
		to, _ := rule["to"].([]any)
		for _, rawPeer := range to {
			peer := rawPeer.(map[string]any)
			podSelector, _ := peer["podSelector"].(map[string]any)
			matchLabels, _ := podSelector["matchLabels"].(map[string]any)
			if matchLabels["app.kubernetes.io/name"] == name {
				return true
			}
		}
	}
	return false
}

func networkPolicyIngressAllowsPod(policy *unstructured.Unstructured, name string) bool {
	ingress, _, _ := unstructured.NestedSlice(policy.Object, "spec", "ingress")
	for _, rawRule := range ingress {
		rule := rawRule.(map[string]any)
		from, _ := rule["from"].([]any)
		for _, rawPeer := range from {
			peer := rawPeer.(map[string]any)
			podSelector, _ := peer["podSelector"].(map[string]any)
			matchExpressions, _ := podSelector["matchExpressions"].([]any)
			for _, rawExpression := range matchExpressions {
				expression := rawExpression.(map[string]any)
				if expression["key"] != "app.kubernetes.io/name" {
					continue
				}
				for _, value := range expression["values"].([]any) {
					if value == name {
						return true
					}
				}
			}
		}
	}
	return false
}

func localHardeningTestCredentials() map[string]string {
	return map[string]string{
		"postgresql-password": "postgres", "redis-password": "redis", "minio-root-user": "minio", "minio-root-password": "minio-secret",
		"clickhouse-password": "clickhouse", "telemetry-clickhouse-migration-password": "ch-migration", "telemetry-clickhouse-writer-password": "ch-writer",
		"telemetry-clickhouse-query-password": "ch-query", "openbao-token": "openbao-token", "server-database-password": "server",
		"worker-database-password": "worker", "gateway-database-password": "gateway", "direct-executor-database-password": "direct",
		"migration-database-password": "migration", "telemetry-ingest-database-password": "ingest", "telemetry-writer-database-password": "writer",
	}
}
