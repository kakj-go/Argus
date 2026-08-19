package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/config"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/telemetry"
)

func main() {
	recordID := flag.String("record-id", "", "telemetry DLQ record UUID")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	id, err := uuid.Parse(*recordID)
	if err != nil {
		logger.Error("invalid telemetry DLQ record ID")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.LoadTelemetry("writer")
	if cfg.DatabaseURL == "" || len(cfg.KafkaBrokers) == 0 || cfg.KafkaUsername == "" || cfg.KafkaPassword == "" {
		logger.Error("telemetry replay requires PostgreSQL and authenticated Kafka configuration")
		os.Exit(1)
	}
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if err = telemetry.ReplayDLQ(ctx, store, cfg.KafkaBrokers, cfg.KafkaUsername, cfg.KafkaPassword, id); err != nil {
		logger.Error("replay telemetry DLQ record", "record_id", id, "error", err)
		os.Exit(1)
	}
	logger.Info("telemetry DLQ record replayed", "record_id", id)
}
