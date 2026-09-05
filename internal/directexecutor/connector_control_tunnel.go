package directexecutor

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"

	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/tunnelaudit"
	"github.com/kakj-go/Argus/internal/tunnelruntime"
)

type connectorControlTunnelEntry struct {
	tunnel            db.ConnectorControlTunnel
	credentialLeaseID uuid.UUID
	client            *ssh.Client
	enroll            net.Listener
	gateway           net.Listener
	cancel            context.CancelFunc
	counters          tunnelruntime.Counters
	closeOnce         sync.Once
	failureOnce       sync.Once
	counted           bool
}

type connectorControlTunnelSupervisor struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*connectorControlTunnelEntry
	limit   int
	limiter *rate.Limiter
}

func newConnectorControlTunnelSupervisor(limit int, bytesPerSecond int64) *connectorControlTunnelSupervisor {
	return &connectorControlTunnelSupervisor{entries: make(map[uuid.UUID]*connectorControlTunnelEntry),
		limit: limit, limiter: tunnelruntime.NewLimiter(bytesPerSecond)}
}

func (executor *Executor) runConnectorTunnelReconciler(ctx context.Context) {
	if executor.controlTunnels == nil {
		executor.controlTunnels = newConnectorControlTunnelSupervisor(executor.controlTunnelLimit(), executor.TunnelBytesPerSecond)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer executor.closeAllConnectorControlTunnels("executor_shutdown")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = executor.Store.Queries.RecoverExpiredConnectorControlTunnels(ctx)
			owned, err := executor.Store.Queries.CountOwnedConnectorControlTunnels(ctx, executor.InstanceID)
			if err != nil {
				continue
			}
			capacity := executor.controlTunnels.limit - int(owned)
			if capacity <= 0 {
				_, _ = executor.Store.Queries.MarkOverdueConnectorControlTunnelQuota(ctx)
				continue
			}
			if capacity > 8 {
				capacity = 8
			}
			tunnels, err := executor.Store.Queries.ClaimConnectorControlTunnels(ctx, db.ClaimConnectorControlTunnelsParams{
				Limit: int32(capacity), LeaseOwner: executor.InstanceID,
			})
			if err != nil {
				continue
			}
			for _, tunnel := range tunnels {
				executor.auditConnectorControlTunnel(ctx, tunnelaudit.Claim, tunnel, tunnel.Status, "")
				go executor.establishConnectorControlTunnel(ctx, tunnel)
			}
		}
	}
}

