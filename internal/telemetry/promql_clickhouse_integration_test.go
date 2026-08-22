package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"
	"github.com/twmb/franz-go/pkg/kgo"
	collectmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	promqlengine "github.com/kakj-go/Argus/internal/telemetry/queryengine/promql"
)

func TestPromQLClickHouseMixedMetricTypes(t *testing.T) {
	address := os.Getenv("ARGUS_CLICKHOUSE_TEST_ADDRESS")
	if address == "" {
		t.Skip("ARGUS_CLICKHOUSE_TEST_ADDRESS is not set")
	}
	username := envOrDefault("ARGUS_CLICKHOUSE_TEST_USERNAME", "argus")
	password := envOrDefault("ARGUS_CLICKHOUSE_TEST_PASSWORD", "argus")
	database := envOrDefault("ARGUS_CLICKHOUSE_TEST_DATABASE", "argus_telemetry")

	conn, err := OpenClickHouse(address, database, username, password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	enterpriseID := uuid.New()
	resourceID := uuid.New()
	router := TenantTableRouter{}
	manager := ClickHouseTenantSchemaManager{Conn: conn, Router: router}
	if err := manager.EnsureTenant(ctx, enterpriseID); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyTenant(ctx, enterpriseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.DropTenant(context.Background(), enterpriseID) })

	now := time.Now().UTC().Truncate(time.Millisecond)
	request := mixedMetricTypesRequest(uint64(now.UnixNano()))
	writer := Writer{ClickHouse: conn, Router: router}
	identity := TrustedIdentity{EnterpriseID: enterpriseID, ResourceID: resourceID, CollectorID: uuid.New()}
	record := &kgo.Record{Topic: "otlp-metrics", Partition: 0, Offset: 1}
	if _, err := writer.writeMetrics(ctx, record, identity, request, time.Hour); err != nil {
		t.Fatal(err)
	}

	engine := promqlengine.NewEngine(conn, router, nil)
	query := func(expression string) promql.Matrix {
		t.Helper()
		result, err := engine.Execute(ctx, promqlengine.Request{
			Expression: expression,
			Start:      now.Add(-time.Minute),
			End:        now.Add(time.Minute),
			Step:       15 * time.Second,
			Scope:      promqlengine.Scope{EnterpriseID: enterpriseID, ResourceIDs: []uuid.UUID{resourceID}},
			MaxSamples: 10_000,
			MaxSeries:  100,
			Timeout:    10 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		matrix, ok := result.Value.(promql.Matrix)
		if !ok {
			t.Fatalf("result type = %T, want promql.Matrix", result.Value)
		}
		if len(matrix) == 0 {
			t.Fatalf("query %q returned no series", expression)
		}
		return matrix
	}

	query("argus_test_gauge")
	native := query("argus_test_native_histogram")
	if len(native[0].Histograms) == 0 {
		t.Fatal("native histogram query returned no histogram samples")
	}
}

func TestPromQLClickHouseConformance(t *testing.T) {
	address := os.Getenv("ARGUS_CLICKHOUSE_TEST_ADDRESS")
	if address == "" {
		t.Skip("ARGUS_CLICKHOUSE_TEST_ADDRESS is not set")
	}
	conn, err := OpenClickHouse(address, envOrDefault("ARGUS_CLICKHOUSE_TEST_DATABASE", "argus_telemetry"), envOrDefault("ARGUS_CLICKHOUSE_TEST_USERNAME", "argus"), envOrDefault("ARGUS_CLICKHOUSE_TEST_PASSWORD", "argus"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	enterpriseID, resourceID := uuid.New(), uuid.New()
	router := TenantTableRouter{}
	manager := ClickHouseTenantSchemaManager{Conn: conn, Router: router}
	if err := manager.EnsureTenant(ctx, enterpriseID); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyTenant(ctx, enterpriseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.DropTenant(context.Background(), enterpriseID) })

	end := time.Now().UTC().Truncate(time.Minute)
	points := conformanceMetricPoints(end)
	writer := Writer{ClickHouse: conn, Router: router}
	if _, err := writer.writeMetrics(ctx, &kgo.Record{Topic: "otlp-metrics", Partition: 0, Offset: 9}, TrustedIdentity{EnterpriseID: enterpriseID, ResourceID: resourceID, CollectorID: uuid.New()}, conformanceMetricsRequest(points), time.Hour); err != nil {
		t.Fatal(err)
	}

	reference := newConformanceQueryable(points)
	argus := promqlengine.NewEngine(conn, router, nil)
	referenceEngine := promql.NewEngine(promql.EngineOpts{MaxSamples: 100_000, Timeout: 10 * time.Second, LookbackDelta: 5 * time.Minute, EnableAtModifier: true, EnableNegativeOffset: true, Parser: parser.NewParser(parser.Options{})})
	instantCases := []string{
		`sum by (service) (argus_test_usage)`,
		`argus_test_usage / on(service) group_left argus_test_limit`,
		`rate(argus_test_requests_total[3m])`,
		`avg_over_time(argus_test_usage[3m])`,
		`max_over_time(argus_test_usage[2m:1m])`,
		`argus_test_usage offset 1m`,
		fmt.Sprintf(`argus_test_usage @ %d`, end.Unix()),
	}
	for _, expression := range instantCases {
		t.Run(expression, func(t *testing.T) {
			actual, err := argus.Execute(ctx, promqlengine.Request{Expression: expression, Instant: true, Start: end.Add(-5 * time.Minute), End: end, Scope: promqlengine.Scope{EnterpriseID: enterpriseID, ResourceIDs: []uuid.UUID{resourceID}}, MaxSamples: 100_000, MaxSeries: 100, MaxScanBytes: 256 << 20, Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			expected := executeReferencePromQL(t, ctx, referenceEngine, reference, expression, true, end.Add(-5*time.Minute), end, time.Minute)
			assertPromQLValueEqual(t, expected, actual.Value)
			if actual.ScannedBytes <= 0 || actual.ScannedRows <= 0 {
				t.Fatalf("missing ClickHouse progress rows=%d bytes=%d", actual.ScannedRows, actual.ScannedBytes)
			}
		})
	}

	for _, expression := range []string{`argus_test_usage`, `sum by (service) (argus_test_usage)`} {
		t.Run("range "+expression, func(t *testing.T) {
			actual, err := argus.Execute(ctx, promqlengine.Request{Expression: expression, Start: end.Add(-2 * time.Minute), End: end, Step: time.Minute, Scope: promqlengine.Scope{EnterpriseID: enterpriseID, ResourceIDs: []uuid.UUID{resourceID}}, MaxSamples: 100_000, MaxSeries: 100, MaxScanBytes: 256 << 20, Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			expected := executeReferencePromQL(t, ctx, referenceEngine, reference, expression, false, end.Add(-2*time.Minute), end, time.Minute)
			assertPromQLValueEqual(t, expected, actual.Value)
		})
	}
}

func TestTenantSchemaClickHouseRejectsColumnDrift(t *testing.T) {
	address := os.Getenv("ARGUS_CLICKHOUSE_TEST_ADDRESS")
	if address == "" {
		t.Skip("ARGUS_CLICKHOUSE_TEST_ADDRESS is not set")
	}
	conn, err := OpenClickHouse(address, envOrDefault("ARGUS_CLICKHOUSE_TEST_DATABASE", "argus_telemetry"), envOrDefault("ARGUS_CLICKHOUSE_TEST_USERNAME", "argus"), envOrDefault("ARGUS_CLICKHOUSE_TEST_PASSWORD", "argus"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	enterpriseID := uuid.New()
	manager := ClickHouseTenantSchemaManager{Conn: conn, Router: TenantTableRouter{}}
	if err := manager.EnsureTenant(ctx, enterpriseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.DropTenant(context.Background(), enterpriseID) })
	if err := manager.VerifyTenant(ctx, enterpriseID); err != nil {
		t.Fatal(err)
	}
	tables, err := manager.Router.Tables(enterpriseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec(ctx, "ALTER TABLE `"+tables.Logs+"` DROP COLUMN body_size"); err != nil {
		t.Fatal(err)
	}
	if err := manager.VerifyTenant(ctx, enterpriseID); err == nil || !strings.Contains(err.Error(), "body_size") {
		t.Fatalf("schema drift was not rejected: %v", err)
	}
}

type conformancePoint struct {
	metric  string
	labels  map[string]string
	times   []time.Time
	values  []float64
	counter bool
}

func conformanceMetricPoints(end time.Time) []conformancePoint {
	times := []time.Time{end.Add(-3 * time.Minute), end.Add(-2 * time.Minute), end.Add(-time.Minute), end}
	return []conformancePoint{
		{metric: "argus.test.usage", labels: map[string]string{"service": "api", "instance": "a"}, times: times, values: []float64{2, 4, 6, 8}},
		{metric: "argus.test.usage", labels: map[string]string{"service": "api", "instance": "b"}, times: times, values: []float64{1, 2, 3, 4}},
		{metric: "argus.test.limit", labels: map[string]string{"service": "api"}, times: times, values: []float64{2, 2, 2, 2}},
		{metric: "argus.test.requests_total", labels: map[string]string{"service": "api", "instance": "a"}, times: times, values: []float64{0, 10, 4, 14}, counter: true},
	}
}

func conformanceMetricsRequest(points []conformancePoint) *collectmetrics.ExportMetricsServiceRequest {
	metrics := make([]*metricspb.Metric, 0, len(points))
	for _, series := range points {
		dataPoints := make([]*metricspb.NumberDataPoint, 0, len(series.times))
		for index, timestamp := range series.times {
			attrs := make([]*commonpb.KeyValue, 0, len(series.labels))
			for key, value := range series.labels {
				attrs = append(attrs, &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}})
			}
			dataPoints = append(dataPoints, &metricspb.NumberDataPoint{TimeUnixNano: uint64(timestamp.UnixNano()), StartTimeUnixNano: uint64(series.times[0].UnixNano()), Attributes: attrs, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: series.values[index]}})
		}
		metric := &metricspb.Metric{Name: series.metric}
		if series.counter {
			metric.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: dataPoints, AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, IsMonotonic: true}}
		} else {
			metric.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: dataPoints}}
		}
		metrics = append(metrics, metric)
	}
	return &collectmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{Resource: &resourcepb.Resource{}, ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: metrics}}}}}
}

