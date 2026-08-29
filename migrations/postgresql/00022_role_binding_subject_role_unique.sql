-- +goose Up

DELETE FROM role_bindings
WHERE id IN (
    SELECT id
    FROM (
        SELECT id, row_number() OVER (
            PARTITION BY enterprise_id, subject_type, subject_id, role_id
            ORDER BY (status = 'active') DESC, updated_at DESC, id DESC
        ) AS duplicate_rank
        FROM role_bindings
    ) duplicate
    WHERE duplicate.duplicate_rank > 1
);

CREATE UNIQUE INDEX role_bindings_subject_role_unique
    ON role_bindings (enterprise_id, subject_type, subject_id, role_id);

-- +goose Down

DROP INDEX IF EXISTS role_bindings_subject_role_unique;
