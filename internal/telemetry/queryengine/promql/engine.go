package promqlengine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"

	"github.com/kakj-go/Argus/internal/telemetry/queryengine/chstats"
)

type Request struct {
	Expression   string
	Instant      bool
	Start        time.Time
	End          time.Time
	Step         time.Duration
	Scope        Scope
	MaxSamples   int
	MaxSeries    int
	MaxScanBytes int64
	Timeout      time.Duration
	Lookback     time.Duration
}

type Result struct {
	Value        parser.Value
	Warnings     []string
	Elapsed      time.Duration
	Stats        any
	ScannedRows  int64
	ScannedBytes int64
}

type Engine struct {
	conn   driver.Conn
	router TableRouter
	logger *slog.Logger
}

func NewEngine(conn driver.Conn, router TableRouter, logger *slog.Logger) *Engine {
	return &Engine{conn: conn, router: router, logger: logger}
}

func (e *Engine) Execute(ctx context.Context, request Request) (Result, error) {
	if e == nil || e.conn == nil || e.router == nil || request.Expression == "" || request.Scope.EnterpriseID == uuid.Nil {
		return Result{}, fmt.Errorf("promql engine unavailable")
	}
	if request.Start.IsZero() || request.End.IsZero() || !request.Start.Before(request.End) {
		return Result{}, fmt.Errorf("invalid promql time range")
	}
	if !request.Instant && request.Step <= 0 {
		request.Step = time.Minute
	}
	if request.Timeout <= 0 {
		request.Timeout = 10 * time.Second
	}
	if request.MaxSamples <= 0 {
		request.MaxSamples = 5_000_000
	}
	lookback := request.Lookback
	if lookback <= 0 {
		lookback = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	if request.MaxSeries <= 0 {
		request.MaxSeries = 100_000
	}
	progress := &chstats.Tracker{}
	queryable := Queryable{Conn: e.conn, Router: e.router, Scope: request.Scope, MaxSeries: request.MaxSeries, MaxSamples: request.MaxSamples, MaxScanBytes: request.MaxScanBytes, Timeout: request.Timeout, Progress: progress}
	engine := promql.NewEngine(promql.EngineOpts{
		Logger: e.logger, MaxSamples: request.MaxSamples, Timeout: request.Timeout,
		LookbackDelta: lookback, EnableAtModifier: true, EnableNegativeOffset: true,
		Parser: parser.NewParser(parser.Options{}),
	})
	started := time.Now()
	var query promql.Query
	var err error
	queryOpts := promql.NewPrometheusQueryOpts(false, lookback)
	if request.Instant {
		query, err = engine.NewInstantQuery(ctx, queryable, queryOpts, request.Expression, request.End)
	} else {
		query, err = engine.NewRangeQuery(ctx, queryable, queryOpts, request.Expression, request.Start, request.End, request.Step)
	}
	if err != nil {
		return Result{}, err
	}
	defer query.Close()
	result := query.Exec(ctx)
	if result == nil {
		return Result{}, fmt.Errorf("promql engine returned nil result")
	}
	if result.Err != nil {
		return Result{}, result.Err
	}
	warnings := make([]string, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		warnings = append(warnings, warning.Error())
	}
	// Query.Close returns matrix point slices to Prometheus' global pools. Copy
	// the value before the deferred Close runs so a later query cannot mutate an
	// already returned result through pooled backing arrays.
	value := clonePromQLValue(result.Value)
	return Result{Value: value, Warnings: warnings, Elapsed: time.Since(started), Stats: query.Stats(), ScannedRows: progress.Rows(), ScannedBytes: progress.Bytes()}, nil
}

func clonePromQLValue(value parser.Value) parser.Value {
	switch typed := value.(type) {
	case promql.Matrix:
		result := make(promql.Matrix, len(typed))
		for index, item := range typed {
			result[index] = promql.Series{Metric: item.Metric, DropName: item.DropName}
			result[index].Floats = append([]promql.FPoint(nil), item.Floats...)
			result[index].Histograms = make([]promql.HPoint, len(item.Histograms))
			for histogramIndex, point := range item.Histograms {
				result[index].Histograms[histogramIndex] = promql.HPoint{T: point.T, H: point.H.Copy()}
			}
		}
		return result
	case promql.Vector:
		result := make(promql.Vector, len(typed))
		for index, sample := range typed {
			result[index] = sample
			if sample.H != nil {
				result[index].H = sample.H.Copy()
			}
		}
		return result
	case promql.Scalar:
		return typed
	case promql.String:
		return typed
	default:
		return value
	}
}

var _ storage.Queryable = Queryable{}
