package telemetry

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

func OpenClickHouse(address, database, username, password string) (driver.Conn, error) {
	if address == "" || username == "" || password == "" {
		return nil, ErrUnavailable
	}
	return clickhouse.Open(&clickhouse.Options{Addr: []string{address}, Auth: clickhouse.Auth{Database: database, Username: username, Password: password},
		DialTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, MaxOpenConns: 20, MaxIdleConns: 5})
}

type ClickHouseQuery struct{ Conn driver.Conn }

func boundedQueryContext(ctx context.Context, request QueryRequest, scannedBytes *uint64) context.Context {
	seconds := float64(request.TimeoutMS) / 1000
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_bytes_to_read":    request.MaxScanBytes,
		"max_execution_time":   seconds,
		"max_result_rows":      request.Limit,
		"result_overflow_mode": "throw",
	}), clickhouse.WithProgress(func(progress *clickhouse.Progress) {
		if progress != nil && scannedBytes != nil {
			*scannedBytes += progress.Bytes
		}
	}))
}

func (query ClickHouseQuery) Metrics(ctx context.Context, request QueryRequest) ([]MetricSeries, QueryMeta, error) {
	aggregation, _ := request.Filter["aggregation"].(string)
	aggregators := map[string]string{"avg": "avg", "min": "min", "max": "max", "sum": "sum", "count": "count", "p50": "quantile(0.50)", "p95": "quantile(0.95)", "p99": "quantile(0.99)"}
	function, ok := aggregators[aggregation]
	if !ok {
		return nil, QueryMeta{}, ErrQueryInvalid
	}
	metric, _ := request.Filter["metric_name"].(string)
	step := int64(60)
	if value, ok := request.Filter["step_seconds"].(int); ok && value >= 10 {
		step = int64(value)
	}
	statement := fmt.Sprintf(`SELECT resource_id, metric_name, any(unit), toStartOfInterval(timestamp, toIntervalSecond(?)) AS bucket, %s(value)
		FROM argus_telemetry.metrics FINAL WHERE enterprise_id = ? AND resource_id IN (?) AND timestamp >= ? AND timestamp < ? AND metric_name = ?
		GROUP BY resource_id, metric_name, bucket ORDER BY resource_id, metric_name, bucket LIMIT ?`, function)
	started := time.Now()
	var scannedBytes uint64
	rows, err := query.Conn.Query(boundedQueryContext(ctx, request, &scannedBytes), statement, step, request.EnterpriseID, request.ResourceIDs, request.From, request.To, metric, request.Limit)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	defer rows.Close()
	byKey := map[string]int{}
	result := []MetricSeries{}
	for rows.Next() {
		var resourceID uuid.UUID
		var name, unit string
		var timestamp time.Time
		var value float64
		if err := rows.Scan(&resourceID, &name, &unit, &timestamp, &value); err != nil {
			return nil, QueryMeta{}, err
		}
		key := resourceID.String() + "/" + name
		index, exists := byKey[key]
		if !exists {
			index = len(result)
			byKey[key] = index
			result = append(result, MetricSeries{ResourceID: resourceID, MetricName: name, Unit: unit})
		}
		result[index].Points = append(result[index].Points, MetricPoint{Timestamp: timestamp, Value: value})
	}
	return result, queryMeta(request, started, scannedBytes), rows.Err()
}

func (query ClickHouseQuery) Logs(ctx context.Context, request QueryRequest) ([]LogRecord, QueryMeta, error) {
	serviceName := stringFilter(request.Filter, "service_name")
	text := stringFilter(request.Filter, "text")
	severity := stringSliceFilter(request.Filter, "severity")
	started := time.Now()
	var scannedBytes uint64
	rows, err := query.Conn.Query(boundedQueryContext(ctx, request, &scannedBytes), `SELECT timestamp, resource_id, severity, service_name, body, trace_id
		FROM argus_telemetry.logs FINAL WHERE enterprise_id = ? AND resource_id IN (?) AND timestamp >= ? AND timestamp < ?
		AND (? = '' OR service_name = ?) AND (length(?) = 0 OR severity IN (?)) AND (? = '' OR positionCaseInsensitive(body, ?) > 0)
		ORDER BY timestamp DESC, ingest_key DESC LIMIT ?`, request.EnterpriseID, request.ResourceIDs, request.From, request.To,
		serviceName, serviceName, severity, severity, text, text, request.Limit)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	defer rows.Close()
	result := []LogRecord{}
	for rows.Next() {
		var item LogRecord
		if err := rows.Scan(&item.Timestamp, &item.ResourceID, &item.Severity, &item.ServiceName, &item.Body, &item.TraceID); err != nil {
			return nil, QueryMeta{}, err
		}
		if !request.Sensitive {
			item.Body = redactTelemetryText(item.Body)
		}
		result = append(result, item)
	}
	return result, queryMeta(request, started, scannedBytes), rows.Err()
}

