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
	"argus-worker-sandbox",
}

func TestTelemetryCatalogByteSizesRemainDecimalAfterHelmValueRoundTrip(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadLocalChart(root, "argus-platform")
	if err != nil {
		t.Fatal(err)
	}
	values := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "0123456789abcdef0123456789abcdef", strings.Repeat("a", 64))
	runtimeValues := values["runtime"].(map[string]any)
	runtimeValues["otelcolLinuxArm64ByteSize"] = float64(61236274)
	runtimeValues["otelcolLinuxAmd64Uri"] = "https://artifacts.example/amd64.tar.gz"
	runtimeValues["otelcolLinuxAmd64Sha256"] = strings.Repeat("a", 64)
	runtimeValues["otelcolLinuxAmd64Signature"] = "signature"
	runtimeValues["otelcolLinuxAmd64ByteSize"] = float64(65985234)
	runtimeValues["otelcolWindowsAmd64Uri"] = "https://artifacts.example/windows.zip"
	runtimeValues["otelcolWindowsAmd64Sha256"] = strings.Repeat("b", 64)
	runtimeValues["otelcolWindowsAmd64Signature"] = "signature"
	runtimeValues["otelcolWindowsAmd64ByteSize"] = float64(65561794)
	configuration := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
	install := action.NewInstall(configuration)
	install.ReleaseName = "argus-byte-size-round-trip"
	install.Namespace = cfg.Spec.Namespaces.System
	install.DryRunStrategy = action.DryRunClient
	rendered, err := install.Run(loaded, values)
	if err != nil {
		t.Fatal(err)
	}
	accessor, err := release.NewAccessor(rendered)
	if err != nil {
		t.Fatal(err)
	}
	manifest := accessor.Manifest()
	for _, hook := range accessor.Hooks() {
		hookAccessor, hookErr := release.NewHookAccessor(hook)
		if hookErr != nil {
			t.Fatal(hookErr)
		}
		manifest += hookAccessor.Manifest()
	}
	for _, value := range []string{"61236274", "65985234", "65561794"} {
		if !strings.Contains(manifest, `value: "`+value+`"`) {
			t.Fatalf("catalog byte size %s was not rendered as a decimal integer", value)
		}
	}
	if strings.Contains(manifest, "e+07") {
		t.Fatal("catalog byte size rendered in scientific notation")
	}
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
		{name: "platform", chartName: "argus-platform", namespace: cfg.Spec.Namespaces.System, values: platformValues(cfg, credentials, "setup-secret", "idempotency", "cursor", "pending", "", strings.Repeat("a", 64))},
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

func TestInstallerProvidesRequiredObjectStoreBootstrapValues(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	credentials := localHardeningTestCredentials()
	data := dataValues(cfg, credentials)
	images := data["images"].(map[string]any)
	if got := images["minioClient"]; got != "minio/mc:RELEASE.2025-08-13T08-35-41Z" {
		t.Fatalf("minioClient = %v", got)
	}
	platform := platformValues(cfg, credentials, "setup-secret", "idempotency", "cursor", "pending", "secret-kek", strings.Repeat("a", 64))
	runtimeValues := platform["runtime"].(map[string]any)
	if got := runtimeValues["remoteOrigin"]; got != "https://argus.dev" {
		t.Fatalf("remoteOrigin = %v", got)
	}
	observabilityService := "argus-telemetry-ingest." + cfg.Spec.Namespaces.Observability + ".svc"
	for key, want := range map[string]string{
		"telemetryIngestGrpcEndpoint": "grpcs://" + observabilityService + ":4317",
		"telemetryIngestHttpEndpoint": "https://" + observabilityService + ":4318",
		"telemetryEnrollmentEndpoint": "https://" + observabilityService + ":4318/v1/identity/enroll",
	} {
		if got := runtimeValues[key]; got != want {
			t.Fatalf("%s = %v, want %s", key, got, want)
		}
	}
}

