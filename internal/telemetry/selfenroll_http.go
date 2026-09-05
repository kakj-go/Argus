package telemetry

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/installinstruction"
)

// bootstrapRateLimit 是公开 bootstrap 端点的轻量滑动窗口限流:
// 按来源 IP(60/分钟)与令牌(10/分钟)两个维度,防止令牌暴力枚举。
type bootstrapRateLimit struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

var bootstrapLimiter = &bootstrapRateLimit{entries: map[string][]time.Time{}}

// httpHostBootstrapScript serves the full strict bootstrap behind the short
// one-line command. The caller may relax TLS for this single download, while
// the returned script pins the Argus Trust Bundle for every later request.
func (server *IngestServer) httpHostBootstrapScript(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.TLS == nil || len(request.TLS.PeerCertificates) != 0 || server.SelfEnroll == nil ||
		request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		http.Error(writer, "host bootstrap script rejected", http.StatusUnauthorized)
		return
	}
	token := request.Header.Get("X-Argus-Enrollment-Token")
	if token == "" || len(token) > 256 || !bootstrapLimiter.allow("host-script-token:"+token, 10, time.Minute) {
		http.Error(writer, "host bootstrap script rejected", http.StatusUnauthorized)
		return
	}
	script, err := server.SelfEnroll.BootstrapScript(request.Context(), token,
		installinstruction.Scope(request.URL.Query().Get("scope")))
	if err != nil {
		if errors.Is(err, ErrDistributionPending) {
			http.Error(writer, "collector distribution unavailable", http.StatusServiceUnavailable)
		} else {
			http.Error(writer, "host bootstrap script rejected", http.StatusUnauthorized)
		}
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	writer.Header().Set("Content-Disposition", `inline; filename="argus-host-bootstrap.sh"`)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = writer.Write([]byte(script))
}

func (limiter *bootstrapRateLimit) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	attempts := limiter.entries[key]
	kept := attempts[:0]
	for _, at := range attempts {
		if now.Sub(at) < window {
			kept = append(kept, at)
		}
	}
	if len(kept) >= limit {
		limiter.entries[key] = kept
		return false
	}
	limiter.entries[key] = append(kept, now)
	if len(limiter.entries) > 4096 {
		// 防内存膨胀:超界时整体重建(清理过期窗口)。
		rebuilt := map[string][]time.Time{}
		for entryKey, values := range limiter.entries {
			recent := make([]time.Time, 0, len(values))
			for _, at := range values {
				if now.Sub(at) < window {
					recent = append(recent, at)
				}
			}
			if len(recent) > 0 {
				rebuilt[entryKey] = recent
			}
		}
		limiter.entries = rebuilt
	}
	return true
}

