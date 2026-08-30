// Package hostprobe 周期探测直连主机的存活状态:TCP 连通 + SSH 握手抓取
// 主机键指纹(不使用任何凭据)。与 hosts.connection_status 的"接入时人工
// 验证"语义分离,结果仅在状态变迁时写入 host_probe_states 并记审计。
package hostprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/ssh"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	StatusOnline     = "online"
	StatusOffline    = "offline"
	StatusKeyChanged = "key_changed"

	defaultInterval       = 60 * time.Second
	dialTimeout           = 3 * time.Second
	sshHandshakeTimeout   = 5 * time.Second
	concurrencyFloor      = 8
	concurrencyCap        = 256
	defaultBatch          = 512
	failuresBeforeOffline = 2
	// 自适应控制器阈值:周期接近间隔上限时扩容,轻松完成时缩容。
	scaleUpThreshold   = 0.75
	scaleDownThreshold = 0.25
)

type Reconciler struct {
	Store  *postgres.Store
	Logger *slog.Logger
	// Interval 探测周期;Batch 单轮认领上限;初始并发由 Concurrency 指定。
	Interval    time.Duration
	Batch       int
	Concurrency int
}

type probeTarget struct {
	HostID        string
	EnterpriseID  string
	Address       string
	Port          int32
	PinnedHostKey string
}

type probeOutcome struct {
	Target      probeTarget
	Status      string
	LatencyMS   int32
	Fingerprint string
	Error       string
	Reached     bool
}

