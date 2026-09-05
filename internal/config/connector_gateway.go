package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type ConnectorGateway struct {
	GRPCAddress                 string
	RemoteWSSAddress            string
	RemoteRPCAddress            string
	RemotePeerServerName        string
	RemotePeerClientURI         string
	RemotePeerHeadlessSuffix    string
	RemotePeerPort              string
	RemotePeerClientCertificate string
	RemotePeerClientPrivateKey  string
	HealthAddress               string
	DatabaseURL                 string
	RedisURL                    string
	InstanceID                  string
	TLSCertificate              string
	TLSPrivateKey               string
	ClientCABundle              string
	SecretKEKPath               string
	KeyWrappingMode             string
	OpenBaoAddress              string
	OpenBaoToken                string
	OpenBaoTransitKey           string
	KubeconfigPath              string
	SystemNamespace             string
	IssuerName                  string
	IssuerGeneration            int32
	HeartbeatInterval           time.Duration
	RemoteOrigin                string
	RemoteAllowedOrigins        []string
	DirectExecutorEndpoint      string
	DirectExecutorServerName    string
	DirectExecutorTLSCert       string
	DirectExecutorTLSKey        string
	DirectExecutorCABundle      string
	DirectExecutorRecipientID   string
	ObjectStoreURL              string
	ObjectStoreBucket           string
	ObjectStoreAccess           string
	ObjectStoreSecret           string
	RemoteUserLimit             int
	RemoteHostLimit             int
	RemoteTenantLimit           int
	TelemetryEnrollmentEndpoint string
	TelemetryIngestGRPCEndpoint string
	TelemetryIngestHTTPEndpoint string
	TelemetryEnabled            bool
	TrustBundlePath             string
	TrustBundleEpoch            int64
}

func LoadConnectorGateway() ConnectorGateway {
	instanceID := os.Getenv("ARGUS_GATEWAY_INSTANCE_ID")
	if instanceID == "" {
		instanceID, _ = os.Hostname()
	}
	issuerGeneration, _ := strconv.ParseInt(valueOrDefault("ARGUS_CONNECTOR_ISSUER_GENERATION", "1"), 10, 32)
	telemetryEnabled, _ := strconv.ParseBool(valueOrDefault("ARGUS_TELEMETRY_TOOL_CATALOG_ENABLED", "true"))
	trustBundleEpoch, _ := strconv.ParseInt(valueOrDefault("ARGUS_TRUST_BUNDLE_EPOCH", "1"), 10, 64)
	return ConnectorGateway{
		GRPCAddress:                 valueOrDefault("ARGUS_CONNECTOR_GRPC_ADDRESS", ":9443"),
		RemoteWSSAddress:            valueOrDefault("ARGUS_REMOTE_WSS_ADDRESS", ":9445"),
		RemoteRPCAddress:            valueOrDefault("ARGUS_REMOTE_RPC_ADDRESS", ":9446"),
		RemotePeerServerName:        valueOrDefault("ARGUS_REMOTE_PEER_SERVER_NAME", "argus-connector-gateway"),
		RemotePeerClientURI:         valueOrDefault("ARGUS_REMOTE_PEER_CLIENT_URI", "spiffe://argus.io/services/connector-gateway/peer-client"),
		RemotePeerHeadlessSuffix:    valueOrDefault("ARGUS_REMOTE_PEER_HEADLESS_SUFFIX", "argus-connector-gateway-headless."+valueOrDefault("ARGUS_SYSTEM_NAMESPACE", "argus-system")+".svc"),
		RemotePeerPort:              valueOrDefault("ARGUS_REMOTE_PEER_PORT", "9446"),
		RemotePeerClientCertificate: valueOrDefault("ARGUS_REMOTE_PEER_CLIENT_CERT_PATH", "/var/run/secrets/argus/connector-gateway-peer-client/tls.crt"),
		RemotePeerClientPrivateKey:  valueOrDefault("ARGUS_REMOTE_PEER_CLIENT_KEY_PATH", "/var/run/secrets/argus/connector-gateway-peer-client/tls.key"),
		HealthAddress:               LoadHealthAddress(),
		DatabaseURL:                 os.Getenv("ARGUS_DATABASE_URL"),
		RedisURL:                    os.Getenv("ARGUS_REDIS_URL"),
		InstanceID:                  instanceID,
		TLSCertificate:              valueOrDefault("ARGUS_CONNECTOR_TLS_CERT_PATH", "/var/run/secrets/argus/connector-gateway/tls.crt"),
		TLSPrivateKey:               valueOrDefault("ARGUS_CONNECTOR_TLS_KEY_PATH", "/var/run/secrets/argus/connector-gateway/tls.key"),
		ClientCABundle:              valueOrDefault("ARGUS_CONNECTOR_CLIENT_CA_PATH", "/var/run/secrets/argus/trust/ca.crt"),
		SecretKEKPath:               valueOrDefault("ARGUS_SECRET_KEK_PATH", "/var/run/secrets/argus/secret-kek/keyring.json"),
		KeyWrappingMode:             valueOrDefault("ARGUS_KEY_WRAPPING_MODE", "local_test"),
		OpenBaoAddress:              os.Getenv("ARGUS_OPENBAO_ADDRESS"),
		OpenBaoToken:                os.Getenv("ARGUS_OPENBAO_TOKEN"),
		OpenBaoTransitKey:           valueOrDefault("ARGUS_OPENBAO_TRANSIT_KEY", "argus-local-hardening"),
		KubeconfigPath:              os.Getenv("ARGUS_KUBECONFIG"),
		SystemNamespace:             valueOrDefault("ARGUS_SYSTEM_NAMESPACE", "argus-system"),
		IssuerName:                  valueOrDefault("ARGUS_CONNECTOR_ISSUER_NAME", "argus-connector-ca"),
		IssuerGeneration:            int32(issuerGeneration),
		HeartbeatInterval:           30 * time.Second,
		RemoteOrigin:                os.Getenv("ARGUS_REMOTE_ORIGIN"),
		RemoteAllowedOrigins:        splitList(os.Getenv("ARGUS_ALLOWED_ORIGINS")),
		DirectExecutorEndpoint:      os.Getenv("ARGUS_DIRECT_EXECUTOR_ENDPOINT"),
		DirectExecutorServerName:    os.Getenv("ARGUS_DIRECT_EXECUTOR_SERVER_NAME"),
		DirectExecutorTLSCert:       valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_CERT_PATH", "/var/run/secrets/argus/direct-executor-client/tls.crt"),
		DirectExecutorTLSKey:        valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_KEY_PATH", "/var/run/secrets/argus/direct-executor-client/tls.key"),
		DirectExecutorCABundle:      valueOrDefault("ARGUS_DIRECT_EXECUTOR_CA_PATH", "/var/run/secrets/argus/trust/ca.crt"),
		DirectExecutorRecipientID:   valueOrDefault("ARGUS_DIRECT_EXECUTOR_RECIPIENT_ID", "argus-direct-executor"),
		ObjectStoreURL:              os.Getenv("ARGUS_OBJECT_STORE_URL"),
		ObjectStoreBucket:           os.Getenv("ARGUS_OBJECT_STORE_BUCKET"),
		ObjectStoreAccess:           os.Getenv("ARGUS_OBJECT_STORE_ACCESS_KEY"),
		ObjectStoreSecret:           os.Getenv("ARGUS_OBJECT_STORE_SECRET_KEY"),
		RemoteUserLimit:             intOrDefault("ARGUS_REMOTE_USER_LIMIT", 3),
		RemoteHostLimit:             intOrDefault("ARGUS_REMOTE_HOST_LIMIT", 5),
		RemoteTenantLimit:           intOrDefault("ARGUS_REMOTE_ENTERPRISE_LIMIT", 50),
		TelemetryEnrollmentEndpoint: os.Getenv("ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT"),
		TelemetryIngestGRPCEndpoint: os.Getenv("ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT"),
		TelemetryIngestHTTPEndpoint: os.Getenv("ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT"),
		TelemetryEnabled:            telemetryEnabled,
		TrustBundlePath:             valueOrDefault("ARGUS_TRUST_BUNDLE_PATH", "/var/run/secrets/argus/trust/ca.crt"),
		TrustBundleEpoch:            trustBundleEpoch,
	}
}

