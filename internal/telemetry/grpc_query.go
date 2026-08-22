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
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/common/v1"
	telemetryv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/telemetry/v1"
	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
)

const overviewSchemaVersion = "argus.telemetry_overview/v1"

type QueryRPCServer struct {
	telemetryv1.UnimplementedTelemetryQueryServiceServer
	Backend     OverviewBackend
	Logger      *slog.Logger
	Limiter     QueryConcurrencyLimiter
	Coordinator *queryengine.Coordinator
	Readiness   TenantReadiness
	Lifecycle   TenantSchemaController
}

type TenantReadiness interface {
	TelemetryTenantReady(context.Context, uuid.UUID) (bool, error)
}

type TenantSchemaController interface {
	EnsureTenantSchema(context.Context, uuid.UUID) error
	DropTenantSchema(context.Context, uuid.UUID) error
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

func RegisterQueryRPC(server *grpc.Server, backend OverviewBackend, limiter QueryConcurrencyLimiter, logger *slog.Logger, coordinator []*queryengine.Coordinator, readiness TenantReadiness, lifecycle ...TenantSchemaController) {
	var engine *queryengine.Coordinator
	if len(coordinator) > 0 {
		engine = coordinator[0]
	}
	var tenantLifecycle TenantSchemaController
	if len(lifecycle) > 0 {
		tenantLifecycle = lifecycle[0]
	}
	telemetryv1.RegisterTelemetryQueryServiceServer(server, &QueryRPCServer{Backend: backend, Limiter: limiter, Logger: logger, Coordinator: engine, Readiness: readiness, Lifecycle: tenantLifecycle})
}

func (server *QueryRPCServer) EnsureTenantSchema(ctx context.Context, request *telemetryv1.EnsureTenantSchemaRequest) (*telemetryv1.EnsureTenantSchemaResponse, error) {
	enterpriseID, err := validateTenantSchemaRPC(ctx, request.GetEnterpriseId(), request.GetSchemaVersion(), server.Lifecycle)
	if err != nil {
		return nil, err
	}
	if err := server.Lifecycle.EnsureTenantSchema(ctx, enterpriseID); err != nil {
		return nil, status.Error(codes.Unavailable, "telemetry tenant schema initialization failed")
	}
	return &telemetryv1.EnsureTenantSchemaResponse{SchemaVersion: uint32(TelemetrySchemaVersion), Status: "ready"}, nil
}

func (server *QueryRPCServer) DropTenantSchema(ctx context.Context, request *telemetryv1.DropTenantSchemaRequest) (*telemetryv1.DropTenantSchemaResponse, error) {
	enterpriseID, err := validateTenantSchemaRPC(ctx, request.GetEnterpriseId(), request.GetSchemaVersion(), server.Lifecycle)
	if err != nil {
		return nil, err
	}
	if err := server.Lifecycle.DropTenantSchema(ctx, enterpriseID); err != nil {
		return nil, status.Error(codes.Unavailable, "telemetry tenant schema deletion failed")
	}
	return &telemetryv1.DropTenantSchemaResponse{SchemaVersion: uint32(TelemetrySchemaVersion), Status: "deleting"}, nil
}

func validateTenantSchemaRPC(ctx context.Context, enterpriseID string, schemaVersion uint32, lifecycle TenantSchemaController) (uuid.UUID, error) {
	if lifecycle == nil || !trustedRPCPeer(ctx) {
		return uuid.Nil, status.Error(codes.Unauthenticated, "telemetry tenant schema mTLS identity required")
	}
	id, err := uuid.Parse(enterpriseID)
	if err != nil || id == uuid.Nil || schemaVersion != uint32(TelemetrySchemaVersion) {
		return uuid.Nil, status.Error(codes.InvalidArgument, "telemetry tenant schema request rejected")
	}
	return id, nil
}

func (server *QueryRPCServer) ExecuteQueryV2(ctx context.Context, request *telemetryv1.ExecuteQueryV2Request) (*telemetryv1.ExecuteQueryV2Response, error) {
	if server.Coordinator == nil || server.Limiter == nil || !trustedRPCPeer(ctx) {
		return nil, status.Error(codes.Unauthenticated, "telemetry query engine unavailable")
	}
	if request == nil || request.Scope == nil || request.Budget == nil || request.SchemaVersion != "argus.telemetry_query/v3" {
		return nil, status.Error(codes.InvalidArgument, "telemetry query request rejected")
	}
	budget, err := queryBudgetFromProto(request.Budget)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, "QUERY_BUDGET_EXCEEDED")
	}
	signal, err := querySignal(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "query variant is required")
	}
	enterpriseID, resources, err := scopeFromProto(request.Scope, signal)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "telemetry query scope rejected")
	}
	if server.Readiness != nil {
		ready, readinessErr := server.Readiness.TelemetryTenantReady(ctx, enterpriseID)
		if readinessErr != nil {
			return nil, status.Error(codes.Unavailable, "telemetry tenant readiness unavailable")
		}
		if !ready {
			return nil, status.Error(codes.Unavailable, "telemetry tenant is not ready")
		}
	}
	input := queryengine.Request{Scope: queryengine.Scope{EnterpriseID: enterpriseID, ResourceIDs: resources, AuthorizationVersion: int64(request.Scope.AuthorizationVersion), SensitiveFields: request.Scope.SensitiveFields}, Budget: budget}
	language := queryengine.LanguagePromQL
	if value := request.GetPromql(); value != nil {
		input.Expression = value.Expression
		if value.Start != nil {
			input.Start = value.Start.AsTime()
		}
		if value.End != nil {
			input.End = value.End.AsTime()
		}
		input.Step = time.Duration(value.StepSeconds) * time.Second
		input.Instant = value.Instant
	} else if value := request.GetKql(); value != nil {
		language, input.Expression, input.Pipeline = queryengine.LanguageKQL, value.Expression, value.Pipeline
		if value.Start != nil {
			input.Start = value.Start.AsTime()
		}
		if value.End != nil {
			input.End = value.End.AsTime()
		}
	} else if value := request.GetTraceGraphql(); value != nil {
		language, input.Expression, input.Operation = queryengine.LanguageTrace, value.Document, value.OperationName
		if value.Variables != nil {
			input.Variables = value.Variables.AsMap()
		}
		if value.Start != nil {
			input.Start = value.Start.AsTime()
		}
		if value.End != nil {
			input.End = value.End.AsTime()
		}
	} else {
		return nil, status.Error(codes.InvalidArgument, "query variant is required")
	}
	input.Language = language
	if input.Start.IsZero() {
		input.Start = time.Now().UTC().Add(-time.Hour)
	}
	if input.End.IsZero() {
		input.End = time.Now().UTC()
	}
	release, err := server.Limiter.Acquire(ctx, enterpriseID, request.Scope.ScopeHash, input.Budget.Timeout+5*time.Second)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, "telemetry query concurrency unavailable")
	}
	defer release()
	queryCtx, cancel := context.WithTimeout(ctx, input.Budget.Timeout)
	defer cancel()
	result, err := server.Coordinator.Execute(queryCtx, input)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, queryErrorCode(err))
	}
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		return nil, status.Error(codes.Internal, "telemetry query encoding failed")
	}
	return &telemetryv1.ExecuteQueryV2Response{SchemaVersion: "argus.telemetry_result/v3", Language: string(result.Language), ResultType: result.ResultType, ResultJson: encoded, Warnings: result.Meta.Warnings, Partial: result.Meta.Partial, Meta: &telemetryv1.QueryMeta{PlanHash: result.Meta.PlanHash, Engine: result.Meta.Engine, EngineVersion: result.Meta.EngineVersion, ScannedBytes: uint64(max(result.Meta.ScannedBytes, 0)), ScannedRows: uint64(max(result.Meta.ScannedRows, 0)), ReturnedRows: uint64(max(result.Meta.ReturnedRows, 0)), LoadedSamples: uint64(max(result.Meta.LoadedSamples, 0)), ElapsedMillis: uint64(max(result.Meta.ElapsedMillis, 0)), Partial: result.Meta.Partial}}, nil
}