func TestPlatformValuesUseUnifiedDomainHosts(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "secret-kek", strings.Repeat("a", 64))
	runtimeValues := values["runtime"].(map[string]any)
	for key, want := range map[string]string{
		"remoteOrigin":                     "https://argus.dev",
		"connectorEnrollmentURL":           "https://argus.dev",
		"connectorGatewayAddress":          "grpcs://connector.argus.dev:9443",
		"connectorEnrollmentForwardTarget": "argus.dev:443",
		"connectorGatewayForwardTarget":    "argus-connector-gateway.argus-e2e-local-system.svc:9443",
	} {
		if got := runtimeValues[key]; got != want {
			t.Fatalf("%s = %v, want %s", key, got, want)
		}
	}
	if got := runtimeValues["secureCookies"]; got != true {
		t.Fatalf("secureCookies = %v, want true", got)
	}
	hosts := values["hosts"].(map[string]any)
	for key, want := range map[string]string{
		"enterprise": "argus.dev", "platform": "platform.argus.dev",
		"cards": "cards.argus.dev", "connector": "connector.argus.dev",
	} {
		if got := hosts[key]; got != want {
			t.Fatalf("hosts.%s = %v, want %s", key, got, want)
		}
	}
	if _, exists := hosts["remote"]; exists {
		t.Fatal("hosts must not carry a dedicated remote terminal domain")
	}
	pki := values["pki"].(map[string]any)
	if pki["mode"] != "managed" || pki["issuerKind"] != "ClusterIssuer" || pki["issuerGroup"] != "cert-manager.io" {
		t.Fatalf("pki values = %#v", pki)
	}
	if got, want := pki["issuerName"], cfg.Spec.ReleaseID+"-ca"; got != want {
		t.Fatalf("pki.issuerName = %v, want %s", got, want)
	}
}

func TestProductionPlatformValuesCarryExplicitTunnelCapacity(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "production.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	values := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "secret-kek", strings.Repeat("a", 64))
	production := values["production"].(map[string]any)
	capacity := production["directExecutor"].(map[string]any)
	if capacity["telemetryTunnelLimit"] != 64 || capacity["controlTunnelLimit"] != 32 || capacity["tunnelBytesPerSecond"] != int64(67108864) {
		t.Fatalf("unexpected production Direct Executor capacity: %#v", capacity)
	}
}

func TestPlatformMFARequirementIsExplicitAndDefaultsOff(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	runtimeValues := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "secret-kek", strings.Repeat("a", 64))["runtime"].(map[string]any)
	if got := runtimeValues["platformMfaRequired"]; got != false {
		t.Fatalf("platformMfaRequired = %v, want false", got)
	}

	cfg.Spec.Security.PlatformMFARequired = true
	runtimeValues = platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "secret-kek", strings.Repeat("a", 64))["runtime"].(map[string]any)
	if got := runtimeValues["platformMfaRequired"]; got != true {
		t.Fatalf("platformMfaRequired = %v, want true", got)
	}
}

func TestAllowedOriginsAreHttpsDomainsOnly(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"evaluation", "local-hardening"} {
		t.Run(profile, func(t *testing.T) {
			cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", profile+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			values := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "secret-kek", strings.Repeat("a", 64))
			runtimeValues := values["runtime"].(map[string]any)
			want := []any{
				"https://argus.dev",
				"https://platform.argus.dev",
				"https://cards.argus.dev",
			}
			if got := runtimeValues["allowedOrigins"]; !reflect.DeepEqual(got, want) {
				t.Fatalf("allowedOrigins = %#v, want %#v", got, want)
			}
		})
	}
}

func TestConnectorGatewayHasDedicatedLoadBalancerService(t *testing.T) {
	for _, profile := range []string{"evaluation", "local-hardening", "production"} {
		t.Run(profile, func(t *testing.T) {
			services := resourcesByKind(renderPlatformResources(t, profile), "Service")
			connector := requireResource(t, services, "argus-connector-gateway")
			if connector.Object["spec"].(map[string]any)["type"] != "ClusterIP" {
				t.Fatalf("internal connector Service is not ClusterIP for %s", profile)
			}
			public := requireResource(t, services, "argus-connector-gateway-public")
			spec := public.Object["spec"].(map[string]any)
			if spec["type"] != "LoadBalancer" {
				t.Fatalf("public connector Service is not LoadBalancer for %s", profile)
			}
			ports := spec["ports"].([]any)
			if len(ports) != 1 || ports[0].(map[string]any)["port"] != int64(9443) {
				t.Fatalf("public connector Service must expose only 9443, got %#v", ports)
			}
		})
	}
}

