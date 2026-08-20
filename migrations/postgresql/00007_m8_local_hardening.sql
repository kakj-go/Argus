-- +goose Up
ALTER TABLE sessions
  ADD COLUMN authenticated_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN step_up_expires_at timestamptz,
  ADD COLUMN amr text[] NOT NULL DEFAULT ARRAY['password']::text[];

ALTER TABLE ai_model_credentials
  ADD COLUMN provider text NOT NULL DEFAULT 'local',
  ALTER COLUMN wrap_nonce DROP NOT NULL;

ALTER TABLE idempotency_records DROP CONSTRAINT IF EXISTS idempotency_records_check;
ALTER TABLE idempotency_records
  ADD COLUMN response_provider text,
  ADD COLUMN response_key_id text,
  ADD COLUMN response_key_version integer,
  ADD CONSTRAINT idempotency_records_response_check CHECK (
    (response_ciphertext IS NULL AND response_nonce IS NULL AND response_status IS NULL AND response_provider IS NULL AND response_key_id IS NULL AND response_key_version IS NULL)
    OR
    (response_ciphertext IS NOT NULL AND response_status IS NOT NULL AND
      ((response_provider IS NULL AND response_nonce IS NOT NULL AND response_key_id IS NULL AND response_key_version IS NULL)
       OR
       (response_provider IS NOT NULL AND response_nonce IS NULL AND response_key_id IS NOT NULL AND response_key_version > 0)))
  );

CREATE TABLE mfa_credentials (
    id uuid PRIMARY KEY,
    audience text NOT NULL CHECK (audience IN ('platform', 'enterprise')),
    user_id uuid NOT NULL,
    provider text NOT NULL CHECK (provider IN ('openbao_transit', 'local_test')),
    key_id text NOT NULL,
    key_version integer NOT NULL CHECK (key_version > 0),
    encrypted_secret bytea NOT NULL,
    last_totp_counter bigint,
    enrollment_hash bytea CHECK (enrollment_hash IS NULL OR octet_length(enrollment_hash) = 32),
    enrollment_expires_at timestamptz,
    status text NOT NULL CHECK (status IN ('pending', 'active', 'disabled')),
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (audience, user_id)
);
CREATE UNIQUE INDEX mfa_credentials_enrollment_hash ON mfa_credentials (enrollment_hash) WHERE enrollment_hash IS NOT NULL;

CREATE TABLE mfa_recovery_codes (
    id uuid PRIMARY KEY,
    credential_id uuid NOT NULL REFERENCES mfa_credentials(id) ON DELETE CASCADE,
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (credential_id, code_hash)
);

CREATE TABLE mfa_challenges (
    id uuid PRIMARY KEY,
    challenge_hash bytea NOT NULL UNIQUE CHECK (octet_length(challenge_hash) = 32),
    audience text NOT NULL CHECK (audience IN ('platform', 'enterprise')),
    user_id uuid NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('login', 'step_up')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mfa_challenges_subject ON mfa_challenges (audience, user_id, expires_at DESC);

CREATE TABLE break_glass_sessions (
    id uuid PRIMARY KEY,
    enterprise_id uuid NOT NULL REFERENCES enterprises(id),
    user_id uuid NOT NULL,
    source_session_id uuid NOT NULL REFERENCES sessions(id),
    authorization_version bigint NOT NULL CHECK (authorization_version > 0),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 8 AND 2048),
    ticket_ref text NOT NULL CHECK (char_length(ticket_ref) BETWEEN 1 AND 256),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'expired')),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (user_id, enterprise_id) REFERENCES enterprise_users(id, enterprise_id)
);
CREATE INDEX break_glass_active_subject ON break_glass_sessions (enterprise_id, user_id, expires_at) WHERE status = 'active';

-- Local-hardening deployments pre-create these login roles in the PostgreSQL
-- bootstrap directory. Evaluation databases intentionally skip this block.
-- +goose StatementBegin
DO $roles$
DECLARE role_name text;
BEGIN
  FOREACH role_name IN ARRAY ARRAY['argus_server','argus_worker','argus_gateway','argus_direct_executor'] LOOP
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = role_name) THEN
      EXECUTE format('GRANT CONNECT ON DATABASE argus TO %I', role_name);
      EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', role_name);
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %I', role_name);
      EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %I', role_name);
    END IF;
  END LOOP;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'argus_telemetry_ingest') THEN
    GRANT CONNECT ON DATABASE argus TO argus_telemetry_ingest;
    GRANT USAGE ON SCHEMA public TO argus_telemetry_ingest;
    GRANT SELECT ON collector_instances, telemetry_certificates, telemetry_routes TO argus_telemetry_ingest;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'argus_telemetry_writer') THEN
    GRANT CONNECT ON DATABASE argus TO argus_telemetry_writer;
    GRANT USAGE ON SCHEMA public TO argus_telemetry_writer;
    GRANT SELECT, INSERT, UPDATE ON telemetry_retention_policies TO argus_telemetry_writer;
    GRANT SELECT, INSERT, UPDATE ON telemetry_usage_daily, telemetry_dlq_records TO argus_telemetry_writer;
  END IF;
END
$roles$;
-- +goose StatementEnd

-- Existing platform super administrators must enroll before privileged access.
UPDATE platform_users SET mfa_enabled = false WHERE status = 'active';

-- +goose Down
DROP TABLE IF EXISTS break_glass_sessions;
DROP TABLE IF EXISTS mfa_challenges;
DROP TABLE IF EXISTS mfa_recovery_codes;
DROP TABLE IF EXISTS mfa_credentials;
DELETE FROM idempotency_records WHERE response_provider IS NOT NULL;
ALTER TABLE idempotency_records DROP CONSTRAINT IF EXISTS idempotency_records_response_check;
ALTER TABLE idempotency_records DROP COLUMN IF EXISTS response_key_version, DROP COLUMN IF EXISTS response_key_id, DROP COLUMN IF EXISTS response_provider;
ALTER TABLE idempotency_records ADD CONSTRAINT idempotency_records_check CHECK (
  (response_ciphertext IS NULL AND response_nonce IS NULL AND response_status IS NULL) OR
  (response_ciphertext IS NOT NULL AND response_nonce IS NOT NULL AND response_status IS NOT NULL)
);
ALTER TABLE ai_model_credentials DROP COLUMN IF EXISTS provider;
ALTER TABLE ai_model_credentials ALTER COLUMN wrap_nonce SET NOT NULL;
ALTER TABLE sessions DROP COLUMN IF EXISTS amr, DROP COLUMN IF EXISTS step_up_expires_at, DROP COLUMN IF EXISTS authenticated_at;
