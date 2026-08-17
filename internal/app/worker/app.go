// Package worker contains the argus-worker application bootstrap.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kakj-go/Argus/internal/app/component"
	"github.com/kakj-go/Argus/internal/config"
	"github.com/kakj-go/Argus/internal/directexecutor"
	directv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/directexecutor/v1"
	"github.com/kakj-go/Argus/internal/resource"
	secretservice "github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
)

const (
	PoolDefault        = "default"
	PoolDirectExecutor = "direct-executor"
)

func Run(ctx context.Context, logger *slog.Logger, pool string) error {
	if pool != PoolDefault && pool != PoolDirectExecutor {
		return fmt.Errorf("unsupported worker pool %q", pool)
	}
	if pool == PoolDirectExecutor {
		return runDirectExecutor(ctx, logger)
	}
	return component.Wait(ctx, logger, "argus-worker-"+pool, config.LoadHealthAddress())
}

func runDirectExecutor(ctx context.Context, logger *slog.Logger) error {
	cfg := config.LoadDirectExecutor()
	if err := cfg.Validate(); err != nil {
		return err
	}
	keyring, err := secretservice.LoadKeyring(cfg.SecretKEKPath)
	if err != nil {
		return err
	}
	denied, err := resource.ParseDeniedCIDRs(cfg.DeniedCIDRs)
	if err != nil {
		return err
	}
	if err := directexecutor.VerifyEgress(ctx, cfg.EgressVerificationURL, cfg.AdvertisedEgress); err != nil {
		return err
	}
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	tlsConfig, err := directexecutor.LoadServerTLS(cfg.TLSCertificate, cfg.TLSPrivateKey, cfg.ClientCABundle, cfg.AuthorizedClientName)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return err
	}
	defer listener.Close()
	executor := &directexecutor.Executor{Store: store, Secrets: secretservice.Service{Store: store, Keyring: keyring},
		Validator: resource.DirectTargetValidator{DeniedCIDRs: denied}, InstanceID: cfg.InstanceID, Concurrency: 8}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.MaxRecvMsgSize(64*1024), grpc.MaxSendMsgSize(64*1024))
	directv1.RegisterDirectExecutorServiceServer(grpcServer, directexecutor.RPCServer{Executor: executor, Context: ctx})
	health := &http.Server{Addr: cfg.HealthAddress, Handler: component.HealthHandler("argus-worker-direct-executor"), ReadHeaderTimeout: 5 * time.Second}
	errorsChannel := make(chan error, 3)
	go func() {
		if err := health.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	go func() { errorsChannel <- grpcServer.Serve(listener) }()
	go func() {
		if err := executor.Run(ctx); err != nil {
			errorsChannel <- err
		}
	}()
	logger.Info("argus Direct Executor started", "instance_id", cfg.InstanceID, "grpc_address", cfg.GRPCAddress)
	select {
	case err := <-errorsChannel:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = health.Shutdown(shutdownCtx)
		done := make(chan struct{})
		go func() { grpcServer.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-shutdownCtx.Done():
			grpcServer.Stop()
		}
		logger.Info("argus Direct Executor stopped")
		return nil
	}
}