func TestIngressRendersUnifiedHostsWithTLS(t *testing.T) {
	resources := renderPlatformResources(t, "evaluation")
	ingress := requireResource(t, resourcesByKind(resources, "Ingress"), "argus-web")
	spec := ingress.Object["spec"].(map[string]any)
	rules := spec["rules"].([]any)
	hosts := map[string]bool{}
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		hosts[rule["host"].(string)] = true
	}
	// The remote-access WSS endpoint shares the enterprise origin instead of
	// a dedicated terminal domain.
	for _, want := range []string{"argus.dev", "platform.argus.dev", "cards.argus.dev", "artifacts.argus.dev"} {
		if !hosts[want] {
			t.Fatalf("Ingress rule for %s missing", want)
		}
	}
	if hosts["remote.argus.dev"] {
		t.Fatal("Ingress must not expose a dedicated remote host")
	}
	tls := spec["tls"].([]any)
	if len(tls) != 4 {
		t.Fatalf("Ingress must terminate TLS with four independent secrets, got %#v", tls)
	}
	secrets := map[string]string{}
	for _, entry := range tls {
		item := entry.(map[string]any)
		hosts := item["hosts"].([]any)
		if len(hosts) != 1 {
			t.Fatalf("each Ingress TLS leaf must cover one host, got %#v", hosts)
		}
		secrets[hosts[0].(string)] = item["secretName"].(string)
	}
	for host, secret := range map[string]string{
		"argus.dev": "argus-enterprise-tls", "platform.argus.dev": "argus-platform-tls",
		"cards.argus.dev": "argus-cards-tls", "artifacts.argus.dev": "argus-artifact-tls",
	} {
		if secrets[host] != secret {
			t.Fatalf("TLS secret for %s = %q, want %q", host, secrets[host], secret)
		}
	}
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		if rule["host"] != "argus.dev" {
			continue
		}
		paths := rule["http"].(map[string]any)["paths"].([]any)
		wss := paths[0].(map[string]any)
		if wss["path"] != "/v1/sessions" {
			t.Fatalf("enterprise host must route /v1/sessions to the gateway, got %#v", paths)
		}
		backend := wss["backend"].(map[string]any)["service"].(map[string]any)
		if backend["name"] != "argus-connector-gateway" {
			t.Fatalf("remote WSS backend must target argus-connector-gateway, got %#v", backend)
		}
	}
	configMap := requireResource(t, resourcesByKind(resources, "ConfigMap"), "argus-web-runtime-config")
	data := configMap.Object["data"].(map[string]any)
	runtimeJSON := data["argus-runtime.json"].(string)
	if !strings.Contains(runtimeJSON, `"cardOrigin": "https://cards.argus.dev"`) || !strings.Contains(runtimeJSON, `"platformLoginUrl": "https://platform.argus.dev/login"`) {
		t.Fatalf("runtime config JSON = %q", runtimeJSON)
	}
}

func TestManagedPKIRendersOnlyOneSteadyStateClusterIssuer(t *testing.T) {
	resources := renderPlatformResources(t, "evaluation")
	issuers := resourcesByKind(resources, "ClusterIssuer")
	if len(issuers) != 1 {
		t.Fatalf("managed PKI rendered %d ClusterIssuers, want 1: %#v", len(issuers), issuers)
	}
	requireResource(t, issuers, "argus-e2e-local-ca")
	bundles := resourcesByKind(resources, "Bundle")
	requireResource(t, bundles, "argus-e2e-local-trust-bundle")
}

