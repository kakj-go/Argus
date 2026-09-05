package connector

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/tunnelaudit"
	"github.com/kakj-go/Argus/internal/tunnelruntime"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultConnectorTelemetryTunnelLimit = 64

type cachedConnectorTunnelLease struct {
	lease db.CredentialLease
	epoch int64
	fence int64
}

func connectorTunnelOwner(connectorID uuid.UUID, epoch int64) string {
	return fmt.Sprintf("connector:%s:%d", connectorID, epoch)
}

func (gateway Gateway) telemetryTunnelLimit() int {
	if gateway.TelemetryTunnelLimit <= 0 || gateway.TelemetryTunnelLimit > defaultConnectorTelemetryTunnelLimit {
		return defaultConnectorTelemetryTunnelLimit
	}
	return gateway.TelemetryTunnelLimit
}

func (gateway Gateway) desiredTelemetryTunnels(
	ctx context.Context,
	identity TrustedIdentity,
	connectionEpoch int64,
	cache map[uuid.UUID]cachedConnectorTunnelLease,
) (*connectorv1.TelemetryTunnelDesiredSet, error) {
	owner := connectorTunnelOwner(identity.ConnectorID, connectionEpoch)
	_, _ = gateway.Service.Store.Queries.RecoverExpiredTelemetryTunnels(ctx)
	_, _ = gateway.Service.Store.Queries.FenceInvalidConnectorTelemetryTunnels(ctx,
		db.FenceInvalidConnectorTelemetryTunnelsParams{
			ConnectorID:  uuid.NullUUID{UUID: identity.ConnectorID, Valid: true},
			EnterpriseID: identity.EnterpriseID,
		})
	owned, err := gateway.Service.Store.Queries.ListOwnedConnectorTelemetryTunnels(ctx,
		db.ListOwnedConnectorTelemetryTunnelsParams{
			ConnectorID: uuid.NullUUID{UUID: identity.ConnectorID, Valid: true}, EnterpriseID: identity.EnterpriseID,
			LeaseOwner: owner, OwnerConnectionEpoch: connectionEpoch,
		})
	if err != nil {
		return nil, err
	}
	capacity := gateway.telemetryTunnelLimit() - len(owned)
	if capacity > 0 {
		claimed, claimErr := gateway.Service.Store.Queries.ClaimConnectorTelemetryTunnels(ctx,
			db.ClaimConnectorTelemetryTunnelsParams{
				ConnectorID: uuid.NullUUID{UUID: identity.ConnectorID, Valid: true}, EnterpriseID: identity.EnterpriseID,
				ConnectionEpoch: connectionEpoch, LeaseOwner: owner, Limit: int32(capacity),
			})
		if claimErr != nil {
			return nil, claimErr
		}
		for _, tunnel := range claimed {
			gateway.auditTelemetryTunnel(ctx, identity, tunnelaudit.Claim, tunnel, tunnel.Status, "")
		}
		owned, err = gateway.Service.Store.Queries.ListOwnedConnectorTelemetryTunnels(ctx,
			db.ListOwnedConnectorTelemetryTunnelsParams{
				ConnectorID: uuid.NullUUID{UUID: identity.ConnectorID, Valid: true}, EnterpriseID: identity.EnterpriseID,
				LeaseOwner: owner, OwnerConnectionEpoch: connectionEpoch,
			})
		if err != nil {
			return nil, err
		}
	}
	active := make(map[uuid.UUID]struct{}, len(owned))
	result := &connectorv1.TelemetryTunnelDesiredSet{FullSnapshot: true, Tunnels: make([]*connectorv1.TelemetryTunnelDesired, 0, len(owned))}
	for _, tunnel := range owned {
		active[tunnel.ID] = struct{}{}
		cached, ok := cache[tunnel.ID]
		if !ok || cached.epoch != tunnel.Epoch || cached.fence != tunnel.Fence ||
			!cached.lease.ExpiresAt.Valid || !time.Now().UTC().Before(cached.lease.ExpiresAt.Time) {
			if ok {
				_ = gateway.Service.Store.Queries.RevokeCredentialLease(ctx, db.RevokeCredentialLeaseParams{
					ID: cached.lease.ID, EnterpriseID: identity.EnterpriseID})
			}
			operationRef := fmt.Sprintf("telemetry_tunnel:%s:%d:%d", tunnel.ID, tunnel.Epoch, tunnel.Fence)
			err = gateway.Service.Store.InTx(secret.WithActorType(ctx, "connector"), func(q *db.Queries) error {
				cached.lease, err = gateway.Credentials.PrepareLeaseWithQueries(ctx, q, identity.ConnectorID.String(),
					identity.EnterpriseID, secret.LeaseRequest{
						CredentialID: tunnel.CredentialID, OperationRef: operationRef,
						TargetResourceType: "telemetry_tunnel", TargetResourceID: tunnel.ID,
						RecipientType: "connector", RecipientID: identity.ConnectorID.String(),
						Protocol: "ssh", TTL: tunnelruntime.LeaseDuration,
					})
				return err
			})
			if err != nil {
				return nil, err
			}
			cached.epoch, cached.fence = tunnel.Epoch, tunnel.Fence
			cache[tunnel.ID] = cached
		}
		identityTarget, targetErr := gateway.telemetryTunnelIdentityTarget(ctx, identity.EnterpriseID, tunnel)
		if targetErr != nil {
			return nil, targetErr
		}
		result.Tunnels = append(result.Tunnels, &connectorv1.TelemetryTunnelDesired{
			TunnelId: tunnel.ID.String(), CollectorId: tunnel.CollectorID.String(),
			Epoch: uint64(tunnel.Epoch), Fence: uint64(tunnel.Fence),
			TargetAddress: tunnel.TargetAddress, TargetPort: uint32(tunnel.TargetPort),
			TargetUsername: tunnel.TargetUsername, PinnedHostKey: tunnel.PinnedHostKey,
			LoopbackPort: uint32(tunnel.LoopbackPort), ForwardTarget: gateway.telemetryTunnelForwardTarget(),
			IdentityLoopbackPort: uint32(tunnel.LoopbackPort + 1), IdentityForwardTarget: identityTarget,
			CredentialLeaseId: cached.lease.ID.String(), LeaseExpiresAt: timestamppb.New(cached.lease.ExpiresAt.Time),
		})
	}
	for tunnelID, cached := range cache {
		if _, ok := active[tunnelID]; ok {
			continue
		}
		_ = gateway.Service.Store.Queries.RevokeCredentialLease(ctx, db.RevokeCredentialLeaseParams{
			ID: cached.lease.ID, EnterpriseID: identity.EnterpriseID})
		delete(cache, tunnelID)
	}
	return result, nil
}

