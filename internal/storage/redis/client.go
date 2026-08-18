// Package redis owns the non-authoritative Redis connection.
package redis

import (
	"context"
	"encoding/json"
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

type ConnectorRegistryEntry struct {
	GatewayInstanceID string `json:"gateway_instance_id"`
	ConnectionEpoch   int64  `json:"connection_epoch"`
}

type ConnectorDispatch struct {
	ConnectorID     string `json:"connector_id"`
	ConnectionEpoch int64  `json:"connection_epoch"`
}

type RemoteAccessTermination struct {
	SessionID    string `json:"session_id"`
	SessionFence int64  `json:"session_fence"`
	Reason       string `json:"reason"`
}

func (client *Client) SetConnectorRegistry(ctx context.Context, connectorID string, entry ConnectorRegistryEntry, ttl time.Duration) error {
	if client == nil || client.Raw == nil {
		return errors.New("connector registry: Redis client is unavailable")
	}
	value, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return client.Raw.Set(ctx, "argus:connector:registry:"+connectorID, value, ttl).Err()
}

func (client *Client) DeleteConnectorRegistry(ctx context.Context, connectorID string) error {
	if client == nil || client.Raw == nil {
		return nil
	}
	return client.Raw.Del(ctx, "argus:connector:registry:"+connectorID).Err()
}

func (client *Client) GetConnectorRegistry(ctx context.Context, connectorID string) (ConnectorRegistryEntry, error) {
	if client == nil || client.Raw == nil {
		return ConnectorRegistryEntry{}, errors.New("connector registry: Redis client is unavailable")
	}
	value, err := client.Raw.Get(ctx, "argus:connector:registry:"+connectorID).Bytes()
	if err != nil {
		return ConnectorRegistryEntry{}, err
	}
	var entry ConnectorRegistryEntry
	if err := json.Unmarshal(value, &entry); err != nil || entry.GatewayInstanceID == "" || entry.ConnectionEpoch < 1 {
		return ConnectorRegistryEntry{}, errors.New("connector registry: invalid entry")
	}
	return entry, nil
}

func (client *Client) PublishConnectorDispatch(ctx context.Context, gatewayInstanceID string, dispatch ConnectorDispatch) error {
	if client == nil || client.Raw == nil {
		return errors.New("connector dispatch: Redis client is unavailable")
	}
	value, err := json.Marshal(dispatch)
	if err != nil {
		return err
	}
	return client.Raw.Publish(ctx, "argus:connector:dispatch:"+gatewayInstanceID, value).Err()
}

func (client *Client) SubscribeConnectorDispatch(ctx context.Context, gatewayInstanceID string) (<-chan ConnectorDispatch, func() error, error) {
	if client == nil || client.Raw == nil {
		return nil, nil, errors.New("connector dispatch: Redis client is unavailable")
	}
	pubsub := client.Raw.Subscribe(ctx, "argus:connector:dispatch:"+gatewayInstanceID)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, nil, err
	}
	output := make(chan ConnectorDispatch, 64)
	go func() {
		defer close(output)
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-pubsub.Channel():
				if !ok {
					return
				}
				var dispatch ConnectorDispatch
				if json.Unmarshal([]byte(message.Payload), &dispatch) != nil || dispatch.ConnectorID == "" || dispatch.ConnectionEpoch < 1 {
					continue
				}
				select {
				case output <- dispatch:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return output, pubsub.Close, nil
}

func (client *Client) PublishRemoteAccessTermination(ctx context.Context, gatewayInstanceID string, termination RemoteAccessTermination) error {
	if client == nil || client.Raw == nil {
		return errors.New("remote access termination: Redis client is unavailable")
	}
	value, err := json.Marshal(termination)
	if err != nil {
		return err
	}
	return client.Raw.Publish(ctx, "argus:remote-access:terminate:"+gatewayInstanceID, value).Err()
}

func (client *Client) SubscribeRemoteAccessTermination(ctx context.Context, gatewayInstanceID string) (<-chan RemoteAccessTermination, func() error, error) {
	if client == nil || client.Raw == nil {
		return nil, nil, errors.New("remote access termination: Redis client is unavailable")
	}
	pubsub := client.Raw.Subscribe(ctx, "argus:remote-access:terminate:"+gatewayInstanceID)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, nil, err
	}
	output := make(chan RemoteAccessTermination, 64)
	go func() {
		defer close(output)
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-pubsub.Channel():
				if !ok {
					return
				}
				var termination RemoteAccessTermination
				if json.Unmarshal([]byte(message.Payload), &termination) != nil || termination.SessionID == "" || termination.SessionFence < 1 {
					continue
				}
				select {
				case output <- termination:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return output, pubsub.Close, nil
}
