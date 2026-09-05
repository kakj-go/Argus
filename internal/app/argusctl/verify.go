package argusctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	Network      *NetworkProfile   `json:"network,omitempty"`
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
		Degradations: []string{},
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
	networkProfile := discoverNetworkProfile(ctx, clients, cfg)
	report.Network = &networkProfile
	if networkProfile.SecurityPosture == "degraded" {
		report.Degradations = append(report.Degradations, networkProfile.Warnings...)
	}
	if cfg.Spec.OpenSandbox.RuntimeClassName == "" && cfg.Spec.OpenSandbox.AllowSharedRuntime {
		report.Degradations = append(report.Degradations, "SHARED_CONTAINER_SANDBOX_RUNTIME")
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
	trustBundlePath, trustBundleErr := materializeTrustBundle(ctx, clients, cfg)
	if trustBundlePath != "" {
		defer os.Remove(trustBundlePath)
	}
	add("pki/trust-bundle", trustBundleErr)

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
		{cfg.Spec.Namespaces.Observability, "argus-telemetry-ingest", 8081},
		{cfg.Spec.Namespaces.Observability, "argus-telemetry-query", 8081},
	} {
		add("health/"+service.name, a.httpProbe(ctx, cfg, service.namespace, service.name, service.port))
	}
	workerDeployments := append(expectedWorkerDeployments(cfg.Spec.Profile), "argus-direct-executor")
	for _, deployment := range workerDeployments {
		add("health/"+deployment, a.podHealthProbe(ctx, cfg, clients, cfg.Spec.Namespaces.System, deployment, 8081))
	}

	ingressHosts := webIngressHosts(cfg)
	ingressAddress, ingressErr := ingressProbeAddress(ctx, clients, cfg)
	add("ingress-ready", ingressErr)
	if ingressErr == nil {
		ingressAddress = selectIngressProbeAddress(ctx, cfg, ingressAddress)
	}
	add("ingress-certificates", ingressCertificatesReady(ctx, clients, cfg, ingressHosts))
	add("connector-lb", connectorLoadBalancerReady(ctx, clients, cfg))
	add("https-endpoint/enterprise", a.httpsProbe(ctx, cfg, cfg.Spec.Exposure.EnterpriseHost, ingressAddress, trustBundlePath))
	add("https-endpoint/platform", a.httpsProbe(ctx, cfg, cfg.Spec.Exposure.PlatformHost, ingressAddress, trustBundlePath))
	add("cors-origin", a.corsOriginProbe(ctx, cfg, cfg.Spec.Exposure.PlatformHost, ingressAddress, trustBundlePath))

	postgresSQL := `set -eu; export PGPASSWORD="$POSTGRES_PASSWORD"; psql -U argus -d argus -v ON_ERROR_STOP=1 -Atc "BEGIN; CREATE TEMP TABLE argus_installation_checks(id text PRIMARY KEY, value text); INSERT INTO argus_installation_checks(id,value) VALUES ('argus-e2e','postgres-ok'); SELECT value FROM argus_installation_checks WHERE id='argus-e2e'; COMMIT;" | grep postgres-ok`
	add("postgresql-write-read", a.execBySelector(ctx, cfg, clients, cfg.Spec.Namespaces.System, "app.kubernetes.io/name=argus-postgresql", "postgresql", postgresSQL))
	redisCommand := `set -eu; redis-cli -a "$REDIS_PASSWORD" SET argus:e2e redis-ok >/dev/null; test "$(redis-cli -a "$REDIS_PASSWORD" GET argus:e2e)" = redis-ok; redis-cli -a "$REDIS_PASSWORD" DEL argus:e2e >/dev/null; test -z "$(redis-cli -a "$REDIS_PASSWORD" GET argus:e2e)"`
	add("redis-write-read", a.execBySelector(ctx, cfg, clients, cfg.Spec.Namespaces.System, "app.kubernetes.io/name=argus-redis", "redis", redisCommand))
	add("minio-object-roundtrip", a.runSmokePod(ctx, cfg, clients, cfg.Spec.Namespaces.System, "minio", minioSmokePod(cfg)))
	add("kafka-produce-consume", a.kafkaSmoke(ctx, cfg, clients))
	add("clickhouse-write-read", a.runSmokePod(ctx, cfg, clients, cfg.Spec.Namespaces.Observability, "clickhouse", clickHouseSmokePod(cfg)))
	add("opensandbox-lifecycle", a.openSandboxSmoke(ctx, cfg, clients))

	for _, image := range []string{"argus-backend", "argus-web", "minio"} {
		digest, inspectErr := a.imageManifestDigest(ctx, cfg, image)
		if inspectErr != nil {
			report.Images[image] = "unavailable: " + inspectErr.Error()
			continue
		}
		report.Images[image] = digest
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

func (a *App) imageManifestDigest(ctx context.Context, cfg *InstallConfig, image string) (string, error) {
	if cfg.Spec.Images.Mode == "local-registry" {
		return localRegistryManifestDigest(ctx, cfg, image)
	}
	inspect, err := a.runner.quiet(ctx, "docker", "manifest", "inspect", cfg.Image(image))
	if err != nil {
		return "", err
	}
	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if json.Unmarshal([]byte(inspect), &manifest) == nil && manifest.Config.Digest != "" {
		return manifest.Config.Digest, nil
	}
	return "manifest-present", nil
}

func localRegistryManifestDigest(ctx context.Context, cfg *InstallConfig, image string) (string, error) {
	_, port, err := net.SplitHostPort(cfg.Spec.Images.Registry)
	if err != nil {
		return "", fmt.Errorf("parse local registry address: %w", err)
	}
	reference := strings.TrimPrefix(localRegistryReference(cfg, image), "localhost:"+port+"/")
	repository, tag, found := strings.Cut(reference, ":")
	if !found || repository == "" || tag == "" || strings.Contains(repository, "..") || strings.ContainsAny(tag, "/\\") {
		return "", fmt.Errorf("invalid local registry image reference %q", reference)
	}
	endpoint := "http://127.0.0.1:" + port + "/v2/" + repository + "/manifests/" + url.PathEscape(tag)
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("inspect local registry manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("local registry manifest returned %s", response.Status)
	}
	digest := strings.TrimSpace(response.Header.Get("Docker-Content-Digest"))
	if digest == "" {
		return "manifest-present", nil
	}
	return digest, nil
}

func ingressProbeAddress(ctx context.Context, clients *kubeClients, cfg *InstallConfig) (string, error) {
	ingress, err := clients.typed.NetworkingV1().Ingresses(cfg.Spec.Namespaces.System).Get(ctx, "argus-web", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read Ingress argus-web: %w", err)
	}
	for _, entry := range ingress.Status.LoadBalancer.Ingress {
		if address := strings.TrimSpace(entry.IP); address != "" {
			return address, nil
		}
		if address := strings.TrimSpace(entry.Hostname); address != "" {
			return address, nil
		}
	}
	return "", fmt.Errorf("Ingress argus-web has no load-balancer address yet; the ingress controller may still be allocating one")
}

func webIngressHosts(cfg *InstallConfig) []string {
	artifactHost := strings.TrimSpace(cfg.Spec.Exposure.ArtifactHost)
	if artifactHost == "" {
		artifactHost = "artifacts." + parentDomain(cfg.Spec.Exposure.EnterpriseHost)
	}
	return []string{
		cfg.Spec.Exposure.EnterpriseHost,
		cfg.Spec.Exposure.PlatformHost,
		"cards." + parentDomain(cfg.Spec.Exposure.EnterpriseHost),
		artifactHost,
	}
}

func ingressCertificatesReady(ctx context.Context, clients *kubeClients, cfg *InstallConfig, hosts []string) error {
	names := []string{"enterprise", "platform", "cards", "artifact"}
	if len(hosts) != len(names) {
		return fmt.Errorf("unexpected ingress host set")
	}
	for index, name := range names {
		certificateName := "argus-" + name
		object, err := clients.dynamic.Resource(pkiCertificateGVR).Namespace(cfg.Spec.Namespaces.System).Get(ctx, certificateName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read Certificate %s: %w", certificateName, err)
		}
		if err := validateIngressCertificateForCurrentPKI(ctx, clients, cfg, object, certificateName, "argus-"+name+"-tls", hosts[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateIngressCertificateForCurrentPKI(ctx context.Context, clients *kubeClients, cfg *InstallConfig, object *unstructured.Unstructured, name, expectedSecret, expectedHost string) error {
	expectedIssuer := cfg.globalIssuerName()
	issuerName, found, err := unstructured.NestedString(object.Object, "spec", "issuerRef", "name")
	if err != nil || !found {
		return fmt.Errorf("Certificate %s has no valid spec.issuerRef.name", name)
	}
	if issuerName == expectedIssuer {
		return validateIngressCertificate(object.Object, name, expectedSecret, expectedHost, expectedIssuer)
	}
	epoch, ok := rotationEpochFromFormerIssuer(cfg.Spec.ReleaseID, issuerName)
	if !ok {
		return fmt.Errorf("Certificate %s does not reference global ClusterIssuer %s or a managed former Issuer", name, expectedIssuer)
	}
	if err = validateIngressCertificate(object.Object, name, expectedSecret, expectedHost, issuerName); err != nil {
		return err
	}
	former, err := clients.dynamic.Resource(pkiIssuerGVR).Get(ctx, issuerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read former ClusterIssuer %s: %w", issuerName, err)
	}
	if former.GetLabels()["argus.io/release-id"] != cfg.Spec.ReleaseID || former.GetLabels()[pkiRoleLabel] != pkiFormerIssuerRole ||
		former.GetAnnotations()[pkiEpochAnnotation] != strconv.FormatInt(epoch, 10) || !issuerReady(former) {
		return fmt.Errorf("former ClusterIssuer %s is not a Ready resource owned by rotation epoch %d", issuerName, epoch)
	}
	stagedName := stagedServerCertificateName(name, epoch)
	staged, err := clients.dynamic.Resource(pkiCertificateGVR).Namespace(cfg.Spec.Namespaces.System).Get(ctx, stagedName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read staged Certificate %s for active former leaf: %w", stagedName, err)
	}
	if err = validateStagedIngressCertificateMetadata(staged, cfg.Spec.ReleaseID, epoch, name, expectedSecret, expectedIssuer); err != nil {
		return err
	}
	return validateIngressCertificate(staged.Object, stagedName, stagedName, expectedHost, expectedIssuer)
}

func rotationEpochFromFormerIssuer(releaseID, issuerName string) (int64, bool) {
	prefix := releaseID + "-ca-former-"
	if !strings.HasPrefix(issuerName, prefix) {
		return 0, false
	}
	epoch, err := strconv.ParseInt(strings.TrimPrefix(issuerName, prefix), 10, 64)
	return epoch, err == nil && epoch > 0
}

func validateStagedIngressCertificateMetadata(staged *unstructured.Unstructured, releaseID string, epoch int64, sourceName, sourceSecret, targetIssuer string) error {
	if staged == nil || staged.GetLabels()["argus.io/release-id"] != releaseID || staged.GetLabels()[pkiRoleLabel] != pkiStagedServerRole ||
		staged.GetAnnotations()[pkiEpochAnnotation] != strconv.FormatInt(epoch, 10) ||
		staged.GetAnnotations()["argus.io/pki-direction"] != "forward" ||
		staged.GetAnnotations()[pkiSourceCertificate] != sourceName ||
		staged.GetAnnotations()[pkiSourceSecret] != sourceSecret ||
		staged.GetAnnotations()[pkiTargetIssuer] != targetIssuer {
		return fmt.Errorf("staged Certificate %s does not exactly describe forward rotation epoch %d for %s", staged.GetName(), epoch, sourceName)
	}
	return nil
}

func validateIngressCertificate(object map[string]any, name, expectedSecret, expectedHost, expectedIssuer string) error {
	secretName, found, err := unstructured.NestedString(object, "spec", "secretName")
	if err != nil || !found {
		return fmt.Errorf("Certificate %s has no valid spec.secretName", name)
	}
	if secretName != expectedSecret {
		return fmt.Errorf("Certificate %s writes secret %q (expected %s)", name, secretName, expectedSecret)
	}
	dnsNames, found, err := unstructured.NestedStringSlice(object, "spec", "dnsNames")
	if err != nil || !found || len(dnsNames) != 1 || dnsNames[0] != expectedHost {
		return fmt.Errorf("Certificate %s must cover only %s", name, expectedHost)
	}
	issuerName, found, err := unstructured.NestedString(object, "spec", "issuerRef", "name")
	if err != nil || !found || issuerName != expectedIssuer {
		return fmt.Errorf("Certificate %s does not reference global ClusterIssuer %s", name, expectedIssuer)
	}
	issuerKind, _, _ := unstructured.NestedString(object, "spec", "issuerRef", "kind")
	if issuerKind != "ClusterIssuer" {
		return fmt.Errorf("Certificate %s issuer kind is not ClusterIssuer", name)
	}
	usages, found, err := unstructured.NestedStringSlice(object, "spec", "usages")
	if err != nil || !found || len(usages) != 1 || usages[0] != "server auth" {
		return fmt.Errorf("Certificate %s must have only server auth usage", name)
	}
	ready := false
	if conditions, conditionsFound, conditionsErr := unstructuredNestedSlice(object, "status", "conditions"); conditionsErr == nil && conditionsFound {
		for _, raw := range conditions {
			condition, _ := raw.(map[string]any)
			if condition["type"] == "Ready" && condition["status"] == "True" {
				ready = true
				break
			}
		}
	}
	if !ready {
		return fmt.Errorf("Certificate %s is not Ready", name)
	}
	return nil
}

func materializeTrustBundle(ctx context.Context, clients *kubeClients, cfg *InstallConfig) (string, error) {
	bundle, err := clients.typed.CoreV1().ConfigMaps(cfg.Spec.Namespaces.System).Get(ctx, cfg.trustBundleName(), metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read Trust Bundle ConfigMap: %w", err)
	}
	value, err := canonicalCABundle([]byte(bundle.Data["ca.crt"]))
	if err != nil {
		return "", fmt.Errorf("validate Trust Bundle: %w", err)
	}
	file, err := os.CreateTemp("", "argus-trust-*.pem")
	if err != nil {
		return "", fmt.Errorf("create temporary Trust Bundle: %w", err)
	}
	path := file.Name()
	if _, err = file.Write(value); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write temporary Trust Bundle: %w", err)
	}
	return path, nil
}

func connectorLoadBalancerReady(ctx context.Context, clients *kubeClients, cfg *InstallConfig) error {
	service, err := clients.typed.CoreV1().Services(cfg.Spec.Namespaces.System).Get(ctx, "argus-connector-gateway-public", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read Service argus-connector-gateway-public: %w", err)
	}
	if len(service.Status.LoadBalancer.Ingress) == 0 {
		return fmt.Errorf("connector load balancer has no address yet; ensure the cluster provides a LoadBalancer implementation")
	}
	return nil
}

func selectIngressProbeAddress(ctx context.Context, cfg *InstallConfig, loadBalancerAddress string) string {
	// Local-registry installations run against a local development cluster.
	// Docker Desktop publishes LoadBalancer port 443 on localhost, while host
	// proxy/TUN software may reset direct connections to its bridge address.
	if cfg.Spec.Images.Mode == "local-registry" && tcpAddressReachable(ctx, "127.0.0.1:443") {
		return "127.0.0.1"
	}
	return loadBalancerAddress
}

func tcpAddressReachable(ctx context.Context, address string) bool {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func (a *App) httpsProbe(ctx context.Context, cfg *InstallConfig, host, ingressAddress, caPath string) error {
	status, err := a.curlStatus(ctx, "https://"+host+"/healthz", "", ingressAddress, caPath)
	if err != nil {
		return err
	}
	if status != "200" {
		return fmt.Errorf("https://%s/healthz returned HTTP %s (expected 200; check DNS, ingress, certificate, and backend)", host, status)
	}
	return nil
}

// corsOriginProbe exercises the exact browser-origin path that produced
// "origin not allowed" regressions: an allowed Origin must pass and a forged
// Origin must be rejected by the backend CORS middleware.
func (a *App) corsOriginProbe(ctx context.Context, cfg *InstallConfig, platformHost, ingressAddress, caPath string) error {
	status, err := a.curlStatus(ctx, "https://"+platformHost+"/api/v1/setup/status", "https://"+platformHost, ingressAddress, caPath)
	if err != nil {
		return err
	}
	if status == "403" {
		return fmt.Errorf("allowed Origin https://%s was rejected (403): ARGUS_ALLOWED_ORIGINS does not match the deployed origin", platformHost)
	}
	if status != "200" && status != "401" {
		return fmt.Errorf("setup status with allowed Origin returned HTTP %s", status)
	}
	status, err = a.curlStatus(ctx, "https://"+platformHost+"/api/v1/setup/status", "https://evil.example.net", ingressAddress, caPath)
	if err != nil {
		return err
	}
	if status != "403" {
		return fmt.Errorf("forged Origin returned HTTP %s (expected 403 rejection)", status)
	}
	return nil
}

func curlStatusArgs(rawURL, origin, ingressAddress, caPath string) ([]string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("probe URL must be absolute HTTPS: %q", rawURL)
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	if strings.TrimSpace(caPath) == "" {
		return nil, fmt.Errorf("probe CA bundle path is required")
	}
	args := []string{"-sS", "--cacert", caPath, "--noproxy", "*", "--connect-timeout", "5", "--max-time", "20", "-o", "/dev/null", "-w", "%{http_code}"}
	if ingressAddress != "" {
		connectHost := ingressAddress
		if strings.Contains(connectHost, ":") && !strings.HasPrefix(connectHost, "[") {
			connectHost = "[" + connectHost + "]"
		}
		args = append(args, "--connect-to", fmt.Sprintf("%s:%s:%s:%s", parsed.Hostname(), port, connectHost, port))
	}
	if origin != "" {
		args = append(args, "-H", "Origin: "+origin)
	}
	return append(args, rawURL), nil
}

func (a *App) curlStatus(ctx context.Context, rawURL, origin, ingressAddress, caPath string) (string, error) {
	args, err := curlStatusArgs(rawURL, origin, ingressAddress, caPath)
	if err != nil {
		return "", err
	}
	var output string
	for attempt := 0; attempt < 3; attempt++ {
		output, err = a.runner.quiet(ctx, "curl", args...)
		if err == nil {
			break
		}
		if attempt < 2 {
			delay := time.Duration(1<<attempt) * 250 * time.Millisecond
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("probe %s with curl after 3 attempts: %w", rawURL, err)
	}
	status := strings.TrimSpace(output)
	if len(status) != 3 {
		return "", fmt.Errorf("probe %s returned unexpected curl output %q", rawURL, output)
	}
	return status, nil
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
	image := ""
	for _, container := range pods.Items[0].Spec.Containers {
		if container.Name == "kafka" {
			image = container.Image
			break
		}
	}
	if image == "" {
		return fmt.Errorf("Kafka broker image not found")
	}
	name := kubernetesName("argus-kafka-smoke-" + cfg.Spec.ReleaseID)
	env := []string{"          - name: KAFKA_PASSWORD\n            valueFrom: {secretKeyRef: {name: argus-telemetry, key: password}}"}
	command := `set -eu
cat >/tmp/client.properties <<EOF
security.protocol=SASL_PLAINTEXT
sasl.mechanism=SCRAM-SHA-512
sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required username="argus-telemetry" password="$KAFKA_PASSWORD";
EOF
message="argus-e2e-$(date +%s%N)"
group="argus-installation-check-$(date +%s%N)"
printf '%s\n' "$message" | /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server argus-kafka-kafka-bootstrap:9093 --command-config /tmp/client.properties --topic argus-installation-check
timeout 15 /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server argus-kafka-kafka-bootstrap:9093 --command-config /tmp/client.properties --topic argus-installation-check --from-beginning --group "$group" --max-messages 100 >/tmp/messages 2>/tmp/consumer.log || true
if ! grep -Fqx "$message" /tmp/messages; then
  cat /tmp/consumer.log >&2
  cat /tmp/messages >&2
  exit 1
fi`
	return a.runPodManifest(ctx, cfg, cfg.Spec.Namespaces.Observability, name, genericSmokePod(name, cfg.Spec.Namespaces.Observability, image, env, command))
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
	command := `set -eu
printf argus-e2e >/tmp/value
for attempt in $(seq 1 12); do
  if mc alias set -- argus http://argus-minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null 2>&1 &&
     mc mb --ignore-existing argus/argus-e2e >/dev/null 2>&1 &&
     mc cp /tmp/value argus/argus-e2e/value >/dev/null 2>&1 &&
     test "$(mc cat argus/argus-e2e/value 2>/dev/null)" = argus-e2e; then
    exit 0
  fi
  sleep 5
done
echo "MinIO object roundtrip did not become writable after bounded retries" >&2
exit 1`
	return genericSmokePod(name, cfg.Spec.Namespaces.System, "minio/mc:RELEASE.2025-08-13T08-35-41Z", env, command)
}

func clickHouseSmokePod(cfg *InstallConfig) string {
	name := kubernetesName("argus-clickhouse-smoke-" + cfg.Spec.ReleaseID)
	env := []string{"          - name: CLICKHOUSE_PASSWORD\n            valueFrom: {secretKeyRef: {name: argus-clickhouse-credentials, key: password}}"}
	command := `set -eu; trap 'clickhouse-client --host argus-clickhouse-client --user argus --password "$CLICKHOUSE_PASSWORD" --query "DROP TABLE IF EXISTS default.argus_e2e_persistence" >/dev/null 2>&1' EXIT; clickhouse-client --host argus-clickhouse-client --user argus --password "$CLICKHOUSE_PASSWORD" --multiquery "CREATE TABLE IF NOT EXISTS default.argus_e2e_persistence (id String, value String) ENGINE=ReplacingMergeTree ORDER BY id; INSERT INTO default.argus_e2e_persistence VALUES ('argus-e2e','clickhouse-ok');"; clickhouse-client --host argus-clickhouse-client --user argus --password "$CLICKHOUSE_PASSWORD" --query "SELECT value FROM default.argus_e2e_persistence FINAL WHERE id='argus-e2e'" | grep -q clickhouse-ok`
	return genericSmokePod(name, cfg.Spec.Namespaces.Observability, "clickhouse/clickhouse-server:26.3.17.110-alpine", env, command)
}

func openSandboxSmokePod(cfg *InstallConfig, name string) string {
	image := cfg.Image("argus-backend")
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
      image: %s
      imagePullPolicy: %s
      command: ["/usr/local/bin/argus-sandbox-smoke"]
      env:
        - name: OPENSANDBOX_BASE_URL
          value: http://opensandbox-server
        - name: OPENSANDBOX_API_KEY
          valueFrom: {secretKeyRef: {name: argus-opensandbox-api, key: api-key}}
`, name, cfg.Spec.Namespaces.Sandbox, image, cfg.Spec.Images.PullPolicy)
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
