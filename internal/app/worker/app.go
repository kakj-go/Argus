// Package worker contains the argus-worker application bootstrap.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kakj-go/Argus/internal/action"
	"github.com/kakj-go/Argus/internal/agent"
	"github.com/kakj-go/Argus/internal/app/component"
	cardservice "github.com/kakj-go/Argus/internal/card"
	"github.com/kakj-go/Argus/internal/collectormanager"
	"github.com/kakj-go/Argus/internal/config"
	connectorservice "github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/directexecutor"
	directv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/directexecutor/v1"
	"github.com/kakj-go/Argus/internal/hostprobe"
	"github.com/kakj-go/Argus/internal/keywrap"
	"github.com/kakj-go/Argus/internal/kubernetesreader"
	"github.com/kakj-go/Argus/internal/mcp"
	modelservice "github.com/kakj-go/Argus/internal/model"
	remoteaccessservice "github.com/kakj-go/Argus/internal/remoteaccess"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/sandbox"
	secretservice "github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
)

const (
	PoolDefault        = "default"
	PoolAgent          = "agent"
	PoolAction         = "action"
	PoolCompaction     = "compaction"
	PoolSandbox        = "sandbox"
	PoolDirectExecutor = "direct-executor"
)

func Run(ctx context.Context, logger *slog.Logger, pool string) error {
	if pool != PoolDefault && pool != PoolAgent && pool != PoolAction && pool != PoolCompaction && pool != PoolSandbox && pool != PoolDirectExecutor {
		return fmt.Errorf("unsupported worker pool %q", pool)
	}
	if pool == PoolDirectExecutor {
		return runDirectExecutor(ctx, logger)
	}
	return runRuntimeWorker(ctx, logger, pool)
}

