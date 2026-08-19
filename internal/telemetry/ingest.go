package telemetry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	collectlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collecttraces "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/storage/redis"
)

const (
	maxCompressedRequest = 4 << 20
	maxDecodedRequest    = 16 << 20
	maxAttributes        = 128
	maxAttributeKey      = 256
	maxAttributeValue    = 4 << 10
	maxLogBody           = 256 << 10
)

var errIngestRejected = errors.New("telemetry ingest rejected")

type TrustedIdentity struct {
	EnterpriseID         uuid.UUID
	ResourceID           uuid.UUID
	CollectorID          uuid.UUID
	ResourceType         string
	Role                 string
	CertificateSerial    string
	AuthorizationVersion int64
}

type IngestServer struct {
	collectmetrics.UnimplementedMetricsServiceServer
	collectlogs.UnimplementedLogsServiceServer
	collecttraces.UnimplementedTraceServiceServer
	Control            IngestControlStore
	Redis              *redis.Client
	Kafka              *kgo.Client
	Logger             *slog.Logger
	Identity           *IdentityService
	IngestGRPCEndpoint string
	IngestHTTPEndpoint string
}

func NewKafkaProducer(brokers []string, username, password string) (*kgo.Client, error) {
	if username == "" || password == "" {
		return nil, ErrUnavailable
	}
	return kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.SASL(scram.Auth{User: username, Pass: password}.AsSha512Mechanism()),
		kgo.RequiredAcks(kgo.AllISRAcks()), kgo.ProducerBatchCompression(kgo.ZstdCompression()))
}

func (server *IngestServer) RegisterGRPC(grpcServer *grpc.Server) {
	server.registerAdapters(grpcServer)
}

func NewIngestGRPCServer(server *IngestServer, tlsConfig *tls.Config) *grpc.Server {
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.MaxRecvMsgSize(maxDecodedRequest), grpc.MaxSendMsgSize(1<<20))
	server.RegisterGRPC(grpcServer)
	return grpcServer
}

func (server *IngestServer) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/identity/enroll", server.httpEnrollCollector)
	mux.HandleFunc("/v1/metrics", server.httpExport("metrics"))
	mux.HandleFunc("/v1/logs", server.httpExport("logs"))
	mux.HandleFunc("/v1/traces", server.httpExport("traces"))
	mux.HandleFunc("/v1/identity/rotate", server.httpRotateCertificate)
	return mux
}

func (server *IngestServer) httpEnrollCollector(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.TLS == nil || len(request.TLS.PeerCertificates) != 0 || server.Identity == nil ||
		request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		http.Error(writer, "telemetry enrollment rejected", http.StatusUnauthorized)
		return
	}
	var body struct {
		CollectorID string `json:"collector_id"`
		CSRPem      string `json:"csr_pem"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 24<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body.CollectorID == "" || body.CSRPem == "" {
		http.Error(writer, "telemetry enrollment request invalid", http.StatusBadRequest)
		return
	}
	result, err := server.Identity.Enroll(request.Context(), request.Header.Get("X-Argus-Telemetry-Enrollment-Token"), body.CollectorID, body.CSRPem)
	if err != nil {
		http.Error(writer, "telemetry enrollment rejected", http.StatusUnauthorized)
		return
	}
	server.writeCertificateResult(writer, result)
}

func (server *IngestServer) httpRotateCertificate(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.TLS == nil || len(request.TLS.PeerCertificates) != 1 || server.Identity == nil {
		http.Error(writer, "telemetry identity required", http.StatusUnauthorized)
		return
	}
	var body struct {
		CSRPem string `json:"csr_pem"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body.CSRPem == "" {
		http.Error(writer, "telemetry rotation request invalid", http.StatusBadRequest)
		return
	}
	result, err := server.Identity.Rotate(request.Context(), request.TLS.PeerCertificates[0], body.CSRPem)
	if err != nil {
		http.Error(writer, "telemetry certificate fenced", http.StatusUnauthorized)
		return
	}
	server.writeCertificateResult(writer, result)
}

