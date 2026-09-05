package argusdev

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const defaultConnectorArtifactBucket = "argus-connector-artifacts"

var immutableReleaseVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,126}$`)

type connectorPublishedManifest struct {
	SchemaVersion    string                       `json:"schema_version"`
	ReleaseID        uuid.UUID                    `json:"release_id"`
	Version          string                       `json:"version"`
	ManifestURI      string                       `json:"manifest_uri"`
	InstallScriptURI string                       `json:"install_script_uri"`
	SigningKeyID     string                       `json:"signing_key_id"`
	SigningPublicKey string                       `json:"signing_public_key"`
	Artifacts        []connectorPublishedArtifact `json:"artifacts"`
}

type connectorPublishedArtifact struct {
	Architecture string `json:"architecture"`
	URI          string `json:"uri"`
	SHA256       string `json:"sha256"`
	Signature    string `json:"signature"`
	SigningKeyID string `json:"signing_key_id"`
	ByteSize     int64  `json:"byte_size"`
}

func (a *App) runConnector(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "publish-artifacts" {
		return fmt.Errorf("%w: usage: argus-dev connector publish-artifacts --endpoint URL --access-key K --secret-key K --version VERSION [--database-url URL]", errUsage)
	}
	return a.publishConnectorArtifacts(ctx, args[1:])
}

func (a *App) publishConnectorArtifacts(ctx context.Context, args []string) error {
	flags, positional, err := parseFlags(args)
	if err != nil || len(positional) != 0 {
		return fmt.Errorf("%w: invalid connector publish arguments", errUsage)
	}
	endpoint, accessKey, secretKey, version := flags["endpoint"], flags["access-key"], flags["secret-key"], flags["version"]
	if version == "" {
		version = strings.TrimSpace(os.Getenv("ARGUS_VERSION"))
	}
	bucket := flags["bucket"]
	if bucket == "" {
		bucket = defaultConnectorArtifactBucket
	}
	publicBase := flags["public-base"]
	if publicBase == "" {
		publicBase = endpoint
	}
	keyID := flags["key-id"]
	if keyID == "" {
		keyID = defaultSigningKeyID
	}
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		accessKey == "" || secretKey == "" || !immutableReleaseVersion.MatchString(version) {
		return fmt.Errorf("%w: endpoint, credentials, and an immutable --version are required", errUsage)
	}
	privateKey, publicKey, keyID, err := a.loadSigningKey(keyID)
	if err != nil {
		return err
	}
	client, err := minio.New(parsed.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: parsed.Scheme == "https", Region: "us-east-1"})
	if err != nil {
		return err
	}
	releaseID := uuid.New()
	base := strings.TrimRight(publicBase, "/")
	manifestKey := "argus-connector/" + version + "/manifest.json"
	scriptKey := "argus-connector/" + version + "/install.sh"
	manifest := connectorPublishedManifest{SchemaVersion: "argus.connector_release/v1", ReleaseID: releaseID, Version: version,
		ManifestURI: base + "/" + bucket + "/" + manifestKey, InstallScriptURI: base + "/" + bucket + "/" + scriptKey,
		SigningKeyID: keyID, SigningPublicKey: base64.RawStdEncoding.EncodeToString(publicKey)}
	for _, architecture := range []string{"amd64", "arm64"} {
		path := filepath.Join(a.root, "build", "connector", "artifacts", "argus-connector-linux-"+architecture)
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "building Connector %s\n", architecture)
		if err = a.runner.Run(ctx, map[string]string{"GOOS": "linux", "GOARCH": architecture, "CGO_ENABLED": "0"},
			"go", "build", "-trimpath", "-ldflags", "-s -w", "-o", path, "./cmd/argus-connector"); err != nil {
			return err
		}
		digest, size, signature, signErr := signCollectorArtifact(privateKey, path)
		if signErr != nil {
			return signErr
		}
		objectKey := "argus-connector/" + version + "/linux-" + architecture
		if _, statErr := client.StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{}); statErr == nil {
			return fmt.Errorf("immutable Connector artifact already exists: %s/%s", bucket, objectKey)
		}
		if _, err = client.FPutObject(ctx, bucket, objectKey, path, minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, connectorPublishedArtifact{Architecture: architecture,
			URI: base + "/" + bucket + "/" + objectKey, SHA256: digest, Signature: signature, SigningKeyID: keyID, ByteSize: int64(size)})
	}
	scriptPath := filepath.Join(a.root, "deploy", "scripts", "connector-install.sh")
	if _, statErr := client.StatObject(ctx, bucket, scriptKey, minio.StatObjectOptions{}); statErr == nil {
		return fmt.Errorf("immutable Connector install script already exists: %s/%s", bucket, scriptKey)
	}
	if _, err = client.FPutObject(ctx, bucket, scriptKey, scriptPath, minio.PutObjectOptions{ContentType: "application/x-sh"}); err != nil {
		return err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(a.root, "build", "connector", "artifacts", "release-"+version+".json")
	if err = os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		return err
	}
	if _, statErr := client.StatObject(ctx, bucket, manifestKey, minio.StatObjectOptions{}); statErr == nil {
		return fmt.Errorf("immutable Connector manifest already exists: %s/%s", bucket, manifestKey)
	}
	if _, err = client.FPutObject(ctx, bucket, manifestKey, manifestPath, minio.PutObjectOptions{ContentType: "application/json"}); err != nil {
		return err
	}
	manifestHash := sha256.Sum256(encoded)
	databaseURL := flags["database-url"]
	if databaseURL == "" {
		databaseURL = os.Getenv("ARGUS_DATABASE_URL")
	}
	if databaseURL != "" {
		store, openErr := postgres.Open(ctx, databaseURL)
		if openErr != nil {
			return openErr
		}
		defer store.Close()
		if _, err = store.Queries.CreateConnectorReleaseVersion(ctx, db.CreateConnectorReleaseVersionParams{ID: releaseID,
			Version: version, Status: "active", Manifest: encoded, ManifestHash: manifestHash[:]}); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.stdout, "published Connector release %s (%s)\n", version, releaseID)
	fmt.Fprintf(a.stdout, "manifest: %s/%s/%s\n", base, bucket, manifestKey)
	fmt.Fprintf(a.stdout, "ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS={%q:%q}\n", keyID, base64.RawStdEncoding.EncodeToString(publicKey))
	if databaseURL == "" {
		fmt.Fprintf(a.stdout, "release manifest saved at %s; pass --database-url to register it as active\n", manifestPath)
	}
	return nil
}
