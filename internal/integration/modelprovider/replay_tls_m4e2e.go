//go:build m4e2e

package modelprovider

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
)

func configureE2EReplayTLS(transport *http.Transport) {
	transport.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // Verification is performed below for both paths.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("model endpoint did not present a certificate")
			}
			if isE2EReplayHost(state.ServerName) {
				return state.PeerCertificates[0].VerifyHostname(state.ServerName)
			}
			intermediates := x509.NewCertPool()
			for _, certificate := range state.PeerCertificates[1:] {
				intermediates.AddCert(certificate)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{
				DNSName:       state.ServerName,
				Intermediates: intermediates,
			})
			return err
		},
	}
}