func (cfg ConnectorGateway) Validate() error {
	mode := cfg.KeyWrappingMode
	if mode == "" {
		mode = "local_test"
	}
	if cfg.GRPCAddress == "" || cfg.HealthAddress == "" || cfg.DatabaseURL == "" || cfg.RedisURL == "" || cfg.InstanceID == "" {
		return errors.New("connector gateway address, database, Redis, and instance ID are required")
	}
	if cfg.TLSCertificate == "" || cfg.TLSPrivateKey == "" || cfg.ClientCABundle == "" || cfg.RemotePeerClientCertificate == "" || cfg.RemotePeerClientPrivateKey == "" {
		return errors.New("connector gateway mTLS files are required")
	}
	if mode == "local_test" && cfg.SecretKEKPath == "" {
		return errors.New("connector gateway local keyring path is required")
	}
	if mode != "local_test" && mode != "openbao_transit" {
		return errors.New("connector gateway key wrapping mode is invalid")
	}
	if mode == "openbao_transit" && (cfg.OpenBaoAddress == "" || cfg.OpenBaoToken == "" || cfg.OpenBaoTransitKey == "") {
		return errors.New("connector gateway OpenBao Transit configuration is required")
	}
	if cfg.SystemNamespace == "" || cfg.IssuerName == "" || cfg.IssuerGeneration < 1 || cfg.TrustBundlePath == "" || cfg.TrustBundleEpoch < 1 {
		return errors.New("connector gateway cert-manager issuer configuration is required")
	}
	if cfg.RemoteWSSAddress == "" || cfg.RemoteRPCAddress == "" || cfg.RemoteOrigin == "" || cfg.RemotePeerServerName == "" || cfg.RemotePeerClientURI == "" || cfg.RemotePeerHeadlessSuffix == "" || cfg.RemotePeerPort == "" {
		return errors.New("remote access WSS, internal RPC, and Origin configuration are required")
	}
	if len(cfg.RemoteAllowedOrigins) == 0 || cfg.DirectExecutorEndpoint == "" || cfg.DirectExecutorServerName == "" || cfg.DirectExecutorTLSCert == "" || cfg.DirectExecutorTLSKey == "" || cfg.DirectExecutorCABundle == "" || cfg.DirectExecutorRecipientID == "" {
		return errors.New("remote access Origin allowlist and Direct Executor mTLS configuration are required")
	}
	if cfg.ObjectStoreURL == "" || cfg.ObjectStoreBucket == "" || cfg.ObjectStoreAccess == "" || cfg.ObjectStoreSecret == "" {
		return errors.New("remote access ObjectStore configuration is required")
	}
	if cfg.RemoteUserLimit < 1 || cfg.RemoteHostLimit < 1 || cfg.RemoteTenantLimit < 1 {
		return errors.New("remote access capacity limits must be positive")
	}
	if cfg.TelemetryEnabled && (cfg.TelemetryEnrollmentEndpoint == "" || cfg.TelemetryIngestGRPCEndpoint == "" || cfg.TelemetryIngestHTTPEndpoint == "") {
		return errors.New("connector gateway telemetry Collector endpoints are required")
	}
	return nil
}
