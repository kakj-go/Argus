package telemetry

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

// IngestControlStore exposes only the control facts required to authenticate
// an OTLP producer. It deliberately excludes resource and authorization writes.
type IngestControlStore interface {
	ResolveCollectorIdentity(context.Context, string) (TrustedIdentity, string, error)
	ResolveDownstreamIdentity(context.Context, TrustedIdentity, uuid.UUID, string) (TrustedIdentity, error)
}

// WriterControlStore exposes only retention, usage, and DLQ settlement facts.
type WriterControlStore interface {
	TelemetryWritable(context.Context, uuid.UUID) (bool, error)
	TelemetryTenantReady(context.Context, uuid.UUID) (bool, error)
	Retention(context.Context, uuid.UUID) (TelemetryRetention, error)
	RecordDLQ(context.Context, TelemetryDLQRecord) error
	IncrementUsage(context.Context, uuid.UUID, TelemetryUsageDelta) error
}

type TelemetryRetention struct {
	Metrics time.Duration
	Logs    time.Duration
	Traces  time.Duration
}

type TelemetryUsageDelta struct {
	Bytes, Metrics, Logs, Spans int64
}

type TelemetryDLQRecord struct {
	ID                      uuid.UUID
	Signal, Topic, DLQTopic string
	Partition, DLQPartition int32
	SourceOffset, DLQOffset int64
	RecordHash              [sha256.Size]byte
	ErrorCode               string
}

type PostgresIngestControl struct{ Queries *db.Queries }

func (store PostgresIngestControl) ResolveCollectorIdentity(ctx context.Context, serial string) (TrustedIdentity, string, error) {
	row, err := store.Queries.GetTelemetryCollectorIdentityBySerial(ctx, serial)
	if err != nil {
		return TrustedIdentity{}, "", err
	}
	pkiIdentity, err := store.Queries.GetActivePKICertificateIdentity(ctx, serial)
	if err != nil || pkiIdentity.SubjectKind != "collector" || pkiIdentity.SubjectID != row.ID.String() ||
		!pkiIdentity.EnterpriseID.Valid || pkiIdentity.EnterpriseID.UUID != row.EnterpriseID || pkiIdentity.ExtendedKeyUsage != "clientAuth" ||
		pkiIdentity.UriSan != row.UriSan {
		return TrustedIdentity{}, "", errors.New("telemetry PKI identity is fenced")
	}
	return TrustedIdentity{EnterpriseID: row.EnterpriseID, ResourceID: row.ResourceID, CollectorID: row.ID,
		ResourceType: row.ResourceType, Role: row.Role, CertificateSerial: row.SerialNumber,
		AuthorizationVersion: row.AuthorizationVersion}, row.UriSan, nil
}

func (store PostgresIngestControl) ResolveDownstreamIdentity(ctx context.Context, gateway TrustedIdentity, collectorID uuid.UUID, serial string) (TrustedIdentity, error) {
	_, certificateErr := store.Queries.GetValidTelemetryCertificateBySerial(ctx, db.GetValidTelemetryCertificateBySerialParams{
		CollectorID: collectorID, SerialNumber: serial,
	})
	pkiIdentity, pkiErr := store.Queries.GetActivePKICertificateIdentity(ctx, serial)
	downstream, downstreamErr := store.Queries.GetCollectorInstanceByID(ctx, collectorID)
	if downstreamErr != nil {
		return TrustedIdentity{}, downstreamErr
	}
	route, routeErr := store.Queries.GetTelemetryRouteByCollector(ctx, db.GetTelemetryRouteByCollectorParams{
		CollectorID: downstream.ID, EnterpriseID: downstream.EnterpriseID,
	})
	if routeErr != nil {
		return TrustedIdentity{}, routeErr
	}
	pkiValid := pkiErr == nil && pkiIdentity.SubjectKind == "collector" && pkiIdentity.SubjectID == collectorID.String() &&
		pkiIdentity.EnterpriseID.Valid && pkiIdentity.EnterpriseID.UUID == downstream.EnterpriseID && pkiIdentity.ExtendedKeyUsage == "clientAuth"
	return validateDownstreamFacts(gateway, downstreamReference{collectorID: collectorID, serial: serial, found: true},
		certificateErr == nil && pkiValid, downstream, route)
}

type PostgresWriterControl struct{ Queries *db.Queries }

func (store PostgresWriterControl) TelemetryWritable(ctx context.Context, enterpriseID uuid.UUID) (bool, error) {
	enterprise, err := store.Queries.GetEnterprise(ctx, enterpriseID)
	if err != nil {
		return false, err
	}
	return enterprise.Status == "active", nil
}

func (store PostgresWriterControl) TelemetryTenantReady(ctx context.Context, enterpriseID uuid.UUID) (bool, error) {
	row, err := store.Queries.GetEnterpriseTelemetryTables(ctx, enterpriseID)
	if err != nil {
		return false, err
	}
	return row.SchemaVersion == int32(TelemetrySchemaVersion) && row.Status == "ready", nil
}

func (store PostgresWriterControl) Retention(ctx context.Context, enterpriseID uuid.UUID) (TelemetryRetention, error) {
	policy, err := store.Queries.EnsureTelemetryRetentionPolicy(ctx, enterpriseID)
	if err != nil {
		return TelemetryRetention{}, err
	}
	return TelemetryRetention{Metrics: time.Duration(policy.MetricsDays) * 24 * time.Hour,
		Logs: time.Duration(policy.LogsDays) * 24 * time.Hour, Traces: time.Duration(policy.TracesDays) * 24 * time.Hour}, nil
}

func (store PostgresWriterControl) RecordDLQ(ctx context.Context, record TelemetryDLQRecord) error {
	_, err := store.Queries.RecordTelemetryDLQ(ctx, db.RecordTelemetryDLQParams{ID: record.ID, Signal: record.Signal,
		Topic: record.Topic, Partition: record.Partition, SourceOffset: record.SourceOffset, DlqTopic: record.DLQTopic,
		DlqPartition: record.DLQPartition, DlqOffset: record.DLQOffset, RecordHash: record.RecordHash[:], ErrorCode: record.ErrorCode})
	return err
}

func (store PostgresWriterControl) IncrementUsage(ctx context.Context, enterpriseID uuid.UUID, delta TelemetryUsageDelta) error {
	return store.Queries.IncrementTelemetryUsage(ctx, db.IncrementTelemetryUsageParams{EnterpriseID: enterpriseID,
		IngestedBytes: delta.Bytes, MetricPoints: delta.Metrics, LogRecords: delta.Logs, Spans: delta.Spans,
		EstimatedStorageBytes: delta.Bytes})
}
