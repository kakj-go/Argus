// Package connectorgateway contains the connector gateway application bootstrap.
package connectorgateway

import (
	"context"
	"log/slog"

	"github.com/kakj-go/Argus/internal/app/component"
	"github.com/kakj-go/Argus/internal/config"
)

func Run(ctx context.Context, logger *slog.Logger) error {
	return component.Wait(ctx, logger, "argus-connector-gateway", config.LoadHealthAddress())
}
