-- name: CreateTrustBundle :one
INSERT INTO pki_trust_bundles (
  epoch, state, bundle_pem, bundle_sha256, current_ca_fingerprints,
  next_ca_fingerprints, started_at, retire_at, last_error
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetCurrentTrustBundle :one
SELECT * FROM pki_trust_bundles
WHERE state <> 'failed'
ORDER BY epoch DESC
LIMIT 1;

-- name: GetTrustBundle :one
SELECT * FROM pki_trust_bundles WHERE epoch = $1;

-- name: ListTrustBundles :many
SELECT * FROM pki_trust_bundles ORDER BY epoch DESC LIMIT $1;

-- name: UpdateTrustBundleState :one
UPDATE pki_trust_bundles
SET state = $2,
    bundle_pem = $3,
    bundle_sha256 = $4,
    current_ca_fingerprints = $5,
    next_ca_fingerprints = $6,
    retire_at = $7,
    last_error = $8,
    updated_at = now()
WHERE epoch = $1
RETURNING *;

-- name: ReverseTrustBundleOverlap :one
UPDATE pki_trust_bundles
SET state = 'overlapping',
    direction = 'rollback',
    current_ca_fingerprints = next_ca_fingerprints,
    next_ca_fingerprints = current_ca_fingerprints,
    retire_at = $2,
    last_error = '',
    updated_at = now()
WHERE epoch = $1
  AND direction = 'forward'
  AND state IN ('preparing','overlapping')
  AND cardinality(current_ca_fingerprints) > 0
  AND cardinality(next_ca_fingerprints) > 0
RETURNING *;

-- name: UpsertNodeTrustPending :one
INSERT INTO pki_node_trust_acks (
  node_kind, node_id, enterprise_id, epoch, bundle_sha256,
  ca_fingerprints, status, required_for_cutover, error
) VALUES ($1, $2, $3, $4, $5, $6, 'pending', true, '')
ON CONFLICT (node_kind, node_id, epoch) DO UPDATE
SET enterprise_id = EXCLUDED.enterprise_id,
    bundle_sha256 = EXCLUDED.bundle_sha256,
    ca_fingerprints = EXCLUDED.ca_fingerprints,
    required_for_cutover = true,
    status = CASE WHEN pki_node_trust_acks.status = 'acked' THEN 'acked' ELSE 'pending' END,
    error = '',
    updated_at = now()
RETURNING *;

-- name: AcknowledgeNodeTrustBundle :one
UPDATE pki_node_trust_acks
SET bundle_sha256 = $4,
    ca_fingerprints = $5,
    status = 'acked',
    error = '',
    acknowledged_at = now(),
    updated_at = now()
WHERE node_kind = $1 AND node_id = $2 AND epoch = $3
RETURNING *;

-- name: FailNodeTrustBundle :one
UPDATE pki_node_trust_acks
SET status = 'failed', error = $4, acknowledged_at = NULL, updated_at = now()
WHERE node_kind = $1 AND node_id = $2 AND epoch = $3
RETURNING *;

-- name: ListNodeTrustAcks :many
SELECT * FROM pki_node_trust_acks
WHERE epoch = $1
ORDER BY node_kind, node_id;

-- name: CountUnacknowledgedTrustNodes :one
SELECT count(*) FROM pki_node_trust_acks
WHERE epoch = $1 AND required_for_cutover AND status <> 'acked';

-- name: SeedTrustBundleNodes :execrows
INSERT INTO pki_node_trust_acks (
  node_kind, node_id, enterprise_id, epoch, bundle_sha256,
  ca_fingerprints, status, required_for_cutover, error
)
SELECT node_kind, node_id, enterprise_id, sqlc.arg(target_epoch),
       sqlc.arg(target_bundle_sha256), sqlc.arg(target_ca_fingerprints)::text[],
       'pending',
       CASE
         WHEN source.node_kind = 'control_plane'
           THEN COALESCE(source.node_id = ANY(sqlc.arg(active_control_plane_ids)::text[]), false)
         ELSE source.updated_at >= sqlc.arg(active_since)
       END,
       ''
FROM pki_node_trust_acks AS source
WHERE source.epoch = sqlc.arg(source_epoch)
  AND source.status = 'acked'
ON CONFLICT (node_kind, node_id, epoch) DO NOTHING;

-- name: MarkUnacknowledgedTrustExpired :execrows
UPDATE pki_node_trust_acks
SET status = 'trust_expired', error = 'TRUST_BUNDLE_EPOCH_RETIRED', updated_at = now()
WHERE epoch = $1 AND status <> 'acked';

-- name: CreatePKICertificateIdentity :one
INSERT INTO pki_certificate_identities (
  serial_number, subject_kind, subject_id, enterprise_id, uri_san, dns_sans,
  extended_key_usage, issuer_generation, certificate_sha256, status,
  not_before, not_after
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (serial_number) DO UPDATE
SET updated_at = pki_certificate_identities.updated_at
WHERE pki_certificate_identities.subject_kind = EXCLUDED.subject_kind
  AND pki_certificate_identities.subject_id = EXCLUDED.subject_id
  AND pki_certificate_identities.enterprise_id IS NOT DISTINCT FROM EXCLUDED.enterprise_id
  AND pki_certificate_identities.uri_san = EXCLUDED.uri_san
  AND pki_certificate_identities.dns_sans = EXCLUDED.dns_sans
  AND pki_certificate_identities.extended_key_usage = EXCLUDED.extended_key_usage
  AND pki_certificate_identities.issuer_generation = EXCLUDED.issuer_generation
  AND pki_certificate_identities.certificate_sha256 = EXCLUDED.certificate_sha256
  AND pki_certificate_identities.status IN ('active','overlap')
  AND pki_certificate_identities.not_before = EXCLUDED.not_before
  AND pki_certificate_identities.not_after = EXCLUDED.not_after
RETURNING *;

-- name: LockPKICertificateSubject :exec
SELECT pg_advisory_xact_lock(hashtextextended(
  sqlc.arg(subject_kind)::text || ':' || sqlc.arg(subject_id)::text,
  0
));

-- name: GetActivePKICertificateIdentity :one
SELECT * FROM pki_certificate_identities
WHERE serial_number = $1 AND status IN ('active','overlap') AND not_after > now();

-- name: RevokePKICertificateIdentity :one
UPDATE pki_certificate_identities
SET status = 'revoked', revoked_at = now(), revocation_reason = $2, updated_at = now()
WHERE serial_number = $1 AND status IN ('active','overlap')
RETURNING *;

-- name: MarkPKISubjectCertificatesOverlap :exec
UPDATE pki_certificate_identities
SET status = 'overlap', not_after = LEAST(not_after, now() + interval '15 minutes'), updated_at = now()
WHERE subject_kind = $1 AND subject_id = $2 AND status = 'active';

-- name: RevokePKISubjectCertificates :exec
UPDATE pki_certificate_identities
SET status = 'revoked', revoked_at = now(), revocation_reason = $3, updated_at = now()
WHERE subject_kind = $1 AND subject_id = $2 AND status IN ('active','overlap');
