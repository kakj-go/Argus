package directexecutor

import (
	"crypto/x509"
	"testing"
)

func TestAuthorizeClientCertificateAcceptsExplicitServiceIdentities(t *testing.T) {
	allowed := []string{"argus-server", "argus-connector-gateway"}
	for _, identity := range allowed {
		t.Run(identity, func(t *testing.T) {
			certificate := &x509.Certificate{DNSNames: []string{identity}}
			if err := authorizeClientCertificate(certificate, allowed); err != nil {
				t.Fatalf("authorized service identity was rejected: %v", err)
			}
		})
	}
}

func TestAuthorizeClientCertificateRejectsUnlistedIdentity(t *testing.T) {
	certificate := &x509.Certificate{DNSNames: []string{"untrusted-worker"}}
	if err := authorizeClientCertificate(certificate, []string{"argus-server", "argus-connector-gateway"}); err == nil {
		t.Fatal("unlisted Direct Executor client identity was accepted")
	}
}
