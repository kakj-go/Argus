package secret

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakj-go/Argus/internal/storage/postgres"
)

const defaultCredentialLeaseReconcileInterval = 5 * time.Second

// LeaseReconciler converges expired credential authorization in PostgreSQL.
// Runtime owners revoke leases on an orderly close; this is the durable
// fallback for killed Pods and lost processes.
type LeaseReconciler struct {
	Store    *postgres.Store
	Logger   *slog.Logger
	Interval time.Duration
}

func (reconciler LeaseReconciler) Run(ctx context.Context) error {
	interval := reconciler.Interval
	if interval <= 0 {
		interval = defaultCredentialLeaseReconcileInterval
	}
	logger := reconciler.Logger
	if logger == nil {
		logger = slog.Default()
	}
	reconcile := func() {
		if _, err := reconciler.Store.Queries.ExpireCredentialLeases(ctx); err != nil && ctx.Err() == nil {
			logger.Error("credential lease expiration reconciliation failed", "error", err)
		}
	}
	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			reconcile()
		}
	}
}
