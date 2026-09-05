package collectormanager

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
	"github.com/kakj-go/Argus/internal/tlsmaterial"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

const (
	MaxArtifactBytes           = 256 << 20
	MaxConfigBytes             = 1 << 20
	localCollectorRoot         = "/var/lib/argus-otelcol"
	localCollectorReadyTimeout = 90 * time.Second
)

var (
	ErrInvalidCommand       = errors.New("collector management command is invalid")
	ErrUnsupportedPlatform  = errors.New("collector platform is not supported")
	ErrArtifactInvalid      = errors.New("collector artifact is invalid")
	ErrTargetAuthFailed     = errors.New("collector target SSH authentication failed")
	ErrTargetHostKeyChanged = errors.New("collector target SSH host key changed")
)

type Result struct {
	CollectorID       string
	EffectiveRevision uint64
	ConfigSHA256      string
	Status            string
	DiagnosticHash    string
}

type Manager struct {
	Root               string
	HTTPClient         *http.Client
	TrustedSigningKeys map[string]ed25519.PublicKey
	// ManageLocalService is enabled only by the Connector's connector_local
	// execution path. Direct and SSH managers retain their existing behavior.
	ManageLocalService bool
}

func NewArtifactHTTPClient(caBundlePath string) (*http.Client, error) {
	material, err := tlsmaterial.Load(tlsmaterial.Options{CABundlePath: caBundlePath})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactInvalid, err)
	}
	transport, err := tlsmaterial.NewHTTPTransport(material, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactInvalid, err)
	}
	return &http.Client{
		Timeout:   2 * time.Minute,
		Transport: transport,
		CheckRedirect: func(next *http.Request, previous []*http.Request) error {
			if len(previous) >= 3 || next.URL.Scheme != "https" || next.URL.Hostname() != previous[0].URL.Hostname() {
				return ErrArtifactInvalid
			}
			return nil
		},
	}, nil
}

type persistedState struct {
	SchemaVersion     string `json:"schema_version"`
	CollectorID       string `json:"collector_id"`
	ResourceID        string `json:"resource_id"`
	ResourceType      string `json:"resource_type"`
	EffectiveRevision uint64 `json:"effective_revision"`
	ConfigSHA256      string `json:"config_sha256"`
	ArtifactSHA256    string `json:"artifact_sha256"`
	RouteKind         string `json:"route_kind"`
	TrustBundleEpoch  uint64 `json:"trust_bundle_epoch"`
	TrustBundleSHA256 string `json:"trust_bundle_sha256"`
	UpdatedAt         string `json:"updated_at"`
}