func (query ClickHouseQuery) Traces(ctx context.Context, request QueryRequest) ([]TraceSummary, QueryMeta, error) {
	serviceName := stringFilter(request.Filter, "service_name")
	operation := stringFilter(request.Filter, "operation")
	statusFilter := stringFilter(request.Filter, "status")
	minDuration := int64(0)
	if value, ok := request.Filter["min_duration_ms"].(int); ok {
		minDuration = int64(value)
	}
	started := time.Now()
	var scannedBytes uint64
	rows, err := query.Conn.Query(boundedQueryContext(ctx, request, &scannedBytes), `SELECT trace_id, any(resource_id), any(service_name), argMin(operation, start_time), min(start_time),
		max(duration_ns) / 1000000.0, count(), if(countIf(status = 'error') > 0, 'error', if(countIf(status = 'ok') > 0, 'ok', 'unset'))
		FROM argus_telemetry.traces FINAL WHERE enterprise_id = ? AND resource_id IN (?) AND start_time >= ? AND start_time < ?
		AND (? = '' OR service_name = ?) AND (? = '' OR operation = ?) AND (? = '' OR status = ?) AND duration_ns >= ? * 1000000
		GROUP BY trace_id ORDER BY min(start_time) DESC LIMIT ?`, request.EnterpriseID, request.ResourceIDs, request.From, request.To,
		serviceName, serviceName, operation, operation, statusFilter, statusFilter, minDuration, request.Limit)
	if err != nil {
		return nil, QueryMeta{}, err
	}
	defer rows.Close()
	result := []TraceSummary{}
	for rows.Next() {
		var item TraceSummary
		var spanCount uint64
		if err := rows.Scan(&item.TraceID, &item.ResourceID, &item.ServiceName, &item.RootSpanName, &item.StartedAt, &item.DurationMS, &spanCount, &item.Status); err != nil {
			return nil, QueryMeta{}, err
		}
		item.SpanCount, err = checkedIntFromUInt64(spanCount)
		if err != nil {
			return nil, QueryMeta{}, err
		}
		result = append(result, item)
	}
	return result, queryMeta(request, started, scannedBytes), rows.Err()
}

func (query ClickHouseQuery) Overview(ctx context.Context, request QueryRequest) (Overview, error) {
	result := Overview{ResourceCount: len(request.ResourceIDs), WindowSeconds: int(request.To.Sub(request.From).Seconds())}
	var metricPoints, logRecords, spans uint64
	if err := query.Conn.QueryRow(boundedQueryContext(ctx, request, nil), `SELECT
		(SELECT count() FROM argus_telemetry.metrics FINAL WHERE enterprise_id = ? AND resource_id IN (?) AND timestamp >= ? AND timestamp < ?),
		(SELECT count() FROM argus_telemetry.logs FINAL WHERE enterprise_id = ? AND resource_id IN (?) AND timestamp >= ? AND timestamp < ?),
		(SELECT count() FROM argus_telemetry.traces FINAL WHERE enterprise_id = ? AND resource_id IN (?) AND start_time >= ? AND start_time < ?)`,
		request.EnterpriseID, request.ResourceIDs, request.From, request.To, request.EnterpriseID, request.ResourceIDs, request.From, request.To,
		request.EnterpriseID, request.ResourceIDs, request.From, request.To).Scan(&metricPoints, &logRecords, &spans); err != nil {
		return Overview{}, err
	}
	var err error
	if result.MetricPoints, err = checkedInt64FromUInt64(metricPoints); err != nil {
		return Overview{}, err
	}
	if result.LogRecords, err = checkedInt64FromUInt64(logRecords); err != nil {
		return Overview{}, err
	}
	if result.Spans, err = checkedInt64FromUInt64(spans); err != nil {
		return Overview{}, err
	}
	return result, nil
}

func checkedIntFromUInt64(value uint64) (int, error) {
	if value > uint64(math.MaxInt) {
		return 0, fmt.Errorf("telemetry count %d exceeds int range", value)
	}
	return int(value), nil
}

func checkedInt64FromUInt64(value uint64) (int64, error) {
	if value > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("telemetry count %d exceeds int64 range", value)
	}
	return int64(value), nil
}

func queryMeta(request QueryRequest, started time.Time, scannedBytes uint64) QueryMeta {
	if scannedBytes > math.MaxInt64 {
		scannedBytes = math.MaxInt64
	}
	return QueryMeta{SchemaVersion: "argus.telemetry_result/v1", PartialReasons: []string{}, AppliedResourceCount: len(request.ResourceIDs),
		ElapsedMS: time.Since(started).Milliseconds(), ScannedBytes: int64(scannedBytes)}
}

func stringFilter(filter map[string]any, key string) string {
	value, _ := filter[key].(string)
	return value
}
func stringSliceFilter(filter map[string]any, key string) []string {
	if values, ok := filter[key].([]string); ok {
		return values
	}
	if values, ok := filter[key].(*[]string); ok && values != nil {
		return *values
	}
	return []string{}
}
func redactTelemetryText(value string) string {
	for _, marker := range []string{"password=", "token=", "secret=", "authorization:", "api_key="} {
		if strings.Contains(strings.ToLower(value), marker) {
			return "[redacted by telemetry field policy]"
		}
	}
	return value
}

type queryWireResponse struct {
	Metrics  []MetricSeries `json:"metrics,omitempty"`
	Logs     []LogRecord    `json:"logs,omitempty"`
	Traces   []TraceSummary `json:"traces,omitempty"`
	Meta     QueryMeta      `json:"meta,omitempty"`
	Overview *Overview      `json:"overview,omitempty"`
}

var _ QueryBackend = ClickHouseQuery{}
