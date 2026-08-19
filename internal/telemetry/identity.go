package telemetry

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	collectorCertificateTTL = 24 * time.Hour
	enrollmentTokenTTL      = 10 * time.Minute
)

var (
	ErrEnrollmentInvalid = errors.New("telemetry collector enrollment invalid")
	ErrCertificateFenced = errors.New("telemetry collector certificate fenced")
)

type IdentityService struct {
	Store  *postgres.Store
	Issuer connector.CertificateIssuer
}

type EnrollmentResult struct {
	CollectorID        uuid.UUID
	CertificatePEM     string
	CABundlePEM        string
	IngestGRPCEndpoint string
	IngestHTTPEndpoint string
	ExpiresAt          time.Time
}

func CollectorCertificateURI(collectorID uuid.UUID) string {
	return "spiffe://argus/telemetry/collectors/" + collectorID.String()
}

func CollectorCertificateDNSName(collectorID uuid.UUID) string {
	return "collector-" + collectorID.String() + ".argus.telemetry"
}

func (service IdentityService) CreateEnrollmentToken(ctx context.Context, q *db.Queries, collectorID uuid.UUID) (string, error) {
	if service.Store == nil && q == nil {
		return "", ErrUnavailable
	}
	token, err := identity.RandomToken(32)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(token))
	queries := q
	if queries == nil {
		queries = service.Store.Queries
	}
	_, err = queries.CreateTelemetryEnrollmentToken(ctx, db.CreateTelemetryEnrollmentTokenParams{
		ID: newTelemetryID(), CollectorID: collectorID, TokenHash: hash[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(enrollmentTokenTTL), Valid: true},
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (service IdentityService) Enroll(ctx context.Context, token, collectorIDValue, csrPEM string) (EnrollmentResult, error) {
	collectorID, err := uuid.Parse(collectorIDValue)
	if err != nil || token == "" || service.Store == nil || service.Issuer == nil {
		return EnrollmentResult{}, ErrEnrollmentInvalid
	}
	csr, csrHash, publicKeyHash, err := parseCollectorCSR(csrPEM, collectorID)
	if err != nil {
		return EnrollmentResult{}, err
	}
	tokenHash := sha256.Sum256([]byte(token))
	var result EnrollmentResult
	err = service.Store.InTx(ctx, func(q *db.Queries) error {
		record, err := q.GetTelemetryEnrollmentTokenForUpdate(ctx, tokenHash[:])
		if err != nil || record.CollectorID != collectorID || record.ConsumedAt.Valid || !time.Now().UTC().Before(record.ExpiresAt.Time) {
			return ErrEnrollmentInvalid
		}
		collector, err := q.GetCollectorInstanceByID(ctx, collectorID)
		if err != nil || collector.Status == "uninstalled" || collector.Status == "result_unknown" {
			return ErrEnrollmentInvalid
		}
		issued, err := service.Issuer.Issue(ctx, collectorID, csr, collectorCertificateTTL)
		if err != nil {
			return err
		}
		certificate, err := validateIssuedCollectorCertificate(issued, collectorID, publicKeyHash)
		if err != nil {
			return err
		}
		certificateHash := sha256.Sum256(certificate.Raw)
		if _, err = q.CreateTelemetryCertificate(ctx, db.CreateTelemetryCertificateParams{
			ID: newTelemetryID(), CollectorID: collectorID, SerialNumber: strings.ToLower(certificate.SerialNumber.Text(16)),
			UriSan: CollectorCertificateURI(collectorID), CsrHash: csrHash, CertificateHash: certificateHash[:],
			CertificateRequestName: issued.CertificateRequestName, IssuerGeneration: issued.IssuerGeneration,
			NotBefore: pgtype.Timestamptz{Time: certificate.NotBefore.UTC(), Valid: true},
			NotAfter:  pgtype.Timestamptz{Time: certificate.NotAfter.UTC(), Valid: true},
		}); err != nil {
			return err
		}
		if _, err = q.ConsumeTelemetryEnrollmentToken(ctx, record.ID); err != nil {
			return ErrEnrollmentInvalid
		}
		result = EnrollmentResult{CollectorID: collectorID, CertificatePEM: issued.PEM, CABundlePEM: issued.CABundlePEM, ExpiresAt: certificate.NotAfter.UTC()}
		return nil
	})
	return result, err
}

func (service IdentityService) Rotate(ctx context.Context, certificate *x509.Certificate, csrPEM string) (EnrollmentResult, error) {
	if service.Store == nil || service.Issuer == nil || certificate == nil || len(certificate.URIs) != 1 {
		return EnrollmentResult{}, ErrCertificateFenced
	}
	collectorID, err := collectorIDFromURI(certificate.URIs[0])
	if err != nil {
		return EnrollmentResult{}, ErrCertificateFenced
	}
	serial := strings.ToLower(certificate.SerialNumber.Text(16))
	csr, csrHash, publicKeyHash, err := parseCollectorCSR(csrPEM, collectorID)
	if err != nil {
		return EnrollmentResult{}, err
	}
	var result EnrollmentResult
	err = service.Store.InTx(ctx, func(q *db.Queries) error {
		if _, err := q.GetValidTelemetryCertificateBySerial(ctx, db.GetValidTelemetryCertificateBySerialParams{CollectorID: collectorID, SerialNumber: serial}); err != nil {
			return ErrCertificateFenced
		}
		collector, err := q.GetCollectorInstanceByID(ctx, collectorID)
		if err != nil || collector.Status == "uninstalled" {
			return ErrCertificateFenced
		}
		issued, err := service.Issuer.Issue(ctx, collectorID, csr, collectorCertificateTTL)
		if err != nil {
			return err
		}
		rotated, err := validateIssuedCollectorCertificate(issued, collectorID, publicKeyHash)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(rotated.Raw)
		newSerial := strings.ToLower(rotated.SerialNumber.Text(16))
		if _, err = q.CreateTelemetryCertificate(ctx, db.CreateTelemetryCertificateParams{
			ID: newTelemetryID(), CollectorID: collectorID, SerialNumber: newSerial, UriSan: CollectorCertificateURI(collectorID),
			CsrHash: csrHash, CertificateHash: hash[:], CertificateRequestName: issued.CertificateRequestName,
			IssuerGeneration: issued.IssuerGeneration, NotBefore: pgtype.Timestamptz{Time: rotated.NotBefore.UTC(), Valid: true},
			NotAfter: pgtype.Timestamptz{Time: rotated.NotAfter.UTC(), Valid: true},
		}); err != nil {
			return err
		}
		if err = q.LimitTelemetryCertificateOverlap(ctx, db.LimitTelemetryCertificateOverlapParams{CollectorID: collectorID, SerialNumber: newSerial}); err != nil {
			return err
		}
		result = EnrollmentResult{CollectorID: collectorID, CertificatePEM: issued.PEM, CABundlePEM: issued.CABundlePEM, ExpiresAt: rotated.NotAfter.UTC()}
		return nil
	})
	return result, err
}

func parseCollectorCSR(value string, collectorID uuid.UUID) (*x509.CertificateRequest, []byte, []byte, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE REQUEST" {
		return nil, nil, nil, ErrEnrollmentInvalid
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil || len(csr.URIs) != 1 || len(csr.DNSNames) != 1 || len(csr.IPAddresses) != 0 ||
		csr.URIs[0].String() != CollectorCertificateURI(collectorID) || csr.DNSNames[0] != CollectorCertificateDNSName(collectorID) {
		return nil, nil, nil, ErrEnrollmentInvalid
	}
	publicKey, err := x509.MarshalPKIXPublicKey(csr.PublicKey)
	if err != nil {
		return nil, nil, nil, ErrEnrollmentInvalid
	}
	csrHash := sha256.Sum256(csr.Raw)
	keyHash := sha256.Sum256(publicKey)
	return csr, csrHash[:], keyHash[:], nil
}

func validateIssuedCollectorCertificate(issued connector.Certificate, collectorID uuid.UUID, expectedPublicKeyHash []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode([]byte(issued.PEM))
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE" {
		return nil, ErrEnrollmentInvalid
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(certificate.URIs) != 1 || len(certificate.DNSNames) != 1 ||
		certificate.URIs[0].String() != CollectorCertificateURI(collectorID) || certificate.DNSNames[0] != CollectorCertificateDNSName(collectorID) ||
		certificate.NotAfter.Sub(certificate.NotBefore) > collectorCertificateTTL+time.Minute {
		return nil, ErrEnrollmentInvalid
	}
	publicKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return nil, ErrEnrollmentInvalid
	}
	hash := sha256.Sum256(publicKey)
	if subtle.ConstantTimeCompare(hash[:], expectedPublicKeyHash) != 1 {
		return nil, ErrEnrollmentInvalid
	}
	return certificate, nil
}

func collectorIDFromURI(value *url.URL) (uuid.UUID, error) {
	if value == nil || value.Scheme != "spiffe" || value.Host != "argus" || !strings.HasPrefix(value.Path, "/telemetry/collectors/") {
		return uuid.Nil, ErrCertificateFenced
	}
	id, err := uuid.Parse(strings.TrimPrefix(value.Path, "/telemetry/collectors/"))
	if err != nil || value.Path != "/telemetry/collectors/"+id.String() {
		return uuid.Nil, fmt.Errorf("%w: invalid URI SAN", ErrCertificateFenced)
	}
	return id, nil
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