func Validate(command *connectorv1.CollectorManagementCommand) error {
	if command == nil {
		return ErrInvalidCommand
	}
	if _, err := uuid.Parse(command.GetCollectorId()); err != nil {
		return ErrInvalidCommand
	}
	if _, err := uuid.Parse(command.GetResourceId()); err != nil {
		return ErrInvalidCommand
	}
	if command.GetCollectorVersion() < 1 || command.GetDesiredRevision() < 1 {
		return ErrInvalidCommand
	}
	switch command.GetOperation() {
	case "install", "configure", "upgrade", "repair", "uninstall":
	default:
		return ErrInvalidCommand
	}
	if command.GetResourceType() != "host" && command.GetResourceType() != "kubernetes_cluster" {
		return ErrInvalidCommand
	}
	if command.GetRouteKind() != "direct_argus" && command.GetRouteKind() != "bastion_gateway" {
		return ErrInvalidCommand
	}
	switch command.GetTransport() {
	case "direct":
	case "executor_tunnel":
		if command.GetRouteKind() != "direct_argus" {
			return ErrInvalidCommand
		}
	case "bastion_tunnel":
		if command.GetRouteKind() != "bastion_gateway" {
			return ErrInvalidCommand
		}
	default:
		return ErrInvalidCommand
	}
	if command.GetOperation() == "uninstall" {
		return nil
	}
	if _, err := commandTrustBundle(command); err != nil {
		return ErrInvalidCommand
	}
	if len(command.GetRenderedConfig()) == 0 || len(command.GetRenderedConfig()) > MaxConfigBytes || !validSHA256(command.GetConfigSha256()) {
		return ErrInvalidCommand
	}
	configHash := sha256.Sum256(command.GetRenderedConfig())
	if !strings.EqualFold(hex.EncodeToString(configHash[:]), command.GetConfigSha256()) {
		return ErrInvalidCommand
	}
	if _, err := configbundle.Extract(command.GetRenderedConfig(), "host"); err != nil {
		return ErrInvalidCommand
	}
	bundleCollectorID, err := configbundle.CollectorID(command.GetRenderedConfig())
	if err != nil || bundleCollectorID != command.GetCollectorId() {
		return ErrInvalidCommand
	}
	if command.GetResourceType() == "kubernetes_cluster" {
		image := strings.TrimSpace(command.GetKubernetesImage())
		if image == "" || strings.ContainsAny(image, " \t\r\n") || !strings.Contains(image, ":") {
			return ErrUnsupportedPlatform
		}
		if _, err := configbundle.Extract(command.GetRenderedConfig(), "kubernetes_agent"); err != nil {
			return ErrInvalidCommand
		}
		if _, err := configbundle.Extract(command.GetRenderedConfig(), "kubernetes_gateway"); err != nil {
			return ErrInvalidCommand
		}
		if len(command.GetImagePullSecrets()) > 16 {
			return ErrInvalidCommand
		}
		seen := make(map[string]struct{}, len(command.GetImagePullSecrets()))
		for _, name := range command.GetImagePullSecrets() {
			if len(validation.IsDNS1123Subdomain(name)) != 0 {
				return ErrInvalidCommand
			}
			if _, exists := seen[name]; exists {
				return ErrInvalidCommand
			}
			seen[name] = struct{}{}
		}
	}
	artifact := command.GetArtifact()
	// 产物平台必须与目标匹配:计划生成时已按主机 OS + 探测架构选定
	// (linux_amd64 / linux_arm64),此处只接受命令携带的合法 linux 产物;
	// windows 产物暂无 SSH 安装路径,维持拒绝。
	if artifact == nil || (artifact.GetPlatform() != "linux_arm64" && artifact.GetPlatform() != "linux_amd64") {
		return ErrUnsupportedPlatform
	}
	if _, err := uuid.Parse(artifact.GetDistributionVersionId()); err != nil {
		return ErrArtifactInvalid
	}
	parsed, err := url.Parse(artifact.GetUri())
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrArtifactInvalid
	}
	if !validSHA256(artifact.GetSha256()) || artifact.GetByteSize() < 1 || artifact.GetByteSize() > MaxArtifactBytes ||
		strings.TrimSpace(artifact.GetSignature()) == "" || strings.TrimSpace(artifact.GetSigningKeyId()) == "" {
		return ErrArtifactInvalid
	}
	return nil
}