func runRuntimeWorker(ctx context.Context, logger *slog.Logger, pool string) error {
	cfg := config.LoadServer()
	if cfg.DatabaseURL == "" || len(cfg.PendingActionKey) != 32 || cfg.SecretKEKPath == "" {
		return errors.New("runtime worker requires PostgreSQL, pending-action key, and secret KEK")
	}
	keyring, err := secretservice.LoadConfiguredKeyring(cfg.SecretKEKPath, cfg.KeyWrappingMode, cfg.OpenBaoAddress, cfg.OpenBaoToken, cfg.OpenBaoTransitKey)
	if err != nil {
		return err
	}
	var wrapping keywrap.Provider = keywrap.Local{Key: cfg.IdempotencyEncryptionKey, KeyID: "argus-evaluation"}
	if cfg.KeyWrappingMode == keywrap.ProviderOpenBaoTransit {
		wrapping = keywrap.OpenBao{Address: cfg.OpenBaoAddress, Token: cfg.OpenBaoToken, KeyID: cfg.OpenBaoTransitKey}
	}
	idempotency := postgres.Idempotency{Key: cfg.IdempotencyEncryptionKey, Provider: wrapping}
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	redisClient, err := redisstore.Open(ctx, cfg.RedisURL)
	if err != nil && redisClient == nil {
		return err
	}
	if redisClient != nil {
		defer func() { _ = redisClient.Close() }()
	}
	kubernetesClient, err := connectorservice.NewDynamicClient(cfg.KubeconfigPath)
	if err != nil {
		return err
	}
	denied, err := resource.ParseDeniedCIDRs(cfg.DirectDeniedCIDRs)
	if err != nil {
		return err
	}
	directDispatcher, err := directexecutor.NewDispatcher(cfg.DirectExecutorEndpoint, cfg.DirectExecutorServerName,
		cfg.DirectExecutorTLSCert, cfg.DirectExecutorTLSKey, cfg.DirectExecutorCABundle)
	if err != nil {
		return err
	}
	defer directDispatcher.Close()
	secretDomain := secretservice.Service{Store: store, Keyring: keyring}
	actionDomain := resource.PendingActionService{Store: store, Idempotency: idempotency, Key: cfg.PendingActionKey}
	connectorDomain := connectorservice.Service{Store: store, Redis: redisClient, GatewayEndpoint: cfg.ConnectorGatewayAddress,
		EnrollmentURL: cfg.ConnectorEnrollmentURL, Credentials: secretDomain,
		Issuer: connectorservice.CertManagerIssuer{Client: kubernetesClient, Namespace: cfg.SystemNamespace,
			IssuerName: cfg.ConnectorIssuerName, IssuerGeneration: cfg.ConnectorIssuerGeneration}}
	bastionDomain := connectorservice.BastionService{Store: store, Actions: actionDomain, Enrollment: connectorDomain}
	telemetryActions := telemetryservice.ActionExtension{Next: bastionDomain, Credentials: secretDomain,
		EnrollmentEndpoint: cfg.TelemetryEnrollment, IngestGRPCEndpoint: cfg.TelemetryIngestGRPC,
		IngestHTTPEndpoint: cfg.TelemetryIngestHTTP, ServerCABundlePath: cfg.TelemetryCABundle}
	resourceDomain := resource.Service{Store: store, Actions: actionDomain, Access: resource.AccessService{},
		Direct: resource.DirectTargetValidator{DeniedCIDRs: denied}, Commands: connectorDomain, DirectCommands: directDispatcher,
		Extension: telemetryActions, ClusterEnrollment: connectorDomain,
		Kubernetes: kubernetesreader.Reader{Store: store, Secrets: secretDomain, Validator: resource.DirectTargetValidator{DeniedCIDRs: denied}, Notifier: connectorDomain}}
	registry := mcp.NewRegistry()
	if err := (agent.ResourceTools{Store: store, Resources: resourceDomain}).Register(registry); err != nil {
		return err
	}
	if cfg.TelemetryEnabled && (pool == PoolDefault || pool == PoolAgent) {
		telemetryTLS, tlsErr := telemetryservice.ClientTLSConfig(cfg.TelemetryClientCert, cfg.TelemetryClientKey, cfg.TelemetryCABundle, cfg.TelemetryServerName)
		if tlsErr != nil {
			return tlsErr
		}
		telemetryQuery, queryErr := telemetryservice.NewGRPCQueryBackend(cfg.TelemetryQueryEndpoint, telemetryTLS, logger)
		if queryErr != nil {
			return queryErr
		}
		defer telemetryQuery.Close()
		telemetryDomain := telemetryservice.Service{Store: store, Access: resource.AccessService{}, Actions: actionDomain, Query: telemetryQuery}
		if err := (telemetryservice.Tools{Service: telemetryDomain}).Register(registry); err != nil {
			return err
		}
	}
	modelDomain := modelservice.Service{Store: store, Keyring: keyring}
	cardDomain := cardservice.Service{Store: store, Idempotency: idempotency, Tools: registry,
		PresentationTTL: cfg.CardPresentationTTL, ValidationTTL: cfg.CardValidationTTL, RuntimeVersion: cfg.CardRuntimeVersion, MaxPresentation: cfg.CardMaxPresentationBytes}
	if err := cardDomain.RegisterRenderTool(registry); err != nil {
		return err
	}
	handlers := map[string]runtime.Handler{
		PoolAgent:      agent.Loop{Store: store, Models: modelDomain, Tools: registry, Cards: cardDomain},
		PoolAction:     action.Executor{Store: store, Resources: resourceDomain, OneTimeResultKey: cfg.PendingActionKey},
		PoolCompaction: agent.Compactor{Store: store, Models: modelDomain},
		PoolSandbox:    sandbox.Runner{Service: sandbox.Service{Store: store, Keyring: keyring}},
	}
	queues := []string{pool}
	if pool == PoolDefault {
		queues = []string{PoolAgent, PoolAction, PoolCompaction, PoolSandbox}
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "argus-worker"
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel := make(chan error, len(queues)+3)
	var group sync.WaitGroup
	for _, queue := range queues {
		queue := queue
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- (runtime.Processor{Store: store, Queue: queue, Owner: hostname + ":" + queue, Handle: handlers[queue], Logger: logger}).Run(workerCtx)
		}()
	}
	if pool == PoolDefault || pool == PoolSandbox {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- (sandbox.Reconciler{Service: sandbox.Service{Store: store, Keyring: keyring}, Logger: logger}).Run(workerCtx)
		}()
	}
	if pool == PoolDefault || pool == PoolAction {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- (action.Reconciler{Executor: action.Executor{Store: store, Resources: resourceDomain}, Logger: logger}).Run(workerCtx)
		}()
	}
	if pool == PoolDefault {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- (remoteaccessservice.RemoteAccessGovernanceReconciler{Service: remoteaccessservice.Service{Store: store, Keyring: keyring}, Logger: logger}).Run(workerCtx)
		}()
	}
	health := &http.Server{Addr: config.LoadHealthAddress(), Handler: component.HealthHandler("argus-worker-" + pool), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := health.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errorsChannel <- serveErr
		}
	}()
	logger.Info("argus runtime worker started", "pool", pool, "queues", queues)
	select {
	case err = <-errorsChannel:
		cancel()
	case <-ctx.Done():
		err = nil
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = health.Shutdown(shutdownCtx)
	group.Wait()
	return err
}

