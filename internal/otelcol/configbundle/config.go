package configbundle

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const SchemaVersion = "argus.otelcol/v1"

type Bundle struct {
	SchemaVersion     string          `json:"schema_version"`
	CollectorID       string          `json:"collector_id"`
	Host              json.RawMessage `json:"host"`
	KubernetesAgent   json.RawMessage `json:"kubernetes_agent"`
	KubernetesGateway json.RawMessage `json:"kubernetes_gateway"`
	ServerCAPEM       string          `json:"server_ca_pem"`
}

type RenderInput struct {
	CollectorID        string
	ResourceID         string
	ResourceType       string
	Role               string
	RouteKind          string
	GatewayEndpoint    string
	GatewayServerName  string
	ProfileKeys        []string
	EnrollmentEndpoint string
	IngestGRPCEndpoint string
	IngestHTTPEndpoint string
	ServerCAPEM        string
}

func Render(input RenderInput) ([]byte, error) {
	if input.CollectorID == "" || input.ResourceID == "" || (input.ResourceType != "host" && input.ResourceType != "kubernetes_cluster") {
		return nil, errors.New("Collector and resource identity are required")
	}
	pool := x509.NewCertPool()
	if input.ServerCAPEM == "" || !pool.AppendCertsFromPEM([]byte(input.ServerCAPEM)) {
		return nil, errors.New("telemetry server CA bundle is required")
	}
	enrollment, err := requireHTTPS(input.EnrollmentEndpoint)
	if err != nil {
		return nil, fmt.Errorf("enrollment endpoint: %w", err)
	}
	ingestHTTP, err := requireHTTPS(input.IngestHTTPEndpoint)
	if err != nil {
		return nil, fmt.Errorf("OTLP HTTP endpoint: %w", err)
	}
	ingestGRPC, err := normalizeGRPCEndpoint(input.IngestGRPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("OTLP gRPC endpoint: %w", err)
	}
	rotation := strings.TrimRight(ingestHTTP.String(), "/") + "/v1/identity/rotate"
	exportEndpoint := ingestGRPC
	if input.RouteKind == "bastion_gateway" {
		exportEndpoint, err = normalizeGRPCEndpoint(input.GatewayEndpoint)
		if err != nil {
			return nil, fmt.Errorf("Gateway OTLP endpoint: %w", err)
		}
		if !validServerName(input.GatewayServerName) {
			return nil, errors.New("Gateway TLS server name is required")
		}
	} else if input.RouteKind != "direct_argus" {
		return nil, errors.New("Collector route kind is invalid")
	}
	profiles, err := normalizeProfiles(input.ResourceType, input.ProfileKeys)
	if err != nil {
		return nil, err
	}
	identity := resourceIdentity(input.CollectorID, input.ResourceID, input.ResourceType)

	host, err := json.Marshal(hostConfig(input, profiles, identity, enrollment.String(), rotation, exportEndpoint))
	if err != nil {
		return nil, err
	}
	agent, err := json.Marshal(kubernetesAgentConfig(input, profiles, identity, enrollment.String(), rotation))
	if err != nil {
		return nil, err
	}
	gateway, err := json.Marshal(kubernetesGatewayConfig(input, profiles, identity, enrollment.String(), rotation, exportEndpoint))
	if err != nil {
		return nil, err
	}
	return json.Marshal(Bundle{SchemaVersion: SchemaVersion, CollectorID: input.CollectorID, Host: host,
		KubernetesAgent: agent, KubernetesGateway: gateway, ServerCAPEM: input.ServerCAPEM})
}

func Extract(value []byte, target string) ([]byte, error) {
	var bundle Bundle
	if json.Unmarshal(value, &bundle) != nil || bundle.SchemaVersion != SchemaVersion || bundle.CollectorID == "" {
		return nil, errors.New("Collector configuration bundle is invalid")
	}
	var config json.RawMessage
	switch target {
	case "host":
		config = bundle.Host
	case "kubernetes_agent":
		config = bundle.KubernetesAgent
	case "kubernetes_gateway":
		config = bundle.KubernetesGateway
	default:
		return nil, errors.New("Collector configuration target is invalid")
	}
	var parsed map[string]any
	if len(config) == 0 || json.Unmarshal(config, &parsed) != nil || parsed["service"] == nil || parsed["receivers"] == nil || parsed["exporters"] == nil {
		return nil, errors.New("Collector runtime configuration is invalid")
	}
	return append([]byte(nil), config...), nil
}

func CollectorID(value []byte) (string, error) {
	var bundle Bundle
	if json.Unmarshal(value, &bundle) != nil || bundle.SchemaVersion != SchemaVersion || bundle.CollectorID == "" {
		return "", errors.New("Collector configuration bundle is invalid")
	}
	return bundle.CollectorID, nil
}

