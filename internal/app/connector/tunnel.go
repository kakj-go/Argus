package connector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/tunnelruntime"
)

const connectorTelemetryTunnelLimit = 64

type memberTunnel struct {
	desired    *connectorv1.TelemetryTunnelDesired
	credential []byte
	cancel     context.CancelFunc
	counters   tunnelruntime.Counters

	mu               sync.Mutex
	status           string
	dropReason       string
	client           *ssh.Client
	listener         net.Listener
	identityListener net.Listener
	active           bool
}

type memberTunnelSupervisor struct {
	mu      sync.Mutex
	tunnels map[string]*memberTunnel
	limit   int
	limiter *rate.Limiter
}

func newMemberTunnelSupervisor(limit int, bytesPerSecond int64) *memberTunnelSupervisor {
	if limit <= 0 || limit > connectorTelemetryTunnelLimit {
		limit = connectorTelemetryTunnelLimit
	}
	return &memberTunnelSupervisor{tunnels: make(map[string]*memberTunnel), limit: limit,
		limiter: tunnelruntime.NewLimiter(bytesPerSecond)}
}

func (supervisor *memberTunnelSupervisor) Has(id string, epoch, fence uint64) bool {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	entry := supervisor.tunnels[id]
	return entry != nil && entry.desired.GetEpoch() == epoch && entry.desired.GetFence() == fence
}

func (supervisor *memberTunnelSupervisor) Apply(parent context.Context, desired *connectorv1.TelemetryTunnelDesired, credential []byte) error {
	if err := validateTunnelDesired(desired); err != nil || len(credential) == 0 {
		return errors.New("invalid telemetry tunnel desired state")
	}
	copyDesired := proto.Clone(desired).(*connectorv1.TelemetryTunnelDesired)
	copyCredential := append([]byte(nil), credential...)
	ctx, cancel := context.WithCancel(parent)
	entry := &memberTunnel{desired: copyDesired, credential: copyCredential, cancel: cancel, status: "establishing"}

	supervisor.mu.Lock()
	previous := supervisor.tunnels[desired.GetTunnelId()]
	if previous == nil && len(supervisor.tunnels) >= supervisor.limit {
		supervisor.mu.Unlock()
		cancel()
		clear(copyCredential)
		return errors.New("telemetry tunnel quota exceeded")
	}
	supervisor.tunnels[desired.GetTunnelId()] = entry
	supervisor.mu.Unlock()
	if previous != nil {
		supervisor.stop(previous)
	}
	go supervisor.run(ctx, entry)
	return nil
}

func (supervisor *memberTunnelSupervisor) ReconcileSnapshot(desired []*connectorv1.TelemetryTunnelDesired) {
	keep := make(map[string]struct{}, len(desired))
	for _, item := range desired {
		if item != nil {
			keep[item.GetTunnelId()] = struct{}{}
		}
	}
	supervisor.mu.Lock()
	removed := make([]*memberTunnel, 0)
	for id, entry := range supervisor.tunnels {
		if _, ok := keep[id]; ok {
			continue
		}
		delete(supervisor.tunnels, id)
		removed = append(removed, entry)
	}
	supervisor.mu.Unlock()
	for _, entry := range removed {
		supervisor.stop(entry)
	}
}

func (supervisor *memberTunnelSupervisor) Snapshot() []*connectorv1.TelemetryTunnelStatus {
	supervisor.mu.Lock()
	entries := make([]*memberTunnel, 0, len(supervisor.tunnels))
	for _, entry := range supervisor.tunnels {
		entries = append(entries, entry)
	}
	supervisor.mu.Unlock()
	result := make([]*connectorv1.TelemetryTunnelStatus, 0, len(entries))
	for _, entry := range entries {
		entry.mu.Lock()
		status, reason := entry.status, entry.dropReason
		entry.mu.Unlock()
		bytes, throttled := entry.counters.Delta()
		result = append(result, &connectorv1.TelemetryTunnelStatus{
			TunnelId: entry.desired.GetTunnelId(), Epoch: entry.desired.GetEpoch(), Fence: entry.desired.GetFence(),
			Status: status, DropReason: reason, BytesRelayed: uint64(bytes), ThrottledEvents: uint64(throttled),
		})
	}
	return result
}

func (supervisor *memberTunnelSupervisor) CloseAll() {
	supervisor.mu.Lock()
	entries := make([]*memberTunnel, 0, len(supervisor.tunnels))
	for id, entry := range supervisor.tunnels {
		entries = append(entries, entry)
		delete(supervisor.tunnels, id)
	}
	supervisor.mu.Unlock()
	for _, entry := range entries {
		supervisor.stop(entry)
	}
}