func TestEveryArgusCertificateUsesUniqueSinglePurposeLeaf(t *testing.T) {
	resources := renderPlatformResources(t, "evaluation")
	certificates := resourcesByKind(resources, "Certificate")
	if len(certificates) < 12 {
		t.Fatalf("rendered only %d Argus Certificates", len(certificates))
	}
	secrets := map[string]string{}
	for name, certificate := range certificates {
		spec := certificate.Object["spec"].(map[string]any)
		secret, _ := spec["secretName"].(string)
		if secret == "" {
			t.Fatalf("Certificate %s has no Secret", name)
		}
		if owner := secrets[certificate.GetNamespace()+"/"+secret]; owner != "" {
			t.Fatalf("Certificates %s and %s share Secret %s", owner, name, secret)
		}
		secrets[certificate.GetNamespace()+"/"+secret] = name
		issuer := spec["issuerRef"].(map[string]any)
		if issuer["name"] != "argus-e2e-local-ca" || issuer["kind"] != "ClusterIssuer" || issuer["group"] != "cert-manager.io" {
			t.Fatalf("Certificate %s uses a non-global Issuer: %#v", name, issuer)
		}
		usages := spec["usages"].([]any)
		if len(usages) != 1 || (usages[0] != "server auth" && usages[0] != "client auth") {
			t.Fatalf("Certificate %s is not single-purpose: %#v", name, usages)
		}
		if usages[0] == "client auth" {
			uris, _ := spec["uris"].([]any)
			if len(uris) != 1 || !strings.HasPrefix(uris[0].(string), "spiffe://argus.io/") || spec["duration"] != "24h" || spec["renewBefore"] != "8h" {
				t.Fatalf("client Certificate %s has invalid URI or lifetime: %#v", name, spec)
			}
		} else if spec["duration"] != "2160h" || spec["renewBefore"] != "360h" {
			t.Fatalf("server Certificate %s has invalid lifetime: %#v", name, spec)
		}
	}
	for _, expected := range []string{
		"argus-server-direct-executor-client", "argus-worker-direct-executor-client",
		"argus-connector-gateway-direct-executor-client", "argus-connector-gateway-peer-client",
		"argus-server-telemetry-client", "argus-worker-telemetry-client",
	} {
		requireResource(t, certificates, expected)
	}

	deployments := resourcesByKind(resources, "Deployment")
	assertDeploymentSecret(t, requireResource(t, deployments, "argus-server"), "direct-executor-client", "argus-server-direct-executor-client-tls")
	assertDeploymentSecret(t, requireResource(t, deployments, "argus-server"), "telemetry-client", "argus-server-telemetry-client-tls")
	assertDeploymentSecret(t, requireResource(t, deployments, "argus-worker"), "direct-executor-client", "argus-worker-direct-executor-client-tls")
	assertDeploymentSecret(t, requireResource(t, deployments, "argus-worker"), "telemetry-client", "argus-worker-telemetry-client-tls")
}

func assertDeploymentSecret(t *testing.T, deployment *unstructured.Unstructured, volumeName, secretName string) {
	t.Helper()
	volumes, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "volumes")
	if err != nil || !found {
		t.Fatalf("read Deployment %s volumes: found=%v err=%v", deployment.GetName(), found, err)
	}
	for _, raw := range volumes {
		volume := raw.(map[string]any)
		if volume["name"] != volumeName {
			continue
		}
		if volume["secret"].(map[string]any)["secretName"] != secretName {
			t.Fatalf("Deployment %s volume %s uses %#v, want %s", deployment.GetName(), volumeName, volume["secret"], secretName)
		}
		return
	}
	t.Fatalf("Deployment %s lacks volume %s", deployment.GetName(), volumeName)
}