func newConformanceQueryable(points []conformancePoint) storage.Queryable {
	result := make([]storage.Series, 0, len(points))
	for _, point := range points {
		values := make([]string, 0, len(point.labels)*2+2)
		values = append(values, "__name__", promMetricName(point.metric))
		keys := make([]string, 0, len(point.labels))
		for key := range point.labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := point.labels[key]
			values = append(values, key, value)
		}
		labelSet := labels.FromStrings(values...)
		samples := make([]chunks.Sample, 0, len(point.times))
		for index, timestamp := range point.times {
			samples = append(samples, conformanceSample{timestamp: timestamp.UnixMilli(), value: point.values[index]})
		}
		result = append(result, storage.NewListSeries(labelSet, samples))
	}
	sort.Slice(result, func(i, j int) bool {
		return labels.Compare(result[i].Labels(), result[j].Labels()) < 0
	})
	return conformanceQueryable{series: result}
}

type conformanceSample struct {
	timestamp int64
	value     float64
}

func (sample conformanceSample) T() int64               { return sample.timestamp }
func (sample conformanceSample) ST() int64              { return 0 }
func (sample conformanceSample) F() float64             { return sample.value }
func (conformanceSample) H() *histogram.Histogram       { return nil }
func (conformanceSample) FH() *histogram.FloatHistogram { return nil }
func (conformanceSample) Type() chunkenc.ValueType      { return chunkenc.ValFloat }
func (sample conformanceSample) Copy() chunks.Sample    { return sample }

