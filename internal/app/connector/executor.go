package connector

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
)

const (
	commandTimeout             = 45 * time.Second
	collectorManagementTimeout = 3 * time.Minute
)

type commandOutcome struct {
	result *anypb.Any
	code   string
	detail string
	stop   bool
}

type commandExecutor struct{}

func (commandExecutor) execute(parent context.Context, command *connectorv1.ConnectorCommand, credential []byte) commandOutcome {
	ctx, cancel := context.WithTimeout(parent, timeoutForCommand(command.GetCommandType()))
	defer cancel()
	if command.GetTypedPayload() == nil || command.GetCommandId() == "" || command.GetExpiresAt() == nil || time.Now().After(command.GetExpiresAt().AsTime()) {
		return commandOutcome{code: "CONNECTOR_COMMAND_INVALID", detail: "missing, malformed, or expired command metadata"}
	}
	var value proto.Message
	var err error
	switch command.GetCommandType() {
	case "host_connection_probe":
		value, err = executeHostProbe(ctx, command.GetTypedPayload(), credential)
	case "kubernetes_connection_probe":
		value, err = executeKubernetesProbe(ctx, command.GetTypedPayload(), credential)
	case "kubernetes_resource_query":
		value, err = executeKubernetesQuery(ctx, command.GetTypedPayload(), credential)
	case "kubernetes_pod_logs":
		value, err = executeKubernetesLogs(ctx, command.GetTypedPayload(), credential)
	case "connector_uninstall":
		var request connectorv1.ConnectorUninstall
		if err = command.GetTypedPayload().UnmarshalTo(&request); err == nil {
			value = &connectorv1.ConnectorUninstallResult{IdentityRemoved: true, ServiceStopped: true}
		}
	case "collector_management":
		value, err = executeCollectorManagement(ctx, command.GetTypedPayload(), credential)
	default:
		err = errors.New("unsupported Connector command")
	}
	if err != nil {
		code := "CONNECTOR_COMMAND_FAILED"
		if command.GetCommandType() == "collector_management" {
			code = collectorManagementFailureCode(err)
		}
		return commandOutcome{code: code, detail: err.Error()}
	}
	typed, err := anypb.New(value)
	if err != nil {
		return commandOutcome{code: "CONNECTOR_RESULT_INVALID", detail: err.Error()}
	}
	return commandOutcome{result: typed, stop: command.GetCommandType() == "connector_uninstall"}
}

func timeoutForCommand(commandType string) time.Duration {
	if commandType == "collector_management" {
		return collectorManagementTimeout
	}
	return commandTimeout
}

