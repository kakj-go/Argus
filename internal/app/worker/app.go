// Package worker contains the argus-worker application bootstrap.
package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kakj-go/Argus/internal/app/component"
	"github.com/kakj-go/Argus/internal/config"
)

const (
	PoolDefault        = "default"
	PoolDirectExecutor = "direct-executor"
)

func Run(ctx context.Context, logger *slog.Logger, pool string) error {
	if pool != PoolDefault && pool != PoolDirectExecutor {
		return fmt.Errorf("unsupported worker pool %q", pool)
	}
	return component.Wait(ctx, logger, "argus-worker-"+pool, config.LoadHealthAddress())
}
