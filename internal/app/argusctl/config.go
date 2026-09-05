package argusctl

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kakj-go/Argus/internal/trustbundle"
	"sigs.k8s.io/yaml"
)

const (
	installAPIVersion = "install.argus.io/v1alpha1"
	installKind       = "ArgusInstallConfig"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type InstallConfig struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec InstallSpec `json:"spec"`
	path string
}

type InstallSpec struct {
	Profile        string                 `json:"profile"`
	KubeContext    string                 `json:"kubeContext"`
	ReleaseID      string                 `json:"releaseId"`
	Namespaces     Namespaces             `json:"namespaces"`
	StorageClass   string                 `json:"storageClass"`
	Images         Images                 `json:"images"`
	Exposure       Exposure               `json:"exposure"`
	Security       Security               `json:"security"`
	Network        Network                `json:"network"`
	PKI            PKIConfig              `json:"pki"`
	DirectExecutor DirectExecutorCapacity `json:"directExecutor"`
	OpenSandbox    struct {
		RuntimeClassName   string `json:"runtimeClassName"`
		AllowSharedRuntime bool   `json:"allowSharedRuntime"`
	} `json:"openSandbox"`
	Telemetry   TelemetryArtifacts `json:"telemetry"`
	Persistence Persistence        `json:"persistence"`
}

// DirectExecutorCapacity is explicit in production because tunnel ownership
// and aggregate relay bandwidth are part of the deployment capacity model.
// Evaluation and local-hardening profiles may leave it empty and use the
// chart's documented defaults.
type DirectExecutorCapacity struct {
	TelemetryTunnelLimit int   `json:"telemetryTunnelLimit"`
	ControlTunnelLimit   int   `json:"controlTunnelLimit"`
	TunnelBytesPerSecond int64 `json:"tunnelBytesPerSecond"`
}

type Network struct {
	Mode   string        `json:"mode"`
	Egress NetworkEgress `json:"egress"`
}

type NetworkEgress struct {
	ExpectedIPs     []string `json:"expectedIPs"`
	VerificationURL string   `json:"verificationURL"`
}

type Security struct {
	PlatformMFARequired bool     `json:"platformMfaRequired"`
	ProtectedCIDRs      []string `json:"protectedCidrs"`
}

type TelemetryArtifacts struct {
	CollectorVersion      string `json:"collectorVersion"`
	LinuxARM64URI         string `json:"linuxArm64Uri"`
	LinuxARM64SHA256      string `json:"linuxArm64Sha256"`
	LinuxARM64Signature   string `json:"linuxArm64Signature"`
	LinuxARM64ByteSize    uint64 `json:"linuxArm64ByteSize"`
	LinuxAMD64URI         string `json:"linuxAmd64Uri"`
	LinuxAMD64SHA256      string `json:"linuxAmd64Sha256"`
	LinuxAMD64Signature   string `json:"linuxAmd64Signature"`
	LinuxAMD64ByteSize    uint64 `json:"linuxAmd64ByteSize"`
	WindowsAMD64URI       string `json:"windowsAmd64Uri"`
	WindowsAMD64SHA256    string `json:"windowsAmd64Sha256"`
	WindowsAMD64Signature string `json:"windowsAmd64Signature"`
	WindowsAMD64ByteSize  uint64 `json:"windowsAmd64ByteSize"`
	SigningKeyID          string `json:"signingKeyId"`
	SigningPublicKey      string `json:"signingPublicKey"`
	// KubernetesImage 为集群 Collector 的默认镜像引用;留空时回退到平台镜像
	// registry 派生值(local-registry 开发模式),安装向导可按次覆盖。
	KubernetesImage string `json:"kubernetesImage"`
	// ExternalIngestHost 为外部 Collector(主机/外部集群)可达的 ingest 主机名;
	// 留空使用集群内 Service 地址(同集群目标,如 E2E)。
	ExternalIngestHost string `json:"externalIngestHost"`
}

