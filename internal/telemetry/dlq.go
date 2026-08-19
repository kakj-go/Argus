package telemetry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const dlqReplayTimeout = 30 * time.Second

func ReplayDLQ(ctx context.Context, store *postgres.Store, brokers []string, username, password string, recordID uuid.UUID) error {
	if store == nil || len(brokers) == 0 || username == "" || password == "" || recordID == uuid.Nil {
		return ErrUnavailable
	}
	record, err := store.Queries.ClaimTelemetryDLQReplay(ctx, recordID)
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		if failed {
			_ = store.Queries.MarkTelemetryDLQReplayFailed(context.Background(), record.ID)
		}
	}()

	replayCtx, cancel := context.WithTimeout(ctx, dlqReplayTimeout)
	defer cancel()
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.SASL(scram.Auth{User: username, Pass: password}.AsSha512Mechanism()),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			record.DlqTopic: {record.DlqPartition: kgo.NewOffset().At(record.DlqOffset)},
		}),
	)
	if err != nil {
		return err
	}
	defer client.Close()

	source, err := findDLQRecord(replayCtx, client, record)
	if err != nil {
		return err
	}
	if err = validateDLQSource(record, source); err != nil {
		return err
	}
	replayed := &kgo.Record{Topic: record.Topic, Key: bytes.Clone(source.Key), Value: bytes.Clone(source.Value), Headers: replayHeaders(source.Headers)}
	if err = client.ProduceSync(replayCtx, replayed).FirstErr(); err != nil {
		return err
	}
	if err = store.InTx(ctx, func(q *db.Queries) error {
		if err := q.MarkTelemetryDLQReplayed(ctx, record.ID); err != nil {
			return err
		}
		if err := audit.InitializeChain(ctx, q, "platform", uuid.NullUUID{}); err != nil {
			return err
		}
		_, err := audit.Append(ctx, q, audit.Entry{Domain: "platform", ActorType: "system", ActorID: "argusctl",
			Action: "telemetry.dlq.replay", ResourceType: "telemetry_dlq_record", ResourceID: record.ID.String(), Result: "success",
			Details: map[string]any{"summary": "telemetry DLQ record replayed", "status": "replayed"}})
		return err
	}); err != nil {
		return err
	}
	failed = false
	return nil
}

func findDLQRecord(ctx context.Context, client *kgo.Client, record db.TelemetryDlqRecord) (*kgo.Record, error) {
	for ctx.Err() == nil {
		fetches := client.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			return nil, err
		}
		for iterator := fetches.RecordIter(); !iterator.Done(); {
			candidate := iterator.Next()
			if candidate.Topic == record.DlqTopic && candidate.Partition == record.DlqPartition && candidate.Offset == record.DlqOffset {
				return candidate, nil
			}
			if candidate.Topic == record.DlqTopic && candidate.Partition == record.DlqPartition && candidate.Offset > record.DlqOffset {
				return nil, errors.New("telemetry DLQ record is no longer available")
			}
		}
	}
	return nil, errors.New("telemetry DLQ replay timed out")
}

func validateDLQSource(record db.TelemetryDlqRecord, source *kgo.Record) error {
	hash := sha256.Sum256(source.Value)
	if !bytes.Equal(hash[:], record.RecordHash) {
		return errors.New("telemetry DLQ payload hash mismatch")
	}
	headers := map[string]string{}
	for _, header := range source.Headers {
		headers[header.Key] = string(header.Value)
	}
	if headers["argus-source-topic"] != record.Topic || headers["argus-source-partition"] != strconv.Itoa(int(record.Partition)) ||
		headers["argus-source-offset"] != strconv.FormatInt(record.SourceOffset, 10) {
		return fmt.Errorf("telemetry DLQ source coordinates mismatch")
	}
	return nil
}

func replayHeaders(values []kgo.RecordHeader) []kgo.RecordHeader {
	result := make([]kgo.RecordHeader, 0, len(values))
	for _, header := range values {
		switch header.Key {
		case "argus-source-topic", "argus-source-partition", "argus-source-offset", "argus-error":
			continue
		default:
			result = append(result, kgo.RecordHeader{Key: header.Key, Value: bytes.Clone(header.Value)})
		}
	}
	return result
}
