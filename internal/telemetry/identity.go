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

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
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
	Store        *postgres.Store
	Issuer       connector.CertificateIssuer
	ServerIssuer connector.CertificateIssuer
	TrustBundles trustbundle.Service
}

type EnrollmentResult struct {
	CollectorID          uuid.UUID
	ClientCertificatePEM string
	ServerCertificatePEM string
	TrustBundle          trustbundle.Bundle
	IngestGRPCEndpoint   string
	IngestHTTPEndpoint   string
	ExpiresAt            time.Time
}

func CollectorCertificateURI(collectorID uuid.UUID) string {
	return "spiffe://argus/telemetry/collectors/" + collectorID.String()
}

func CollectorCertificateDNSName(collectorID uuid.UUID) string {
	return "collector-" + collectorID.String() + ".argus.telemetry"
}

func CollectorServerCertificateURI(collectorID uuid.UUID) string {
	return "spiffe://argus/telemetry/collector-servers/" + collectorID.String()
}

func (service IdentityService) CreateEnrollmentToken(ctx context.Context, q *db.Queries, collectorID uuid.UUID) (string, error) {
	return service.createEnrollmentToken(ctx, q, collectorID, uuid.NullUUID{})
}

func (service IdentityService) CreateEnrollmentTokenForHost(ctx context.Context, q *db.Queries, collectorID, hostEnrollmentTokenID uuid.UUID) (string, error) {
	return service.createEnrollmentToken(ctx, q, collectorID, uuid.NullUUID{UUID: hostEnrollmentTokenID, Valid: true})
}

func (service IdentityService) createEnrollmentToken(ctx context.Context, q *db.Queries, collectorID uuid.UUID, hostEnrollmentTokenID uuid.NullUUID) (string, error) {
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
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(enrollmentTokenTTL), Valid: true}, HostEnrollmentTokenID: hostEnrollmentTokenID,
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (service IdentityService) Enroll(ctx context.Context, token, collectorIDValue, clientCSRPEM, serverCSRPEM string) (EnrollmentResult, error) {
	collectorID, err := uuid.Parse(collectorIDValue)
	if err != nil || token == "" || service.Store == nil || service.Issuer == nil || service.ServerIssuer == nil {
		return EnrollmentResult{}, ErrEnrollmentInvalid
	}
	clientCSR, clientCSRHash, clientPublicKeyHash, err := parseCollectorCSR(clientCSRPEM, collectorID, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return EnrollmentResult{}, err
	}
	serverCSR, serverCSRHash, serverPublicKeyHash, err := parseCollectorCSR(serverCSRPEM, collectorID, x509.ExtKeyUsageServerAuth)
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
		clientIssued, err := service.Issuer.Issue(ctx, collectorID, clientCSR, collectorCertificateTTL)
		if err != nil {
			return err
		}
		serverIssued, err := service.ServerIssuer.Issue(ctx, collectorID, serverCSR, collectorCertificateTTL)
		if err != nil {
			return err
		}
		clientCertificate, err := validateIssuedCollectorCertificate(clientIssued, collectorID, clientPublicKeyHash, x509.ExtKeyUsageClientAuth)
		if err != nil {
			return err
		}
		serverCertificate, err := validateIssuedCollectorCertificate(serverIssued, collectorID, serverPublicKeyHash, x509.ExtKeyUsageServerAuth)
		if err != nil {
			return err
		}
		clientCertificateHash := sha256.Sum256(clientCertificate.Raw)
		if _, err = q.CreateTelemetryCertificate(ctx, db.CreateTelemetryCertificateParams{
			ID: newTelemetryID(), CollectorID: collectorID, SerialNumber: strings.ToLower(clientCertificate.SerialNumber.Text(16)),
			UriSan: CollectorCertificateURI(collectorID), CsrHash: clientCSRHash, CertificateHash: clientCertificateHash[:],
			CertificateRequestName: clientIssued.CertificateRequestName, IssuerGeneration: clientIssued.IssuerGeneration,
			NotBefore: pgtype.Timestamptz{Time: clientCertificate.NotBefore.UTC(), Valid: true},
			NotAfter:  pgtype.Timestamptz{Time: clientCertificate.NotAfter.UTC(), Valid: true}, CertificateUsage: "clientAuth",
		}); err != nil {
			return err
		}
		if err = trustbundle.RegisterCertificateIdentity(ctx, q, clientCertificate, trustbundle.CertificateIdentity{Kind: "collector",
			SubjectID: collectorID.String(), EnterpriseID: uuid.NullUUID{UUID: collector.EnterpriseID, Valid: true},
			Usage: x509.ExtKeyUsageClientAuth, IssuerGeneration: clientIssued.IssuerGeneration}); err != nil {
			return err
		}
		serverCertificateHash := sha256.Sum256(serverCertificate.Raw)
		if _, err = q.CreateTelemetryCertificate(ctx, db.CreateTelemetryCertificateParams{
			ID: newTelemetryID(), CollectorID: collectorID, SerialNumber: strings.ToLower(serverCertificate.SerialNumber.Text(16)),
			UriSan: CollectorServerCertificateURI(collectorID), CsrHash: serverCSRHash, CertificateHash: serverCertificateHash[:],
			CertificateRequestName: serverIssued.CertificateRequestName, IssuerGeneration: serverIssued.IssuerGeneration,
			NotBefore: pgtype.Timestamptz{Time: serverCertificate.NotBefore.UTC(), Valid: true},
			NotAfter:  pgtype.Timestamptz{Time: serverCertificate.NotAfter.UTC(), Valid: true}, CertificateUsage: "serverAuth",
		}); err != nil {
			return err
		}
		if err = trustbundle.RegisterCertificateIdentity(ctx, q, serverCertificate, trustbundle.CertificateIdentity{Kind: "collector",
			SubjectID: collectorID.String(), EnterpriseID: uuid.NullUUID{UUID: collector.EnterpriseID, Valid: true},
			Usage: x509.ExtKeyUsageServerAuth, IssuerGeneration: serverIssued.IssuerGeneration}); err != nil {
			return err
		}
		if _, err = q.ConsumeTelemetryEnrollmentToken(ctx, record.ID); err != nil {
			return ErrEnrollmentInvalid
		}
		if err = convergeSelfEnrolledHostAfterEnrollment(ctx, q, collector, record); err != nil {
			return err
		}
		touchHostSeen(ctx, q, collector)
		bundle, bundleErr := service.enrollmentTrustBundle(ctx, clientIssued.CABundlePEM)
		if bundleErr != nil {
			return bundleErr
		}
		expiresAt := clientCertificate.NotAfter.UTC()
		if serverCertificate.NotAfter.Before(expiresAt) {
			expiresAt = serverCertificate.NotAfter.UTC()
		}
		result = EnrollmentResult{CollectorID: collectorID, ClientCertificatePEM: clientIssued.PEM,
			ServerCertificatePEM: serverIssued.PEM, TrustBundle: bundle, ExpiresAt: expiresAt}
		return nil
	})
	return result, err
}

