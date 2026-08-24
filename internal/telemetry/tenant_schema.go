package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

// TenantTables is the only table-name surface exposed to query and writer code.
// Names are derived from a trusted UUID and never from user input.
type TenantTables struct {
	MetricSeries   string
	MetricSamples  string
	Logs           string
	Traces         string
	TraceSummary   string
	TraceSpanEdges string
}

type TenantTableRouter struct{}

func (TenantTableRouter) Tables(enterpriseID uuid.UUID) (TenantTables, error) {
	if enterpriseID == uuid.Nil {
		return TenantTables{}, fmt.Errorf("enterprise id is required")
	}
	suffix := strings.ReplaceAll(strings.ToLower(enterpriseID.String()), "-", "")
	return TenantTables{
		MetricSeries:   "metric_series_" + suffix,
		MetricSamples:  "metric_samples_" + suffix,
		Logs:           "logs_" + suffix,
		Traces:         "traces_" + suffix,
		TraceSummary:   "trace_summary_" + suffix,
		TraceSpanEdges: "trace_span_edges_" + suffix,
	}, nil
}

func (r TenantTableRouter) Table(name string, enterpriseID uuid.UUID) (string, error) {
	tables, err := r.Tables(enterpriseID)
	if err != nil {
		return "", err
	}
	switch name {
	case "metric_series":
		return tables.MetricSeries, nil
	case "metric_samples":
		return tables.MetricSamples, nil
	case "logs":
		return tables.Logs, nil
	case "traces":
		return tables.Traces, nil
	case "trace_summary":
		return tables.TraceSummary, nil
	case "trace_span_edges":
		return tables.TraceSpanEdges, nil
	default:
		return "", fmt.Errorf("unknown telemetry table %q", name)
	}
}

type TenantSchemaManager interface {
	EnsureTenant(context.Context, uuid.UUID) error
	VerifyTenant(context.Context, uuid.UUID) error
	DropTenant(context.Context, uuid.UUID) error
}

type TenantSchemaStateStore interface {
	GetEnterpriseTelemetryTables(context.Context, uuid.UUID) (db.EnterpriseTelemetryTable, error)
	UpsertEnterpriseTelemetryTables(context.Context, db.UpsertEnterpriseTelemetryTablesParams) error
}

type TenantSchemaLocker interface {
	WithTenantSchemaLock(context.Context, uuid.UUID, func() error) error
}

type PostgresTenantSchemaLocker struct{ Pool *pgxpool.Pool }

func (locker PostgresTenantSchemaLocker) WithTenantSchemaLock(ctx context.Context, enterpriseID uuid.UUID, run func() error) error {
	if locker.Pool == nil || enterpriseID == uuid.Nil || run == nil {
		return ErrQueryBackend
	}
	connection, err := locker.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	key := "argus:telemetry-tenant-schema:" + enterpriseID.String()
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", key); err != nil {
		return err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = connection.Exec(unlockCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", key)
	}()
	return run()
}

type ClickHouseTenantSchemaManager struct {
	Conn   driver.Conn
	Router TenantTableRouter
}

// TenantSchemaLifecycle is the trusted orchestration boundary around tenant
// DDL and its PostgreSQL readiness record. Query owns this lifecycle so API
// and writer processes never need ClickHouse DDL credentials.
type TenantSchemaLifecycle struct {
	Manager TenantSchemaManager
	Queries TenantSchemaStateStore
	Locker  TenantSchemaLocker
}

