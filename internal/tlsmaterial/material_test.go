package tlsmaterial

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testCA struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

func TestServerMaterialReloadsAtomicallyAndRetainsLastValidSnapshot(t *testing.T) {
	directory := t.TempDir()
	ca := newTestCA(t, 1)
	certPath, keyPath, caPath := writeTestMaterial(t, directory, ca, 2, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	material, err := Load(Options{
		CertificatePath: certPath,
		PrivateKeyPath:  keyPath,
		CABundlePath:    caPath,
		Usage:           x509.ExtKeyUsageServerAuth,
		RequiredDNS:     []string{"service.argus.svc"},
		RequiredURIs:    []string{"spiffe://argus.io/services/test/server"},
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := material.ServerConfig(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if serial := serverSerial(t, configuration); serial != "2" {
		t.Fatalf("initial server serial = %s", serial)
	}

	certificatePEM, keyPEM := newTestLeaf(t, ca, 3, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	writeTestFile(t, certPath, certificatePEM)
	writeTestFile(t, keyPath, keyPEM)
	if err := material.Reload(); err != nil {
		t.Fatal(err)
	}
	if serial := serverSerial(t, configuration); serial != "3" {
		t.Fatalf("reloaded server serial = %s", serial)
	}

	writeTestFile(t, keyPath, []byte("not a key"))
	if err := material.Reload(); err == nil {
		t.Fatal("invalid projected update was accepted")
	}
	if serial := serverSerial(t, configuration); serial != "3" {
		t.Fatalf("invalid update replaced last valid serial with %s", serial)
	}
	if state := material.State(); state.Failures == 0 || state.LastError == "" {
		t.Fatalf("reload failure was not observable: %#v", state)
	}
}

func TestMaterialSupportsRootOverlapAndRetirement(t *testing.T) {
	directory := t.TempDir()
	oldCA := newTestCA(t, 10)
	newCA := newTestCA(t, 11)
	certPath, keyPath, caPath := writeTestMaterial(t, directory, oldCA, 12, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	material, err := Load(Options{CertificatePath: certPath, PrivateKeyPath: keyPath, CABundlePath: caPath,
		Usage: x509.ExtKeyUsageClientAuth, RequiredDNS: []string{"argus-client"}, RequiredURIs: []string{"spiffe://argus.io/services/test/client"}})
	if err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, caPath, append(append([]byte(nil), oldCA.pem...), newCA.pem...))
	if err := material.Reload(); err != nil {
		t.Fatalf("dual-trust Bundle rejected: %v", err)
	}
	certificatePEM, keyPEM := newTestLeaf(t, newCA, 13, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	writeTestFile(t, certPath, certificatePEM)
	writeTestFile(t, keyPath, keyPEM)
	if err := material.Reload(); err != nil {
		t.Fatalf("new-CA leaf rejected during overlap: %v", err)
	}
	writeTestFile(t, caPath, newCA.pem)
	if err := material.Reload(); err != nil {
		t.Fatalf("old CA retirement rejected after leaf rotation: %v", err)
	}
}

func TestMaterialRejectsMixedEKUAndWrongSAN(t *testing.T) {
	directory := t.TempDir()
	ca := newTestCA(t, 20)
	certPath, keyPath, caPath := writeTestMaterial(t, directory, ca, 21,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth})
	if _, err := Load(Options{CertificatePath: certPath, PrivateKeyPath: keyPath, CABundlePath: caPath,
		Usage: x509.ExtKeyUsageServerAuth, RequiredDNS: []string{"service.argus.svc"}}); err == nil {
		t.Fatal("mixed server/client EKU was accepted")
	}
	certificatePEM, keyPEM := newTestLeaf(t, ca, 22, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	writeTestFile(t, certPath, certificatePEM)
	writeTestFile(t, keyPath, keyPEM)
	if _, err := Load(Options{CertificatePath: certPath, PrivateKeyPath: keyPath, CABundlePath: caPath,
		Usage: x509.ExtKeyUsageServerAuth, RequiredDNS: []string{"wrong.argus.svc"}}); err == nil {
		t.Fatal("wrong DNS SAN was accepted")
	}
}

func serverSerial(t *testing.T, configuration *tls.Config) string {
	t.Helper()
	config := configuration.Clone()
	current, err := config.GetConfigForClient(nil)
	if err != nil || len(current.Certificates) != 1 || current.Certificates[0].Leaf == nil {
		t.Fatalf("read dynamic server certificate: %v", err)
	}
	return current.Certificates[0].Leaf.SerialNumber.String()
}

func newTestCA(t *testing.T, serial int64) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "test root"}, NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{certificate: certificate, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})}
}

func newTestLeaf(t *testing.T, ca testCA, serial int64, usages []x509.ExtKeyUsage) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	uri, _ := url.Parse("spiffe://argus.io/services/test/server")
	dnsName := "service.argus.svc"
	if len(usages) == 1 && usages[0] == x509.ExtKeyUsageClientAuth {
		uri, _ = url.Parse("spiffe://argus.io/services/test/client")
		dnsName = "argus-client"
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: dnsName}, NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(12 * time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: usages, DNSNames: []string{dnsName}, URIs: []*url.URL{uri}}
	raw, err := x509.CreateCertificate(rand.Reader, template, ca.certificate, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func writeTestMaterial(t *testing.T, directory string, ca testCA, serial int64, usages []x509.ExtKeyUsage) (string, string, string) {
	t.Helper()
	certificatePEM, keyPEM := newTestLeaf(t, ca, serial, usages)
	certPath := filepath.Join(directory, "tls.crt")
	keyPath := filepath.Join(directory, "tls.key")
	caPath := filepath.Join(directory, "ca.crt")
	writeTestFile(t, certPath, certificatePEM)
	writeTestFile(t, keyPath, keyPEM)
	writeTestFile(t, caPath, ca.pem)
	return certPath, keyPath, caPath
}

func writeTestFile(t *testing.T, path string, value []byte) {
	t.Helper()
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
}