func (manager Manager) ApplyLocal(ctx context.Context, command *connectorv1.CollectorManagementCommand) (Result, error) {
	if err := Validate(command); err != nil {
		return Result{}, err
	}
	root := manager.Root
	if root == "" {
		root = "/var/lib/argus-otelcol"
	}
	directory := filepath.Join(root, command.GetCollectorId())
	if command.GetOperation() == "uninstall" {
		if manager.ManageLocalService {
			if err := uninstallLocalCollectorService(ctx, root); err != nil {
				return Result{}, err
			}
		}
		if err := os.RemoveAll(directory); err != nil {
			return Result{}, err
		}
		return buildResult(command, "uninstalled"), nil
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Result{}, err
	}
	if err := manager.writeArtifactAtomic(ctx, filepath.Join(directory, "collector.artifact"), command.GetArtifact(), 0o500); err != nil {
		return Result{}, err
	}
	runtimeConfig, err := configbundle.Extract(command.GetRenderedConfig(), "host")
	if err != nil {
		return Result{}, err
	}
	if err = writeAtomic(filepath.Join(directory, "collector-config.yaml"), runtimeConfig, 0o600); err != nil {
		return Result{}, err
	}
	trust, err := commandTrustBundle(command)
	if err != nil {
		return Result{}, err
	}
	if err = writeAtomic(filepath.Join(directory, "server-ca.pem"), trust.PEM, 0o600); err != nil {
		return Result{}, err
	}
	if len(command.GetEnrollmentToken()) > 0 && !manager.ManageLocalService {
		if err = writeAtomic(filepath.Join(directory, "enrollment-token"), command.GetEnrollmentToken(), 0o600); err != nil {
			return Result{}, err
		}
	}
	state := persistedState{SchemaVersion: "argus.collector_state/v1", CollectorID: command.GetCollectorId(), ResourceID: command.GetResourceId(),
		ResourceType: command.GetResourceType(), EffectiveRevision: command.GetDesiredRevision(), ConfigSHA256: strings.ToLower(command.GetConfigSha256()),
		ArtifactSHA256: strings.ToLower(command.GetArtifact().GetSha256()), RouteKind: command.GetRouteKind(),
		TrustBundleEpoch: command.GetTrustBundleEpoch(), TrustBundleSHA256: strings.ToLower(command.GetTrustBundleSha256()),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	encoded, err := json.Marshal(state)
	if err != nil {
		return Result{}, err
	}
	if manager.ManageLocalService {
		if err = activateLocalCollectorService(ctx, command, root, directory); err != nil {
			return Result{}, err
		}
	}
	if err = writeAtomic(filepath.Join(directory, "state.json"), encoded, 0o600); err != nil {
		return Result{}, err
	}
	return buildResult(command, "converged"), nil
}

func activateLocalCollectorService(ctx context.Context, command *connectorv1.CollectorManagementCommand, root, directory string) error {
	if os.Geteuid() != 0 || root != localCollectorRoot || !validLocalCollectorRoot(root) {
		return ErrInvalidCommand
	}
	if err := installCollectorArchive(filepath.Join(directory, "collector.artifact"), "/usr/local/bin/argus-otelcol"); err != nil {
		return err
	}
	runtimeConfig, err := configbundle.Extract(command.GetRenderedConfig(), "host")
	if err != nil {
		return ErrInvalidCommand
	}
	trust, err := commandTrustBundle(command)
	if err != nil {
		return ErrInvalidCommand
	}
	if err = os.MkdirAll("/etc/argus-otelcol", 0o700); err != nil {
		return err
	}
	if err = writeAtomic("/etc/argus-otelcol/config.yaml", runtimeConfig, 0o600); err != nil {
		return err
	}
	if err = writeAtomic("/etc/argus-otelcol/server-ca.pem", trust.PEM, 0o600); err != nil {
		return err
	}
	if err = prepareLocalCollectorIdentity(ctx, root, command.GetCollectorId()); err != nil {
		return err
	}
	identityReady := validateLocalCollectorIdentity(root, command.GetCollectorId()) == nil
	if !identityReady && len(command.GetEnrollmentToken()) > 0 {
		if err = writeAtomic("/etc/argus-otelcol/enrollment-token", command.GetEnrollmentToken(), 0o600); err != nil {
			return err
		}
	} else if !identityReady {
		return ErrInvalidCommand
	} else if err = os.Remove("/etc/argus-otelcol/enrollment-token"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	unit := localCollectorSystemdUnit(command, root)
	if err = writeAtomic("/etc/systemd/system/argus-otelcol.service", []byte(unit), 0o644); err != nil {
		return err
	}
	for _, arguments := range [][]string{{"daemon-reload"}, {"enable", "--now", "argus-otelcol.service"}, {"is-active", "--quiet", "argus-otelcol.service"}} {
		if output, commandErr := exec.CommandContext(ctx, "systemctl", arguments...).CombinedOutput(); commandErr != nil {
			return fmt.Errorf("collector systemd %s: %w: %s", arguments[0], commandErr, strings.TrimSpace(string(output)))
		}
	}
	return waitLocalCollectorReady(ctx, root, command.GetCollectorId())
}

func prepareLocalCollectorIdentity(ctx context.Context, root, collectorID string) error {
	marker := filepath.Join(root, ".active-collector-id")
	current, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if strings.TrimSpace(string(current)) != collectorID {
		_, _ = exec.CommandContext(ctx, "systemctl", "disable", "--now", "argus-otelcol.service").CombinedOutput()
		if err = os.RemoveAll(filepath.Join(root, "identity")); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(filepath.Join(root, "identity"), 0o700); err != nil {
		return err
	}
	return writeAtomic(marker, []byte(collectorID), 0o600)
}

func waitLocalCollectorReady(ctx context.Context, root, collectorID string) error {
	readyCtx, cancel := context.WithTimeout(ctx, localCollectorReadyTimeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		identityErr := validateLocalCollectorIdentity(root, collectorID)
		_, tokenErr := os.Stat("/etc/argus-otelcol/enrollment-token")
		if identityErr == nil && errors.Is(tokenErr, os.ErrNotExist) && localCollectorHealthReady(readyCtx) {
			return nil
		}
		select {
		case <-readyCtx.Done():
			return readyCtx.Err()
		case <-ticker.C:
		}
	}
}

func localCollectorHealthReady(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", "127.0.0.1:13133")
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func validateLocalCollectorIdentity(root, collectorID string) error {
	directory := filepath.Join(root, "identity")
	certificatePEM, err := os.ReadFile(filepath.Join(directory, "client.pem"))
	if err != nil {
		return err
	}
	privateKeyPEM, err := os.ReadFile(filepath.Join(directory, "client-key.pem"))
	if err != nil {
		return err
	}
	caPEM, err := os.ReadFile(filepath.Join(directory, "ca.pem"))
	if err != nil {
		return err
	}
	keyPair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil || len(keyPair.Certificate) != 1 {
		return ErrInvalidCommand
	}
	certificate, err := x509.ParseCertificate(keyPair.Certificate[0])
	wantURI := "spiffe://argus/telemetry/collectors/" + collectorID
	if err != nil || time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) ||
		len(certificate.URIs) != 1 || certificate.URIs[0].String() != wantURI {
		return ErrInvalidCommand
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return ErrInvalidCommand
	}
	for _, name := range []string{"client.pem", "client-key.pem", "ca.pem"} {
		info, statErr := os.Stat(filepath.Join(directory, name))
		if statErr != nil || info.Size() == 0 || info.Mode().Perm()&0o077 != 0 {
			return ErrInvalidCommand
		}
	}
	return nil
}

func uninstallLocalCollectorService(ctx context.Context, root string) error {
	if os.Geteuid() != 0 || !validLocalCollectorRoot(root) {
		return ErrInvalidCommand
	}
	if output, err := exec.CommandContext(ctx, "systemctl", "disable", "--now", "argus-otelcol.service").CombinedOutput(); err != nil &&
		!strings.Contains(string(output), "does not exist") && !strings.Contains(string(output), "not loaded") {
		return fmt.Errorf("collector systemd uninstall: %w: %s", err, strings.TrimSpace(string(output)))
	}
	for _, path := range []string{"/etc/systemd/system/argus-otelcol.service", "/usr/local/bin/argus-otelcol", "/etc/argus-otelcol"} {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if err = os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if output, err := exec.CommandContext(ctx, "systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("collector systemd daemon-reload: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func validLocalCollectorRoot(root string) bool {
	return filepath.IsAbs(root) && filepath.Clean(root) == root && strings.HasPrefix(root, "/var/lib/") &&
		root != "/var/lib" && !strings.ContainsAny(root, "\r\n\t ")
}

func localCollectorSystemdUnit(command *connectorv1.CollectorManagementCommand, root string) string {
	environment := func(name, value string) string {
		return "Environment=" + strconv.Quote(name+"="+value) + "\n"
	}
	return `[Unit]
Description=Argus managed OpenTelemetry Collector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
` + environment("ARGUS_TELEMETRY_ENROLLMENT_TOKEN_FILE", "/etc/argus-otelcol/enrollment-token") +
		environment("ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT", command.GetEnrollmentEndpoint()) +
		environment("ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT", command.GetIngestGrpcEndpoint()) +
		environment("ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT", command.GetIngestHttpEndpoint()) +
		`ExecStart=/usr/local/bin/argus-otelcol --config=/etc/argus-otelcol/config.yaml
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
ReadWritePaths=` + root + ` /etc/argus-otelcol

[Install]
WantedBy=multi-user.target
`
}

func installCollectorArchive(archivePath, destination string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("%w: collector archive: %v", ErrArtifactInvalid, err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("%w: collector archive: %v", ErrArtifactInvalid, nextErr)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(filepath.Clean(header.Name)) != "argus-otelcol" || header.Size < 1 || header.Size > MaxArtifactBytes {
			continue
		}
		temporary, createErr := os.CreateTemp(filepath.Dir(destination), ".argus-otelcol-*")
		if createErr != nil {
			return createErr
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if createErr = temporary.Chmod(0o755); createErr == nil {
			var written int64
			written, createErr = io.Copy(temporary, io.LimitReader(reader, header.Size+1))
			if createErr == nil && written != header.Size {
				createErr = ErrArtifactInvalid
			}
		}
		if createErr == nil {
			createErr = temporary.Sync()
		}
		closeErr := temporary.Close()
		if createErr != nil {
			return createErr
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Rename(temporaryPath, destination)
	}
	return fmt.Errorf("%w: collector binary is missing", ErrArtifactInvalid)
}

func (manager Manager) FetchArtifact(ctx context.Context, artifact *connectorv1.CollectorArtifact) ([]byte, error) {
	var value bytes.Buffer
	if err := manager.FetchArtifactTo(ctx, artifact, &value); err != nil {
		return nil, err
	}
	return value.Bytes(), nil
}

// FetchArtifactTo streams an immutable artifact into destination while
// calculating its digest. Callers must keep destination private until this
// method returns nil because the signature can only be checked after EOF.
func (manager Manager) FetchArtifactTo(ctx context.Context, artifact *connectorv1.CollectorArtifact, destination io.Writer) error {
	if artifact == nil || destination == nil || artifact.GetByteSize() < 1 || artifact.GetByteSize() > MaxArtifactBytes ||
		!validSHA256(artifact.GetSha256()) || strings.TrimSpace(artifact.GetSignature()) == "" ||
		strings.TrimSpace(artifact.GetSigningKeyId()) == "" {
		return ErrArtifactInvalid
	}
	parsed, err := url.Parse(artifact.GetUri())
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrArtifactInvalid
	}
	client := manager.HTTPClient
	if client == nil {
		return fmt.Errorf("%w: Artifact HTTPS client has no Argus Trust Bundle", ErrArtifactInvalid)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.GetUri(), nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrArtifactInvalid, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: fetch %s: %v", ErrArtifactInvalid, artifact.GetUri(), err)
	}
	defer response.Body.Close()
	expected := int64(artifact.GetByteSize())
	if response.StatusCode != http.StatusOK || response.ContentLength >= 0 && response.ContentLength != expected {
		return fmt.Errorf("%w: status %d contentLength %d want %d bytes", ErrArtifactInvalid, response.StatusCode, response.ContentLength, artifact.GetByteSize())
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(response.Body, expected+1))
	if err != nil || written != expected {
		return fmt.Errorf("%w: read body: got %d bytes want %d: %v", ErrArtifactInvalid, written, artifact.GetByteSize(), err)
	}
	digest := hash.Sum(nil)
	if !strings.EqualFold(hex.EncodeToString(digest), artifact.GetSha256()) {
		return fmt.Errorf("%w: sha256 mismatch", ErrArtifactInvalid)
	}
	keys := manager.TrustedSigningKeys
	if len(keys) == 0 {
		keys = signingKeysFromEnvironment()
	}
	key := keys[artifact.GetSigningKeyId()]
	signature, decodeErr := base64.RawStdEncoding.DecodeString(artifact.GetSignature())
	if len(key) != ed25519.PublicKeySize || decodeErr != nil || !ed25519.Verify(key, digest, signature) {
		return fmt.Errorf("%w: signature verify failed for key %q", ErrArtifactInvalid, artifact.GetSigningKeyId())
	}
	return nil
}

func (manager Manager) writeArtifactAtomic(ctx context.Context, path string, artifact *connectorv1.CollectorArtifact, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".argus-artifact-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(mode); err == nil {
		err = manager.FetchArtifactTo(ctx, artifact, temporary)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporaryPath, path)
}

func signingKeysFromEnvironment() map[string]ed25519.PublicKey {
	var encoded map[string]string
	raw := strings.TrimSpace(os.Getenv("ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS"))
	if raw != "" && json.Unmarshal([]byte(raw), &encoded) != nil {
		return nil
	}
	if len(encoded) == 0 {
		path := strings.TrimSpace(os.Getenv("ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS_FILE"))
		if path != "" {
			value, err := os.ReadFile(path)
			if err != nil || json.Unmarshal(value, &encoded) != nil {
				return nil
			}
		}
	}
	result := make(map[string]ed25519.PublicKey, len(encoded))
	for id, value := range encoded {
		decoded, err := base64.RawStdEncoding.DecodeString(value)
		if err == nil && len(decoded) == ed25519.PublicKeySize {
			result[id] = ed25519.PublicKey(decoded)
		}
	}
	return result
}

func commandTrustBundle(command *connectorv1.CollectorManagementCommand) (trustbundle.Material, error) {
	if command == nil || command.GetTrustBundleEpoch() < 1 || len(command.GetTrustBundlePem()) == 0 ||
		len(command.GetTrustBundlePem()) > 128<<10 || !validSHA256(command.GetTrustBundleSha256()) {
		return trustbundle.Material{}, ErrInvalidCommand
	}
	material, err := trustbundle.Parse(command.GetTrustBundlePem(), time.Now().UTC())
	if err != nil || !strings.EqualFold(material.SHA256, command.GetTrustBundleSha256()) {
		return trustbundle.Material{}, ErrInvalidCommand
	}
	fingerprints := append([]string(nil), command.GetTrustBundleCaFingerprints()...)
	for index := range fingerprints {
		fingerprints[index] = strings.ToLower(strings.TrimSpace(fingerprints[index]))
	}
	slices.Sort(fingerprints)
	if !slices.Equal(material.Fingerprints, fingerprints) {
		return trustbundle.Material{}, ErrInvalidCommand
	}
	return material, nil
}

func buildResult(command *connectorv1.CollectorManagementCommand, status string) Result {
	diagnostic := sha256.Sum256([]byte(strings.Join([]string{command.GetCollectorId(), command.GetOperation(),
		fmt.Sprint(command.GetDesiredRevision()), command.GetConfigSha256(), status}, "\x00")))
	return Result{CollectorID: command.GetCollectorId(), EffectiveRevision: command.GetDesiredRevision(), ConfigSHA256: strings.ToLower(command.GetConfigSha256()),
		Status: status, DiagnosticHash: hex.EncodeToString(diagnostic[:])}
}

func writeAtomic(path string, value []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".argus-collector-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(value)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporaryPath, path)
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
