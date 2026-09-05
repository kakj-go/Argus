package config

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
)

type DirectExecutor struct {
	GRPCAddress                   string
	HealthAddress                 string
	DatabaseURL                   string
	PendingActionKey              []byte
	SecretKEKPath                 string
	KeyWrappingMode               string
	OpenBaoAddress                string
	OpenBaoToken                  string
	OpenBaoTransitKey             string
	InstanceID                    string
	DeploymentProfile             string
	AdvertisedEgress              []string
	EgressVerificationURL         string
	DeniedCIDRs                   []string
	TLSCertificate                string
	TLSPrivateKey                 string
	ClientCABundle                string
	TrustBundlePath               string
	TrustBundleEpoch              int64
	AuthorizedClientURIs          []string
	TelemetryEnrollmentEndpoint   string
	TelemetryIngestGRPCEndpoint   string
	TelemetryIngestHTTPEndpoint   string
	TunnelForwardTarget           string
	TunnelIdentityForwardTarget   string
	ConnectorEnrollForwardTarget  string
	ConnectorGatewayForwardTarget string
	SystemNamespace               string
	OtelcolArtifactCABundle       string
	TelemetryEnabled              bool
	TelemetryTunnelLimit          int
	ControlTunnelLimit            int
	TunnelBytesPerSecond          int64
}

func LoadDirectExecutor() DirectExecutor {
	instanceID := valueOrDefault("ARGUS_DIRECT_EXECUTOR_INSTANCE_ID", "argus-direct-executor")
	profile := valueOrDefault("ARGUS_DEPLOYMENT_PROFILE", "evaluation")
	telemetryEnabled, _ := strconv.ParseBool(valueOrDefault("ARGUS_TELEMETRY_TOOL_CATALOG_ENABLED", "true"))
	trustBundleEpoch, _ := strconv.ParseInt(valueOrDefault("ARGUS_TRUST_BUNDLE_EPOCH", "1"), 10, 64)
	pendingActionKey, _ := base64.RawURLEncoding.DecodeString(os.Getenv("ARGUS_PENDING_ACTION_ENCRYPTION_KEY"))
	telemetryTunnelLimit, _ := strconv.Atoi(tunnelLimitValue(profile, "ARGUS_TELEMETRY_TUNNEL_LIMIT", "64"))
	controlTunnelLimit, _ := strconv.Atoi(tunnelLimitValue(profile, "ARGUS_CONTROL_TUNNEL_LIMIT", "32"))
	tunnelBytesPerSecond, _ := strconv.ParseInt(tunnelLimitValue(profile, "ARGUS_TUNNEL_BYTES_PER_SECOND", "67108864"), 10, 64)
	return DirectExecutor{GRPCAddress: valueOrDefault("ARGUS_DIRECT_EXECUTOR_GRPC_ADDRESS", ":9444"), HealthAddress: LoadHealthAddress(), DatabaseURL: os.Getenv("ARGUS_DATABASE_URL"),
		PendingActionKey: pendingActionKey,
		SecretKEKPath:    valueOrDefault("ARGUS_SECRET_KEK_PATH", "/var/run/secrets/argus/secret-kek/keyring.json"), InstanceID: instanceID,
		KeyWrappingMode: valueOrDefault("ARGUS_KEY_WRAPPING_MODE", "local_test"), OpenBaoAddress: os.Getenv("ARGUS_OPENBAO_ADDRESS"),
		OpenBaoToken: os.Getenv("ARGUS_OPENBAO_TOKEN"), OpenBaoTransitKey: valueOrDefault("ARGUS_OPENBAO_TRANSIT_KEY", "argus-local-hardening"),
		DeploymentProfile: profile, AdvertisedEgress: splitList(os.Getenv("ARGUS_DIRECT_ADVERTISED_EGRESS")),
		EgressVerificationURL: os.Getenv("ARGUS_DIRECT_VERIFICATION_URL"), DeniedCIDRs: splitList(os.Getenv("ARGUS_DIRECT_DENIED_CIDRS")),
		TLSCertificate:   valueOrDefault("ARGUS_DIRECT_EXECUTOR_TLS_CERT_PATH", "/var/run/secrets/argus/direct-executor/tls.crt"),
		TLSPrivateKey:    valueOrDefault("ARGUS_DIRECT_EXECUTOR_TLS_KEY_PATH", "/var/run/secrets/argus/direct-executor/tls.key"),
		ClientCABundle:   valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_CA_PATH", "/var/run/secrets/argus/trust/ca.crt"),
		TrustBundlePath:  valueOrDefault("ARGUS_TRUST_BUNDLE_PATH", "/var/run/secrets/argus/trust/ca.crt"),
		TrustBundleEpoch: trustBundleEpoch,
		AuthorizedClientURIs: splitList(valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_URIS",
			"spiffe://argus.io/services/server/client,spiffe://argus.io/services/worker/client,spiffe://argus.io/services/connector-gateway/direct-executor-client")),
		TelemetryEnrollmentEndpoint:   os.Getenv("ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT"),
		TelemetryIngestGRPCEndpoint:   os.Getenv("ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT"),
		TelemetryIngestHTTPEndpoint:   os.Getenv("ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT"),
		TunnelForwardTarget:           os.Getenv("ARGUS_TELEMETRY_INGEST_FORWARD_ENDPOINT"),
		TunnelIdentityForwardTarget:   os.Getenv("ARGUS_TELEMETRY_IDENTITY_FORWARD_ENDPOINT"),
		ConnectorEnrollForwardTarget:  os.Getenv("ARGUS_CONNECTOR_ENROLLMENT_FORWARD_TARGET"),
		ConnectorGatewayForwardTarget: os.Getenv("ARGUS_CONNECTOR_GATEWAY_FORWARD_TARGET"),
		SystemNamespace:               valueOrDefault("ARGUS_SYSTEM_NAMESPACE", "argus-system"),
		OtelcolArtifactCABundle:       os.Getenv("ARGUS_OTELCOL_ARTIFACT_CA_PATH"),
		TelemetryEnabled:              telemetryEnabled,
		TelemetryTunnelLimit:          telemetryTunnelLimit, ControlTunnelLimit: controlTunnelLimit, TunnelBytesPerSecond: tunnelBytesPerSecond}
}

