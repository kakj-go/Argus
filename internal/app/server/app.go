// Package server contains the argus-server application bootstrap.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/kakj-go/Argus/internal/config"
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
	server := &http.Server{
		Addr:              a.config.Address,
		Handler:           httpapi.NewRouter(),
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
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	<-shutdownDone
	a.logger.Info("argus-server stopped")
	return nil
}
