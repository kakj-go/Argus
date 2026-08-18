package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
)

type Relay struct {
	Store        *postgres.Store
	Redis        *redisstore.Client
	Logger       *slog.Logger
	BatchSize    int32
	PollInterval time.Duration
}

func (relay Relay) Run(ctx context.Context) {
	if relay.BatchSize <= 0 {
		relay.BatchSize = 100
	}
	if relay.PollInterval <= 0 {
		relay.PollInterval = time.Second
	}
	ticker := time.NewTicker(relay.PollInterval)
	defer ticker.Stop()
	for {
		relay.flush(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (relay Relay) flush(ctx context.Context) {
	events, err := relay.Store.Queries.ClaimOutboxEvents(ctx, relay.BatchSize)
	if err != nil {
		relay.logError("claim outbox events", err)
		return
	}
	for _, event := range events {
		if err := relay.publish(ctx, event); err != nil {
			delay := time.Duration(1<<min(event.Attempts, 8)) * time.Second
			_ = relay.Store.Queries.RetryOutboxEvent(ctx, db.RetryOutboxEventParams{ID: event.ID,
				AvailableAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(delay), Valid: true}, LastError: text(err.Error())})
			relay.logError("publish outbox event", err)
			continue
		}
		if err := relay.Store.Queries.MarkOutboxPublished(ctx, event.ID); err != nil {
			relay.logError("mark outbox event published", err)
		}
	}
}

func (relay Relay) publish(ctx context.Context, event db.OutboxEvent) error {
	if err := relay.Redis.PublishOutbox(ctx, event.ID.String(), event.Topic, event.AggregateType, event.AggregateID, event.Payload); err != nil {
		return err
	}
	if event.Topic != "remote_access.session.terminate" {
		return nil
	}
	sessionID, err := uuid.Parse(event.AggregateID)
	if err != nil {
		return err
	}
	route, err := relay.Store.Queries.GetRemoteAccessRoute(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var termination redisstore.RemoteAccessTermination
	if err := json.Unmarshal(event.Payload, &termination); err != nil || termination.SessionID != sessionID.String() || termination.SessionFence < 1 {
		return errors.New("invalid remote access termination payload")
	}
	return relay.Redis.PublishRemoteAccessTermination(ctx, route.GatewayInstance, termination)
}

func (relay Relay) logError(message string, err error) {
	if relay.Logger != nil {
		relay.Logger.Error(message, "error", err)
	}
}

func text(value string) pgtype.Text { return pgtype.Text{String: value, Valid: true} }
