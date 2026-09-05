package trustbundle

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestParseCanonicalizesOrderAndRejectsDuplicate(t *testing.T) {
	now := time.Now().UTC()
	a := testCA(t, "a", now)
	b := testCA(t, "b", now)
	first, err := Parse(append(append([]byte{}, a...), b...), now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse(append(append([]byte{}, b...), a...), now)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 || string(first.PEM) != string(second.PEM) {
		t.Fatal("canonical Bundle depends on input order")
	}
	if _, err = Parse(append(append([]byte{}, a...), a...), now); err == nil {
		t.Fatal("duplicate CA was accepted")
	}
}

func TestBundleMatchesVersionHashAndFingerprints(t *testing.T) {
	material, err := Parse(testCA(t, "root", time.Now().UTC()), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	bundle := Bundle{Epoch: 7, Material: material}
	if !bundle.Matches(Acknowledgement{Epoch: 7, SHA256: material.SHA256, Fingerprints: material.Fingerprints}) {
		t.Fatal("valid acknowledgement did not match")
	}
	if bundle.Matches(Acknowledgement{Epoch: 6, SHA256: material.SHA256, Fingerprints: material.Fingerprints}) {
		t.Fatal("stale acknowledgement matched")
	}
}

func testCA(t *testing.T, commonName string, now time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(int64(len(commonName) + 1)), Subject: pkix.Name{CommonName: commonName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
