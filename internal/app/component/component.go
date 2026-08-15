// Package component provides the shared lifecycle for service skeletons.
package component

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

type healthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
}

func HealthHandler(name string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeHealth(writer, name, "ok")
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		writeHealth(writer, name, "ready")
	})
	return mux
}

func Wait(ctx context.Context, logger *slog.Logger, name, address string) error {
	server := &http.Server{
		Addr:              address,
		Handler:           HealthHandler(name),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error(name+" health server graceful shutdown failed", "error", err)
		}
	}()

	logger.Info(name+" started", "status", "skeleton", "health_address", address)
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-shutdownDone
	logger.Info(name + " stopped")
	return nil
}

func writeHealth(writer http.ResponseWriter, service, status string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(healthResponse{Service: service, Status: status})
}
