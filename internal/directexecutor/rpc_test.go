package directexecutor

import (
	"crypto/x509"
	"math/big"
	"net/url"
	"testing"

	"github.com/kakj-go/Argus/internal/trustbundle"
)

func TestDirectExecutorAcceptsExplicitServiceURIIdentities(t *testing.T) {
	allowed := []string{"spiffe://argus.io/services/server/client", "spiffe://argus.io/services/connector-gateway/direct-executor-client"}
	for _, identity := range allowed {
		t.Run(identity, func(t *testing.T) {
			uri, err := url.Parse(identity)
			if err != nil {
				t.Fatal(err)
			}
			certificate := &x509.Certificate{SerialNumber: big.NewInt(1), URIs: []*url.URL{uri}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
			if _, err = trustbundle.ServiceCertificateIdentity(certificate, allowed); err != nil {
				t.Fatalf("authorized service identity was rejected: %v", err)
			}
		})
	}
}

func TestDirectExecutorRejectsUnlistedServiceURI(t *testing.T) {
	uri, _ := url.Parse("spiffe://argus.io/services/untrusted/client")
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), URIs: []*url.URL{uri}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if _, err := trustbundle.ServiceCertificateIdentity(certificate, []string{"spiffe://argus.io/services/server/client"}); err == nil {
		t.Fatal("unlisted Direct Executor client identity was accepted")
	}
}