func runDirectExecutor(ctx context.Context, logger *slog.Logger) error {
	cfg := config.LoadDirectExecutor()
	if err := cfg.Validate(); err != nil {
		return err
	}
	keyring, err := secretservice.LoadConfiguredKeyring(cfg.SecretKEKPath, cfg.KeyWrappingMode, cfg.OpenBaoAddress, cfg.OpenBaoToken, cfg.OpenBaoTransitKey)
	if err != nil {
		return err
	}
	denied, err := resource.ParseDeniedCIDRs(cfg.DeniedCIDRs)
	if err != nil {
		return err
	}
	if observed, verifyErr := directexecutor.VerifyEgressObserved(ctx, cfg.EgressVerificationURL, cfg.AdvertisedEgress); verifyErr != nil {
		// Egress governance is optional. Keep the executor available on the
		// cluster default route and expose the degraded posture in logs.
		logger.Warn("Direct Executor egress verification failed; continuing with default route", "error", verifyErr)
	} else if observed != "" {
		logger.Info("Direct Executor egress verified", "observed_ip", observed)
	}
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	tlsConfig, err := directexecutor.LoadServerTLS(cfg.TLSCertificate, cfg.TLSPrivateKey, cfg.ClientCABundle, cfg.AuthorizedClientNames)
	if err != nil {
		return err
	}
	var artifactClient *http.Client
	if cfg.OtelcolArtifactTLSInsecure {
		artifactClient, err = collectormanager.NewArtifactHTTPClientInsecure()
	} else {
		artifactClient, err = collectormanager.NewArtifactHTTPClient(cfg.OtelcolArtifactCABundle)
	}
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return err
	}
	defer listener.Close()
	executor := &directexecutor.Executor{Store: store, Secrets: secretservice.Service{Store: store, Keyring: keyring},
		Validator: resource.DirectTargetValidator{DeniedCIDRs: denied}, InstanceID: cfg.InstanceID, Concurrency: 8,
		CollectorIdentity: telemetryservice.IdentityService{Store: store}, TelemetryEnrollmentEndpoint: cfg.TelemetryEnrollmentEndpoint,
		TelemetryIngestGRPCEndpoint: cfg.TelemetryIngestGRPCEndpoint, TelemetryIngestHTTPEndpoint: cfg.TelemetryIngestHTTPEndpoint,
		CollectorArtifactHTTPClient: artifactClient}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.MaxRecvMsgSize(64*1024), grpc.MaxSendMsgSize(64*1024))
	directv1.RegisterDirectExecutorServiceServer(grpcServer, directexecutor.RPCServer{Executor: executor, Context: ctx, Logger: logger})
	health := &http.Server{Addr: cfg.HealthAddress, Handler: component.HealthHandler("argus-worker-direct-executor"), ReadHeaderTimeout: 5 * time.Second}
	errorsChannel := make(chan error, 3)
	// 主机实时探活归属 Direct Executor 池:它是唯一出站不受 NetworkPolicy
	// 端口限制的组件(worker 只放行固定内部端口,探测任意目标端口会被拒)。
	go func() { errorsChannel <- (hostprobe.Reconciler{Store: store, Logger: logger}).Run(ctx) }()
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
	go monitorDirectEgress(ctx, logger, cfg.EgressVerificationURL, cfg.AdvertisedEgress)
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

func monitorDirectEgress(ctx context.Context, logger *slog.Logger, verificationURL string, advertised []string) {
	if verificationURL == "" && len(advertised) == 0 {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	degraded := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			observed, err := directexecutor.VerifyEgressObserved(ctx, verificationURL, advertised)
			if err != nil {
				if !degraded {
					logger.Warn("Direct Executor egress gateway lost or unverified; using cluster default route", "error", err)
				}
				degraded = true
				continue
			}
			if degraded {
				logger.Info("Direct Executor egress verification recovered", "observed_ip", observed)
			}
			degraded = false
		}
	}
}