func (lifecycle TenantSchemaLifecycle) EnsureTenantSchema(ctx context.Context, enterpriseID uuid.UUID) error {
	if lifecycle.Manager == nil || lifecycle.Queries == nil || lifecycle.Locker == nil || enterpriseID == uuid.Nil {
		return ErrQueryBackend
	}
	return lifecycle.Locker.WithTenantSchemaLock(ctx, enterpriseID, func() error {
		current, err := lifecycle.Queries.GetEnterpriseTelemetryTables(ctx, enterpriseID)
		if err == nil && current.SchemaVersion == int32(TelemetrySchemaVersion) && current.Status == "ready" {
			if verifyErr := lifecycle.Manager.VerifyTenant(ctx, enterpriseID); verifyErr == nil {
				return nil
			}
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := lifecycle.record(ctx, enterpriseID, "pending", pgtype.Timestamptz{}, pgtype.Text{}); err != nil {
			return err
		}
		if err := lifecycle.Manager.EnsureTenant(ctx, enterpriseID); err != nil {
			lifecycle.recordError(ctx, enterpriseID, err)
			return err
		}
		if err := lifecycle.Manager.VerifyTenant(ctx, enterpriseID); err != nil {
			_ = lifecycle.Manager.DropTenant(ctx, enterpriseID)
			lifecycle.recordError(ctx, enterpriseID, err)
			return err
		}
		if err := lifecycle.record(ctx, enterpriseID, "ready", pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}, pgtype.Text{}); err != nil {
			_ = lifecycle.Manager.DropTenant(ctx, enterpriseID)
			lifecycle.recordError(ctx, enterpriseID, err)
			return err
		}
		return nil
	})
}

