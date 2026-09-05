package telemetry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/tlsmaterial"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

func ServerTLSConfig(certPath, keyPath, clientCAPath string, queries *db.Queries, authorizedClientURIs []string) (*tls.Config, error) {
	material, err := tlsmaterial.Load(tlsmaterial.Options{CertificatePath: certPath, PrivateKeyPath: keyPath,
		CABundlePath: clientCAPath, Usage: x509.ExtKeyUsageServerAuth})
	if err != nil || queries == nil || len(authorizedClientURIs) == 0 {
		if err == nil {
			err = errors.New("telemetry query service identity registry is not configured")
		}
		return nil, err
	}
	return material.ServerConfig(tls.RequireAndVerifyClientCert, func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("telemetry query client certificate is missing")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return trustbundle.VerifyServiceCertificate(ctx, queries, state.PeerCertificates[0], authorizedClientURIs)
	})
}

func EnrollmentServerTLSConfig(certPath, keyPath, clientCAPath string) (*tls.Config, error) {
	return serverTLSConfig(certPath, keyPath, clientCAPath, tls.VerifyClientCertIfGiven)
}

func serverTLSConfig(certPath, keyPath, clientCAPath string, clientAuth tls.ClientAuthType) (*tls.Config, error) {
	material, err := tlsmaterial.Load(tlsmaterial.Options{CertificatePath: certPath, PrivateKeyPath: keyPath,
		CABundlePath: clientCAPath, Usage: x509.ExtKeyUsageServerAuth})
	if err != nil {
		return nil, err
	}
	return material.ServerConfig(clientAuth, nil)
}

func ClientTLSConfig(certPath, keyPath, caPath, serverName string) (credentials.TransportCredentials, error) {
	material, err := tlsmaterial.Load(tlsmaterial.Options{CertificatePath: certPath, PrivateKeyPath: keyPath,
		CABundlePath: caPath, Usage: x509.ExtKeyUsageClientAuth})
	if err != nil {
		return nil, err
	}
	return tlsmaterial.ClientCredentials(material, serverName)
}
