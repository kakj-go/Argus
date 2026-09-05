package directexecutor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"

	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/tunnelaudit"
	"github.com/kakj-go/Argus/internal/tunnelruntime"
)

// tunnelReconcileInterval 与 hostprobe 一致:周期认领 desired/down 隧道行,
// 建立 SSH remote forward(目标回环端口 → 平台内部 ingest)。
const tunnelReconcileInterval = time.Second

// 隧道断开原因(写入 last_drop_reason,安装时序门据此区分等待与失败)。
const (
	tunnelReasonForwardUnconfigured = "tunnel_forward_target_unconfigured"
	tunnelReasonPortConflict        = "loopback_port_conflict"
	tunnelReasonEstablishFailed     = "establish_failed"
	tunnelReasonCredentialRevoked   = "credential_revoked"
	tunnelReasonQuotaExceeded       = "tunnel_quota_exceeded"
)

var errHostKeyMismatch = errors.New("host key mismatch")

// isLoopbackPortConflict 识别远端 sshd 绑定回环端口失败的典型错误文本。
func isLoopbackPortConflict(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "address already in use") ||
		strings.Contains(message, "bind:") && strings.Contains(message, "127.0.0.1")
}

// activeTunnel 是本副本持有的一条已建隧道。epoch/fence 与数据库行绑定,
// 重建(认领)会使 epoch+1,旧副本据此自知失效并拆除本地连接。
type activeTunnel struct {
	id                uuid.UUID
	enterpriseID      uuid.UUID
	hostID            uuid.UUID
	collectorID       uuid.UUID
	initiator         string
	epoch             int64
	fence             int64
	leaseOwner        string
	credentialID      uuid.UUID
	credentialVersion int64
	credentialLeaseID uuid.UUID
	client            *ssh.Client
	listener          net.Listener
	identityListener  net.Listener
	cancel            context.CancelFunc
	counters          tunnelruntime.Counters
	lastHeartbeat     time.Time
	closeOnce         sync.Once
	failureOnce       sync.Once
	counted           bool
}

// tunnelSupervisor 管理本 Executor 副本的全部活动隧道。
type tunnelSupervisor struct {
	forwardTarget         string // 平台内部 ingest gRPC 端点(svc 直连)
	identityForwardTarget string // 平台内部 ingest HTTP 端点(svc 直连)
	limit                 int
	limiter               *rate.Limiter
	mu                    sync.Mutex
	tunnels               map[uuid.UUID]*activeTunnel
}

func newTunnelSupervisor(forwardTarget, identityForwardTarget string, limit int, bytesPerSecond int64) *tunnelSupervisor {
	return &tunnelSupervisor{forwardTarget: forwardTarget, identityForwardTarget: identityForwardTarget, limit: limit,
		limiter: tunnelruntime.NewLimiter(bytesPerSecond), tunnels: map[uuid.UUID]*activeTunnel{}}
}

// RunTunnelReconciler 周期收敛 desired 隧道。forwardTarget 为空时 fail closed:
// 存在 desired 隧道却未配置转发端点属于部署错误,记录并跳过(不建立半隧道)。
func (executor *Executor) RunTunnelReconciler(ctx context.Context) {
	if executor.tunnels == nil {
		executor.tunnels = newTunnelSupervisor("", "", executor.telemetryTunnelLimit(), executor.TunnelBytesPerSecond)
	}
	ticker := time.NewTicker(tunnelReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			executor.dropAllTunnels("executor_shutdown")
			return
		case <-ticker.C:
			executor.reconcileTunnels(ctx)
		}
	}
}

func (executor *Executor) dropAllTunnels(reason string) {
	executor.tunnels.mu.Lock()
	held := make([]*activeTunnel, 0, len(executor.tunnels.tunnels))
	for _, tunnel := range executor.tunnels.tunnels {
		held = append(held, tunnel)
	}
	executor.tunnels.tunnels = map[uuid.UUID]*activeTunnel{}
	executor.tunnels.mu.Unlock()
	for _, tunnel := range held {
		executor.closeTelemetryTunnel(tunnel)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		executor.persistTelemetryTunnelDrop(shutdownCtx, tunnel, reason)
		cancel()
	}
	if len(reason) > 0 {
		slog.Info("tunnel supervisor dropped all tunnels", "reason", reason)
	}
}

