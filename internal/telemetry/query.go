package telemetry

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultMaxRows      = 50_000
	HardMaxRows         = 100_000
	DefaultMaxSamples   = 5_000_000
	HardMaxSamples      = 10_000_000
	DefaultMaxSeries    = 100_000
	HardMaxSeries       = 1_000_000
	DefaultMaxScanBytes = int64(256 << 20)
	HardMaxScanBytes    = int64(1 << 30)
	HardMaxResultBytes  = int64(32 << 20)
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
	PlanHash             string   `json:"plan_hash,omitempty"`
	NextCursor           string   `json:"next_cursor,omitempty"`
}

type LogRecord struct {
	Timestamp   time.Time `json:"timestamp"`
	ResourceID  uuid.UUID `json:"resource_id"`
	Severity    string    `json:"severity"`
	Body        string    `json:"body"`
	ServiceName string    `json:"service_name"`
	TraceID     string    `json:"trace_id"`
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

type OverviewRequest struct {
	EnterpriseID         uuid.UUID
	ResourceIDs          []uuid.UUID
	AuthorizationVersion int64
	From                 time.Time
	To                   time.Time
	MaxScanBytes         int64
	Timeout              time.Duration
}

type OverviewBackend interface {
	Overview(context.Context, OverviewRequest) (Overview, error)
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

func (service Service) QueryOverview(ctx context.Context, actor Actor, ids []uuid.UUID, lookback int) (Overview, error) {
	if service.Query == nil || lookback < 300 || lookback > 7*24*60*60 {
		return Overview{}, ErrQueryInvalid
	}
	policy, err := service.Store.Queries.EnsureTelemetryRetentionPolicy(ctx, actor.EnterpriseID)
	if err != nil {
		return Overview{}, err
	}
	now := time.Now().UTC()
	resources, partial, err := service.AuthorizedResources(ctx, actor, ids)
	if err != nil {
		return Overview{}, err
	}
	request := OverviewRequest{EnterpriseID: actor.EnterpriseID, ResourceIDs: resources, AuthorizationVersion: actor.AuthorizationVersion,
		From: now.Add(-time.Duration(lookback) * time.Second), To: now, MaxScanBytes: policy.MaxScanBytes, Timeout: time.Duration(policy.MaxExecutionMs) * time.Millisecond}
	result, err := service.Query.Overview(ctx, request)
	if err != nil {
		return Overview{}, err
	}
	result.Partial = result.Partial || partial
	return result, nil
}

var ErrQueryBackend = errors.New("telemetry query backend unavailable")

func redactTelemetryText(value string) string {
	for _, marker := range []string{"password=", "token=", "secret=", "authorization:", "api_key="} {
		if strings.Contains(strings.ToLower(value), marker) {
			return "[redacted by telemetry field policy]"
		}
	}
	return value
}
