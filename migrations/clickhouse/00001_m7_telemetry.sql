-- M7 ClickHouse schema. Run with the migration account before Writer or Query starts.
CREATE DATABASE IF NOT EXISTS argus_telemetry;

CREATE TABLE IF NOT EXISTS argus_telemetry.schema_versions
(
    version UInt32,
    applied_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/argus_telemetry_schema_versions', '{replica}')
ORDER BY version;

CREATE TABLE IF NOT EXISTS argus_telemetry.metrics_local
(
    enterprise_id UUID,
    resource_id UUID,
    collector_id UUID,
    metric_name LowCardinality(String),
    unit LowCardinality(String),
    timestamp DateTime64(9, 'UTC'),
    value Float64,
    attributes Map(String, String),
    ingest_key FixedString(64),
    kafka_topic LowCardinality(String),
    kafka_partition Int32,
    kafka_offset Int64,
    record_sequence UInt32,
    expires_at DateTime64(3, 'UTC')
)
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{shard}/argus_metrics', '{replica}', kafka_offset)
PARTITION BY toYYYYMM(timestamp)
ORDER BY (enterprise_id, resource_id, metric_name, timestamp, ingest_key)
TTL expires_at DELETE;

-- ReplacingMergeTree projections must be rebuilt when a replacing merge drops
-- obsolete rows. Set this explicitly before adding the projection so a retry
-- also repairs tables created by an earlier, partially failed migration.
ALTER TABLE argus_telemetry.metrics_local MODIFY SETTING deduplicate_merge_projection_mode = 'rebuild';

ALTER TABLE argus_telemetry.metrics_local ADD PROJECTION IF NOT EXISTS metrics_5m_projection
(
    SELECT enterprise_id, resource_id, metric_name,
        toStartOfInterval(timestamp, INTERVAL 5 MINUTE) AS bucket,
        avg(value) AS avg_value, min(value) AS min_value, max(value) AS max_value, sum(value) AS sum_value, count() AS point_count
    GROUP BY enterprise_id, resource_id, metric_name, bucket
);

CREATE TABLE IF NOT EXISTS argus_telemetry.logs_local
(
    enterprise_id UUID,
    resource_id UUID,
    collector_id UUID,
    timestamp DateTime64(9, 'UTC'),
    severity LowCardinality(String),
    service_name LowCardinality(String),
    body String,
    attributes Map(String, String),
    trace_id FixedString(32),
    span_id FixedString(16),
    ingest_key FixedString(64),
    kafka_topic LowCardinality(String),
    kafka_partition Int32,
    kafka_offset Int64,
    record_sequence UInt32,
    expires_at DateTime64(3, 'UTC')
)
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{shard}/argus_logs', '{replica}', kafka_offset)
PARTITION BY toYYYYMM(timestamp)
ORDER BY (enterprise_id, resource_id, service_name, timestamp, ingest_key)
TTL expires_at DELETE;

ALTER TABLE argus_telemetry.logs_local ADD INDEX IF NOT EXISTS logs_body_token body TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 4;
ALTER TABLE argus_telemetry.logs_local ADD INDEX IF NOT EXISTS logs_trace_bloom trace_id TYPE bloom_filter(0.01) GRANULARITY 4;

CREATE TABLE IF NOT EXISTS argus_telemetry.traces_local
(
    enterprise_id UUID,
    resource_id UUID,
    collector_id UUID,
    trace_id FixedString(32),
    span_id FixedString(16),
    parent_span_id FixedString(16),
    service_name LowCardinality(String),
    operation String,
    status LowCardinality(String),
    start_time DateTime64(9, 'UTC'),
    duration_ns UInt64,
    attributes Map(String, String),
    ingest_key FixedString(64),
    kafka_topic LowCardinality(String),
    kafka_partition Int32,
    kafka_offset Int64,
    record_sequence UInt32,
    expires_at DateTime64(3, 'UTC')
)
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{shard}/argus_traces', '{replica}', kafka_offset)
PARTITION BY toYYYYMM(start_time)
ORDER BY (enterprise_id, resource_id, trace_id, start_time, ingest_key)
TTL expires_at DELETE;

ALTER TABLE argus_telemetry.traces_local ADD INDEX IF NOT EXISTS traces_operation_token operation TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 4;
ALTER TABLE argus_telemetry.traces_local ADD INDEX IF NOT EXISTS traces_status_bloom status TYPE bloom_filter(0.01) GRANULARITY 4;

CREATE TABLE IF NOT EXISTS argus_telemetry.metrics AS argus_telemetry.metrics_local
ENGINE = Distributed('main', argus_telemetry, metrics_local, cityHash64(enterprise_id, resource_id));
CREATE TABLE IF NOT EXISTS argus_telemetry.logs AS argus_telemetry.logs_local
ENGINE = Distributed('main', argus_telemetry, logs_local, cityHash64(enterprise_id, resource_id));
CREATE TABLE IF NOT EXISTS argus_telemetry.traces AS argus_telemetry.traces_local
ENGINE = Distributed('main', argus_telemetry, traces_local, cityHash64(enterprise_id, resource_id));

INSERT INTO argus_telemetry.schema_versions (version)
SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM argus_telemetry.schema_versions WHERE version = 1);
