package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	telemetryapi "github.com/kakj-go/Argus/internal/gen/openapi/telemetryapi"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
)

func TestTelemetryAuthStatusPreservesAuthorizationVersionConflict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code   string
		status int
	}{
		{"AUTHORIZATION_VERSION_STALE", http.StatusConflict},
		{"SESSION_EXPIRED", http.StatusUnauthorized},
		{"SESSION_REVOKED", http.StatusUnauthorized},
		{"AUTHORIZATION_DENIED", http.StatusForbidden},
	}
	for _, test := range tests {
		if got := telemetryAuthStatus(telemetryapi.ApiError{Code: test.code}); got != test.status {
			t.Fatalf("telemetryAuthStatus(%q) = %d, want %d", test.code, got, test.status)
		}
	}
}

func TestTelemetryEngineBudgetBindsMaxSeries(t *testing.T) {
	maxSeries, maxSamples, timeout := 321, 654, 1234
	budget := telemetryEngineBudget(&telemetryapi.TelemetryQueryBudget{MaxSeries: &maxSeries, MaxSamples: &maxSamples, TimeoutMs: &timeout})
	if budget.MaxSeries != maxSeries || budget.MaxSamples != maxSamples || budget.Timeout != 1234*time.Millisecond {
		t.Fatalf("unexpected query budget: %#v", budget)
	}
	defaults := telemetryEngineBudget(nil)
	if defaults.MaxSeries != 100_000 {
		t.Fatalf("default max series = %d, want 100000", defaults.MaxSeries)
	}
}

func TestProtocolSpecificTelemetryResponses(t *testing.T) {
	result := queryengine.Result{ResultType: "matrix", Data: []any{"sample"}, Meta: queryengine.QueryMeta{PlanHash: "a", Engine: "prometheus-promql", EngineVersion: "v1", ScannedBytes: 10, ScannedRows: 20, ReturnedRows: 1, LoadedSamples: 2, ElapsedMillis: 3}}
	prometheus := prometheusQueryResponse(result)
	if prometheus.Status != telemetryapi.Success || prometheus.Data.ResultType != "matrix" || prometheus.ArgusMeta.LoadedSamples != 2 {
		t.Fatalf("unexpected Prometheus response: %#v", prometheus)
	}
	result.ResultType = "log_entries"
	kql := kqlQueryResponse(result)
	if kql.SchemaVersion != telemetryapi.ArgusKqlResultv1 || kql.ResultType != "log_entries" {
		t.Fatalf("unexpected KQL response: %#v", kql)
	}
	result.Data = map[string]any{"queryTrace": map[string]any{"traceId": "trace-1"}}
	trace, err := skyWalkingGraphQLResponse(result)
	if err != nil || trace.Data["queryTrace"] == nil || trace.Extensions.Argus.Engine != "prometheus-promql" {
		t.Fatalf("unexpected GraphQL response: %#v, %v", trace, err)
	}
}

func TestTelemetryRequestErrorPreservesStaleAuthorizationResponse(t *testing.T) {
	t.Parallel()
	want := telemetryapi.ApiError{
		Code:       "AUTHORIZATION_VERSION_STALE",
		MessageKey: "errors.auth.authorization_version_stale",
		RequestId:  "request-123",
	}
	err := telemetryRequestError{apiError: want}

	if got := telemetryStatus(err); got != http.StatusConflict {
		t.Fatalf("telemetryStatus(stale authorization) = %d, want %d", got, http.StatusConflict)
	}
	if got := telemetryError(context.Background(), err); got != want {
		t.Fatalf("telemetryError(stale authorization) = %#v, want %#v", got, want)
	}
}

func TestTelemetryQueryErrorsHaveStablePublicMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{telemetryservice.ErrQueryParse, http.StatusBadRequest, "QUERY_PARSE_ERROR"},
		{telemetryservice.ErrQueryType, http.StatusBadRequest, "QUERY_TYPE_ERROR"},
		{telemetryservice.ErrQueryUnsupported, http.StatusBadRequest, "QUERY_FEATURE_UNSUPPORTED"},
		{telemetryservice.ErrQueryComplexity, http.StatusBadRequest, "QUERY_COMPLEXITY_LIMIT"},
		{telemetryservice.ErrQueryScope, http.StatusForbidden, "QUERY_SCOPE_DENIED"},
		{telemetryservice.ErrQueryBudget, http.StatusRequestEntityTooLarge, "QUERY_BUDGET_EXCEEDED"},
		{queryengine.ErrBudget, http.StatusRequestEntityTooLarge, "QUERY_BUDGET_EXCEEDED"},
	}
	for _, test := range tests {
		if got := telemetryStatus(test.err); got != test.status {
			t.Fatalf("telemetryStatus(%v) = %d, want %d", test.err, got, test.status)
		}
		if got := telemetryError(context.Background(), test.err).Code; got != test.code {
			t.Fatalf("telemetryError(%v) = %q, want %q", test.err, got, test.code)
		}
		if test.code == "QUERY_BUDGET_EXCEEDED" {
			if got := telemetryError(context.Background(), test.err).MessageKey; got != "errors.telemetry.query_budget_exceeded" {
				t.Fatalf("telemetryError(%v) message key = %q", test.err, got)
			}
		}
	}
}
