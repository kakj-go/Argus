package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	collectlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collecttraces "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type Writer struct {
	Kafka      *kgo.Client
	ClickHouse driver.Conn
	Control    WriterControlStore
	Router     TenantTableRouter
	readyMu    sync.Mutex
	readyCache map[uuid.UUID]tenantReadyEntry
}

type tenantReadyEntry struct {
	ready   bool
	checked time.Time
}

type spanFact struct {
	traceID, spanID, parentID, service, operation, status string
	start, end                                            time.Time
	duration                                              uint64
}

func NewKafkaConsumer(brokers []string, group, username, password string) (*kgo.Client, error) {
	if username == "" || password == "" {
		return nil, ErrUnavailable
	}
	return kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.SASL(scram.Auth{User: username, Pass: password}.AsSha512Mechanism()),
		kgo.ConsumerGroup(group), kgo.ConsumeTopics("otlp-metrics", "otlp-logs", "otlp-traces"),
		kgo.DisableAutoCommit(), kgo.FetchMaxBytes(32<<20), kgo.FetchMaxPartitionBytes(16<<20))
}

func (writer *Writer) Run(ctx context.Context) error {
	if writer.Kafka == nil || writer.ClickHouse == nil || writer.Control == nil {
		return ErrUnavailable
	}
	for ctx.Err() == nil {
		fetches := writer.Kafka.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			time.Sleep(time.Second)
			continue
		}
		for iterator := fetches.RecordIter(); !iterator.Done(); {
			record := iterator.Next()
			if err := writer.writeRecord(ctx, record); err != nil {
				if errors.Is(err, errPermanentRecord) {
					if dlqErr := writer.deadLetter(ctx, record, err); dlqErr != nil {
						return dlqErr
					}
				} else {
					time.Sleep(time.Second)
					break
				}
			}
			if err := writer.Kafka.CommitRecords(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

var errPermanentRecord = errors.New("permanent telemetry record error")

func (writer *Writer) writeRecord(ctx context.Context, record *kgo.Record) error {
	identity, err := identityFromHeaders(record.Headers)
	if err != nil {
		return fmt.Errorf("%w: identity", errPermanentRecord)
	}
	writable, err := writer.Control.TelemetryWritable(ctx, identity.EnterpriseID)
	if err != nil {
		return err
	}
	if !writable {
		return fmt.Errorf("%w: enterprise telemetry is disabled", errPermanentRecord)
	}
	ready, err := writer.tenantReady(ctx, identity.EnterpriseID)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%w: enterprise telemetry tables are not ready", errPermanentRecord)
	}
	retention, err := writer.Control.Retention(ctx, identity.EnterpriseID)
	if err != nil {
		return err
	}
	switch record.Topic {
	case "otlp-metrics":
		request := &collectmetrics.ExportMetricsServiceRequest{}
		if proto.Unmarshal(record.Value, request) != nil || validateMetrics(request) != nil {
			return fmt.Errorf("%w: metrics payload", errPermanentRecord)
		}
		count, err := writer.writeMetrics(ctx, record, identity, request, retention.Metrics)
		if err == nil {
			err = writer.usage(ctx, identity.EnterpriseID, int64(len(record.Value)), count, 0, 0)
		}
		return err
	case "otlp-logs":
		request := &collectlogs.ExportLogsServiceRequest{}
		if proto.Unmarshal(record.Value, request) != nil || validateLogs(request) != nil {
			return fmt.Errorf("%w: logs payload", errPermanentRecord)
		}
		count, err := writer.writeLogs(ctx, record, identity, request, retention.Logs)
		if err == nil {
			err = writer.usage(ctx, identity.EnterpriseID, int64(len(record.Value)), 0, count, 0)
		}
		return err
	case "otlp-traces":
		request := &collecttraces.ExportTraceServiceRequest{}
		if proto.Unmarshal(record.Value, request) != nil || validateTraces(request) != nil {
			return fmt.Errorf("%w: traces payload", errPermanentRecord)
		}
		count, err := writer.writeTraces(ctx, record, identity, request, retention.Traces)
		if err == nil {
			err = writer.usage(ctx, identity.EnterpriseID, int64(len(record.Value)), 0, 0, count)
		}
		return err
	default:
		return fmt.Errorf("%w: unknown topic", errPermanentRecord)
	}
}

func (writer *Writer) tenantReady(ctx context.Context, enterpriseID uuid.UUID) (bool, error) {
	now := time.Now()
	writer.readyMu.Lock()
	if entry, ok := writer.readyCache[enterpriseID]; ok && now.Sub(entry.checked) < 5*time.Second {
		writer.readyMu.Unlock()
		return entry.ready, nil
	}
	writer.readyMu.Unlock()
	ready, err := writer.Control.TelemetryTenantReady(ctx, enterpriseID)
	if err != nil {
		return false, err
	}
	writer.readyMu.Lock()
	if writer.readyCache == nil {
		writer.readyCache = make(map[uuid.UUID]tenantReadyEntry)
	}
	writer.readyCache[enterpriseID] = tenantReadyEntry{ready: ready, checked: now}
	writer.readyMu.Unlock()
	return ready, nil
}

func (writer *Writer) writeMetrics(ctx context.Context, record *kgo.Record, identity TrustedIdentity, request *collectmetrics.ExportMetricsServiceRequest, retention time.Duration) (int64, error) {
	tables, err := writer.Router.Tables(identity.EnterpriseID)
	if err != nil {
		return 0, err
	}
	seriesBatch, err := writer.ClickHouse.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s
		(resource_id, series_id, metric_name, labels, labels_hash, resource_attributes, scope_name, scope_version, scope_attributes, metric_type, temporality, is_monotonic, unit, description, kafka_topic, kafka_partition, kafka_offset, record_sequence, expires_at)`, tables.MetricSeries))
	if err != nil {
		return 0, err
	}
	sampleBatch, err := writer.ClickHouse.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s
		(resource_id, series_id, metric_name, timestamp, start_timestamp, value, float_value, count, sum, min, max, bucket_counts, explicit_bounds, quantile_values,
		 exponential_scale, exponential_zero_count, exponential_zero_threshold, exponential_positive_offset, exponential_positive_bucket_counts, exponential_negative_offset, exponential_negative_bucket_counts,
			sample_type, ingest_key, kafka_topic, kafka_partition, kafka_offset, record_sequence, expires_at)`, tables.MetricSamples))
	if err != nil {
		return 0, err
	}
	var sequence uint32
	seriesSeen := map[string]struct{}{}
	for _, resourceMetrics := range request.ResourceMetrics {
		resourceAttrs := attributesMap(resourceMetrics.Resource.GetAttributes())
		for _, scope := range resourceMetrics.ScopeMetrics {
			scopeAttrs := attributesMap(scope.Scope.GetAttributes())
			for _, metric := range scope.Metrics {
				metricName := promMetricName(metric.Name)
				if metricName == "" {
					return 0, fmt.Errorf("%w: invalid metric name", errPermanentRecord)
				}
				appendPoint := func(timestamp uint64, start uint64, value any, count any, sum any, min any, max any, buckets []uint64, bounds []float64, quantiles [][]float64,
					exponentialScale any, exponentialZeroCount any, exponentialZeroThreshold any, exponentialPositiveOffset any, exponentialPositiveBuckets []uint64,
					exponentialNegativeOffset any, exponentialNegativeBuckets []uint64, attrs []*commonpb.KeyValue, metricType, temporality string, monotonic bool) error {
					sequence++
					observed := timestampValue(timestamp)
					labels := normalizedMetricLabels(attrs)
					seriesID, labelsHash := metricSeriesID(identity, metricName, labels)
					seriesKey := seriesID.String()
					if _, exists := seriesSeen[seriesKey]; !exists {
						seriesSeen[seriesKey] = struct{}{}
						if err := seriesBatch.Append(identity.ResourceID, seriesID, metricName, labels, labelsHash, resourceAttrs,
							scope.Scope.GetName(), scope.Scope.GetVersion(), scopeAttrs, metricType, temporality, monotonic, metric.Unit, metric.Description,
							record.Topic, record.Partition, record.Offset, sequence, observed.Add(retention)); err != nil {
							return err
						}
					}
					if err := sampleBatch.Append(identity.ResourceID, seriesID, metricName, observed, timestampValue(start), value, value, count, sum, min, max,
						buckets, bounds, quantiles, exponentialScale, exponentialZeroCount, exponentialZeroThreshold, exponentialPositiveOffset, exponentialPositiveBuckets,
						exponentialNegativeOffset, exponentialNegativeBuckets, metricType, ingestKey(record, sequence), record.Topic, record.Partition, record.Offset, sequence, observed.Add(retention)); err != nil {
						return err
					}
					return nil
				}
				switch data := metric.Data.(type) {
				case *metricspb.Metric_Gauge:
					for _, point := range data.Gauge.DataPoints {
						if err := appendPoint(point.TimeUnixNano, 0, numberValue(point), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, point.Attributes, "gauge", "unspecified", false); err != nil {
							return 0, err
						}
					}
				case *metricspb.Metric_Sum:
					if data.Sum.IsMonotonic && data.Sum.AggregationTemporality == metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA {
						return 0, fmt.Errorf("%w: delta monotonic sums are not accepted without cumulative conversion", errPermanentRecord)
					}
					for _, point := range data.Sum.DataPoints {
						if err := appendPoint(point.TimeUnixNano, point.StartTimeUnixNano, numberValue(point), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, point.Attributes, "sum", data.Sum.AggregationTemporality.String(), data.Sum.IsMonotonic); err != nil {
							return 0, err
						}
					}
				case *metricspb.Metric_Histogram:
					for _, point := range data.Histogram.DataPoints {
						var sum any
						if point.Sum != nil {
							sum = *point.Sum
						}
						if err := appendPoint(point.TimeUnixNano, point.StartTimeUnixNano, nil, point.Count, sum, point.Min, point.Max, point.BucketCounts, point.ExplicitBounds, nil, nil, nil, nil, nil, nil, nil, nil, point.Attributes, "histogram", data.Histogram.AggregationTemporality.String(), false); err != nil {
							return 0, err
						}
					}
				case *metricspb.Metric_ExponentialHistogram:
					for _, point := range data.ExponentialHistogram.DataPoints {
						var sum any
						if point.Sum != nil {
							sum = *point.Sum
						}
						if err := appendPoint(point.TimeUnixNano, point.StartTimeUnixNano, nil, point.Count, sum, point.Min, point.Max, nil, nil, nil,
							point.Scale, point.ZeroCount, point.ZeroThreshold, point.Positive.GetOffset(), point.Positive.GetBucketCounts(), point.Negative.GetOffset(), point.Negative.GetBucketCounts(),
							point.Attributes, "exponential_histogram", data.ExponentialHistogram.AggregationTemporality.String(), false); err != nil {
							return 0, err
						}
					}
				case *metricspb.Metric_Summary:
					for _, point := range data.Summary.DataPoints {
						quantiles := make([][]float64, 0, len(point.QuantileValues))
						for _, quantile := range point.QuantileValues {
							quantiles = append(quantiles, []float64{quantile.Quantile, quantile.Value})
						}
						if err := appendPoint(point.TimeUnixNano, point.StartTimeUnixNano, nil, point.Count, point.Sum, nil, nil, nil, nil, quantiles, nil, nil, nil, nil, nil, nil, nil, point.Attributes, "summary", "unspecified", false); err != nil {
							return 0, err
						}
					}
				}
			}
		}
	}
	if sequence == 0 {
		return 0, fmt.Errorf("%w: empty metrics", errPermanentRecord)
	}
	if err := seriesBatch.Send(); err != nil {
		return 0, err
	}
	return int64(sequence), sampleBatch.Send()
}

