package promqlengine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/kakj-go/Argus/internal/telemetry/queryengine/chstats"
)

type TableRouter interface {
	Table(name string, enterpriseID uuid.UUID) (string, error)
}

type Scope struct {
	EnterpriseID uuid.UUID
	ResourceIDs  []uuid.UUID
}

type Queryable struct {
	Conn         driver.Conn
	Router       TableRouter
	Scope        Scope
	MaxSeries    int
	MaxSamples   int
	MaxScanBytes int64
	Timeout      time.Duration
	Progress     *chstats.Tracker
}

var _ storage.Queryable = Queryable{}

func (q Queryable) Querier(mint, maxt int64) (storage.Querier, error) {
	if q.Conn == nil || q.Router == nil || q.Scope.EnterpriseID == uuid.Nil || mint > maxt {
		return nil, fmt.Errorf("promql storage unavailable")
	}
	table, err := q.Router.Table("metric_series", q.Scope.EnterpriseID)
	if err != nil {
		return nil, err
	}
	samples, err := q.Router.Table("metric_samples", q.Scope.EnterpriseID)
	if err != nil {
		return nil, err
	}
	return &querier{conn: q.Conn, seriesTable: table, samplesTable: samples, scope: q.Scope, mint: mint, maxt: maxt, maxSeries: q.MaxSeries, maxSamples: q.MaxSamples, maxScanBytes: q.MaxScanBytes, timeout: q.Timeout, progress: q.Progress}, nil
}

type querier struct {
	conn                      driver.Conn
	seriesTable, samplesTable string
	scope                     Scope
	mint, maxt                int64
	maxSeries, maxSamples     int
	maxScanBytes              int64
	timeout                   time.Duration
	progress                  *chstats.Tracker
}

var _ storage.Querier = (*querier)(nil)

func (q *querier) queryContext(ctx context.Context, settings clickhouse.Settings) context.Context {
	if q.progress == nil {
		return clickhouse.Context(ctx, clickhouse.WithSettings(settings))
	}
	return q.progress.Context(ctx, settings)
}

