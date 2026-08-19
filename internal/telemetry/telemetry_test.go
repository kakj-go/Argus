package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	collectmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/common/v1"
	telemetryv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/telemetry/v1"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestValidateArtifactHash(t *testing.T) {
	hash := sha256.Sum256([]byte("collector"))
	value, err := validateArtifactHash(stringHex(hash[:]))
	if err != nil || value != stringHex(hash[:]) {
		t.Fatalf("valid hash rejected: %q %v", value, err)
	}
	for _, invalid := range []string{"", "00", "not-hex"} {
		if _, err := validateArtifactHash(invalid); err == nil {
			t.Fatalf("invalid artifact hash %q accepted", invalid)
		}
	}
}

func TestOverrideMetricIdentityRemovesForgedArgusAttributes(t *testing.T) {
	identity := TrustedIdentity{EnterpriseID: uuid.New(), ResourceID: uuid.New(), CollectorID: uuid.New(), ResourceType: "host"}
	request := &collectmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
		{Key: "argus.enterprise.id", Value: stringValue(uuid.NewString())},
		{Key: "service.name", Value: stringValue("checkout")},
	}}}}}
	overrideMetricIdentity(request, identity)
	attributes := request.ResourceMetrics[0].Resource.Attributes
	if got := attributeString(attributes, "argus.enterprise.id"); got != identity.EnterpriseID.String() {
		t.Fatalf("trusted enterprise identity not applied: %q", got)
	}
	if got := attributeString(attributes, "service.name"); got != "checkout" {
		t.Fatalf("non-Argus resource attribute lost: %q", got)
	}
	count := 0
	for _, item := range attributes {
		if item.Key == "argus.enterprise.id" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("forged Argus attributes were not replaced: %d", count)
	}
}

func TestDownstreamIdentityRejectsIncompleteMixedAndNonGatewayClaims(t *testing.T) {
	collectorID := uuid.New()
	valid := []*commonpb.KeyValue{
		{Key: "argus.downstream.collector.id", Value: stringValue(collectorID.String())},
		{Key: "argus.downstream.certificate.serial", Value: stringValue("00AB")},
	}
	reference, err := downstreamFromAttributes(valid)
	if err != nil || !reference.found || reference.collectorID != collectorID || reference.serial != "00ab" {
		t.Fatalf("valid downstream reference rejected: %#v %v", reference, err)
	}
	if _, err = downstreamFromAttributes(valid[:1]); err == nil {
		t.Fatal("downstream reference without certificate serial accepted")
	}
	if _, err = mergeDownstreamReference(reference, downstreamReference{collectorID: uuid.New(), serial: "00ab", found: true}); err == nil {
		t.Fatal("mixed downstream Collector identities accepted")
	}
	if _, err = mergeDownstreamReference(reference, downstreamReference{}); err == nil {
		t.Fatal("mixed downstream and direct resources accepted")
	}

	direct := TrustedIdentity{EnterpriseID: uuid.New(), ResourceID: uuid.New(), CollectorID: uuid.New(), ResourceType: "host", Role: "node_agent"}
	resolved, err := (&IngestServer{}).resolveDownstreamIdentity(context.Background(), direct, downstreamReference{})
	if err != nil || resolved != direct {
		t.Fatalf("direct Collector identity rejected: %#v %v", resolved, err)
	}
	if _, err = (&IngestServer{}).resolveDownstreamIdentity(context.Background(), direct, reference); err == nil {
		t.Fatal("non-Gateway Collector forwarded a downstream identity")
	}
	kubernetesGateway := TrustedIdentity{EnterpriseID: uuid.New(), ResourceID: uuid.New(), CollectorID: reference.collectorID,
		ResourceType: "kubernetes_cluster", Role: "daemonset", CertificateSerial: reference.serial}
	resolved, err = (&IngestServer{}).resolveDownstreamIdentity(context.Background(), kubernetesGateway, reference)
	if err != nil || resolved != kubernetesGateway {
		t.Fatalf("same-Collector Kubernetes Gateway identity rejected: %#v %v", resolved, err)
	}
	for name, forged := range map[string]downstreamReference{
		"collector": {collectorID: uuid.New(), serial: reference.serial, found: true},
		"serial":    {collectorID: reference.collectorID, serial: "deadbeef", found: true},
	} {
		t.Run("kubernetes Gateway rejects forged "+name, func(t *testing.T) {
			if _, err := (&IngestServer{}).resolveDownstreamIdentity(context.Background(), kubernetesGateway, forged); err == nil {
				t.Fatal("forged Kubernetes Gateway downstream identity was accepted")
			}
		})
	}
}