func TestPKIControllerRBACSupportsStagedLeafCutover(t *testing.T) {
	resources := renderPlatformResources(t, "evaluation")
	runtimeConfig := requireResource(t, resourcesByKind(resources, "ConfigMap"), "argus-runtime-config")
	if runtimeConfig.GetLabels()["argus.io/release-id"] != "argus-e2e-local" {
		t.Fatal("runtime issuer ConfigMap is not protected by release ownership metadata")
	}
	telemetryIngest := requireResource(t, resourcesByKind(resources, "Deployment"), "argus-telemetry-ingest")
	if telemetryIngest.GetLabels()["argus.io/release-id"] != "argus-e2e-local" {
		t.Fatal("telemetry identity issuer workload is not protected by release ownership metadata")
	}
	reader := requireResource(t, resourcesByKind(resources, "ClusterRole"), "argus-e2e-local-pki-target-issuer-reader")
	rules, found, err := unstructured.NestedSlice(reader.Object, "rules")
	if err != nil || !found || len(rules) != 1 {
		t.Fatalf("read target issuer reader rules: found=%v err=%v rules=%#v", found, err, rules)
	}
	rule := rules[0].(map[string]any)
	if !reflect.DeepEqual(rule["resourceNames"], []any{"argus-e2e-local-ca"}) || !reflect.DeepEqual(rule["verbs"], []any{"get"}) {
		t.Fatalf("target issuer reader must be restricted to the steady issuer: %#v", rule)
	}

	wantNamespaces := map[string]bool{
		"argus-e2e-local-system":        false,
		"argus-e2e-local-observability": false,
		"argus-e2e-local-sandbox":       false,
	}
	for _, resource := range resources {
		if resource.GetKind() != "Role" || resource.GetName() != "argus-pki-controller-material" {
			continue
		}
		if _, ok := wantNamespaces[resource.GetNamespace()]; !ok {
			t.Fatalf("PKI material role rendered in unexpected namespace %q", resource.GetNamespace())
		}
		wantNamespaces[resource.GetNamespace()] = true
		rules, found, err := unstructured.NestedSlice(resource.Object, "rules")
		if err != nil || !found {
			t.Fatalf("read PKI material rules in %s: found=%v err=%v", resource.GetNamespace(), found, err)
		}
		assertPKIMaterialRule(t, rules, "", "secrets", []any{"get", "update", "delete"})
		assertPKIMaterialRule(t, rules, "", "pods", []any{"list"})
		assertPKIMaterialRule(t, rules, "cert-manager.io", "certificates", []any{"get", "list", "update", "delete"})
	}
	for namespace, found := range wantNamespaces {
		if !found {
			t.Errorf("PKI material role missing from namespace %s", namespace)
		}
	}
}

func assertPKIMaterialRule(t *testing.T, rules []any, apiGroup, resource string, verbs []any) {
	t.Helper()
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		if !reflect.DeepEqual(rule["apiGroups"], []any{apiGroup}) || !reflect.DeepEqual(rule["resources"], []any{resource}) {
			continue
		}
		if !reflect.DeepEqual(rule["verbs"], verbs) {
			t.Fatalf("%s rule verbs = %#v, want %#v", resource, rule["verbs"], verbs)
		}
		return
	}
	t.Fatalf("missing PKI material rule for %s/%s", apiGroup, resource)
}

func TestLocalHardeningOpenBaoRunsAsNonRootWithoutConfigChown(t *testing.T) {
	resources := renderDataResources(t, "local-hardening")
	statefulSet := requireResource(t, resourcesByKind(resources, "StatefulSet"), "argus-openbao")
	containers, found, err := unstructured.NestedSlice(statefulSet.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) < 1 {
		t.Fatalf("read OpenBao containers: found=%v err=%v", found, err)
	}
	container := containers[0].(map[string]any)
	securityContext := container["securityContext"].(map[string]any)
	if securityContext["runAsNonRoot"] != true || securityContext["runAsUser"] != int64(1000) || securityContext["runAsGroup"] != int64(1000) {
		t.Fatalf("OpenBao security context = %#v", securityContext)
	}
	environment := container["env"].([]any)
	if len(environment) != 1 || !reflect.DeepEqual(environment[0], map[string]any{"name": "SKIP_CHOWN", "value": "true"}) {
		t.Fatalf("OpenBao environment = %#v", environment)
	}
}

