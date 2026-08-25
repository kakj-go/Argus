package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	workerapp "github.com/kakj-go/Argus/internal/app/worker"
)

func main() {
	pool := flag.String("pool", workerapp.PoolDefault, "worker pool: default, agent, action, compaction, sandbox, or direct-executor")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := workerapp.Run(ctx, logger, *pool); err != nil {
		logger.Error("argus-worker stopped with an error", "error", err)
		os.Exit(1)
	}
}