func queryBudgetFromProto(value *telemetryv1.QueryBudget) (queryengine.Budget, error) {
	if value == nil {
		return queryengine.Budget{}, ErrQueryInvalid
	}
	budget := queryengine.Budget{
		MaxScanBytes:   DefaultMaxScanBytes,
		MaxRows:        DefaultMaxRows,
		MaxSamples:     DefaultMaxSamples,
		MaxSeries:      DefaultMaxSeries,
		MaxResultBytes: 8 << 20,
		Timeout:        DefaultTimeout,
	}
	if value.MaxScanBytes > 0 {
		budget.MaxScanBytes = int64(value.MaxScanBytes)
	}
	if value.MaxRows > 0 {
		budget.MaxRows = int(value.MaxRows)
	}
	if value.MaxSamples > 0 {
		budget.MaxSamples = int(value.MaxSamples)
	}
	if value.MaxSeries > 0 {
		budget.MaxSeries = int(value.MaxSeries)
	}
	if value.MaxResultBytes > 0 {
		budget.MaxResultBytes = int64(value.MaxResultBytes)
	}
	if value.TimeoutMillis > 0 {
		budget.Timeout = time.Duration(value.TimeoutMillis) * time.Millisecond
	}
	if budget.MaxRows > HardMaxRows || budget.MaxSamples > HardMaxSamples || budget.MaxSeries > HardMaxSeries ||
		budget.MaxScanBytes > HardMaxScanBytes || budget.MaxResultBytes > HardMaxResultBytes || budget.Timeout > HardTimeout {
		return queryengine.Budget{}, ErrQueryBudget
	}
	return budget, nil
}

