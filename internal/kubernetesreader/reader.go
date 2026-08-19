package kubernetesreader

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type Reader struct {
	Store     *postgres.Store
	Secrets   secret.Service
	Validator resource.DirectTargetValidator
	Notifier  interface {
		NotifyConnectorCommand(context.Context, uuid.UUID, int64)
	}
	Timeout time.Duration
}

func (reader Reader) List(ctx context.Context, cluster db.KubernetesCluster, query resource.KubernetesQuery) ([]resource.KubernetesObject, error) {
	if cluster.ConnectionMode == "direct" {
		return reader.listDirect(ctx, cluster, query)
	}
	payload := &connectorv1.KubernetesResourceQuery{ClusterId: cluster.ID.String(), ResourceType: query.ResourceType, Namespace: query.Namespace,
		Name: query.Query, Limit: uint32(query.Limit), MaxResultBytes: 1 << 20}
	var typed connectorv1.KubernetesResourceQueryResult
	err := reader.connectorCommand(ctx, cluster, "kubernetes_resource_query", payload, &typed)
	if err != nil {
		return nil, err
	}
	items := make([]resource.KubernetesObject, 0, len(typed.ResourcesJson))
	for _, encoded := range typed.ResourcesJson {
		item, err := objectFromJSON(encoded, query.ResourceType)
		if err != nil {
			return nil, resource.ErrKubernetesUnavailable
		}
		items = append(items, item)
	}
	return items, nil
}

func (reader Reader) PodLogs(ctx context.Context, cluster db.KubernetesCluster, query resource.PodLogsQuery) ([]byte, bool, error) {
	if cluster.ConnectionMode == "direct" {
		return reader.logsDirect(ctx, cluster, query)
	}
	payload := &connectorv1.KubernetesPodLogsQuery{ClusterId: cluster.ID.String(), Namespace: query.Namespace, Pod: query.Pod,
		Container: query.Container, TailLines: uint32(query.TailLines), MaxResultBytes: 1 << 20}
	var typed connectorv1.KubernetesPodLogsResult
	err := reader.connectorCommand(ctx, cluster, "kubernetes_pod_logs", payload, &typed)
	if err != nil {
		return nil, false, err
	}
	return typed.Content, typed.Truncated, nil
}

func (reader Reader) listDirect(ctx context.Context, cluster db.KubernetesCluster, query resource.KubernetesQuery) ([]resource.KubernetesObject, error) {
	config, httpClient, cleanup, err := reader.directConfig(ctx, cluster)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	client, err := dynamic.NewForConfigAndClient(config, httpClient)
	if err != nil {
		return nil, resource.ErrKubernetesUnavailable
	}
	gvr, namespaced, ok := resourceMapping(query.ResourceType)
	if !ok {
		return nil, resource.ErrKubernetesUnavailable
	}
	interfaceForResource := client.Resource(gvr)
	var source dynamic.ResourceInterface = interfaceForResource
	if namespaced {
		source = interfaceForResource.Namespace(query.Namespace)
	}
	options := metav1.ListOptions{Limit: int64(query.Limit)}
	if query.Query != "" {
		options.FieldSelector = "metadata.name=" + query.Query
	}
	list, err := source.List(ctx, options)
	if err != nil {
		return nil, resource.ErrKubernetesUnavailable
	}
	items := make([]resource.KubernetesObject, 0, len(list.Items))
	for _, item := range list.Items {
		items = append(items, resource.KubernetesObject{ResourceType: query.ResourceType, Namespace: item.GetNamespace(), Name: item.GetName(),
			Labels: item.GetLabels(), Summary: map[string]any{"uid": string(item.GetUID()), "created_at": item.GetCreationTimestamp().Time}})
	}
	return items, nil
}

func (reader Reader) logsDirect(ctx context.Context, cluster db.KubernetesCluster, query resource.PodLogsQuery) ([]byte, bool, error) {
	config, httpClient, cleanup, err := reader.directConfig(ctx, cluster)
	if err != nil {
		return nil, false, err
	}
	defer cleanup()
	client, err := kubernetes.NewForConfigAndClient(config, httpClient)
	if err != nil {
		return nil, false, resource.ErrKubernetesUnavailable
	}
	options := &corev1.PodLogOptions{TailLines: &query.TailLines}
	if query.Container != "" {
		options.Container = query.Container
	}
	stream, err := client.CoreV1().Pods(query.Namespace).GetLogs(query.Pod, options).Stream(ctx)
	if err != nil {
		return nil, false, resource.ErrKubernetesUnavailable
	}
	defer stream.Close()
	content, err := io.ReadAll(io.LimitReader(stream, (1<<20)+1))
	if err != nil {
		return nil, false, resource.ErrKubernetesUnavailable
	}
	truncated := len(content) > 1<<20
	if truncated {
		content = content[:1<<20]
	}
	return content, truncated, nil
}