func (gateway Gateway) telemetryTunnelIdentityTarget(ctx context.Context, enterpriseID uuid.UUID, tunnel db.TelemetryTunnel) (string, error) {
	route, err := gateway.Service.Store.Queries.GetTelemetryRouteByCollector(ctx, db.GetTelemetryRouteByCollectorParams{
		CollectorID: tunnel.CollectorID, EnterpriseID: enterpriseID})
	if err != nil {
		return "", err
	}
	if route.GatewayCollectorID.Valid {
		upstream, upstreamErr := gateway.Service.Store.Queries.GetTelemetryRouteByCollector(ctx, db.GetTelemetryRouteByCollectorParams{
			CollectorID: route.GatewayCollectorID.UUID, EnterpriseID: enterpriseID})
		if upstreamErr != nil {
			return "", upstreamErr
		}
		if upstream.Transport != "direct" && upstream.LoopbackPort.Valid && upstream.LoopbackPort.Int32 > 0 && upstream.LoopbackPort.Int32 < 65535 {
			return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(upstream.LoopbackPort.Int32+1))), nil
		}
	}
	return identityEndpointAddress(gateway.TelemetryTunnelIdentityForwardTarget)
}

func identityEndpointAddress(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		port := parsed.Port()
		if port == "" {
			port = "443"
		}
		return net.JoinHostPort(parsed.Hostname(), port), nil
	}
	if host, port, splitErr := net.SplitHostPort(trimmed); splitErr == nil && host != "" && port != "" {
		return net.JoinHostPort(host, port), nil
	}
	return "", fmt.Errorf("telemetry identity forward target is invalid")
}

func (gateway Gateway) telemetryTunnelForwardTarget() string {
	if strings.TrimSpace(gateway.TelemetryTunnelForwardTarget) == "" {
		return "127.0.0.1:4317"
	}
	return gateway.TelemetryTunnelForwardTarget
}

