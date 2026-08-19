package telemetry

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultMaxRows      = 50_000
	HardMaxRows         = 100_000
	DefaultMaxScanBytes = int64(256 << 20)
	HardMaxScanBytes    = int64(1 << 30)
	DefaultTimeout      = 10 * time.Second
	HardTimeout         = 30 * time.Second
)

type QueryMeta struct {
	SchemaVersion        string   `json:"schema_version"`
	Partial              bool     `json:"partial"`
	PartialReasons       []string `json:"partial_reasons"`
	AppliedResourceCount int      `json:"applied_resource_count"`
	ScannedBytes         int64    `json:"scanned_bytes"`
	ElapsedMS            int64    `json:"elapsed_ms"`
	NextCursor           string   `json:"next_cursor,omitempty"`
}

type QueryRequest struct {
	EnterpriseID         uuid.UUID      `json:"enterprise_id"`
	ResourceIDs          []uuid.UUID    `json:"resource_ids"`
	AuthorizationVersion int64          `json:"authorization_version"`
	Signal               string         `json:"signal"`
	From                 time.Time      `json:"from"`
	To                   time.Time      `json:"to"`
	Limit                int            `json:"limit"`
	Cursor               string         `json:"cursor,omitempty"`
	Filter               map[string]any `json:"filter"`
	Sensitive            bool           `json:"sensitive"`
	MaxScanBytes         int64          `json:"max_scan_bytes"`
	TimeoutMS            int            `json:"timeout_ms"`
}

type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}
type MetricSeries struct {
	ResourceID uuid.UUID     `json:"resource_id"`
	MetricName string        `json:"metric_name"`
	Unit       string        `json:"unit"`
	Points     []MetricPoint `json:"points"`
}
type LogRecord struct {
	Timestamp   time.Time `json:"timestamp"`
	ResourceID  uuid.UUID `json:"resource_id"`
	Severity    string    `json:"severity"`
	Body        string    `json:"body"`
	ServiceName string    `json:"service_name"`
	TraceID     string    `json:"trace_id"`
}
type TraceSummary struct {
	TraceID      string    `json:"trace_id"`
	ResourceID   uuid.UUID `json:"resource_id"`
	ServiceName  string    `json:"service_name"`
	RootSpanName string    `json:"root_span_name"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"started_at"`
	DurationMS   float64   `json:"duration_ms"`
	SpanCount    int       `json:"span_count"`
}
type Overview struct {
	ResourceCount      int   `json:"resource_count"`
	HealthyCollectors  int   `json:"healthy_collectors"`
	DegradedCollectors int   `json:"degraded_collectors"`
	MetricPoints       int64 `json:"metric_points"`
	LogRecords         int64 `json:"log_records"`
	Spans              int64 `json:"spans"`
	WindowSeconds      int   `json:"window_seconds"`
	Partial            bool  `json:"partial"`
}

type QueryBackend interface {
	Metrics(context.Context, QueryRequest) ([]MetricSeries, QueryMeta, error)
	Logs(context.Context, QueryRequest) ([]LogRecord, QueryMeta, error)
	Traces(context.Context, QueryRequest) ([]TraceSummary, QueryMeta, error)
	Overview(context.Context, QueryRequest) (Overview, error)
}

func (service Service) AuthorizedResources(ctx context.Context, actor Actor, requested []uuid.UUID) ([]uuid.UUID, bool, error) {
	if len(requested) == 0 || len(requested) > 1000 {
		return nil, false, ErrQueryInvalid
	}
	seen := make(map[uuid.UUID]struct{}, len(requested))
	allowed := make([]uuid.UUID, 0, len(requested))
	for _, id := range requested {
		if id == uuid.Nil {
			return nil, false, ErrQueryInvalid
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		if service.requireResource(ctx, actor, "host", id) == nil || service.requireResource(ctx, actor, "kubernetes_cluster", id) == nil {
			allowed = append(allowed, id)
		}
	}
	if len(allowed) == 0 {
		return nil, len(seen) > 0, ErrDenied
	}
	return allowed, len(allowed) != len(seen), nil
}

func (service Service) queryRequest(ctx context.Context, actor Actor, ids []uuid.UUID, from, to time.Time, limit int, cursor string, filter map[string]any, sensitive bool) (QueryRequest, bool, error) {
	if service.Query == nil || from.IsZero() || to.IsZero() || !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		return QueryRequest{}, false, ErrQueryInvalid
	}
	policy, err := service.Store.Queries.EnsureTelemetryRetentionPolicy(ctx, actor.EnterpriseID)
	if err != nil {
		return QueryRequest{}, false, err
	}
	if limit <= 0 {
		limit = int(policy.MaxRows)
	}
	if limit > HardMaxRows || policy.MaxScanBytes > HardMaxScanBytes || policy.MaxExecutionMs > int32(HardTimeout/time.Millisecond) {
		return QueryRequest{}, false, ErrQueryBudget
	}
	if limit > int(policy.MaxRows) {
		limit = int(policy.MaxRows)
	}
	resources, partial, err := service.AuthorizedResources(ctx, actor, ids)
	if err != nil {
		return QueryRequest{}, false, err
	}
	return QueryRequest{EnterpriseID: actor.EnterpriseID, ResourceIDs: resources, AuthorizationVersion: actor.AuthorizationVersion,
		From: from.UTC(), To: to.UTC(), Limit: limit, Cursor: cursor,
		Filter: filter, Sensitive: sensitive, MaxScanBytes: policy.MaxScanBytes, TimeoutMS: int(policy.MaxExecutionMs)}, partial, nil
}

