package connector

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestGenerateLocalIdentityUsesConnectorURIAndPrivatePermissions(t *testing.T) {
	connectorID := uuid.MustParse("018f47e2-9a4c-7b31-8acd-02a2475e8d2f")
	directory := filepath.Join(t.TempDir(), "identity")
	identity, err := GenerateLocalIdentity(directory, connectorID)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(identity.CSRPEM))
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(csr.URIs) != 1 || csr.URIs[0].String() != CertificateURI(connectorID) {
		t.Fatalf("unexpected CSR URI SAN: %v", csr.URIs)
	}
	for _, name := range []string{"connector-key.pem", "connector.csr.pem"} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode is %o", name, info.Mode().Perm())
		}
	}
}