func TestLocalHardeningPostgreSQLRoleInitIsReadableAfterUserDrop(t *testing.T) {
	resources := renderDataResources(t, "local-hardening")
	statefulSet := requireResource(t, resourcesByKind(resources, "StatefulSet"), "argus-postgresql")
	volumes, found, err := unstructured.NestedSlice(statefulSet.Object, "spec", "template", "spec", "volumes")
	if err != nil || !found {
		t.Fatalf("read PostgreSQL volumes: found=%v err=%v", found, err)
	}
	for _, rawVolume := range volumes {
		volume := rawVolume.(map[string]any)
		if volume["name"] != "role-init" {
			continue
		}
		configMap := volume["configMap"].(map[string]any)
		if configMap["defaultMode"] != int64(0o555) {
			t.Fatalf("PostgreSQL role init defaultMode = %#v", configMap["defaultMode"])
		}
		return
	}
	t.Fatal("PostgreSQL role-init volume was not rendered")
}

func TestMinIOBucketInitStopsOptionParsingBeforeCredentials(t *testing.T) {
	resources := renderDataResources(t, "evaluation")
	job := requireResource(t, resourcesByKind(resources, "Job"), "argus-minio-bucket-init")
	containers, found, err := unstructured.NestedSlice(job.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) != 1 {
		t.Fatalf("read MinIO bucket init containers: found=%v err=%v", found, err)
	}
	container := containers[0].(map[string]any)
	args := container["args"].([]any)
	if len(args) != 1 || !strings.Contains(args[0].(string), `mc alias set -- argus`) {
		t.Fatalf("MinIO bucket init command does not stop option parsing: %#v", args)
	}
	for _, bucket := range []string{"argus-collector-artifacts", "argus-connector-artifacts"} {
		if !strings.Contains(args[0].(string), "mc anonymous set download argus/"+bucket) {
			t.Fatalf("MinIO bucket init does not publish %s for signed downloads: %#v", bucket, args)
		}
	}
}

func TestSetupTokenIsNeverMountedIntoWeb(t *testing.T) {
	for _, profile := range []string{"evaluation", "local-hardening", "production"} {
		t.Run(profile, func(t *testing.T) {
			deployments := resourcesByKind(renderPlatformResources(t, profile), "Deployment")
			web := requireResource(t, deployments, "argus-web")
			volumes, _, err := unstructured.NestedSlice(web.Object, "spec", "template", "spec", "volumes")
			if err != nil {
				t.Fatal(err)
			}
			for _, rawVolume := range volumes {
				volume := rawVolume.(map[string]any)
				if volume["name"] == "setup-token" {
					t.Fatal("setup-token must not be exposed to the web container")
				}
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
	values := platformValues(cfg, localHardeningTestCredentials(), "setup-secret", "idempotency", "cursor", "pending", "", strings.Repeat("a", 64))
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
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(accessor.Manifest()), 4096)
	for {
		var object unstructured.Unstructured
		if err = decoder.Decode(&object); err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch object.GetName() {
		case "argus-telemetry-catalog-sync", "argus-telemetry-ingest", "argus-server-telemetry-client-tls", "argus-worker-telemetry-client-tls":
			t.Fatalf("telemetry resource %s rendered while telemetry was disabled", object.GetName())
		}
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
			for _, name := range []string{"argus-worker-agent"} {
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
		strings.Repeat("a", 64),
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

func renderDataResources(t *testing.T, profile string) []*unstructured.Unstructured {
	t.Helper()
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", profile+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadLocalChart(root, "argus-data")
	if err != nil {
		t.Fatal(err)
	}
	configuration := action.NewConfiguration(action.ConfigurationSetLogger(slog.NewTextHandler(io.Discard, nil)))
	install := action.NewInstall(configuration)
	install.ReleaseName = "argus-" + profile + "-data-render"
	install.Namespace = cfg.Spec.Namespaces.System
	install.DryRunStrategy = action.DryRunClient
	rendered, err := install.Run(loaded, dataValues(cfg, localHardeningTestCredentials()))
	if err != nil {
		t.Fatalf("render data chart for %s: %v", profile, err)
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
			t.Fatalf("decode rendered data manifest for %s: %v", profile, err)
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
		"telemetry-query-database-password": "query",
	}
}