func (writer *Writer) writeLogs(ctx context.Context, record *kgo.Record, identity TrustedIdentity, request *collectlogs.ExportLogsServiceRequest, retention time.Duration) (int64, error) {
	tables, err := writer.Router.Tables(identity.EnterpriseID)
	if err != nil {
		return 0, err
	}
	batch, err := writer.ClickHouse.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s
		(resource_id, collector_id, timestamp, observed_timestamp, severity_text, severity_number, service_name, scope_name, scope_version, scope_attributes, stream_labels, structured_metadata, body, body_size, trace_id, span_id, event_id, ingest_key, kafka_topic, kafka_partition, kafka_offset, record_sequence, expires_at)`, tables.Logs))
	if err != nil {
		return 0, err
	}
	var sequence uint32
	for _, resourceLogs := range request.ResourceLogs {
		serviceName := attributeString(resourceLogs.Resource.GetAttributes(), "service.name")
		resourceAttrs := attributesMap(resourceLogs.Resource.GetAttributes())
		streamLabels := streamLabelMap(resourceAttrs)
		for _, scope := range resourceLogs.ScopeLogs {
			scopeAttrs := attributesMap(scope.Scope.GetAttributes())
			for _, item := range scope.LogRecords {
				sequence++
				observed := timestampValue(item.TimeUnixNano)
				if item.TimeUnixNano == 0 {
					observed = timestampValue(item.ObservedTimeUnixNano)
				}
				body := anyValueString(item.Body)
				if err := batch.Append(identity.ResourceID, identity.CollectorID, observed, timestampValue(item.ObservedTimeUnixNano), item.SeverityText, item.SeverityNumber, serviceName,
					scope.Scope.GetName(), scope.Scope.GetVersion(), scopeAttrs,
					streamLabels, attributesMap(item.Attributes), body, uint32(len(body)), hex.EncodeToString(item.TraceId), hex.EncodeToString(item.SpanId), ingestKey(record, sequence), ingestKey(record, sequence),
					record.Topic, record.Partition, record.Offset, sequence, observed.Add(retention)); err != nil {
					return 0, err
				}
			}
		}
	}
	if sequence == 0 {
		return 0, fmt.Errorf("%w: empty logs", errPermanentRecord)
	}
	return int64(sequence), batch.Send()
}

func (writer *Writer) writeTraces(ctx context.Context, record *kgo.Record, identity TrustedIdentity, request *collecttraces.ExportTraceServiceRequest, retention time.Duration) (int64, error) {
	tables, err := writer.Router.Tables(identity.EnterpriseID)
	if err != nil {
		return 0, err
	}
	batch, err := writer.ClickHouse.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s
		(resource_id, collector_id, trace_id, span_id, parent_span_id, root_span_id, service_name, operation, span_kind, status, status_code, status_message, trace_state, start_time, end_time, duration_ns, resource_attributes, scope_name, scope_version, scope_attributes, attributes, events, links,
			 ingest_key, kafka_topic, kafka_partition, kafka_offset, record_sequence, expires_at)`, tables.Traces))
	if err != nil {
		return 0, err
	}
	var sequence uint32
	byTrace := map[string][]spanFact{}
	rootSpanIDs := traceRootSpanIDs(request)
	for _, resourceSpans := range request.ResourceSpans {
		serviceName := attributeString(resourceSpans.Resource.GetAttributes(), "service.name")
		resourceAttrs := attributesMap(resourceSpans.Resource.GetAttributes())
		for _, scope := range resourceSpans.ScopeSpans {
			scopeAttrs := attributesMap(scope.Scope.GetAttributes())
			for _, span := range scope.Spans {
				sequence++
				started := timestampValue(span.StartTimeUnixNano)
				duration := uint64(0)
				if span.EndTimeUnixNano >= span.StartTimeUnixNano {
					duration = span.EndTimeUnixNano - span.StartTimeUnixNano
				}
				statusCode := tracepb.Status_STATUS_CODE_UNSET
				statusMessage := ""
				if span.Status != nil {
					statusCode = span.Status.Code
					statusMessage = span.Status.Message
				}
				spanStatus := normalizedSpanStatus(statusCode)
				traceID := hex.EncodeToString(span.TraceId)
				spanID := hex.EncodeToString(span.SpanId)
				parentID := hex.EncodeToString(span.ParentSpanId)
				rootID := rootSpanIDs[traceID]
				ended := started.Add(time.Duration(duration))
				events, _ := json.Marshal(span.Events)
				links, _ := json.Marshal(span.Links)
				if err := batch.Append(identity.ResourceID, identity.CollectorID, traceID, spanID,
					parentID, rootID, serviceName, span.Name, uint8(span.Kind), spanStatus, uint8(statusCode), statusMessage, span.TraceState, started, ended, duration, resourceAttrs,
					scope.Scope.GetName(), scope.Scope.GetVersion(), scopeAttrs, attributesMap(span.Attributes), string(events), string(links), ingestKey(record, sequence),
					record.Topic, record.Partition, record.Offset, sequence, started.Add(retention)); err != nil {
					return 0, err
				}
				byTrace[traceID] = append(byTrace[traceID], spanFact{traceID, spanID, parentID, serviceName, span.Name, spanStatus, started, ended, duration})
			}
		}
	}
	if sequence == 0 {
		return 0, fmt.Errorf("%w: empty traces", errPermanentRecord)
	}
	if err := batch.Send(); err != nil {
		return 0, err
	}
	if err := writer.writeTraceDerived(ctx, identity, byTrace, record, retention); err != nil {
		return 0, err
	}
	return int64(sequence), nil
}

