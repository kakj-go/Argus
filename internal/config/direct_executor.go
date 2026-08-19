package config

import (
	"errors"
	"os"
)

type DirectExecutor struct {
	GRPCAddress                 string
	HealthAddress               string
	DatabaseURL                 string
	SecretKEKPath               string
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
}

func LoadDirectExecutor() DirectExecutor {
	instanceID := valueOrDefault("ARGUS_DIRECT_EXECUTOR_INSTANCE_ID", "argus-direct-executor")
	return DirectExecutor{GRPCAddress: valueOrDefault("ARGUS_DIRECT_EXECUTOR_GRPC_ADDRESS", ":9444"), HealthAddress: LoadHealthAddress(), DatabaseURL: os.Getenv("ARGUS_DATABASE_URL"),
		SecretKEKPath: valueOrDefault("ARGUS_SECRET_KEK_PATH", "/var/run/secrets/argus/secret-kek/keyring.json"), InstanceID: instanceID,
		DeploymentProfile: valueOrDefault("ARGUS_DEPLOYMENT_PROFILE", "evaluation"), AdvertisedEgress: splitList(os.Getenv("ARGUS_DIRECT_ADVERTISED_EGRESS")),
		EgressVerificationURL: os.Getenv("ARGUS_DIRECT_VERIFICATION_URL"), DeniedCIDRs: splitList(os.Getenv("ARGUS_DIRECT_DENIED_CIDRS")),
		TLSCertificate:              valueOrDefault("ARGUS_DIRECT_EXECUTOR_TLS_CERT_PATH", "/var/run/secrets/argus/direct-executor/tls.crt"),
		TLSPrivateKey:               valueOrDefault("ARGUS_DIRECT_EXECUTOR_TLS_KEY_PATH", "/var/run/secrets/argus/direct-executor/tls.key"),
		ClientCABundle:              valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_CA_PATH", "/var/run/secrets/argus/direct-executor-ca/ca.crt"),
		AuthorizedClientNames:       splitList(valueOrDefault("ARGUS_DIRECT_EXECUTOR_CLIENT_NAMES", "argus-server,argus-connector-gateway")),
		TelemetryEnrollmentEndpoint: os.Getenv("ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT"),
		TelemetryIngestGRPCEndpoint: os.Getenv("ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT"),
		TelemetryIngestHTTPEndpoint: os.Getenv("ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT"),
		OtelcolArtifactCABundle:     os.Getenv("ARGUS_OTELCOL_ARTIFACT_CA_PATH")}
}

func (cfg DirectExecutor) Validate() error {
	if cfg.GRPCAddress == "" || cfg.HealthAddress == "" || cfg.DatabaseURL == "" || cfg.SecretKEKPath == "" || cfg.InstanceID == "" {
		return errors.New("direct executor gRPC, database, keyring, health address, and instance ID are required")
	}
	if cfg.TLSCertificate == "" || cfg.TLSPrivateKey == "" || cfg.ClientCABundle == "" || len(cfg.AuthorizedClientNames) == 0 {
		return errors.New("direct executor mTLS files and authorized client names are required")
	}
	if cfg.TelemetryEnrollmentEndpoint == "" || cfg.TelemetryIngestGRPCEndpoint == "" || cfg.TelemetryIngestHTTPEndpoint == "" {
		return errors.New("direct executor telemetry Collector endpoints are required")
	}
	if cfg.DeploymentProfile == "production" && (len(cfg.AdvertisedEgress) == 0 || cfg.EgressVerificationURL == "") {
		return errors.New("production Direct Executor requires advertised egress and a verification URL")
	}
	return nil
}
