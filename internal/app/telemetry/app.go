// Package telemetry contains the telemetry ingest/query application bootstrap.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kakj-go/Argus/internal/app/component"
	"github.com/kakj-go/Argus/internal/config"
)

const (
	ModeIngest = "ingest"
	ModeQuery  = "query"
)

func Run(ctx context.Context, logger *slog.Logger, mode string) error {
	if mode != ModeIngest && mode != ModeQuery {
		return fmt.Errorf("unsupported telemetry mode %q", mode)
	}
	return component.Wait(ctx, logger, "argus-telemetry-"+mode, config.LoadHealthAddress())
}