type conformanceQueryable struct {
	series []storage.Series
}

func (queryable conformanceQueryable) Querier(mint, maxt int64) (storage.Querier, error) {
	return &conformanceQuerier{mint: mint, maxt: maxt, series: queryable.series}, nil
}

type conformanceQuerier struct {
	mint   int64
	maxt   int64
	series []storage.Series
}

func (querier *conformanceQuerier) Select(_ context.Context, sortSeries bool, _ *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	selected := make([]storage.Series, 0, len(querier.series))
	for _, series := range querier.series {
		if matchesConformanceSeries(series.Labels(), matchers) {
			selected = append(selected, series)
		}
	}
	if sortSeries {
		sort.Slice(selected, func(i, j int) bool {
			return labels.Compare(selected[i].Labels(), selected[j].Labels()) < 0
		})
	}
	return &conformanceSeriesSet{series: selected, index: -1}
}

func (querier *conformanceQuerier) LabelValues(_ context.Context, name string, _ *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	values := make([]string, 0)
	for _, series := range querier.series {
		if matchesConformanceSeries(series.Labels(), matchers) {
			value := series.Labels().Get(name)
			if value != "" && !slices.Contains(values, value) {
				values = append(values, value)
			}
		}
	}
	sort.Strings(values)
	return values, nil, nil
}

