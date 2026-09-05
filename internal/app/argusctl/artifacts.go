package argusctl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	collectorArtifactBucket = "argus-collector-artifacts"
	connectorArtifactBucket = "argus-connector-artifacts"
	signingKeyPath          = "deploy/.keys/otelcol-signing-key.json"
)

type connectorReleaseRegistration struct {
	ID                  uuid.UUID
	Version             string
	Manifest            json.RawMessage
	ManifestHash        []byte
	HostInstallerSHA256 string
}

type connectorInstallManifest struct {
	SchemaVersion       string                     `json:"schema_version"`
	ReleaseID           uuid.UUID                  `json:"release_id"`
	Version             string                     `json:"version"`
	ManifestURI         string                     `json:"manifest_uri"`
	InstallScriptURI    string                     `json:"install_script_uri"`
	InstallScriptSHA256 string                     `json:"install_script_sha256"`
	SigningKeyID        string                     `json:"signing_key_id"`
	SigningPublicKey    string                     `json:"signing_public_key"`
	Artifacts           []connectorInstallArtifact `json:"artifacts"`
}

type connectorInstallArtifact struct {
	Architecture string `json:"architecture"`
	URI          string `json:"uri"`
	SHA256       string `json:"sha256"`
	Signature    string `json:"signature"`
	SigningKeyID string `json:"signing_key_id"`
	ByteSize     int64  `json:"byte_size"`
}