func (executor *Executor) establishConnectorControlTunnel(parent context.Context, tunnel db.ConnectorControlTunnel) {
	ctx, cancel := context.WithCancel(parent)
	credential, err := executor.Store.Queries.GetCredential(ctx, db.GetCredentialParams{
		ID: tunnel.CredentialID, EnterpriseID: tunnel.EnterpriseID})
	if err != nil || credential.Status != "active" || credential.Version != tunnel.CredentialVersion {
		cancel()
		executor.markConnectorControlTunnelDropped(context.Background(), tunnel, "down", "credential_unavailable")
		return
	}
	addresses, err := executor.Validator.Resolve(ctx, tunnel.TargetAddress)
	if err != nil || len(addresses) == 0 {
		cancel()
		executor.markConnectorControlTunnelDropped(context.Background(), tunnel, "down", "target_resolution_failed")
		return
	}
	lease, err := executor.Secrets.IssueLease(secret.WithActorType(ctx, "direct_executor"), executor.InstanceID, tunnel.EnterpriseID, secret.LeaseRequest{
		CredentialID: tunnel.CredentialID, OperationRef: "connector_control_tunnel:" + tunnel.ID.String(), TargetResourceType: "host",
		TargetResourceID: tunnel.HostID, RecipientType: "direct_executor", RecipientID: executor.InstanceID,
		Protocol: "ssh", TTL: tunnelruntime.LeaseDuration,
	})
	if err != nil {
		cancel()
		executor.markConnectorControlTunnelDropped(context.Background(), tunnel, "down", "credential_unavailable")
		return
	}
	defer clear(lease.Value)
	revokeLease := func() {
		_ = executor.Store.Queries.RevokeCredentialLease(context.Background(), db.RevokeCredentialLeaseParams{
			ID: lease.Lease.ID, EnterpriseID: tunnel.EnterpriseID})
	}
	client, err := executor.dialPinnedSSH(ctx, addresses[0], tunnel.TargetPort, tunnel.TargetUsername, tunnel.PinnedHostKey, lease.Value)
	if err != nil {
		cancel()
		revokeLease()
		reason := "ssh_connect_failed"
		if errors.Is(err, resource.ErrDirectTargetDenied) || errors.Is(err, errHostKeyMismatch) {
			reason = "host_key_changed"
		}
		executor.markConnectorControlTunnelDropped(context.Background(), tunnel, "down", reason)
		return
	}
	if err = executor.Validator.Revalidate(ctx, tunnel.TargetAddress, addresses); err != nil {
		cancel()
		_ = client.Close()
		revokeLease()
		executor.markConnectorControlTunnelDropped(context.Background(), tunnel, "down", "target_revalidation_failed")
		return
	}
	enroll, err := client.Listen("tcp", "127.0.0.1:8443")
	if err != nil {
		cancel()
		_ = client.Close()
		revokeLease()
		executor.markConnectorControlTunnelDropped(context.Background(), tunnel, "degraded", "enroll_forward_failed")
		return
	}
	gateway, err := client.Listen("tcp", "127.0.0.1:9443")
	if err != nil {
		cancel()
		_ = enroll.Close()
		_ = client.Close()
		revokeLease()
		executor.markConnectorControlTunnelDropped(context.Background(), tunnel, "degraded", "gateway_forward_failed")
		return
	}
	current, err := executor.Store.Queries.MarkConnectorControlTunnelEstablished(ctx, db.MarkConnectorControlTunnelEstablishedParams{
		ID: tunnel.ID, EnterpriseID: tunnel.EnterpriseID, Fence: tunnel.Fence, LeaseOwner: executor.InstanceID,
	})
	if err != nil {
		cancel()
		_ = enroll.Close()
		_ = gateway.Close()
		_ = client.Close()
		revokeLease()
		return
	}
	entry := &connectorControlTunnelEntry{tunnel: current, credentialLeaseID: lease.Lease.ID,
		client: client, enroll: enroll, gateway: gateway, cancel: cancel, counted: true}
	if previous := executor.controlTunnels.put(entry); previous != nil {
		executor.closeConnectorControlTunnel(previous)
	}
	tunnelruntime.AddActive("control", 1)
	if current.Fence > 1 {
		tunnelruntime.RecordTakeover("control")
	}
	executor.auditConnectorControlTunnel(ctx, tunnelaudit.Establish, current, current.Status, "")
	go executor.serveConnectorControlListener(ctx, entry, enroll, current.EnrollForwardTarget)
	go executor.serveConnectorControlListener(ctx, entry, gateway, current.GatewayForwardTarget)
	executor.superviseConnectorControlTunnel(ctx, entry)
}

func (executor *Executor) serveConnectorControlListener(ctx context.Context, entry *connectorControlTunnelEntry, listener net.Listener, target string) {
	relay := tunnelruntime.Relay{Target: target, Kind: "control", Limiter: executor.controlTunnels.limiter,
		Counters: &entry.counters, Dialer: net.Dialer{Timeout: 10 * time.Second}}
	if err := relay.Serve(ctx, listener); err != nil && ctx.Err() == nil {
		executor.failConnectorControlTunnel(entry, "relay_failed")
	}
}

func (executor *Executor) superviseConnectorControlTunnel(ctx context.Context, entry *connectorControlTunnelEntry) {
	ticker := time.NewTicker(tunnelruntime.HeartbeatInterval)
	defer ticker.Stop()
	defer executor.controlTunnels.remove(entry)
	defer executor.closeConnectorControlTunnel(entry)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, _, err := entry.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				tunnelruntime.RecordReconnect("control")
				executor.failConnectorControlTunnel(entry, "keepalive_failed")
				return
			}
			credential, err := executor.Store.Queries.GetCredential(ctx, db.GetCredentialParams{
				ID: entry.tunnel.CredentialID, EnterpriseID: entry.tunnel.EnterpriseID})
			if err != nil || credential.Status != "active" || credential.Version != entry.tunnel.CredentialVersion {
				executor.failConnectorControlTunnel(entry, "credential_revoked")
				return
			}
			if _, err = executor.Secrets.RenewLease(ctx, entry.tunnel.EnterpriseID, entry.credentialLeaseID,
				"direct_executor", executor.InstanceID, entry.tunnel.CredentialVersion, tunnelruntime.LeaseDuration); err != nil {
				executor.failConnectorControlTunnel(entry, "credential_lease_renewal_failed")
				return
			}
			bytes, throttled := entry.counters.Delta()
			rows, _ := executor.Store.Queries.HeartbeatConnectorControlTunnel(ctx, db.HeartbeatConnectorControlTunnelParams{
				ID: entry.tunnel.ID, EnterpriseID: entry.tunnel.EnterpriseID, Fence: entry.tunnel.Fence,
				LeaseOwner: executor.InstanceID, BytesRelayed: bytes, ThrottledEvents: throttled,
			})
			if rows == 0 {
				return
			}
		}
	}
}