func mergePartial(meta QueryMeta, partial bool) QueryMeta {
	if meta.SchemaVersion == "" {
		meta.SchemaVersion = "argus.telemetry_result/v1"
	}
	if partial {
		meta.Partial = true
		if !slices.Contains(meta.PartialReasons, "unauthorized_resources") {
			meta.PartialReasons = append(meta.PartialReasons, "unauthorized_resources")
		}
	}
	return meta
}

func validateBackendMeta(meta QueryMeta, request QueryRequest) error {
	if meta.ScannedBytes < 0 || meta.ElapsedMS < 0 || meta.ScannedBytes > request.MaxScanBytes || meta.ElapsedMS > int64(request.TimeoutMS) {
		return ErrQueryBudget
	}
	return nil
}

func (service Service) QueryMetrics(ctx context.Context, actor Actor, ids []uuid.UUID, from, to time.Time, limit int, cursor, metric, aggregation string, step int, sensitive bool) ([]MetricSeries, QueryMeta, error) {
	request, partial, err := service.queryRequest(ctx, actor, ids, from, to, limit, cursor,
		map[string]any{"metric_name": metric, "aggregation": aggregation, "step_seconds": step}, sensitive)
	if err = validateMetricQueryInput(err, metric); err != nil {
		return nil, QueryMeta{}, err
	}
	request.Signal = "metrics"
	rows, meta, err := service.Query.Metrics(ctx, request)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	if err := validateBackendMeta(meta, request); err != nil {
		return nil, QueryMeta{}, err
	}
	return rows, mergePartial(meta, partial), nil
}

func validateMetricQueryInput(err error, metric string) error {
	if err != nil {
		return err
	}
	if metric == "" {
		return ErrQueryInvalid
	}
	return nil
}

func (service Service) QueryLogs(ctx context.Context, actor Actor, ids []uuid.UUID, from, to time.Time, limit int, cursor string, filter map[string]any, sensitive bool) ([]LogRecord, QueryMeta, error) {
	request, partial, err := service.queryRequest(ctx, actor, ids, from, to, limit, cursor, filter, sensitive)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	request.Signal = "logs"
	rows, meta, err := service.Query.Logs(ctx, request)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	if err := validateBackendMeta(meta, request); err != nil {
		return nil, QueryMeta{}, err
	}
	return rows, mergePartial(meta, partial), nil
}

func (service Service) QueryTraces(ctx context.Context, actor Actor, ids []uuid.UUID, from, to time.Time, limit int, cursor string, filter map[string]any, sensitive bool) ([]TraceSummary, QueryMeta, error) {
	request, partial, err := service.queryRequest(ctx, actor, ids, from, to, limit, cursor, filter, sensitive)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	request.Signal = "traces"
	rows, meta, err := service.Query.Traces(ctx, request)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	if err := validateBackendMeta(meta, request); err != nil {
		return nil, QueryMeta{}, err
	}
	return rows, mergePartial(meta, partial), nil
}

func (service Service) QueryOverview(ctx context.Context, actor Actor, ids []uuid.UUID, lookback int) (Overview, error) {
	if lookback < 300 || lookback > 7*24*60*60 {
		return Overview{}, ErrQueryInvalid
	}
	now := time.Now().UTC()
	request, partial, err := service.queryRequest(ctx, actor, ids, now.Add(-time.Duration(lookback)*time.Second), now, DefaultMaxRows, "", map[string]any{"lookback_seconds": lookback}, false)
	if err != nil {
		return Overview{}, err
	}
	request.Signal = "overview"
	result, err := service.Query.Overview(ctx, request)
	if err != nil {
		return Overview{}, err
	}
	result.Partial = result.Partial || partial
	return result, nil
}

var ErrQueryBackend = errors.New("telemetry query backend unavailable")
