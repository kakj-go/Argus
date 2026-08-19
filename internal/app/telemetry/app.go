// Package telemetry contains the telemetry ingest, writer, and query bootstrap.
package telemetry

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kakj-go/Argus/internal/app/component"
	"github.com/kakj-go/Argus/internal/config"
	connectorservice "github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
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
	if mode == ModeIngest || mode == ModeWriter {
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
		shutdown, err = runQuery(ctx, cfg, logger, errorsChannel)
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
	identityService := &telemetryservice.IdentityService{Store: store, Issuer: connectorservice.CertManagerIssuer{
		Client: kubernetesClient, Namespace: cfg.CertificateRequestNamespace, IssuerName: cfg.IssuerName, IssuerKind: "ClusterIssuer",
		RequestPrefix: "argus-telemetry-", SubjectLabel: "argus.io/telemetry-collector-id", IssuerGeneration: cfg.IssuerGeneration,
		Usages: []string{"client auth", "server auth"},
	}}
	domain := &telemetryservice.IngestServer{Control: telemetryservice.PostgresIngestControl{Queries: store.Queries}, Redis: redisClient, Kafka: producer, Identity: identityService, Logger: logger,
		IngestGRPCEndpoint: cfg.IngestGRPCEndpoint, IngestHTTPEndpoint: cfg.IngestHTTPEndpoint}
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
	writer := &telemetryservice.Writer{Kafka: consumer, ClickHouse: clickhouse, Control: telemetryservice.PostgresWriterControl{Queries: store.Queries}}
	go func() { errorsChannel <- writer.Run(ctx) }()
	return func(context.Context) { consumer.Close() }, nil
}

func runQuery(ctx context.Context, cfg config.Telemetry, logger *slog.Logger, errorsChannel chan<- error) (func(context.Context), error) {
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
		_ = redisClient.Close()
		return nil, err
	}
	tlsConfig, err := telemetryservice.ServerTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.ClientCAPath)
	if err != nil {
		_ = redisClient.Close()
		return nil, err
	}
	listener, err := net.Listen("tcp", cfg.QueryAddress)
	if err != nil {
		_ = redisClient.Close()
		return nil, err
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)), grpc.MaxRecvMsgSize(1<<20), grpc.MaxSendMsgSize(8<<20))
	telemetryservice.RegisterQueryRPC(server, telemetryservice.ClickHouseQuery{Conn: clickhouse}, telemetryservice.RedisQueryConcurrencyLimiter{
		Client: redisClient.Raw, Limit: int64(cfg.QueryConcurrency),
	}, logger)
	go func() { errorsChannel <- server.Serve(listener) }()
	return func(context.Context) { server.GracefulStop(); _ = redisClient.Close() }, nil
}
