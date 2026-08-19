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
	"github.com/kakj-go/Argus/internal/directexecutor"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	remotev1 "github.com/kakj-go/Argus/internal/gen/proto/argus/remoteaccess/v1"
	"github.com/kakj-go/Argus/internal/remoteaccess"
	secretservice "github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/objectstore"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
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
	objects, err := objectstore.Open(ctx, cfg.ObjectStoreURL, cfg.ObjectStoreBucket, cfg.ObjectStoreAccess, cfg.ObjectStoreSecret)
	if err != nil {
		return err
	}
	direct, err := directexecutor.NewDispatcher(cfg.DirectExecutorEndpoint, cfg.DirectExecutorServerName, cfg.DirectExecutorTLSCert, cfg.DirectExecutorTLSKey, cfg.DirectExecutorCABundle)
	if err != nil {
		return err
	}
	defer direct.Close()
	kubernetesClient, err := connector.NewDynamicClient(cfg.KubeconfigPath)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return err
	}
	defer listener.Close()
	remotePeerListener, err := net.Listen("tcp", cfg.RemoteRPCAddress)
	if err != nil {
		return err
	}
	defer remotePeerListener.Close()
	remotePeerTLS, err := remoteaccess.LoadGatewayPeerServerTLS(cfg.TLSCertificate, cfg.TLSPrivateKey, cfg.ClientCABundle, cfg.RemotePeerServerName)
	if err != nil {
		return err
	}
	remotePeerDialer, err := remoteaccess.NewGatewayPeerDialer(cfg.TLSCertificate, cfg.TLSPrivateKey, cfg.ClientCABundle,
		cfg.RemotePeerServerName, cfg.RemotePeerHeadlessSuffix, cfg.RemotePeerPort)
	if err != nil {
		return err
	}
	remotePeerDialer.Resolver = remoteaccess.KubernetesGatewayPeerResolver{Client: kubernetesClient, Namespace: cfg.SystemNamespace}
	remotePeerDialer.Logger = logger
	domain := connector.Service{Store: store, Redis: redisClient, GatewayInstance: cfg.InstanceID, RegistryTTL: 95 * time.Second,
		Issuer: connector.CertManagerIssuer{Client: kubernetesClient, Namespace: cfg.SystemNamespace,
			IssuerName: cfg.IssuerName, IssuerGeneration: cfg.IssuerGeneration}}
	dispatchHub := connector.NewDispatchHub()
	remoteHub := connector.NewRemoteAccessHub()
	terminationHub := remoteaccess.NewTerminationHub()
	remoteSessions := remoteaccess.NewSessionTracker()
	rejectNewRemote, forceRemoteDrain := make(chan struct{}), make(chan struct{})
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.MaxRecvMsgSize(connector.MaxMessageBytes), grpc.MaxSendMsgSize(connector.MaxMessageBytes))
	collectorIdentity := telemetryservice.IdentityService{Store: store}
	connectorv1.RegisterConnectorControlServiceServer(grpcServer, connector.Gateway{Service: domain,
		Credentials: secretservice.Service{Store: store, Keyring: keyring}, HeartbeatInterval: cfg.HeartbeatInterval, Dispatch: dispatchHub,
		RemoteAccess: remoteHub, Drain: forceRemoteDrain,
		CreateCollectorEnrollment: func(ctx context.Context, collectorID uuid.UUID) (connector.CollectorEnrollmentMaterial, error) {
			token, tokenErr := collectorIdentity.CreateEnrollmentToken(ctx, nil, collectorID)
			return connector.CollectorEnrollmentMaterial{Token: token, EnrollmentEndpoint: cfg.TelemetryEnrollmentEndpoint,
				IngestGRPCEndpoint: cfg.TelemetryIngestGRPCEndpoint, IngestHTTPEndpoint: cfg.TelemetryIngestHTTPEndpoint}, tokenErr
		}})
	remoteService := remoteaccess.GatewayService{Store: store, Credentials: secretservice.Service{Store: store, Keyring: keyring}, InstanceID: cfg.InstanceID,
		DirectRecipientID: cfg.DirectExecutorRecipientID}
	peerGRPCServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(remotePeerTLS)), grpc.MaxRecvMsgSize(remoteaccess.MaxFrameBytes), grpc.MaxSendMsgSize(remoteaccess.MaxFrameBytes))
	remotev1.RegisterGatewayPeerServiceServer(peerGRPCServer, remoteaccess.GatewayPeerServer{Service: remoteService, Local: remoteHub, InstanceID: cfg.InstanceID, Logger: logger})
	connectorBackends := remoteaccess.DistributedConnectorFactory{InstanceID: cfg.InstanceID, Local: remoteHub,
		Resolver: remoteaccess.ConnectorOwnerResolver{Store: store, Redis: redisClient}, Peers: remotePeerDialer}
	remoteServer := &http.Server{Addr: cfg.RemoteWSSAddress, Handler: remoteaccess.WebSocketGateway{Service: remoteService,
		Backends: remoteaccess.RoutedBackendFactory{Direct: remoteaccess.DirectBackendFactory{
			Opener: direct, HandshakeTimeout: 15 * time.Second, Logger: logger,
		}, Connector: connectorBackends},
		ObjectStore: objects, AllowedOrigins: cfg.RemoteAllowedOrigins, RejectNew: rejectNewRemote, Drain: forceRemoteDrain,
		Sessions: remoteSessions, Terminations: terminationHub, Logger: logger}, ReadHeaderTimeout: 5 * time.Second}
	healthServer := &http.Server{Addr: cfg.HealthAddress, Handler: component.HealthHandler("argus-connector-gateway"), ReadHeaderTimeout: 5 * time.Second}
	errorsChannel := make(chan error, 4)
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	go func() { errorsChannel <- grpcServer.Serve(listener) }()
	go func() { errorsChannel <- peerGRPCServer.Serve(remotePeerListener) }()
	go func() {
		if err := remoteServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	go sweepConnectorState(ctx, domain, logger)
	go routeConnectorDispatch(ctx, redisClient, cfg.InstanceID, dispatchHub, logger)
	go routeRemoteTerminations(ctx, redisClient, cfg.InstanceID, terminationHub, logger)
	logger.Info("argus-connector-gateway started", "grpc_address", cfg.GRPCAddress, "remote_wss_address", cfg.RemoteWSSAddress,
		"remote_rpc_address", cfg.RemoteRPCAddress, "health_address", cfg.HealthAddress, "instance_id", cfg.InstanceID)
	select {
	case err := <-errorsChannel:
		return err
	case <-ctx.Done():
		close(rejectNewRemote)
		drained := remoteSessions.BeginDrain()
		<-time.After(30 * time.Second)
		close(forceRemoteDrain)
		select {
		case <-drained:
		case <-time.After(20 * time.Second):
			logger.Warn("remote access sessions did not finish before the drain deadline")
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = healthServer.Shutdown(shutdownCtx)
		_ = remoteServer.Shutdown(shutdownCtx)
		done := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			peerGRPCServer.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-shutdownCtx.Done():
			grpcServer.Stop()
			peerGRPCServer.Stop()
		}
		logger.Info("argus-connector-gateway stopped")
		return nil
	}
}

func routeRemoteTerminations(ctx context.Context, client *redisstore.Client, instanceID string, hub *remoteaccess.TerminationHub, logger *slog.Logger) {
	for ctx.Err() == nil {
		terminations, closeSubscription, err := client.SubscribeRemoteAccessTermination(ctx, instanceID)
		if err != nil {
			logger.Warn("subscribe remote access termination notifications", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		for termination := range terminations {
			sessionID, err := uuid.Parse(termination.SessionID)
			if err == nil {
				hub.Notify(sessionID, remoteaccess.Termination{Fence: termination.SessionFence, Reason: termination.Reason})
			}
		}
		_ = closeSubscription()
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