func (executor *Executor) reconcileTunnels(ctx context.Context) {
	_, _ = executor.Store.Queries.RecoverExpiredTelemetryTunnels(ctx)
	executor.tunnels.mu.Lock()
	capacity := executor.tunnels.limit - len(executor.tunnels.tunnels)
	executor.tunnels.mu.Unlock()
	if capacity < 0 {
		capacity = 0
	}
	if capacity == 0 {
		_, _ = executor.Store.Queries.MarkOverdueTelemetryTunnelQuota(ctx)
	}
	if capacity > 8 {
		capacity = 8
	}
	var claimed []db.TelemetryTunnel
	var err error
	if capacity > 0 {
		claimed, err = executor.Store.Queries.ClaimTelemetryTunnelBatch(ctx, db.ClaimTelemetryTunnelBatchParams{Limit: int32(capacity), LeaseOwner: executor.InstanceID})
	}
	if err != nil {
		return
	}
	for _, tunnel := range claimed {
		executor.auditTelemetryTunnel(ctx, tunnelaudit.Claim, tunnel, tunnel.Status, "")
		executor.reconcileTunnel(ctx, tunnel)
	}
	// 心跳与陈旧检测:本副本持有但行内 epoch 已变(他副本重建)→ 拆除。
	executor.tunnels.mu.Lock()
	held := make([]*activeTunnel, 0, len(executor.tunnels.tunnels))
	for _, tunnel := range executor.tunnels.tunnels {
		held = append(held, tunnel)
	}
	executor.tunnels.mu.Unlock()
	for _, tunnel := range held {
		if time.Since(tunnel.lastHeartbeat) < tunnelruntime.HeartbeatInterval {
			continue
		}
		row, err := executor.Store.Queries.GetTelemetryTunnelByCollector(ctx, tunnel.collectorID)
		if err != nil || row.Epoch != tunnel.epoch || row.Fence != tunnel.fence || row.LeaseOwner != tunnel.leaseOwner || row.Status == "removed" {
			executor.dropTelemetryTunnelLocal(tunnel, "superseded")
			continue
		}
		credential, credentialErr := executor.Store.Queries.GetCredential(ctx, db.GetCredentialParams{ID: tunnel.credentialID, EnterpriseID: tunnel.enterpriseID})
		if credentialErr != nil || credential.Status != "active" || credential.Version != tunnel.credentialVersion {
			executor.failTelemetryTunnel(tunnel, tunnelReasonCredentialRevoked)
			continue
		}
		if _, renewErr := executor.Secrets.RenewLease(ctx, tunnel.enterpriseID, tunnel.credentialLeaseID,
			"direct_executor", executor.InstanceID, tunnel.credentialVersion, tunnelruntime.LeaseDuration); renewErr != nil {
			executor.failTelemetryTunnel(tunnel, "credential_lease_renewal_failed")
			continue
		}
		bytes, throttled := tunnel.counters.Delta()
		rows, heartbeatErr := executor.Store.Queries.HeartbeatTelemetryTunnel(ctx, db.HeartbeatTelemetryTunnelParams{
			ID: row.ID, Fence: row.Fence, LeaseOwner: row.LeaseOwner, BytesRelayed: bytes, ThrottledEvents: throttled})
		if heartbeatErr != nil || rows == 0 {
			executor.dropTelemetryTunnelLocal(tunnel, "lease_lost")
			continue
		}
		tunnel.lastHeartbeat = time.Now()
	}
}

func (executor *Executor) dropTelemetryTunnelLocal(tunnel *activeTunnel, reason string) {
	executor.tunnels.mu.Lock()
	if executor.tunnels.tunnels[tunnel.collectorID] == tunnel {
		delete(executor.tunnels.tunnels, tunnel.collectorID)
	}
	executor.tunnels.mu.Unlock()
	executor.closeTelemetryTunnel(tunnel)
	slog.Info("tunnel dropped", "collector_id", tunnel.collectorID, "epoch", tunnel.epoch, "reason", reason)
}