func (supervisor *memberTunnelSupervisor) run(ctx context.Context, entry *memberTunnel) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		entry.setStatus("establishing", "")
		client, listener, identityListener, err := establishMemberTunnel(ctx, entry.desired, entry.credential)
		if err != nil {
			attempt++
			entry.setStatus("down", tunnelFailureReason(err))
			tunnelruntime.RecordReconnect("bastion_telemetry")
			if !waitTunnelBackoff(ctx, tunnelruntime.Backoff(attempt)) {
				return
			}
			continue
		}
		attempt = 0
		entry.mu.Lock()
		entry.client, entry.listener, entry.identityListener, entry.active = client, listener, identityListener, true
		entry.mu.Unlock()
		tunnelruntime.AddActive("bastion_telemetry", 1)
		entry.setStatus("established", "")
		relayCtx, relayCancel := context.WithCancel(ctx)
		relayDone := make(chan error, 2)
		serve := func(listener net.Listener, target string) {
			relay := tunnelruntime.Relay{Target: target, Kind: "bastion_telemetry",
				Limiter: supervisor.limiter, Counters: &entry.counters, Dialer: net.Dialer{Timeout: 10 * time.Second}}
			relayDone <- relay.Serve(relayCtx, listener)
		}
		go serve(listener, entry.desired.GetForwardTarget())
		go serve(identityListener, entry.desired.GetIdentityForwardTarget())
		keepaliveDone := make(chan error, 1)
		go func() { keepaliveDone <- tunnelruntime.Keepalive(relayCtx, client, tunnelruntime.HeartbeatInterval) }()
		reason := "connector_session_closed"
		select {
		case <-ctx.Done():
		case err = <-relayDone:
			if err != nil {
				reason = "relay_failed"
			}
		case err = <-keepaliveDone:
			if err != nil {
				reason = "keepalive_failed"
			}
		}
		relayCancel()
		entry.closeConnection()
		if ctx.Err() != nil {
			return
		}
		attempt++
		entry.setStatus("degraded", reason)
		tunnelruntime.RecordReconnect("bastion_telemetry")
		if !waitTunnelBackoff(ctx, tunnelruntime.Backoff(attempt)) {
			return
		}
	}
}

func establishMemberTunnel(ctx context.Context, desired *connectorv1.TelemetryTunnelDesired, credential []byte) (*ssh.Client, net.Listener, net.Listener, error) {
	auth, err := connectorSSHAuth(credential)
	if err != nil {
		return nil, nil, nil, err
	}
	configuration := &ssh.ClientConfig{User: desired.GetTargetUsername(), Auth: []ssh.AuthMethod{auth}, Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != desired.GetPinnedHostKey() {
				return errors.New("host key mismatch")
			}
			return nil
		}}
	address := net.JoinHostPort(desired.GetTargetAddress(), fmt.Sprint(desired.GetTargetPort()))
	connection, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, nil, err
	}
	transport, channels, requests, err := ssh.NewClientConn(connection, address, configuration)
	if err != nil {
		_ = connection.Close()
		return nil, nil, nil, err
	}
	client := ssh.NewClient(transport, channels, requests)
	listener, err := client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", desired.GetLoopbackPort()))
	if err != nil {
		_ = client.Close()
		return nil, nil, nil, err
	}
	identityListener, err := client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", desired.GetIdentityLoopbackPort()))
	if err != nil {
		_ = listener.Close()
		_ = client.Close()
		return nil, nil, nil, err
	}
	slog.Info("member telemetry tunnel established", "tunnel_id", desired.GetTunnelId(),
		"collector_id", desired.GetCollectorId(), "loopback", desired.GetLoopbackPort(),
		"identity_loopback", desired.GetIdentityLoopbackPort(), "epoch", desired.GetEpoch())
	return client, listener, identityListener, nil
}

func (supervisor *memberTunnelSupervisor) stop(entry *memberTunnel) {
	entry.cancel()
	entry.closeConnection()
	clear(entry.credential)
}

func (entry *memberTunnel) closeConnection() {
	entry.mu.Lock()
	client, listener, identityListener, active := entry.client, entry.listener, entry.identityListener, entry.active
	entry.client, entry.listener, entry.identityListener, entry.active = nil, nil, nil, false
	entry.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	if identityListener != nil {
		_ = identityListener.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	if active {
		tunnelruntime.AddActive("bastion_telemetry", -1)
	}
}

func (entry *memberTunnel) setStatus(status, reason string) {
	entry.mu.Lock()
	entry.status, entry.dropReason = status, reason
	entry.mu.Unlock()
}

func validateTunnelDesired(value *connectorv1.TelemetryTunnelDesired) error {
	if value == nil || value.GetTunnelId() == "" || value.GetCollectorId() == "" || value.GetEpoch() == 0 ||
		value.GetFence() == 0 || value.GetTargetAddress() == "" || value.GetTargetPort() == 0 ||
		value.GetTargetUsername() == "" || value.GetPinnedHostKey() == "" || value.GetLoopbackPort() == 0 ||
		value.GetLoopbackPort() >= 65535 || value.GetIdentityLoopbackPort() != value.GetLoopbackPort()+1 ||
		value.GetForwardTarget() == "" || value.GetIdentityForwardTarget() == "" || value.GetCredentialLeaseId() == "" || value.GetLeaseExpiresAt() == nil ||
		time.Now().After(value.GetLeaseExpiresAt().AsTime()) {
		return errors.New("invalid telemetry tunnel desired state")
	}
	return nil
}

func tunnelFailureReason(err error) string {
	if err == nil {
		return "connection_closed"
	}
	message := err.Error()
	switch {
	case stringsContain(message, "host key mismatch"):
		return "host_key_changed"
	case stringsContain(message, "address already in use"), stringsContain(message, "bind:"):
		return "loopback_port_conflict"
	default:
		return "ssh_connect_failed"
	}
}

func stringsContain(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}

func waitTunnelBackoff(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