func TestValidateDownstreamFactsBindsEnterpriseGatewayRouteAndCertificate(t *testing.T) {
	enterpriseID, gatewayID, downstreamID, resourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	gateway := TrustedIdentity{EnterpriseID: enterpriseID, CollectorID: gatewayID, ResourceID: uuid.New(), Role: "edge_gateway"}
	reference := downstreamReference{collectorID: downstreamID, serial: "00ab", found: true}
	downstream := db.CollectorInstance{ID: downstreamID, EnterpriseID: enterpriseID, ResourceID: resourceID,
		ResourceType: "host", Role: "node_agent", AuthorizationVersion: 9}
	route := db.TelemetryRoute{CollectorID: downstreamID, EnterpriseID: enterpriseID, Kind: "bastion_gateway", Status: "active",
		GatewayCollectorID: uuid.NullUUID{UUID: gatewayID, Valid: true}}
	identity, err := validateDownstreamFacts(gateway, reference, true, downstream, route)
	if err != nil || identity.CollectorID != downstreamID || identity.ResourceID != resourceID || identity.AuthorizationVersion != 9 {
		t.Fatalf("valid Gateway downstream facts rejected: %#v %v", identity, err)
	}

	cases := []struct {
		name             string
		certificateValid bool
		downstream       db.CollectorInstance
		route            db.TelemetryRoute
	}{
		{name: "revoked certificate", downstream: downstream, route: route},
		{name: "cross enterprise", certificateValid: true, downstream: func() db.CollectorInstance { value := downstream; value.EnterpriseID = uuid.New(); return value }(), route: route},
		{name: "wrong Gateway", certificateValid: true, downstream: downstream, route: func() db.TelemetryRoute {
			value := route
			value.GatewayCollectorID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			return value
		}()},
		{name: "inactive route", certificateValid: true, downstream: downstream, route: func() db.TelemetryRoute { value := route; value.Status = "degraded"; return value }()},
		{name: "direct route", certificateValid: true, downstream: downstream, route: func() db.TelemetryRoute { value := route; value.Kind = "direct_argus"; return value }()},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateDownstreamFacts(gateway, reference, test.certificateValid, test.downstream, test.route); err == nil {
				t.Fatal("invalid Gateway downstream facts accepted")
			}
		})
	}
}

func TestRouteTestOutcomeRequiresConvergedActiveFacts(t *testing.T) {
	status, code := routeTestOutcome("converged", "active")
	if status != "succeeded" || code.Valid {
		t.Fatalf("converged active route failed: %q %#v", status, code)
	}
	for _, state := range [][2]string{{"installing", "active"}, {"converged", "pending"}, {"degraded", "degraded"}} {
		status, code = routeTestOutcome(state[0], state[1])
		if status != "failed" || !code.Valid || code.String != "TELEMETRY_ROUTE_NOT_CONVERGED" {
			t.Fatalf("route state %v was not rejected: %q %#v", state, status, code)
		}
	}
}

func TestValidateMetricsSupportsM7MetricKinds(t *testing.T) {
	now := uint64(time.Now().UnixNano())
	request := &collectmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{
		{Name: "request.duration", Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{DataPoints: []*metricspb.ExponentialHistogramDataPoint{{TimeUnixNano: now, Count: 1}}}}},
		{Name: "payload.size", Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{DataPoints: []*metricspb.SummaryDataPoint{{TimeUnixNano: now, Count: 1, Sum: 2}}}}},
	}}}}}}
	if err := validateMetrics(request); err != nil {
		t.Fatalf("valid M7 metric kinds rejected: %v", err)
	}
}

