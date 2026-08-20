// Package config loads process configuration from the environment.
package config

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultHTTPAddress = ":8080"
const defaultHealthAddress = ":8081"

type Server struct {
	Address                   string
	DatabaseURL               string
	RedisURL                  string
	SetupTokenPath            string
	SetupTokenExpiresPath     string
	AllowedOrigins            []string
	SecureCookies             bool
	IdempotencyEncryptionKey  []byte
	CursorSigningKey          []byte
	PendingActionKey          []byte
	SecretKEKPath             string
	ConnectorEnrollmentURL    string
	ConnectorGatewayAddress   string
	KubeconfigPath            string
	SystemNamespace           string
	ConnectorIssuerName       string
	ConnectorIssuerGeneration int32
	DeploymentProfile         string
	DirectAdvertisedEgress    []string
	DirectVerificationURL     string
	DirectDeniedCIDRs         []string
	DirectExecutorEndpoint    string
	DirectExecutorServerName  string
	DirectExecutorTLSCert     string
	DirectExecutorTLSKey      string
	DirectExecutorCABundle    string
	SessionIdleTTL            time.Duration
	SessionAbsoluteTTL        time.Duration
	CardPresentationTTL       time.Duration
	CardValidationTTL         time.Duration
	CardRuntimeVersion        string
	CardMaxPresentationBytes  int
	RemoteOrigin              string
	RemoteUserLimit           int
	RemoteHostLimit           int
	RemoteEnterpriseLimit     int
	ObjectStoreURL            string
	ObjectStoreBucket         string
	ObjectStoreAccess         string
	ObjectStoreSecret         string
	TelemetryQueryEndpoint    string
	TelemetryClientCert       string
	TelemetryClientKey        string
	TelemetryCABundle         string
	TelemetryServerName       string
	TelemetryIssuerName       string
	TelemetryIssuerGeneration int32
	TelemetryIngestGRPC       string
	TelemetryIngestHTTP       string
	TelemetryEnrollment       string
	OtelcolKubernetesImage    string
	TelemetryEnabled          bool
	KeyWrappingMode           string
	OpenBaoAddress            string
	OpenBaoToken              string
	OpenBaoTransitKey         string
	BreakGlassEnabled         bool
}

