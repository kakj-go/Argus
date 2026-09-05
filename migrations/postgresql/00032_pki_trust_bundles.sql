-- +goose Up

-- Trust Bundles are versioned deployment state.  The PEM is public material;
-- private CA keys remain exclusively in cert-manager issuer Secrets.
ALTER TABLE telemetry_certificates ADD COLUMN certificate_usage text NOT NULL
    CHECK (certificate_usage IN ('clientAuth','serverAuth'));

CREATE TABLE pki_trust_bundles (
    epoch bigint PRIMARY KEY CHECK (epoch > 0),
    state text NOT NULL CHECK (state IN ('stable','preparing','overlapping','retiring','failed')),
    direction text NOT NULL DEFAULT 'forward' CHECK (direction IN ('forward','rollback')),
    bundle_pem text NOT NULL CHECK (char_length(bundle_pem) BETWEEN 128 AND 131072),
    bundle_sha256 text NOT NULL CHECK (bundle_sha256 ~ '^[a-f0-9]{64}$'),
    current_ca_fingerprints text[] NOT NULL CHECK (cardinality(current_ca_fingerprints) > 0),
    next_ca_fingerprints text[] NOT NULL DEFAULT '{}',
    started_at timestamptz NOT NULL DEFAULT now(),
    retire_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((state IN ('overlapping','retiring')) = (retire_at IS NOT NULL)),
    CHECK (state <> 'overlapping' OR cardinality(next_ca_fingerprints) > 0),
    CHECK (direction = 'forward' OR state IN ('overlapping','retiring'))
);

CREATE TABLE pki_node_trust_acks (
    node_kind text NOT NULL CHECK (node_kind IN ('connector','collector','kubernetes_connector','control_plane')),
    node_id text NOT NULL CHECK (char_length(node_id) BETWEEN 1 AND 256),
    enterprise_id uuid REFERENCES enterprises(id),
    epoch bigint NOT NULL REFERENCES pki_trust_bundles(epoch) ON DELETE CASCADE,
    bundle_sha256 text NOT NULL CHECK (bundle_sha256 ~ '^[a-f0-9]{64}$'),
    ca_fingerprints text[] NOT NULL DEFAULT '{}',
    status text NOT NULL CHECK (status IN ('pending','acked','failed','trust_expired')),
    required_for_cutover boolean NOT NULL DEFAULT false,
    error text NOT NULL DEFAULT '',
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    acknowledged_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (node_kind, node_id, epoch),
    CHECK ((status = 'acked') = (acknowledged_at IS NOT NULL))
);
CREATE INDEX pki_node_trust_acks_epoch_status_idx ON pki_node_trust_acks (epoch, status, node_kind);

-- All issued identities are recorded in one role-aware registry in addition
-- to domain-specific certificate history.  This is the revocation and
-- cross-role-use decision point for mTLS authorization.
CREATE TABLE pki_certificate_identities (
    serial_number text PRIMARY KEY CHECK (char_length(serial_number) BETWEEN 1 AND 256),
    subject_kind text NOT NULL CHECK (subject_kind IN ('service','connector','collector')),
    subject_id text NOT NULL CHECK (char_length(subject_id) BETWEEN 1 AND 256),
    enterprise_id uuid REFERENCES enterprises(id),
    uri_san text NOT NULL DEFAULT '',
    dns_sans text[] NOT NULL DEFAULT '{}',
    extended_key_usage text NOT NULL CHECK (extended_key_usage IN ('serverAuth','clientAuth')),
    issuer_generation integer NOT NULL CHECK (issuer_generation > 0),
    certificate_sha256 text NOT NULL CHECK (certificate_sha256 ~ '^[a-f0-9]{64}$'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','overlap','revoked','expired')),
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    revoked_at timestamptz,
    revocation_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (not_after > not_before),
    CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);
CREATE INDEX pki_certificate_identities_subject_idx
    ON pki_certificate_identities (subject_kind, subject_id, status);
CREATE INDEX pki_certificate_identities_expiry_idx
    ON pki_certificate_identities (not_after) WHERE status IN ('active','overlap');

-- +goose Down
DROP INDEX IF EXISTS pki_certificate_identities_expiry_idx;
DROP INDEX IF EXISTS pki_certificate_identities_subject_idx;
DROP TABLE IF EXISTS pki_certificate_identities;
DROP INDEX IF EXISTS pki_node_trust_acks_epoch_status_idx;
DROP TABLE IF EXISTS pki_node_trust_acks;
DROP TABLE IF EXISTS pki_trust_bundles;
ALTER TABLE telemetry_certificates DROP COLUMN IF EXISTS certificate_usage;
