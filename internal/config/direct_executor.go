package config

import (
	"errors"
	"os"
	"strconv"
)

type DirectExecutor struct {
	GRPCAddress                 string
	HealthAddress               string
	DatabaseURL                 string
	SecretKEKPath               string
	KeyWrappingMode             string
	OpenBaoAddress              string
	OpenBaoToken                string
	OpenBaoTransitKey           string
	InstanceID                  string
	DeploymentProfile           string
	AdvertisedEgress            []string
	EgressVerificationURL       string
	DeniedCIDRs                 []string
	TLSCertificate              string
	TLSPrivateKey               string
	ClientCABundle              string
	AuthorizedClientNames       []string
	TelemetryEnrollmentEndpoint string
	TelemetryIngestGRPCEndpoint string
	TelemetryIngestHTTPEndpoint string
	OtelcolArtifactCABundle     string
	// OtelcolArtifactTLSInsecure 仅跳过产物下载的传输层证书校验
	// (本地自签场景);产物 sha256 + ed25519 签名校验不受此开关影响。
	OtelcolArtifactTLSInsecure bool
	TelemetryEnabled           bool
}

func LoadDirectExecutor() DirectExecutor {
	instanceID := valueOrDefault("ARGUS_DIRECT_EXECUTOR_INSTANCE_ID", "argus-direct-executor")
	telemetryEnabled, _ := strconv.ParseBool(valueOrDefault("ARGUS_TELEMETRY_TOOL_CATALOG_ENABLED", "true"))
	return DirectExecutor{GRPCAddress: valueOrDefault("ARGUS_DIRECT_EXECUTOR_GRPC_ADDRESS", ":9444"), HealthAddress: LoadHealthAddress(), DatabaseURL: os.Getenv("ARGUS_DATABASE_URL"),
		SecretKEKPath: valueOrDefault("ARGUS_SECRET_KEK_PATH", "/var/run/secrets/argus/secret-kek/keyring.json"), InstanceID: instanceID,
		KeyWrappingMode: valueOrDefault("ARGUS_KEY_WRAPPING_MODE", "local_test"), OpenBaoAddress: os.Getenv("ARGUS_OPENBAO_ADDRESS"),
		OpenBaoToken: os.Getenv("ARGUS_OPENBAO_TOKEN"), OpenBaoTransitKey: valueOrDefault("ARGUS_OPENBAO_TRANSIT_KEY", "argus-local-hardening"),
		DeploymentProfile: valueOrDefault("ARGUS_DEPLOYMENT_PROFILE", "evaluation"), AdvertisedEgress: splitList(os.Getenv("ARGUS_DIRECT_ADVERTISED_EGRESS")),
		EgressVerificationURL: os.Getenv("ARGUS_DIRECT_VERIFICATION_URL"), DeniedCIDRs: splitList(os.Getenv("ARGUS_DIRECT_DENIED_CIDRS")),
		TLSCertificate:              valueOrDefault("ARGUS_DIRECT_EXECUTOR_TLS_CERT_PATH", "/var/run/secrets/argus/direct-executor/tls.crt"),
		TLSPrivateKey:               valueOrDefault("ARGUS_DIRECT_EXECUTOR_TLS_KEY_PATH", "/var/run/secrets/argus/direct-executor/tls.key"),
		ClientCABundle:              valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_CA_PATH", "/var/run/secrets/argus/direct-executor-ca/ca.crt"),
		AuthorizedClientNames:       splitList(valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_NAMES", "argus-server,argus-connector-gateway")),
		TelemetryEnrollmentEndpoint: os.Getenv("ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT"),
		TelemetryIngestGRPCEndpoint: os.Getenv("ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT"),
		TelemetryIngestHTTPEndpoint: os.Getenv("ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT"),
		OtelcolArtifactCABundle:     os.Getenv("ARGUS_OTELCOL_ARTIFACT_CA_PATH"),
		OtelcolArtifactTLSInsecure:  os.Getenv("ARGUS_OTELCOL_ARTIFACT_TLS_MODE") == "insecure", TelemetryEnabled: telemetryEnabled}
}

func (cfg DirectExecutor) Validate() error {
	mode := cfg.KeyWrappingMode
	if mode == "" {
		mode = "local_test"
	}
	if cfg.GRPCAddress == "" || cfg.HealthAddress == "" || cfg.DatabaseURL == "" || cfg.InstanceID == "" {
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
	if cfg.TLSCertificate == "" || cfg.TLSPrivateKey == "" || cfg.ClientCABundle == "" || len(cfg.AuthorizedClientNames) == 0 {
		return errors.New("direct executor mTLS files and authorized client names are required")
	}
	if cfg.TelemetryEnabled && (cfg.TelemetryEnrollmentEndpoint == "" || cfg.TelemetryIngestGRPCEndpoint == "" || cfg.TelemetryIngestHTTPEndpoint == "") {
		return errors.New("direct executor telemetry Collector endpoints are required")
	}
	return nil
}
