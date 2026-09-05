package argusidentity

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	bundleStateStable      = "stable"
	bundleStatePreparing   = "preparing"
	bundleStateOverlapping = "overlapping"
	bundleStateRetiring    = "retiring"
)

type bundleMaterial struct {
	PEM          []byte
	SHA256       string
	Fingerprints []string
}

type bundleSnapshot struct {
	Epoch                 int64
	State                 string
	Material              bundleMaterial
	CurrentCAFingerprints []string
	NextCAFingerprints    []string
	StartedAt             time.Time
	RetireAt              time.Time
}

func parseTrustBundle(value []byte, now time.Time) (bundleMaterial, error) {
	rest := bytes.TrimSpace(value)
	if len(rest) == 0 {
		return bundleMaterial{}, errors.New("telemetry Trust Bundle is empty")
	}
	blocks := map[string][]byte{}
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return bundleMaterial{}, errors.New("telemetry Trust Bundle contains non-certificate data")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 ||
			now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return bundleMaterial{}, errors.New("telemetry Trust Bundle contains an invalid CA")
		}
		digest := sha256.Sum256(certificate.Raw)
		fingerprint := hex.EncodeToString(digest[:])
		if _, exists := blocks[fingerprint]; exists {
			return bundleMaterial{}, fmt.Errorf("telemetry Trust Bundle contains duplicate CA %s", fingerprint)
		}
		blocks[fingerprint] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
		rest = bytes.TrimSpace(remaining)
	}
	fingerprints := make([]string, 0, len(blocks))
	for fingerprint := range blocks {
		fingerprints = append(fingerprints, fingerprint)
	}
	slices.Sort(fingerprints)
	var canonical bytes.Buffer
	for _, fingerprint := range fingerprints {
		canonical.Write(blocks[fingerprint])
	}
	digest := sha256.Sum256(canonical.Bytes())
	return bundleMaterial{PEM: canonical.Bytes(), SHA256: hex.EncodeToString(digest[:]), Fingerprints: fingerprints}, nil
}

func bundleMatches(snapshot bundleSnapshot, epoch int64, digest string, fingerprints []string) bool {
	normalized := append([]string{}, fingerprints...)
	slices.Sort(normalized)
	return snapshot.Epoch == epoch && snapshot.Material.SHA256 == digest && slices.Equal(snapshot.Material.Fingerprints, normalized)
}
