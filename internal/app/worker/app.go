// Package worker contains the argus-worker application bootstrap.
package worker

import (
	"context"
	"log/slog"

	"github.com/kakj-go/Argus/internal/app/component"
)

func Run(ctx context.Context, logger *slog.Logger) error {
	return component.Wait(ctx, logger, "argus-worker")
}
