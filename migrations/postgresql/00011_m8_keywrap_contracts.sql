-- +goose Up
ALTER TABLE secret_versions
  ALTER COLUMN wrap_nonce DROP NOT NULL,
  DROP CONSTRAINT IF EXISTS secret_versions_envelope_check,
  ADD CONSTRAINT secret_versions_envelope_check CHECK (
    octet_length(wrapped_dek) > 0
    AND octet_length(nonce) = 12
    AND octet_length(ciphertext) > 0
    AND (
      (provider = 'local' AND wrap_nonce IS NOT NULL AND octet_length(wrap_nonce) = 12)
      OR (provider = 'openbao_transit' AND wrap_nonce IS NULL)
    )
  );

ALTER TABLE ai_model_credentials
  DROP CONSTRAINT IF EXISTS ai_model_credentials_provider_check,
  DROP CONSTRAINT IF EXISTS ai_model_credentials_envelope_check,
  ADD CONSTRAINT ai_model_credentials_provider_check
    CHECK (provider IN ('local', 'openbao_transit')),
  ADD CONSTRAINT ai_model_credentials_envelope_check CHECK (
    octet_length(wrapped_dek) > 0
    AND octet_length(nonce) = 12
    AND octet_length(ciphertext) > 0
    AND (
      (provider = 'local' AND wrap_nonce IS NOT NULL AND octet_length(wrap_nonce) = 12)
      OR (provider = 'openbao_transit' AND wrap_nonce IS NULL)
    )
  );

ALTER TABLE idempotency_records
  DROP CONSTRAINT IF EXISTS idempotency_records_provider_check,
  ADD CONSTRAINT idempotency_records_provider_check
    CHECK (response_provider IS NULL OR response_provider IN ('local_test', 'openbao_transit'));

ALTER TABLE sandbox_backends
  DROP CONSTRAINT IF EXISTS sandbox_backends_check,
  DROP CONSTRAINT IF EXISTS sandbox_backends_credential_envelope_check,
  ADD CONSTRAINT sandbox_backends_credential_envelope_check CHECK (
    (
      credential_provider IS NULL
      AND credential_key_id IS NULL
      AND credential_key_version IS NULL
      AND credential_wrapped_dek IS NULL
      AND credential_wrap_nonce IS NULL
      AND credential_nonce IS NULL
      AND credential_ciphertext IS NULL
      AND credential_value_hash IS NULL
    )
    OR
    (
      credential_provider IN ('local', 'openbao_transit')
      AND credential_key_id IS NOT NULL
      AND credential_key_version > 0
      AND octet_length(credential_wrapped_dek) > 0
      AND octet_length(credential_nonce) = 12
      AND octet_length(credential_ciphertext) > 0
      AND octet_length(credential_value_hash) = 32
      AND (
        (credential_provider = 'local' AND credential_wrap_nonce IS NOT NULL AND octet_length(credential_wrap_nonce) = 12)
        OR (credential_provider = 'openbao_transit' AND credential_wrap_nonce IS NULL)
      )
    )
  );

ALTER TABLE remote_access_recordings
  DROP CONSTRAINT IF EXISTS remote_access_recordings_key_provider_check,
  ADD CONSTRAINT remote_access_recordings_key_provider_check
    CHECK (key_provider IN ('local', 'openbao_transit'));

-- +goose Down
ALTER TABLE remote_access_recordings
  DROP CONSTRAINT IF EXISTS remote_access_recordings_key_provider_check;

ALTER TABLE sandbox_backends
  DROP CONSTRAINT IF EXISTS sandbox_backends_credential_envelope_check,
  ADD CONSTRAINT sandbox_backends_check
    CHECK ((credential_ciphertext IS NULL) = (credential_key_version IS NULL));

ALTER TABLE idempotency_records
  DROP CONSTRAINT IF EXISTS idempotency_records_provider_check;

ALTER TABLE ai_model_credentials
  DROP CONSTRAINT IF EXISTS ai_model_credentials_envelope_check,
  DROP CONSTRAINT IF EXISTS ai_model_credentials_provider_check;

ALTER TABLE secret_versions
  DROP CONSTRAINT IF EXISTS secret_versions_envelope_check;
