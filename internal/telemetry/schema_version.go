package telemetry

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const TelemetrySchemaVersion uint32 = 3

func RequireTelemetrySchemaVersion(ctx context.Context, conn driver.Conn) error {
	if conn == nil {
		return ErrUnavailable
	}
	var version uint32
	if err := conn.QueryRow(ctx, `SELECT max(version) FROM argus_telemetry.schema_versions`).Scan(&version); err != nil {
		return fmt.Errorf("telemetry schema version lookup: %w", err)
	}
	if version != TelemetrySchemaVersion {
		return fmt.Errorf("telemetry schema version %d is required, found %d: %w", TelemetrySchemaVersion, version, ErrUnavailable)
	}
	return nil
}