type Namespaces struct {
	System        string `json:"system"`
	Sandbox       string `json:"sandbox"`
	Observability string `json:"observability"`
}

type Images struct {
	Mode          string `json:"mode"`
	Registry      string `json:"registry"`
	Tag           string `json:"tag"`
	PullPolicy    string `json:"pullPolicy"`
	PullSecretRef string `json:"pullSecretRef"`
}

type Exposure struct {
	IngressClassName string `json:"ingressClassName"`
	EnterpriseHost   string `json:"enterpriseHost"`
	PlatformHost     string `json:"platformHost"`
	ConnectorHost    string `json:"connectorHost"`
	// ArtifactHost 为 Collector 产物下载源的主机名;留空时按
	// artifacts.<enterprise 父域名> 派生。
	ArtifactHost string `json:"artifactHost"`
}

type PKIConfig struct {
	Mode             PKIMode        `json:"mode"`
	BootstrapTLSMode string         `json:"bootstrapTLSMode,omitempty"`
	IssuerRef        PKIIssuerRef   `json:"issuerRef,omitempty"`
	CABundle         CABundleSource `json:"caBundle,omitempty"`
	Rotation         PKIRotation    `json:"rotation"`
}

type PKIMode string

const (
	PKIModeManaged               PKIMode = "managed"
	PKIModeExistingClusterIssuer PKIMode = "existing-cluster-issuer"
	defaultPKIOverlap                    = "168h"
)

type PKIIssuerRef struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

type CABundleSource struct {
	File      string `json:"file,omitempty"`
	InlinePEM string `json:"inlinePEM,omitempty"`
}

type PKIRotation struct {
	Overlap string `json:"overlap"`
}

type Persistence struct {
	PostgreSQL string `json:"postgresql"`
	Redis      string `json:"redis"`
	MinIO      string `json:"minio"`
	Kafka      string `json:"kafka"`
	ClickHouse string `json:"clickhouse"`
	Keeper     string `json:"keeper"`
}

