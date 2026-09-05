package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	telemetryapi "github.com/kakj-go/Argus/internal/gen/openapi/telemetryapi"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func TestCollectorPreviewInputPreservesRouteTransportAndLoopbackPort(t *testing.T) {
	distributionID := uuid.Must(uuid.NewV7())
	profileID := uuid.Must(uuid.NewV7())
	port := 14317
	input, err := collectorPreviewInputFromAPI(telemetryapi.CollectorPreview{
		DistributionVersionId: openapi_types.UUID(distributionID), ProfileIds: []openapi_types.UUID{openapi_types.UUID(profileID)},
		RouteKind: telemetryapi.DirectArgus, Transport: telemetryapi.TelemetryRouteTransportExecutorTunnel, LoopbackPort: &port,
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.Transport != "executor_tunnel" || input.LoopbackPort != 14317 || input.RouteKind != "direct_argus" {
		t.Fatalf("route transport fields were dropped: %#v", input)
	}
}

func TestCollectorPreviewInputRejectsInvalidLoopbackPort(t *testing.T) {
	port := 65536
	_, err := collectorPreviewInputFromAPI(telemetryapi.CollectorPreview{
		DistributionVersionId: openapi_types.UUID(uuid.Must(uuid.NewV7())),
		ProfileIds:            []openapi_types.UUID{openapi_types.UUID(uuid.Must(uuid.NewV7()))}, RouteKind: telemetryapi.DirectArgus,
		Transport: telemetryapi.TelemetryRouteTransportDirect, LoopbackPort: &port,
	})
	if err == nil {
		t.Fatal("invalid tunnel loopback port accepted")
	}
}

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

func TestTelemetryIdempotencyErrorsHaveStablePublicMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		code string
		key  string
	}{
		{postgres.ErrIdempotencyConflict, "IDEMPOTENCY_CONFLICT", "errors.common.idempotency_conflict"},
		{postgres.ErrIdempotencyExpired, "IDEMPOTENCY_RESULT_EXPIRED", "errors.common.idempotency_result_expired"},
	}
	for _, test := range tests {
		if got := telemetryStatus(test.err); got != http.StatusConflict {
			t.Fatalf("telemetryStatus(%v) = %d, want %d", test.err, got, http.StatusConflict)
		}
		got := telemetryError(context.Background(), test.err)
		if got.Code != test.code || got.MessageKey != test.key {
			t.Fatalf("telemetryError(%v) = %#v", test.err, got)
		}
	}
}

func TestTelemetryDependencyErrorMatchesPublicContract(t *testing.T) {
	t.Parallel()
	got := telemetryError(context.Background(), telemetryservice.ErrUnavailable)
	if got.Code != "TELEMETRY_DEPENDENCY_UNAVAILABLE" || got.MessageKey != "errors.telemetry.dependency_unavailable" || got.Retryable == nil || !*got.Retryable {
		t.Fatalf("telemetryError(ErrUnavailable) = %#v", got)
	}
}