func (writer *Writer) writeTraceDerived(ctx context.Context, identity TrustedIdentity, traces map[string][]spanFact, record *kgo.Record, retention time.Duration) error {
	if len(traces) == 0 {
		return nil
	}
	tables, err := writer.Router.Tables(identity.EnterpriseID)
	if err != nil {
		return err
	}
	summary, err := writer.ClickHouse.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s
		(resource_id, trace_id, root_span_id, root_service, root_operation, start_time, duration_ns, span_count, error_count, status, expires_at)`, tables.TraceSummary))
	if err != nil {
		return err
	}
	edges, err := writer.ClickHouse.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s
		(resource_id, trace_id, parent_span_id, child_span_id, parent_service, child_service, depth, expires_at)`, tables.TraceSpanEdges))
	if err != nil {
		return err
	}
	for traceID, spans := range traces {
		if len(spans) == 0 {
			continue
		}
		root := spans[0]
		errorsCount := uint32(0)
		maxEnd := root.end
		rootFound := false
		services := map[string]string{}
		for _, span := range spans {
			services[span.spanID] = span.service
			if span.parentID == "" {
				root = span
				rootFound = true
			}
			if span.status == "error" {
				errorsCount++
			}
			if span.end.After(maxEnd) {
				maxEnd = span.end
			}
		}
		if !rootFound {
			root = spans[0]
		}
		status := "unset"
		if errorsCount > 0 {
			status = "error"
		} else {
			status = "ok"
		}
		if err := summary.Append(identity.ResourceID, traceID, root.spanID, root.service, root.operation, root.start, uint64(maxEnd.Sub(root.start)), uint32(len(spans)), errorsCount, status, root.start.Add(retention)); err != nil {
			return err
		}
		for _, span := range spans {
			if span.parentID != "" {
				if err := edges.Append(identity.ResourceID, traceID, span.parentID, span.spanID, services[span.parentID], span.service, uint16(1), span.start.Add(retention)); err != nil {
					return err
				}
			}
		}
	}
	if err := summary.Send(); err != nil {
		return err
	}
	return edges.Send()
}