func (executor *Executor) closeTelemetryTunnel(tunnel *activeTunnel) {
	tunnel.closeOnce.Do(func() {
		tunnel.cancel()
		if tunnel.listener != nil {
			_ = tunnel.listener.Close()
		}
		if tunnel.identityListener != nil {
			_ = tunnel.identityListener.Close()
		}
		_ = tunnel.client.Close()
		if tunnel.credentialLeaseID != uuid.Nil {
			_ = executor.Store.Queries.RevokeCredentialLease(context.Background(), db.RevokeCredentialLeaseParams{
				ID: tunnel.credentialLeaseID, EnterpriseID: tunnel.enterpriseID})
		}
		if tunnel.counted {
			tunnelruntime.AddActive("telemetry", -1)
		}
	})
}

func (executor *Executor) failTelemetryTunnel(tunnel *activeTunnel, reason string) {
	tunnel.failureOnce.Do(func() {
		executor.persistTelemetryTunnelDrop(context.Background(), tunnel, reason)
		executor.dropTelemetryTunnelLocal(tunnel, reason)
	})
}

func (executor *Executor) persistTelemetryTunnelDrop(ctx context.Context, tunnel *activeTunnel, reason string) {
	bytes, throttled := tunnel.counters.Delta()
	_, _ = executor.Store.Queries.HeartbeatTelemetryTunnel(ctx, db.HeartbeatTelemetryTunnelParams{
		ID: tunnel.id, Fence: tunnel.fence, LeaseOwner: tunnel.leaseOwner,
		BytesRelayed: bytes, ThrottledEvents: throttled})
	executor.markTelemetryTunnelDropped(ctx, db.TelemetryTunnel{
		ID: tunnel.id, EnterpriseID: tunnel.enterpriseID, HostID: tunnel.hostID, CollectorID: tunnel.collectorID,
		CredentialID: tunnel.credentialID, Epoch: tunnel.epoch, Fence: tunnel.fence,
		LeaseOwner: tunnel.leaseOwner, Status: "established", Initiator: tunnel.initiator,
	}, "degraded", reason)
}

func (executor *Executor) reconcileTunnel(ctx context.Context, tunnel db.TelemetryTunnel) {
	if tunnel.Transport != "executor_tunnel" {
		return
	}
	executor.tunnels.mu.Lock()
	existing := executor.tunnels.tunnels[tunnel.CollectorID]
	executor.tunnels.mu.Unlock()
	if existing != nil && existing.epoch == tunnel.Epoch {
		return // 本副本已持有且未换代
	}
	if existing != nil {
		executor.dropTelemetryTunnelLocal(existing, "epoch_changed")
	}
	if tunnel.Status == "removed" {
		return
	}
	if executor.tunnels.forwardTarget == "" || executor.tunnels.identityForwardTarget == "" {
		// 部署错误 fail closed:显式落原因,安装时序门将据此失败而不是无限等待。
		slog.Error("tunnel forward target unconfigured; marking tunnel down",
			"collector_id", tunnel.CollectorID, "tunnel_id", tunnel.ID)
		executor.markTelemetryTunnelDropped(ctx, tunnel, "down", tunnelReasonForwardUnconfigured)
		return
	}
	if err := executor.establishTunnel(ctx, tunnel); err != nil {
		reason := tunnelReasonEstablishFailed
		if isLoopbackPortConflict(err) {
			reason = tunnelReasonPortConflict
		}
		executor.markTelemetryTunnelDropped(ctx, tunnel, "down", reason)
		slog.Error("tunnel establish failed", "collector_id", tunnel.CollectorID, "reason", reason, "error", err)
	}
}

