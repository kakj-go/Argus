package config

import "testing"

func TestTelemetryValidateModeDependencies(t *testing.T) {
	tests := []struct {
		name   string
		config Telemetry
	}{
		{
			name: "query needs Redis ClickHouse and mTLS",
			config: Telemetry{
				Mode: ModeQueryForTest, DatabaseURL: "postgres://argus", ClickHouseAddress: "clickhouse:9000", ClickHouseUsername: "query", ClickHousePassword: "secret",
				ClickHouseSchemaUsername: "schema", ClickHouseSchemaPassword: "schema-secret",
				RedisURL: "redis://redis", QueryConcurrency: 4, QueryAddress: ":9447", TLSCertPath: "tls.crt", TLSKeyPath: "tls.key", ClientCAPath: "ca.crt",
				AuthorizedClientURIs: []string{"spiffe://argus.io/services/server/telemetry-client"},
				TrustBundlePath:      "ca.crt", TrustBundleEpoch: 1,
			},
		},
		{
			name: "writer needs PostgreSQL Kafka and ClickHouse",
			config: Telemetry{
				Mode: "writer", DatabaseURL: "postgres://argus", KafkaBrokers: []string{"kafka:9093"}, KafkaUsername: "writer", KafkaPassword: "secret",
				ClickHouseAddress: "clickhouse:9000", ClickHouseUsername: "writer", ClickHousePassword: "secret",
			},
		},
		{
			name: "ingest needs control plane Kafka Redis mTLS and rotation endpoints",
			config: Telemetry{
				Mode: "ingest", DatabaseURL: "postgres://argus", KafkaBrokers: []string{"kafka:9093"}, KafkaUsername: "ingest", KafkaPassword: "secret",
				RedisURL: "redis://redis", IngestGRPCAddress: ":4317", IngestHTTPAddress: ":4318", TLSCertPath: "tls.crt", TLSKeyPath: "tls.key", ClientCAPath: "ca.crt",
				CertificateRequestNamespace: "observability", IssuerName: "telemetry-ca", IssuerGeneration: 1,
				IngestGRPCEndpoint: "grpcs://ingest:4317", IngestHTTPEndpoint: "https://ingest:4318",
				PendingActionKey: make([]byte, 32), TrustBundlePath: "ca.crt", TrustBundleEpoch: 1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.config.Validate(); err != nil {
				t.Fatalf("valid mode configuration rejected: %v", err)
			}
		})
	}
}

func TestTelemetryQueryDoesNotAcceptMissingClickHouse(t *testing.T) {
	config := Telemetry{Mode: ModeQueryForTest, QueryAddress: ":9447", TLSCertPath: "tls.crt", TLSKeyPath: "tls.key", ClientCAPath: "ca.crt"}
	if err := config.Validate(); err == nil {
		t.Fatal("query configuration without ClickHouse was accepted")
	}
}

func TestTelemetryQueryRequiresDedicatedSchemaCredentials(t *testing.T) {
	config := Telemetry{
		Mode: ModeQueryForTest, DatabaseURL: "postgres://argus", ClickHouseAddress: "clickhouse:9000", ClickHouseUsername: "query", ClickHousePassword: "secret",
		RedisURL: "redis://redis", QueryConcurrency: 4, QueryAddress: ":9447", TLSCertPath: "tls.crt", TLSKeyPath: "tls.key", ClientCAPath: "ca.crt",
		AuthorizedClientURIs: []string{"spiffe://argus.io/services/server/telemetry-client"},
	}
	if err := config.Validate(); err == nil {
		t.Fatal("query configuration without dedicated schema credentials was accepted")
	}
}

const ModeQueryForTest = "query"