func ServerCA(value []byte) ([]byte, error) {
	var bundle Bundle
	if json.Unmarshal(value, &bundle) != nil || bundle.SchemaVersion != SchemaVersion {
		return nil, errors.New("Collector configuration bundle is invalid")
	}
	pool := x509.NewCertPool()
	if bundle.ServerCAPEM == "" || !pool.AppendCertsFromPEM([]byte(bundle.ServerCAPEM)) {
		return nil, errors.New("Collector server CA bundle is invalid")
	}
	return []byte(bundle.ServerCAPEM), nil
}

func outboundConfig(collectorID, enrollmentEndpoint, rotationEndpoint, ingestEndpoint, serverName, tokenFile, identityDirectory, serverCAFile string,
	receivers map[string]any, processors map[string]any, pipelines map[string]any) map[string]any {
	return map[string]any{
		"extensions": map[string]any{
			"argus_identity": identityConfig(collectorID, enrollmentEndpoint, rotationEndpoint, tokenFile, identityDirectory, serverCAFile),
			"file_storage":   map[string]any{"directory": identityDirectory + "/queue", "create_directory": true},
			"health_check":   map[string]any{"endpoint": "0.0.0.0:13133"},
		},
		"receivers": receivers,
		"processors": mergeMaps(map[string]any{
			"memory_limiter": map[string]any{"check_interval": "1s", "limit_mib": 256, "spike_limit_mib": 64},
			"batch":          map[string]any{"timeout": "1s", "send_batch_size": 1024},
		}, processors),
		"exporters": map[string]any{
			"otlp/argus": map[string]any{
				"endpoint": ingestEndpoint,
				"tls": map[string]any{"ca_file": identityDirectory + "/ca.pem", "cert_file": identityDirectory + "/client.pem",
					"key_file": identityDirectory + "/client-key.pem", "reload_interval": "1m", "server_name_override": serverName},
				"sending_queue":    map[string]any{"enabled": true, "storage": "file_storage", "queue_size": 4096},
				"retry_on_failure": map[string]any{"enabled": true, "initial_interval": "1s", "max_interval": "30s", "max_elapsed_time": "0s"},
			},
		},
		"service": map[string]any{
			"extensions": []string{"argus_identity", "file_storage", "health_check"},
			"pipelines":  pipelines,
		},
	}
}

func hostConfig(input RenderInput, profiles map[string]bool, identity map[string]string, enrollment, rotation, endpoint string) map[string]any {
	receivers := hostReceivers(profiles)
	processors := map[string]any{"resource/argus": resourceProcessor(identity)}
	baseReceivers := receivers
	if input.Role == "edge_gateway" {
		receivers["otlp/downstream"] = secureOTLPReceiver("0.0.0.0", "/var/lib/argus-otelcol/identity")
		// Keep the authenticated downstream receiver out of the ordinary
		// self-collection pipelines. Sharing one receiver across both paths can
		// drop the receiver auth context for logs and traces before the identity
		// processor runs.
		baseReceivers = make(map[string]any, len(receivers)-1)
		for name, receiver := range receivers {
			if name != "otlp/downstream" {
				baseReceivers[name] = receiver
			}
		}
	}
	pipelines := pipelineConfig(baseReceivers, []string{"memory_limiter", "resource/argus", "batch"}, "otlp/argus")
	if input.Role == "edge_gateway" {
		processors["argus_gateway_identity"] = map[string]any{}
		for signal := range rangeSignalPipelines() {
			pipelines[signal+"/downstream"] = map[string]any{
				"receivers": []string{"otlp/downstream"}, "processors": []string{"memory_limiter", "argus_gateway_identity"}, "exporters": []string{"otlp/argus"},
			}
		}
	}
	return outboundConfig(input.CollectorID, enrollment, rotation, endpoint, exporterServerName(input, endpoint), "/etc/argus-otelcol/enrollment-token",
		"/var/lib/argus-otelcol/identity", "/etc/argus-otelcol/server-ca.pem", receivers, processors, pipelines)
}

func hostReceivers(profiles map[string]bool) map[string]any {
	receivers := map[string]any{}
	if profiles["otlp-receiver"] {
		receivers["otlp"] = otlpReceiver("127.0.0.1")
	}
	if profiles["host-basic"] || profiles["collector-self"] {
		receivers["hostmetrics"] = map[string]any{"collection_interval": "30s", "scrapers": map[string]any{
			"cpu": map[string]any{}, "memory": map[string]any{}, "load": map[string]any{}, "filesystem": map[string]any{}, "network": map[string]any{},
		}}
	}
	if profiles["linux-journald"] {
		receivers["journald"] = map[string]any{"directory": "/var/log/journal"}
	}
	if profiles["file-log"] {
		receivers["filelog"] = map[string]any{"include": []string{"/var/log/*.log"}, "exclude": []string{"/var/log/argus-otelcol*.log"}, "start_at": "end"}
	}
	if profiles["prometheus-endpoint"] {
		receivers["prometheus"] = map[string]any{"config": map[string]any{"scrape_configs": []map[string]any{{
			"job_name": "argus-managed-local", "scrape_interval": "30s", "static_configs": []map[string]any{{"targets": []string{"127.0.0.1:9090"}}},
		}}}}
	}
	return receivers
}