// Run 启动周期探测循环。并发自适应:上一轮耗时超过周期的 75% 时并发 ×1.5
// (上限 256),低于 25% 时 ×0.75(下限 8);轮次重叠时跳过 tick 防止堆积。
func (reconciler Reconciler) Run(ctx context.Context) error {
	if reconciler.Store == nil {
		return errors.New("host probe reconciler requires PostgreSQL")
	}
	if reconciler.Interval <= 0 {
		reconciler.Interval = defaultInterval
	}
	if reconciler.Batch <= 0 {
		reconciler.Batch = defaultBatch
	}
	if reconciler.Concurrency < concurrencyFloor {
		reconciler.Concurrency = concurrencyFloor
	}
	if reconciler.Concurrency > concurrencyCap {
		reconciler.Concurrency = concurrencyCap
	}
	if reconciler.Logger == nil {
		reconciler.Logger = slog.Default()
	}
	ticker := time.NewTicker(reconciler.Interval)
	defer ticker.Stop()
	var running sync.Mutex
	for {
		if !running.TryLock() {
			reconciler.Logger.Warn("host probe cycle overlapped; skipping tick and scaling up")
			reconciler.Concurrency = scaleConcurrency(reconciler.Concurrency, 1.5)
		} else {
			started := time.Now()
			reconciler.cycle(ctx)
			elapsed := time.Since(started)
			running.Unlock()
			ratio := elapsed.Seconds() / reconciler.Interval.Seconds()
			previous := reconciler.Concurrency
			switch {
			case ratio > scaleUpThreshold:
				reconciler.Concurrency = scaleConcurrency(reconciler.Concurrency, 1.5)
			case ratio < scaleDownThreshold:
				reconciler.Concurrency = scaleConcurrency(reconciler.Concurrency, 0.75)
			}
			if reconciler.Concurrency != previous {
				reconciler.Logger.Info("host probe concurrency adjusted",
					"from", previous, "to", reconciler.Concurrency, "cycle_seconds", elapsed.Seconds())
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func scaleConcurrency(current int, factor float64) int {
	next := int(float64(current) * factor)
	if next < concurrencyFloor {
		next = concurrencyFloor
	}
	if next > concurrencyCap {
		next = concurrencyCap
	}
	return next
}

func (reconciler Reconciler) cycle(ctx context.Context) {
	targets, err := reconciler.Store.Queries.ClaimHostProbeBatch(ctx, int32(reconciler.Batch))
	if err != nil || len(targets) == 0 {
		if err != nil && ctx.Err() == nil {
			reconciler.Logger.Error("host probe claim failed", "error", err)
		}
		return
	}
	// 读取既有状态用于去抖判定(连续失败计数)。
	states, err := reconciler.Store.Queries.ListHostProbeStatesByHosts(ctx, hostIDs(targets))
	if err != nil {
		reconciler.Logger.Error("host probe state lookup failed", "error", err)
		return
	}
	previous := make(map[string]db.HostProbeState, len(states))
	for _, state := range states {
		previous[state.HostID.String()] = state
	}

	outcomes := make(chan probeOutcome, len(targets))
	workers := reconciler.Concurrency
	if workers > len(targets) {
		workers = len(targets)
	}
	queue := make(chan probeTarget)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for target := range queue {
				outcomes <- probeOne(ctx, target)
			}
		}()
	}
	for _, target := range targets {
		queue <- probeTarget{HostID: target.ID.String(), EnterpriseID: target.EnterpriseID.String(),
			Address: target.Address, Port: target.Port, PinnedHostKey: target.PinnedHostKey}
	}
	close(queue)
	go func() { group.Wait(); close(outcomes) }()

	for outcome := range outcomes {
		reconciler.record(ctx, outcome, previous[outcome.Target.HostID])
	}
}

// record 应用去抖(连续 failuresBeforeOffline 次失败才转 offline,成功即恢复)
// 并仅在状态变迁时写库与审计,避免每轮刷写造成版本与审计噪音。
func (reconciler Reconciler) record(ctx context.Context, outcome probeOutcome, before db.HostProbeState) {
	status := outcome.Status
	failures := int32(0)
	if outcome.Status == StatusOffline {
		failures = before.ConsecutiveFailures + 1
		if failures < failuresBeforeOffline {
			// 未达离线阈值:记录计数但保持原状态(首次探测时视为 offline 待确认)。
			status = before.Status
			if status == "" {
				status = StatusOffline
			}
		}
	}
	hostID, hostErr := uuid.Parse(outcome.Target.HostID)
	enterpriseID, enterpriseErr := uuid.Parse(outcome.Target.EnterpriseID)
	if hostErr != nil || enterpriseErr != nil {
		return
	}
	_, err := reconciler.Store.Queries.UpsertHostProbeState(ctx, db.UpsertHostProbeStateParams{
		HostID: hostID, EnterpriseID: enterpriseID,
		Status: status, LastCheckedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		LatencyMs:   outcome.LatencyMS,
		Fingerprint: outcome.Fingerprint, ConsecutiveFailures: failures, Error: outcome.Error,
	})
	if err != nil && ctx.Err() == nil {
		reconciler.Logger.Error("host probe state write failed", "host_id", outcome.Target.HostID, "error", err)
		return
	}
	if before.Status != "" && before.Status != status {
		_, auditErr := audit.Append(ctx, reconciler.Store.Queries, audit.Entry{Domain: "enterprise",
			EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: "system",
			Action: "host.probe_transition", ResourceType: "host", ResourceID: outcome.Target.HostID,
			Result: "success", Details: map[string]any{"from": before.Status, "to": status, "latency_ms": outcome.LatencyMS}})
		if auditErr != nil && ctx.Err() == nil {
			reconciler.Logger.Error("host probe audit failed", "host_id", outcome.Target.HostID, "error", auditErr)
		}
	}
}

// probeOne 对单台主机执行 TCP 连通 + SSH 握手(无认证):可达且主机键与
// pinned 值一致 → online;键不一致 → key_changed;不可达 → offline。
func probeOne(ctx context.Context, target probeTarget) probeOutcome {
	started := time.Now()
	address := net.JoinHostPort(target.Address, fmt.Sprint(target.Port))
	dialer := net.Dialer{Timeout: dialTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return probeOutcome{Target: target, Status: StatusOffline, Error: err.Error()}
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(sshHandshakeTimeout))
	var fingerprint string
	sshConfig := &ssh.ClientConfig{User: "probe", HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fingerprint = ssh.FingerprintSHA256(key)
		return nil
	}}
	_, _, _, err = ssh.NewClientConn(connection, address, sshConfig)
	// 握手到"等待认证"即证明 sshd 存活;认证类错误不区分,握手失败按不可达处理。
	if fingerprint == "" {
		if err != nil {
			return probeOutcome{Target: target, Status: StatusOffline, Error: err.Error()}
		}
		return probeOutcome{Target: target, Status: StatusOffline, Error: "no host key captured"}
	}
	latency := int32(time.Since(started).Milliseconds())
	if target.PinnedHostKey != "" && fingerprint != target.PinnedHostKey {
		return probeOutcome{Target: target, Status: StatusKeyChanged, LatencyMS: latency,
			Fingerprint: fingerprint, Error: "host key differs from pinned value", Reached: true}
	}
	return probeOutcome{Target: target, Status: StatusOnline, LatencyMS: latency, Fingerprint: fingerprint, Reached: true}
}

func hostIDs(targets []db.ClaimHostProbeBatchRow) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	return ids
}
