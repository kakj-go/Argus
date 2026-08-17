package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type ConnectorGateway struct {
	GRPCAddress       string
	HealthAddress     string
	DatabaseURL       string
	RedisURL          string
	InstanceID        string
	TLSCertificate    string
	TLSPrivateKey     string
	ClientCABundle    string
	SecretKEKPath     string
	KubeconfigPath    string
	SystemNamespace   string
	IssuerName        string
	IssuerGeneration  int32
	HeartbeatInterval time.Duration
}

func LoadConnectorGateway() ConnectorGateway {
	instanceID := os.Getenv("ARGUS_GATEWAY_INSTANCE_ID")
	if instanceID == "" {
		instanceID, _ = os.Hostname()
	}
	issuerGeneration, _ := strconv.ParseInt(valueOrDefault("ARGUS_CONNECTOR_ISSUER_GENERATION", "1"), 10, 32)
	return ConnectorGateway{
		GRPCAddress:       valueOrDefault("ARGUS_CONNECTOR_GRPC_ADDRESS", ":9443"),
		HealthAddress:     LoadHealthAddress(),
		DatabaseURL:       os.Getenv("ARGUS_DATABASE_URL"),
		RedisURL:          os.Getenv("ARGUS_REDIS_URL"),
		InstanceID:        instanceID,
		TLSCertificate:    valueOrDefault("ARGUS_CONNECTOR_TLS_CERT_PATH", "/var/run/secrets/argus/connector-gateway/tls.crt"),
		TLSPrivateKey:     valueOrDefault("ARGUS_CONNECTOR_TLS_KEY_PATH", "/var/run/secrets/argus/connector-gateway/tls.key"),
		ClientCABundle:    valueOrDefault("ARGUS_CONNECTOR_CLIENT_CA_PATH", "/var/run/secrets/argus/connector-ca/ca.crt"),
		SecretKEKPath:     valueOrDefault("ARGUS_SECRET_KEK_PATH", "/var/run/secrets/argus/secret-kek/keyring.json"),
		KubeconfigPath:    os.Getenv("ARGUS_KUBECONFIG"),
		SystemNamespace:   valueOrDefault("ARGUS_SYSTEM_NAMESPACE", "argus-system"),
		IssuerName:        valueOrDefault("ARGUS_CONNECTOR_ISSUER_NAME", "argus-connector-ca"),
		IssuerGeneration:  int32(issuerGeneration),
		HeartbeatInterval: 30 * time.Second,
	}
}

func (cfg ConnectorGateway) Validate() error {
	if cfg.GRPCAddress == "" || cfg.HealthAddress == "" || cfg.DatabaseURL == "" || cfg.RedisURL == "" || cfg.InstanceID == "" {
		return errors.New("connector gateway address, database, Redis, and instance ID are required")
	}
	if cfg.TLSCertificate == "" || cfg.TLSPrivateKey == "" || cfg.ClientCABundle == "" || cfg.SecretKEKPath == "" {
		return errors.New("connector gateway mTLS files are required")
	}
	if cfg.SystemNamespace == "" || cfg.IssuerName == "" || cfg.IssuerGeneration < 1 {
		return errors.New("connector gateway cert-manager issuer configuration is required")
	}
	return nil
}
