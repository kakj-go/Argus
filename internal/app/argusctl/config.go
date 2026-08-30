package argusctl

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	Profile      string     `json:"profile"`
	KubeContext  string     `json:"kubeContext"`
	ReleaseID    string     `json:"releaseId"`
	Namespaces   Namespaces `json:"namespaces"`
	StorageClass string     `json:"storageClass"`
	Images       Images     `json:"images"`
	Exposure     Exposure   `json:"exposure"`
	Security     Security   `json:"security"`
	Network      Network    `json:"network"`
	OpenSandbox  struct {
		RuntimeClassName   string `json:"runtimeClassName"`
		AllowSharedRuntime bool   `json:"allowSharedRuntime"`
	} `json:"openSandbox"`
	Telemetry   TelemetryArtifacts `json:"telemetry"`
	Persistence Persistence        `json:"persistence"`
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
	// ArtifactCABundleSecretName 为产物 HTTPS 源 CA 的 Secret 名,留空表示产物源使用系统信任。
	ArtifactCABundleSecretName string `json:"artifactCABundleSecretName"`
	// ExternalIngestHost 为外部 Collector(主机/外部集群)可达的 ingest 主机名;
	// 留空使用集群内 Service 地址(同集群目标,如 E2E)。
	ExternalIngestHost string `json:"externalIngestHost"`
	// ArtifactTLSMode 控制产物下载的传输层校验:verify(默认)或 insecure。
	// insecure 仅跳过证书校验(本地自签),sha256+ed25519 签名校验恒定执行。
	ArtifactTLSMode string `json:"artifactTLSMode"`
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
	ArtifactHost string     `json:"artifactHost"`
	TLS          *TLSConfig `json:"tls,omitempty"`
}

type TLSConfig struct {
	Enabled    bool      `json:"enabled"`
	Mode       TLSMode   `json:"mode"`
	IssuerRef  IssuerRef `json:"issuerRef,omitempty"`
	SecretName string    `json:"secretName,omitempty"`
}

type TLSMode string

const (
	TLSModeCertManagerSelfSigned TLSMode = "cert-manager-selfsigned"
	TLSModeCertManagerIssuer     TLSMode = "cert-manager-issuer"
	TLSModeUserProvided          TLSMode = "user-provided"
)

type IssuerRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // "Issuer" or "ClusterIssuer"
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
	if !contains([]string{"", "verify", "insecure"}, c.Spec.Telemetry.ArtifactTLSMode) {
		return fmt.Errorf("unsupported spec.telemetry.artifactTLSMode %q (verify|insecure)", c.Spec.Telemetry.ArtifactTLSMode)
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
	if c.Spec.Exposure.TLS == nil || !c.Spec.Exposure.TLS.Enabled {
		return fmt.Errorf("spec.exposure.tls.enabled must be true; plain HTTP exposure is not supported (modes: cert-manager-selfsigned, cert-manager-issuer, user-provided)")
	}
	switch c.Spec.Exposure.TLS.Mode {
	case TLSModeCertManagerSelfSigned:
		// No additional validation needed
	case TLSModeCertManagerIssuer:
		if c.Spec.Exposure.TLS.IssuerRef.Name == "" {
			return fmt.Errorf("spec.exposure.tls.issuerRef.name required when mode is cert-manager-issuer")
		}
		if c.Spec.Exposure.TLS.IssuerRef.Kind == "" {
			c.Spec.Exposure.TLS.IssuerRef.Kind = "ClusterIssuer"
		}
	case TLSModeUserProvided:
		if c.Spec.Exposure.TLS.SecretName == "" {
			return fmt.Errorf("spec.exposure.tls.secretName required when mode is user-provided")
		}
	default:
		return fmt.Errorf("spec.exposure.tls.mode must be cert-manager-selfsigned, cert-manager-issuer, or user-provided")
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

// collectorArtifactCA 解析产物 HTTPS 源的信任 Secret:显式配置优先;
// cert-manager-selfsigned 模式下默认信任平台多 SAN 证书(其 ca.crt 即信任锚);
// artifactTLSMode=insecure 时无需 CA,返回空避免无谓挂载。
func (c *InstallConfig) collectorArtifactCA() string {
	if strings.TrimSpace(c.Spec.Telemetry.ArtifactTLSMode) == "insecure" {
		return ""
	}
	if name := strings.TrimSpace(c.Spec.Telemetry.ArtifactCABundleSecretName); name != "" {
		return name
	}
	if tls := c.Spec.Exposure.TLS; tls != nil && tls.Enabled && tls.Mode == TLSModeCertManagerSelfSigned {
		return "argus-web-tls"
	}
	return ""
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
