//go:build m4e2e

package main

import (
	"testing"
	"time"

	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func TestMetricsRequestIncludesProfileAndRecoveryMetrics(t *testing.T) {
	request := metricsRequest(&resourcepb.Resource{}, uint64(time.Now().UnixNano()), ".backlog")
	metrics := request.ResourceMetrics[0].ScopeMetrics[0].Metrics
	if len(metrics) != 4 {
		t.Fatalf("metric count = %d, want 4", len(metrics))
	}
	if metrics[0].Name != "system.cpu.utilization" || metrics[0].Unit != "1" {
		t.Fatalf("profile metric = %q %q", metrics[0].Name, metrics[0].Unit)
	}
	if metrics[1].Name != "argus.m7.e2e.gauge.backlog" {
		t.Fatalf("recovery metric = %q", metrics[1].Name)
	}
	if metrics[2].Name != "argus.m7.e2e.native.histogram.backlog" || metrics[2].GetExponentialHistogram() == nil {
		t.Fatalf("native histogram metric = %q", metrics[2].Name)
	}
	if metrics[3].Name != "argus.m7.e2e.summary.backlog" || metrics[3].GetSummary() == nil {
		t.Fatalf("summary metric = %q", metrics[3].Name)
	}
}
