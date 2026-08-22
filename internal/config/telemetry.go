package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Telemetry struct {
	Mode                        string
	HealthAddress               string
	DatabaseURL                 string
	RedisURL                    string
	KafkaBrokers                []string
	KafkaGroup                  string
	KafkaUsername               string
	KafkaPassword               string
	ClickHouseAddress           string
	ClickHouseDatabase          string
	ClickHouseUsername          string
	ClickHousePassword          string
	ClickHouseSchemaUsername    string
	ClickHouseSchemaPassword    string
	IngestGRPCAddress           string
	IngestHTTPAddress           string
	QueryAddress                string
	QueryEndpoint               string
	TLSCertPath                 string
	TLSKeyPath                  string
	ClientCAPath                string
	ClientCertPath              string
	ClientKeyPath               string
	ServerName                  string
	KubeconfigPath              string
	CertificateRequestNamespace string
	IssuerName                  string
	IssuerGeneration            int32
	IngestGRPCEndpoint          string
	IngestHTTPEndpoint          string
	QueryConcurrency            int
}

func LoadTelemetry(mode string) Telemetry {
	issuerGeneration, _ := strconv.ParseInt(valueOrDefault("ARGUS_TELEMETRY_ISSUER_GENERATION", "1"), 10, 32)
	queryConcurrency, _ := strconv.Atoi(valueOrDefault("ARGUS_TELEMETRY_QUERY_CONCURRENCY", "4"))
	return Telemetry{
		Mode: mode, HealthAddress: LoadHealthAddress(), DatabaseURL: os.Getenv("ARGUS_DATABASE_URL"), RedisURL: os.Getenv("ARGUS_REDIS_URL"),
		KafkaBrokers: splitList(os.Getenv("ARGUS_KAFKA_BROKERS")), KafkaGroup: valueOrDefault("ARGUS_TELEMETRY_KAFKA_GROUP", "argus-telemetry-writer-v1"),
		KafkaUsername: os.Getenv("ARGUS_KAFKA_USERNAME"), KafkaPassword: os.Getenv("ARGUS_KAFKA_PASSWORD"),
		ClickHouseAddress: os.Getenv("ARGUS_CLICKHOUSE_ADDRESS"), ClickHouseDatabase: valueOrDefault("ARGUS_CLICKHOUSE_DATABASE", "argus_telemetry"),
		ClickHouseUsername: os.Getenv("ARGUS_CLICKHOUSE_USERNAME"), ClickHousePassword: os.Getenv("ARGUS_CLICKHOUSE_PASSWORD"),
		ClickHouseSchemaUsername: os.Getenv("ARGUS_CLICKHOUSE_SCHEMA_USERNAME"), ClickHouseSchemaPassword: os.Getenv("ARGUS_CLICKHOUSE_SCHEMA_PASSWORD"),
		IngestGRPCAddress: valueOrDefault("ARGUS_TELEMETRY_INGEST_GRPC_ADDRESS", ":4317"), IngestHTTPAddress: valueOrDefault("ARGUS_TELEMETRY_INGEST_HTTP_ADDRESS", ":4318"),
		QueryAddress: valueOrDefault("ARGUS_TELEMETRY_QUERY_ADDRESS", ":9447"), QueryEndpoint: os.Getenv("ARGUS_TELEMETRY_QUERY_ENDPOINT"),
		TLSCertPath:                 valueOrDefault("ARGUS_TELEMETRY_TLS_CERT_PATH", "/var/run/secrets/argus/telemetry-server/tls.crt"),
		TLSKeyPath:                  valueOrDefault("ARGUS_TELEMETRY_TLS_KEY_PATH", "/var/run/secrets/argus/telemetry-server/tls.key"),
		ClientCAPath:                valueOrDefault("ARGUS_TELEMETRY_CLIENT_CA_PATH", "/var/run/secrets/argus/telemetry-ca/ca.crt"),
		ClientCertPath:              valueOrDefault("ARGUS_TELEMETRY_CLIENT_CERT_PATH", "/var/run/secrets/argus/telemetry-client/tls.crt"),
		ClientKeyPath:               valueOrDefault("ARGUS_TELEMETRY_CLIENT_KEY_PATH", "/var/run/secrets/argus/telemetry-client/tls.key"),
		ServerName:                  valueOrDefault("ARGUS_TELEMETRY_SERVER_NAME", "argus-telemetry"),
		KubeconfigPath:              os.Getenv("ARGUS_KUBECONFIG"),
		CertificateRequestNamespace: valueOrDefault("ARGUS_TELEMETRY_CERTIFICATE_REQUEST_NAMESPACE", "argus-observability"),
		IssuerName:                  valueOrDefault("ARGUS_TELEMETRY_ISSUER_NAME", "argus-telemetry-ca"),
		IssuerGeneration:            int32(issuerGeneration),
		IngestGRPCEndpoint:          os.Getenv("ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT"),
		IngestHTTPEndpoint:          os.Getenv("ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT"),
		QueryConcurrency:            queryConcurrency,
	}
}

func (cfg Telemetry) Validate() error {
	if cfg.Mode != "ingest" && cfg.Mode != "writer" && cfg.Mode != "query" {
		return errors.New("telemetry mode must be ingest, writer, or query")
	}
	if cfg.DatabaseURL == "" {
		return errors.New("telemetry PostgreSQL configuration is required")
	}
	if (cfg.Mode == "ingest" || cfg.Mode == "writer") && (len(cfg.KafkaBrokers) == 0 || cfg.KafkaUsername == "" || cfg.KafkaPassword == "") {
		return errors.New("telemetry PostgreSQL and authenticated Kafka configuration are required")
	}
	if cfg.Mode == "ingest" || cfg.Mode == "writer" {
		for _, broker := range cfg.KafkaBrokers {
			if strings.TrimSpace(broker) == "" {
				return errors.New("telemetry Kafka broker is invalid")
			}
		}
	}
	if cfg.Mode == "ingest" && (cfg.RedisURL == "" || cfg.IngestGRPCAddress == "" || cfg.IngestHTTPAddress == "" || cfg.TLSCertPath == "" || cfg.TLSKeyPath == "" || cfg.ClientCAPath == "") {
		return errors.New("telemetry ingest Redis, listeners, and mTLS configuration are required")
	}
	if cfg.Mode == "ingest" && (cfg.CertificateRequestNamespace == "" || cfg.IssuerName == "" || cfg.IssuerGeneration < 1 || cfg.IngestGRPCEndpoint == "" || cfg.IngestHTTPEndpoint == "") {
		return errors.New("telemetry Collector rotation issuer and endpoints are required")
	}
	if (cfg.Mode == "writer" || cfg.Mode == "query") && (cfg.ClickHouseAddress == "" || cfg.ClickHouseUsername == "" || cfg.ClickHousePassword == "") {
		return errors.New("telemetry ClickHouse configuration is required")
	}
	if cfg.Mode == "query" && (cfg.ClickHouseSchemaUsername == "" || cfg.ClickHouseSchemaPassword == "") {
		return errors.New("telemetry query ClickHouse schema manager configuration is required")
	}
	if cfg.Mode == "query" && (cfg.RedisURL == "" || cfg.QueryAddress == "" || cfg.TLSCertPath == "" || cfg.TLSKeyPath == "" || cfg.ClientCAPath == "" || cfg.QueryConcurrency < 1 || cfg.QueryConcurrency > 64) {
		return errors.New("telemetry query Redis, listener, concurrency, and mTLS configuration are required")
	}
	return nil
}