func convergeSelfEnrolledHostAfterEnrollment(ctx context.Context, q *db.Queries, collector db.CollectorInstance, enrollment db.TelemetryEnrollmentToken) error {
	if collector.ResourceType != "host" || !enrollment.HostEnrollmentTokenID.Valid {
		return nil
	}
	exchange, err := q.GetHostEnrollmentTokenForUpdate(ctx, enrollment.HostEnrollmentTokenID.UUID)
	if err != nil || exchange.PreallocatedHostID != collector.ResourceID || exchange.EnterpriseID != collector.EnterpriseID {
		return ErrEnrollmentInvalid
	}
	if _, err = q.ActivateSelfEnrolledHost(ctx, db.ActivateSelfEnrolledHostParams{ID: collector.ResourceID,
		EnterpriseID: collector.EnterpriseID, Hostname: exchange.ReportedHostname, Address: exchange.ReportedAddress,
		Architecture: exchange.ReportedArchitecture}); err != nil {
		return err
	}
	if err = finishBootstrapOperation(ctx, q, collector.EnterpriseID, collector, "install"); err != nil {
		return err
	}
	_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: collector.EnterpriseID, Valid: true},
		ActorType: "system", ActorID: "collector-enrollment", Action: "host.self_enrollment.activate", ResourceType: "host",
		ResourceID: collector.ResourceID.String(), Result: "success", Details: map[string]any{"collector_id": collector.ID.String(), "token_id": exchange.ID.String()}})
	return err
}