func kubernetesAgentConfig(input RenderInput, profiles map[string]bool, identity map[string]string, enrollment, rotation string) map[string]any {
	receivers := map[string]any{}
	if profiles["k8s-node-container"] {
		receivers["kubeletstats"] = map[string]any{"collection_interval": "30s", "auth_type": "serviceAccount", "endpoint": "https://${env:K8S_NODE_NAME}:10250",
			"ca_file": "/var/run/argus-kubelet/pki/kubelet.crt", "insecure_skip_verify": false}
		receivers["filelog"] = map[string]any{"include": []string{"/var/log/pods/*/*/*.log"}, "start_at": "end", "include_file_path": true}
	}
	if profiles["otlp-receiver"] || profiles["k8s-otlp-gateway"] {
		receivers["otlp"] = otlpReceiver("127.0.0.1")
	}
	if len(receivers) == 0 {
		receivers["hostmetrics"] = map[string]any{"collection_interval": "30s", "root_path": "/hostfs", "scrapers": map[string]any{"cpu": map[string]any{}, "memory": map[string]any{}}}
	}
	identityDirectory := "/var/lib/argus-otelcol/identity"
	return map[string]any{
		"extensions": map[string]any{"argus_identity": identityConfig(input.CollectorID, enrollment, rotation, "/var/run/argus-bootstrap/enrollment-token", identityDirectory, "/etc/argus-otelcol/server-ca.pem")},
		"receivers":  receivers,
		"processors": map[string]any{
			"memory_limiter": map[string]any{"check_interval": "1s", "limit_mib": 192, "spike_limit_mib": 48},
			"resource/argus": resourceProcessor(identity),
			"batch":          map[string]any{"timeout": "1s", "send_batch_size": 1024},
		},
		"exporters": map[string]any{"otlp/gateway": map[string]any{"endpoint": "argus-otelcol-gateway.argus-telemetry.svc.cluster.local:4317", "tls": map[string]any{
			"ca_file": identityDirectory + "/ca.pem", "cert_file": identityDirectory + "/client.pem", "key_file": identityDirectory + "/client-key.pem",
			"reload_interval": "1m", "server_name_override": collectorServerName(input.CollectorID),
		}}},
		"service": map[string]any{"extensions": []string{"argus_identity"}, "pipelines": pipelineConfig(receivers, []string{"memory_limiter", "resource/argus", "batch"}, "otlp/gateway")},
	}
}

func kubernetesGatewayConfig(input RenderInput, profiles map[string]bool, identity map[string]string, enrollment, rotation, endpoint string) map[string]any {
	receivers := map[string]any{"otlp/downstream": secureOTLPReceiver("0.0.0.0", "/var/lib/argus-otelcol/identity")}
	if profiles["k8s-cluster"] {
		receivers["k8s_cluster"] = map[string]any{"auth_type": "serviceAccount", "collection_interval": "30s"}
	}
	processors := map[string]any{"resource/argus": resourceProcessor(identity), "argus_gateway_identity": map[string]any{}}
	baseReceivers := map[string]any{}
	if receiver, ok := receivers["k8s_cluster"]; ok {
		baseReceivers["k8s_cluster"] = receiver
	}
	pipelines := pipelineConfig(baseReceivers, []string{"memory_limiter", "resource/argus", "batch"}, "otlp/argus")
	for signal := range rangeSignalPipelines() {
		pipelines[signal+"/downstream"] = map[string]any{"receivers": []string{"otlp/downstream"},
			"processors": []string{"memory_limiter", "argus_gateway_identity", "resource/argus"}, "exporters": []string{"otlp/argus"}}
	}
	return outboundConfig(input.CollectorID, enrollment, rotation, endpoint, exporterServerName(input, endpoint), "/var/run/argus-bootstrap/enrollment-token",
		"/var/lib/argus-otelcol/identity", "/etc/argus-otelcol/server-ca.pem", receivers, processors, pipelines)
}

