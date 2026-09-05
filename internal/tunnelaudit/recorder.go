// Package tunnelaudit persists security-relevant tunnel lifecycle transitions.
// It deliberately stores only stable identifiers and bounded runtime metadata;
// credentials, target addresses, host keys, and lease material never enter audit.
package tunnelaudit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type Event string

const (
	Claim      Event = "claim"
	Establish  Event = "establish"
	Disconnect Event = "disconnect"
)

func Telemetry(ctx context.Context, store *postgres.Store, actorID string, event Event, tunnel db.TelemetryTunnel, status, reason string) error {
	details := map[string]any{
		"tunnel_id": tunnel.ID, "collector_id": tunnel.CollectorID, "host_id": tunnel.HostID,
		"epoch": tunnel.Epoch, "fence": tunnel.Fence, "lease_owner": tunnel.LeaseOwner,
		"initiator": tunnel.Initiator, "status": status, "drop_reason": reason,
		"bytes_relayed": tunnel.BytesRelayed, "throttled_events": tunnel.ThrottledEvents,
		"connection_epoch": tunnel.OwnerConnectionEpoch,
	}
	if tunnel.ConnectorID.Valid {
		details["connector_id"] = tunnel.ConnectorID.UUID
	}
	return appendEntry(ctx, store, audit.Entry{
		Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: tunnel.EnterpriseID, Valid: true},
		ActorType: "system", ActorID: normalizedActorID(actorID), Action: "telemetry.tunnel." + string(event),
		ResourceType: "telemetry_tunnel", ResourceID: tunnel.ID.String(), Result: "success", Details: details,
	})
}

func Control(ctx context.Context, store *postgres.Store, actorID string, event Event, tunnel db.ConnectorControlTunnel, status, reason string) error {
	return appendEntry(ctx, store, audit.Entry{
		Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: tunnel.EnterpriseID, Valid: true},
		ActorType: "system", ActorID: normalizedActorID(actorID), Action: "connector.control_tunnel." + string(event),
		ResourceType: "connector_control_tunnel", ResourceID: tunnel.ID.String(), Result: "success",
		Details: map[string]any{
			"tunnel_id": tunnel.ID, "connector_id": tunnel.ConnectorID, "bastion_scope_id": tunnel.BastionScopeID,
			"host_id": tunnel.HostID, "epoch": tunnel.Epoch, "fence": tunnel.Fence,
			"lease_owner": tunnel.LeaseOwner, "status": status, "drop_reason": reason,
			"bytes_relayed": tunnel.BytesRelayed, "throttled_events": tunnel.ThrottledEvents,
		},
	})
}

func appendEntry(ctx context.Context, store *postgres.Store, entry audit.Entry) error {
	if store == nil {
		return errors.New("tunnel audit store unavailable")
	}
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = store.InReadCommittedTx(ctx, func(queries *db.Queries) error {
			if initializeErr := audit.InitializeChain(ctx, queries, entry.Domain, entry.EnterpriseID); initializeErr != nil {
				return initializeErr
			}
			_, appendErr := audit.Append(ctx, queries, entry)
			return appendErr
		})
		if err == nil || !retryable(err) || attempt == 4 {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("append tunnel audit: %w", err)
}

func retryable(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && (pgError.Code == "40001" || pgError.Code == "40P01")
}

func normalizedActorID(value string) string {
	if value == "" {
		return "argus-tunnel-runtime"
	}
	return value
}
