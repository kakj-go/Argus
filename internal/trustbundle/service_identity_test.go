package trustbundle

import (
	"crypto/x509"
	"math/big"
	"net/url"
	"testing"
)

func TestServiceCertificateIdentityRequiresAuthorizedSinglePurposeURI(t *testing.T) {
	allowed := "spiffe://argus.io/services/server/client"
	uri, _ := url.Parse(allowed)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(7), URIs: []*url.URL{uri},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	identity, err := ServiceCertificateIdentity(certificate, []string{allowed})
	if err != nil || identity.Kind != "service" || identity.SubjectID != allowed || identity.EnterpriseID.Valid || identity.Usage != x509.ExtKeyUsageClientAuth {
		t.Fatalf("valid service identity rejected: identity=%+v err=%v", identity, err)
	}

	other, _ := url.Parse("spiffe://argus.io/services/worker/client")
	tests := map[string]*x509.Certificate{
		"unlisted URI": {SerialNumber: big.NewInt(8), URIs: []*url.URL{other}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}},
		"mixed EKU": {SerialNumber: big.NewInt(9), URIs: []*url.URL{uri},
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}},
		"multiple URI": {SerialNumber: big.NewInt(10), URIs: []*url.URL{uri, other}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}},
	}
	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ServiceCertificateIdentity(candidate, []string{allowed}); err == nil {
				t.Fatal("invalid service client identity was accepted")
			}
		})
	}
}