func identityConfig(collectorID, enrollmentEndpoint, rotationEndpoint, tokenFile, identityDirectory, serverCAFile string) map[string]any {
	return map[string]any{
		"collector_id": collectorID, "enrollment_endpoint": enrollmentEndpoint, "rotation_endpoint": rotationEndpoint,
		"enrollment_token_file": tokenFile, "certificate_file": identityDirectory + "/client.pem",
		"private_key_file": identityDirectory + "/client-key.pem", "ca_bundle_file": identityDirectory + "/ca.pem",
		"server_ca_file": serverCAFile, "rotate_before": "8h", "check_interval": "5m",
	}
}

func otlpReceiver(host string) map[string]any {
	return map[string]any{"protocols": map[string]any{
		"grpc": map[string]any{"endpoint": host + ":4317"}, "http": map[string]any{"endpoint": host + ":4318"},
	}}
}

func secureOTLPReceiver(host, identityDirectory string) map[string]any {
	tlsConfig := map[string]any{"cert_file": identityDirectory + "/client.pem", "key_file": identityDirectory + "/client-key.pem", "client_ca_file": identityDirectory + "/ca.pem"}
	return map[string]any{"protocols": map[string]any{
		"grpc": map[string]any{"endpoint": host + ":4317", "tls": tlsConfig, "auth": map[string]any{"authenticator": "argus_identity"}},
		"http": map[string]any{"endpoint": host + ":4318", "tls": tlsConfig, "auth": map[string]any{"authenticator": "argus_identity"}},
	}}
}

func collectorServerName(collectorID string) string {
	return "collector-" + collectorID + ".argus.telemetry"
}

func pipelineConfig(receivers map[string]any, processors []string, exporter string) map[string]any {
	bySignal := map[string][]string{"metrics": {}, "logs": {}, "traces": {}}
	for name := range receivers {
		switch name {
		case "hostmetrics", "prometheus", "kubeletstats", "k8s_cluster":
			bySignal["metrics"] = append(bySignal["metrics"], name)
		case "journald", "filelog":
			bySignal["logs"] = append(bySignal["logs"], name)
		default:
			for signal := range bySignal {
				bySignal[signal] = append(bySignal[signal], name)
			}
		}
	}
	result := map[string]any{}
	for signal, signalReceivers := range bySignal {
		if len(signalReceivers) > 0 {
			result[signal] = map[string]any{"receivers": signalReceivers, "processors": processors, "exporters": []string{exporter}}
		}
	}
	return result
}

func normalizeProfiles(resourceType string, values []string) (map[string]bool, error) {
	allowed := map[string]bool{
		"host-basic": true, "linux-journald": true, "file-log": true, "prometheus-endpoint": true, "otlp-receiver": true,
		"k8s-node-container": true, "k8s-cluster": true, "k8s-otlp-gateway": true, "collector-self": true,
	}
	result := map[string]bool{}
	for _, value := range values {
		key, version, versioned := strings.Cut(value, "@")
		if versioned && (version == "" || strings.Contains(version, "@")) {
			return nil, fmt.Errorf("Collection Profile %q has an invalid version", value)
		}
		if !allowed[key] || (resourceType == "host" && strings.HasPrefix(key, "k8s-")) || (resourceType == "kubernetes_cluster" && !strings.HasPrefix(key, "k8s-") && key != "otlp-receiver" && key != "collector-self") {
			return nil, fmt.Errorf("Collection Profile %q is not valid for %s", value, resourceType)
		}
		result[key] = true
	}
	if len(result) == 0 {
		return nil, errors.New("at least one Collection Profile is required")
	}
	return result, nil
}

func resourceIdentity(collectorID, resourceID, resourceType string) map[string]string {
	return map[string]string{"argus.collector.id": collectorID, "argus.resource.id": resourceID, "argus.resource.type": resourceType}
}

func resourceProcessor(identity map[string]string) map[string]any {
	actions := make([]map[string]any, 0, len(identity))
	for _, key := range []string{"argus.collector.id", "argus.resource.id", "argus.resource.type"} {
		actions = append(actions, map[string]any{"key": key, "value": identity[key], "action": "upsert"})
	}
	return map[string]any{"attributes": actions}
}

func mergeMaps(base, extra map[string]any) map[string]any {
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func rangeSignalPipelines() map[string]struct{} {
	return map[string]struct{}{"metrics": {}, "logs": {}, "traces": {}}
}

func requireHTTPS(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("HTTPS URL is required")
	}
	return parsed, nil
}

func normalizeGRPCEndpoint(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "grpcs" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("grpcs endpoint is required")
	}
	return parsed.Host, nil
}

func exporterServerName(input RenderInput, endpoint string) string {
	if input.RouteKind == "bastion_gateway" {
		return input.GatewayServerName
	}
	host, _, found := strings.Cut(endpoint, ":")
	if found {
		return host
	}
	return endpoint
}

func validServerName(value string) bool {
	parsed, err := url.Parse("https://" + strings.TrimSpace(value))
	return err == nil && parsed.Hostname() == value && parsed.Port() == "" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
