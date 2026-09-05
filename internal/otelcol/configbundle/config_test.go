package configbundle

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestRenderProducesRunnableTargetsWithoutSecrets(t *testing.T) {
	value, err := Render(RenderInput{CollectorID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111", ResourceID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d222",
		ResourceType: "host", Role: "direct", RouteKind: "direct_argus", Transport: "direct", ProfileKeys: []string{"host-basic", "otlp-receiver"},
		EnrollmentEndpoint: "https://api.example.com/api/v1/telemetry/collectors/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318"})
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

func TestRenderRejectsMissingTransport(t *testing.T) {
	_, err := Render(RenderInput{
		CollectorID: "collector", ResourceID: "resource", ResourceType: "host", Role: "direct",
		RouteKind: "direct_argus", ProfileKeys: []string{"host-basic"},
		EnrollmentEndpoint: "https://api.example.com/enroll", IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317",
		IngestHTTPEndpoint: "https://telemetry.example.com:4318",
	})
	if err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("missing transport accepted: %v", err)
	}
}

func TestRenderRejectsCleartextEndpoints(t *testing.T) {
	_, err := Render(RenderInput{CollectorID: "collector", ResourceID: "resource", ResourceType: "host", Role: "direct",
		RouteKind: "direct_argus", Transport: "direct", ProfileKeys: []string{"host-basic"}, EnrollmentEndpoint: "http://api.example.com/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318"})
	if err == nil {
		t.Fatal("cleartext enrollment must be rejected")
	}
}

func TestRenderUsesLoopbackEnrollmentDialAddressWithoutChangingTLSIdentity(t *testing.T) {
	input := RenderInput{CollectorID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111", ResourceID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d222",
		ResourceType: "host", Role: "edge_gateway", RouteKind: "direct_argus", Transport: "executor_tunnel", TunnelLoopbackPort: 14317,
		ProfileKeys: []string{"host-basic", "otlp-receiver"}, EnrollmentEndpoint: "https://telemetry.argus.example:4318/v1/identity/enroll",
		EnrollmentDialAddress: "127.0.0.1:14318", IngestGRPCEndpoint: "grpcs://telemetry.argus.example:4317",
		IngestHTTPEndpoint: "https://telemetry.argus.example:4318"}
	value, err := Render(input)
	if err != nil {
		t.Fatal(err)
	}
	host := decodeTarget(t, value, "host")
	receivers := nestedMap(t, host, "receivers")
	if _, exists := receivers["otlp"]; exists {
		t.Fatal("edge gateway rendered an unauthenticated OTLP receiver alongside its mTLS downstream receiver")
	}
	if _, exists := receivers["otlp/downstream"]; !exists {
		t.Fatal("edge gateway mTLS downstream receiver is missing")
	}
	identity := nestedMap(t, host, "extensions", "argus_identity")
	assertString(t, identity, "enrollment_endpoint", input.EnrollmentEndpoint)
	assertString(t, identity, "dial_address", input.EnrollmentDialAddress)
	exporter := nestedMap(t, host, "exporters", "otlp/argus")
	assertString(t, exporter, "endpoint", "127.0.0.1:14317")
	assertString(t, nestedMap(t, exporter, "tls"), "server_name_override", "telemetry.argus.example")
}

