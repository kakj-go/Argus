package argusdev

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// E2EConnectorArtifacts is the immutable, signed release used by every E2E
// flow that can create a Bastion enrollment. Keeping the release in the same
// HTTPS fixture as Collector artifacts exercises the production download and
// trust path without introducing a development-only installer fallback.
type E2EConnectorArtifacts struct {
	ReleaseID    uuid.UUID
	Version      string
	Manifest     connectorPublishedManifest
	ManifestJSON []byte
	ManifestHash [sha256.Size]byte
}

func (a *App) prepareE2EArtifactServer(env *E2EEnvironment) error {
	if !suiteFixtureFeatures(env.Options.Suite).Artifact {
		return nil
	}
	staging := filepath.Join(a.root, "build", "e2e-artifacts")
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(staging, "install"), 0o755); err != nil {
		return err
	}
	if err := copyE2EArtifact(filepath.Join(a.root, "deploy", "scripts", "host-install.sh"), filepath.Join(staging, "install", "host.sh")); err != nil {
		return err
	}
	serviceName := "argus-e2e-artifact-server"
	dnsName := serviceName + "." + env.SystemNS + ".svc"
	tls, err := generateFixtureCertificate(serviceName, []string{serviceName, dnsName}, nil)
	if err != nil {
		return err
	}
	env.ArtifactTLS = tls
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	env.ArtifactSigning = &E2EArtifactSigning{
		KeyID: "argus-e2e-" + env.Options.RunID, PublicKey: publicKey, PrivateKey: privateKey,
	}
	return nil
}

func (a *App) prepareE2EConnectorArtifacts(ctx context.Context, env *E2EEnvironment) error {
	if !suiteHas(env.Options.Suite, "m3") && !suiteHas(env.Options.Suite, "m4") && env.Options.Suite != "p4" {
		return nil
	}
	if env.ArtifactSigning == nil || len(env.ArtifactSigning.PrivateKey) != ed25519.PrivateKeySize || len(env.ArtifactSigning.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("E2E artifact signing root is unavailable")
	}
	var err error
	runHash := sha256.Sum256([]byte(env.Options.RunID))
	version := "0.1.0-e2e-" + hex.EncodeToString(runHash[:6])
	releaseID := uuid.New()
	dnsName := "argus-e2e-artifact-server." + env.SystemNS + ".svc"
	baseURI := "https://" + dnsName + ":8443/connector/" + version
	keyID := env.ArtifactSigning.KeyID
	manifest := connectorPublishedManifest{
		SchemaVersion:    "argus.connector_release/v1",
		ReleaseID:        releaseID,
		Version:          version,
		ManifestURI:      baseURI + "/manifest.json",
		InstallScriptURI: baseURI + "/install.sh",
		SigningKeyID:     keyID,
		SigningPublicKey: base64.RawStdEncoding.EncodeToString(env.ArtifactSigning.PublicKey),
	}
	staging := filepath.Join(a.root, "build", "e2e-artifacts", "connector", version)
	if err = os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		binary := filepath.Join(a.root, "build", "connector", "artifacts", "argus-connector-linux-"+architecture)
		if err = os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
			return err
		}
		if err = a.runner.Run(ctx, map[string]string{"GOOS": "linux", "GOARCH": architecture, "CGO_ENABLED": "0"},
			"go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, "./cmd/argus-connector"); err != nil {
			return err
		}
		digest, size, signature, signErr := signCollectorArtifact(env.ArtifactSigning.PrivateKey, binary)
		if signErr != nil {
			return signErr
		}
		name := "linux-" + architecture
		if err = copyE2EArtifact(binary, filepath.Join(staging, name)); err != nil {
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, connectorPublishedArtifact{
			Architecture: architecture,
			URI:          baseURI + "/" + name,
			SHA256:       digest,
			Signature:    signature,
			SigningKeyID: keyID,
			ByteSize:     int64(size),
		})
	}
	if err = copyE2EArtifact(filepath.Join(a.root, "deploy", "scripts", "connector-install.sh"), filepath.Join(staging, "install.sh")); err != nil {
		return err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(staging, "manifest.json"), encoded, 0o644); err != nil {
		return err
	}
	env.ConnectorArtifacts = &E2EConnectorArtifacts{
		ReleaseID: releaseID, Version: version, Manifest: manifest,
		ManifestJSON: encoded, ManifestHash: sha256.Sum256(encoded),
	}
	return nil
}

func (a *App) registerE2EConnectorRelease(ctx context.Context, env *E2EEnvironment) error {
	release := env.ConnectorArtifacts
	if release == nil {
		return nil
	}
	manifest := base64.StdEncoding.EncodeToString(release.ManifestJSON)
	// Keep SELECT as the top-level statement. psql prints an INSERT command tag
	// even in tuples-only mode, which would otherwise be appended to the UUID.
	query := fmt.Sprintf(`WITH registered_release AS (
  INSERT INTO connector_release_versions (id,version,status,manifest,manifest_hash)
  VALUES ('%s','%s','active',convert_from(decode('%s','base64'),'UTF8')::jsonb,decode('%s','hex'))
  ON CONFLICT (version) DO UPDATE SET status='active',manifest=EXCLUDED.manifest,manifest_hash=EXCLUDED.manifest_hash,updated_at=now()
  RETURNING id
)
SELECT id FROM registered_release;`, release.ReleaseID, release.Version, manifest, hex.EncodeToString(release.ManifestHash[:]))
	value, err := a.postgresQuery(ctx, env, query)
	if err != nil {
		return err
	}
	registered, err := uuid.Parse(stringTrim(value))
	if err != nil {
		return fmt.Errorf("register E2E Connector release: %w", err)
	}
	env.State.Values["connector_release_id"] = registered.String()
	return writePrivate(filepath.Join(env.Options.Artifacts, "connector-release-manifest.json"), release.ManifestJSON)
}

func copyE2EArtifact(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func stringTrim(value string) string {
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\n' || value[0] == '\r' || value[0] == '\t') {
		value = value[1:]
	}
	for len(value) > 0 {
		last := value[len(value)-1]
		if last != ' ' && last != '\n' && last != '\r' && last != '\t' {
			break
		}
		value = value[:len(value)-1]
	}
	return value
}