func (cfg DirectExecutor) Validate() error {
	mode := cfg.KeyWrappingMode
	if mode == "" {
		mode = "local_test"
	}
	if cfg.GRPCAddress == "" || cfg.HealthAddress == "" || cfg.DatabaseURL == "" || cfg.InstanceID == "" || len(cfg.PendingActionKey) != 32 {
		return errors.New("direct executor gRPC, database, keyring, health address, and instance ID are required")
	}
	if mode == "local_test" && cfg.SecretKEKPath == "" {
		return errors.New("direct executor local keyring path is required")
	}
	if mode != "local_test" && mode != "openbao_transit" {
		return errors.New("direct executor key wrapping mode is invalid")
	}
	if mode == "openbao_transit" && (cfg.OpenBaoAddress == "" || cfg.OpenBaoToken == "" || cfg.OpenBaoTransitKey == "") {
		return errors.New("direct executor OpenBao Transit configuration is required")
	}
	if cfg.TLSCertificate == "" || cfg.TLSPrivateKey == "" || cfg.ClientCABundle == "" || cfg.TrustBundlePath == "" || cfg.TrustBundleEpoch < 1 || len(cfg.AuthorizedClientURIs) == 0 {
		return errors.New("direct executor mTLS files and authorized client URI identities are required")
	}
	if cfg.TelemetryEnabled && (cfg.TelemetryEnrollmentEndpoint == "" || cfg.TelemetryIngestGRPCEndpoint == "" || cfg.TelemetryIngestHTTPEndpoint == "") {
		return errors.New("direct executor telemetry Collector endpoints are required")
	}
	if cfg.TelemetryTunnelLimit <= 0 || cfg.ControlTunnelLimit <= 0 || cfg.TunnelBytesPerSecond <= 0 {
		return errors.New("direct executor tunnel limits must be positive")
	}
	if cfg.TelemetryEnabled && (cfg.TunnelForwardTarget == "" || cfg.TunnelIdentityForwardTarget == "") {
		return errors.New("direct executor telemetry tunnel forward targets are required")
	}
	if cfg.ConnectorEnrollForwardTarget == "" || cfg.ConnectorGatewayForwardTarget == "" {
		return errors.New("direct executor Connector control tunnel forward targets are required")
	}
	return nil
}

func tunnelLimitValue(profile, name, evaluationDefault string) string {
	if profile == "production" {
		return os.Getenv(name)
	}
	return valueOrDefault(name, evaluationDefault)
}
