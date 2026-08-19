package telemetry

import (
	"crypto/sha256"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestValidateDLQSourceRejectsPayloadAndCoordinateChanges(t *testing.T) {
	payload := []byte("bounded-otlp-payload")
	hash := sha256.Sum256(payload)
	record := db.TelemetryDlqRecord{Topic: "otlp-metrics", Partition: 2, SourceOffset: 41, RecordHash: hash[:]}
	source := &kgo.Record{Value: payload, Headers: []kgo.RecordHeader{
		{Key: "argus-source-topic", Value: []byte("otlp-metrics")},
		{Key: "argus-source-partition", Value: []byte("2")},
		{Key: "argus-source-offset", Value: []byte("41")},
	}}
	if err := validateDLQSource(record, source); err != nil {
		t.Fatalf("valid DLQ source rejected: %v", err)
	}

	tampered := *source
	tampered.Value = []byte("tampered")
	if err := validateDLQSource(record, &tampered); err == nil {
		t.Fatal("tampered DLQ payload accepted")
	}

	wrongCoordinates := *source
	wrongCoordinates.Headers = append([]kgo.RecordHeader(nil), source.Headers...)
	wrongCoordinates.Headers[2].Value = []byte("42")
	if err := validateDLQSource(record, &wrongCoordinates); err == nil {
		t.Fatal("mismatched DLQ source coordinates accepted")
	}
}

func TestReplayHeadersRemovesDLQMetadata(t *testing.T) {
	headers := replayHeaders([]kgo.RecordHeader{
		{Key: "content-type", Value: []byte("application/x-protobuf")},
		{Key: "argus-source-topic", Value: []byte("otlp-logs")},
		{Key: "argus-source-partition", Value: []byte("0")},
		{Key: "argus-source-offset", Value: []byte("7")},
		{Key: "argus-error", Value: []byte("OTLP_DECODE_FAILED")},
	})
	if len(headers) != 1 || headers[0].Key != "content-type" {
		t.Fatalf("unexpected replay headers: %#v", headers)
	}
	headers[0].Value[0] = 'X'
	if string(headers[0].Value) == "application/x-protobuf" {
		t.Fatal("test mutation did not change replay header copy")
	}
}
