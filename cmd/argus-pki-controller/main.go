package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	pkicontroller "github.com/kakj-go/Argus/internal/app/pkicontroller"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := pkicontroller.Run(ctx, logger); err != nil {
		logger.Error("Argus PKI controller stopped with an error", "error", err)
		os.Exit(1)
	}
}
