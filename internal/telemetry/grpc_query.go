package telemetry

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	redislib "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/common/v1"
	telemetryv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/telemetry/v1"
)

const querySchemaVersion = "argus.telemetry_query/v1"

type QueryRPCServer struct {
	telemetryv1.UnimplementedTelemetryQueryServiceServer
	Backend QueryBackend
	Logger  *slog.Logger
	Limiter QueryConcurrencyLimiter
}

type QueryConcurrencyLimiter interface {
	Acquire(context.Context, uuid.UUID, string, time.Duration) (func(), error)
}

type RedisQueryConcurrencyLimiter struct {
	Client *redislib.Client
	Limit  int64
}

var acquireQueryPermit = redislib.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[2]) end
if current > tonumber(ARGV[1]) then
  redis.call('DECR', KEYS[1])
  return 0
end
return 1
`)

func (limiter RedisQueryConcurrencyLimiter) Acquire(ctx context.Context, enterpriseID uuid.UUID, scope string, ttl time.Duration) (func(), error) {
	if limiter.Client == nil || limiter.Limit < 1 || scope == "" {
		return nil, ErrQueryBackend
	}
	key := "argus:telemetry:query:" + enterpriseID.String() + ":" + scope
	allowed, err := acquireQueryPermit.Run(ctx, limiter.Client, []string{key}, limiter.Limit, ttl.Milliseconds()).Int64()
	if err != nil || allowed != 1 {
		return nil, ErrQueryBackend
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = limiter.Client.Decr(releaseCtx, key).Err()
	}, nil
}

func RegisterQueryRPC(server *grpc.Server, backend QueryBackend, limiter QueryConcurrencyLimiter, logger *slog.Logger) {
	telemetryv1.RegisterTelemetryQueryServiceServer(server, &QueryRPCServer{Backend: backend, Limiter: limiter, Logger: logger})
}

func (server *QueryRPCServer) QueryMetrics(ctx context.Context, request *telemetryv1.QueryMetricsRequest) (*telemetryv1.QueryMetricsResponse, error) {
	result, err := server.query(ctx, "metrics", request.GetQuery())
	return &telemetryv1.QueryMetricsResponse{Result: result}, err
}

func (server *QueryRPCServer) QueryLogs(ctx context.Context, request *telemetryv1.QueryLogsRequest) (*telemetryv1.QueryLogsResponse, error) {
	result, err := server.query(ctx, "logs", request.GetQuery())
	return &telemetryv1.QueryLogsResponse{Result: result}, err
}

func (server *QueryRPCServer) QueryTraces(ctx context.Context, request *telemetryv1.QueryTracesRequest) (*telemetryv1.QueryTracesResponse, error) {
	result, err := server.query(ctx, "traces", request.GetQuery())
	return &telemetryv1.QueryTracesResponse{Result: result}, err
}

func (server *QueryRPCServer) QueryOverview(ctx context.Context, request *telemetryv1.QueryOverviewRequest) (*telemetryv1.QueryOverviewResponse, error) {
	result, err := server.query(ctx, "overview", request.GetQuery())
	return &telemetryv1.QueryOverviewResponse{Result: result}, err
}

func (server *QueryRPCServer) query(ctx context.Context, signal string, value *telemetryv1.TelemetryQueryRequest) (*telemetryv1.TelemetryQueryResponse, error) {
	if server.Backend == nil || server.Limiter == nil || !trustedRPCPeer(ctx) {
		return nil, status.Error(codes.Unauthenticated, "telemetry query mTLS identity required")
	}
	request, err := queryRequestFromProto(signal, value)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "telemetry query rejected")
	}
	release, err := server.Limiter.Acquire(ctx, request.EnterpriseID, value.Scope.ScopeHash, time.Duration(request.TimeoutMS)*time.Millisecond+5*time.Second)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, "telemetry query concurrency unavailable")
	}
	defer release()
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
	defer cancel()

	output := queryWireResponse{}
	switch signal {
	case "metrics":
		output.Metrics, output.Meta, err = server.Backend.Metrics(queryCtx, request)
	case "logs":
		output.Logs, output.Meta, err = server.Backend.Logs(queryCtx, request)
	case "traces":
		output.Traces, output.Meta, err = server.Backend.Traces(queryCtx, request)
	case "overview":
		var overview Overview
		overview, err = server.Backend.Overview(queryCtx, request)
		output.Overview = &overview
	}
	if err != nil {
		if server.Logger != nil {
			server.Logger.Warn("telemetry query backend failed", "signal", signal, "error", err)
		}
		return nil, status.Error(codes.Unavailable, "telemetry query unavailable")
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return nil, status.Error(codes.Internal, "telemetry query encoding failed")
	}
	return &telemetryv1.TelemetryQueryResponse{
		SchemaVersion:  "argus.telemetry_result/v1",
		ResultJson:     encoded,
		Partial:        output.Meta.Partial,
		PartialReasons: output.Meta.PartialReasons,
		ScannedBytes:   uint64(max(output.Meta.ScannedBytes, 0)),
		ElapsedMillis:  uint64(max(output.Meta.ElapsedMS, 0)),
	}, nil
}

func trustedRPCPeer(ctx context.Context) bool {
	value, ok := peer.FromContext(ctx)
	if !ok {
		return false
	}
	info, ok := value.AuthInfo.(credentials.TLSInfo)
	return ok && len(info.State.PeerCertificates) == 1
}

func queryRequestFromProto(signal string, value *telemetryv1.TelemetryQueryRequest) (QueryRequest, error) {
	if value == nil || value.SchemaVersion != querySchemaVersion || value.Scope == nil || !slices.Contains(value.Scope.AllowedSignals, signal) ||
		value.From == nil || value.To == nil || !value.From.IsValid() || !value.To.IsValid() || value.Limit < 1 || value.Limit > HardMaxRows ||
		value.MaxScanBytes < 1 || value.MaxScanBytes > uint64(HardMaxScanBytes) || value.TimeoutMillis < 1 || value.TimeoutMillis > uint32(HardTimeout/time.Millisecond) {
		return QueryRequest{}, ErrQueryInvalid
	}
	enterpriseID, err := uuid.Parse(value.Scope.EnterpriseId)
	if err != nil || enterpriseID == uuid.Nil || len(value.Scope.AuthorizedResources) == 0 || len(value.Scope.AuthorizedResources) > 1000 {
		return QueryRequest{}, ErrQueryInvalid
	}
	resourceIDs := make([]uuid.UUID, 0, len(value.Scope.AuthorizedResources))
	for _, resource := range value.Scope.AuthorizedResources {
		id, parseErr := uuid.Parse(resource.ResourceId)
		if parseErr != nil || id == uuid.Nil || (resource.ResourceType != "host" && resource.ResourceType != "kubernetes_cluster") {
			return QueryRequest{}, ErrQueryInvalid
		}
		resourceIDs = append(resourceIDs, id)
	}
	if value.Scope.ScopeHash != scopeHash(enterpriseID, resourceIDs, int64(value.Scope.AuthorizationVersion), signal) {
		return QueryRequest{}, ErrQueryInvalid
	}
	filter := map[string]any{}
	if len(value.FilterJson) > 0 && json.Unmarshal(value.FilterJson, &filter) != nil {
		return QueryRequest{}, ErrQueryInvalid
	}
	from, to := value.From.AsTime(), value.To.AsTime()
	if !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		return QueryRequest{}, ErrQueryInvalid
	}
	return QueryRequest{
		EnterpriseID: enterpriseID, ResourceIDs: resourceIDs, AuthorizationVersion: int64(value.Scope.AuthorizationVersion), Signal: signal,
		From: from, To: to, Limit: int(value.Limit), Cursor: value.Cursor, Filter: filter, Sensitive: value.SensitiveFields,
		MaxScanBytes: int64(value.MaxScanBytes), TimeoutMS: int(value.TimeoutMillis),
	}, nil
}

type GRPCQueryBackend struct {
	connection *grpc.ClientConn
	client     telemetryv1.TelemetryQueryServiceClient
}

func NewGRPCQueryBackend(endpoint string, tlsConfig *tls.Config) (*GRPCQueryBackend, error) {
	target, err := grpcTarget(endpoint)
	if err != nil || tlsConfig == nil {
		return nil, ErrQueryBackend
	}
	connection, err := grpc.NewClient(target, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, err
	}
	return &GRPCQueryBackend{connection: connection, client: telemetryv1.NewTelemetryQueryServiceClient(connection)}, nil
}

func (backend *GRPCQueryBackend) Close() error {
	if backend == nil || backend.connection == nil {
		return nil
	}
	return backend.connection.Close()
}

func (backend *GRPCQueryBackend) call(ctx context.Context, signal string, input QueryRequest) (queryWireResponse, error) {
	if backend == nil || backend.client == nil {
		return queryWireResponse{}, ErrQueryBackend
	}
	filter, err := json.Marshal(input.Filter)
	if err != nil {
		return queryWireResponse{}, err
	}
	resources := make([]*commonv1.ResourceRef, 0, len(input.ResourceIDs))
	for _, id := range input.ResourceIDs {
		resources = append(resources, &commonv1.ResourceRef{ResourceType: "host", ResourceId: id.String()})
	}
	scope := &telemetryv1.TelemetryQueryScope{
		EnterpriseId: input.EnterpriseID.String(), AuthorizedResources: resources, AllowedSignals: []string{signal},
		AuthorizationVersion: uint64(input.AuthorizationVersion), ScopeHash: scopeHash(input.EnterpriseID, input.ResourceIDs, input.AuthorizationVersion, signal),
	}
	request := &telemetryv1.TelemetryQueryRequest{
		SchemaVersion: querySchemaVersion, Scope: scope, From: timestamppb.New(input.From), To: timestamppb.New(input.To),
		Limit: uint32(input.Limit), Cursor: input.Cursor, FilterJson: filter, SensitiveFields: input.Sensitive,
		MaxScanBytes: uint64(input.MaxScanBytes), TimeoutMillis: uint32(input.TimeoutMS),
	}
	var response *telemetryv1.TelemetryQueryResponse
	switch signal {
	case "metrics":
		var wrapped *telemetryv1.QueryMetricsResponse
		wrapped, err = backend.client.QueryMetrics(ctx, &telemetryv1.QueryMetricsRequest{Query: request})
		response = wrapped.GetResult()
	case "logs":
		var wrapped *telemetryv1.QueryLogsResponse
		wrapped, err = backend.client.QueryLogs(ctx, &telemetryv1.QueryLogsRequest{Query: request})
		response = wrapped.GetResult()
	case "traces":
		var wrapped *telemetryv1.QueryTracesResponse
		wrapped, err = backend.client.QueryTraces(ctx, &telemetryv1.QueryTracesRequest{Query: request})
		response = wrapped.GetResult()
	case "overview":
		var wrapped *telemetryv1.QueryOverviewResponse
		wrapped, err = backend.client.QueryOverview(ctx, &telemetryv1.QueryOverviewRequest{Query: request})
		response = wrapped.GetResult()
	default:
		return queryWireResponse{}, ErrQueryInvalid
	}
	if err != nil || response == nil || response.SchemaVersion != "argus.telemetry_result/v1" {
		return queryWireResponse{}, ErrQueryBackend
	}
	var output queryWireResponse
	if json.Unmarshal(response.ResultJson, &output) != nil {
		return queryWireResponse{}, ErrQueryBackend
	}
	return output, nil
}

func (backend *GRPCQueryBackend) Metrics(ctx context.Context, input QueryRequest) ([]MetricSeries, QueryMeta, error) {
	output, err := backend.call(ctx, "metrics", input)
	return output.Metrics, output.Meta, err
}

func (backend *GRPCQueryBackend) Logs(ctx context.Context, input QueryRequest) ([]LogRecord, QueryMeta, error) {
	output, err := backend.call(ctx, "logs", input)
	return output.Logs, output.Meta, err
}

func (backend *GRPCQueryBackend) Traces(ctx context.Context, input QueryRequest) ([]TraceSummary, QueryMeta, error) {
	output, err := backend.call(ctx, "traces", input)
	return output.Traces, output.Meta, err
}

func (backend *GRPCQueryBackend) Overview(ctx context.Context, input QueryRequest) (Overview, error) {
	output, err := backend.call(ctx, "overview", input)
	if output.Overview == nil && err == nil {
		err = ErrQueryBackend
	}
	if output.Overview == nil {
		return Overview{}, err
	}
	return *output.Overview, err
}

func scopeHash(enterpriseID uuid.UUID, resourceIDs []uuid.UUID, authorizationVersion int64, signal string) string {
	values := make([]string, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		values = append(values, id.String())
	}
	slices.Sort(values)
	material := enterpriseID.String() + "\n" + strings.Join(values, "\n") + "\n" + signal + "\n" + fmt.Sprintf("%d", authorizationVersion)
	hash := sha256.Sum256([]byte(material))
	return hex.EncodeToString(hash[:])
}

func grpcTarget(endpoint string) (string, error) {
	if !strings.Contains(endpoint, "://") {
		if endpoint == "" {
			return "", errors.New("empty telemetry query endpoint")
		}
		return endpoint, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "grpcs" || parsed.Host == "" || parsed.Path != "" {
		return "", errors.New("telemetry query endpoint must be grpcs://host:port")
	}
	return parsed.Host, nil
}

var _ QueryBackend = (*GRPCQueryBackend)(nil)
