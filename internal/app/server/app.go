// Package server contains the argus-server application bootstrap.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/config"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/outbox"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/platform"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
	"github.com/kakj-go/Argus/internal/transport/httpapi"
)

const shutdownTimeout = 10 * time.Second

type App struct {
	config config.Server
	logger *slog.Logger
}

func New(cfg config.Server, logger *slog.Logger) *App {
	return &App{config: cfg, logger: logger}
}

func (a *App) Run(ctx context.Context) error {
	if err := a.config.Validate(); err != nil {
		return err
	}
	postgresStore, err := postgres.Open(ctx, a.config.DatabaseURL)
	if err != nil {
		return err
	}
	defer postgresStore.Close()
	redisClient, err := redisstore.Open(ctx, a.config.RedisURL)
	if err != nil {
		if redisClient == nil {
			return err
		}
		a.logger.Warn("Redis unavailable; starting in degraded mode", "error", err)
	}
	defer func() { _ = redisClient.Close() }()
	identityService := identity.Service{Store: postgresStore, Redis: redisClient, IdleTTL: a.config.SessionIdleTTL, AbsoluteTTL: a.config.SessionAbsoluteTTL}
	cursorSigner := pagination.Signer{Key: a.config.CursorSigningKey}
	setupHandler := httpapi.SetupHandler{
		Config: a.config, Setup: platform.SetupService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Identity: identityService,
		Token: platform.SetupTokenProvider{TokenPath: a.config.SetupTokenPath, ExpiresPath: a.config.SetupTokenExpiresPath},
	}
	platformHandler := httpapi.PlatformHandler{Auth: setupHandler, Enterprise: platform.EnterpriseService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Cursor: cursorSigner}
	machineService := identity.MachineService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}
	enterpriseIdentityHandler := httpapi.EnterpriseIdentityHandler{Auth: setupHandler, Service: identity.EnterpriseService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Machine: machineService, Cursor: cursorSigner}
	enterpriseAuthorizationHandler := httpapi.EnterpriseAuthorizationHandler{Identity: enterpriseIdentityHandler, Service: authorization.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Cursor: cursorSigner}
	machineHandler := httpapi.MachineHandler{Identity: enterpriseIdentityHandler, Service: machineService, Cursor: cursorSigner}
	auditHandler := httpapi.AuditHandler{Auth: setupHandler, Enterprise: enterpriseIdentityHandler, Store: postgresStore, Cursor: cursorSigner}
	go (outbox.Relay{Store: postgresStore, Redis: redisClient, Logger: a.logger}).Run(ctx)
	server := &http.Server{
		Addr: a.config.Address,
		Handler: httpapi.NewRouterWithOptions(httpapi.RouterOptions{
			PostgreSQL: postgresStore, Redis: redisClient, Setup: &setupHandler, Platform: &platformHandler,
			EnterpriseIdentity: &enterpriseIdentityHandler, EnterpriseAuthorization: &enterpriseAuthorizationHandler,
			Machine: &machineHandler, Audit: &auditHandler, AllowedOrigins: a.config.AllowedOrigins,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			a.logger.Error("HTTP server graceful shutdown failed", "error", err)
		}
	}()

	a.logger.Info("argus-server started", "address", a.config.Address)
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	<-shutdownDone
	a.logger.Info("argus-server stopped")
	return nil
}
