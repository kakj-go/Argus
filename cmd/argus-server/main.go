package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"

	serverapp "github.com/kakj-go/Argus/internal/app/server"
	"github.com/kakj-go/Argus/internal/config"
	"github.com/kakj-go/Argus/internal/platform"
	"github.com/kakj-go/Argus/internal/storage/postgres"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "admin-reset-password" {
		os.Exit(runAdminResetPassword(os.Args[2:]))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	app := serverapp.New(config.LoadServer(), logger)

	if err := app.Run(ctx); err != nil {
		logger.Error("argus-server stopped with an error", "error", err)
		os.Exit(1)
	}
}

func runAdminResetPassword(args []string) int {
	flags := flag.NewFlagSet("admin-reset-password", flag.ContinueOnError)
	userID := flags.String("user-id", "", "enterprise administrator user ID")
	if err := flags.Parse(args); err != nil || *userID == "" {
		fmt.Fprintln(os.Stderr, "usage: argus-server admin-reset-password --user-id UUID")
		return 2
	}
	id, err := uuid.Parse(*userID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid user ID")
		return 2
	}
	cfg := config.LoadServer()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer store.Close()
	created, err := (platform.EnterpriseService{Store: store, Idempotency: postgres.Idempotency{Key: cfg.IdempotencyEncryptionKey}}).ResetAdminPassword(ctx, "argusctl", id, uuid.NewString())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stdout, created.TemporaryPassword)
	return 0
}
