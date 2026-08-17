// Package connectorgateway contains the connector gateway application bootstrap.
package connectorgateway

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kakj-go/Argus/internal/app/component"
	"github.com/kakj-go/Argus/internal/config"
	"github.com/kakj-go/Argus/internal/connector"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	secretservice "github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
)

func Run(ctx context.Context, logger *slog.Logger) error {
	cfg := config.LoadConnectorGateway()
	if err := cfg.Validate(); err != nil {
		return err
	}
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	redisClient, redisErr := redisstore.Open(ctx, cfg.RedisURL)
	if redisErr != nil {
		if redisClient == nil {
			return redisErr
		}
		logger.Warn("Redis unavailable; Connector registry will recover from heartbeats", "error", redisErr)
	}
	defer func() { _ = redisClient.Close() }()
	tlsConfig, err := connector.LoadServerTLS(cfg.TLSCertificate, cfg.TLSPrivateKey, cfg.ClientCABundle)
	if err != nil {
		return err
	}
	keyring, err := secretservice.LoadKeyring(cfg.SecretKEKPath)
	if err != nil {
		return err
	}
	kubernetesClient, err := connector.NewDynamicClient(cfg.KubeconfigPath)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return err
	}
	defer listener.Close()
	domain := connector.Service{Store: store, Redis: redisClient, GatewayInstance: cfg.InstanceID, RegistryTTL: 95 * time.Second,
		Issuer: connector.CertManagerIssuer{Client: kubernetesClient, Namespace: cfg.SystemNamespace,
			IssuerName: cfg.IssuerName, IssuerGeneration: cfg.IssuerGeneration}}
	dispatchHub := connector.NewDispatchHub()
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.MaxRecvMsgSize(connector.MaxMessageBytes), grpc.MaxSendMsgSize(connector.MaxMessageBytes))
	connectorv1.RegisterConnectorControlServiceServer(grpcServer, connector.Gateway{Service: domain,
		Credentials: secretservice.Service{Store: store, Keyring: keyring}, HeartbeatInterval: cfg.HeartbeatInterval, Dispatch: dispatchHub, Drain: ctx.Done()})
	healthServer := &http.Server{Addr: cfg.HealthAddress, Handler: component.HealthHandler("argus-connector-gateway"), ReadHeaderTimeout: 5 * time.Second}
	errorsChannel := make(chan error, 2)
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	go func() { errorsChannel <- grpcServer.Serve(listener) }()
	go sweepConnectorState(ctx, domain, logger)
	go routeConnectorDispatch(ctx, redisClient, cfg.InstanceID, dispatchHub, logger)
	logger.Info("argus-connector-gateway started", "grpc_address", cfg.GRPCAddress, "health_address", cfg.HealthAddress, "instance_id", cfg.InstanceID)
	select {
	case err := <-errorsChannel:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = healthServer.Shutdown(shutdownCtx)
		done := make(chan struct{})
		go func() { grpcServer.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-shutdownCtx.Done():
			grpcServer.Stop()
		}
		logger.Info("argus-connector-gateway stopped")
		return nil
	}
}

func sweepConnectorState(ctx context.Context, service connector.Service, logger *slog.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := service.SweepCommandTimeouts(ctx); err != nil {
				logger.Warn("sweep Connector command timeouts", "error", err)
			}
			if _, err := service.Store.Queries.MarkStaleConnectorsOffline(ctx); err != nil {
				logger.Warn("mark stale Connectors offline", "error", err)
			}
			if _, err := service.Store.Queries.MarkStaleBastionScopesOffline(ctx); err != nil {
				logger.Warn("mark stale Bastion Scopes offline", "error", err)
			}
		}
	}
}

func routeConnectorDispatch(ctx context.Context, client *redisstore.Client, instanceID string, hub *connector.DispatchHub, logger *slog.Logger) {
	for ctx.Err() == nil {
		dispatches, closeSubscription, err := client.SubscribeConnectorDispatch(ctx, instanceID)
		if err != nil {
			logger.Warn("subscribe Connector dispatch notifications", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		for dispatch := range dispatches {
			connectorID, err := uuid.Parse(dispatch.ConnectorID)
			if err == nil {
				hub.Notify(connectorID)
			}
		}
		_ = closeSubscription()
	}
}