func traceRootSpanIDs(request *collecttraces.ExportTraceServiceRequest) map[string]string {
	result := map[string]string{}
	if request == nil {
		return result
	}
	for _, resourceSpans := range request.ResourceSpans {
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			for _, span := range scopeSpans.Spans {
				if len(span.TraceId) == 0 || len(span.SpanId) == 0 || len(span.ParentSpanId) != 0 {
					continue
				}
				traceID := hex.EncodeToString(span.TraceId)
				if _, exists := result[traceID]; !exists {
					result[traceID] = hex.EncodeToString(span.SpanId)
				}
			}
		}
	}
	return result
}

func normalizedSpanStatus(code tracepb.Status_StatusCode) string {
	switch code {
	case tracepb.Status_STATUS_CODE_OK:
		return "ok"
	case tracepb.Status_STATUS_CODE_ERROR:
		return "error"
	default:
		return "unset"
	}
}

func metricSeriesID(identity TrustedIdentity, metricName string, labels map[string]string) (uuid.UUID, uint64) {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := fnv.New64a()
	for _, key := range keys {
		_, _ = hash.Write([]byte(key))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(labels[key]))
		_, _ = hash.Write([]byte{0})
	}
	labelsHash := hash.Sum64()
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s/%s/%s/%d", identity.EnterpriseID, identity.ResourceID, metricName, labelsHash))), labelsHash
}

