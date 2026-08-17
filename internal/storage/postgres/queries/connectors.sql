-- name: CreateBastionScope :one
INSERT INTO bastion_scopes (id, enterprise_id, name, environment, labels, labels_hash)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: GetBastionScope :one
SELECT s.*, (SELECT count(*) FROM hosts h WHERE h.bastion_scope_id = s.id AND h.connection_mode = 'via_bastion' AND h.status <> 'deleted')::bigint AS member_count
FROM bastion_scopes s WHERE s.id = $1 AND s.enterprise_id = $2 AND s.status <> 'deleted';

-- name: ListBastionScopes :many
SELECT s.*, (SELECT count(*) FROM hosts h WHERE h.bastion_scope_id = s.id AND h.connection_mode = 'via_bastion' AND h.status <> 'deleted')::bigint AS member_count
FROM bastion_scopes s WHERE s.enterprise_id = $1 AND s.status <> 'deleted' ORDER BY s.created_at, s.id;

-- name: UpdateBastionScope :one
UPDATE bastion_scopes SET name = COALESCE(sqlc.narg('name'), name), environment = COALESCE(sqlc.narg('environment'), environment),
 labels = COALESCE(sqlc.narg('labels'), labels), labels_hash = COALESCE(sqlc.narg('labels_hash'), labels_hash),
 resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND resource_version = $3 AND status <> 'deleted' RETURNING *;

-- name: FenceBastionScope :one
UPDATE bastion_scopes SET fencing_generation = fencing_generation + 1, active_connector_id = NULL, status = 'offline', resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND resource_version = $3 AND status IN ('offline','uninstalled','suspected_offline') RETURNING *;

-- name: DeleteBastionScope :one
UPDATE bastion_scopes AS scope SET status = 'deleted', deleted_at = now(), resource_version = scope.resource_version + 1, updated_at = now()
WHERE scope.id = $1 AND scope.enterprise_id = $2 AND scope.resource_version = $3
AND (scope.status = 'uninstalled' OR (scope.status = 'offline' AND scope.active_connector_id IS NULL))
AND NOT EXISTS (SELECT 1 FROM hosts AS host WHERE host.bastion_scope_id = scope.id AND host.connection_mode = 'via_bastion' AND host.status <> 'deleted')
AND NOT EXISTS (
  SELECT 1 FROM connector_commands AS command
  JOIN connectors AS connector ON connector.id = command.connector_id AND connector.enterprise_id = command.enterprise_id
  WHERE connector.bastion_scope_id = scope.id
    AND command.status IN ('queued','dispatched','acknowledged','running','delivery_unknown','result_unknown')
)
RETURNING *;