func (querier *conformanceQuerier) LabelNames(_ context.Context, _ *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	names := make([]string, 0)
	for _, series := range querier.series {
		if !matchesConformanceSeries(series.Labels(), matchers) {
			continue
		}
		series.Labels().Range(func(label labels.Label) {
			if !slices.Contains(names, label.Name) {
				names = append(names, label.Name)
			}
		})
	}
	sort.Strings(names)
	return names, nil, nil
}

func (*conformanceQuerier) Close() error { return nil }

func matchesConformanceSeries(labelSet labels.Labels, matchers []*labels.Matcher) bool {
	for _, matcher := range matchers {
		if !matcher.Matches(labelSet.Get(matcher.Name)) {
			return false
		}
	}
	return true
}

type conformanceSeriesSet struct {
	series []storage.Series
	index  int
}

func (set *conformanceSeriesSet) Next() bool {
	set.index++
	return set.index < len(set.series)
}

func (set *conformanceSeriesSet) At() storage.Series {
	if set.index < 0 || set.index >= len(set.series) {
		return nil
	}
	return set.series[set.index]
}

func (*conformanceSeriesSet) Err() error                        { return nil }
func (*conformanceSeriesSet) Warnings() annotations.Annotations { return nil }

func executeReferencePromQL(t *testing.T, ctx context.Context, engine *promql.Engine, queryable storage.Queryable, expression string, instant bool, start, end time.Time, step time.Duration) parser.Value {
	t.Helper()
	options := promql.NewPrometheusQueryOpts(false, 5*time.Minute)
	var query promql.Query
	var err error
	if instant {
		query, err = engine.NewInstantQuery(ctx, queryable, options, expression, end)
	} else {
		query, err = engine.NewRangeQuery(ctx, queryable, options, expression, start, end, step)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer query.Close()
	result := query.Exec(ctx)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	return result.Value
}

func assertPromQLValueEqual(t *testing.T, expected, actual parser.Value) {
	t.Helper()
	want, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("PromQL result mismatch\nwant: %s\n got: %s", want, got)
	}
}

func mixedMetricTypesRequest(timestamp uint64) *collectmetrics.ExportMetricsServiceRequest {
	sum := 7.0
	return &collectmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: &resourcepb.Resource{},
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
			{Name: "argus.test.gauge", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{{TimeUnixNano: timestamp, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 7}}}}}},
			{Name: "argus.test.native.histogram", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
				AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
				DataPoints: []*metricspb.ExponentialHistogramDataPoint{{TimeUnixNano: timestamp, Count: 3, Sum: &sum, Scale: 1, ZeroCount: 1,
					Positive: &metricspb.ExponentialHistogramDataPoint_Buckets{Offset: 0, BucketCounts: []uint64{1, 1}}}},
			}}},
			{Name: "argus.test.summary", Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{DataPoints: []*metricspb.SummaryDataPoint{{
				TimeUnixNano: timestamp, Count: 2, Sum: 7,
				QuantileValues: []*metricspb.SummaryDataPoint_ValueAtQuantile{{Quantile: 0.5, Value: 3}, {Quantile: 0.9, Value: 4}},
			}}}}},
		}}},
	}}}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
