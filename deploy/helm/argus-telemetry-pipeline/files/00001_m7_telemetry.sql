-- Argus Telemetry Schema v3.
-- Shared telemetry fact tables are intentionally removed. Each enterprise gets
-- six physical tables created by TenantSchemaManager from a trusted UUID.
CREATE DATABASE IF NOT EXISTS argus_telemetry;
DROP TABLE IF EXISTS argus_telemetry.metric_series;
DROP TABLE IF EXISTS argus_telemetry.metric_samples;
DROP TABLE IF EXISTS argus_telemetry.logs;
DROP TABLE IF EXISTS argus_telemetry.traces;
DROP TABLE IF EXISTS argus_telemetry.trace_summary;
DROP TABLE IF EXISTS argus_telemetry.trace_span_edges;
DROP TABLE IF EXISTS argus_telemetry.metric_series_local;
DROP TABLE IF EXISTS argus_telemetry.metric_samples_local;
DROP TABLE IF EXISTS argus_telemetry.logs_local;
DROP TABLE IF EXISTS argus_telemetry.traces_local;
DROP TABLE IF EXISTS argus_telemetry.trace_summary_local;
DROP TABLE IF EXISTS argus_telemetry.trace_span_edges_local;
CREATE TABLE IF NOT EXISTS argus_telemetry.schema_versions
(
    version UInt32,
    applied_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplacingMergeTree
ORDER BY version;
INSERT INTO argus_telemetry.schema_versions (version)
SELECT 3 WHERE NOT EXISTS (SELECT 1 FROM argus_telemetry.schema_versions WHERE version = 3);
