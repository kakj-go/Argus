package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	serverapp "github.com/kakj-go/Argus/internal/app/server"
	"github.com/kakj-go/Argus/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	app := serverapp.New(config.LoadServer(), logger)

	if err := app.Run(ctx); err != nil {
		logger.Error("argus-server stopped with an error", "error", err)
		os.Exit(1)
	}
}
