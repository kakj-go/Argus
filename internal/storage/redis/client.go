// Package redis owns the non-authoritative Redis connection.
package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

type Client struct{ Raw *redislib.Client }

func Open(ctx context.Context, redisURL string) (*Client, error) {
	options, err := redislib.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse Redis configuration: %w", err)
	}
	client := redislib.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		// Keep the client alive so go-redis can reconnect after a transient outage.
		return &Client{Raw: client}, fmt.Errorf("ping Redis: %w", err)
	}
	return &Client{Raw: client}, nil
}

func (client *Client) Close() error {
	if client == nil || client.Raw == nil {
		return nil
	}
	return client.Raw.Close()
}

func (client *Client) Ready(ctx context.Context) error {
	if client == nil || client.Raw == nil {
		return errors.New("Redis client is unavailable")
	}
	return client.Raw.Ping(ctx).Err()
}

var publishOnceScript = redislib.NewScript(`
if redis.call('SET', KEYS[1], '1', 'NX', 'PX', ARGV[1]) then
  return redis.call('XADD', KEYS[2], '*', 'event_id', ARGV[2], 'topic', ARGV[3], 'aggregate_type', ARGV[4], 'aggregate_id', ARGV[5], 'payload', ARGV[6])
end
return ''
`)

func (client *Client) PublishOutbox(ctx context.Context, eventID, topic, aggregateType, aggregateID string, payload []byte) error {
	if client == nil || client.Raw == nil {
		return errors.New("publish outbox event: Redis client is unavailable")
	}
	dedupeKey := "argus:outbox:published:" + eventID
	streamKey := "argus:events:" + topic
	_, err := publishOnceScript.Run(ctx, client.Raw, []string{dedupeKey, streamKey},
		(7 * 24 * time.Hour).Milliseconds(), eventID, topic, aggregateType, aggregateID, payload).Result()
	if err != nil {
		return fmt.Errorf("publish outbox event: %w", err)
	}
	return nil
}
