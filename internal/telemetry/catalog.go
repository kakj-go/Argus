package telemetry

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const CatalogRevision = 1

const (
	LinuxARM64DistributionName   = "argus-otelcol-linux-arm64"
	LinuxAMD64DistributionName   = "argus-otelcol-linux-amd64"
	WindowsAMD64DistributionName = "argus-otelcol-windows-amd64"
)

type CatalogSync struct {
	Service                     Service
	Version                     string
	LinuxArtifactURI            string
	LinuxArtifactSHA256         string
	LinuxArtifactSignature      string
	LinuxArtifactByteSize       uint64
	LinuxAMD64ArtifactURI       string
	LinuxAMD64ArtifactSHA256    string
	LinuxAMD64ArtifactSignature string
	LinuxAMD64ArtifactByteSize  uint64
	WindowsArtifactURI          string
	WindowsArtifactSHA256       string
	WindowsArtifactSignature    string
	WindowsArtifactByteSize     uint64
	SigningKeyID                string
	SigningPublicKey            string
}

func (sync CatalogSync) Run(ctx context.Context) error {
	if sync.Version == "" {
		sync.Version = "0.1.0-m7"
	}
	if sync.Service.Store == nil || sync.LinuxArtifactURI == "" || sync.SigningKeyID == "" {
		return errors.New("telemetry Linux arm64 artifact URI, signing key, and store are required")
	}
	linuxHash, err := validateArtifactHash(sync.LinuxArtifactSHA256)
	if err != nil {
		return err
	}
	linuxAMD64Configured := sync.LinuxAMD64ArtifactURI != "" || sync.LinuxAMD64ArtifactSHA256 != "" || sync.LinuxAMD64ArtifactSignature != "" || sync.LinuxAMD64ArtifactByteSize != 0
	var linuxAMD64Hash string
	if linuxAMD64Configured {
		if sync.LinuxAMD64ArtifactURI == "" || sync.LinuxAMD64ArtifactSHA256 == "" || sync.LinuxAMD64ArtifactSignature == "" || sync.LinuxAMD64ArtifactByteSize == 0 {
			return errors.New("telemetry Linux amd64 artifact must be configured completely")
		}
		linuxAMD64Hash, err = validateArtifactHash(sync.LinuxAMD64ArtifactSHA256)
		if err != nil {
			return err
		}
	}
	windowsConfigured := sync.WindowsArtifactURI != "" || sync.WindowsArtifactSHA256 != "" || sync.WindowsArtifactSignature != "" || sync.WindowsArtifactByteSize != 0
	var windowsHash string
	if windowsConfigured {
		if sync.WindowsArtifactURI == "" || sync.WindowsArtifactSHA256 == "" || sync.WindowsArtifactSignature == "" || sync.WindowsArtifactByteSize == 0 {
			return errors.New("telemetry Windows artifact must be configured completely")
		}
		windowsHash, err = validateArtifactHash(sync.WindowsArtifactSHA256)
		if err != nil {
			return err
		}
	}
	publicKey, err := base64.RawStdEncoding.DecodeString(sync.SigningPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("telemetry catalog signing public key is invalid")
	}
	if err = validateArtifactSignature(publicKey, linuxHash, sync.LinuxArtifactSignature, sync.LinuxArtifactByteSize); err != nil {
		return err
	}
	if linuxAMD64Configured {
		if err = validateArtifactSignature(publicKey, linuxAMD64Hash, sync.LinuxAMD64ArtifactSignature, sync.LinuxAMD64ArtifactByteSize); err != nil {
			return err
		}
	}
	if windowsConfigured {
		if err = validateArtifactSignature(publicKey, windowsHash, sync.WindowsArtifactSignature, sync.WindowsArtifactByteSize); err != nil {
			return err
		}
	}
	linuxComponents := []string{"otlp", "hostmetrics", "kubeletstats", "filelog", "journald", "prometheus", "batch", "memory_limiter", "file_storage", "argus_identity"}
	distributions := []struct {
		name, platform, uri, hash, signature, status string
		byteSize                                     uint64
		components                                   []string
	}{
		{LinuxARM64DistributionName, "linux_arm64", sync.LinuxArtifactURI, linuxHash, sync.LinuxArtifactSignature, "supported", sync.LinuxArtifactByteSize, linuxComponents},
	}
	if linuxAMD64Configured {
		distributions = append(distributions, struct {
			name, platform, uri, hash, signature, status string
			byteSize                                     uint64
			components                                   []string
		}{LinuxAMD64DistributionName, "linux_amd64", sync.LinuxAMD64ArtifactURI, linuxAMD64Hash, sync.LinuxAMD64ArtifactSignature, "supported", sync.LinuxAMD64ArtifactByteSize, linuxComponents})
	}
	if windowsConfigured {
		distributions = append(distributions, struct {
			name, platform, uri, hash, signature, status string
			byteSize                                     uint64
			components                                   []string
		}{WindowsAMD64DistributionName, "windows_amd64", sync.WindowsArtifactURI, windowsHash, sync.WindowsArtifactSignature, "validation_pending", sync.WindowsArtifactByteSize,
			[]string{"otlp", "hostmetrics", "filelog", "windowseventlog", "prometheus", "batch", "memory_limiter", "file_storage", "argus_identity"}})
	}
	for _, distribution := range distributions {
		artifacts, marshalErr := json.Marshal([]map[string]any{{
			"platform": distribution.platform, "uri": distribution.uri, "sha256": distribution.hash,
			"signature": distribution.signature, "signing_key_id": sync.SigningKeyID, "byte_size": distribution.byteSize,
		}})
		if marshalErr != nil {
			return marshalErr
		}
		_, err = sync.Service.Store.Queries.UpsertCollectorDistributionVersion(ctx, db.UpsertCollectorDistributionVersionParams{
			ID:   uuid.NewSHA1(uuid.NameSpaceURL, []byte("argus.collector.distribution/"+distribution.name+"/"+sync.Version)),
			Name: distribution.name, Version: sync.Version, CollectorVersion: "0.133.0", ConfigSchemaVersion: "argus.collector_config/v1",
			SupportStatus: distribution.status, Components: distribution.components, ArtifactManifest: artifacts, CatalogRevision: CatalogRevision,
		})
		if err != nil {
			return err
		}
	}
	profiles := []struct {
		key, name, description, status         string
		signals, components, platforms, claims []string
	}{
		{"host-basic", "Host basic", "Bounded host metrics and Collector self telemetry.", "supported", []string{"metrics"}, []string{"hostmetrics", "collector-self"}, []string{"linux_arm64", "linux_amd64"}, []string{"host"}},
		{"linux-journald", "Linux journald", "Controlled journald ingestion.", "supported", []string{"logs"}, []string{"journald"}, []string{"linux_arm64", "linux_amd64"}, []string{"host-log"}},
		{"file-log", "File log", "Controlled allowlisted file log ingestion.", "supported", []string{"logs"}, []string{"filelog"}, []string{"linux_arm64", "linux_amd64"}, []string{"host-log"}},
		{"prometheus-endpoint", "Prometheus endpoint", "Bounded Prometheus endpoint scraping.", "supported", []string{"metrics"}, []string{"prometheus"}, []string{"linux_arm64", "linux_amd64"}, []string{"prometheus-target"}},
		{"otlp-receiver", "OTLP receiver", "Bounded local OTLP receiver.", "supported", []string{"metrics", "logs", "traces"}, []string{"otlp"}, []string{"linux_arm64", "linux_amd64"}, []string{"otlp-source"}},
		{"k8s-node-container", "Kubernetes node and container", "DaemonSet node, pod, and container telemetry.", "supported", []string{"metrics", "logs"}, []string{"kubeletstats", "filelog"}, []string{"linux_arm64", "linux_amd64"}, []string{"kubernetes-node"}},
		{"k8s-cluster", "Kubernetes cluster", "Cluster metadata telemetry.", "supported", []string{"metrics"}, []string{"k8scluster"}, []string{"linux_arm64", "linux_amd64"}, []string{"kubernetes-cluster"}},
		{"k8s-otlp-gateway", "Kubernetes OTLP gateway", "In-cluster aggregation gateway.", "supported", []string{"metrics", "logs", "traces"}, []string{"otlp", "batch"}, []string{"linux_arm64", "linux_amd64"}, []string{"kubernetes-gateway"}},
		{"collector-self", "Collector self", "Collector health and queue telemetry.", "supported", []string{"metrics", "logs"}, []string{"collector-self"}, []string{"linux_arm64", "linux_amd64"}, []string{"collector"}},
	}
	if windowsConfigured {
		profiles = append(profiles, struct {
			key, name, description, status         string
			signals, components, platforms, claims []string
		}{"windows-event-log", "Windows event log", "Windows Event Log profile pending physical validation.", "validation_pending", []string{"logs"}, []string{"windowseventlog"}, []string{"windows_amd64"}, []string{"host-log"}})
	}
	for _, profile := range profiles {
		_, err = sync.Service.Store.Queries.UpsertCollectionProfile(ctx, db.UpsertCollectionProfileParams{
			ID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("argus.collection-profile/"+profile.key+"/v1")), ProfileKey: profile.key, Version: "1",
			Name: profile.name, Description: profile.description, Signals: profile.signals, RequiredComponents: profile.components,
			SupportedPlatforms: profile.platforms, ClaimTypes: profile.claims, ConfigSchemaVersion: "argus.collector_config/v1", SupportStatus: profile.status, CatalogRevision: CatalogRevision,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func validateArtifactSignature(publicKey []byte, hash, signature string, byteSize uint64) error {
	if byteSize < 1 || byteSize > 256<<20 {
		return errors.New("telemetry catalog artifact size is invalid")
	}
	digest, err := hex.DecodeString(hash)
	if err != nil {
		return errors.New("telemetry catalog artifact hash is invalid")
	}
	decoded, err := base64.RawStdEncoding.DecodeString(signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), digest, decoded) {
		return errors.New("telemetry catalog artifact signature is invalid")
	}
	return nil
}

func validateArtifactHash(value string) (string, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("telemetry catalog artifact SHA-256 is required")
	}
	return hex.EncodeToString(decoded), nil
}