func (q *querier) Select(ctx context.Context, sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	if q == nil || q.conn == nil {
		return storage.ErrSeriesSet(fmt.Errorf("promql storage unavailable"))
	}
	where := []string{"s.metric_name != ''"}
	args := make([]any, 0, len(matchers)*2+3)
	requestedMetric := ""
	for _, matcher := range matchers {
		if matcher == nil {
			continue
		}
		column := "s.labels[?]"
		if matcher.Name == "__name__" {
			column = "s.metric_name"
			if matcher.Type == labels.MatchEqual {
				requestedMetric = matcher.Value
				if base, ok := histogramBaseName(matcher.Value); ok {
					args = append(args, base)
					where = append(where, "s.metric_name = ?")
					continue
				}
			}
		}
		expression, values := matcherSQL(column, matcher)
		where = append(where, expression)
		args = append(args, values...)
	}
	if len(q.scope.ResourceIDs) > 0 {
		where = append(where, "s.resource_id IN (?)")
		args = append(args, q.scope.ResourceIDs)
	}
	limit := q.maxSeries
	if hints != nil && hints.Limit > 0 && (limit <= 0 || hints.Limit < limit) {
		limit = hints.Limit
	}
	if limit <= 0 {
		limit = 100000
	}
	settings := clickhouse.Settings{"max_result_rows": limit}
	if q.maxScanBytes > 0 {
		settings["max_bytes_to_read"] = q.maxScanBytes
	}
	if q.timeout > 0 {
		settings["max_execution_time"] = max(1, int(q.timeout.Seconds()))
	}
	seriesQuery := fmt.Sprintf("SELECT s.series_id, s.metric_name, s.labels FROM %s AS s WHERE %s ORDER BY s.series_id LIMIT ?", quoteIdentifier(q.seriesTable), strings.Join(where, " AND "))
	seriesArgs := append(args, limit)
	seriesRows, err := q.conn.Query(q.queryContext(ctx, settings), seriesQuery, seriesArgs...)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	selected := make(map[string]*series, limit)
	selectedIDs := make([]uuid.UUID, 0, limit)
	for seriesRows.Next() {
		var id uuid.UUID
		var metricName string
		var rawLabels map[string]string
		if err := seriesRows.Scan(&id, &metricName, &rawLabels); err != nil {
			seriesRows.Close()
			return storage.ErrSeriesSet(err)
		}
		item := ensureSeries(selected, id.String(), metricName, rawLabels, limit)
		if item == nil {
			seriesRows.Close()
			return storage.ErrSeriesSet(fmt.Errorf("promql max series exceeded"))
		}
		selectedIDs = append(selectedIDs, id)
	}
	if err := seriesRows.Err(); err != nil {
		seriesRows.Close()
		return storage.ErrSeriesSet(err)
	}
	seriesRows.Close()
	if len(selectedIDs) == 0 {
		return &seriesSet{items: nil, index: -1}
	}
	sampleQuery := fmt.Sprintf(`SELECT series_id, timestamp, float_value, stale_marker, sample_type, count, sum, bucket_counts, explicit_bounds,
	quantile_values, exponential_scale, exponential_zero_count, exponential_zero_threshold, exponential_positive_offset, exponential_positive_bucket_counts,
	exponential_negative_offset, exponential_negative_bucket_counts
FROM %s WHERE series_id IN (?) AND timestamp >= ? AND timestamp <= ? ORDER BY series_id, timestamp, ingest_key`, quoteIdentifier(q.samplesTable))
	// Prometheus storage uses Unix milliseconds, while ClickHouse stores the
	// sample timestamp as DateTime64(9). Binding integers makes ClickHouse treat
	// the comparison as decimal arithmetic and can overflow on current releases.
	sampleArgs := []any{selectedIDs, time.UnixMilli(q.mint).UTC(), time.UnixMilli(q.maxt).UTC()}
	rows, err := q.conn.Query(q.queryContext(ctx, settings), sampleQuery, sampleArgs...)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	defer rows.Close()
	seriesByID := make(map[string]*series)
	for rows.Next() {
		var id uuid.UUID
		var timestamp time.Time
		var value *float64
		var stale bool
		var sampleType string
		var count *uint64
		var sum *float64
		var bucketCounts []uint64
		var explicitBounds []float64
		var quantileRaw [][]float64
		var exponentialScale *int32
		var exponentialZeroCount *uint64
		var exponentialZeroThreshold *float64
		var exponentialPositiveOffset *int32
		var exponentialPositiveBuckets []uint64
		var exponentialNegativeOffset *int32
		var exponentialNegativeBuckets []uint64
		if err := rows.Scan(&id, &timestamp, &value, &stale, &sampleType, &count, &sum, &bucketCounts, &explicitBounds,
			&quantileRaw, &exponentialScale, &exponentialZeroCount, &exponentialZeroThreshold, &exponentialPositiveOffset, &exponentialPositiveBuckets,
			&exponentialNegativeOffset, &exponentialNegativeBuckets); err != nil {
			return storage.ErrSeriesSet(err)
		}
		item := selected[id.String()]
		if item == nil {
			continue
		}
		seriesByID[id.String()] = item
		quantileValues := normalizeQuantiles(quantileRaw)
		if sampleType == "histogram" && value == nil {
			appendHistogramSeries(seriesByID, id.String(), item.labels.Get("__name__"), labelsToMap(item.labels), timestamp, count, sum, bucketCounts, explicitBounds, requestedMetric, limit, q.maxSamples)
			continue
		}
		if sampleType == "summary" && value == nil {
			appendSummarySeries(seriesByID, id.String(), item.labels.Get("__name__"), labelsToMap(item.labels), timestamp, count, sum, quantileValues, requestedMetric, limit, q.maxSamples)
			continue
		}
		if sampleType == "exponential_histogram" && value == nil {
			native := buildFloatHistogram(exponentialScale, exponentialZeroCount, exponentialZeroThreshold, exponentialPositiveOffset, exponentialPositiveBuckets, exponentialNegativeOffset, exponentialNegativeBuckets, count, sum)
			if native == nil {
				return storage.ErrSeriesSet(fmt.Errorf("promql exponential histogram sample is malformed"))
			}
			appendHistogramSample(item, timestamp, native, q.maxSamples)
			continue
		}
		if value != nil && !timestamp.IsZero() {
			appendFloatSample(item, timestamp, *value, stale)
		}
	}
	if err := rows.Err(); err != nil {
		return storage.ErrSeriesSet(err)
	}
	result := make([]*series, 0, len(seriesByID))
	loadedSamples := 0
	for _, item := range seriesByID {
		sort.SliceStable(item.samples, func(i, j int) bool {
			return item.samples[i].T() < item.samples[j].T()
		})
		dedupeSeries(item)
		loadedSamples += len(item.samples)
		result = append(result, item)
	}
	if q.maxSamples > 0 && loadedSamples > q.maxSamples {
		return storage.ErrSeriesSet(fmt.Errorf("promql max samples exceeded"))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].labels.String() < result[j].labels.String() })
	if !sortSeries {
		return &seriesSet{items: result, index: -1}
	}
	return &seriesSet{items: result, index: -1}
}