type persistedSigningKey struct {
	KeyID      string `json:"key_id"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

func (a *App) buildConnectorDistributionArtifacts(ctx context.Context, root string) error {
	directory := filepath.Join(root, "build", "connector", "artifacts")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		path := filepath.Join(directory, "argus-connector-linux-"+architecture)
		_, _ = fmt.Fprintf(a.stdout, "Building Connector distribution for linux/%s\n", architecture)
		if _, err := a.runner.runEnv(ctx, map[string]string{"GOOS": "linux", "GOARCH": architecture, "CGO_ENABLED": "0"}, nil,
			"go", "build", "-trimpath", "-ldflags", "-s -w", "-o", path, "./cmd/argus-connector"); err != nil {
			return err
		}
	}
	return nil
}

// publishInstallArtifacts makes catalog metadata and object-store state one
// installation stage. The platform is not installed until every configured
// Collector object and a complete dual-architecture Connector release exist.
func (a *App) publishInstallArtifacts(
	ctx context.Context,
	cfg *InstallConfig,
	clients *kubeClients,
	root string,
	installationCredentials map[string]string,
) (connectorReleaseRegistration, error) {
	forward, err := portForwardService(ctx, clients, cfg.Spec.Namespaces.System, "argus-minio", 9000)
	if err != nil {
		return connectorReleaseRegistration{}, fmt.Errorf("connect to Artifact Store: %w", err)
	}
	defer func() { _ = forward.Stop() }()
	client, err := minio.New(fmt.Sprintf("127.0.0.1:%d", forward.localPort), &minio.Options{
		Creds: credentials.NewStaticV4(installationCredentials["minio-root-user"], installationCredentials["minio-root-password"], ""),
	})
	if err != nil {
		return connectorReleaseRegistration{}, err
	}
	for _, bucket := range []string{collectorArtifactBucket, connectorArtifactBucket} {
		if err = ensureDownloadBucket(ctx, client, bucket); err != nil {
			return connectorReleaseRegistration{}, err
		}
	}
	publicKey, privateKey, keyID, err := loadArtifactSigningKey(root, cfg.Spec.Telemetry.SigningKeyID, cfg.Spec.Telemetry.SigningPublicKey)
	if err != nil {
		return connectorReleaseRegistration{}, err
	}
	hostInstallerSHA256, err := publishCollectorArtifacts(ctx, client, cfg, root, publicKey)
	if err != nil {
		return connectorReleaseRegistration{}, err
	}
	registration, err := publishConnectorArtifacts(ctx, client, cfg, root, privateKey, publicKey, keyID)
	if err != nil {
		return connectorReleaseRegistration{}, err
	}
	registration.HostInstallerSHA256 = hostInstallerSHA256
	_, _ = fmt.Fprintf(a.stdout, "Artifact Store synchronized: Collector distributions and Connector release %s\n", registration.Version)
	return registration, nil
}

func ensureDownloadBucket(ctx context.Context, client *minio.Client, bucket string) error {
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check Artifact Store bucket %s: %w", bucket, err)
	}
	if !exists {
		if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			return fmt.Errorf("create Artifact Store bucket %s: %w", bucket, err)
		}
	}
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
	if err = client.SetBucketPolicy(ctx, bucket, policy); err != nil {
		return fmt.Errorf("set download policy on Artifact Store bucket %s: %w", bucket, err)
	}
	return nil
}

func loadArtifactSigningKey(root, expectedKeyID, expectedPublicKey string) (ed25519.PublicKey, ed25519.PrivateKey, string, error) {
	if encoded := strings.TrimSpace(os.Getenv("ARGUS_OTELCOL_SIGNING_PRIVATE_KEY")); encoded != "" {
		privateKey, err := base64.RawStdEncoding.DecodeString(encoded)
		if err != nil || len(privateKey) != ed25519.PrivateKeySize {
			return nil, nil, "", errors.New("ARGUS_OTELCOL_SIGNING_PRIVATE_KEY must contain a base64 Ed25519 private key")
		}
		publicKey := ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey)
		if base64.RawStdEncoding.EncodeToString(publicKey) != expectedPublicKey || expectedKeyID == "" {
			return nil, nil, "", errors.New("ARGUS_OTELCOL_SIGNING_PRIVATE_KEY does not match spec.telemetry signingPublicKey")
		}
		return publicKey, ed25519.PrivateKey(privateKey), expectedKeyID, nil
	}
	raw, err := os.ReadFile(filepath.Join(root, signingKeyPath))
	if err != nil {
		return nil, nil, "", fmt.Errorf("read signing key %s: %w; the installation release bundle must include the key used by spec.telemetry", signingKeyPath, err)
	}
	var material persistedSigningKey
	if json.Unmarshal(raw, &material) != nil {
		return nil, nil, "", errors.New("artifact signing key file is invalid")
	}
	privateKey, privateErr := base64.RawStdEncoding.DecodeString(material.PrivateKey)
	publicKey, publicErr := base64.RawStdEncoding.DecodeString(material.PublicKey)
	if privateErr != nil || publicErr != nil || len(privateKey) != ed25519.PrivateKeySize || len(publicKey) != ed25519.PublicKeySize ||
		material.KeyID != expectedKeyID || material.PublicKey != expectedPublicKey || !bytes.Equal(ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey), publicKey) {
		return nil, nil, "", errors.New("artifact signing key does not match spec.telemetry signingKeyId/signingPublicKey")
	}
	return ed25519.PublicKey(publicKey), ed25519.PrivateKey(privateKey), material.KeyID, nil
}

func publishCollectorArtifacts(ctx context.Context, client *minio.Client, cfg *InstallConfig, root string, publicKey ed25519.PublicKey) (string, error) {
	items := []struct {
		uri, digest, signature, filename, contentType string
		size                                          uint64
	}{
		{cfg.Spec.Telemetry.LinuxARM64URI, cfg.Spec.Telemetry.LinuxARM64SHA256, cfg.Spec.Telemetry.LinuxARM64Signature, "argus-otelcol-linux-arm64.tar.gz", "application/gzip", cfg.Spec.Telemetry.LinuxARM64ByteSize},
		{cfg.Spec.Telemetry.LinuxAMD64URI, cfg.Spec.Telemetry.LinuxAMD64SHA256, cfg.Spec.Telemetry.LinuxAMD64Signature, "argus-otelcol-linux-amd64.tar.gz", "application/gzip", cfg.Spec.Telemetry.LinuxAMD64ByteSize},
		{cfg.Spec.Telemetry.WindowsAMD64URI, cfg.Spec.Telemetry.WindowsAMD64SHA256, cfg.Spec.Telemetry.WindowsAMD64Signature, "argus-otelcol-windows-amd64.zip", "application/zip", cfg.Spec.Telemetry.WindowsAMD64ByteSize},
	}
	for _, item := range items {
		if strings.TrimSpace(item.uri) == "" {
			continue
		}
		bucket, objectKey, err := configuredArtifactObject(cfg, item.uri, collectorArtifactBucket)
		if err != nil {
			return "", err
		}
		path := filepath.Join(root, "build", "otelcol", "artifacts", item.filename)
		if err = verifySignedArtifact(path, item.digest, item.signature, item.size, publicKey); err != nil {
			return "", fmt.Errorf("Collector artifact %s: %w", item.filename, err)
		}
		if err = uploadArtifactFile(ctx, client, bucket, objectKey, path, item.contentType); err != nil {
			return "", err
		}
	}
	scriptPath := filepath.Join(root, "deploy", "scripts", "host-install.sh")
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(script)
	if err = uploadArtifactFile(ctx, client, collectorArtifactBucket, "install/host.sh", scriptPath, "application/x-sh"); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest[:]), nil
}

func configuredArtifactObject(cfg *InstallConfig, rawURL, expectedBucket string) (string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil {
		return "", "", fmt.Errorf("artifact URI %q is invalid", rawURL)
	}
	artifactHost := strings.TrimSpace(cfg.Spec.Exposure.ArtifactHost)
	if artifactHost == "" {
		artifactHost = "artifacts." + parentDomain(cfg.Spec.Exposure.EnterpriseHost)
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), artifactHost) || len(segments) < 2 || segments[0] != expectedBucket {
		return "", "", fmt.Errorf("artifact URI %q must use bundled HTTPS host %s and bucket %s", rawURL, artifactHost, expectedBucket)
	}
	return segments[0], strings.Join(segments[1:], "/"), nil
}

func verifySignedArtifact(path, expectedDigest, encodedSignature string, expectedSize uint64, publicKey ed25519.PublicKey) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read release file: %w", err)
	}
	digest := sha256.Sum256(raw)
	signature, signatureErr := base64.RawStdEncoding.DecodeString(encodedSignature)
	if hex.EncodeToString(digest[:]) != strings.ToLower(expectedDigest) || uint64(len(raw)) != expectedSize || signatureErr != nil ||
		len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, digest[:], signature) {
		return errors.New("file does not match configured sha256, size, and Ed25519 signature")
	}
	return nil
}

func uploadArtifactFile(ctx context.Context, client *minio.Client, bucket, objectKey, path, contentType string) error {
	info, err := client.FPutObject(ctx, bucket, objectKey, path, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("upload %s/%s: %w", bucket, objectKey, err)
	}
	stat, err := os.Stat(path)
	if err != nil || info.Size != stat.Size() {
		return fmt.Errorf("verify upload %s/%s: byte size mismatch", bucket, objectKey)
	}
	return nil
}

func publishConnectorArtifacts(
	ctx context.Context,
	client *minio.Client,
	cfg *InstallConfig,
	root string,
	privateKey ed25519.PrivateKey,
	publicKey ed25519.PublicKey,
	keyID string,
) (connectorReleaseRegistration, error) {
	artifacts := make([]connectorInstallArtifact, 0, 2)
	fingerprint := sha256.New()
	_, _ = fingerprint.Write([]byte("argus.connector_release/v2\x00" + cfg.Spec.Images.Tag + "\x00" + keyID + "\x00"))
	for _, architecture := range []string{"amd64", "arm64"} {
		path := filepath.Join(root, "build", "connector", "artifacts", "argus-connector-linux-"+architecture)
		raw, err := os.ReadFile(path)
		if err != nil {
			return connectorReleaseRegistration{}, fmt.Errorf("Connector %s artifact is missing; run argusctl images build first: %w", architecture, err)
		}
		digest := sha256.Sum256(raw)
		_, _ = fingerprint.Write(digest[:])
		artifacts = append(artifacts, connectorInstallArtifact{Architecture: architecture, SHA256: hex.EncodeToString(digest[:]),
			Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(privateKey, digest[:])), SigningKeyID: keyID, ByteSize: int64(len(raw))})
	}
	scriptPath := filepath.Join(root, "deploy", "scripts", "connector-install.sh")
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return connectorReleaseRegistration{}, err
	}
	scriptDigest := sha256.Sum256(script)
	_, _ = fingerprint.Write(scriptDigest[:])
	contentID := hex.EncodeToString(fingerprint.Sum(nil))
	versionPrefix := sanitizeReleaseVersion(cfg.Spec.Images.Tag)
	version := versionPrefix + "-" + contentID[:16]
	releaseID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("argus.connector_release/v2:"+contentID))
	base := "https://" + artifactHostname(cfg)
	manifestKey := "argus-connector/" + version + "/manifest.json"
	scriptKey := "argus-connector/" + version + "/install.sh"
	manifest := connectorInstallManifest{SchemaVersion: "argus.connector_release/v2", ReleaseID: releaseID, Version: version,
		ManifestURI:         base + "/" + connectorArtifactBucket + "/" + manifestKey,
		InstallScriptURI:    base + "/" + connectorArtifactBucket + "/" + scriptKey,
		InstallScriptSHA256: hex.EncodeToString(scriptDigest[:]),
		SigningKeyID:        keyID, SigningPublicKey: base64.RawStdEncoding.EncodeToString(publicKey), Artifacts: artifacts}
	for index := range manifest.Artifacts {
		objectKey := "argus-connector/" + version + "/linux-" + manifest.Artifacts[index].Architecture
		manifest.Artifacts[index].URI = base + "/" + connectorArtifactBucket + "/" + objectKey
		path := filepath.Join(root, "build", "connector", "artifacts", "argus-connector-linux-"+manifest.Artifacts[index].Architecture)
		if err = uploadArtifactFile(ctx, client, connectorArtifactBucket, objectKey, path, "application/octet-stream"); err != nil {
			return connectorReleaseRegistration{}, err
		}
	}
	if err = uploadArtifactFile(ctx, client, connectorArtifactBucket, scriptKey, scriptPath, "application/x-sh"); err != nil {
		return connectorReleaseRegistration{}, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return connectorReleaseRegistration{}, err
	}
	if _, err = client.PutObject(ctx, connectorArtifactBucket, manifestKey, bytes.NewReader(encoded), int64(len(encoded)), minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
		return connectorReleaseRegistration{}, fmt.Errorf("upload Connector manifest: %w", err)
	}
	manifestHash := sha256.Sum256(encoded)
	return connectorReleaseRegistration{ID: releaseID, Version: version, Manifest: encoded, ManifestHash: manifestHash[:]}, nil
}

func artifactHostname(cfg *InstallConfig) string {
	if value := strings.TrimSpace(cfg.Spec.Exposure.ArtifactHost); value != "" {
		return value
	}
	return "artifacts." + parentDomain(cfg.Spec.Exposure.EnterpriseHost)
}

func sanitizeReleaseVersion(value string) string {
	var output strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-' {
			output.WriteRune(char)
		} else {
			output.WriteByte('-')
		}
	}
	result := strings.Trim(output.String(), "-._")
	if result == "" {
		return "release"
	}
	if len(result) > 96 {
		return result[:96]
	}
	return result
}

func registerConnectorRelease(
	ctx context.Context,
	clients *kubeClients,
	cfg *InstallConfig,
	installationCredentials map[string]string,
	release connectorReleaseRegistration,
) error {
	forward, err := portForwardService(ctx, clients, cfg.Spec.Namespaces.System, "argus-postgresql", 5432)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL for Connector catalog sync: %w", err)
	}
	defer func() { _ = forward.Stop() }()
	databaseURL := &url.URL{Scheme: "postgres", User: url.UserPassword("argus", installationCredentials["postgresql-password"]),
		Host: fmt.Sprintf("127.0.0.1:%d", forward.localPort), Path: "/argus", RawQuery: "sslmode=disable"}
	store, err := postgres.Open(ctx, databaseURL.String())
	if err != nil {
		return err
	}
	defer store.Close()
	created, err := store.Queries.CreateConnectorReleaseVersion(ctx, db.CreateConnectorReleaseVersionParams{ID: release.ID,
		Version: release.Version, Status: "active", Manifest: release.Manifest, ManifestHash: release.ManifestHash})
	if err != nil {
		return fmt.Errorf("register immutable Connector release %s: %w", release.Version, err)
	}
	active, err := store.Queries.GetActiveConnectorReleaseVersion(ctx)
	if err != nil || active.ID != created.ID || !bytes.Equal(active.ManifestHash, release.ManifestHash) {
		return fmt.Errorf("Connector release %s was registered but did not become the active catalog version", release.Version)
	}
	return nil
}