func querySignal(request *telemetryv1.ExecuteQueryV2Request) (string, error) {
	switch {
	case request == nil:
		return "", ErrQueryInvalid
	case request.GetPromql() != nil:
		return "metrics", nil
	case request.GetKql() != nil:
		return "logs", nil
	case request.GetTraceGraphql() != nil:
		return "traces", nil
	default:
		return "", ErrQueryInvalid
	}
}

func (server *QueryRPCServer) QueryOverview(ctx context.Context, request *telemetryv1.QueryOverviewRequest) (*telemetryv1.QueryOverviewResponse, error) {
	if server.Backend == nil || server.Limiter == nil || !trustedRPCPeer(ctx) {
		return nil, status.Error(codes.Unauthenticated, "telemetry query mTLS identity required")
	}
	if request == nil || request.Scope == nil || request.From == nil || request.To == nil || request.SchemaVersion != overviewSchemaVersion ||
		!request.From.IsValid() || !request.To.IsValid() || request.MaxScanBytes < 1 || request.MaxScanBytes > uint64(HardMaxScanBytes) ||
		request.TimeoutMillis < 1 || request.TimeoutMillis > uint32(HardTimeout/time.Millisecond) {
		return nil, status.Error(codes.InvalidArgument, "telemetry overview rejected")
	}
	enterpriseID, resources, err := scopeFromProto(request.Scope, "overview")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "telemetry overview scope rejected")
	}
	from, to := request.From.AsTime(), request.To.AsTime()
	if !from.Before(to) || to.Sub(from) > 7*24*time.Hour {
		return nil, status.Error(codes.InvalidArgument, "telemetry overview window rejected")
	}
	timeout := time.Duration(request.TimeoutMillis) * time.Millisecond
	release, err := server.Limiter.Acquire(ctx, enterpriseID, request.Scope.ScopeHash, timeout+5*time.Second)
	if err != nil {
		return nil, status.Error(codes.ResourceExhausted, "telemetry query concurrency unavailable")
	}
	defer release()
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := server.Backend.Overview(queryCtx, OverviewRequest{EnterpriseID: enterpriseID, ResourceIDs: resources,
		AuthorizationVersion: int64(request.Scope.AuthorizationVersion), From: from, To: to, MaxScanBytes: int64(request.MaxScanBytes), Timeout: timeout})
	if err != nil {
		if server.Logger != nil {
			server.Logger.Warn("telemetry overview backend failed", "error", err)
		}
		return nil, status.Error(codes.Unavailable, "telemetry query unavailable")
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return nil, status.Error(codes.Internal, "telemetry query encoding failed")
	}
	return &telemetryv1.QueryOverviewResponse{Result: &telemetryv1.TelemetryQueryResponse{
		SchemaVersion: "argus.telemetry_overview_result/v1",
		ResultJson:    encoded,
		Partial:       output.Partial,
	}}, nil
}

