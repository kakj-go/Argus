package telemetry

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestBootstrapExchangeEnvelopeBindsTokenAndReturnsOriginalPayload(t *testing.T) {
	record := db.HostEnrollmentToken{ID: uuid.New(), EnterpriseID: uuid.New(), CollectorID: uuid.New(), PreallocatedHostID: uuid.New()}
	payload := BootstrapPayload{Mode: "install", CollectorID: record.CollectorID, HostID: record.PreallocatedHostID,
		EnrollmentToken: "first-enrollment-token", HasArtifact: true, Artifact: collectorArtifact{URI: "https://artifacts.invalid/collector.tgz"},
		ExpiresAt: time.Now().UTC().Add(time.Minute)}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	nonce, ciphertext, err := encryptBootstrapExchange(key, record, payload)
	if err != nil {
		t.Fatal(err)
	}
	record.ExchangeKeyVersion.Valid, record.ExchangeKeyVersion.Int32 = true, 1
	record.ExchangeNonce, record.ExchangeCiphertext = nonce, ciphertext
	actual, err := decryptBootstrapExchange(key, record)
	if err != nil {
		t.Fatal(err)
	}
	if actual.EnrollmentToken != payload.EnrollmentToken {
		t.Fatalf("enrollment token = %q, want original token", actual.EnrollmentToken)
	}
	record.ID = uuid.New()
	if _, err = decryptBootstrapExchange(key, record); err == nil {
		t.Fatal("envelope must not decrypt for another host enrollment token")
	}
}

func TestBootstrapSigningTrustFailsClosed(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := BootstrapPayload{HasArtifact: true, Artifact: collectorArtifact{SigningKeyID: "release-key",
		Signature: base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))}}
	service := SelfEnrollService{SigningPublicKeys: map[string]string{"release-key": base64.RawStdEncoding.EncodeToString(publicKey)}}
	if err = service.attachTrustedSigningKey(&payload); err != nil {
		t.Fatal(err)
	}
	payload.Artifact.Signature = ""
	if err = service.attachTrustedSigningKey(&payload); err == nil {
		t.Fatal("missing signature must fail closed")
	}
	payload.Artifact.Signature = base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	service.SigningPublicKeys = nil
	if err = service.attachTrustedSigningKey(&payload); err == nil {
		t.Fatal("missing trusted public key must fail closed")
	}
}

func TestUninstallCompletionTokenIsStableAndRecordBound(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	service := SelfEnrollService{BootstrapSecretKey: key}
	record := db.HostUninstallToken{ID: uuid.New(), EnterpriseID: uuid.New()}
	first := service.uninstallCompletionToken(record)
	if first == "" || first != service.uninstallCompletionToken(record) {
		t.Fatal("completion token must be deterministic for an idempotent exchange retry")
	}
	record.ID = uuid.New()
	if first == service.uninstallCompletionToken(record) {
		t.Fatal("completion token must be bound to the uninstall record")
	}
}
