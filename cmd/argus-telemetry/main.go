package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	telemetryapp "github.com/kakj-go/Argus/internal/app/telemetry"
)

func main() {
	mode := flag.String("mode", "", "runtime mode: ingest or query")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := telemetryapp.Run(ctx, logger, *mode); err != nil {
		logger.Error("argus-telemetry stopped with an error", "error", err)
		os.Exit(1)
	}
}
