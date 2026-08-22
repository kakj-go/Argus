-- +goose Up
ALTER TABLE secret_versions
  DROP CONSTRAINT IF EXISTS secret_versions_provider_check;

UPDATE secret_versions
SET provider = 'openbao_transit'
WHERE provider = 'openbao';

ALTER TABLE secret_versions
  ADD CONSTRAINT secret_versions_provider_check
  CHECK (provider IN ('local', 'openbao_transit'));

-- +goose Down
ALTER TABLE secret_versions
  DROP CONSTRAINT IF EXISTS secret_versions_provider_check;

UPDATE secret_versions
SET provider = 'openbao'
WHERE provider = 'openbao_transit';

ALTER TABLE secret_versions
  ADD CONSTRAINT secret_versions_provider_check
  CHECK (provider IN ('local', 'vault', 'openbao', 'aws_kms', 'gcp_kms', 'azure_key_vault'));