func trustedRPCPeer(ctx context.Context) bool {
	value, ok := peer.FromContext(ctx)
	if !ok {
		return false
	}
	info, ok := value.AuthInfo.(credentials.TLSInfo)
	return ok && len(info.State.PeerCertificates) == 1
}

func scopeFromProto(value *telemetryv1.TelemetryQueryScope, signal string) (uuid.UUID, []uuid.UUID, error) {
	if value == nil || (signal != "" && !slices.Contains(value.AllowedSignals, signal)) || len(value.AuthorizedResources) == 0 || len(value.AuthorizedResources) > 1000 {
		return uuid.Nil, nil, ErrQueryInvalid
	}
	enterpriseID, err := uuid.Parse(value.EnterpriseId)
	if err != nil || enterpriseID == uuid.Nil {
		return uuid.Nil, nil, ErrQueryInvalid
	}
	resourceIDs := make([]uuid.UUID, 0, len(value.AuthorizedResources))
	for _, resource := range value.AuthorizedResources {
		id, parseErr := uuid.Parse(resource.ResourceId)
		if parseErr != nil || id == uuid.Nil || (resource.ResourceType != "host" && resource.ResourceType != "kubernetes_cluster") {
			return uuid.Nil, nil, ErrQueryInvalid
		}
		resourceIDs = append(resourceIDs, id)
	}
	if value.ScopeHash != scopeHash(enterpriseID, resourceIDs, int64(value.AuthorizationVersion), signal, value.SensitiveFields) {
		return uuid.Nil, nil, ErrQueryInvalid
	}
	return enterpriseID, resourceIDs, nil
}

type GRPCQueryBackend struct {
	connection *grpc.ClientConn
	client     telemetryv1.TelemetryQueryServiceClient
	logger     *slog.Logger
}

func NewGRPCQueryBackend(endpoint string, tlsConfig *tls.Config, logger *slog.Logger) (*GRPCQueryBackend, error) {
	target, err := grpcTarget(endpoint)
	if err != nil || tlsConfig == nil {
		return nil, ErrQueryBackend
	}
	connection, err := grpc.NewClient(target, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, err
	}
	return &GRPCQueryBackend{connection: connection, client: telemetryv1.NewTelemetryQueryServiceClient(connection), logger: logger}, nil
}

func (backend *GRPCQueryBackend) Close() error {
	if backend == nil || backend.connection == nil {
		return nil
	}
	return backend.connection.Close()
}

func (backend *GRPCQueryBackend) EnsureTenantSchema(ctx context.Context, enterpriseID uuid.UUID) error {
	if backend == nil || backend.client == nil || enterpriseID == uuid.Nil {
		return ErrQueryBackend
	}
	response, err := backend.client.EnsureTenantSchema(ctx, &telemetryv1.EnsureTenantSchemaRequest{EnterpriseId: enterpriseID.String(), SchemaVersion: uint32(TelemetrySchemaVersion)})
	if err != nil {
		return err
	}
	if response.GetSchemaVersion() != uint32(TelemetrySchemaVersion) || response.GetStatus() != "ready" {
		return ErrQueryBackend
	}
	return nil
}

