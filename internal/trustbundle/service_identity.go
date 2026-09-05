package trustbundle

import (
	"context"
	"crypto/x509"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

// ServiceCertificateIdentity validates the application identity encoded in a
// service-to-service client leaf. CA verification is performed by crypto/tls;
// this function enforces the narrower Argus authorization boundary.
func ServiceCertificateIdentity(certificate *x509.Certificate, allowedURIs []string) (CertificateIdentity, error) {
	if certificate == nil || certificate.SerialNumber == nil || len(certificate.URIs) != 1 ||
		len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		return CertificateIdentity{}, errors.New("service client certificate must contain one URI identity and only clientAuth EKU")
	}
	uri := certificate.URIs[0].String()
	if uri == "" || !slices.Contains(allowedURIs, uri) {
		return CertificateIdentity{}, errors.New("service client certificate URI identity is not authorized")
	}
	return CertificateIdentity{Kind: "service", SubjectID: uri, EnterpriseID: uuid.NullUUID{}, Usage: x509.ExtKeyUsageClientAuth}, nil
}

// VerifyServiceCertificate requires the presented leaf to be both authorized
// for this server and present as active/overlap in the PKI identity registry.
// Revoked, expired, unknown, wrong-role, wrong-fingerprint and wrong-URI leaves
// therefore fail closed even when they chain to the shared Argus CA.
func VerifyServiceCertificate(ctx context.Context, queries *db.Queries, certificate *x509.Certificate, allowedURIs []string) error {
	if queries == nil {
		return errors.New("service certificate identity registry is unavailable")
	}
	identity, err := ServiceCertificateIdentity(certificate, allowedURIs)
	if err != nil {
		return err
	}
	record, err := queries.GetActivePKICertificateIdentity(ctx, CertificateSerial(certificate))
	if err != nil {
		return errors.New("service client certificate is unknown, expired, or revoked")
	}
	return VerifyCertificateIdentity(record, certificate, identity)
}