func (service IdentityService) Rotate(ctx context.Context, certificate *x509.Certificate, clientCSRPEM, serverCSRPEM string) (EnrollmentResult, error) {
	if service.Store == nil || service.Issuer == nil || service.ServerIssuer == nil || certificate == nil || len(certificate.URIs) != 1 {
		return EnrollmentResult{}, ErrCertificateFenced
	}
	collectorID, err := collectorIDFromURI(certificate.URIs[0])
	if err != nil {
		return EnrollmentResult{}, ErrCertificateFenced
	}
	serial := strings.ToLower(certificate.SerialNumber.Text(16))
	clientCSR, clientCSRHash, clientPublicKeyHash, err := parseCollectorCSR(clientCSRPEM, collectorID, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return EnrollmentResult{}, err
	}
	serverCSR, serverCSRHash, serverPublicKeyHash, err := parseCollectorCSR(serverCSRPEM, collectorID, x509.ExtKeyUsageServerAuth)
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
		clientIssued, err := service.Issuer.Issue(ctx, collectorID, clientCSR, collectorCertificateTTL)
		if err != nil {
			return err
		}
		serverIssued, err := service.ServerIssuer.Issue(ctx, collectorID, serverCSR, collectorCertificateTTL)
		if err != nil {
			return err
		}
		clientCertificate, err := validateIssuedCollectorCertificate(clientIssued, collectorID, clientPublicKeyHash, x509.ExtKeyUsageClientAuth)
		if err != nil {
			return err
		}
		serverCertificate, err := validateIssuedCollectorCertificate(serverIssued, collectorID, serverPublicKeyHash, x509.ExtKeyUsageServerAuth)
		if err != nil {
			return err
		}
		clientHash := sha256.Sum256(clientCertificate.Raw)
		clientSerial := strings.ToLower(clientCertificate.SerialNumber.Text(16))
		if _, err = q.CreateTelemetryCertificate(ctx, db.CreateTelemetryCertificateParams{
			ID: newTelemetryID(), CollectorID: collectorID, SerialNumber: clientSerial, UriSan: CollectorCertificateURI(collectorID),
			CsrHash: clientCSRHash, CertificateHash: clientHash[:], CertificateRequestName: clientIssued.CertificateRequestName,
			IssuerGeneration: clientIssued.IssuerGeneration, NotBefore: pgtype.Timestamptz{Time: clientCertificate.NotBefore.UTC(), Valid: true},
			NotAfter: pgtype.Timestamptz{Time: clientCertificate.NotAfter.UTC(), Valid: true}, CertificateUsage: "clientAuth",
		}); err != nil {
			return err
		}
		if err = q.MarkPKISubjectCertificatesOverlap(ctx, db.MarkPKISubjectCertificatesOverlapParams{SubjectKind: "collector", SubjectID: collectorID.String()}); err != nil {
			return err
		}
		if err = trustbundle.RegisterCertificateIdentity(ctx, q, clientCertificate, trustbundle.CertificateIdentity{Kind: "collector",
			SubjectID: collectorID.String(), EnterpriseID: uuid.NullUUID{UUID: collector.EnterpriseID, Valid: true},
			Usage: x509.ExtKeyUsageClientAuth, IssuerGeneration: clientIssued.IssuerGeneration}); err != nil {
			return err
		}
		serverHash := sha256.Sum256(serverCertificate.Raw)
		serverSerial := strings.ToLower(serverCertificate.SerialNumber.Text(16))
		if _, err = q.CreateTelemetryCertificate(ctx, db.CreateTelemetryCertificateParams{
			ID: newTelemetryID(), CollectorID: collectorID, SerialNumber: serverSerial, UriSan: CollectorServerCertificateURI(collectorID),
			CsrHash: serverCSRHash, CertificateHash: serverHash[:], CertificateRequestName: serverIssued.CertificateRequestName,
			IssuerGeneration: serverIssued.IssuerGeneration, NotBefore: pgtype.Timestamptz{Time: serverCertificate.NotBefore.UTC(), Valid: true},
			NotAfter: pgtype.Timestamptz{Time: serverCertificate.NotAfter.UTC(), Valid: true}, CertificateUsage: "serverAuth",
		}); err != nil {
			return err
		}
		if err = trustbundle.RegisterCertificateIdentity(ctx, q, serverCertificate, trustbundle.CertificateIdentity{Kind: "collector",
			SubjectID: collectorID.String(), EnterpriseID: uuid.NullUUID{UUID: collector.EnterpriseID, Valid: true},
			Usage: x509.ExtKeyUsageServerAuth, IssuerGeneration: serverIssued.IssuerGeneration}); err != nil {
			return err
		}
		if err = q.LimitTelemetryCertificateOverlap(ctx, db.LimitTelemetryCertificateOverlapParams{
			CollectorID: collectorID, SerialNumber: clientSerial, SerialNumber_2: serverSerial}); err != nil {
			return err
		}
		touchHostSeen(ctx, q, collector)
		bundle, bundleErr := service.enrollmentTrustBundle(ctx, clientIssued.CABundlePEM)
		if bundleErr != nil {
			return bundleErr
		}
		expiresAt := clientCertificate.NotAfter.UTC()
		if serverCertificate.NotAfter.Before(expiresAt) {
			expiresAt = serverCertificate.NotAfter.UTC()
		}
		result = EnrollmentResult{CollectorID: collectorID, ClientCertificatePEM: clientIssued.PEM,
			ServerCertificatePEM: serverIssued.PEM, TrustBundle: bundle, ExpiresAt: expiresAt}
		return nil
	})
	return result, err
}