func (gateway Gateway) applyTelemetryTunnelStatuses(
	ctx context.Context,
	identity TrustedIdentity,
	connectionEpoch int64,
	statuses []*connectorv1.TelemetryTunnelStatus,
) error {
	if len(statuses) > gateway.telemetryTunnelLimit() {
		return ErrCommandState
	}
	owner := connectorTunnelOwner(identity.ConnectorID, connectionEpoch)
	for _, status := range statuses {
		tunnelID, err := uuid.Parse(status.GetTunnelId())
		if err != nil || status.GetEpoch() > math.MaxInt64 || status.GetFence() > math.MaxInt64 ||
			status.GetBytesRelayed() > math.MaxInt64 || status.GetThrottledEvents() > math.MaxInt64 {
			return ErrCommandState
		}
		state := status.GetStatus()
		if state != "establishing" && state != "established" && state != "degraded" && state != "down" {
			return ErrCommandState
		}
		reason := strings.TrimSpace(status.GetDropReason())
		if len(reason) > 128 {
			return ErrCommandState
		}
		before, beforeErr := gateway.Service.Store.Queries.GetTelemetryTunnel(ctx, db.GetTelemetryTunnelParams{
			ID: tunnelID, EnterpriseID: identity.EnterpriseID,
		})
		if beforeErr != nil {
			return beforeErr
		}
		rows, heartbeatErr := gateway.Service.Store.Queries.HeartbeatConnectorTelemetryTunnel(ctx,
			db.HeartbeatConnectorTelemetryTunnelParams{
				ID: tunnelID, EnterpriseID: identity.EnterpriseID,
				ConnectorID: uuid.NullUUID{UUID: identity.ConnectorID, Valid: true},
				Epoch:       int64(status.GetEpoch()), Fence: int64(status.GetFence()), LeaseOwner: owner,
				Status: state, BytesRelayed: int64(status.GetBytesRelayed()),
				ThrottledEvents: int64(status.GetThrottledEvents()), LastDropReason: reason,
			})
		if heartbeatErr != nil {
			return heartbeatErr
		}
		if rows != 1 {
			return ErrConnectorFenced
		}
		if before.Status != state {
			before.Status, before.LastDropReason = state, reason
			event := tunnelaudit.Disconnect
			if state == "established" {
				event = tunnelaudit.Establish
			}
			if state == "established" || state == "degraded" || state == "down" {
				gateway.auditTelemetryTunnel(ctx, identity, event, before, state, reason)
			}
		}
	}
	return nil
}

func (gateway Gateway) releaseTelemetryTunnels(
	identity TrustedIdentity,
	connectionEpoch int64,
	cache map[uuid.UUID]cachedConnectorTunnelLease,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	owner := connectorTunnelOwner(identity.ConnectorID, connectionEpoch)
	owned, listErr := gateway.Service.Store.Queries.ListOwnedConnectorTelemetryTunnels(ctx,
		db.ListOwnedConnectorTelemetryTunnelsParams{
			ConnectorID: uuid.NullUUID{UUID: identity.ConnectorID, Valid: true}, EnterpriseID: identity.EnterpriseID,
			LeaseOwner: owner, OwnerConnectionEpoch: connectionEpoch,
		})
	rows, dropErr := gateway.Service.Store.Queries.DropOwnedConnectorTelemetryTunnels(ctx,
		db.DropOwnedConnectorTelemetryTunnelsParams{
			ConnectorID: uuid.NullUUID{UUID: identity.ConnectorID, Valid: true}, EnterpriseID: identity.EnterpriseID,
			LeaseOwner: owner, LastDropReason: "connector_session_closed",
		})
	if listErr == nil && dropErr == nil && rows > 0 {
		for _, tunnel := range owned {
			tunnel.Status, tunnel.LastDropReason = "down", "connector_session_closed"
			gateway.auditTelemetryTunnel(ctx, identity, tunnelaudit.Disconnect, tunnel, tunnel.Status, tunnel.LastDropReason)
		}
	}
	for _, cached := range cache {
		_ = gateway.Service.Store.Queries.RevokeCredentialLease(ctx, db.RevokeCredentialLeaseParams{
			ID: cached.lease.ID, EnterpriseID: identity.EnterpriseID})
	}
}

func (gateway Gateway) auditTelemetryTunnel(ctx context.Context, identity TrustedIdentity, event tunnelaudit.Event, tunnel db.TelemetryTunnel, status, reason string) {
	actorID := "connector-gateway:" + identity.ConnectorID.String()
	if err := tunnelaudit.Telemetry(ctx, gateway.Service.Store, actorID, event, tunnel, status, reason); err != nil && ctx.Err() == nil {
		slog.Error("Connector telemetry tunnel audit append failed", "tunnel_id", tunnel.ID, "event", event, "error", err)
	}
}