func executeHostProbe(ctx context.Context, payload *anypb.Any, credential []byte) (*connectorv1.HostConnectionProbeResult, error) {
	var request connectorv1.HostConnectionProbe
	if payload.UnmarshalTo(&request) != nil || request.Address == "" || request.Port == 0 || request.Username == "" || len(credential) == 0 {
		return nil, errors.New("invalid Host probe")
	}
	started := time.Now()
	resolved, err := resolveTarget(ctx, request.Address)
	if err != nil {
		return nil, err
	}
	result := &connectorv1.HostConnectionProbeResult{ResolvedIps: resolved, Platform: "linux"}
	switch request.Protocol {
	case "ssh":
		auth, err := connectorSSHAuth(credential)
		if err != nil {
			return nil, err
		}
		configuration := &ssh.ClientConfig{User: request.Username, Auth: []ssh.AuthMethod{auth}, Timeout: 10 * time.Second,
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				result.HostKeyFingerprint = ssh.FingerprintSHA256(key)
				if request.ExpectedHostKeyFingerprint != "" && request.ExpectedHostKeyFingerprint != result.HostKeyFingerprint {
					return errors.New("Host key changed")
				}
				return nil
			}}
		connection, err := ssh.Dial("tcp", net.JoinHostPort(request.Address, fmt.Sprint(request.Port)), configuration)
		if err != nil {
			return nil, err
		}
		result.RemoteVersion = string(connection.ServerVersion())
		_ = connection.Close()
	case "winrm":
		result.Platform = "windows"
		scheme := "http"
		if request.Port == 443 || request.Port == 5986 {
			scheme = "https"
		}
		transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: request.Address}}
		client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: rejectConnectorRedirect}
		body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`
		httpRequest, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			(&url.URL{Scheme: scheme, Host: net.JoinHostPort(request.Address, fmt.Sprint(request.Port)), Path: "/wsman"}).String(), strings.NewReader(body))
		httpRequest.Header.Set("Content-Type", "application/soap+xml;charset=UTF-8")
		httpRequest.SetBasicAuth(request.Username, string(credential))
		response, err := client.Do(httpRequest)
		if err != nil {
			return nil, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		_ = response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode >= 500 {
			return nil, errors.New("WinRM authentication failed")
		}
		result.RemoteVersion = response.Header.Get("Server")
	default:
		return nil, errors.New("unsupported Host probe protocol")
	}
	result.LatencyMillis = uint64(time.Since(started).Milliseconds())
	return result, nil
}

func executeKubernetesProbe(ctx context.Context, payload *anypb.Any, credential []byte) (*connectorv1.KubernetesConnectionProbeResult, error) {
	var request connectorv1.KubernetesConnectionProbe
	if payload.UnmarshalTo(&request) != nil || request.ApiServer == "" {
		return nil, errors.New("invalid Kubernetes probe")
	}
	config, err := connectorKubeconfig(credential, request.ApiServer)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil {
		return nil, err
	}
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil {
		return nil, err
	}
	ready := uint32(0)
	for _, node := range nodes.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				ready++
				break
			}
		}
	}
	names := make([]string, 0, len(namespaces.Items))
	for _, namespace := range namespaces.Items {
		names = append(names, namespace.Name)
	}
	return &connectorv1.KubernetesConnectionProbeResult{ServerVersion: version.GitVersion, NodeCount: uint32(len(nodes.Items)),
		ReadyNodeCount: ready, Namespaces: names}, nil
}

func executeKubernetesQuery(ctx context.Context, payload *anypb.Any, credential []byte) (*connectorv1.KubernetesResourceQueryResult, error) {
	var request connectorv1.KubernetesResourceQuery
	if payload.UnmarshalTo(&request) != nil || request.ClusterId == "" || request.ResourceType == "" {
		return nil, errors.New("invalid Kubernetes query")
	}
	config, err := connectorKubeconfig(credential, "")
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	gvr, namespaced, ok := connectorResourceMapping(request.ResourceType)
	if !ok {
		return nil, errors.New("unsupported Kubernetes resource type")
	}
	var source dynamic.ResourceInterface = client.Resource(gvr)
	if namespaced {
		if request.Namespace == "" {
			return nil, errors.New("Kubernetes namespace is required")
		}
		source = client.Resource(gvr).Namespace(request.Namespace)
	}
	limit := int64(request.Limit)
	if limit < 1 || limit > 200 {
		limit = 50
	}
	options := metav1.ListOptions{Limit: limit, Continue: request.ContinueToken, LabelSelector: request.LabelSelector}
	if request.Name != "" {
		options.FieldSelector = "metadata.name=" + request.Name
	}
	list, err := source.List(ctx, options)
	if err != nil {
		return nil, err
	}
	maxBytes := int(request.MaxResultBytes)
	if maxBytes <= 0 || maxBytes > 1<<20 {
		maxBytes = 1 << 20
	}
	result := &connectorv1.KubernetesResourceQueryResult{ContinueToken: list.GetContinue()}
	used := 0
	for _, item := range list.Items {
		encoded, err := json.Marshal(item.Object)
		if err != nil {
			return nil, err
		}
		if used+len(encoded) > maxBytes {
			result.Truncated = true
			break
		}
		result.ResourcesJson = append(result.ResourcesJson, encoded)
		used += len(encoded)
	}
	return result, nil
}

func executeKubernetesLogs(ctx context.Context, payload *anypb.Any, credential []byte) (*connectorv1.KubernetesPodLogsResult, error) {
	var request connectorv1.KubernetesPodLogsQuery
	if payload.UnmarshalTo(&request) != nil || request.ClusterId == "" || request.Namespace == "" || request.Pod == "" {
		return nil, errors.New("invalid Kubernetes logs query")
	}
	config, err := connectorKubeconfig(credential, "")
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	tail := int64(request.TailLines)
	options := &corev1.PodLogOptions{TailLines: &tail, Container: request.Container}
	stream, err := client.CoreV1().Pods(request.Namespace).GetLogs(request.Pod, options).Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	limit := int64(request.MaxResultBytes)
	if limit <= 0 || limit > 1<<20 {
		limit = 1 << 20
	}
	content, err := io.ReadAll(io.LimitReader(stream, limit+1))
	if err != nil {
		return nil, err
	}
	truncated := int64(len(content)) > limit
	if truncated {
		content = content[:limit]
	}
	return &connectorv1.KubernetesPodLogsResult{Content: content, Truncated: truncated}, nil
}

func connectorKubeconfig(value []byte, apiServer string) (*rest.Config, error) {
	if len(value) == 0 {
		return rest.InClusterConfig()
	}
	config, err := clientcmd.RESTConfigFromKubeConfig(value)
	if err != nil || config.Insecure || config.ExecProvider != nil || config.AuthProvider != nil || config.Proxy != nil || config.WrapTransport != nil {
		return nil, errors.New("unsafe kubeconfig")
	}
	if apiServer != "" {
		parsed, err := url.Parse(apiServer)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, errors.New("Kubernetes API server must use HTTPS")
		}
		config.Host = parsed.String()
	}
	config.Timeout = commandTimeout
	return config, nil
}

func connectorSSHAuth(value []byte) (ssh.AuthMethod, error) {
	if bytes.Contains(value, []byte("PRIVATE KEY")) {
		signer, err := ssh.ParsePrivateKey(value)
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(signer), nil
	}
	return ssh.Password(string(value)), nil
}

func resolveTarget(ctx context.Context, hostname string) ([]string, error) {
	values, err := (&net.Resolver{}).LookupIPAddr(ctx, hostname)
	if err != nil || len(values) == 0 {
		return nil, errors.New("target DNS resolution failed")
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.IP.String())
	}
	return result, nil
}

func rejectConnectorRedirect(_ *http.Request, _ []*http.Request) error {
	return errors.New("redirects are not allowed")
}

func connectorResourceMapping(resourceType string) (schema.GroupVersionResource, bool, bool) {
	values := map[string]struct {
		gvr        schema.GroupVersionResource
		namespaced bool
	}{
		"namespace":   {schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, false},
		"node":        {schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, false},
		"pod":         {schema.GroupVersionResource{Version: "v1", Resource: "pods"}, true},
		"service":     {schema.GroupVersionResource{Version: "v1", Resource: "services"}, true},
		"deployment":  {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true},
		"statefulset": {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true},
		"daemonset":   {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true},
	}
	value, ok := values[strings.ToLower(resourceType)]
	return value.gvr, value.namespaced, ok
}
