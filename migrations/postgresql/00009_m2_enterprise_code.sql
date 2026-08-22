-- +goose Up
ALTER TABLE enterprises DROP CONSTRAINT enterprises_code_check;
ALTER TABLE enterprises ADD CONSTRAINT enterprises_code_check
    CHECK (char_length(code) BETWEEN 1 AND 63 AND code ~ '^[a-z0-9]+(-[a-z0-9]+)*$');

-- +goose Down
ALTER TABLE enterprises DROP CONSTRAINT enterprises_code_check;
ALTER TABLE enterprises ADD CONSTRAINT enterprises_code_check
    CHECK (code ~ '^[a-z][a-z0-9-]{1,62}$');