// httpHostInstallBootstrap 是 GET /v1/host-install/{token} 的公开处理器:
// 目标主机上的安装脚本用它交换冻结计划。自报参数仅用于回填展示,不参与信任判定。
func (server *IngestServer) httpHostInstallBootstrap(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.TLS == nil || len(request.TLS.PeerCertificates) != 0 || server.SelfEnroll == nil ||
		request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		http.Error(writer, "host install bootstrap rejected", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(request.URL.Path, "/v1/host-install/")
	if token == "" || strings.Contains(token, "/") || len(token) > 256 {
		http.Error(writer, "host install bootstrap rejected", http.StatusUnauthorized)
		return
	}
	if remote, _, splitErr := net.SplitHostPort(request.RemoteAddr); splitErr == nil {
		if !bootstrapLimiter.allow("ip:"+remote, 60, time.Minute) {
			http.Error(writer, "host install bootstrap rate limited", http.StatusTooManyRequests)
			return
		}
	}
	if !bootstrapLimiter.allow("token:"+token, 10, time.Minute) {
		http.Error(writer, "host install bootstrap rate limited", http.StatusTooManyRequests)
		return
	}
	query := request.URL.Query()
	payload, err := server.SelfEnroll.ExchangeBootstrap(request.Context(), token,
		query.Get("arch"), query.Get("hostname"), query.Get("address"))
	if err != nil {
		switch {
		case errors.Is(err, ErrHostInstallTokenConflict):
			http.Error(writer, "host install token already consumed by another device", http.StatusConflict)
		case errors.Is(err, ErrDistributionPending):
			http.Error(writer, "collector distribution unavailable", http.StatusServiceUnavailable)
		default:
			http.Error(writer, "host install bootstrap rejected", http.StatusUnauthorized)
		}
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(bootstrapPayloadDTOFrom(payload))
}

// bootstrapPayloadDTO 与 OpenAPI HostInstallBootstrap schema 对齐(snake_case)。
type bootstrapPayloadDTO struct {
	CollectorID             string             `json:"collector_id"`
	HostID                  string             `json:"host_id"`
	DesiredRevision         int64              `json:"desired_revision"`
	ConfigBundle            string             `json:"config_bundle,omitempty"`
	TrustBundle             string             `json:"trust_bundle,omitempty"`
	TrustBundleEpoch        int64              `json:"trust_bundle_epoch,omitempty"`
	TrustBundleSHA256       string             `json:"trust_bundle_sha256,omitempty"`
	TrustBundleFingerprints []string           `json:"trust_bundle_ca_fingerprints,omitempty"`
	Mode                    string             `json:"mode"`
	Artifact                *bootstrapArtifact `json:"artifact,omitempty"`
	SigningPublicKey        string             `json:"signing_public_key,omitempty"`
	SigningKeyID            string             `json:"signing_key_id,omitempty"`
	EnrollmentToken         string             `json:"enrollment_token,omitempty"`
	EnrollmentEndpoint      string             `json:"enrollment_endpoint,omitempty"`
	IngestGRPCEndpoint      string             `json:"ingest_grpc_endpoint,omitempty"`
	IngestHTTPEndpoint      string             `json:"ingest_http_endpoint,omitempty"`
	ExpiresAt               string             `json:"expires_at"`
}

type bootstrapArtifact struct {
	DistributionVersionID string `json:"distribution_version_id"`
	Platform              string `json:"platform"`
	URI                   string `json:"uri"`
	SHA256                string `json:"sha256"`
	Signature             string `json:"signature"`
	SigningKeyID          string `json:"signing_key_id"`
	ByteSize              uint64 `json:"byte_size"`
}

func bootstrapPayloadDTOFrom(payload BootstrapPayload) bootstrapPayloadDTO {
	dto := bootstrapPayloadDTO{CollectorID: payload.CollectorID.String(), HostID: payload.HostID.String(),
		DesiredRevision: payload.DesiredRevision, Mode: payload.Mode, ExpiresAt: payload.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
	if payload.ConfigBundle != nil {
		dto.ConfigBundle = base64.StdEncoding.EncodeToString(payload.ConfigBundle)
	}
	if payload.TrustBundle != nil {
		dto.TrustBundle = base64.StdEncoding.EncodeToString(payload.TrustBundle)
		dto.TrustBundleEpoch = payload.TrustBundleEpoch
		dto.TrustBundleSHA256 = payload.TrustBundleSHA256
		dto.TrustBundleFingerprints = append([]string(nil), payload.TrustBundleFingerprints...)
	}
	if payload.HasArtifact {
		dto.Artifact = &bootstrapArtifact{DistributionVersionID: payload.DistributionVersionID,
			Platform: payload.Artifact.Platform, URI: payload.Artifact.URI, SHA256: payload.Artifact.SHA256,
			Signature: payload.Artifact.Signature, SigningKeyID: payload.Artifact.SigningKeyID, ByteSize: payload.Artifact.ByteSize}
	}
	dto.SigningPublicKey = payload.SigningPublicKey
	dto.SigningKeyID = payload.SigningKeyID
	dto.EnrollmentToken = payload.EnrollmentToken
	dto.EnrollmentEndpoint = payload.EnrollmentEndpoint
	dto.IngestGRPCEndpoint = payload.IngestGRPCEndpoint
	dto.IngestHTTPEndpoint = payload.IngestHTTPEndpoint
	return dto
}

func (server *IngestServer) httpHostUninstall(writer http.ResponseWriter, request *http.Request) {
	if request.TLS == nil || len(request.TLS.PeerCertificates) != 0 || server.SelfEnroll == nil ||
		request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		http.Error(writer, "host uninstall rejected", http.StatusUnauthorized)
		return
	}
	value := strings.TrimPrefix(request.URL.Path, "/v1/host-uninstall/")
	if strings.HasSuffix(value, "/complete") {
		server.httpCompleteHostUninstall(writer, request, strings.TrimSuffix(value, "/complete"))
		return
	}
	if request.Method != http.MethodGet || value == "" || strings.Contains(value, "/") || len(value) > 256 {
		http.Error(writer, "host uninstall rejected", http.StatusUnauthorized)
		return
	}
	if !bootstrapLimiter.allow("uninstall-token:"+value, 10, time.Minute) {
		http.Error(writer, "host uninstall rate limited", http.StatusTooManyRequests)
		return
	}
	query := request.URL.Query()
	payload, err := server.SelfEnroll.ExchangeUninstall(request.Context(), value,
		query.Get("arch"), query.Get("hostname"), query.Get("address"))
	if err != nil {
		if errors.Is(err, ErrHostUninstallTokenConflict) {
			http.Error(writer, "host uninstall token already consumed by another device", http.StatusConflict)
		} else {
			http.Error(writer, "host uninstall rejected", http.StatusUnauthorized)
		}
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(struct {
		HostID          string `json:"host_id"`
		CollectorID     string `json:"collector_id"`
		CompletionURL   string `json:"completion_url"`
		CompletionToken string `json:"completion_token"`
		ExpiresAt       string `json:"expires_at"`
	}{HostID: payload.HostID.String(), CollectorID: payload.CollectorID.String(), CompletionURL: payload.CompletionURL,
		CompletionToken: payload.CompletionToken, ExpiresAt: payload.ExpiresAt.UTC().Format(time.RFC3339)})
}

func (server *IngestServer) httpCompleteHostUninstall(writer http.ResponseWriter, request *http.Request, tokenIDValue string) {
	tokenID, err := uuid.Parse(tokenIDValue)
	completionToken := request.Header.Get("X-Argus-Uninstall-Completion-Token")
	if request.Method != http.MethodPost || err != nil || completionToken == "" || len(completionToken) > 256 || request.ContentLength > 0 {
		http.Error(writer, "host uninstall completion rejected", http.StatusUnauthorized)
		return
	}
	if !bootstrapLimiter.allow("uninstall-complete:"+tokenIDValue, 10, time.Minute) {
		http.Error(writer, "host uninstall completion rate limited", http.StatusTooManyRequests)
		return
	}
	if err = server.SelfEnroll.CompleteUninstall(request.Context(), tokenID, completionToken); err != nil {
		http.Error(writer, "host uninstall completion rejected", http.StatusUnauthorized)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}