func TestRenderRejectsNonLoopbackEnrollmentDialAddress(t *testing.T) {
	_, err := Render(RenderInput{CollectorID: "collector", ResourceID: "resource", ResourceType: "host", Role: "direct",
		RouteKind: "direct_argus", Transport: "direct", ProfileKeys: []string{"host-basic"}, EnrollmentEndpoint: "https://api.example.com/enroll",
		EnrollmentDialAddress: "10.0.0.8:8443", IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317",
		IngestHTTPEndpoint: "https://telemetry.example.com:4318"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback enrollment dial address accepted: %v", err)
	}
}

func TestRenderBastionTunnelUsesLoopbackAndGatewayTLSIdentityWithoutPhysicalEndpoint(t *testing.T) {
	input := RenderInput{CollectorID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111", ResourceID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d222",
		ResourceType: "host", Role: "leaf", RouteKind: "bastion_gateway", Transport: "bastion_tunnel", TunnelLoopbackPort: 14318,
		GatewayServerName: "collector-018f08d2-7d43-7a54-a8fb-f2f3f2f0d999.argus.telemetry",
		ProfileKeys:       []string{"host-basic"}, EnrollmentEndpoint: "https://telemetry.argus.example:4318/v1/identity/enroll",
		EnrollmentDialAddress: "127.0.0.1:14319", IngestGRPCEndpoint: "grpcs://telemetry.argus.example:4317",
		IngestHTTPEndpoint: "https://telemetry.argus.example:4318"}
	value, err := Render(input)
	if err != nil {
		t.Fatal(err)
	}
	host := decodeTarget(t, value, "host")
	exporter := nestedMap(t, host, "exporters", "otlp/argus")
	assertString(t, exporter, "endpoint", "127.0.0.1:14318")
	assertString(t, nestedMap(t, exporter, "tls"), "server_name_override", input.GatewayServerName)
}

func TestRenderDirectBastionRouteRequiresPhysicalGatewayEndpoint(t *testing.T) {
	_, err := Render(RenderInput{CollectorID: "collector", ResourceID: "resource", ResourceType: "host", Role: "leaf",
		RouteKind: "bastion_gateway", Transport: "direct", GatewayServerName: "collector-gateway.argus.telemetry",
		ProfileKeys: []string{"host-basic"}, EnrollmentEndpoint: "https://api.example.com/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318"})
	if err == nil || !strings.Contains(err.Error(), "Gateway OTLP endpoint") {
		t.Fatalf("direct Bastion route accepted without a physical Gateway endpoint: %v", err)
	}
}

func TestRenderKubernetesConfigUsesMutualTLSAndIsolatesGatewayIdentity(t *testing.T) {
	value, err := Render(RenderInput{CollectorID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111", ResourceID: "018f08d2-7d43-7a54-a8fb-f2f3f2f0d222",
		ResourceType: "kubernetes_cluster", Role: "kubernetes", RouteKind: "direct_argus", Transport: "direct",
		ProfileKeys:        []string{"k8s-node-container", "k8s-cluster", "k8s-otlp-gateway"},
		EnrollmentEndpoint: "https://api.example.com/api/v1/telemetry/collectors/enroll",
		IngestGRPCEndpoint: "grpcs://telemetry.example.com:4317", IngestHTTPEndpoint: "https://telemetry.example.com:4318"})
	if err != nil {
		t.Fatal(err)
	}
	agent := decodeTarget(t, value, "kubernetes_agent")
	agentIdentity := nestedMap(t, agent, "extensions", "argus_identity")
	assertString(t, agentIdentity, "bootstrap_identity_directory", "/var/run/argus-bootstrap/identity")
	assertString(t, agentIdentity, "enrollment_token_file", "/var/lib/argus-otelcol/identity/enrollment-token")
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
	gatewayIdentity := nestedMap(t, gateway, "extensions", "argus_identity")
	assertString(t, gatewayIdentity, "bootstrap_identity_directory", "/var/run/argus-bootstrap/identity")
	assertString(t, gatewayIdentity, "enrollment_token_file", "/var/lib/argus-otelcol/identity/enrollment-token")
	grpc := nestedMap(t, gateway, "receivers", "otlp/downstream", "protocols", "grpc")
	receiverTLS := nestedMap(t, grpc, "tls")
	assertString(t, receiverTLS, "client_ca_file", "/var/lib/argus-otelcol/identity/ca.pem")
	assertString(t, receiverTLS, "cert_file", "/var/lib/argus-otelcol/identity/server.pem")
	assertString(t, receiverTLS, "key_file", "/var/lib/argus-otelcol/identity/server-key.pem")
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

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
