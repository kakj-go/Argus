package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	connectorgatewayapp "github.com/kakj-go/Argus/internal/app/connectorgateway"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := connectorgatewayapp.Run(ctx, logger); err != nil {
		logger.Error("argus-connector-gateway stopped with an error", "error", err)
		os.Exit(1)
	}
}