func (reader Reader) directConfig(ctx context.Context, cluster db.KubernetesCluster) (*rest.Config, *http.Client, func(), error) {
	if !cluster.CredentialID.Valid {
		return nil, nil, func() {}, resource.ErrKubernetesUnavailable
	}
	issued, err := reader.Secrets.IssueLease(secret.WithActorType(ctx, "direct_executor"), "kubernetes-reader", cluster.EnterpriseID, secret.LeaseRequest{
		CredentialID: cluster.CredentialID.UUID, OperationRef: "kubernetes.query/" + uuid.NewString(), TargetResourceType: "kubernetes_cluster",
		TargetResourceID: cluster.ID, RecipientType: "direct_executor", RecipientID: "kubernetes-reader", Protocol: "kubernetes", TTL: time.Minute})
	if err != nil {
		return nil, nil, func() {}, resource.ErrKubernetesUnavailable
	}
	cleanup := func() {
		clear(issued.Value)
		_ = reader.Secrets.ConsumeLease(context.Background(), cluster.EnterpriseID, issued.Lease.ID)
	}
	config, err := clientcmd.RESTConfigFromKubeConfig(issued.Value)
	if err != nil || config.Insecure || config.ExecProvider != nil || config.AuthProvider != nil || config.Proxy != nil || config.WrapTransport != nil {
		cleanup()
		return nil, nil, func() {}, resource.ErrKubernetesUnavailable
	}
	target, addresses, err := reader.Validator.ResolveHTTPS(ctx, cluster.ApiServer)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	config.Host = target.Scheme + "://" + target.Host
	if config.ServerName == "" {
		config.ServerName = target.Hostname()
	}
	port := 443
	if target.Port() != "" {
		_, _ = fmt.Sscan(target.Port(), &port)
	}
	config.Dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(addresses[0].String(), fmt.Sprint(port)))
	}
	config.Timeout = reader.timeout()
	transport, err := rest.TransportFor(config)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, resource.ErrKubernetesUnavailable
	}
	httpClient := &http.Client{
		Transport: revalidatingTransport{
			base:      transport,
			validator: reader.Validator,
			host:      target.Hostname(),
			addresses: addresses,
		},
		Timeout:       reader.timeout(),
		CheckRedirect: rejectRedirect,
	}
	return config, httpClient, cleanup, nil
}

type revalidatingTransport struct {
	base      http.RoundTripper
	validator resource.DirectTargetValidator
	host      string
	addresses []netip.Addr
}

func (transport revalidatingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := transport.validator.Revalidate(request.Context(), transport.host, transport.addresses); err != nil {
		return nil, err
	}
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if err := transport.validator.Revalidate(request.Context(), transport.host, transport.addresses); err != nil {
		response.Body.Close()
		return nil, err
	}
	return response, nil
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return resource.ErrDirectTargetDenied
}