func LoadServer() Server {
	address := os.Getenv("ARGUS_HTTP_ADDRESS")
	if address == "" {
		address = defaultHTTPAddress
	}

	secureCookies, _ := strconv.ParseBool(os.Getenv("ARGUS_SECURE_COOKIES"))
	breakGlassEnabled, _ := strconv.ParseBool(os.Getenv("ARGUS_BREAK_GLASS_ENABLED"))
	key, _ := base64.RawURLEncoding.DecodeString(os.Getenv("ARGUS_IDEMPOTENCY_ENCRYPTION_KEY"))
	cursorKey, _ := base64.RawURLEncoding.DecodeString(os.Getenv("ARGUS_CURSOR_SIGNING_KEY"))
	pendingActionKey, _ := base64.RawURLEncoding.DecodeString(os.Getenv("ARGUS_PENDING_ACTION_ENCRYPTION_KEY"))
	issuerGeneration, _ := strconv.ParseInt(valueOrDefault("ARGUS_CONNECTOR_ISSUER_GENERATION", "1"), 10, 32)
	telemetryIssuerGeneration, _ := strconv.ParseInt(valueOrDefault("ARGUS_TELEMETRY_ISSUER_GENERATION", "1"), 10, 32)
	telemetryEnabled, _ := strconv.ParseBool(valueOrDefault("ARGUS_TELEMETRY_TOOL_CATALOG_ENABLED", "true"))
	return Server{
		Address:                   address,
		DatabaseURL:               os.Getenv("ARGUS_DATABASE_URL"),
		RedisURL:                  os.Getenv("ARGUS_REDIS_URL"),
		SetupTokenPath:            valueOrDefault("ARGUS_SETUP_TOKEN_PATH", "/var/run/secrets/argus/setup/token"),
		SetupTokenExpiresPath:     valueOrDefault("ARGUS_SETUP_TOKEN_EXPIRES_PATH", "/var/run/secrets/argus/setup/expires-at"),
		AllowedOrigins:            splitList(os.Getenv("ARGUS_ALLOWED_ORIGINS")),
		SecureCookies:             secureCookies,
		IdempotencyEncryptionKey:  key,
		CursorSigningKey:          cursorKey,
		PendingActionKey:          pendingActionKey,
		SecretKEKPath:             valueOrDefault("ARGUS_SECRET_KEK_PATH", "/var/run/secrets/argus/secret-kek/keyring.json"),
		ConnectorEnrollmentURL:    os.Getenv("ARGUS_CONNECTOR_ENROLLMENT_URL"),
		ConnectorGatewayAddress:   os.Getenv("ARGUS_CONNECTOR_GATEWAY_ADDRESS"),
		KubeconfigPath:            os.Getenv("ARGUS_KUBECONFIG"),
		SystemNamespace:           valueOrDefault("ARGUS_SYSTEM_NAMESPACE", "argus-system"),
		ConnectorIssuerName:       valueOrDefault("ARGUS_CONNECTOR_ISSUER_NAME", "argus-connector-ca"),
		ConnectorIssuerGeneration: int32(issuerGeneration),
		DeploymentProfile:         valueOrDefault("ARGUS_DEPLOYMENT_PROFILE", "evaluation"),
		DirectAdvertisedEgress:    splitList(os.Getenv("ARGUS_DIRECT_ADVERTISED_EGRESS")),
		DirectVerificationURL:     os.Getenv("ARGUS_DIRECT_VERIFICATION_URL"),
		DirectDeniedCIDRs:         splitList(os.Getenv("ARGUS_DIRECT_DENIED_CIDRS")),
		DirectExecutorEndpoint:    os.Getenv("ARGUS_DIRECT_EXECUTOR_ENDPOINT"),
		DirectExecutorServerName:  os.Getenv("ARGUS_DIRECT_EXECUTOR_SERVER_NAME"),
		DirectExecutorTLSCert:     valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_CERT_PATH", "/var/run/secrets/argus/direct-executor-client/tls.crt"),
		DirectExecutorTLSKey:      valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_KEY_PATH", "/var/run/secrets/argus/direct-executor-client/tls.key"),
		DirectExecutorCABundle:    valueOrDefault("ARGUS_DIRECT_EXECUTOR_CA_PATH", "/var/run/secrets/argus/direct-executor-ca/ca.crt"),
		SessionIdleTTL:            30 * time.Minute,
		SessionAbsoluteTTL:        12 * time.Hour,
		CardPresentationTTL:       durationOrDefault("ARGUS_CARD_PRESENTATION_TTL", 10*time.Minute),
		CardValidationTTL:         durationOrDefault("ARGUS_CARD_VALIDATION_TTL", 30*time.Minute),
		CardRuntimeVersion:        valueOrDefault("ARGUS_CARD_RUNTIME_VERSION", "argus-card-runtime/v1"),
		CardMaxPresentationBytes:  intOrDefault("ARGUS_CARD_MAX_PRESENTATION_BYTES", 1024*1024),
		RemoteOrigin:              os.Getenv("ARGUS_REMOTE_ORIGIN"),
		RemoteUserLimit:           intOrDefault("ARGUS_REMOTE_USER_LIMIT", 3),
		RemoteHostLimit:           intOrDefault("ARGUS_REMOTE_HOST_LIMIT", 5),
		RemoteEnterpriseLimit:     intOrDefault("ARGUS_REMOTE_ENTERPRISE_LIMIT", 50),
		ObjectStoreURL:            os.Getenv("ARGUS_OBJECT_STORE_URL"),
		ObjectStoreBucket:         os.Getenv("ARGUS_OBJECT_STORE_BUCKET"),
		ObjectStoreAccess:         os.Getenv("ARGUS_OBJECT_STORE_ACCESS_KEY"),
		ObjectStoreSecret:         os.Getenv("ARGUS_OBJECT_STORE_SECRET_KEY"),
		TelemetryQueryEndpoint:    os.Getenv("ARGUS_TELEMETRY_QUERY_ENDPOINT"),
		TelemetryClientCert:       valueOrDefault("ARGUS_TELEMETRY_CLIENT_CERT_PATH", "/var/run/secrets/argus/telemetry-client/tls.crt"),
		TelemetryClientKey:        valueOrDefault("ARGUS_TELEMETRY_CLIENT_KEY_PATH", "/var/run/secrets/argus/telemetry-client/tls.key"),
		TelemetryCABundle:         valueOrDefault("ARGUS_TELEMETRY_CLIENT_CA_PATH", "/var/run/secrets/argus/telemetry-ca/ca.crt"),
		TelemetryServerName:       valueOrDefault("ARGUS_TELEMETRY_SERVER_NAME", "argus-telemetry-query"),
		TelemetryIssuerName:       valueOrDefault("ARGUS_TELEMETRY_ISSUER_NAME", "argus-telemetry-ca"),
		TelemetryIssuerGeneration: int32(telemetryIssuerGeneration),
		TelemetryIngestGRPC:       os.Getenv("ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT"),
		TelemetryIngestHTTP:       os.Getenv("ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT"),
		TelemetryEnrollment:       os.Getenv("ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT"),
		OtelcolKubernetesImage:    os.Getenv("ARGUS_OTELCOL_KUBERNETES_IMAGE"),
		TelemetryEnabled:          telemetryEnabled,
		KeyWrappingMode:           valueOrDefault("ARGUS_KEY_WRAPPING_MODE", "local_test"),
		OpenBaoAddress:            os.Getenv("ARGUS_OPENBAO_ADDRESS"),
		OpenBaoToken:              os.Getenv("ARGUS_OPENBAO_TOKEN"),
		OpenBaoTransitKey:         valueOrDefault("ARGUS_OPENBAO_TRANSIT_KEY", "argus-local-hardening"),
		BreakGlassEnabled:         breakGlassEnabled,
	}
}