func (executor *Executor) failConnectorControlTunnel(entry *connectorControlTunnelEntry, reason string) {
	entry.failureOnce.Do(func() {
		bytes, throttled := entry.counters.Delta()
		_, _ = executor.Store.Queries.HeartbeatConnectorControlTunnel(context.Background(), db.HeartbeatConnectorControlTunnelParams{
			ID: entry.tunnel.ID, EnterpriseID: entry.tunnel.EnterpriseID, Fence: entry.tunnel.Fence,
			LeaseOwner: executor.InstanceID, BytesRelayed: bytes, ThrottledEvents: throttled,
		})
		executor.markConnectorControlTunnelDropped(context.Background(), entry.tunnel, "degraded", reason)
		executor.closeConnectorControlTunnel(entry)
	})
}

func (executor *Executor) markConnectorControlTunnelDropped(ctx context.Context, tunnel db.ConnectorControlTunnel, status, reason string) {
	rows, err := executor.Store.Queries.MarkConnectorControlTunnelDropped(ctx, db.MarkConnectorControlTunnelDroppedParams{
		ID: tunnel.ID, EnterpriseID: tunnel.EnterpriseID, Fence: tunnel.Fence, LeaseOwner: executor.InstanceID,
		Status: status, LastDropReason: reason,
	})
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("connector control tunnel drop persistence failed", "tunnel_id", tunnel.ID, "error", err)
		}
		return
	}
	if rows > 0 {
		tunnel.Status, tunnel.LastDropReason = status, reason
		executor.auditConnectorControlTunnel(ctx, tunnelaudit.Disconnect, tunnel, status, reason)
	}
}

func (executor *Executor) closeConnectorControlTunnel(entry *connectorControlTunnelEntry) {
	entry.closeOnce.Do(func() {
		entry.cancel()
		_ = entry.enroll.Close()
		_ = entry.gateway.Close()
		_ = entry.client.Close()
		if entry.credentialLeaseID != uuid.Nil {
			_ = executor.Store.Queries.RevokeCredentialLease(context.Background(), db.RevokeCredentialLeaseParams{
				ID: entry.credentialLeaseID, EnterpriseID: entry.tunnel.EnterpriseID})
		}
		if entry.counted {
			tunnelruntime.AddActive("control", -1)
		}
	})
}

func (executor *Executor) closeAllConnectorControlTunnels(reason string) {
	entries := executor.controlTunnels.takeAll()
	for _, entry := range entries {
		executor.markConnectorControlTunnelDropped(context.Background(), entry.tunnel, "degraded", reason)
		executor.closeConnectorControlTunnel(entry)
	}
}

func (supervisor *connectorControlTunnelSupervisor) put(entry *connectorControlTunnelEntry) *connectorControlTunnelEntry {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	previous := supervisor.entries[entry.tunnel.ID]
	supervisor.entries[entry.tunnel.ID] = entry
	slog.Info("connector control tunnel established", "tunnel_id", entry.tunnel.ID, "epoch", entry.tunnel.Epoch)
	return previous
}

func (supervisor *connectorControlTunnelSupervisor) remove(entry *connectorControlTunnelEntry) {
	supervisor.mu.Lock()
	if supervisor.entries[entry.tunnel.ID] == entry {
		delete(supervisor.entries, entry.tunnel.ID)
	}
	supervisor.mu.Unlock()
}

func (supervisor *connectorControlTunnelSupervisor) takeAll() []*connectorControlTunnelEntry {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	entries := make([]*connectorControlTunnelEntry, 0, len(supervisor.entries))
	for id, entry := range supervisor.entries {
		entries = append(entries, entry)
		delete(supervisor.entries, id)
	}
	return entries
}
