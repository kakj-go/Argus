package telemetry

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

func ServerTLSConfig(certPath, keyPath, clientCAPath string) (*tls.Config, error) {
	return serverTLSConfig(certPath, keyPath, clientCAPath, tls.RequireAndVerifyClientCert)
}

func EnrollmentServerTLSConfig(certPath, keyPath, clientCAPath string) (*tls.Config, error) {
	return serverTLSConfig(certPath, keyPath, clientCAPath, tls.VerifyClientCertIfGiven)
}

func serverTLSConfig(certPath, keyPath, clientCAPath string, clientAuth tls.ClientAuthType) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(clientCAPath)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("telemetry client CA is invalid")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: pool,
		ClientAuth: clientAuth, NextProtos: []string{"h2", "http/1.1"}}, nil
}

func ClientTLSConfig(certPath, keyPath, caPath, serverName string) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("telemetry server CA is invalid")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, RootCAs: pool, ServerName: serverName}, nil
}