func (cfg Server) Validate() error {
	if cfg.DatabaseURL == "" {
		return errors.New("ARGUS_DATABASE_URL is required")
	}
	if cfg.RedisURL == "" {
		return errors.New("ARGUS_REDIS_URL is required")
	}
	if len(cfg.AllowedOrigins) == 0 {
		return errors.New("ARGUS_ALLOWED_ORIGINS is required")
	}
	if len(cfg.IdempotencyEncryptionKey) != 32 {
		return errors.New("ARGUS_IDEMPOTENCY_ENCRYPTION_KEY must be 32 bytes in base64url form")
	}
	if len(cfg.CursorSigningKey) != 32 {
		return errors.New("ARGUS_CURSOR_SIGNING_KEY must be 32 bytes in base64url form")
	}
	if len(cfg.PendingActionKey) != 32 {
		return errors.New("ARGUS_PENDING_ACTION_ENCRYPTION_KEY must be 32 bytes in base64url form")
	}
	if cfg.KeyWrappingMode == "local_test" && cfg.SecretKEKPath == "" {
		return errors.New("ARGUS_SECRET_KEK_PATH is required")
	}
	if cfg.ConnectorEnrollmentURL == "" || cfg.ConnectorGatewayAddress == "" || cfg.SystemNamespace == "" || cfg.ConnectorIssuerName == "" || cfg.ConnectorIssuerGeneration < 1 {
		return errors.New("Connector enrollment, gateway, and cert-manager issuer configuration are required")
	}
	if cfg.DeploymentProfile == "production" && (len(cfg.DirectAdvertisedEgress) == 0 || cfg.DirectVerificationURL == "") {
		return errors.New("production requires ARGUS_DIRECT_ADVERTISED_EGRESS and ARGUS_DIRECT_VERIFICATION_URL")
	}
	if cfg.DirectExecutorEndpoint == "" || cfg.DirectExecutorServerName == "" || cfg.DirectExecutorTLSCert == "" || cfg.DirectExecutorTLSKey == "" || cfg.DirectExecutorCABundle == "" {
		return errors.New("Direct Executor endpoint and mTLS configuration are required")
	}
	if cfg.CardPresentationTTL <= 0 || cfg.CardPresentationTTL > time.Hour || cfg.CardValidationTTL <= 0 || cfg.CardValidationTTL > 24*time.Hour || cfg.CardRuntimeVersion == "" || cfg.CardMaxPresentationBytes <= 0 || cfg.CardMaxPresentationBytes > 1024*1024 {
		return errors.New("Card presentation, validation, runtime, and size configuration is invalid")
	}
	if cfg.RemoteOrigin == "" || cfg.RemoteUserLimit < 1 || cfg.RemoteHostLimit < 1 || cfg.RemoteEnterpriseLimit < 1 {
		return errors.New("remote access Origin and capacity limits are required")
	}
	if cfg.ObjectStoreURL == "" || cfg.ObjectStoreBucket == "" || cfg.ObjectStoreAccess == "" || cfg.ObjectStoreSecret == "" {
		return errors.New("remote access ObjectStore configuration is required")
	}
	if cfg.TelemetryEnabled {
		if cfg.TelemetryQueryEndpoint == "" || cfg.TelemetryClientCert == "" || cfg.TelemetryClientKey == "" || cfg.TelemetryCABundle == "" || cfg.TelemetryServerName == "" {
			return errors.New("telemetry internal query and mTLS configuration are required")
		}
		if cfg.TelemetryIssuerName == "" || cfg.TelemetryIssuerGeneration < 1 || cfg.TelemetryIngestGRPC == "" || cfg.TelemetryIngestHTTP == "" || cfg.TelemetryEnrollment == "" || cfg.OtelcolKubernetesImage == "" {
			return errors.New("telemetry Collector issuer and ingest endpoints are required")
		}
	}
	if cfg.KeyWrappingMode != "local_test" && cfg.KeyWrappingMode != "openbao_transit" {
		return errors.New("ARGUS_KEY_WRAPPING_MODE must be local_test or openbao_transit")
	}
	if cfg.DeploymentProfile == "local-hardening" && cfg.KeyWrappingMode != "openbao_transit" {
		return errors.New("local-hardening requires ARGUS_KEY_WRAPPING_MODE=openbao_transit")
	}
	if cfg.DeploymentProfile == "local-hardening" && !cfg.BreakGlassEnabled {
		return errors.New("local-hardening requires explicit ARGUS_BREAK_GLASS_ENABLED=true")
	}
	if cfg.KeyWrappingMode == "openbao_transit" && (cfg.OpenBaoAddress == "" || cfg.OpenBaoToken == "" || cfg.OpenBaoTransitKey == "") {
		return errors.New("OpenBao address, token, and Transit key are required")
	}
	return nil
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return parsed
}

func intOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func LoadHealthAddress() string {
	address := os.Getenv("ARGUS_HEALTH_ADDRESS")
	if address == "" {
		address = defaultHealthAddress
	}
	return address
}
