package directexecutor

import (
	"context"
	"log/slog"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/tunnelaudit"
)

func (executor *Executor) auditTelemetryTunnel(ctx context.Context, event tunnelaudit.Event, tunnel db.TelemetryTunnel, status, reason string) {
	if err := tunnelaudit.Telemetry(ctx, executor.Store, executor.InstanceID, event, tunnel, status, reason); err != nil && ctx.Err() == nil {
		slog.Error("telemetry tunnel audit append failed", "tunnel_id", tunnel.ID, "event", event, "error", err)
	}
}

func (executor *Executor) markTelemetryTunnelDropped(ctx context.Context, tunnel db.TelemetryTunnel, status, reason string) int64 {
	rows, err := executor.Store.Queries.MarkTelemetryTunnelDropped(ctx, db.MarkTelemetryTunnelDroppedParams{
		ID: tunnel.ID, Fence: tunnel.Fence, LeaseOwner: tunnel.LeaseOwner,
		Status: status, LastDropReason: reason,
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("telemetry tunnel drop persistence failed", "tunnel_id", tunnel.ID, "error", err)
		}
		return 0
	}
	if rows > 0 {
		tunnel.Status, tunnel.LastDropReason = status, reason
		executor.auditTelemetryTunnel(ctx, tunnelaudit.Disconnect, tunnel, status, reason)
	}
	return rows
}

func (executor *Executor) auditConnectorControlTunnel(ctx context.Context, event tunnelaudit.Event, tunnel db.ConnectorControlTunnel, status, reason string) {
	if err := tunnelaudit.Control(ctx, executor.Store, executor.InstanceID, event, tunnel, status, reason); err != nil && ctx.Err() == nil {
		slog.Error("connector control tunnel audit append failed", "tunnel_id", tunnel.ID, "event", event, "error", err)
	}
}