func (service IdentityService) enrollmentTrustBundle(ctx context.Context, issuerBundle string) (trustbundle.Bundle, error) {
	if service.TrustBundles.Store != nil {
		return service.TrustBundles.Current(ctx)
	}
	material, err := trustbundle.Parse([]byte(issuerBundle), time.Now().UTC())
	if err != nil {
		return trustbundle.Bundle{}, err
	}
	return trustbundle.Bundle{Epoch: 1, State: trustbundle.StateStable, Material: material,
		CurrentCAFingerprints: material.Fingerprints, StartedAt: time.Now().UTC()}, nil
}

func parseCollectorCSR(value string, collectorID uuid.UUID, usage x509.ExtKeyUsage) (*x509.CertificateRequest, []byte, []byte, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE REQUEST" {
		return nil, nil, nil, ErrEnrollmentInvalid
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil || len(csr.URIs) != 1 || len(csr.IPAddresses) != 0 {
		return nil, nil, nil, ErrEnrollmentInvalid
	}
	switch usage {
	case x509.ExtKeyUsageClientAuth:
		if csr.URIs[0].String() != CollectorCertificateURI(collectorID) || len(csr.DNSNames) != 0 {
			return nil, nil, nil, ErrEnrollmentInvalid
		}
	case x509.ExtKeyUsageServerAuth:
		if csr.URIs[0].String() != CollectorServerCertificateURI(collectorID) || len(csr.DNSNames) != 1 || csr.DNSNames[0] != CollectorCertificateDNSName(collectorID) {
			return nil, nil, nil, ErrEnrollmentInvalid
		}
	default:
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

func validateIssuedCollectorCertificate(issued connector.Certificate, collectorID uuid.UUID, expectedPublicKeyHash []byte, usage x509.ExtKeyUsage) (*x509.Certificate, error) {
	block, rest := pem.Decode([]byte(issued.PEM))
	if block == nil || len(rest) != 0 || block.Type != "CERTIFICATE" {
		return nil, ErrEnrollmentInvalid
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(certificate.URIs) != 1 || certificate.NotAfter.Sub(certificate.NotBefore) > collectorCertificateTTL+time.Minute ||
		len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != usage {
		return nil, ErrEnrollmentInvalid
	}
	if usage == x509.ExtKeyUsageClientAuth && (certificate.URIs[0].String() != CollectorCertificateURI(collectorID) || len(certificate.DNSNames) != 0) {
		return nil, ErrEnrollmentInvalid
	}
	if usage == x509.ExtKeyUsageServerAuth && (certificate.URIs[0].String() != CollectorServerCertificateURI(collectorID) ||
		len(certificate.DNSNames) != 1 || certificate.DNSNames[0] != CollectorCertificateDNSName(collectorID)) {
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
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(issued.CABundlePEM)) {
		return nil, ErrEnrollmentInvalid
	}
	verifyOptions := x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{usage}}
	if usage == x509.ExtKeyUsageServerAuth {
		verifyOptions.DNSName = CollectorCertificateDNSName(collectorID)
	}
	if _, err = certificate.Verify(verifyOptions); err != nil {
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

// touchHostSeen 在证书签发/轮换成功后刷新宿主 Host 的 last_seen:
// self_enrolled 主机没有探活通道,这是其在线事实的持续来源。
func touchHostSeen(ctx context.Context, q *db.Queries, collector db.CollectorInstance) {
	if collector.ResourceType != "host" {
		return
	}
	_, _ = q.MarkHostSeen(ctx, db.MarkHostSeenParams{ID: collector.ResourceID, EnterpriseID: collector.EnterpriseID})
}