// establishTunnel 建立到目标的 SSH 并开 remote forward:目标回环端口转发回
// 本 Executor,再桥接到平台内部 ingest。凭据/固定 IP/host key 校验与 Collector
// 安装路径同源。
func (executor *Executor) establishTunnel(ctx context.Context, tunnel db.TelemetryTunnel) error {
	credential, err := executor.Store.Queries.GetCredential(ctx, db.GetCredentialParams{ID: tunnel.CredentialID, EnterpriseID: tunnel.EnterpriseID})
	if err != nil || credential.Status != "active" || credential.Version != tunnel.CredentialVersion {
		return errors.New("credential unavailable")
	}
	addresses, err := executor.Validator.Resolve(ctx, tunnel.TargetAddress)
	if err != nil || len(addresses) == 0 {
		return errors.New("target resolve denied")
	}
	lease, err := executor.Secrets.IssueLease(secret.WithActorType(ctx, "direct_executor"), executor.InstanceID, tunnel.EnterpriseID, secret.LeaseRequest{
		CredentialID: credential.ID, OperationRef: "telemetry_tunnel:" + tunnel.ID.String(), TargetResourceType: "host",
		TargetResourceID: tunnel.HostID, RecipientType: "direct_executor", RecipientID: executor.InstanceID,
		Protocol: "ssh", TTL: tunnelruntime.LeaseDuration,
	})
	if err != nil {
		return err
	}
	defer clear(lease.Value)
	defer func() {
		if err != nil {
			_ = executor.Store.Queries.RevokeCredentialLease(context.Background(), db.RevokeCredentialLeaseParams{ID: lease.Lease.ID, EnterpriseID: tunnel.EnterpriseID})
		}
	}()
	if err = executor.Validator.Revalidate(ctx, tunnel.TargetAddress, addresses); err != nil {
		return err
	}
	client, err := executor.dialPinnedSSH(ctx, addresses[0], tunnel.TargetPort, tunnel.TargetUsername, tunnel.PinnedHostKey, lease.Value)
	if err != nil {
		return err
	}
	listener, err := client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.LoopbackPort))
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("remote forward 127.0.0.1:%d: %w", tunnel.LoopbackPort, err)
	}
	identityPort := tunnel.LoopbackPort + 1
	identityListener, err := client.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", identityPort))
	if err != nil {
		_ = listener.Close()
		_ = client.Close()
		return fmt.Errorf("identity remote forward 127.0.0.1:%d: %w", identityPort, err)
	}
	tunnelCtx, cancel := context.WithCancel(ctx)
	entry := &activeTunnel{id: tunnel.ID, enterpriseID: tunnel.EnterpriseID, hostID: tunnel.HostID,
		collectorID: tunnel.CollectorID, initiator: tunnel.Initiator,
		epoch: tunnel.Epoch, fence: tunnel.Fence, leaseOwner: tunnel.LeaseOwner,
		credentialID: tunnel.CredentialID, credentialVersion: tunnel.CredentialVersion,
		credentialLeaseID: lease.Lease.ID, client: client, listener: listener, identityListener: identityListener,
		cancel: cancel, lastHeartbeat: time.Now()}
	current, err := executor.Store.Queries.MarkTelemetryTunnelEstablished(ctx, db.MarkTelemetryTunnelEstablishedParams{
		ID: tunnel.ID, Fence: tunnel.Fence, LeaseOwner: tunnel.LeaseOwner})
	if err != nil {
		executor.closeTelemetryTunnel(entry)
		return err
	}
	executor.auditTelemetryTunnel(ctx, tunnelaudit.Establish, current, current.Status, "")
	executor.tunnels.mu.Lock()
	entry.counted = true
	executor.tunnels.tunnels[tunnel.CollectorID] = entry
	executor.tunnels.mu.Unlock()
	tunnelruntime.AddActive("telemetry", 1)
	if tunnel.Fence > 1 {
		tunnelruntime.RecordTakeover("telemetry")
	}
	go executor.serveTelemetryTunnel(tunnelCtx, entry, listener, executor.tunnels.forwardTarget)
	go executor.serveTelemetryTunnel(tunnelCtx, entry, identityListener, executor.tunnels.identityForwardTarget)
	go executor.keepTelemetryTunnelAlive(tunnelCtx, entry)
	slog.Info("tunnel established", "collector_id", tunnel.CollectorID, "loopback", tunnel.LoopbackPort,
		"identity_loopback", identityPort, "epoch", tunnel.Epoch)
	return nil
}

