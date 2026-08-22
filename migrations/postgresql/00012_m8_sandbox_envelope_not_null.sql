-- +goose Up
ALTER TABLE sandbox_backends
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
      AND credential_key_version IS NOT NULL
      AND credential_key_version > 0
      AND credential_wrapped_dek IS NOT NULL
      AND octet_length(credential_wrapped_dek) > 0
      AND credential_nonce IS NOT NULL
      AND octet_length(credential_nonce) = 12
      AND credential_ciphertext IS NOT NULL
      AND octet_length(credential_ciphertext) > 0
      AND credential_value_hash IS NOT NULL
      AND octet_length(credential_value_hash) = 32
      AND (
        (credential_provider = 'local' AND credential_wrap_nonce IS NOT NULL AND octet_length(credential_wrap_nonce) = 12)
        OR (credential_provider = 'openbao_transit' AND credential_wrap_nonce IS NULL)
      )
    )
  );

-- +goose Down
ALTER TABLE sandbox_backends
  DROP CONSTRAINT IF EXISTS sandbox_backends_credential_envelope_check;
