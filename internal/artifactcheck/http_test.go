package artifactcheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPCheckerUsesInternalOriginAndPreservesObjectPath(t *testing.T) {
	t.Parallel()
	requested := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested = request.Method + " " + request.URL.Path
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	checker, err := NewHTTPChecker(testCABundle(t), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = checker.Check(context.Background(), "https://artifacts.example.com/bucket/release/file"); err != nil {
		t.Fatal(err)
	}
	if requested != "HEAD /bucket/release/file" {
		t.Fatalf("probe request = %q", requested)
	}
}

func TestHTTPCheckerReportsMissingObject(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	checker, err := NewHTTPChecker(testCABundle(t), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = checker.Check(context.Background(), "https://artifacts.example.com/missing"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing object error = %v", err)
	}
}

func testCABundle(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "artifact test CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