func TestQueryRequestBindsScopeSignalAndBudget(t *testing.T) {
	enterpriseID, resourceID := uuid.New(), uuid.New()
	from, to := time.Now().Add(-time.Hour).UTC(), time.Now().UTC()
	request := &telemetryv1.TelemetryQueryRequest{
		SchemaVersion: querySchemaVersion,
		Scope: &telemetryv1.TelemetryQueryScope{
			EnterpriseId: enterpriseID.String(), AuthorizedResources: []*commonv1.ResourceRef{{ResourceType: "host", ResourceId: resourceID.String()}},
			AllowedSignals: []string{"metrics"}, AuthorizationVersion: 7,
		},
		From: timestamppb.New(from), To: timestamppb.New(to), Limit: 100, FilterJson: []byte(`{"metric_name":"cpu"}`),
		MaxScanBytes: uint64(DefaultMaxScanBytes), TimeoutMillis: uint32(DefaultTimeout / time.Millisecond),
	}
	request.Scope.ScopeHash = scopeHash(enterpriseID, []uuid.UUID{resourceID}, 7, "metrics")
	if _, err := queryRequestFromProto("metrics", request); err != nil {
		t.Fatalf("valid query rejected: %v", err)
	}
	request.Scope.AllowedSignals = []string{"logs"}
	if _, err := queryRequestFromProto("metrics", request); err == nil {
		t.Fatal("cross-signal query accepted")
	}
	request.Scope.AllowedSignals = []string{"metrics"}
	request.Scope.ScopeHash = scopeHash(enterpriseID, []uuid.UUID{uuid.New()}, 7, "metrics")
	if _, err := queryRequestFromProto("metrics", request); err == nil {
		t.Fatal("forged resource scope hash accepted")
	}
	request.Scope.ScopeHash = scopeHash(enterpriseID, []uuid.UUID{resourceID}, 7, "metrics")
	request.Scope.AuthorizationVersion = 8
	if _, err := queryRequestFromProto("metrics", request); err == nil {
		t.Fatal("stale AuthorizationVersion scope hash accepted")
	}
}

func TestQueryBudgetAndPartialMetadataAreFailClosed(t *testing.T) {
	request := QueryRequest{MaxScanBytes: 1024, TimeoutMS: 100}
	for name, meta := range map[string]QueryMeta{
		"scan":          {ScannedBytes: 1025},
		"time":          {ElapsedMS: 101},
		"negative scan": {ScannedBytes: -1},
		"negative time": {ElapsedMS: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateBackendMeta(meta, request); !errors.Is(err, ErrQueryBudget) {
				t.Fatalf("budget violation was accepted: %v", err)
			}
		})
	}
	meta := mergePartial(QueryMeta{PartialReasons: []string{"backend_truncated", "unauthorized_resources"}}, true)
	if !meta.Partial || !slices.Equal(meta.PartialReasons, []string{"backend_truncated", "unauthorized_resources"}) {
		t.Fatalf("partial metadata is unstable: %#v", meta)
	}
}

func TestMetricQueryPreservesAuthorizationErrors(t *testing.T) {
	if !errors.Is(validateMetricQueryInput(ErrDenied, "system.cpu.utilization"), ErrDenied) {
		t.Fatal("authorization denial was converted into an input validation error")
	}
	if !errors.Is(validateMetricQueryInput(nil, ""), ErrQueryInvalid) {
		t.Fatal("missing metric name was not rejected")
	}
}

func TestTelemetryProjectionUsesSnakeCaseAndSensitivePolicy(t *testing.T) {
	overview := Overview{ResourceCount: 2, HealthyCollectors: 1, MetricPoints: 3, LogRecords: 4, Spans: 5, WindowSeconds: 3600, Partial: true}
	encoded, err := json.Marshal(overview)
	if err != nil {
		t.Fatal(err)
	}
	var projection map[string]any
	if json.Unmarshal(encoded, &projection) != nil || projection["resource_count"] != float64(2) || projection["metric_points"] != float64(3) {
		t.Fatalf("Telemetry Tool/Card projection is not snake_case: %s", encoded)
	}
	if _, leaked := projection["ResourceCount"]; leaked {
		t.Fatalf("Go field leaked into public projection: %s", encoded)
	}
	required, ok := overviewOutputSchema()["required"].([]string)
	if !ok {
		t.Fatal("Telemetry Tool/Card output schema has no required field list")
	}
	for _, key := range required {
		if _, exists := projection[key]; !exists {
			t.Fatalf("Web/Agent/Card overview projection is missing %q: %s", key, encoded)
		}
	}
	if got := redactTelemetryText("Authorization: Bearer private-value"); got != "[redacted by telemetry field policy]" {
		t.Fatalf("sensitive log body was not redacted: %q", got)
	}
	if got := redactTelemetryText("ordinary application log"); got != "ordinary application log" {
		t.Fatalf("ordinary log was unexpectedly changed: %q", got)
	}
}