func streamLabelMap(attrs map[string]string) map[string]string {
	result := make(map[string]string)
	for source, target := range map[string]string{
		"service.name":           "service_name",
		"deployment.environment": "deployment_environment",
		"host.name":              "host_name",
		"k8s.namespace.name":     "k8s_namespace_name",
		"k8s.cluster.name":       "k8s_cluster_name",
	} {
		if value := attrs[source]; value != "" {
			result[target] = value
		}
	}
	return result
}

func normalizedMetricLabels(values []*commonpb.KeyValue) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		name := promLabelName(value.Key)
		if name == "" || strings.HasPrefix(name, "__") || strings.HasPrefix(name, "argus_") {
			continue
		}
		result[name] = anyValueString(value.Value)
	}
	return result
}

func promLabelName(value string) string {
	if value == "" {
		return ""
	}
	var result strings.Builder
	result.Grow(len(value))
	for index, current := range []byte(value) {
		valid := current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' || current == '_' || index > 0 && current >= '0' && current <= '9'
		if valid {
			result.WriteByte(current)
		} else {
			result.WriteByte('_')
		}
	}
	return result.String()
}

func promMetricName(value string) string {
	if value == "" {
		return ""
	}
	var result strings.Builder
	result.Grow(len(value))
	for index, current := range []byte(value) {
		valid := current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' || current == '_' || current == ':' || index > 0 && current >= '0' && current <= '9'
		if valid {
			result.WriteByte(current)
		} else {
			result.WriteByte('_')
		}
	}
	return result.String()
}

