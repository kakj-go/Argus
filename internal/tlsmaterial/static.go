package tlsmaterial

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
	"time"
)

// StaticClientConfig applies the same strict CA, key, chain and EKU
// validation as reloadable Material to transient enrollment connections whose
// Trust Bundle is carried in a signed control command instead of a file.
func StaticClientConfig(caPEM, certificatePEM, privateKeyPEM []byte, serverName string) (*tls.Config, error) {
	if strings.TrimSpace(serverName) == "" {
		return nil, errors.New("TLS server name is required")
	}
	roots, err := parseCABundle(caPEM, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	configuration := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots,
		NextProtos: []string{"h2", "http/1.1"}}
	if len(certificatePEM) == 0 && len(privateKeyPEM) == 0 {
		return configuration, nil
	}
	if len(certificatePEM) == 0 || len(privateKeyPEM) == 0 {
		return nil, errors.New("TLS client certificate and private key must be configured together")
	}
	pair, _, err := parseLeaf(certificatePEM, privateKeyPEM, roots, Options{Usage: x509.ExtKeyUsageClientAuth}, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	configuration.Certificates = []tls.Certificate{pair}
	return configuration, nil
}
