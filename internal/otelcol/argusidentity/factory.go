package argusidentity

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

var componentType = component.MustNewType("argus_identity")

type Config struct {
	CollectorID         string        `mapstructure:"collector_id"`
	EnrollmentEndpoint  string        `mapstructure:"enrollment_endpoint"`
	RotationEndpoint    string        `mapstructure:"rotation_endpoint"`
	EnrollmentTokenFile string        `mapstructure:"enrollment_token_file"`
	CertificateFile     string        `mapstructure:"certificate_file"`
	PrivateKeyFile      string        `mapstructure:"private_key_file"`
	CABundleFile        string        `mapstructure:"ca_bundle_file"`
	ServerCAFile        string        `mapstructure:"server_ca_file"`
	RotateBefore        time.Duration `mapstructure:"rotate_before"`
	CheckInterval       time.Duration `mapstructure:"check_interval"`
}

func (config *Config) Validate() error {
	if config.CollectorID == "" || config.EnrollmentEndpoint == "" || config.RotationEndpoint == "" ||
		config.EnrollmentTokenFile == "" || config.CertificateFile == "" || config.PrivateKeyFile == "" || config.CABundleFile == "" {
		return errors.New("argus_identity requires collector, endpoints, token, certificate, key, and CA paths")
	}
	if config.RotateBefore <= 0 || config.CheckInterval <= 0 || config.RotateBefore < config.CheckInterval {
		return errors.New("argus_identity rotation intervals are invalid")
	}
	return nil
}

func NewFactory() extension.Factory {
	return extension.NewFactory(componentType, createDefaultConfig, createExtension, component.StabilityLevelBeta)
}

func createDefaultConfig() component.Config {
	return &Config{
		EnrollmentTokenFile: "/var/lib/argus-otelcol/identity/enrollment-token",
		CertificateFile:     "/var/lib/argus-otelcol/identity/client.pem",
		PrivateKeyFile:      "/var/lib/argus-otelcol/identity/client-key.pem",
		CABundleFile:        "/var/lib/argus-otelcol/identity/ca.pem",
		RotateBefore:        8 * time.Hour,
		CheckInterval:       5 * time.Minute,
	}
}

func createExtension(_ context.Context, settings extension.Settings, raw component.Config) (extension.Extension, error) {
	config := raw.(*Config)
	return newIdentityExtension(*config, settings.Logger), nil
}