func (lifecycle TenantSchemaLifecycle) DropTenantSchema(ctx context.Context, enterpriseID uuid.UUID) error {
	if lifecycle.Manager == nil || lifecycle.Queries == nil || lifecycle.Locker == nil || enterpriseID == uuid.Nil {
		return ErrQueryBackend
	}
	return lifecycle.Locker.WithTenantSchemaLock(ctx, enterpriseID, func() error {
		current, err := lifecycle.Queries.GetEnterpriseTelemetryTables(ctx, enterpriseID)
		if err == nil && current.SchemaVersion == int32(TelemetrySchemaVersion) && current.Status == "deleting" {
			return nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := lifecycle.record(ctx, enterpriseID, "deleting", pgtype.Timestamptz{}, pgtype.Text{}); err != nil {
			return err
		}
		if err := lifecycle.Manager.DropTenant(ctx, enterpriseID); err != nil {
			lifecycle.recordError(ctx, enterpriseID, err)
			return err
		}
		return nil
	})
}

func (lifecycle TenantSchemaLifecycle) record(ctx context.Context, enterpriseID uuid.UUID, status string, readyAt pgtype.Timestamptz, lastError pgtype.Text) error {
	return lifecycle.Queries.UpsertEnterpriseTelemetryTables(ctx, db.UpsertEnterpriseTelemetryTablesParams{
		EnterpriseID:  enterpriseID,
		SchemaVersion: int32(TelemetrySchemaVersion),
		Status:        status,
		ReadyAt:       readyAt,
		LastError:     lastError,
	})
}

func (lifecycle TenantSchemaLifecycle) recordError(ctx context.Context, enterpriseID uuid.UUID, cause error) {
	_ = lifecycle.record(ctx, enterpriseID, "error", pgtype.Timestamptz{}, pgtype.Text{String: cause.Error(), Valid: true})
}

func (m ClickHouseTenantSchemaManager) EnsureTenant(ctx context.Context, enterpriseID uuid.UUID) error {
	if m.Conn == nil {
		return ErrQueryBackend
	}
	tables, err := m.Router.Tables(enterpriseID)
	if err != nil {
		return err
	}
	for _, ddl := range tenantTableDDL(tables) {
		if err := m.Conn.Exec(ctx, ddl); err != nil {
			_ = m.DropTenant(ctx, enterpriseID)
			return err
		}
	}
	return nil
}

func (m ClickHouseTenantSchemaManager) VerifyTenant(ctx context.Context, enterpriseID uuid.UUID) error {
	if m.Conn == nil {
		return ErrQueryBackend
	}
	tables, err := m.Router.Tables(enterpriseID)
	if err != nil {
		return err
	}
	for table, expectation := range tenantTableExpectations(tables) {
		var engine, sortingKey, createQuery string
		if err := m.Conn.QueryRow(ctx, "SELECT engine, sorting_key, create_table_query FROM system.tables WHERE database = currentDatabase() AND name = ?", table).Scan(&engine, &sortingKey, &createQuery); err != nil {
			return err
		}
		if engine != "ReplacingMergeTree" {
			return fmt.Errorf("telemetry tenant table %s engine is %s, want ReplacingMergeTree", table, engine)
		}
		if normalizeSchemaExpression(sortingKey) != normalizeSchemaExpression(expectation.SortingKey) {
			return fmt.Errorf("telemetry tenant table %s sorting key is %q, want %q", table, sortingKey, expectation.SortingKey)
		}
		if !strings.Contains(normalizeSchemaExpression(createQuery), "ttlexpires_at") {
			return fmt.Errorf("telemetry tenant table %s TTL is missing or invalid", table)
		}
		rows, err := m.Conn.Query(ctx, "SELECT name, type FROM system.columns WHERE database = currentDatabase() AND table = ?", table)
		if err != nil {
			return err
		}
		actual := make(map[string]string, len(expectation.Columns))
		for rows.Next() {
			var name, columnType string
			if err := rows.Scan(&name, &columnType); err != nil {
				rows.Close()
				return err
			}
			actual[name] = normalizeColumnType(columnType)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for name, columnType := range expectation.Columns {
			if actual[name] != normalizeColumnType(columnType) {
				return fmt.Errorf("telemetry tenant table %s column %s is %q, want %q", table, name, actual[name], columnType)
			}
		}
		if len(actual) != len(expectation.Columns) {
			return fmt.Errorf("telemetry tenant table %s has %d columns, want %d", table, len(actual), len(expectation.Columns))
		}
	}
	return nil
}

type tenantTableExpectation struct {
	SortingKey string
	Columns    map[string]string
}

func tenantTableExpectations(t TenantTables) map[string]tenantTableExpectation {
	return map[string]tenantTableExpectation{
		t.MetricSeries: {
			SortingKey: "metric_name, labels_hash, resource_id, series_id",
			Columns: columns(
				"resource_id", "UUID", "series_id", "UUID", "metric_name", "LowCardinality(String)", "labels", "Map(String,String)", "labels_hash", "UInt64",
				"resource_attributes", "Map(String,String)", "scope_name", "LowCardinality(String)", "scope_version", "LowCardinality(String)", "scope_attributes", "Map(String,String)",
				"metric_type", "LowCardinality(String)", "temporality", "LowCardinality(String)", "is_monotonic", "Bool", "unit", "LowCardinality(String)", "description", "String",
				"kafka_topic", "LowCardinality(String)", "kafka_partition", "Int32", "kafka_offset", "Int64", "record_sequence", "UInt32", "expires_at", "DateTime64(3,'UTC')",
			),
		},
		t.MetricSamples: {
			SortingKey: "series_id, timestamp, ingest_key",
			Columns: columns(
				"resource_id", "UUID", "series_id", "UUID", "metric_name", "LowCardinality(String)", "timestamp", "DateTime64(9,'UTC')", "start_timestamp", "DateTime64(9,'UTC')",
				"value", "Nullable(Float64)", "float_value", "Nullable(Float64)", "stale_marker", "Bool", "count", "Nullable(UInt64)", "sum", "Nullable(Float64)", "min", "Nullable(Float64)", "max", "Nullable(Float64)",
				"bucket_counts", "Array(UInt64)", "explicit_bounds", "Array(Float64)", "quantile_values", "Array(Tuple(Float64,Float64))", "exponential_scale", "Nullable(Int32)", "exponential_zero_count", "Nullable(UInt64)", "exponential_zero_threshold", "Nullable(Float64)",
				"exponential_positive_offset", "Nullable(Int32)", "exponential_positive_bucket_counts", "Array(UInt64)", "exponential_negative_offset", "Nullable(Int32)", "exponential_negative_bucket_counts", "Array(UInt64)",
				"sample_type", "LowCardinality(String)", "ingest_key", "String", "kafka_topic", "LowCardinality(String)", "kafka_partition", "Int32", "kafka_offset", "Int64", "record_sequence", "UInt32", "expires_at", "DateTime64(3,'UTC')",
			),
		},
		t.Logs: {
			SortingKey: "resource_id, service_name, timestamp, event_id",
			Columns: columns(
				"resource_id", "UUID", "collector_id", "UUID", "timestamp", "DateTime64(9,'UTC')", "observed_timestamp", "DateTime64(9,'UTC')", "severity_text", "LowCardinality(String)", "severity_number", "UInt8",
				"service_name", "LowCardinality(String)", "scope_name", "LowCardinality(String)", "scope_version", "LowCardinality(String)", "scope_attributes", "Map(String,String)", "stream_labels", "Map(String,String)", "structured_metadata", "Map(String,String)",
				"body", "String", "body_size", "UInt32", "trace_id", "String", "span_id", "String", "event_id", "String", "ingest_key", "String", "kafka_topic", "LowCardinality(String)", "kafka_partition", "Int32", "kafka_offset", "Int64", "record_sequence", "UInt32", "expires_at", "DateTime64(3,'UTC')",
			),
		},
		t.Traces: {
			SortingKey: "resource_id, trace_id, start_time, span_id",
			Columns: columns(
				"resource_id", "UUID", "collector_id", "UUID", "trace_id", "String", "span_id", "String", "parent_span_id", "String", "root_span_id", "String", "service_name", "LowCardinality(String)", "operation", "LowCardinality(String)",
				"span_kind", "UInt8", "status", "LowCardinality(String)", "status_code", "UInt8", "status_message", "String", "trace_state", "String", "start_time", "DateTime64(9,'UTC')", "end_time", "DateTime64(9,'UTC')", "duration_ns", "UInt64",
				"resource_attributes", "Map(String,String)", "scope_name", "LowCardinality(String)", "scope_version", "LowCardinality(String)", "scope_attributes", "Map(String,String)", "attributes", "Map(String,String)", "events", "String", "links", "String",
				"ingest_key", "String", "kafka_topic", "LowCardinality(String)", "kafka_partition", "Int32", "kafka_offset", "Int64", "record_sequence", "UInt32", "expires_at", "DateTime64(3,'UTC')",
			),
		},
		t.TraceSummary: {
			SortingKey: "resource_id, trace_id",
			Columns: columns(
				"resource_id", "UUID", "trace_id", "String", "root_span_id", "String", "root_service", "LowCardinality(String)", "root_operation", "LowCardinality(String)", "start_time", "DateTime64(9,'UTC')", "duration_ns", "UInt64", "span_count", "UInt32", "error_count", "UInt32", "status", "LowCardinality(String)", "expires_at", "DateTime64(3,'UTC')",
			),
		},
		t.TraceSpanEdges: {
			SortingKey: "resource_id, trace_id, parent_span_id, child_span_id",
			Columns: columns(
				"resource_id", "UUID", "trace_id", "String", "parent_span_id", "String", "child_span_id", "String", "parent_service", "LowCardinality(String)", "child_service", "LowCardinality(String)", "depth", "UInt16", "expires_at", "DateTime64(3,'UTC')",
			),
		},
	}
}

func columns(values ...string) map[string]string {
	result := make(map[string]string, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		result[values[index]] = values[index+1]
	}
	return result
}

func normalizeSchemaExpression(value string) string {
	normalized := strings.NewReplacer(" ", "", "\n", "", "\t", "", "`", "", "(", "", ")", "").Replace(strings.ToLower(value))
	return strings.TrimPrefix(normalized, "tuple")
}

func normalizeColumnType(value string) string {
	return strings.NewReplacer(" ", "", "\n", "", "\t", "").Replace(value)
}

func (m ClickHouseTenantSchemaManager) DropTenant(ctx context.Context, enterpriseID uuid.UUID) error {
	if m.Conn == nil {
		return ErrQueryBackend
	}
	tables, err := m.Router.Tables(enterpriseID)
	if err != nil {
		return err
	}
	var first error
	for _, table := range []string{tables.MetricSeries, tables.MetricSamples, tables.Logs, tables.Traces, tables.TraceSummary, tables.TraceSpanEdges} {
		if err := m.Conn.Exec(ctx, "DROP TABLE IF EXISTS `"+table+"`"); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func tenantTableDDL(t TenantTables) []string {
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
resource_id UUID, series_id UUID, metric_name LowCardinality(String), labels Map(String,String), labels_hash UInt64,
resource_attributes Map(String,String), scope_name LowCardinality(String), scope_version LowCardinality(String), scope_attributes Map(String,String),
metric_type LowCardinality(String), temporality LowCardinality(String), is_monotonic Bool, unit LowCardinality(String), description String,
kafka_topic LowCardinality(String), kafka_partition Int32, kafka_offset Int64, record_sequence UInt32, expires_at DateTime64(3,'UTC')
) ENGINE = ReplacingMergeTree ORDER BY (metric_name, labels_hash, resource_id, series_id) TTL expires_at DELETE`, t.MetricSeries),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
resource_id UUID, series_id UUID, metric_name LowCardinality(String), timestamp DateTime64(9,'UTC'), start_timestamp DateTime64(9,'UTC'),
value Nullable(Float64), float_value Nullable(Float64), stale_marker Bool DEFAULT false, count Nullable(UInt64), sum Nullable(Float64), min Nullable(Float64), max Nullable(Float64),
bucket_counts Array(UInt64), explicit_bounds Array(Float64), quantile_values Array(Tuple(Float64,Float64)), exponential_scale Nullable(Int32), exponential_zero_count Nullable(UInt64), exponential_zero_threshold Nullable(Float64),
exponential_positive_offset Nullable(Int32), exponential_positive_bucket_counts Array(UInt64), exponential_negative_offset Nullable(Int32), exponential_negative_bucket_counts Array(UInt64),
sample_type LowCardinality(String), ingest_key String, kafka_topic LowCardinality(String), kafka_partition Int32, kafka_offset Int64, record_sequence UInt32, expires_at DateTime64(3,'UTC')
) ENGINE = ReplacingMergeTree ORDER BY (series_id, timestamp, ingest_key) TTL expires_at DELETE`, t.MetricSamples),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
resource_id UUID, collector_id UUID, timestamp DateTime64(9,'UTC'), observed_timestamp DateTime64(9,'UTC'), severity_text LowCardinality(String), severity_number UInt8,
service_name LowCardinality(String), scope_name LowCardinality(String), scope_version LowCardinality(String), scope_attributes Map(String,String), stream_labels Map(String,String), structured_metadata Map(String,String),
body String, body_size UInt32, trace_id String, span_id String, event_id String, ingest_key String, kafka_topic LowCardinality(String), kafka_partition Int32, kafka_offset Int64, record_sequence UInt32, expires_at DateTime64(3,'UTC')
) ENGINE = ReplacingMergeTree ORDER BY (resource_id, service_name, timestamp, event_id) TTL expires_at DELETE`, t.Logs),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
resource_id UUID, collector_id UUID, trace_id String, span_id String, parent_span_id String, root_span_id String, service_name LowCardinality(String), operation LowCardinality(String),
span_kind UInt8, status LowCardinality(String), status_code UInt8, status_message String, trace_state String, start_time DateTime64(9,'UTC'), end_time DateTime64(9,'UTC'), duration_ns UInt64,
resource_attributes Map(String,String), scope_name LowCardinality(String), scope_version LowCardinality(String), scope_attributes Map(String,String), attributes Map(String,String), events String, links String,
ingest_key String, kafka_topic LowCardinality(String), kafka_partition Int32, kafka_offset Int64, record_sequence UInt32, expires_at DateTime64(3,'UTC')
) ENGINE = ReplacingMergeTree ORDER BY (resource_id, trace_id, start_time, span_id) TTL expires_at DELETE`, t.Traces),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
resource_id UUID, trace_id String, root_span_id String, root_service LowCardinality(String), root_operation LowCardinality(String), start_time DateTime64(9,'UTC'), duration_ns UInt64, span_count UInt32, error_count UInt32, status LowCardinality(String), expires_at DateTime64(3,'UTC')
) ENGINE = ReplacingMergeTree ORDER BY (resource_id, trace_id) TTL expires_at DELETE`, t.TraceSummary),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
resource_id UUID, trace_id String, parent_span_id String, child_span_id String, parent_service LowCardinality(String), child_service LowCardinality(String), depth UInt16, expires_at DateTime64(3,'UTC')
		) ENGINE = ReplacingMergeTree ORDER BY (resource_id, trace_id, parent_span_id, child_span_id) TTL expires_at DELETE`, t.TraceSpanEdges),
	}
}