func LoadConfig(path string) (*InstallConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg InstallConfig
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg.path = abs
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *InstallConfig) Validate() error {
	if c.APIVersion != installAPIVersion || c.Kind != installKind {
		return fmt.Errorf("config must be %s %s", installAPIVersion, installKind)
	}
	if !contains([]string{"evaluation", "local-hardening", "production"}, c.Spec.Profile) {
		return fmt.Errorf("spec.profile must be evaluation, local-hardening, or production")
	}
	if c.Spec.Profile == "production" && (c.Spec.DirectExecutor.TelemetryTunnelLimit <= 0 ||
		c.Spec.DirectExecutor.ControlTunnelLimit <= 0 || c.Spec.DirectExecutor.TunnelBytesPerSecond <= 0) {
		return fmt.Errorf("spec.directExecutor telemetryTunnelLimit, controlTunnelLimit, and tunnelBytesPerSecond must be positive in production")
	}
	if c.Spec.DirectExecutor.TelemetryTunnelLimit < 0 || c.Spec.DirectExecutor.ControlTunnelLimit < 0 ||
		c.Spec.DirectExecutor.TunnelBytesPerSecond < 0 {
		return fmt.Errorf("spec.directExecutor capacity values cannot be negative")
	}
	if c.Spec.Network.Mode == "" {
		c.Spec.Network.Mode = "auto"
	}
	if !contains([]string{"auto", "portable", "network-policy", "external"}, c.Spec.Network.Mode) {
		return fmt.Errorf("spec.network.mode must be auto, portable, network-policy, or external")
	}
	for _, value := range c.Spec.Network.Egress.ExpectedIPs {
		if _, err := netip.ParseAddr(value); err != nil {
			return fmt.Errorf("spec.network.egress.expectedIPs contains invalid address %q", value)
		}
	}
	for _, value := range c.Spec.Security.ProtectedCIDRs {
		if _, err := netip.ParsePrefix(value); err != nil {
			return fmt.Errorf("spec.security.protectedCidrs contains invalid CIDR %q", value)
		}
	}
	if c.Spec.Network.Egress.VerificationURL != "" {
		parsed, err := url.Parse(c.Spec.Network.Egress.VerificationURL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
			return fmt.Errorf("spec.network.egress.verificationURL must be an HTTPS URL")
		}
	}
	for name, value := range map[string]string{
		"metadata.name":                 c.Metadata.Name,
		"spec.releaseId":                c.Spec.ReleaseID,
		"spec.namespaces.system":        c.Spec.Namespaces.System,
		"spec.namespaces.sandbox":       c.Spec.Namespaces.Sandbox,
		"spec.namespaces.observability": c.Spec.Namespaces.Observability,
	} {
		if !dnsLabel.MatchString(value) || len(value) > 63 {
			return fmt.Errorf("%s must be a Kubernetes DNS label", name)
		}
	}
	// Chart release names append suffixes such as "-telemetry-pipeline".
	// Helm limits the resulting name to 53 characters.
	if len(c.Spec.ReleaseID) > 34 {
		return fmt.Errorf("spec.releaseId must not exceed 34 characters")
	}
	if c.Spec.KubeContext == "" || c.Spec.StorageClass == "" {
		return fmt.Errorf("spec.kubeContext and spec.storageClass are required")
	}
	if c.Spec.Images.Registry == "" || c.Spec.Images.Tag == "" {
		return fmt.Errorf("spec.images.registry and spec.images.tag are required")
	}
	if c.Spec.Telemetry.CollectorVersion == "" || c.Spec.Telemetry.LinuxARM64URI == "" || c.Spec.Telemetry.LinuxARM64Signature == "" ||
		c.Spec.Telemetry.LinuxARM64ByteSize == 0 || c.Spec.Telemetry.SigningKeyID == "" || c.Spec.Telemetry.SigningPublicKey == "" {
		return fmt.Errorf("spec.telemetry Collector version, signed Linux arm64 artifact, and signing key are required")
	}
	digests := map[string]string{"spec.telemetry.linuxArm64Sha256": c.Spec.Telemetry.LinuxARM64SHA256}
	// Linux amd64 产物按需配置:配置了任一字段则要求完整(目录才会注册 amd64 产物)。
	if c.Spec.Telemetry.LinuxAMD64URI != "" || c.Spec.Telemetry.LinuxAMD64SHA256 != "" || c.Spec.Telemetry.LinuxAMD64Signature != "" || c.Spec.Telemetry.LinuxAMD64ByteSize != 0 {
		if c.Spec.Telemetry.LinuxAMD64URI == "" || c.Spec.Telemetry.LinuxAMD64Signature == "" || c.Spec.Telemetry.LinuxAMD64ByteSize == 0 {
			return fmt.Errorf("complete spec.telemetry Linux amd64 artifact metadata is required when configured")
		}
		digests["spec.telemetry.linuxAmd64Sha256"] = c.Spec.Telemetry.LinuxAMD64SHA256
	}
	if c.Spec.Profile != "local-hardening" || c.Spec.Telemetry.WindowsAMD64URI != "" || c.Spec.Telemetry.WindowsAMD64SHA256 != "" || c.Spec.Telemetry.WindowsAMD64Signature != "" || c.Spec.Telemetry.WindowsAMD64ByteSize != 0 {
		if c.Spec.Telemetry.WindowsAMD64URI == "" || c.Spec.Telemetry.WindowsAMD64Signature == "" || c.Spec.Telemetry.WindowsAMD64ByteSize == 0 {
			return fmt.Errorf("complete spec.telemetry Windows artifact metadata is required outside local-hardening")
		}
		digests["spec.telemetry.windowsAmd64Sha256"] = c.Spec.Telemetry.WindowsAMD64SHA256
	}
	for name, value := range digests {
		if matched, _ := regexp.MatchString(`^[0-9a-fA-F]{64}$`, value); !matched {
			return fmt.Errorf("%s must be a SHA-256 hex digest", name)
		}
	}
	if !contains([]string{"local-registry", "oci-registry"}, c.Spec.Images.Mode) {
		return fmt.Errorf("unsupported image mode %q", c.Spec.Images.Mode)
	}
	if !contains([]string{"Never", "IfNotPresent", "Always"}, c.Spec.Images.PullPolicy) {
		return fmt.Errorf("unsupported image pull policy %q", c.Spec.Images.PullPolicy)
	}
	if c.Spec.Exposure.IngressClassName == "" {
		c.Spec.Exposure.IngressClassName = "nginx"
	}
	if !dnsLabel.MatchString(c.Spec.Exposure.IngressClassName) {
		return fmt.Errorf("spec.exposure.ingressClassName must be a Kubernetes DNS label")
	}
	if c.Spec.Exposure.EnterpriseHost == "" || c.Spec.Exposure.PlatformHost == "" {
		return fmt.Errorf("spec.exposure.enterpriseHost and spec.exposure.platformHost are required for domain-based exposure")
	}
	if !validHostname(c.Spec.Exposure.EnterpriseHost) || !validHostname(c.Spec.Exposure.PlatformHost) {
		return fmt.Errorf("spec.exposure enterpriseHost and platformHost must be DNS hostnames")
	}
	if c.Spec.Exposure.ConnectorHost == "" {
		return fmt.Errorf("spec.exposure.connectorHost is required for domain-based exposure")
	}
	if !validHostname(c.Spec.Exposure.ConnectorHost) {
		return fmt.Errorf("spec.exposure.connectorHost must be a DNS hostname")
	}
	if c.Spec.PKI.Rotation.Overlap == "" {
		c.Spec.PKI.Rotation.Overlap = defaultPKIOverlap
	}
	overlap, err := time.ParseDuration(c.Spec.PKI.Rotation.Overlap)
	if err != nil || overlap < trustbundle.MinimumOverlap {
		return fmt.Errorf("spec.pki.rotation.overlap must be a duration of at least %s", trustbundle.MinimumOverlap)
	}
	switch c.Spec.PKI.Mode {
	case PKIModeManaged:
		if c.Spec.PKI.BootstrapTLSMode == "" {
			c.Spec.PKI.BootstrapTLSMode = "insecure-first-fetch"
		}
		if c.Spec.PKI.IssuerRef.Name != "" || strings.TrimSpace(c.Spec.PKI.CABundle.File) != "" || strings.TrimSpace(c.Spec.PKI.CABundle.InlinePEM) != "" {
			return fmt.Errorf("spec.pki managed mode must not configure issuerRef or caBundle")
		}
	case PKIModeExistingClusterIssuer:
		if c.Spec.PKI.BootstrapTLSMode == "" {
			c.Spec.PKI.BootstrapTLSMode = "strict"
		}
		if c.Spec.PKI.IssuerRef.Name == "" {
			return fmt.Errorf("spec.pki.issuerRef.name is required for existing-cluster-issuer")
		}
		if c.Spec.PKI.IssuerRef.Group == "" {
			c.Spec.PKI.IssuerRef.Group = "cert-manager.io"
		}
		if c.Spec.PKI.IssuerRef.Group != "cert-manager.io" {
			return fmt.Errorf("spec.pki.issuerRef.group must be cert-manager.io")
		}
		if _, err := c.CABundlePEM(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("spec.pki.mode must be managed or existing-cluster-issuer")
	}
	if c.Spec.PKI.BootstrapTLSMode != "strict" && c.Spec.PKI.BootstrapTLSMode != "insecure-first-fetch" {
		return fmt.Errorf("spec.pki.bootstrapTLSMode must be strict or insecure-first-fetch")
	}
	return nil
}

func validHostname(value string) bool {
	if len(value) == 0 || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (c *InstallConfig) Image(name string) string {
	registry := strings.TrimSuffix(c.Spec.Images.Registry, "/")
	if !strings.HasSuffix(registry, "/argus") {
		registry += "/argus"
	}
	return fmt.Sprintf("%s/%s:%s", registry, name, c.Spec.Images.Tag)
}

// collectorKubernetesImage 解析集群 Collector 默认镜像:显式配置的
// spec.telemetry.kubernetesImage 优先(发布镜像或客户内网 registry),
// 留空回退平台镜像派生值(local-registry 开发安装)。
func (c *InstallConfig) collectorKubernetesImage() string {
	if image := strings.TrimSpace(c.Spec.Telemetry.KubernetesImage); image != "" {
		return image
	}
	return c.Image("argus-otelcol")
}

// collectorArtifactCA returns the single public trust bundle distributed by
// trust-manager. Artifact transport never has an insecure mode.
func (c *InstallConfig) collectorArtifactCA() string {
	return c.trustBundleName()
}

func (c *InstallConfig) globalIssuerName() string {
	if c.Spec.PKI.Mode == PKIModeExistingClusterIssuer {
		return c.Spec.PKI.IssuerRef.Name
	}
	return c.Spec.ReleaseID + "-ca"
}

func (c *InstallConfig) trustSourceName() string {
	return c.Spec.ReleaseID + "-trust-source"
}

func (c *InstallConfig) trustBundleName() string {
	return c.Spec.ReleaseID + "-trust-bundle"
}

// CABundlePEM reads and canonicalizes the public trust anchors configured for
// an existing ClusterIssuer. Relative paths are resolved beside the install
// config, never against the process working directory.
func (c *InstallConfig) CABundlePEM() ([]byte, error) {
	file := strings.TrimSpace(c.Spec.PKI.CABundle.File)
	inline := strings.TrimSpace(c.Spec.PKI.CABundle.InlinePEM)
	if (file == "") == (inline == "") {
		return nil, fmt.Errorf("spec.pki.caBundle must set exactly one of file or inlinePEM")
	}
	var value []byte
	var err error
	if file != "" {
		if !filepath.IsAbs(file) {
			file = filepath.Join(filepath.Dir(c.path), file)
		}
		value, err = os.ReadFile(filepath.Clean(file))
		if err != nil {
			return nil, fmt.Errorf("read spec.pki.caBundle.file: %w", err)
		}
	} else {
		value = []byte(inline)
	}
	canonical, err := canonicalCABundle(value)
	if err != nil {
		return nil, fmt.Errorf("spec.pki.caBundle: %w", err)
	}
	return canonical, nil
}

func canonicalCABundle(value []byte) ([]byte, error) {
	now := time.Now().UTC()
	rest := bytes.TrimSpace(value)
	seen := map[[32]byte]struct{}{}
	var result bytes.Buffer
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("bundle must contain only PEM CERTIFICATE blocks")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		if !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
			return nil, fmt.Errorf("certificate %q is not a signing CA", certificate.Subject.String())
		}
		if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return nil, fmt.Errorf("CA certificate %q is not currently valid", certificate.Subject.String())
		}
		digest := sha256.Sum256(block.Bytes)
		if _, ok := seen[digest]; ok {
			return nil, fmt.Errorf("duplicate CA certificate %s", hex.EncodeToString(digest[:]))
		}
		seen[digest] = struct{}{}
		_ = pem.Encode(&result, &pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})
		rest = bytes.TrimSpace(remaining)
	}
	if result.Len() == 0 {
		return nil, fmt.Errorf("bundle contains no CA certificate")
	}
	return result.Bytes(), nil
}

func (c *InstallConfig) registryContainerName() string {
	return "argus-registry-" + c.Spec.ReleaseID
}

func (c *InstallConfig) upstreamReleaseName(component string) string {
	digest := sha256.Sum256([]byte(c.Spec.ReleaseID))
	return fmt.Sprintf("a%x-%s", digest[:3], component)
}

func findRepoRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for dir := abs; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "deploy", "versions.lock.yaml")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root from %s", start)
		}
	}
}
