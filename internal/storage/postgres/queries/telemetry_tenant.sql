-- name: UpsertEnterpriseTelemetryTables :exec
INSERT INTO enterprise_telemetry_tables (enterprise_id, schema_version, status, ready_at, last_error, updated_at)
VALUES (sqlc.arg('enterprise_id'), sqlc.arg('schema_version'), sqlc.arg('status'), sqlc.narg('ready_at'), sqlc.narg('last_error'), now())
ON CONFLICT (enterprise_id) DO UPDATE SET schema_version = EXCLUDED.schema_version, status = EXCLUDED.status, ready_at = EXCLUDED.ready_at, last_error = EXCLUDED.last_error, updated_at = now();

-- name: MarkEnterpriseTelemetryDeleting :exec
UPDATE enterprise_telemetry_tables SET status = 'deleting', updated_at = now() WHERE enterprise_id = sqlc.arg('enterprise_id');

-- name: GetEnterpriseTelemetryTables :one
SELECT enterprise_id, schema_version, status, ready_at, last_error, updated_at
FROM enterprise_telemetry_tables
WHERE enterprise_id = sqlc.arg('enterprise_id');