-- name: CreateConnectorEnrollmentToken :one
INSERT INTO connector_enrollment_tokens (id, preallocated_connector_id, enterprise_id, role, purpose, bastion_scope_id, kubernetes_cluster_id, preallocated_host_id, token_hash, policy, expires_at, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: GetEnrollmentTokenForUpdate :one
SELECT * FROM connector_enrollment_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: ConsumeEnrollmentToken :one
UPDATE connector_enrollment_tokens SET status = 'consumed', consumed_at = now(), consumed_device_hash = $2, registered_connector_id = $3
WHERE id = $1 AND status = 'active' AND expires_at > now() RETURNING *;

-- name: RevokeActiveEnrollmentTokens :exec
UPDATE connector_enrollment_tokens SET status = 'revoked'
WHERE enterprise_id = $1 AND status = 'active' AND (bastion_scope_id = $2 OR kubernetes_cluster_id = $2);

-- name: CreateConnector :one
INSERT INTO connectors (id, enterprise_id, role, name, host_id, bastion_scope_id, kubernetes_cluster_id, instance_id, device_fingerprint_hash, public_key_hash, software_version, capabilities, certificate_expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: GetConnector :one
SELECT * FROM connectors WHERE id = $1 AND enterprise_id = $2;

-- name: GetConnectorByID :one
SELECT * FROM connectors WHERE id = $1;

-- name: ListConnectors :many
SELECT * FROM connectors WHERE enterprise_id = $1 ORDER BY created_at, id;

-- name: GetConnectorByInstance :one
SELECT * FROM connectors WHERE enterprise_id = $1 AND instance_id = $2;

-- name: ActivateBastionConnector :one
UPDATE bastion_scopes SET connector_host_id = $3, active_connector_id = $4, status = 'active', resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 RETURNING *;

-- name: ActivateKubernetesConnector :one
UPDATE kubernetes_clusters SET connector_id = $3, connection_status = 'connected', resource_version = resource_version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 RETURNING *;

-- name: RestoreBastionConnectorOnline :execrows
UPDATE bastion_scopes SET status = 'active', resource_version = resource_version + 1, updated_at = now()
WHERE enterprise_id = $1 AND active_connector_id = $2 AND status IN ('suspected_offline','offline');

-- name: RestoreKubernetesConnectorOnline :execrows
UPDATE kubernetes_clusters SET connection_status = 'connected', resource_version = resource_version + 1, updated_at = now()
WHERE enterprise_id = $1 AND connector_id = $2 AND status <> 'deleted' AND connection_status IN ('degraded','disconnected');

-- name: CreateConnectorCertificate :one
INSERT INTO connector_certificates (id, connector_id, enterprise_id, serial_number, issuer_generation, certificate_request_name, certificate_pem, ca_bundle_pem, not_before, not_after)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: GetActiveConnectorCertificate :one
SELECT * FROM connector_certificates
WHERE connector_id = $1 AND enterprise_id = $2 AND status IN ('active','overlap') AND not_after > now()
ORDER BY not_after DESC LIMIT 1;

-- name: GetValidConnectorCertificateBySerial :one
SELECT * FROM connector_certificates
WHERE connector_id = $1 AND enterprise_id = $2 AND serial_number = $3
  AND status IN ('active','overlap') AND not_before <= now() AND not_after > now();

-- name: RequestConnectorCertificateRotation :one
UPDATE connectors SET certificate_rotation_requested_at = now(), version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $3
  AND status NOT IN ('uninstalled','revoked') RETURNING *;

-- name: StartConnectorUninstall :one
UPDATE connectors SET status = 'draining', version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND version = $3 AND connection_epoch = $4 AND status = 'online'
RETURNING *;

-- name: MarkBastionScopeUninstalling :execrows
UPDATE bastion_scopes SET status = 'uninstalling', resource_version = resource_version + 1, updated_at = now()
WHERE enterprise_id = $1 AND active_connector_id = $2 AND status = 'active';

-- name: MarkKubernetesConnectorUninstalling :execrows
UPDATE kubernetes_clusters SET connection_status = 'degraded', resource_version = resource_version + 1, updated_at = now()
WHERE enterprise_id = $1 AND connector_id = $2 AND status <> 'deleted';

-- name: FinalizeConnectorUninstall :one
UPDATE connectors SET status = 'uninstalled', version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status IN ('draining','online','suspected_offline','offline')
RETURNING *;

-- name: FinalizeBastionConnectorUninstall :execrows
UPDATE bastion_scopes SET active_connector_id = NULL, status = 'uninstalled', fencing_generation = fencing_generation + 1,
 resource_version = resource_version + 1, updated_at = now()
WHERE enterprise_id = $1 AND active_connector_id = $2 AND status IN ('active','uninstalling','suspected_offline','offline');

-- name: FinalizeKubernetesConnectorUninstall :execrows
UPDATE kubernetes_clusters SET connector_id = NULL, connection_status = 'disconnected', resource_version = resource_version + 1, updated_at = now()
WHERE enterprise_id = $1 AND connector_id = $2 AND status <> 'deleted';

-- name: MarkConnectorCertificatesOverlap :exec
UPDATE connector_certificates
SET status = 'overlap', not_after = LEAST(not_after, now() + interval '15 minutes')
WHERE connector_id = $1 AND enterprise_id = $2 AND status = 'active';

-- name: CompleteConnectorCertificateRotation :one
UPDATE connectors SET public_key_hash = $3, certificate_expires_at = $4,
 certificate_rotation_requested_at = NULL, version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status NOT IN ('uninstalled','revoked') RETURNING *;

-- name: RevokeConnectorCertificates :exec
UPDATE connector_certificates SET status = 'revoked', revoked_at = now()
WHERE connector_id = $1 AND enterprise_id = $2 AND status IN ('active','overlap');

-- name: AdvanceConnectorEpoch :one
UPDATE connectors SET connection_epoch = connection_epoch + 1, status = 'online', connected_at = now(), last_heartbeat_at = now(), updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status NOT IN ('uninstalled','revoked') RETURNING *;

-- name: UpsertConnectorSession :one
INSERT INTO connector_sessions (connector_id, enterprise_id, gateway_instance_id, connection_epoch, capabilities, connected_at, last_heartbeat_at)
VALUES ($1,$2,$3,$4,$5,now(),now())
ON CONFLICT (connector_id) DO UPDATE SET gateway_instance_id = EXCLUDED.gateway_instance_id, connection_epoch = EXCLUDED.connection_epoch,
 capabilities = EXCLUDED.capabilities, connected_at = now(), last_heartbeat_at = now(), draining = false
RETURNING *;

-- name: HeartbeatConnectorSession :execrows
UPDATE connector_sessions SET last_heartbeat_at = now()
WHERE connector_id = $1 AND enterprise_id = $2 AND connection_epoch = $3;

-- name: CloseConnectorSession :execrows
DELETE FROM connector_sessions WHERE connector_id = $1 AND enterprise_id = $2 AND connection_epoch = $3;

-- name: MarkConnectorDisconnected :execrows
UPDATE connectors SET status = 'suspected_offline', updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND connection_epoch = $3 AND status IN ('online','draining');

-- name: MarkBastionScopeConnectorSuspectedOffline :execrows
UPDATE bastion_scopes SET status = 'suspected_offline', updated_at = now()
WHERE enterprise_id = $1 AND active_connector_id = $2 AND status = 'active';

-- name: MarkStaleConnectorsOffline :execrows
UPDATE connectors SET status = 'offline', updated_at = now()
WHERE status = 'suspected_offline' AND last_heartbeat_at < now() - interval '5 minutes';

-- name: MarkStaleBastionScopesOffline :execrows
UPDATE bastion_scopes AS scope SET status = 'offline', updated_at = now()
WHERE scope.status = 'suspected_offline' AND EXISTS (
  SELECT 1 FROM connectors AS connector
  WHERE connector.id = scope.active_connector_id AND connector.enterprise_id = scope.enterprise_id AND connector.status = 'offline'
);

-- name: CreateConnectorCommand :one
INSERT INTO connector_commands (id, command_id, enterprise_id, connector_id, connection_epoch, operation_ref, credential_lease_id, command_type, payload_schema_version, payload, payload_hash, idempotency_key, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: GetConnectorCommand :one
SELECT * FROM connector_commands WHERE command_id = $1 AND connector_id = $2 AND connection_epoch = $3;

-- name: GetConnectorCommandByID :one
SELECT * FROM connector_commands WHERE id = $1;

-- name: ListUncertainConnectorCommands :many
SELECT * FROM connector_commands WHERE connector_id = $1 AND status IN ('delivery_unknown','result_unknown')
ORDER BY created_at LIMIT $2;

-- name: ListDispatchableConnectorCommands :many
SELECT * FROM connector_commands WHERE connector_id = $1 AND connection_epoch = $2 AND status = 'queued' AND expires_at > now()
ORDER BY created_at LIMIT $3 FOR UPDATE SKIP LOCKED;

-- name: ExpireQueuedConnectorCommands :many
UPDATE connector_commands SET status = 'expired', error_code = 'CONNECTOR_COMMAND_EXPIRED', completed_at = now(), updated_at = now()
WHERE status = 'queued' AND expires_at <= now() RETURNING *;

-- name: TimeoutActiveConnectorCommands :many
UPDATE connector_commands SET status = 'timed_out', error_code = 'CONNECTOR_COMMAND_TIMED_OUT', completed_at = now(), updated_at = now()
WHERE status IN ('dispatched','acknowledged','running') AND expires_at <= now() RETURNING *;

-- name: TransitionConnectorCommand :one
UPDATE connector_commands SET status = $4, result = COALESCE(sqlc.narg('result'), result), result_hash = COALESCE(sqlc.narg('result_hash'), result_hash),
 error_code = COALESCE(sqlc.narg('error_code'), error_code), updated_at = now(),
 acknowledged_at = CASE WHEN $4 = 'acknowledged' THEN now() ELSE acknowledged_at END,
 started_at = CASE WHEN $4 = 'running' THEN now() ELSE started_at END,
 completed_at = CASE WHEN $4 IN ('succeeded','failed','timed_out','expired') THEN now() ELSE completed_at END
WHERE command_id = $1 AND connector_id = $2 AND connection_epoch = $3 RETURNING *;
