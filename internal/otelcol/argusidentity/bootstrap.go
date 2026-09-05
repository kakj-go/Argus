package argusidentity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// bootstrapIdentity copies Kubernetes Secret projections into the Collector's
// writable identity directory once per Pod. The source remains read-only and
// is never used directly for rotating private material.
func bootstrapIdentity(config Config) error {
	if config.BootstrapIdentityDir == "" {
		return nil
	}
	if _, err := os.Stat(config.CertificateFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect writable Collector identity: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(config.CertificateFile), 0o700); err != nil {
		return err
	}
	files := map[string]string{
		"client.pem":        config.CertificateFile,
		"client-key.pem":    config.PrivateKeyFile,
		"server.pem":        config.ServerCertificateFile,
		"server-key.pem":    config.ServerPrivateKeyFile,
		"ca.pem":            config.CABundleFile,
		"trust-bundle.json": config.TrustBundleStateFile,
	}
	for name, target := range files {
		value, err := os.ReadFile(filepath.Join(config.BootstrapIdentityDir, name))
		if err != nil {
			return fmt.Errorf("bootstrap Collector identity %s: %w", name, err)
		}
		if len(value) == 0 {
			return fmt.Errorf("bootstrap Collector identity %s is empty", name)
		}
		if err = writeAtomic(target, value, 0o600); err != nil {
			return err
		}
	}
	// A repair enrollment may provide a token in the same fixed Secret. It is
	// copied to writable state and removed there after one successful exchange.
	if token, err := os.ReadFile(filepath.Join(config.BootstrapIdentityDir, "enrollment-token")); err == nil && len(token) > 0 {
		if err = writeAtomic(config.EnrollmentTokenFile, token, 0o600); err != nil {
			return err
		}
	}
	return nil
}