func ensureSeries(seriesByID map[string]*series, id, metricName string, rawLabels map[string]string, limit int) *series {
	item := seriesByID[id]
	if item != nil {
		return item
	}
	if len(seriesByID) >= limit {
		return nil
	}
	labelsCopy := make(map[string]string, len(rawLabels)+1)
	for key, value := range rawLabels {
		labelsCopy[key] = value
	}
	if metricName != "" {
		labelsCopy["__name__"] = metricName
	}
	item = &series{id: id, labels: labelSet(labelsCopy)}
	seriesByID[id] = item
	return item
}

func appendHistogramSeries(seriesByID map[string]*series, id, metricName string, rawLabels map[string]string, timestamp time.Time, count *uint64, sum *float64, bucketCounts []uint64, explicitBounds []float64, requestedMetric string, limit, maxSamples int) {
	appendValue := func(suffix, seriesID string, extra map[string]string, value float64) {
		name := metricName + suffix
		if requestedMetric != "" && requestedMetric != name {
			return
		}
		labelsCopy := make(map[string]string, len(rawLabels)+2)
		for key, value := range rawLabels {
			labelsCopy[key] = value
		}
		labelsCopy["__name__"] = name
		for key, value := range extra {
			labelsCopy[key] = value
		}
		item := ensureSeries(seriesByID, seriesID+suffix+labelsCopy["le"], name, labelsCopy, limit)
		if item == nil || timestamp.IsZero() {
			return
		}
		appendFloatSample(item, timestamp, value, false)
	}
	if count != nil {
		appendValue("_count", id, nil, float64(*count))
	}
	if sum != nil {
		appendValue("_sum", id, nil, *sum)
	}
	cumulative := uint64(0)
	for index, bucket := range bucketCounts {
		cumulative += bucket
		le := "+Inf"
		if index < len(explicitBounds) {
			le = fmt.Sprintf("%g", explicitBounds[index])
		}
		appendValue("_bucket", id, map[string]string{"le": le}, float64(cumulative))
	}
	if len(bucketCounts) == len(explicitBounds) {
		appendValue("_bucket", id, map[string]string{"le": "+Inf"}, float64(cumulative))
	}
}