func (server *IngestServer) writeCertificateResult(writer http.ResponseWriter, result EnrollmentResult) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"collector_id": result.CollectorID, "certificate_pem": result.CertificatePEM, "ca_bundle_pem": result.CABundlePEM,
		"ingest_grpc_endpoint": server.IngestGRPCEndpoint, "ingest_http_endpoint": server.IngestHTTPEndpoint,
		"certificate_expires_at": result.ExpiresAt,
	})
}

func (server *IngestServer) Export(ctx context.Context, request *collectmetrics.ExportMetricsServiceRequest) (*collectmetrics.ExportMetricsServiceResponse, error) {
	identity, err := server.grpcIdentity(ctx)
	if err != nil || validateMetrics(request) != nil {
		return nil, status.Error(codes.PermissionDenied, "telemetry identity or payload rejected")
	}
	identity, err = server.resolveMetricPayloadIdentity(ctx, identity, request)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "telemetry downstream identity rejected")
	}
	overrideMetricIdentity(request, identity)
	payload, err := proto.Marshal(request)
	if err == nil {
		err = server.publish(ctx, "metrics", identity, payload)
	}
	if err != nil {
		server.logPublishFailure("metrics", identity, err)
		return nil, status.Error(codes.Unavailable, "telemetry ingest unavailable")
	}
	return &collectmetrics.ExportMetricsServiceResponse{}, nil
}

func (server *IngestServer) ExportLogs(ctx context.Context, request *collectlogs.ExportLogsServiceRequest) (*collectlogs.ExportLogsServiceResponse, error) {
	identity, err := server.grpcIdentity(ctx)
	if err != nil || validateLogs(request) != nil {
		return nil, status.Error(codes.PermissionDenied, "telemetry identity or payload rejected")
	}
	identity, err = server.resolveLogPayloadIdentity(ctx, identity, request)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "telemetry downstream identity rejected")
	}
	overrideLogIdentity(request, identity)
	payload, err := proto.Marshal(request)
	if err == nil {
		err = server.publish(ctx, "logs", identity, payload)
	}
	if err != nil {
		server.logPublishFailure("logs", identity, err)
		return nil, status.Error(codes.Unavailable, "telemetry ingest unavailable")
	}
	return &collectlogs.ExportLogsServiceResponse{}, nil
}

func (server *IngestServer) ExportTraces(ctx context.Context, request *collecttraces.ExportTraceServiceRequest) (*collecttraces.ExportTraceServiceResponse, error) {
	identity, err := server.grpcIdentity(ctx)
	if err != nil || validateTraces(request) != nil {
		return nil, status.Error(codes.PermissionDenied, "telemetry identity or payload rejected")
	}
	identity, err = server.resolveTracePayloadIdentity(ctx, identity, request)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "telemetry downstream identity rejected")
	}
	overrideTraceIdentity(request, identity)
	payload, err := proto.Marshal(request)
	if err == nil {
		err = server.publish(ctx, "traces", identity, payload)
	}
	if err != nil {
		server.logPublishFailure("traces", identity, err)
		return nil, status.Error(codes.Unavailable, "telemetry ingest unavailable")
	}
	return &collecttraces.ExportTraceServiceResponse{}, nil
}

// ExportLogs and ExportTraces are adapters used by generated service wrappers.
func (server *IngestServer) grpcIdentity(ctx context.Context) (TrustedIdentity, error) {
	value, ok := peer.FromContext(ctx)
	if !ok {
		return TrustedIdentity{}, errIngestRejected
	}
	auth, ok := value.AuthInfo.(credentials.TLSInfo)
	if !ok || len(auth.State.PeerCertificates) != 1 {
		return TrustedIdentity{}, errIngestRejected
	}
	return server.resolveIdentity(ctx, auth.State.PeerCertificates[0])
}

