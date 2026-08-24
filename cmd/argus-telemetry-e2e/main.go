//go:build m4e2e

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	collectlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collecttraces "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	endpoint := flag.String("endpoint", "argus-otelcol-gateway.argus-telemetry.svc.cluster.local:4317", "OTLP gRPC endpoint")
	resourceID := flag.String("resource-id", "e2e-resource", "resource marker")
	collectorID := flag.String("collector-id", "", "trusted Collector UUID for Kafka injection")
	enterpriseID := flag.String("enterprise-id", "", "trusted Enterprise UUID for Kafka injection")
	marker := flag.String("marker", "", "suffix used to distinguish recovery records")
	logBody := flag.String("log-body", "", "explicit log body used by authorization and redaction tests")
	kafkaBrokers := flag.String("kafka-brokers", "", "comma-separated Kafka brokers for permanent-record injection")
	kafkaUsername := flag.String("kafka-username", "", "Kafka SCRAM username")
	kafkaPassword := flag.String("kafka-password", "", "Kafka SCRAM password")
	tlsCA := flag.String("tls-ca", "", "trusted OTLP server CA path")
	tlsCertificate := flag.String("tls-cert", "", "OTLP client certificate path")
	tlsKey := flag.String("tls-key", "", "OTLP client private key path")
	tlsServerName := flag.String("tls-server-name", "", "expected OTLP server name")
	flag.Parse()
	if *kafkaPassword == "" {
		*kafkaPassword = os.Getenv("ARGUS_E2E_KAFKA_PASSWORD")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if *kafkaBrokers != "" {
		producePermanentRecord(ctx, strings.Split(*kafkaBrokers, ","), *kafkaUsername, *kafkaPassword, *enterpriseID, *resourceID, *collectorID)
		return
	}
	transport := credentials.TransportCredentials(insecure.NewCredentials())
	if *tlsCA != "" || *tlsCertificate != "" || *tlsKey != "" || *tlsServerName != "" {
		transport = telemetryTransportCredentials(*tlsCA, *tlsCertificate, *tlsKey, *tlsServerName)
	}
	now := uint64(time.Now().UTC().UnixNano())
	resource := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
		{Key: "service.name", Value: stringValue("argus-m7-e2e")},
		{Key: "argus.enterprise.id", Value: stringValue("forged-enterprise")},
		{Key: "argus.resource.id", Value: stringValue(*resourceID)},
	}}
	suffix := ""
	if *marker != "" {
		suffix = "." + *marker
	}
	// Use an independent authenticated RPC connection for each signal. This
	// keeps the gateway's per-RPC identity context isolated while still
	// exercising the same leaf certificate and route for all three signals.
	for _, export := range []func(grpc.ClientConnInterface) error{
		func(connection grpc.ClientConnInterface) error {
			_, err := collectmetrics.NewMetricsServiceClient(connection).Export(ctx, metricsRequest(resource, now, suffix))
			return err
		},
		func(connection grpc.ClientConnInterface) error {
			_, err := collectlogs.NewLogsServiceClient(connection).Export(ctx, logsRequest(resource, now, suffix, *logBody))
			return err
		},
		func(connection grpc.ClientConnInterface) error {
			_, err := collecttraces.NewTraceServiceClient(connection).Export(ctx, tracesRequest(resource, now, suffix))
			return err
		},
	} {
		if err := exportWithRetry(ctx, *endpoint, transport, 500*time.Millisecond, export); err != nil {
			log.Fatal(err)
		}
	}
}

func exportWithRetry(ctx context.Context, endpoint string, transport credentials.TransportCredentials, retryDelay time.Duration, export func(grpc.ClientConnInterface) error) error {
	var lastErr error
	for {
		connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(transport))
		if err == nil {
			err = export(connection)
			_ = connection.Close()
		}
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(retryDelay):
		}
	}
}

func telemetryTransportCredentials(caPath, certificatePath, keyPath, serverName string) credentials.TransportCredentials {
	if caPath == "" || certificatePath == "" || keyPath == "" || serverName == "" {
		log.Fatal("complete telemetry mTLS configuration is required")
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		log.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		log.Fatal("telemetry CA bundle is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		log.Fatal(err)
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: serverName})
}

func metricsRequest(resource *resourcepb.Resource, now uint64, suffix string) *collectmetrics.ExportMetricsServiceRequest {
	histogramSum := 7.0
	return &collectmetrics.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: resource,
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{
					{Name: "system.cpu.utilization", Unit: "1", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
						DataPoints: []*metricspb.NumberDataPoint{{TimeUnixNano: now, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 0.42}}},
					}}},
					{Name: "argus.m7.e2e.gauge" + suffix, Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
						DataPoints: []*metricspb.NumberDataPoint{{TimeUnixNano: now, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 7}}},
					}}},
					{Name: "argus.m7.e2e.native.histogram" + suffix, Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
						AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
						DataPoints: []*metricspb.ExponentialHistogramDataPoint{{TimeUnixNano: now, Count: 3, Sum: &histogramSum, Scale: 1, ZeroCount: 1,
							Positive: &metricspb.ExponentialHistogramDataPoint_Buckets{Offset: 0, BucketCounts: []uint64{1, 1}}}},
					}}},
					{Name: "argus.m7.e2e.summary" + suffix, Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{DataPoints: []*metricspb.SummaryDataPoint{{
						TimeUnixNano: now, Count: 2, Sum: 7, QuantileValues: []*metricspb.SummaryDataPoint_ValueAtQuantile{{Quantile: 0.5, Value: 3}, {Quantile: 0.9, Value: 4}},
					}}}}},
				},
			}},
		}},
	}
}

func logsRequest(resource *resourcepb.Resource, now uint64, suffix, explicitBody string) *collectlogs.ExportLogsServiceRequest {
	body := "argus m7 e2e log" + suffix
	if explicitBody != "" {
		body = explicitBody
	}
	return &collectlogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{Resource: resource,
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{TimeUnixNano: now, SeverityText: "INFO", Body: stringValue(body)}}}}}}}
}

func tracesRequest(resource *resourcepb.Resource, now uint64, suffix string) *collecttraces.ExportTraceServiceRequest {
	return &collecttraces.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{Resource: resource,
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{TraceId: []byte("0123456789abcdef"), SpanId: []byte("12345678"), Name: "argus-m7-e2e" + suffix,
			Kind: tracepb.Span_SPAN_KIND_INTERNAL, StartTimeUnixNano: now, EndTimeUnixNano: now + uint64(time.Millisecond)}}}}}}}
}

func producePermanentRecord(ctx context.Context, brokers []string, username, password, enterpriseID, resourceID, collectorID string) {
	if len(brokers) == 0 || username == "" || password == "" || enterpriseID == "" || resourceID == "" || collectorID == "" {
		log.Fatal("Kafka injection requires brokers, credentials, and trusted identity")
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.SASL(scram.Auth{User: username, Pass: password}.AsSha512Mechanism()),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	record := &kgo.Record{Topic: "otlp-metrics", Value: []byte("not-an-otlp-protobuf"), Headers: []kgo.RecordHeader{
		{Key: "argus-enterprise-id", Value: []byte(enterpriseID)},
		{Key: "argus-resource-id", Value: []byte(resourceID)},
		{Key: "argus-collector-id", Value: []byte(collectorID)},
		{Key: "argus-resource-type", Value: []byte("kubernetes_cluster")},
	}}
	if err = client.ProduceSync(ctx, record).FirstErr(); err != nil {
		log.Fatal(err)
	}
}

func stringValue(value string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}
}