// acceptTunnelConnections 桥接目标回环连接到平台内部 ingest。
func (executor *Executor) serveTelemetryTunnel(ctx context.Context, tunnel *activeTunnel, listener net.Listener, target string) {
	relay := tunnelruntime.Relay{Target: target, Kind: "telemetry",
		Limiter: executor.tunnels.limiter, Counters: &tunnel.counters, Dialer: net.Dialer{Timeout: 10 * time.Second}}
	if err := relay.Serve(ctx, listener); err != nil && ctx.Err() == nil {
		executor.failTelemetryTunnel(tunnel, "relay_failed")
	}
}

// keepTunnelAlive 通过 SSH 全局请求探活;失败时由 reconcile 重建。
func (executor *Executor) keepTelemetryTunnelAlive(ctx context.Context, tunnel *activeTunnel) {
	if err := tunnelruntime.Keepalive(ctx, tunnel.client, tunnelruntime.HeartbeatInterval); err != nil && ctx.Err() == nil {
		tunnelruntime.RecordReconnect("telemetry")
		executor.failTelemetryTunnel(tunnel, "keepalive_failed")
	}
}

func sshAuthentication(value []byte) (ssh.AuthMethod, error) {
	signer, err := ssh.ParsePrivateKey(value)
	if err == nil {
		return ssh.PublicKeys(signer), nil
	}
	return ssh.Password(string(value)), nil
}

// tunnelGateForCollector 是安装执行的时序门:返回 (ready, failureCode)。
// ready=false 且 failureCode 非空表示不可恢复的前件失败,应直接失败 operation;
// ready=false 且 failureCode 为空表示暂时未就绪,等待租约重排。
func (executor *Executor) tunnelGateForCollector(ctx context.Context, collectorID uuid.UUID) (bool, string) {
	tunnel, err := executor.Store.Queries.GetTelemetryTunnelByCollector(ctx, collectorID)
	if err != nil {
		return true, "" // 无隧道行 → direct 路由
	}
	switch tunnel.Status {
	case "established":
		return true, ""
	case "down":
		switch tunnel.LastDropReason {
		case tunnelReasonPortConflict:
			return false, "COLLECTOR_LOOPBACK_PORT_CONFLICT"
		case tunnelReasonForwardUnconfigured:
			return false, "TUNNEL_FORWARD_TARGET_UNCONFIGURED"
		case tunnelReasonQuotaExceeded:
			return false, "TUNNEL_QUOTA_EXCEEDED"
		case tunnelReasonCredentialRevoked:
			return false, "CREDENTIAL_UNAVAILABLE"
		}
	}
	return false, "" // desired/establishing/degraded 或瞬时 down:等待重建
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// dialPinnedSSH 建立带 host key pin 的 SSH 连接(代装与隧道共用)。
func (executor *Executor) dialPinnedSSH(ctx context.Context, address netip.Addr, port int32, username, pinnedHostKey string, credential []byte) (*ssh.Client, error) {
	if pinnedHostKey == "" {
		return nil, errors.New("target host key is not pinned")
	}
	authMethod, authErr := sshAuthentication(credential)
	if authErr != nil {
		return nil, authErr
	}
	configuration := &ssh.ClientConfig{User: username, Auth: []ssh.AuthMethod{authMethod}, Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != pinnedHostKey {
				return errHostKeyMismatch
			}
			return nil
		}}
	connection, err := dialFixed(ctx, address, port)
	if err != nil {
		return nil, err
	}
	transport, channels, requests, err := ssh.NewClientConn(connection,
		net.JoinHostPort(address.String(), fmt.Sprint(port)), configuration)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(transport, channels, requests), nil
}

// runSSHCommand 在目标上执行命令,可选 stdin 输入;远端 stderr 进入错误信息。
func runSSHCommand(client *ssh.Client, command string, input []byte) error {
	if input == nil {
		return runSSHCommandReader(client, command, nil)
	}
	return runSSHCommandReader(client, command, bytes.NewReader(input))
}

func runSSHCommandReader(client *ssh.Client, command string, input io.Reader) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	if input != nil {
		session.Stdin = input
	}
	var stderr strings.Builder
	session.Stderr = &stderr
	if err = session.Run(command); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}
