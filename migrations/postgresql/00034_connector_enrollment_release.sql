-- +goose Up

-- Manual bootstrap script downloads must remain bound to the release frozen
-- by the Pending Action even when the active release changes before download.
-- Keep this in a new migration because development clusters may already have
-- applied migration 00031 before this relationship was introduced.
ALTER TABLE connector_enrollment_tokens
    ADD COLUMN release_version_id uuid REFERENCES connector_release_versions(id);

-- +goose Down
ALTER TABLE connector_enrollment_tokens DROP COLUMN IF EXISTS release_version_id;
