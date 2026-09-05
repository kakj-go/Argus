// Package tlsmaterial provides fail-closed, reloadable TLS material for Argus
// services and clients. Kubernetes projected volumes are replaced through
// symlinks, so every reload reads all files before atomically publishing a
// validated snapshot.
package tlsmaterial

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	CertificatePath string
	PrivateKeyPath  string
	CABundlePath    string
	Usage           x509.ExtKeyUsage
	RequiredDNS     []string
	RequiredURIs    []string
	ReloadInterval  time.Duration
	OnReload        func(State)
	OnError         func(error)
}

type State struct {
	SHA256      string
	Reloads     uint64
	Failures    uint64
	LastSuccess time.Time
	LastFailure time.Time
	LastError   string
	Certificate *x509.Certificate
}

type snapshot struct {
	hash        [32]byte
	certificate *tls.Certificate
	leaf        *x509.Certificate
	roots       *x509.CertPool
}

type Material struct {
	options Options
	current atomic.Pointer[snapshot]
	reload  sync.Mutex
	state   sync.RWMutex
	status  State
}

func Load(options Options) (*Material, error) {
	if strings.TrimSpace(options.CABundlePath) == "" {
		return nil, errors.New("TLS CA bundle path is required")
	}
	if (options.CertificatePath == "") != (options.PrivateKeyPath == "") {
		return nil, errors.New("TLS certificate and private key must be configured together")
	}
	if options.CertificatePath != "" && options.Usage != x509.ExtKeyUsageServerAuth && options.Usage != x509.ExtKeyUsageClientAuth {
		return nil, errors.New("TLS leaf usage must be serverAuth or clientAuth")
	}
	if options.ReloadInterval <= 0 {
		options.ReloadInterval = 5 * time.Second
	}
	material := &Material{options: options}
	if err := material.Reload(); err != nil {
		return nil, err
	}
	return material, nil
}

// Reload publishes a new snapshot only when every input validates. The last
// valid snapshot remains available when a projected update is partial or bad.
func (material *Material) Reload() error {
	if material == nil {
		return errors.New("TLS material is nil")
	}
	material.reload.Lock()
	defer material.reload.Unlock()
	loaded, err := material.readSnapshot()
	if err != nil {
		material.recordFailure(err)
		if material.options.OnError != nil {
			material.options.OnError(err)
		}
		return err
	}
	if current := material.current.Load(); current != nil && current.hash == loaded.hash {
		return nil
	}
	material.current.Store(loaded)
	material.state.Lock()
	material.status.SHA256 = hex.EncodeToString(loaded.hash[:])
	material.status.Reloads++
	material.status.LastSuccess = time.Now().UTC()
	material.status.LastError = ""
	material.status.Certificate = loaded.leaf
	state := material.status
	material.state.Unlock()
	if material.options.OnReload != nil {
		material.options.OnReload(state)
	}
	return nil
}

func (material *Material) Watch(ctx context.Context) {
	if material == nil {
		return
	}
	ticker := time.NewTicker(material.options.ReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = material.Reload()
		}
	}
}

func (material *Material) State() State {
	if material == nil {
		return State{}
	}
	material.state.RLock()
	defer material.state.RUnlock()
	return material.status
}

func (material *Material) ServerConfig(clientAuth tls.ClientAuthType, verifyConnection func(tls.ConnectionState) error) (*tls.Config, error) {
	current, err := material.validSnapshot()
	if err != nil || current.certificate == nil || material.options.Usage != x509.ExtKeyUsageServerAuth {
		return nil, errors.New("server TLS material is unavailable or not serverAuth")
	}
	base := &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: clientAuth, NextProtos: []string{"h2", "http/1.1"}, VerifyConnection: verifyConnection}
	applyServerSnapshot(base, current)
	base.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		current := material.latestSnapshot()
		configuration := base.Clone()
		configuration.GetConfigForClient = nil
		applyServerSnapshot(configuration, current)
		return configuration, nil
	}
	return base, nil
}

func (material *Material) ClientConfig(serverName string) (*tls.Config, error) {
	if strings.TrimSpace(serverName) == "" {
		return nil, errors.New("TLS server name is required")
	}
	current := material.latestSnapshot()
	configuration := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: current.roots, NextProtos: []string{"h2", "http/1.1"}}
	if current.certificate != nil {
		if material.options.Usage != x509.ExtKeyUsageClientAuth {
			return nil, errors.New("client TLS material is not clientAuth")
		}
		configuration.Certificates = []tls.Certificate{*current.certificate}
	}
	return configuration, nil
}

func (material *Material) latestSnapshot() *snapshot {
	_ = material.Reload()
	current, _ := material.validSnapshot()
	return current
}

