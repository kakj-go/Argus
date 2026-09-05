package argusidentity

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

var componentType = component.MustNewType("argus_identity")

type Config struct {
	CollectorID           string        `mapstructure:"collector_id"`
	EnrollmentEndpoint    string        `mapstructure:"enrollment_endpoint"`
	RotationEndpoint      string        `mapstructure:"rotation_endpoint"`
	TrustBundleEndpoint   string        `mapstructure:"trust_bundle_endpoint"`
	DialAddress           string        `mapstructure:"dial_address"`
	EnrollmentTokenFile   string        `mapstructure:"enrollment_token_file"`
	CertificateFile       string        `mapstructure:"certificate_file"`
	PrivateKeyFile        string        `mapstructure:"private_key_file"`
	ServerCertificateFile string        `mapstructure:"server_certificate_file"`
	ServerPrivateKeyFile  string        `mapstructure:"server_private_key_file"`
	CABundleFile          string        `mapstructure:"ca_bundle_file"`
	ServerCAFile          string        `mapstructure:"server_ca_file"`
	TrustBundleStateFile  string        `mapstructure:"trust_bundle_state_file"`
	BootstrapIdentityDir  string        `mapstructure:"bootstrap_identity_directory"`
	RotateBefore          time.Duration `mapstructure:"rotate_before"`
	CheckInterval         time.Duration `mapstructure:"check_interval"`
}

func (config *Config) Validate() error {
	if config.CollectorID == "" || config.EnrollmentEndpoint == "" || config.RotationEndpoint == "" || config.TrustBundleEndpoint == "" ||
		config.EnrollmentTokenFile == "" || config.CertificateFile == "" || config.PrivateKeyFile == "" || config.ServerCertificateFile == "" ||
		config.ServerPrivateKeyFile == "" || config.CABundleFile == "" || config.TrustBundleStateFile == "" {
		return errors.New("argus_identity requires collector, endpoints, token, certificate, key, and CA paths")
	}
	if config.RotateBefore <= 0 || config.CheckInterval <= 0 || config.RotateBefore < config.CheckInterval {
		return errors.New("argus_identity rotation intervals are invalid")
	}
	if config.DialAddress != "" && !validIdentityDialAddress(config.DialAddress) {
		return errors.New("argus_identity dial address must be a loopback endpoint")
	}
	if config.BootstrapIdentityDir != "" && strings.TrimSpace(config.BootstrapIdentityDir) != config.BootstrapIdentityDir {
		return errors.New("argus_identity bootstrap identity directory is invalid")
	}
	return nil
}

func validIdentityDialAddress(value string) bool {
	host, rawPort, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	address, err := netip.ParseAddr(host)
	if err != nil || !address.IsLoopback() {
		return false
	}
	port, err := strconv.Atoi(rawPort)
	return err == nil && port >= 1 && port <= 65535
}

func NewFactory() extension.Factory {
	return extension.NewFactory(componentType, createDefaultConfig, createExtension, component.StabilityLevelBeta)
}

func createDefaultConfig() component.Config {
	return &Config{
		EnrollmentTokenFile:   "/var/lib/argus-otelcol/identity/enrollment-token",
		CertificateFile:       "/var/lib/argus-otelcol/identity/client.pem",
		PrivateKeyFile:        "/var/lib/argus-otelcol/identity/client-key.pem",
		ServerCertificateFile: "/var/lib/argus-otelcol/identity/server.pem",
		ServerPrivateKeyFile:  "/var/lib/argus-otelcol/identity/server-key.pem",
		CABundleFile:          "/var/lib/argus-otelcol/identity/ca.pem",
		TrustBundleStateFile:  "/var/lib/argus-otelcol/identity/trust-bundle.json",
		RotateBefore:          8 * time.Hour,
		CheckInterval:         5 * time.Minute,
	}
}

func createExtension(_ context.Context, settings extension.Settings, raw component.Config) (extension.Extension, error) {
	config := raw.(*Config)
	return newIdentityExtension(*config, settings.Logger), nil
}
