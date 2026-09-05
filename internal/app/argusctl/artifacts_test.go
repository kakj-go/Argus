package argusctl

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredArtifactObjectAcceptsOnlyBundledBucket(t *testing.T) {
	cfg := &InstallConfig{Spec: InstallSpec{Exposure: Exposure{EnterpriseHost: "argus.example.com", ArtifactHost: "artifacts.example.com"}}}
	bucket, key, err := configuredArtifactObject(cfg, "https://artifacts.example.com/argus-collector-artifacts/releases/linux.tar.gz", collectorArtifactBucket)
	if err != nil || bucket != collectorArtifactBucket || key != "releases/linux.tar.gz" {
		t.Fatalf("object location = %q %q %v", bucket, key, err)
	}
	if _, _, err = configuredArtifactObject(cfg, "https://external.example.com/argus-collector-artifacts/releases/linux.tar.gz", collectorArtifactBucket); err == nil {
		t.Fatal("external artifact host was accepted as bundled storage")
	}
}

func TestVerifySignedArtifactRejectsMetadataDrift(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "artifact")
	raw := []byte("immutable release")
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:]))
	if err = verifySignedArtifact(path, hex.EncodeToString(digest[:]), signature, uint64(len(raw)), publicKey); err != nil {
		t.Fatal(err)
	}
	if err = verifySignedArtifact(path, hex.EncodeToString(digest[:]), signature, uint64(len(raw)+1), publicKey); err == nil {
		t.Fatal("byte-size drift was accepted")
	}
}

func TestSanitizeReleaseVersionIsStable(t *testing.T) {
	if got := sanitizeReleaseVersion("dev build@1"); got != "dev-build-1" {
		t.Fatalf("sanitized version = %q", got)
	}
}
