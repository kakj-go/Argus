-- name: ListCollectorInstances :many
SELECT * FROM collector_instances
WHERE enterprise_id = $1
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type'))
  AND (sqlc.narg('resource_id')::uuid IS NULL OR resource_id = sqlc.narg('resource_id'))
ORDER BY updated_at DESC, id
LIMIT $2;

-- name: GetCollectorInstance :one
SELECT * FROM collector_instances WHERE id = $1 AND enterprise_id = $2;

-- name: GetCollectorForResource :one
SELECT * FROM collector_instances WHERE enterprise_id = $1 AND resource_type = $2 AND resource_id = $3;

-- name: GetCollectorInstanceByID :one
SELECT * FROM collector_instances WHERE id = $1;

-- name: UpsertCollectorForAction :one
INSERT INTO collector_instances (
  id, enterprise_id, resource_type, resource_id, distribution_version_id,
  platform, role, status, desired_revision, effective_revision
) VALUES ($1,$2,$3,$4,$5,$6,$7,'installing',1,0)
ON CONFLICT (enterprise_id, resource_type, resource_id) DO UPDATE SET
  distribution_version_id = EXCLUDED.distribution_version_id,
  platform = EXCLUDED.platform,
  role = EXCLUDED.role,
  status = 'installing',
  desired_revision = collector_instances.desired_revision + 1,
  version = collector_instances.version + 1,
  updated_at = now()
RETURNING *;

-- name: MarkCollectorUninstalling :one
UPDATE collector_instances SET status = 'uninstalling', desired_revision = desired_revision + 1,
  version = version + 1, updated_at = now()
WHERE enterprise_id = $1 AND resource_type = $2 AND resource_id = $3
  AND ($4::bigint = 0 OR version = $4)
RETURNING *;

-- name: SupersedeCollectorConfigRevisions :exec
UPDATE collector_config_revisions SET status = 'superseded'
WHERE collector_id = $1 AND status IN ('prepared','applying','effective');

-- name: CreateCollectorConfigRevision :one
INSERT INTO collector_config_revisions (
  id, collector_id, revision, profile_ids, rendered_config, config_hash, status
) VALUES ($1,$2,$3,$4,$5,$6,'prepared') RETURNING *;

-- name: UpsertTelemetryRoute :one
INSERT INTO telemetry_routes (
  id, enterprise_id, collector_id, kind, gateway_collector_id, status
) VALUES ($1,$2,$3,$4,$5,'pending')
ON CONFLICT (collector_id) DO UPDATE SET
  kind = EXCLUDED.kind,
  gateway_collector_id = EXCLUDED.gateway_collector_id,
  status = 'pending',
  version = telemetry_routes.version + 1,
  updated_at = now()
RETURNING *;

-- name: CreateTelemetryCollectorOperation :one
INSERT INTO telemetry_collector_operations (
  id, enterprise_id, collector_id, pending_action_id, operation, executor_kind,
  plan, plan_hash, expires_at
) VALUES ($1,$2,$3,$4,$5,'direct',$6,$7,$8)
RETURNING *;