func appendSummarySeries(seriesByID map[string]*series, id, metricName string, rawLabels map[string]string, timestamp time.Time, count *uint64, sum *float64, quantiles [][2]float64, requestedMetric string, limit, maxSamples int) {
	appendValue := func(name string, extra map[string]string, value float64) {
		if requestedMetric != "" && requestedMetric != name {
			return
		}
		labelsCopy := make(map[string]string, len(rawLabels)+2)
		for key, itemValue := range rawLabels {
			labelsCopy[key] = itemValue
		}
		labelsCopy["__name__"] = name
		for key, itemValue := range extra {
			labelsCopy[key] = itemValue
		}
		item := ensureSeries(seriesByID, id+name+labelsCopy["quantile"], name, labelsCopy, limit)
		if item == nil || timestamp.IsZero() {
			return
		}
		appendFloatSample(item, timestamp, value, false)
	}
	if count != nil {
		appendValue(metricName+"_count", nil, float64(*count))
	}
	if sum != nil {
		appendValue(metricName+"_sum", nil, *sum)
	}
	for _, quantile := range quantiles {
		appendValue(metricName, map[string]string{"quantile": fmt.Sprintf("%g", quantile[0])}, quantile[1])
	}
}

func normalizeQuantiles(raw [][]float64) [][2]float64 {
	result := make([][2]float64, 0, len(raw))
	for _, parts := range raw {
		if len(parts) != 2 {
			continue
		}
		result = append(result, [2]float64{parts[0], parts[1]})
	}
	return result
}

func buildFloatHistogram(scale *int32, zeroCount *uint64, zeroThreshold *float64, positiveOffset *int32, positiveBuckets []uint64, negativeOffset *int32, negativeBuckets []uint64, count *uint64, sum *float64) *histogram.FloatHistogram {
	if scale == nil || zeroCount == nil || zeroThreshold == nil || count == nil {
		return nil
	}
	sumValue := float64(0)
	if sum != nil {
		sumValue = *sum
	}
	toFloat := func(values []uint64) []float64 {
		result := make([]float64, len(values))
		for index, value := range values {
			result[index] = float64(value)
		}
		return result
	}
	result := &histogram.FloatHistogram{Schema: *scale, ZeroThreshold: *zeroThreshold, ZeroCount: float64(*zeroCount), Count: float64(*count), Sum: sumValue}
	if positiveOffset != nil && len(positiveBuckets) > 0 {
		result.PositiveSpans = []histogram.Span{{Offset: *positiveOffset, Length: uint32(len(positiveBuckets))}}
		result.PositiveBuckets = toFloat(positiveBuckets)
	}
	if negativeOffset != nil && len(negativeBuckets) > 0 {
		result.NegativeSpans = []histogram.Span{{Offset: *negativeOffset, Length: uint32(len(negativeBuckets))}}
		result.NegativeBuckets = toFloat(negativeBuckets)
	}
	if err := result.Validate(); err != nil {
		return nil
	}
	return result
}

func histogramBaseName(name string) (string, bool) {
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return strings.TrimSuffix(name, suffix), true
		}
	}
	return "", false
}

