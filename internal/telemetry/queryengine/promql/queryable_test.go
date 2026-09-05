package promqlengine

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunks"
)

func TestSeriesSetStartsAtFirstSeries(t *testing.T) {
	first := &series{id: "first"}
	second := &series{id: "second"}
	set := &seriesSet{items: []*series{first, second}, index: -1}
	if !set.Next() || set.At() != first {
		t.Fatal("first series was skipped")
	}
	if !set.Next() || set.At() != second {
		t.Fatal("second series was skipped")
	}
	if set.Next() {
		t.Fatal("series set should be exhausted")
	}
}

func TestPromQLMatchersCompileToBoundParameters(t *testing.T) {
	matcher := labels.MustNewMatcher(labels.MatchRegexp, "service", "api-.+")
	sql, args := matcherSQL("s.labels[?]", matcher)
	if sql != "match(s.labels[?], ?)" || len(args) != 2 || args[0] != "service" || args[1] != "api-.+" {
		t.Fatalf("unexpected matcher: %q %#v", sql, args)
	}
}

func TestLabelSetIsStableAndIncludesMetricName(t *testing.T) {
	set := labelSet(map[string]string{"zone": "a", "__name__": "requests_total"})
	if set.Get("__name__") != "requests_total" || set.Get("zone") != "a" {
		t.Fatalf("unexpected labels: %s", set.String())
	}
}

func TestHistogramBaseAndSyntheticSeries(t *testing.T) {
	if base, ok := histogramBaseName("request_duration_seconds_bucket"); !ok || base != "request_duration_seconds" {
		t.Fatalf("unexpected histogram base: %q %v", base, ok)
	}
	seriesByID := map[string]*series{}
	count := uint64(3)
	sum := 7.5
	appendHistogramSeries(seriesByID, "series", "request_duration_seconds", map[string]string{"service": "api"},
		time.Unix(10, 0), &count, &sum, []uint64{1, 2}, []float64{0.5, 1}, "request_duration_seconds_bucket", 10, 100)
	if len(seriesByID) != 3 {
		t.Fatalf("expected count, sum and bucket series, got %d", len(seriesByID))
	}
	for _, item := range seriesByID {
		if len(item.samples) != 1 {
			t.Fatalf("expected one histogram sample, got %d", len(item.samples))
		}
	}
}

func TestSummaryAndNativeHistogramSamples(t *testing.T) {
	seriesByID := map[string]*series{}
	count, sum := uint64(4), 9.5
	appendSummarySeries(seriesByID, "series", "request_duration_seconds", map[string]string{"service": "api"}, time.Unix(10, 0), &count, &sum, [][2]float64{{0.5, 1.2}}, "", 10, 100)
	if len(seriesByID) != 3 {
		t.Fatalf("expected summary count, sum and quantile series, got %d", len(seriesByID))
	}
	for _, item := range seriesByID {
		if len(item.samples) != 1 {
			t.Fatalf("expected one summary sample, got %d", len(item.samples))
		}
	}
	scale, zeroCount, zeroThreshold, positiveOffset := int32(0), uint64(1), 0.001, int32(0)
	native := buildFloatHistogram(&scale, &zeroCount, &zeroThreshold, &positiveOffset, []uint64{2, 1}, nil, nil, &count, &sum)
	if native == nil || native.Count != float64(count) || len(native.PositiveBuckets) != 2 {
		t.Fatalf("unexpected native histogram: %#v", native)
	}
}

func TestDedupeSeriesKeepsLastIngestedSample(t *testing.T) {
	item := &series{samples: chunks.SampleSlice{
		querySample{timestamp: 10, value: 1},
		querySample{timestamp: 10, value: 2},
		querySample{timestamp: 20, value: 3},
	}}
	dedupeSeries(item)
	if len(item.samples) != 2 || item.samples[0].T() != 10 || item.samples[0].F() != 2 || item.samples[1].F() != 3 {
		t.Fatalf("unexpected deduped samples: %#v", item.samples)
	}
}

func TestSeriesIteratorSupportsRangeAggregationReuse(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UnixMilli()
	items := []*series{
		{id: "a", labels: labelSet(map[string]string{"__name__": "usage", "service": "api", "instance": "a"}), samples: chunks.SampleSlice{querySample{timestamp: start, value: 2}, querySample{timestamp: start + 60_000, value: 4}, querySample{timestamp: start + 120_000, value: 6}}},
		{id: "b", labels: labelSet(map[string]string{"__name__": "usage", "service": "api", "instance": "b"}), samples: chunks.SampleSlice{querySample{timestamp: start, value: 1}, querySample{timestamp: start + 60_000, value: 2}, querySample{timestamp: start + 120_000, value: 3}}},
	}
	queryable := storage.MockQueryable{MockQuerier: &storage.MockQuerier{SelectMockFunction: func(bool, *storage.SelectHints, ...*labels.Matcher) storage.SeriesSet {
		return &seriesSet{items: items, index: -1}
	}}}
	engine := promql.NewEngine(promql.EngineOpts{MaxSamples: 1_000, Timeout: time.Second, LookbackDelta: 5 * time.Minute, Parser: parser.NewParser(parser.Options{})})
	query, err := engine.NewRangeQuery(context.Background(), &queryable, promql.NewPrometheusQueryOpts(false, 5*time.Minute), "sum by (service) (usage)", time.UnixMilli(start), time.UnixMilli(start+120_000), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer query.Close()
	result := query.Exec(context.Background())
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	matrix, ok := result.Value.(promql.Matrix)
	if !ok || len(matrix) != 1 || len(matrix[0].Floats) != 3 {
		t.Fatalf("unexpected range aggregation result: %#v", result.Value)
	}
	for index, want := range []float64{3, 6, 9} {
		if matrix[0].Floats[index].F != want {
			t.Fatalf("point %d = %v, want %v", index, matrix[0].Floats[index].F, want)
		}
	}
}

func TestClonePromQLValueDoesNotSharePointStorage(t *testing.T) {
	original := promql.Matrix{{Floats: []promql.FPoint{{T: 1, F: 2}}}}
	cloned, ok := clonePromQLValue(original).(promql.Matrix)
	if !ok {
		t.Fatalf("clone type = %T", clonePromQLValue(original))
	}
	original[0].Floats[0].F = 99
	if cloned[0].Floats[0].F != 2 {
		t.Fatalf("clone shares pooled float storage: %#v", cloned)
	}
}