func TestIngestFailsClosedWithoutRedisAndKafka(t *testing.T) {
	server := &IngestServer{}
	err := server.publish(context.Background(), "metrics", TrustedIdentity{CollectorID: uuid.New()}, []byte("payload"))
	if err == nil {
		t.Fatal("ingest accepted payload without Redis and Kafka")
	}
}

func TestIngestRegistersGZIPCompression(t *testing.T) {
	if encoding.GetCompressor("gzip") == nil {
		t.Fatal("OTLP/gRPC gzip decompressor is not registered")
	}
}

func TestClickHouseUnsignedCountsUseCheckedConversions(t *testing.T) {
	if got, err := checkedIntFromUInt64(7); err != nil || got != 7 {
		t.Fatalf("valid trace count rejected: %d %v", got, err)
	}
	if _, err := checkedIntFromUInt64(uint64(math.MaxInt) + 1); err == nil {
		t.Fatal("overflowing trace count accepted")
	}
	if got, err := checkedInt64FromUInt64(11); err != nil || got != 11 {
		t.Fatalf("valid overview count rejected: %d %v", got, err)
	}
	if _, err := checkedInt64FromUInt64(uint64(math.MaxInt64) + 1); err == nil {
		t.Fatal("overflowing overview count accepted")
	}
}

func TestGRPCTargetRequiresSecureSchemeWhenURLIsUsed(t *testing.T) {
	if _, err := grpcTarget("https://query.internal:9447"); err == nil {
		t.Fatal("HTTPS URL accepted for gRPC endpoint")
	}
	if value, err := grpcTarget("grpcs://query.internal:9447"); err != nil || value != "query.internal:9447" {
		t.Fatalf("valid gRPC endpoint rejected: %q %v", value, err)
	}
}

func TestCollectorActionRiskUsesPendingActionContract(t *testing.T) {
	for _, operation := range []string{"install", "configure", "repair"} {
		if got := collectorActionRisk(operation); got != "write" {
			t.Fatalf("%s risk = %q, want write", operation, got)
		}
	}
	for _, operation := range []string{"upgrade", "uninstall"} {
		if got := collectorActionRisk(operation); got != "dangerous" {
			t.Fatalf("%s risk = %q, want dangerous", operation, got)
		}
	}
}

func TestExpandProfileClaimsSeparatesMultiSignalOwnership(t *testing.T) {
	claims, err := expandProfileClaims("otlp-receiver", []string{"application.otlp"}, []string{"metrics", "logs", "traces"})
	if err != nil {
		t.Fatalf("expand multi-signal claims: %v", err)
	}
	want := []profileClaim{
		{ClaimType: "application.otlp.metrics", Signal: "metrics"},
		{ClaimType: "application.otlp.logs", Signal: "logs"},
		{ClaimType: "application.otlp.traces", Signal: "traces"},
	}
	if len(claims) != len(want) {
		t.Fatalf("claim count = %d, want %d", len(claims), len(want))
	}
	for index := range want {
		if claims[index] != want[index] {
			t.Fatalf("claim %d = %#v, want %#v", index, claims[index], want[index])
		}
	}
	if _, err = expandProfileClaims("invalid", []string{"one", "two"}, []string{"metrics", "logs", "traces"}); err == nil {
		t.Fatal("ambiguous claim-to-signal mapping accepted")
	}
	if _, err = expandProfileClaims("invalid", []string{"duplicate", "duplicate"}, []string{"metrics", "logs"}); err == nil {
		t.Fatal("duplicate primary claim type accepted")
	}
}

func stringHex(value []byte) string {
	const alphabet = "0123456789abcdef"
	encoded := make([]byte, len(value)*2)
	for index, item := range value {
		encoded[index*2] = alphabet[item>>4]
		encoded[index*2+1] = alphabet[item&0x0f]
	}
	return string(encoded)
}