func (backend *GRPCQueryBackend) DropTenantSchema(ctx context.Context, enterpriseID uuid.UUID) error {
	if backend == nil || backend.client == nil || enterpriseID == uuid.Nil {
		return ErrQueryBackend
	}
	response, err := backend.client.DropTenantSchema(ctx, &telemetryv1.DropTenantSchemaRequest{EnterpriseId: enterpriseID.String(), SchemaVersion: uint32(TelemetrySchemaVersion)})
	if err != nil {
		return err
	}
	if response.GetSchemaVersion() != uint32(TelemetrySchemaVersion) || response.GetStatus() != "deleting" {
		return ErrQueryBackend
	}
	return nil
}

func (backend *GRPCQueryBackend) ExecuteEngineQuery(ctx context.Context, input queryengine.Request) (queryengine.Result, error) {
	if backend == nil || backend.client == nil || input.Scope.EnterpriseID == uuid.Nil {
		return queryengine.Result{}, ErrQueryBackend
	}
	signal := "metrics"
	if input.Language == queryengine.LanguageKQL {
		signal = "logs"
	} else if input.Language == queryengine.LanguageTrace {
		signal = "traces"
	}
	resources := make([]*commonv1.ResourceRef, 0, len(input.Scope.ResourceIDs))
	for _, id := range input.Scope.ResourceIDs {
		resources = append(resources, &commonv1.ResourceRef{ResourceType: "host", ResourceId: id.String()})
	}
	scope := &telemetryv1.TelemetryQueryScope{EnterpriseId: input.Scope.EnterpriseID.String(), AuthorizedResources: resources, AllowedSignals: []string{signal}, AuthorizationVersion: uint64(input.Scope.AuthorizationVersion), SensitiveFields: input.Scope.SensitiveFields, ScopeHash: scopeHash(input.Scope.EnterpriseID, input.Scope.ResourceIDs, input.Scope.AuthorizationVersion, signal, input.Scope.SensitiveFields)}
	budget := &telemetryv1.QueryBudget{MaxScanBytes: uint64(input.Budget.MaxScanBytes), MaxRows: uint32(input.Budget.MaxRows), MaxSamples: uint32(input.Budget.MaxSamples), MaxSeries: uint32(input.Budget.MaxSeries), MaxResultBytes: uint64(input.Budget.MaxResultBytes), TimeoutMillis: uint32(input.Budget.Timeout.Milliseconds())}
	request := &telemetryv1.ExecuteQueryV2Request{SchemaVersion: "argus.telemetry_query/v3", Scope: scope, Budget: budget}
	switch input.Language {
	case queryengine.LanguagePromQL:
		request.Query = &telemetryv1.ExecuteQueryV2Request_Promql{Promql: &telemetryv1.PromQLQuery{Expression: input.Expression, Start: timestamppb.New(input.Start), End: timestamppb.New(input.End), StepSeconds: uint64(input.Step.Seconds()), Instant: input.Instant}}
	case queryengine.LanguageKQL:
		request.Query = &telemetryv1.ExecuteQueryV2Request_Kql{Kql: &telemetryv1.KQLQuery{Expression: input.Expression, Pipeline: input.Pipeline, Start: timestamppb.New(input.Start), End: timestamppb.New(input.End)}}
	case queryengine.LanguageTrace:
		variables := &structpb.Struct{}
		if input.Variables != nil {
			var err error
			variables, err = structpb.NewStruct(input.Variables)
			if err != nil {
				return queryengine.Result{}, err
			}
		}
		request.Query = &telemetryv1.ExecuteQueryV2Request_TraceGraphql{TraceGraphql: &telemetryv1.TraceGraphQLQuery{Document: input.Expression, OperationName: input.Operation, Variables: variables, Start: timestamppb.New(input.Start), End: timestamppb.New(input.End)}}
	default:
		return queryengine.Result{}, queryengine.ErrUnsupportedLanguage
	}
	response, err := backend.client.ExecuteQueryV2(ctx, request)
	if err != nil {
		if backend.logger != nil {
			backend.logger.Warn("telemetry query v2 RPC failed", "code", status.Code(err), "error", status.Convert(err).Message())
		}
		return queryengine.Result{}, engineRPCError(err)
	}
	if response == nil || response.SchemaVersion != "argus.telemetry_result/v3" {
		return queryengine.Result{}, ErrQueryBackend
	}
	var data any
	if err := json.Unmarshal(response.ResultJson, &data); err != nil {
		return queryengine.Result{}, err
	}
	warnings := response.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	meta := queryengine.QueryMeta{Engine: response.Meta.GetEngine(), EngineVersion: response.Meta.GetEngineVersion(), PlanHash: response.Meta.GetPlanHash(), ScannedBytes: int64(response.Meta.GetScannedBytes()), ScannedRows: int64(response.Meta.GetScannedRows()), ReturnedRows: int64(response.Meta.GetReturnedRows()), LoadedSamples: int64(response.Meta.GetLoadedSamples()), ElapsedMillis: int64(response.Meta.GetElapsedMillis()), Partial: response.Partial, Warnings: warnings}
	return queryengine.Result{Language: queryengine.Language(response.Language), ResultType: response.ResultType, Data: data, Meta: meta}, nil
}

func engineRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Convert(err).Message() {
	case "QUERY_BUDGET_EXCEEDED":
		return queryengine.ErrBudget
	case "QUERY_FEATURE_UNSUPPORTED":
		return queryengine.ErrUnsupportedLanguage
	case "QUERY_INVALID":
		return ErrQueryInvalid
	default:
		return ErrQueryBackend
	}
}

func queryErrorCode(err error) string {
	switch {
	case errors.Is(err, queryengine.ErrUnsupportedLanguage):
		return "QUERY_FEATURE_UNSUPPORTED"
	case errors.Is(err, queryengine.ErrBudget), errors.Is(err, context.DeadlineExceeded):
		return "QUERY_BUDGET_EXCEEDED"
	default:
		return "QUERY_INVALID"
	}
}

func (backend *GRPCQueryBackend) Overview(ctx context.Context, input OverviewRequest) (Overview, error) {
	if backend == nil || backend.client == nil {
		return Overview{}, ErrQueryBackend
	}
	resources := make([]*commonv1.ResourceRef, 0, len(input.ResourceIDs))
	for _, id := range input.ResourceIDs {
		resources = append(resources, &commonv1.ResourceRef{ResourceType: "host", ResourceId: id.String()})
	}
	scope := &telemetryv1.TelemetryQueryScope{EnterpriseId: input.EnterpriseID.String(), AuthorizedResources: resources, AllowedSignals: []string{"overview"},
		AuthorizationVersion: uint64(input.AuthorizationVersion), ScopeHash: scopeHash(input.EnterpriseID, input.ResourceIDs, input.AuthorizationVersion, "overview", false)}
	response, err := backend.client.QueryOverview(ctx, &telemetryv1.QueryOverviewRequest{SchemaVersion: overviewSchemaVersion, Scope: scope,
		From: timestamppb.New(input.From), To: timestamppb.New(input.To), MaxScanBytes: uint64(input.MaxScanBytes), TimeoutMillis: uint32(input.Timeout.Milliseconds())})
	if err != nil || response == nil || response.Result == nil || response.Result.SchemaVersion != "argus.telemetry_overview_result/v1" {
		return Overview{}, ErrQueryBackend
	}
	var output Overview
	if json.Unmarshal(response.Result.ResultJson, &output) != nil {
		return Overview{}, ErrQueryBackend
	}
	return output, nil
}

func scopeHash(enterpriseID uuid.UUID, resourceIDs []uuid.UUID, authorizationVersion int64, signal string, sensitiveFields bool) string {
	values := make([]string, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		values = append(values, id.String())
	}
	slices.Sort(values)
	material := enterpriseID.String() + "\n" + strings.Join(values, "\n") + "\n" + signal + "\n" + fmt.Sprintf("%d", authorizationVersion) + "\n" + fmt.Sprintf("%t", sensitiveFields)
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

var _ OverviewBackend = (*GRPCQueryBackend)(nil)
