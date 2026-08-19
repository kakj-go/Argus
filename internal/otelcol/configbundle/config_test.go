package configbundle

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRenderProducesRunnableTargetsWithoutSecrets(t *testing.T) {
	value, err := Render(RenderInput{CollectorID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111", ResourceID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d222",
		ResourceType: "host", Role: "direct", RouteKind: "direct_argus", ProfileKeys: []string{"host-basic", "otlp-receiver"},
		EnrollmentEndpoint: "https://api.example.com/api/v1/telemetry/collectors/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318", ServerCAPEM: testCA(t)})
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(value) == false {
		t.Fatal("bundle must be valid JSON")
	}
	for _, target := range []string{"host", "kubernetes_agent", "kubernetes_gateway"} {
		config, extractErr := Extract(value, target)
		if extractErr != nil || !json.Valid(config) {
			t.Fatalf("target %s is invalid: %v", target, extractErr)
		}
		if target != "kubernetes_agent" && !contains(string(config), `"endpoint":"0.0.0.0:13133"`) {
			t.Fatalf("target %s does not expose the fixed identity health endpoint", target)
		}
	}
	if string(value) == "" || contains(string(value), "enrollment-token-value") {
		t.Fatal("rendered configuration must not contain an enrollment token")
	}
}

func TestRenderRejectsCleartextEndpoints(t *testing.T) {
	_, err := Render(RenderInput{CollectorID: "collector", ResourceID: "resource", ResourceType: "host", Role: "direct",
		RouteKind: "direct_argus", ProfileKeys: []string{"host-basic"}, EnrollmentEndpoint: "http://api.example.com/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318", ServerCAPEM: testCA(t)})
	if err == nil {
		t.Fatal("cleartext enrollment must be rejected")
	}
}

func TestRenderKubernetesConfigUsesMutualTLSAndIsolatesGatewayIdentity(t *testing.T) {
	value, err := Render(RenderInput{CollectorID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111", ResourceID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d222",
		ResourceType: "kubernetes_cluster", Role: "kubernetes", RouteKind: "direct_argus",
		ProfileKeys:        []string{"k8s-node-container", "k8s-cluster", "k8s-otlp-gateway"},
		EnrollmentEndpoint: "https://api.example.com/api/v1/telemetry/collectors/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318", ServerCAPEM: testCA(t)})
	if err != nil {
		t.Fatal(err)
	}
	agent := decodeTarget(t, value, "kubernetes_agent")
	agentTLS := nestedMap(t, agent, "exporters", "otlp/gateway", "tls")
	assertString(t, agentTLS, "ca_file", "/var/lib/argus-otelcol/identity/ca.pem")
	assertString(t, agentTLS, "cert_file", "/var/lib/argus-otelcol/identity/client.pem")
	assertString(t, agentTLS, "key_file", "/var/lib/argus-otelcol/identity/client-key.pem")
	kubelet := nestedMap(t, agent, "receivers", "kubeletstats")
	assertString(t, kubelet, "ca_file", "/var/run/argus-kubelet/pki/kubelet.crt")
	if insecure, ok := kubelet["insecure_skip_verify"].(bool); !ok || insecure {
		t.Fatal("kubelet receiver must verify the Kubernetes API certificate")
	}

	gateway := decodeTarget(t, value, "kubernetes_gateway")
	grpc := nestedMap(t, gateway, "receivers", "otlp/downstream", "protocols", "grpc")
	receiverTLS := nestedMap(t, grpc, "tls")
	assertString(t, receiverTLS, "client_ca_file", "/var/lib/argus-otelcol/identity/ca.pem")
	assertString(t, receiverTLS, "cert_file", "/var/lib/argus-otelcol/identity/client.pem")
	assertString(t, receiverTLS, "key_file", "/var/lib/argus-otelcol/identity/client-key.pem")
	assertString(t, nestedMap(t, grpc, "auth"), "authenticator", "argus_identity")

	pipelines := nestedMap(t, gateway, "service", "pipelines")
	baseProcessors := stringSlice(t, nestedMap(t, pipelines, "metrics"), "processors")
	if slices.Contains(baseProcessors, "argus_gateway_identity") {
		t.Fatal("Gateway self-collected Kubernetes metrics must not require a downstream client identity")
	}
	for _, signal := range []string{"metrics", "logs", "traces"} {
		pipeline, ok := pipelines[signal].(map[string]any)
		if !ok {
			continue
		}
		receivers := stringSlice(t, pipeline, "receivers")
		if slices.Contains(receivers, "otlp/downstream") {
			t.Fatalf("Gateway downstream receiver must not be shared with self-collected %s pipeline", signal)
		}
	}
	downstreamProcessors := stringSlice(t, nestedMap(t, pipelines, "metrics/downstream"), "processors")
	if !slices.Contains(downstreamProcessors, "argus_gateway_identity") {
		t.Fatal("Gateway downstream metrics must be normalized from the authenticated Collector identity")
	}
	if slices.Contains(downstreamProcessors, "batch") {
		t.Fatal("Gateway downstream pipelines must not batch records from different Collector identities")
	}
}

func decodeTarget(t *testing.T, bundle []byte, target string) map[string]any {
	t.Helper()
	value, err := Extract(bundle, target)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(value, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func nestedMap(t *testing.T, value map[string]any, path ...string) map[string]any {
	t.Helper()
	current := value
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("configuration path %s is not an object", strings.Join(path, "."))
		}
		current = next
	}
	return current
}

func stringSlice(t *testing.T, value map[string]any, key string) []string {
	t.Helper()
	items, ok := value[key].([]any)
	if !ok {
		t.Fatalf("configuration key %s is not an array", key)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("configuration key %s contains a non-string value", key)
		}
		result = append(result, text)
	}
	return result
}

func assertString(t *testing.T, value map[string]any, key, expected string) {
	t.Helper()
	if actual, ok := value[key].(string); !ok || actual != expected {
		t.Fatalf("configuration key %s = %#v, want %q", key, value[key], expected)
	}
}

func testCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}))
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
