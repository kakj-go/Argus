package collectormanager

import (
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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
)

const (
	MaxArtifactBytes = 256 << 20
	MaxConfigBytes   = 1 << 20
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
}

func NewArtifactHTTPClient(caBundlePath string) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if strings.TrimSpace(caBundlePath) != "" {
		value, readErr := os.ReadFile(caBundlePath)
		if readErr != nil || !pool.AppendCertsFromPEM(value) {
			return nil, ErrArtifactInvalid
		}
	}
	return newArtifactHTTPClient(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool}), nil
}

// NewArtifactHTTPClientInsecure 供 artifactTLSMode=insecure 使用:仅跳过
// 传输层证书校验(本地自签场景),https 与重定向约束保持不变;产物完整性
// 仍由 sha256 + ed25519 签名校验端到端保证,不受此开关影响。
func NewArtifactHTTPClientInsecure() (*http.Client, error) {
	return newArtifactHTTPClient(&tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}), nil //nolint:gosec // 显式配置的本地部署模式,签名链是信任根
}

func newArtifactHTTPClient(tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		Timeout:   2 * time.Minute,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		CheckRedirect: func(next *http.Request, previous []*http.Request) error {
			if len(previous) >= 3 || next.URL.Scheme != "https" || next.URL.Hostname() != previous[0].URL.Hostname() {
				return ErrArtifactInvalid
			}
			return nil
		},
	}
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
	if command.GetOperation() == "uninstall" {
		return nil
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
		if err := os.RemoveAll(directory); err != nil {
			return Result{}, err
		}
		return buildResult(command, "uninstalled"), nil
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Result{}, err
	}
	artifact, err := manager.FetchArtifact(ctx, command.GetArtifact())
	if err != nil {
		return Result{}, err
	}
	if err = writeAtomic(filepath.Join(directory, "collector.artifact"), artifact, 0o500); err != nil {
		return Result{}, err
	}
	runtimeConfig, err := configbundle.Extract(command.GetRenderedConfig(), "host")
	if err != nil {
		return Result{}, err
	}
	if err = writeAtomic(filepath.Join(directory, "collector-config.yaml"), runtimeConfig, 0o600); err != nil {
		return Result{}, err
	}
	serverCA, err := configbundle.ServerCA(command.GetRenderedConfig())
	if err != nil {
		return Result{}, err
	}
	if err = writeAtomic(filepath.Join(directory, "server-ca.pem"), serverCA, 0o600); err != nil {
		return Result{}, err
	}
	if len(command.GetEnrollmentToken()) > 0 {
		if err = writeAtomic(filepath.Join(directory, "enrollment-token"), command.GetEnrollmentToken(), 0o600); err != nil {
			return Result{}, err
		}
	}
	state := persistedState{SchemaVersion: "argus.collector_state/v1", CollectorID: command.GetCollectorId(), ResourceID: command.GetResourceId(),
		ResourceType: command.GetResourceType(), EffectiveRevision: command.GetDesiredRevision(), ConfigSHA256: strings.ToLower(command.GetConfigSha256()),
		ArtifactSHA256: strings.ToLower(command.GetArtifact().GetSha256()), RouteKind: command.GetRouteKind(), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	encoded, err := json.Marshal(state)
	if err != nil {
		return Result{}, err
	}
	if err = writeAtomic(filepath.Join(directory, "state.json"), encoded, 0o600); err != nil {
		return Result{}, err
	}
	return buildResult(command, "converged"), nil
}

func (manager Manager) FetchArtifact(ctx context.Context, artifact *connectorv1.CollectorArtifact) ([]byte, error) {
	client := manager.HTTPClient
	if client == nil {
		client, _ = NewArtifactHTTPClient("")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.GetUri(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrArtifactInvalid, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch %s: %v", ErrArtifactInvalid, artifact.GetUri(), err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > int64(artifact.GetByteSize()) {
		return nil, fmt.Errorf("%w: status %d contentLength %d want %d bytes", ErrArtifactInvalid, response.StatusCode, response.ContentLength, artifact.GetByteSize())
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, int64(artifact.GetByteSize())+1))
	if err != nil || uint64(len(value)) != artifact.GetByteSize() {
		return nil, fmt.Errorf("%w: read body: got %d bytes want %d: %v", ErrArtifactInvalid, len(value), artifact.GetByteSize(), err)
	}
	hash := sha256.Sum256(value)
	if !strings.EqualFold(hex.EncodeToString(hash[:]), artifact.GetSha256()) {
		return nil, fmt.Errorf("%w: sha256 mismatch", ErrArtifactInvalid)
	}
	keys := manager.TrustedSigningKeys
	if len(keys) == 0 {
		keys = signingKeysFromEnvironment()
	}
	key := keys[artifact.GetSigningKeyId()]
	signature, decodeErr := base64.RawStdEncoding.DecodeString(artifact.GetSignature())
	if len(key) != ed25519.PublicKeySize || decodeErr != nil || !ed25519.Verify(key, hash[:], signature) {
		return nil, fmt.Errorf("%w: signature verify failed for key %q", ErrArtifactInvalid, artifact.GetSigningKeyId())
	}
	return value, nil
}

func signingKeysFromEnvironment() map[string]ed25519.PublicKey {
	var encoded map[string]string
	if json.Unmarshal([]byte(os.Getenv("ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS")), &encoded) != nil {
		return nil
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