func (writer *Writer) deadLetter(ctx context.Context, record *kgo.Record, cause error) error {
	hash := sha256.Sum256(record.Value)
	dlq := &kgo.Record{Topic: record.Topic + "-dlq", Key: record.Key, Value: record.Value, Headers: append(record.Headers,
		kgo.RecordHeader{Key: "argus-source-topic", Value: []byte(record.Topic)}, kgo.RecordHeader{Key: "argus-source-partition", Value: []byte(strconv.Itoa(int(record.Partition)))},
		kgo.RecordHeader{Key: "argus-source-offset", Value: []byte(strconv.FormatInt(record.Offset, 10))}, kgo.RecordHeader{Key: "argus-error", Value: []byte(cause.Error())})}
	result := writer.Kafka.ProduceSync(ctx, dlq)
	if err := result.FirstErr(); err != nil {
		return err
	}
	delivered := result[0].Record
	signal := strings.TrimPrefix(record.Topic, "otlp-")
	return writer.Control.RecordDLQ(ctx, TelemetryDLQRecord{ID: uuid.Must(uuid.NewV7()), Signal: signal, Topic: record.Topic,
		Partition: record.Partition, SourceOffset: record.Offset, DLQTopic: delivered.Topic, DLQPartition: delivered.Partition,
		DLQOffset: delivered.Offset, RecordHash: hash, ErrorCode: "TELEMETRY_RECORD_PERMANENT_ERROR"})
}

func (writer *Writer) usage(ctx context.Context, enterpriseID uuid.UUID, bytes, metrics, logs, spans int64) error {
	return writer.Control.IncrementUsage(ctx, enterpriseID, TelemetryUsageDelta{Bytes: bytes, Metrics: metrics, Logs: logs, Spans: spans})
}

func identityFromHeaders(headers []kgo.RecordHeader) (TrustedIdentity, error) {
	values := map[string]string{}
	for _, header := range headers {
		values[header.Key] = string(header.Value)
	}
	enterprise, err := uuid.Parse(values["argus-enterprise-id"])
	if err != nil {
		return TrustedIdentity{}, err
	}
	resourceID, err := uuid.Parse(values["argus-resource-id"])
	if err != nil {
		return TrustedIdentity{}, err
	}
	collector, err := uuid.Parse(values["argus-collector-id"])
	if err != nil {
		return TrustedIdentity{}, err
	}
	return TrustedIdentity{EnterpriseID: enterprise, ResourceID: resourceID, CollectorID: collector, ResourceType: values["argus-resource-type"]}, nil
}

func numberValue(point *metricspb.NumberDataPoint) float64 {
	switch value := point.Value.(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		return value.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		return float64(value.AsInt)
	default:
		return 0
	}
}
func timestampValue(value uint64) time.Time {
	if value == 0 {
		return time.Now().UTC()
	}
	return time.Unix(0, int64(value)).UTC()
}
func ingestKey(record *kgo.Record, sequence uint32) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%d/%d", record.Topic, record.Partition, record.Offset, sequence)))
	return hex.EncodeToString(hash[:])
}
func attributesMap(values []*commonpb.KeyValue) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		if value != nil {
			result[value.Key] = anyValueString(value.Value)
		}
	}
	return result
}
func attributeString(values []*commonpb.KeyValue, key string) string {
	for _, value := range values {
		if value != nil && value.Key == key {
			return anyValueString(value.Value)
		}
	}
	return ""
}
func anyValueString(value *commonpb.AnyValue) string {
	if value == nil {
		return ""
	}
	switch item := value.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return item.StringValue
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(item.BoolValue)
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(item.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(item.DoubleValue, 'g', -1, 64)
	case *commonpb.AnyValue_BytesValue:
		return hex.EncodeToString(item.BytesValue)
	default:
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
}
