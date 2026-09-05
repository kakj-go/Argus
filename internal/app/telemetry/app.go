// Package telemetry contains the telemetry ingest, writer, and query bootstrap.
package telemetry

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kakj-go/Argus/internal/app/component"
	"github.com/kakj-go/Argus/internal/config"
	connectorservice "github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
	queryengine "github.com/kakj-go/Argus/internal/telemetry/queryengine"
	promqlengine "github.com/kakj-go/Argus/internal/telemetry/queryengine/promql"
	skywalking "github.com/kakj-go/Argus/internal/telemetry/queryengine/skywalking"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

const (
	ModeIngest = "ingest"
	ModeWriter = "writer"
	ModeQuery  = "query"
)

func Run(ctx context.Context, logger *slog.Logger, mode string) error {
	cfg := config.LoadTelemetry(mode)
	if err := cfg.Validate(); err != nil {
		return err
	}
	var store *postgres.Store
	var err error
	if mode == ModeIngest || mode == ModeWriter || mode == ModeQuery {
		store, err = postgres.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer store.Close()
	}
	health := &http.Server{Addr: cfg.HealthAddress, Handler: component.HealthHandler("argus-telemetry-" + mode), ReadHeaderTimeout: 5 * time.Second}
	errorsChannel := make(chan error, 4)
	go func() {
		if err := health.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	var shutdown func(context.Context)
	switch mode {
	case ModeIngest:
		shutdown, err = runIngest(ctx, cfg, store, logger, errorsChannel)
	case ModeWriter:
		shutdown, err = runWriter(ctx, cfg, store, errorsChannel)
	case ModeQuery:
		shutdown, err = runQuery(ctx, cfg, store, logger, errorsChannel)
	}
	if err != nil {
		_ = health.Close()
		return err
	}
	logger.Info("argus-telemetry started", "mode", mode, "health_address", cfg.HealthAddress)
	select {
	case err := <-errorsChannel:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		shutdown(shutdownCtx)
		_ = health.Shutdown(shutdownCtx)
		logger.Info("argus-telemetry stopped", "mode", mode)
		return nil
	}
}

func runIngest(ctx context.Context, cfg config.Telemetry, store *postgres.Store, logger *slog.Logger, errorsChannel chan<- error) (func(context.Context), error) {
	redisClient, err := redisstore.Open(ctx, cfg.RedisURL)
	if err != nil {
		if redisClient != nil {
			_ = redisClient.Close()
		}
		return nil, err
	}
	producer, err := telemetryservice.NewKafkaProducer(cfg.KafkaBrokers, cfg.KafkaUsername, cfg.KafkaPassword)
	if err != nil {
		_ = redisClient.Close()
		return nil, err
	}
	tlsConfig, err := telemetryservice.EnrollmentServerTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.ClientCAPath)
	if err != nil {
		producer.Close()
		_ = redisClient.Close()
		return nil, err
	}
	kubernetesClient, err := connectorservice.NewDynamicClient(cfg.KubeconfigPath)
	if err != nil {
		producer.Close()
		_ = redisClient.Close()
		return nil, err
	}
	bundles := trustbundle.Service{Store: store, MountedPath: cfg.TrustBundlePath, InitialEpoch: cfg.TrustBundleEpoch}
	activeBundle, err := bundles.EnsureInitial(ctx)
	if err != nil {
		producer.Close()
		_ = redisClient.Close()
		return nil, err
	}
	bundleNodeID := trustbundle.ProcessNodeID("telemetry-ingest")
	if err := bundles.AcknowledgeMounted(ctx, bundleNodeID); err != nil {
		producer.Close()
		_ = redisClient.Close()
		return nil, err
	}
	go bundles.RunMountedAcknowledger(ctx, bundleNodeID, 5*time.Second, func(err error) {
		logger.Warn("Trust Bundle acknowledgement failed", "error", err)
	})
	identityService := &telemetryservice.IdentityService{Store: store, TrustBundles: bundles, Issuer: connectorservice.CertManagerIssuer{
		Client: kubernetesClient, Namespace: cfg.CertificateRequestNamespace, IssuerName: cfg.IssuerName, IssuerKind: "ClusterIssuer",
		RequestPrefix: "argus-telemetry-", SubjectLabel: "argus.io/telemetry-collector-id", IssuerGeneration: int32(activeBundle.Epoch),
		Usages: []string{"client auth"},
	}, ServerIssuer: connectorservice.CertManagerIssuer{
		Client: kubernetesClient, Namespace: cfg.CertificateRequestNamespace, IssuerName: cfg.IssuerName, IssuerKind: "ClusterIssuer",
		RequestPrefix: "argus-telemetry-server-", SubjectLabel: "argus.io/telemetry-collector-id", IssuerGeneration: int32(activeBundle.Epoch),
		Usages: []string{"server auth"},
	}}
	enrollmentEndpoint := strings.TrimRight(cfg.IngestHTTPEndpoint, "/") + "/v1/identity/enroll"
	selfEnroll := &telemetryservice.SelfEnrollService{Store: store, Identity: *identityService,
		EnrollmentEndpoint: enrollmentEndpoint, IngestGRPCEndpoint: cfg.IngestGRPCEndpoint, IngestHTTPEndpoint: cfg.IngestHTTPEndpoint,
		SigningPublicKeys: telemetryservice.LoadSelfEnrollSigningKeys(), BootstrapSecretKey: cfg.PendingActionKey}
	domain := &telemetryservice.IngestServer{Control: telemetryservice.PostgresIngestControl{Queries: store.Queries}, Redis: redisClient, Kafka: producer, Identity: identityService, Logger: logger,
		SelfEnroll: selfEnroll, IngestGRPCEndpoint: cfg.IngestGRPCEndpoint, IngestHTTPEndpoint: cfg.IngestHTTPEndpoint}
	grpcServer := telemetryservice.NewIngestGRPCServer(domain, tlsConfig)
	grpcListener, err := net.Listen("tcp", cfg.IngestGRPCAddress)
	if err != nil {
		producer.Close()
		_ = redisClient.Close()
		return nil, err
	}
	httpServer := &http.Server{Addr: cfg.IngestHTTPAddress, Handler: domain.HTTPHandler(), TLSConfig: tlsConfig, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Second}
	httpListener, err := net.Listen("tcp", cfg.IngestHTTPAddress)
	if err != nil {
		_ = grpcListener.Close()
		producer.Close()
		_ = redisClient.Close()
		return nil, err
	}
	go func() { errorsChannel <- grpcServer.Serve(grpcListener) }()
	go func() {
		if err := httpServer.Serve(tls.NewListener(httpListener, tlsConfig)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	return func(shutdownCtx context.Context) {
		_ = httpServer.Shutdown(shutdownCtx)
		grpcServer.GracefulStop()
		producer.Close()
		_ = redisClient.Close()
	}, nil
}

func runWriter(ctx context.Context, cfg config.Telemetry, store *postgres.Store, errorsChannel chan<- error) (func(context.Context), error) {
	consumer, err := telemetryservice.NewKafkaConsumer(cfg.KafkaBrokers, cfg.KafkaGroup, cfg.KafkaUsername, cfg.KafkaPassword)
	if err != nil {
		return nil, err
	}
	clickhouse, err := telemetryservice.OpenClickHouse(cfg.ClickHouseAddress, cfg.ClickHouseDatabase, cfg.ClickHouseUsername, cfg.ClickHousePassword)
	if err != nil {
		consumer.Close()
		return nil, err
	}
	if err = clickhouse.Ping(ctx); err != nil {
		consumer.Close()
		return nil, err
	}
	if err = telemetryservice.RequireTelemetrySchemaVersion(ctx, clickhouse); err != nil {
		consumer.Close()
		return nil, err
	}
	writer := &telemetryservice.Writer{Kafka: consumer, ClickHouse: clickhouse, Router: telemetryservice.TenantTableRouter{}, Control: telemetryservice.PostgresWriterControl{Queries: store.Queries}}
	go func() { errorsChannel <- writer.Run(ctx) }()
	return func(context.Context) { consumer.Close() }, nil
}

func runQuery(ctx context.Context, cfg config.Telemetry, store *postgres.Store, logger *slog.Logger, errorsChannel chan<- error) (func(context.Context), error) {
	bundles := trustbundle.Service{Store: store, MountedPath: cfg.TrustBundlePath, InitialEpoch: cfg.TrustBundleEpoch}
	if _, err := bundles.EnsureInitial(ctx); err != nil {
		return nil, err
	}
	bundleNodeID := trustbundle.ProcessNodeID("telemetry-query")
	if err := bundles.AcknowledgeMounted(ctx, bundleNodeID); err != nil {
		return nil, err
	}
	go bundles.RunMountedAcknowledger(ctx, bundleNodeID, 5*time.Second, func(err error) {
		logger.Warn("Trust Bundle acknowledgement failed", "error", err)
	})
	redisClient, err := redisstore.Open(ctx, cfg.RedisURL)
	if err != nil {
		if redisClient != nil {
			_ = redisClient.Close()
		}
		return nil, err
	}
	clickhouse, err := telemetryservice.OpenClickHouse(cfg.ClickHouseAddress, cfg.ClickHouseDatabase, cfg.ClickHouseUsername, cfg.ClickHousePassword)
	if err != nil {
		_ = redisClient.Close()
		return nil, err
	}
	if err = clickhouse.Ping(ctx); err != nil {
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, err
	}
	if err = telemetryservice.RequireTelemetrySchemaVersion(ctx, clickhouse); err != nil {
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, err
	}
	if store == nil {
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, errors.New("telemetry query postgres store unavailable")
	}
	schemaConn, err := telemetryservice.OpenClickHouse(cfg.ClickHouseAddress, cfg.ClickHouseDatabase, cfg.ClickHouseSchemaUsername, cfg.ClickHouseSchemaPassword)
	if err != nil {
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, err
	}
	if err = schemaConn.Ping(ctx); err != nil {
		_ = schemaConn.Close()
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, err
	}
	manager := telemetryservice.ClickHouseTenantSchemaManager{Conn: schemaConn, Router: telemetryservice.TenantTableRouter{}}
	lifecycle := telemetryservice.TenantSchemaLifecycle{
		Manager: manager,
		Queries: store.Queries,
		Locker:  telemetryservice.PostgresTenantSchemaLocker{Pool: store.Pool},
	}
	if err := reconcileTenantSchemas(ctx, store, lifecycle); err != nil {
		_ = schemaConn.Close()
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, err
	}
	tlsConfig, err := telemetryservice.ServerTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.ClientCAPath,
		store.Queries, cfg.AuthorizedClientURIs)
	if err != nil {
		_ = schemaConn.Close()
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, err
	}
	listener, err := net.Listen("tcp", cfg.QueryAddress)
	if err != nil {
		_ = schemaConn.Close()
		_ = clickhouse.Close()
		_ = redisClient.Close()
		return nil, err
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.MaxRecvMsgSize(1<<20), grpc.MaxSendMsgSize(8<<20))
	router := telemetryservice.TenantTableRouter{}
	promEngine := promqlengine.NewEngine(clickhouse, router, logger)
	coordinator := &queryengine.Coordinator{
		PromQL: queryengine.PromQLEngine{Engine: promEngine},
		KQL:    queryengine.KQLEngine{Conn: clickhouse, Router: router},
		Trace:  queryengine.TraceEngine{Engine: skywalking.Engine{Conn: clickhouse, Router: router}},
		Audit:  queryengine.PersistentAuditSink{Store: store, Logger: logger},
		Logger: logger,
	}
	telemetryservice.RegisterQueryRPC(server, telemetryservice.ClickHouseQuery{Conn: clickhouse, Router: router, PromQL: promEngine}, telemetryservice.RedisQueryConcurrencyLimiter{
		Client: redisClient.Raw, Limit: int64(cfg.QueryConcurrency),
	}, logger, []*queryengine.Coordinator{coordinator}, telemetryservice.PostgresWriterControl{Queries: store.Queries}, lifecycle)
	reconcileCtx, reconcileCancel := context.WithCancel(ctx)
	go runTenantSchemaReconciler(reconcileCtx, store, lifecycle, logger)
	go func() { errorsChannel <- server.Serve(listener) }()
	return func(context.Context) {
		reconcileCancel()
		server.GracefulStop()
		_ = schemaConn.Close()
		_ = clickhouse.Close()
		_ = redisClient.Close()
	}, nil
}

func reconcileTenantSchemas(ctx context.Context, store *postgres.Store, lifecycle telemetryservice.TenantSchemaController) error {
	enterprises, err := store.Queries.ListAllEnterprises(ctx)
	if err != nil {
		return err
	}
	for _, enterprise := range enterprises {
		switch enterprise.Status {
		case "active":
			if err := lifecycle.EnsureTenantSchema(ctx, enterprise.ID); err != nil {
				return err
			}
		case "disabled":
			if err := lifecycle.DropTenantSchema(ctx, enterprise.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func runTenantSchemaReconciler(ctx context.Context, store *postgres.Store, lifecycle telemetryservice.TenantSchemaController, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := reconcileTenantSchemas(ctx, store, lifecycle); err != nil && logger != nil {
				logger.Error("telemetry tenant schema reconciliation failed", "error", err)
			}
		}
	}
}
