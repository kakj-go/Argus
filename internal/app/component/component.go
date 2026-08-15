// Package component provides the shared lifecycle for service skeletons.
package component

import (
	"context"
	"log/slog"
)

func Wait(ctx context.Context, logger *slog.Logger, name string) error {
	logger.Info(name+" started", "status", "skeleton")
	<-ctx.Done()
	logger.Info(name + " stopped")
	return nil
}
