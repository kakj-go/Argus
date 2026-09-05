package trustbundle

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type CertificateIdentity struct {
	Kind             string
	SubjectID        string
	EnterpriseID     uuid.NullUUID
	Usage            x509.ExtKeyUsage
	IssuerGeneration int32
}

func RegisterCertificateIdentity(ctx context.Context, q *db.Queries, certificate *x509.Certificate, identity CertificateIdentity) error {
	if q == nil || certificate == nil || identity.SubjectID == "" ||
		(identity.Kind != "connector" && identity.Kind != "collector" && identity.Kind != "service") {
		return errors.New("PKI certificate identity is invalid")
	}
	usage := ""
	switch identity.Usage {
	case x509.ExtKeyUsageClientAuth:
		usage = "clientAuth"
	case x509.ExtKeyUsageServerAuth:
		usage = "serverAuth"
	default:
		return errors.New("PKI certificate identity EKU is invalid")
	}
	if len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != identity.Usage || certificate.SerialNumber == nil {
		return errors.New("PKI certificate must have exactly one expected EKU")
	}
	uriSAN := ""
	if len(certificate.URIs) > 1 {
		return errors.New("PKI certificate has multiple URI identities")
	}
	if len(certificate.URIs) == 1 {
		uriSAN = certificate.URIs[0].String()
	}
	digest := sha256.Sum256(certificate.Raw)
	record, err := q.CreatePKICertificateIdentity(ctx, db.CreatePKICertificateIdentityParams{
		SerialNumber: CertificateSerial(certificate), SubjectKind: identity.Kind, SubjectID: identity.SubjectID,
		EnterpriseID: identity.EnterpriseID, UriSan: uriSAN, DnsSans: certificate.DNSNames, ExtendedKeyUsage: usage,
		IssuerGeneration: identity.IssuerGeneration, CertificateSha256: hex.EncodeToString(digest[:]), Status: "active",
		NotBefore: pgtype.Timestamptz{Time: certificate.NotBefore.UTC(), Valid: true}, NotAfter: pgtype.Timestamptz{Time: certificate.NotAfter.UTC(), Valid: true},
	})
	if err != nil {
		return err
	}
	return VerifyCertificateIdentity(record, certificate, identity)
}

func CertificateSerial(certificate *x509.Certificate) string {
	if certificate == nil || certificate.SerialNumber == nil {
		return ""
	}
	return strings.ToLower(certificate.SerialNumber.Text(16))
}

func VerifyCertificateIdentity(record db.PkiCertificateIdentity, certificate *x509.Certificate, identity CertificateIdentity) error {
	if certificate == nil || record.SerialNumber != CertificateSerial(certificate) || record.SubjectKind != identity.Kind ||
		record.SubjectID != identity.SubjectID || record.EnterpriseID != identity.EnterpriseID ||
		(record.ExtendedKeyUsage == "clientAuth") != (identity.Usage == x509.ExtKeyUsageClientAuth) ||
		(record.ExtendedKeyUsage == "serverAuth") != (identity.Usage == x509.ExtKeyUsageServerAuth) {
		return errors.New("PKI certificate role, tenant, or usage is invalid")
	}
	uriSAN := ""
	if len(certificate.URIs) == 1 {
		uriSAN = certificate.URIs[0].String()
	}
	if len(certificate.URIs) > 1 || record.UriSan != uriSAN {
		return errors.New("PKI certificate URI identity is invalid")
	}
	digest := sha256.Sum256(certificate.Raw)
	if record.CertificateSha256 != hex.EncodeToString(digest[:]) {
		return errors.New("PKI certificate fingerprint is invalid")
	}
	return nil
}
