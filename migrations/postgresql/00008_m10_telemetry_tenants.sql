-- +goose Up
CREATE TABLE enterprise_telemetry_tables (
    enterprise_id UUID PRIMARY KEY REFERENCES enterprises(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'ready', 'deleting', 'error')),
    ready_at TIMESTAMPTZ,
    last_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementBegin
DO $roles$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'argus_telemetry_query') THEN
    GRANT CONNECT ON DATABASE argus TO argus_telemetry_query;
    GRANT USAGE ON SCHEMA public TO argus_telemetry_query;
    GRANT SELECT ON enterprises TO argus_telemetry_query;
    GRANT SELECT, INSERT, UPDATE ON enterprise_telemetry_tables TO argus_telemetry_query;
    GRANT SELECT, INSERT, UPDATE ON audit_chain_heads TO argus_telemetry_query;
    GRANT INSERT ON audit_events TO argus_telemetry_query;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'argus_telemetry_writer') THEN
    GRANT CONNECT ON DATABASE argus TO argus_telemetry_writer;
    GRANT USAGE ON SCHEMA public TO argus_telemetry_writer;
    GRANT SELECT ON enterprise_telemetry_tables TO argus_telemetry_writer;
  END IF;
END
$roles$;
-- +goose StatementEnd