func (reader Reader) connectorCommand(ctx context.Context, cluster db.KubernetesCluster, commandType string, payload any, result proto.Message) error {
	connectorRecord, err := reader.connectorForCluster(ctx, cluster)
	if err != nil {
		return err
	}
	var leaseID uuid.NullUUID
	if cluster.CredentialID.Valid {
		issued, err := reader.Secrets.IssueLease(secret.WithActorType(ctx, "connector_gateway"), connectorRecord.ID.String(), cluster.EnterpriseID, secret.LeaseRequest{
			CredentialID: cluster.CredentialID.UUID, OperationRef: "kubernetes.query/" + uuid.NewString(), TargetResourceType: "kubernetes_cluster", TargetResourceID: cluster.ID,
			RecipientType: "connector", RecipientID: connectorRecord.ID.String(), Protocol: "kubernetes", TTL: time.Minute})
		if err != nil {
			return resource.ErrKubernetesUnavailable
		}
		clear(issued.Value)
		leaseID = uuid.NullUUID{UUID: issued.Lease.ID, Valid: true}
	}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > 1<<20 {
		return resource.ErrKubernetesUnavailable
	}
	hash := sha256.Sum256(encoded)
	commandID := "cmd_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	command, err := reader.Store.Queries.CreateConnectorCommand(ctx, db.CreateConnectorCommandParams{ID: uuid.New(), CommandID: commandID,
		EnterpriseID: cluster.EnterpriseID, ConnectorID: connectorRecord.ID, ConnectionEpoch: connectorRecord.ConnectionEpoch,
		OperationRef: cluster.ID.String(), CredentialLeaseID: leaseID, CommandType: commandType, PayloadSchemaVersion: "argus.connector_command/v1",
		Payload: encoded, PayloadHash: hash[:], IdempotencyKey: uuid.NewString(), ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(reader.timeout()), Valid: true}})
	if err != nil {
		return resource.ErrKubernetesUnavailable
	}
	if reader.Notifier != nil {
		reader.Notifier.NotifyConnectorCommand(ctx, command.ConnectorID, command.ConnectionEpoch)
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(reader.timeout())
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return resource.ErrKubernetesUnavailable
		case <-ticker.C:
			command, err = reader.Store.Queries.GetConnectorCommand(ctx, db.GetConnectorCommandParams{CommandID: command.CommandID, ConnectorID: command.ConnectorID, ConnectionEpoch: command.ConnectionEpoch})
			if err != nil {
				return resource.ErrKubernetesUnavailable
			}
			if command.Status == "failed" || command.Status == "timed_out" || command.Status == "result_unknown" {
				return resource.ErrKubernetesUnavailable
			}
			if command.Status == "succeeded" {
				if err := unmarshalConnectorResult(command.Result, result); err != nil {
					return err
				}
				return nil
			}
		}
	}
}

func unmarshalConnectorResult(encoded json.RawMessage, result proto.Message) error {
	if len(encoded) == 0 || result == nil || protojson.Unmarshal(encoded, result) != nil {
		return resource.ErrKubernetesUnavailable
	}
	return nil
}

func (reader Reader) connectorForCluster(ctx context.Context, cluster db.KubernetesCluster) (db.Connector, error) {
	connectorID := cluster.ConnectorID
	if cluster.ConnectionMode == "via_bastion" {
		if !cluster.BastionScopeID.Valid {
			return db.Connector{}, resource.ErrKubernetesUnavailable
		}
		scope, err := reader.Store.Queries.GetBastionScope(ctx, db.GetBastionScopeParams{ID: cluster.BastionScopeID.UUID, EnterpriseID: cluster.EnterpriseID})
		if err != nil || !scope.ActiveConnectorID.Valid {
			return db.Connector{}, resource.ErrKubernetesUnavailable
		}
		connectorID = scope.ActiveConnectorID
	}
	if !connectorID.Valid {
		return db.Connector{}, resource.ErrKubernetesUnavailable
	}
	value, err := reader.Store.Queries.GetConnector(ctx, db.GetConnectorParams{ID: connectorID.UUID, EnterpriseID: cluster.EnterpriseID})
	if err != nil || value.Status != "online" || value.ConnectionEpoch < 1 {
		return db.Connector{}, resource.ErrKubernetesUnavailable
	}
	return value, nil
}

func objectFromJSON(encoded []byte, resourceType string) (resource.KubernetesObject, error) {
	var value struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	if json.Unmarshal(encoded, &value) != nil || value.Metadata.Name == "" {
		return resource.KubernetesObject{}, errors.New("invalid Kubernetes resource")
	}
	return resource.KubernetesObject{ResourceType: resourceType, Namespace: value.Metadata.Namespace, Name: value.Metadata.Name,
		Labels: value.Metadata.Labels, Summary: map[string]any{}}, nil
}

func resourceMapping(resourceType string) (schema.GroupVersionResource, bool, bool) {
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
	value, ok := values[resourceType]
	return value.gvr, value.namespaced, ok
}

func (reader Reader) timeout() time.Duration {
	if reader.Timeout <= 0 || reader.Timeout > 30*time.Second {
		return 15 * time.Second
	}
	return reader.Timeout
}

var _ resource.KubernetesReader = Reader{}