-- name: ClaimTelemetryCollectorOperations :many
WITH claimed AS (
  SELECT id FROM telemetry_collector_operations
  WHERE status = 'queued' AND expires_at > now()
  ORDER BY created_at, id
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
UPDATE telemetry_collector_operations AS operation SET
  status = 'running', lease_owner = $2, fence = fence + 1,
  lease_expires_at = now() + interval '2 minutes', attempts = attempts + 1, updated_at = now()
FROM claimed WHERE operation.id = claimed.id
RETURNING operation.*;

-- name: ClaimTelemetryCollectorOperation :one
UPDATE telemetry_collector_operations SET
  status = 'running', lease_owner = $2, fence = fence + 1,
  lease_expires_at = now() + interval '2 minutes', attempts = attempts + 1, updated_at = now()
WHERE id = $1 AND status = 'queued' AND expires_at > now()
RETURNING *;

-- name: GetTelemetryCollectorOperation :one
SELECT * FROM telemetry_collector_operations WHERE id = $1 AND enterprise_id = $2;

-- name: FinishTelemetryCollectorOperation :one
UPDATE telemetry_collector_operations SET status = $4, result_hash = $5, error_code = $6,
  lease_owner = NULL, lease_expires_at = NULL, completed_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND fence = $3 AND status IN ('running','result_unknown')
RETURNING *;

-- name: RecoverTelemetryCollectorOperations :execrows
UPDATE telemetry_collector_operations SET status = 'result_unknown', lease_owner = NULL,
  lease_expires_at = NULL, error_code = 'EXECUTION_RESULT_UNKNOWN', updated_at = now()
WHERE status = 'running' AND lease_expires_at <= now();

-- name: ExpireTelemetryCollectorOperations :execrows
UPDATE telemetry_collector_operations SET status = 'expired', error_code = 'COLLECTOR_OPERATION_EXPIRED', completed_at = now(), updated_at = now()
WHERE status = 'queued' AND expires_at <= now();

-- name: ApplyCollectorOperationSuccess :execrows
UPDATE collector_instances SET
  status = CASE WHEN $3 = 'uninstall' THEN 'uninstalled' ELSE 'converged' END,
  effective_revision = CASE WHEN $3 = 'uninstall' THEN effective_revision ELSE desired_revision END,
  version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2;

-- name: ApplyCollectorOperationFailure :execrows
UPDATE collector_instances SET status = $3, version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2;

-- name: MarkCollectorConfigApplying :execrows
UPDATE collector_config_revisions SET status = 'applying'
WHERE collector_id = $1 AND revision = $2 AND status = 'prepared';

-- name: MarkCollectorConfigEffective :execrows
UPDATE collector_config_revisions SET status = 'effective', applied_at = now()
WHERE collector_id = $1 AND revision = $2 AND status IN ('prepared','applying');

-- name: MarkCollectorConfigFailed :execrows
UPDATE collector_config_revisions SET status = 'failed', failure_code = $3
WHERE collector_id = $1 AND revision = $2 AND status IN ('prepared','applying');

-- name: MarkTelemetryRouteActive :execrows
UPDATE telemetry_routes SET status = 'active', last_tested_at = now(), version = version + 1, updated_at = now()
WHERE collector_id = $1 AND enterprise_id = $2 AND status IN ('pending','testing','degraded');

-- name: MarkTelemetryRouteDegraded :execrows
UPDATE telemetry_routes SET status = 'degraded', version = version + 1, updated_at = now()
WHERE collector_id = $1 AND enterprise_id = $2 AND status <> 'invalidated';

-- name: ReleaseCollectorClaims :exec
UPDATE collection_claims SET status = 'released'
WHERE enterprise_id = $1 AND collector_id = $2 AND status IN ('active','conflict');

-- name: CreateCollectionClaim :one
INSERT INTO collection_claims (
  id, enterprise_id, physical_resource_ref, collector_id, profile_id,
  claim_type, signal, selector, selector_hash, ownership, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'primary','active') RETURNING *;

-- name: GetActivePrimaryCollectionClaim :one
SELECT * FROM collection_claims
WHERE enterprise_id = $1 AND physical_resource_ref = $2 AND claim_type = $3
  AND selector_hash = $4 AND ownership = 'primary' AND status = 'active'
FOR UPDATE;

-- name: CreateMigrationCollectionClaim :one
INSERT INTO collection_claims (
  id, enterprise_id, physical_resource_ref, collector_id, profile_id,
  claim_type, signal, selector, selector_hash, ownership, status,
  primary_claim_id, rollback_plan, expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'migration','active',$10,$11,$12) RETURNING *;

-- name: FinalizeCollectorClaimMigrations :execrows
WITH migrations AS (
  SELECT claims.id, claims.primary_claim_id FROM collection_claims AS claims
  WHERE claims.enterprise_id = $1 AND claims.collector_id = $2 AND claims.ownership = 'migration' AND claims.status = 'active' AND claims.expires_at > now()
), released AS (
  UPDATE collection_claims AS primary_claim SET status = 'released'
  WHERE primary_claim.id IN (SELECT migration.primary_claim_id FROM migrations AS migration) AND primary_claim.status = 'active'
)
UPDATE collection_claims AS migration_claim SET ownership = 'primary', primary_claim_id = NULL, rollback_plan = NULL, expires_at = NULL
WHERE migration_claim.id IN (SELECT migration.id FROM migrations AS migration);

-- name: RollbackCollectorClaimMigrations :execrows
UPDATE collection_claims SET status = 'expired'
WHERE enterprise_id = $1 AND collector_id = $2 AND ownership = 'migration' AND status = 'active';

-- name: ExpireCollectionClaimMigrations :execrows
UPDATE collection_claims SET status = 'expired'
WHERE ownership = 'migration' AND status = 'active' AND expires_at <= now();

-- name: UpsertKubernetesNodeHostBindingProposal :one
INSERT INTO kubernetes_node_host_bindings (
  id, enterprise_id, kubernetes_cluster_id, node_uid, node_name, host_id,
  matched_by, evidence, evidence_hash, confidence, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'proposed')
ON CONFLICT (enterprise_id, kubernetes_cluster_id, node_uid) DO UPDATE SET
  node_name = EXCLUDED.node_name,
	host_id = CASE
		WHEN kubernetes_node_host_bindings.status = 'verified' AND kubernetes_node_host_bindings.evidence_hash = EXCLUDED.evidence_hash
		THEN kubernetes_node_host_bindings.host_id ELSE EXCLUDED.host_id END,
  evidence = EXCLUDED.evidence,
  evidence_hash = EXCLUDED.evidence_hash,
	matched_by = CASE
		WHEN kubernetes_node_host_bindings.status = 'verified' AND kubernetes_node_host_bindings.evidence_hash = EXCLUDED.evidence_hash
		THEN kubernetes_node_host_bindings.matched_by ELSE EXCLUDED.matched_by END,
	confidence = CASE
		WHEN kubernetes_node_host_bindings.status = 'verified' AND kubernetes_node_host_bindings.evidence_hash = EXCLUDED.evidence_hash
		THEN kubernetes_node_host_bindings.confidence ELSE EXCLUDED.confidence END,
  status = CASE WHEN kubernetes_node_host_bindings.status = 'verified' AND kubernetes_node_host_bindings.evidence_hash = EXCLUDED.evidence_hash THEN 'verified' ELSE 'proposed' END,
  version = kubernetes_node_host_bindings.version + 1,
  updated_at = now()
RETURNING *;

-- name: ConfirmKubernetesNodeHostBinding :one
UPDATE kubernetes_node_host_bindings SET
  host_id = $3, matched_by = 'manual', status = 'verified', version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $4 AND evidence_hash = $5
	AND status = 'proposed'
RETURNING *;

-- name: RevokeCollectorCertificates :exec
UPDATE telemetry_certificates SET revoked_at = now(), revoke_reason = $2
WHERE collector_id = $1 AND revoked_at IS NULL;

-- name: CreateTelemetryEnrollmentToken :one
INSERT INTO telemetry_enrollment_tokens (id, collector_id, token_hash, expires_at)
VALUES ($1,$2,$3,$4) RETURNING *;

-- name: GetTelemetryEnrollmentTokenForUpdate :one
SELECT * FROM telemetry_enrollment_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: ConsumeTelemetryEnrollmentToken :one
UPDATE telemetry_enrollment_tokens SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: CreateTelemetryCertificate :one
INSERT INTO telemetry_certificates (
  id, collector_id, serial_number, uri_san, csr_hash, certificate_hash,
  certificate_request_name, issuer_generation, not_before, not_after
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: GetValidTelemetryCertificateBySerial :one
SELECT * FROM telemetry_certificates
WHERE collector_id = $1 AND serial_number = $2 AND revoked_at IS NULL AND not_after > now();

-- name: LimitTelemetryCertificateOverlap :exec
UPDATE telemetry_certificates
SET not_after = LEAST(not_after, now() + interval '15 minutes')
WHERE collector_id = $1 AND revoked_at IS NULL AND serial_number <> $2;

-- name: ListTelemetryRoutes :many
SELECT * FROM telemetry_routes WHERE enterprise_id = $1 ORDER BY updated_at DESC, id;

-- name: GetTelemetryRoute :one
SELECT * FROM telemetry_routes WHERE id = $1 AND enterprise_id = $2;

-- name: GetTelemetryRouteByCollector :one
SELECT * FROM telemetry_routes WHERE collector_id = $1 AND enterprise_id = $2;

-- name: CreateTelemetryRouteTest :one
INSERT INTO telemetry_route_tests (id, enterprise_id, route_id, status, expires_at)
VALUES ($1,$2,$3,'queued',$4) RETURNING *;

-- name: CompleteTelemetryRouteTest :one
UPDATE telemetry_route_tests SET
  status = $3,
  result_code = $4,
  result_hash = $5,
  completed_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'queued'
RETURNING *;

-- name: ListCollectionClaims :many
SELECT * FROM collection_claims
WHERE enterprise_id = $1
	AND (sqlc.narg('physical_resource_ref')::text IS NULL
		OR physical_resource_ref = 'host:' || sqlc.narg('physical_resource_ref')
		OR physical_resource_ref = 'kubernetes_cluster:' || sqlc.narg('physical_resource_ref'))
ORDER BY created_at DESC, id;

-- name: ListKubernetesNodeHostBindings :many
SELECT * FROM kubernetes_node_host_bindings
WHERE enterprise_id = $1 AND kubernetes_cluster_id = $2
ORDER BY node_name, id;

-- name: GetKubernetesNodeHostBinding :one
SELECT * FROM kubernetes_node_host_bindings WHERE id = $1 AND enterprise_id = $2;

-- name: GetTelemetryRetentionPolicy :one
SELECT * FROM telemetry_retention_policies WHERE enterprise_id = $1;

-- name: EnsureTelemetryRetentionPolicy :one
INSERT INTO telemetry_retention_policies (enterprise_id) VALUES ($1)
ON CONFLICT (enterprise_id) DO UPDATE SET enterprise_id = EXCLUDED.enterprise_id
RETURNING *;

-- name: GetTelemetryUsage :one
SELECT
    coalesce(sum(ingested_bytes), 0)::bigint AS ingested_bytes,
    coalesce(sum(metric_points), 0)::bigint AS metric_points,
    coalesce(sum(log_records), 0)::bigint AS log_records,
    coalesce(sum(spans), 0)::bigint AS spans,
    coalesce(sum(estimated_storage_bytes), 0)::bigint AS estimated_storage_bytes
FROM telemetry_usage_daily
WHERE enterprise_id = $1 AND usage_date >= $2 AND usage_date <= $3;

-- name: RecordTelemetryDLQ :one
INSERT INTO telemetry_dlq_records (id, signal, topic, partition, source_offset, dlq_topic, dlq_partition, dlq_offset, record_hash, error_code)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (topic, partition, source_offset, record_hash) DO UPDATE SET error_code = EXCLUDED.error_code
RETURNING *;

-- name: ClaimTelemetryDLQReplay :one
UPDATE telemetry_dlq_records SET status = 'replaying'
WHERE id = $1 AND status IN ('pending','failed')
RETURNING *;

-- name: MarkTelemetryDLQReplayed :exec
UPDATE telemetry_dlq_records SET status = 'replayed', replayed_at = now() WHERE id = $1 AND status = 'replaying';

-- name: MarkTelemetryDLQReplayFailed :exec
UPDATE telemetry_dlq_records SET status = 'failed' WHERE id = $1 AND status = 'replaying';

-- name: GetTelemetryCollectorIdentityBySerial :one
SELECT ci.*, tc.serial_number, tc.uri_san, tc.not_after
FROM telemetry_certificates tc
JOIN collector_instances ci ON ci.id = tc.collector_id
WHERE tc.serial_number = $1 AND tc.revoked_at IS NULL AND tc.not_before <= now() AND tc.not_after > now()
  AND ci.status NOT IN ('uninstalled','uninstalling');

-- name: GetTelemetryCollectorIdentity :one
SELECT * FROM collector_instances WHERE id = $1 AND status NOT IN ('uninstalled','uninstalling');

-- name: IncrementTelemetryUsage :exec
INSERT INTO telemetry_usage_daily (enterprise_id, usage_date, ingested_bytes, metric_points, log_records, spans, estimated_storage_bytes)
VALUES ($1, current_date, $2, $3, $4, $5, $6)
ON CONFLICT (enterprise_id, usage_date) DO UPDATE SET
  ingested_bytes = telemetry_usage_daily.ingested_bytes + EXCLUDED.ingested_bytes,
  metric_points = telemetry_usage_daily.metric_points + EXCLUDED.metric_points,
  log_records = telemetry_usage_daily.log_records + EXCLUDED.log_records,
  spans = telemetry_usage_daily.spans + EXCLUDED.spans,
  estimated_storage_bytes = telemetry_usage_daily.estimated_storage_bytes + EXCLUDED.estimated_storage_bytes,
  updated_at = now();
