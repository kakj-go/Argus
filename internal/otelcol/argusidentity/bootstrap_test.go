package argusidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapIdentityCopiesSecretProjectionIntoWritableState(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "bootstrap")
	target := filepath.Join(root, "state")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"client.pem", "client-key.pem", "server.pem", "server-key.pem", "ca.pem", "trust-bundle.json", "enrollment-token"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("value-"+name), 0o440); err != nil {
			t.Fatal(err)
		}
	}
	config := Config{BootstrapIdentityDir: source, CertificateFile: filepath.Join(target, "client.pem"),
		PrivateKeyFile: filepath.Join(target, "client-key.pem"), ServerCertificateFile: filepath.Join(target, "server.pem"),
		ServerPrivateKeyFile: filepath.Join(target, "server-key.pem"), CABundleFile: filepath.Join(target, "ca.pem"),
		TrustBundleStateFile: filepath.Join(target, "trust-bundle.json"), EnrollmentTokenFile: filepath.Join(target, "enrollment-token")}
	if err := bootstrapIdentity(config); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{config.CertificateFile, config.PrivateKeyFile, config.ServerCertificateFile, config.ServerPrivateKeyFile,
		config.CABundleFile, config.TrustBundleStateFile, config.EnrollmentTokenFile} {
		value, err := os.ReadFile(path)
		if err != nil || len(value) == 0 {
			t.Fatalf("bootstrap file %s is unavailable: %v", path, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("bootstrap file %s has unsafe permissions: %v", path, err)
		}
	}
}

func TestValidKubernetesName(t *testing.T) {
	for _, value := range []string{"argus-telemetry", "a", "a1"} {
		if !validKubernetesName(value) {
			t.Fatalf("valid Kubernetes name %q rejected", value)
		}
	}
	for _, value := range []string{"", "Argus", "-argus", "argus-", "argus.telemetry"} {
		if validKubernetesName(value) {
			t.Fatalf("invalid Kubernetes name %q accepted", value)
		}
	}
}