func (q *querier) LabelValues(ctx context.Context, name string, _ *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	set := q.Select(ctx, false, nil, matchers...)
	values := map[string]struct{}{}
	for set.Next() {
		if value := set.At().Labels().Get(name); value != "" {
			values[value] = struct{}{}
		}
	}
	if err := set.Err(); err != nil {
		return nil, nil, err
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil, nil
}

func (q *querier) LabelNames(ctx context.Context, _ *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	set := q.Select(ctx, false, nil, matchers...)
	names := map[string]struct{}{}
	for set.Next() {
		set.At().Labels().Range(func(label labels.Label) { names[label.Name] = struct{}{} })
	}
	if err := set.Err(); err != nil {
		return nil, nil, err
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil, nil
}

func (*querier) Close() error { return nil }

type series struct {
	id      string
	labels  labels.Labels
	samples chunks.SampleSlice
}

func (s *series) Labels() labels.Labels { return s.labels }
func (s *series) Iterator(_ chunkenc.Iterator) chunkenc.Iterator {
	return storage.NewListSeriesIteratorWithCopy(s.samples)
}

func appendFloatSample(item *series, timestamp time.Time, value float64, stale bool) {
	if stale {
		value = staleNaN
	}
	item.samples = append(item.samples, querySample{timestamp: timestamp.UnixNano() / 1e6, value: value})
}

func appendHistogramSample(item *series, timestamp time.Time, value *histogram.FloatHistogram, maxSamples int) {
	if value == nil || timestamp.IsZero() {
		return
	}
	item.samples = append(item.samples, querySample{timestamp: timestamp.UnixNano() / 1e6, floatHistogram: value})
}

type querySample struct {
	timestamp      int64
	value          float64
	floatHistogram *histogram.FloatHistogram
}

func (sample querySample) T() int64                      { return sample.timestamp }
func (querySample) ST() int64                            { return 0 }
func (sample querySample) F() float64                    { return sample.value }
func (querySample) H() *histogram.Histogram              { return nil }
func (sample querySample) FH() *histogram.FloatHistogram { return sample.floatHistogram }
func (sample querySample) Type() chunkenc.ValueType {
	if sample.floatHistogram != nil {
		return chunkenc.ValFloatHistogram
	}
	return chunkenc.ValFloat
}
func (sample querySample) Copy() chunks.Sample {
	copy := sample
	if sample.floatHistogram != nil {
		copy.floatHistogram = sample.floatHistogram.Copy()
	}
	return copy
}

type seriesSet struct {
	items []*series
	index int
}

func (s *seriesSet) Next() bool { s.index++; return s.index < len(s.items) }
func (s *seriesSet) At() storage.Series {
	if s.index < 0 || s.index >= len(s.items) {
		return nil
	}
	return s.items[s.index]
}
func (s *seriesSet) Err() error                        { return nil }
func (s *seriesSet) Warnings() annotations.Annotations { return nil }

var staleNaN = math.Float64frombits(0x7ff0000000000002)

func matcherSQL(column string, matcher *labels.Matcher) (string, []any) {
	if matcher.Name != "__name__" {
		expression, values := matcherSQLWithKey(column, matcher)
		return expression, append([]any{matcher.Name}, values...)
	}
	return matcherSQLWithKey(column, matcher)
}

func matcherSQLWithKey(column string, matcher *labels.Matcher) (string, []any) {
	value := matcher.Value
	switch matcher.Type {
	case labels.MatchEqual:
		return column + " = ?", []any{value}
	case labels.MatchNotEqual:
		return column + " != ?", []any{value}
	case labels.MatchRegexp:
		return "match(" + column + ", ?)", []any{matcher.GetRegexString()}
	case labels.MatchNotRegexp:
		return "NOT match(" + column + ", ?)", []any{matcher.GetRegexString()}
	default:
		return "1 = 0", nil
	}
}

func labelSet(values map[string]string) labels.Labels {
	list := make([]labels.Label, 0, len(values)+1)
	for name, value := range values {
		list = append(list, labels.Label{Name: name, Value: value})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return labels.New(list...)
}

func labelsToMap(values labels.Labels) map[string]string {
	result := make(map[string]string, values.Len())
	values.Range(func(label labels.Label) { result[label.Name] = label.Value })
	delete(result, "__name__")
	return result
}

func dedupeSeries(item *series) {
	if item == nil || len(item.samples) < 2 {
		return
	}
	write := 0
	for read := 0; read < len(item.samples); read++ {
		if write > 0 && item.samples[write-1].T() == item.samples[read].T() {
			item.samples[write-1] = item.samples[read]
			continue
		}
		item.samples[write] = item.samples[read]
		write++
	}
	item.samples = item.samples[:write]
}

func quoteIdentifier(value string) string { return "`" + strings.ReplaceAll(value, "`", "") + "`" }
