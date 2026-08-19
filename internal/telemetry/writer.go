package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	"google.golang.org/protobuf/proto"
)

type Writer struct {
	Kafka      *kgo.Client
	ClickHouse driver.Conn
	Control    WriterControlStore
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

func (writer *Writer) writeMetrics(ctx context.Context, record *kgo.Record, identity TrustedIdentity, request *collectmetrics.ExportMetricsServiceRequest, retention time.Duration) (int64, error) {
	batch, err := writer.ClickHouse.PrepareBatch(ctx, `INSERT INTO argus_telemetry.metrics
		(enterprise_id, resource_id, collector_id, metric_name, unit, timestamp, value, attributes, ingest_key, kafka_topic, kafka_partition, kafka_offset, record_sequence, expires_at)`)
	if err != nil {
		return 0, err
	}
	var sequence uint32
	for _, resourceMetrics := range request.ResourceMetrics {
		for _, scope := range resourceMetrics.ScopeMetrics {
			for _, metric := range scope.Metrics {
				appendPoint := func(timestamp uint64, value float64, attrs []*commonpb.KeyValue, suffix string) error {
					sequence++
					observed := timestampValue(timestamp)
					return batch.Append(identity.EnterpriseID, identity.ResourceID, identity.CollectorID, metric.Name+suffix, metric.Unit, observed, value,
						attributesMap(attrs), ingestKey(record, sequence), record.Topic, record.Partition, record.Offset, sequence, observed.Add(retention))
				}
				switch data := metric.Data.(type) {
				case *metricspb.Metric_Gauge:
					for _, point := range data.Gauge.DataPoints {
						if err := appendPoint(point.TimeUnixNano, numberValue(point), point.Attributes, ""); err != nil {
							return 0, err
						}
					}
				case *metricspb.Metric_Sum:
					for _, point := range data.Sum.DataPoints {
						if err := appendPoint(point.TimeUnixNano, numberValue(point), point.Attributes, ""); err != nil {
							return 0, err
						}
					}
				case *metricspb.Metric_Histogram:
					for _, point := range data.Histogram.DataPoints {
						if err := appendPoint(point.TimeUnixNano, float64(point.Count), point.Attributes, ".count"); err != nil {
							return 0, err
						}
						if point.Sum != nil {
							if err := appendPoint(point.TimeUnixNano, *point.Sum, point.Attributes, ".sum"); err != nil {
								return 0, err
							}
						}
					}
				case *metricspb.Metric_ExponentialHistogram:
					for _, point := range data.ExponentialHistogram.DataPoints {
						if err := appendPoint(point.TimeUnixNano, float64(point.Count), point.Attributes, ".count"); err != nil {
							return 0, err
						}
						if point.Sum != nil {
							if err := appendPoint(point.TimeUnixNano, *point.Sum, point.Attributes, ".sum"); err != nil {
								return 0, err
							}
						}
					}
				case *metricspb.Metric_Summary:
					for _, point := range data.Summary.DataPoints {
						if err := appendPoint(point.TimeUnixNano, float64(point.Count), point.Attributes, ".count"); err != nil {
							return 0, err
						}
						if err := appendPoint(point.TimeUnixNano, point.Sum, point.Attributes, ".sum"); err != nil {
							return 0, err
						}
						for _, quantile := range point.QuantileValues {
							suffix := ".quantile." + strconv.FormatFloat(quantile.Quantile, 'g', -1, 64)
							if err := appendPoint(point.TimeUnixNano, quantile.Value, point.Attributes, suffix); err != nil {
								return 0, err
							}
						}
					}
				}
			}
		}
	}
	if sequence == 0 {
		return 0, fmt.Errorf("%w: empty metrics", errPermanentRecord)
	}
	return int64(sequence), batch.Send()
}

func (writer *Writer) writeLogs(ctx context.Context, record *kgo.Record, identity TrustedIdentity, request *collectlogs.ExportLogsServiceRequest, retention time.Duration) (int64, error) {
	batch, err := writer.ClickHouse.PrepareBatch(ctx, `INSERT INTO argus_telemetry.logs
		(enterprise_id, resource_id, collector_id, timestamp, severity, service_name, body, attributes, trace_id, span_id, ingest_key, kafka_topic, kafka_partition, kafka_offset, record_sequence, expires_at)`)
	if err != nil {
		return 0, err
	}
	var sequence uint32
	for _, resourceLogs := range request.ResourceLogs {
		serviceName := attributeString(resourceLogs.Resource.GetAttributes(), "service.name")
		for _, scope := range resourceLogs.ScopeLogs {
			for _, item := range scope.LogRecords {
				sequence++
				observed := timestampValue(item.TimeUnixNano)
				if item.TimeUnixNano == 0 {
					observed = timestampValue(item.ObservedTimeUnixNano)
				}
				if err := batch.Append(identity.EnterpriseID, identity.ResourceID, identity.CollectorID, observed, item.SeverityText, serviceName,
					anyValueString(item.Body), attributesMap(item.Attributes), hex.EncodeToString(item.TraceId), hex.EncodeToString(item.SpanId), ingestKey(record, sequence),
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
	batch, err := writer.ClickHouse.PrepareBatch(ctx, `INSERT INTO argus_telemetry.traces
		(enterprise_id, resource_id, collector_id, trace_id, span_id, parent_span_id, service_name, operation, status, start_time, duration_ns, attributes,
		ingest_key, kafka_topic, kafka_partition, kafka_offset, record_sequence, expires_at)`)
	if err != nil {
		return 0, err
	}
	var sequence uint32
	for _, resourceSpans := range request.ResourceSpans {
		serviceName := attributeString(resourceSpans.Resource.GetAttributes(), "service.name")
		for _, scope := range resourceSpans.ScopeSpans {
			for _, span := range scope.Spans {
				sequence++
				started := timestampValue(span.StartTimeUnixNano)
				duration := uint64(0)
				if span.EndTimeUnixNano >= span.StartTimeUnixNano {
					duration = span.EndTimeUnixNano - span.StartTimeUnixNano
				}
				spanStatus := strings.ToLower(span.Status.GetCode().String())
				if err := batch.Append(identity.EnterpriseID, identity.ResourceID, identity.CollectorID, hex.EncodeToString(span.TraceId), hex.EncodeToString(span.SpanId),
					hex.EncodeToString(span.ParentSpanId), serviceName, span.Name, spanStatus, started, duration, attributesMap(span.Attributes), ingestKey(record, sequence),
					record.Topic, record.Partition, record.Offset, sequence, started.Add(retention)); err != nil {
					return 0, err
				}
			}
		}
	}
	if sequence == 0 {
		return 0, fmt.Errorf("%w: empty traces", errPermanentRecord)
	}
	return int64(sequence), batch.Send()
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
