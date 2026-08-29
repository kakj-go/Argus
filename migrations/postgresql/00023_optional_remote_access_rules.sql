-- +goose Up

ALTER TABLE remote_access_rules
    DROP CONSTRAINT IF EXISTS remote_access_rules_effects_check;

UPDATE remote_access_rules
SET effects = array_remove(effects, 'allow'),
    version = version + 1,
    updated_at = now()
WHERE 'allow' = ANY(effects);

UPDATE remote_access_rules
SET status = 'archived',
    effects = ARRAY['notify']::text[],
    version = version + 1,
    updated_at = now()
WHERE cardinality(effects) = 0
  AND session_profile_id IS NULL;

ALTER TABLE remote_access_rules
    ADD CONSTRAINT remote_access_rules_effects_check CHECK (
        cardinality(effects) <= 4
        AND effects <@ ARRAY['deny','require_mfa','require_approval','notify']::text[]
        AND (cardinality(effects) > 0 OR session_profile_id IS NOT NULL)
    );

-- +goose Down

ALTER TABLE remote_access_rules
    DROP CONSTRAINT IF EXISTS remote_access_rules_effects_check;

UPDATE remote_access_rules
SET effects = ARRAY['allow']::text[],
    version = version + 1,
    updated_at = now()
WHERE cardinality(effects) = 0;

ALTER TABLE remote_access_rules
    ADD CONSTRAINT remote_access_rules_effects_check CHECK (
        cardinality(effects) > 0
        AND effects <@ ARRAY['allow','deny','require_mfa','require_approval','notify']::text[]
    );