func (server *IngestServer) httpExport(signal string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.TLS == nil || len(request.TLS.PeerCertificates) != 1 {
			http.Error(writer, "telemetry identity required", http.StatusUnauthorized)
			return
		}
		identity, err := server.resolveIdentity(request.Context(), request.TLS.PeerCertificates[0])
		if err != nil {
			http.Error(writer, "telemetry identity rejected", http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxCompressedRequest))
		if err != nil || len(body) > maxDecodedRequest {
			http.Error(writer, "telemetry payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		var message proto.Message
		switch signal {
		case "metrics":
			value := &collectmetrics.ExportMetricsServiceRequest{}
			err = proto.Unmarshal(body, value)
			if err == nil {
				err = validateMetrics(value)
				if err == nil {
					identity, err = server.resolveMetricPayloadIdentity(request.Context(), identity, value)
				}
				overrideMetricIdentity(value, identity)
				message = value
			}
		case "logs":
			value := &collectlogs.ExportLogsServiceRequest{}
			err = proto.Unmarshal(body, value)
			if err == nil {
				err = validateLogs(value)
				if err == nil {
					identity, err = server.resolveLogPayloadIdentity(request.Context(), identity, value)
				}
				overrideLogIdentity(value, identity)
				message = value
			}
		case "traces":
			value := &collecttraces.ExportTraceServiceRequest{}
			err = proto.Unmarshal(body, value)
			if err == nil {
				err = validateTraces(value)
				if err == nil {
					identity, err = server.resolveTracePayloadIdentity(request.Context(), identity, value)
				}
				overrideTraceIdentity(value, identity)
				message = value
			}
		}
		if err != nil || message == nil {
			http.Error(writer, "telemetry payload rejected", http.StatusBadRequest)
			return
		}
		encoded, _ := proto.Marshal(message)
		if err := server.publish(request.Context(), signal, identity, encoded); err != nil {
			http.Error(writer, "telemetry ingest unavailable", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/x-protobuf")
		writer.WriteHeader(http.StatusOK)
	}
}

func (server *IngestServer) resolveIdentity(ctx context.Context, certificate *x509.Certificate) (TrustedIdentity, error) {
	if server.Control == nil || certificate == nil || len(certificate.URIs) != 1 {
		return TrustedIdentity{}, errIngestRejected
	}
	identity, uriSAN, err := server.Control.ResolveCollectorIdentity(ctx, strings.ToLower(certificate.SerialNumber.Text(16)))
	if err != nil || uriSAN != certificate.URIs[0].String() || !validCollectorURI(certificate.URIs[0], identity.CollectorID) {
		return TrustedIdentity{}, errIngestRejected
	}
	identity.CertificateSerial = strings.ToLower(identity.CertificateSerial)
	return identity, nil
}

type downstreamReference struct {
	collectorID uuid.UUID
	serial      string
	found       bool
}

func downstreamFromAttributes(attributes []*commonpb.KeyValue) (downstreamReference, error) {
	collector, serial := "", ""
	for _, attribute := range attributes {
		if attribute == nil || attribute.Value == nil {
			continue
		}
		switch attribute.Key {
		case "argus.downstream.collector.id":
			collector = attribute.Value.GetStringValue()
		case "argus.downstream.certificate.serial":
			serial = attribute.Value.GetStringValue()
		}
	}
	if collector == "" && serial == "" {
		return downstreamReference{}, nil
	}
	id, err := uuid.Parse(collector)
	if err != nil || id == uuid.Nil || serial == "" {
		return downstreamReference{}, errIngestRejected
	}
	return downstreamReference{collectorID: id, serial: strings.ToLower(serial), found: true}, nil
}

func mergeDownstreamReference(current, next downstreamReference) (downstreamReference, error) {
	if !next.found {
		if current.found {
			return downstreamReference{}, errIngestRejected
		}
		return current, nil
	}
	if current.found && (current.collectorID != next.collectorID || current.serial != next.serial) {
		return downstreamReference{}, errIngestRejected
	}
	return next, nil
}

func (server *IngestServer) resolveDownstreamIdentity(ctx context.Context, gateway TrustedIdentity, reference downstreamReference) (TrustedIdentity, error) {
	if !reference.found {
		return gateway, nil
	}
	if gateway.Role == "daemonset" && reference.collectorID == gateway.CollectorID && reference.serial == gateway.CertificateSerial {
		return gateway, nil
	}
	if gateway.Role != "edge_gateway" {
		return TrustedIdentity{}, errIngestRejected
	}
	if server.Control == nil {
		return TrustedIdentity{}, errIngestRejected
	}
	identity, err := server.Control.ResolveDownstreamIdentity(ctx, gateway, reference.collectorID, reference.serial)
	if err != nil {
		return TrustedIdentity{}, errIngestRejected
	}
	return identity, nil
}

func validateDownstreamFacts(gateway TrustedIdentity, reference downstreamReference, certificateValid bool,
	downstream db.CollectorInstance, route db.TelemetryRoute) (TrustedIdentity, error) {
	if !reference.found || !certificateValid || downstream.ID != reference.collectorID || downstream.EnterpriseID != gateway.EnterpriseID ||
		downstream.ID == gateway.CollectorID || route.CollectorID != downstream.ID || route.EnterpriseID != downstream.EnterpriseID ||
		route.Kind != "bastion_gateway" || route.Status != "active" || !route.GatewayCollectorID.Valid || route.GatewayCollectorID.UUID != gateway.CollectorID {
		return TrustedIdentity{}, errIngestRejected
	}
	return TrustedIdentity{EnterpriseID: downstream.EnterpriseID, ResourceID: downstream.ResourceID, CollectorID: downstream.ID,
		ResourceType: downstream.ResourceType, Role: downstream.Role, AuthorizationVersion: downstream.AuthorizationVersion}, nil
}

func (server *IngestServer) resolveMetricPayloadIdentity(ctx context.Context, gateway TrustedIdentity, request *collectmetrics.ExportMetricsServiceRequest) (TrustedIdentity, error) {
	reference := downstreamReference{}
	for _, item := range request.ResourceMetrics {
		next, err := downstreamFromAttributes(item.GetResource().GetAttributes())
		if err != nil {
			return TrustedIdentity{}, err
		}
		reference, err = mergeDownstreamReference(reference, next)
		if err != nil {
			return TrustedIdentity{}, err
		}
	}
	return server.resolveDownstreamIdentity(ctx, gateway, reference)
}

func (server *IngestServer) resolveLogPayloadIdentity(ctx context.Context, gateway TrustedIdentity, request *collectlogs.ExportLogsServiceRequest) (TrustedIdentity, error) {
	reference := downstreamReference{}
	for _, item := range request.ResourceLogs {
		next, err := downstreamFromAttributes(item.GetResource().GetAttributes())
		if err != nil {
			return TrustedIdentity{}, err
		}
		reference, err = mergeDownstreamReference(reference, next)
		if err != nil {
			return TrustedIdentity{}, err
		}
	}
	return server.resolveDownstreamIdentity(ctx, gateway, reference)
}

func (server *IngestServer) resolveTracePayloadIdentity(ctx context.Context, gateway TrustedIdentity, request *collecttraces.ExportTraceServiceRequest) (TrustedIdentity, error) {
	reference := downstreamReference{}
	for _, item := range request.ResourceSpans {
		next, err := downstreamFromAttributes(item.GetResource().GetAttributes())
		if err != nil {
			return TrustedIdentity{}, err
		}
		reference, err = mergeDownstreamReference(reference, next)
		if err != nil {
			return TrustedIdentity{}, err
		}
	}
	return server.resolveDownstreamIdentity(ctx, gateway, reference)
}

func validCollectorURI(uri *url.URL, collectorID uuid.UUID) bool {
	return uri != nil && uri.Scheme == "spiffe" && uri.Host == "argus" && uri.Path == "/telemetry/collectors/"+collectorID.String()
}

func (server *IngestServer) publish(ctx context.Context, signal string, identity TrustedIdentity, payload []byte) error {
	if server.Redis == nil || server.Kafka == nil || server.Redis.Ready(ctx) != nil {
		return ErrUnavailable
	}
	minute := time.Now().UTC().Format("200601021504")
	key := "argus:telemetry:ingest:" + identity.CollectorID.String() + ":" + minute
	count, err := server.Redis.Raw.Incr(ctx, key).Result()
	if err != nil {
		return err
	}
	if count == 1 {
		_ = server.Redis.Raw.Expire(ctx, key, 2*time.Minute).Err()
	}
	if count > 1200 {
		return errors.New("telemetry ingest rate exceeded")
	}
	record := &kgo.Record{Topic: "otlp-" + signal, Key: []byte(identity.CollectorID.String()), Value: payload, Headers: []kgo.RecordHeader{
		{Key: "argus-enterprise-id", Value: []byte(identity.EnterpriseID.String())}, {Key: "argus-resource-id", Value: []byte(identity.ResourceID.String())},
		{Key: "argus-collector-id", Value: []byte(identity.CollectorID.String())}, {Key: "argus-resource-type", Value: []byte(identity.ResourceType)},
	}}
	return server.Kafka.ProduceSync(ctx, record).FirstErr()
}

func (server *IngestServer) logPublishFailure(signal string, identity TrustedIdentity, err error) {
	if server.Logger != nil {
		server.Logger.Warn("telemetry ingest publish failed", "signal", signal, "collector_id", identity.CollectorID, "error", err)
	}
}

func trustedAttributes(identity TrustedIdentity) []*commonpb.KeyValue {
	return []*commonpb.KeyValue{
		{Key: "argus.enterprise.id", Value: stringValue(identity.EnterpriseID.String())}, {Key: "argus.resource.id", Value: stringValue(identity.ResourceID.String())},
		{Key: "argus.collector.id", Value: stringValue(identity.CollectorID.String())}, {Key: "argus.resource.type", Value: stringValue(identity.ResourceType)},
	}
}

func stringValue(value string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}
}
func overwriteResource(resource **resourcepb.Resource, identity TrustedIdentity) {
	if *resource == nil {
		*resource = &resourcepb.Resource{}
	}
	filtered := (*resource).Attributes[:0]
	for _, item := range (*resource).Attributes {
		if !strings.HasPrefix(item.Key, "argus.") {
			filtered = append(filtered, item)
		}
	}
	(*resource).Attributes = append(filtered, trustedAttributes(identity)...)
}
func overrideMetricIdentity(value *collectmetrics.ExportMetricsServiceRequest, identity TrustedIdentity) {
	for _, item := range value.ResourceMetrics {
		overwriteResource(&item.Resource, identity)
	}
}
func overrideLogIdentity(value *collectlogs.ExportLogsServiceRequest, identity TrustedIdentity) {
	for _, item := range value.ResourceLogs {
		overwriteResource(&item.Resource, identity)
	}
}
func overrideTraceIdentity(value *collecttraces.ExportTraceServiceRequest, identity TrustedIdentity) {
	for _, item := range value.ResourceSpans {
		overwriteResource(&item.Resource, identity)
	}
}

func validateAttributes(values []*commonpb.KeyValue) error {
	if len(values) > maxAttributes {
		return errIngestRejected
	}
	for _, value := range values {
		if value == nil || len(value.Key) == 0 || len(value.Key) > maxAttributeKey {
			return errIngestRejected
		}
		encoded, _ := json.Marshal(value.Value)
		if len(encoded) > maxAttributeValue {
			return errIngestRejected
		}
	}
	return nil
}

func validateMetrics(value *collectmetrics.ExportMetricsServiceRequest) error {
	for _, resource := range value.ResourceMetrics {
		if resource.Resource != nil && validateAttributes(resource.Resource.Attributes) != nil {
			return errIngestRejected
		}
		for _, scope := range resource.ScopeMetrics {
			for _, metric := range scope.Metrics {
				switch data := metric.Data.(type) {
				case *metricspb.Metric_Gauge:
					for _, point := range data.Gauge.DataPoints {
						if validateAttributes(point.Attributes) != nil {
							return errIngestRejected
						}
					}
				case *metricspb.Metric_Sum:
					for _, point := range data.Sum.DataPoints {
						if validateAttributes(point.Attributes) != nil {
							return errIngestRejected
						}
					}
				case *metricspb.Metric_Histogram:
					for _, point := range data.Histogram.DataPoints {
						if validateAttributes(point.Attributes) != nil {
							return errIngestRejected
						}
					}
				case *metricspb.Metric_ExponentialHistogram:
					for _, point := range data.ExponentialHistogram.DataPoints {
						if validateAttributes(point.Attributes) != nil {
							return errIngestRejected
						}
					}
				case *metricspb.Metric_Summary:
					for _, point := range data.Summary.DataPoints {
						if validateAttributes(point.Attributes) != nil {
							return errIngestRejected
						}
					}
				default:
					return errIngestRejected
				}
			}
		}
	}
	return nil
}
func validateLogs(value *collectlogs.ExportLogsServiceRequest) error {
	for _, resource := range value.ResourceLogs {
		if resource.Resource != nil && validateAttributes(resource.Resource.Attributes) != nil {
			return errIngestRejected
		}
		for _, scope := range resource.ScopeLogs {
			for _, record := range scope.LogRecords {
				if validateAttributes(record.Attributes) != nil || anyValueSize(record.Body) > maxLogBody {
					return errIngestRejected
				}
			}
		}
	}
	return nil
}
func validateTraces(value *collecttraces.ExportTraceServiceRequest) error {
	for _, resource := range value.ResourceSpans {
		if resource.Resource != nil && validateAttributes(resource.Resource.Attributes) != nil {
			return errIngestRejected
		}
		for _, scope := range resource.ScopeSpans {
			for _, span := range scope.Spans {
				if validateAttributes(span.Attributes) != nil {
					return errIngestRejected
				}
			}
		}
	}
	return nil
}
func anyValueSize(value *commonpb.AnyValue) int {
	if value == nil {
		return 0
	}
	encoded, _ := json.Marshal(value)
	return len(encoded)
}

var _ collectmetrics.MetricsServiceServer = (*IngestServer)(nil)
var _ collectlogs.LogsServiceServer = (*logsAdapter)(nil)
var _ collecttraces.TraceServiceServer = (*tracesAdapter)(nil)

type logsAdapter struct {
	collectlogs.UnimplementedLogsServiceServer
	server *IngestServer
}

func (adapter *logsAdapter) Export(ctx context.Context, request *collectlogs.ExportLogsServiceRequest) (*collectlogs.ExportLogsServiceResponse, error) {
	return adapter.server.ExportLogs(ctx, request)
}

type tracesAdapter struct {
	collecttraces.UnimplementedTraceServiceServer
	server *IngestServer
}

func (adapter *tracesAdapter) Export(ctx context.Context, request *collecttraces.ExportTraceServiceRequest) (*collecttraces.ExportTraceServiceResponse, error) {
	return adapter.server.ExportTraces(ctx, request)
}

func (server *IngestServer) registerAdapters(grpcServer *grpc.Server) {
	collectmetrics.RegisterMetricsServiceServer(grpcServer, server)
	collectlogs.RegisterLogsServiceServer(grpcServer, &logsAdapter{server: server})
	collecttraces.RegisterTraceServiceServer(grpcServer, &tracesAdapter{server: server})
}

var _ = fmt.Sprintf
var _ = logspb.LogRecord{}
var _ = tracepb.Span{}