func (material *Material) validSnapshot() (*snapshot, error) {
	if material == nil {
		return nil, errors.New("TLS material is nil")
	}
	current := material.current.Load()
	if current == nil {
		return nil, errors.New("TLS material has no valid snapshot")
	}
	return current, nil
}

func applyServerSnapshot(configuration *tls.Config, current *snapshot) {
	configuration.Certificates = []tls.Certificate{*current.certificate}
	configuration.ClientCAs = current.roots
}

func (material *Material) readSnapshot() (*snapshot, error) {
	caPEM, err := os.ReadFile(material.options.CABundlePath)
	if err != nil {
		return nil, fmt.Errorf("read TLS CA bundle: %w", err)
	}
	roots, err := parseCABundle(caPEM, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	hasher := sha256.New()
	_, _ = hasher.Write(caPEM)
	loaded := &snapshot{roots: roots}
	if material.options.CertificatePath != "" {
		certificatePEM, readErr := os.ReadFile(material.options.CertificatePath)
		if readErr != nil {
			return nil, fmt.Errorf("read TLS certificate: %w", readErr)
		}
		privateKeyPEM, readErr := os.ReadFile(material.options.PrivateKeyPath)
		if readErr != nil {
			return nil, fmt.Errorf("read TLS private key: %w", readErr)
		}
		certificate, leaf, parseErr := parseLeaf(certificatePEM, privateKeyPEM, roots, material.options, time.Now().UTC())
		if parseErr != nil {
			return nil, parseErr
		}
		loaded.certificate, loaded.leaf = &certificate, leaf
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(certificatePEM)
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(privateKeyPEM)
	}
	copy(loaded.hash[:], hasher.Sum(nil))
	return loaded, nil
}

func parseCABundle(value []byte, now time.Time) (*x509.CertPool, error) {
	rest := bytes.TrimSpace(value)
	pool := x509.NewCertPool()
	seen := map[[32]byte]struct{}{}
	count := 0
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, errors.New("TLS CA bundle must contain only PEM certificates")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
			return nil, errors.New("TLS CA bundle contains an invalid CA certificate")
		}
		if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return nil, errors.New("TLS CA bundle contains a certificate outside its validity period")
		}
		digest := sha256.Sum256(block.Bytes)
		if _, exists := seen[digest]; exists {
			return nil, errors.New("TLS CA bundle contains a duplicate certificate")
		}
		seen[digest] = struct{}{}
		pool.AddCert(certificate)
		count++
		rest = bytes.TrimSpace(remaining)
	}
	if count == 0 {
		return nil, errors.New("TLS CA bundle contains no certificates")
	}
	return pool, nil
}

func parseLeaf(certificatePEM, privateKeyPEM []byte, roots *x509.CertPool, options Options, now time.Time) (tls.Certificate, *x509.Certificate, error) {
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return tls.Certificate{}, nil, errors.New("TLS certificate and private key do not match")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.IsCA || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return tls.Certificate{}, nil, errors.New("TLS leaf certificate is invalid or outside its validity period")
	}
	publicKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() {
		return tls.Certificate{}, nil, errors.New("TLS leaf certificate must use ECDSA P-256")
	}
	if len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != options.Usage {
		return tls.Certificate{}, nil, errors.New("TLS leaf certificate has the wrong or mixed EKU")
	}
	for _, name := range options.RequiredDNS {
		if !slices.Contains(leaf.DNSNames, name) {
			return tls.Certificate{}, nil, fmt.Errorf("TLS leaf certificate is missing DNS SAN %s", name)
		}
	}
	actualURIs := make([]string, 0, len(leaf.URIs))
	for _, value := range leaf.URIs {
		actualURIs = append(actualURIs, value.String())
	}
	for _, value := range options.RequiredURIs {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || parsed.Scheme == "" || !slices.Contains(actualURIs, value) {
			return tls.Certificate{}, nil, fmt.Errorf("TLS leaf certificate is missing URI SAN %s", value)
		}
	}
	intermediates := x509.NewCertPool()
	for _, raw := range pair.Certificate[1:] {
		certificate, parseErr := x509.ParseCertificate(raw)
		if parseErr != nil || !certificate.IsCA {
			return tls.Certificate{}, nil, errors.New("TLS leaf chain contains an invalid intermediate")
		}
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{options.Usage}, CurrentTime: now}); err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("verify TLS leaf chain: %w", err)
	}
	pair.Leaf = leaf
	return pair, leaf, nil
}

func (material *Material) recordFailure(err error) {
	material.state.Lock()
	material.status.Failures++
	material.status.LastFailure = time.Now().UTC()
	material.status.LastError = err.Error()
	material.state.Unlock()
}
