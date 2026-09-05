-- Authoritative onboarding projections. These queries deliberately join the
-- workflow/result/operation records in PostgreSQL so API clients never infer
-- state by correlating tokens or local caches.

-- name: ListHostOnboardingFacts :many
SELECT
  host.id AS resource_id,
  COALESCE(action.action_ref, '')::text AS action_ref,
  COALESCE(action.status, '')::text AS action_status,
  execution.id AS execution_id,
  COALESCE(execution.status, '')::text AS execution_status,
  COALESCE(execution.error_code, action.error_code, '')::text AS error_code,
  CASE
    WHEN result.execution_id IS NULL THEN 'unavailable'
    WHEN result.consumed_at IS NOT NULL THEN 'consumed'
    WHEN result.expires_at <= now() THEN 'expired'
    ELSE 'available'
  END::text AS one_time_result_state,
  COALESCE(enrollment.status, '')::text AS enrollment_status,
  COALESCE(collector.status, '')::text AS collector_status,
  COALESCE(enrollment.updated_at, execution.updated_at, action.updated_at, collector.updated_at, host.updated_at) AS projection_updated_at
FROM hosts AS host
LEFT JOIN LATERAL (
  SELECT candidate.* FROM pending_actions AS candidate
  WHERE candidate.enterprise_id = host.enterprise_id
    AND candidate.resource_type = 'host'
    AND candidate.action_type IN ('host.create','host.enrollment.rotate')
    AND COALESCE(candidate.result_resource_id, candidate.resource_id) = host.id
  ORDER BY candidate.updated_at DESC, candidate.id DESC LIMIT 1
) AS action ON true
LEFT JOIN executions AS execution ON execution.pending_action_id = action.id
LEFT JOIN execution_one_time_results AS result ON result.execution_id = execution.id
LEFT JOIN LATERAL (
  SELECT token.status, token.updated_at FROM host_enrollment_tokens AS token
  WHERE token.enterprise_id = host.enterprise_id AND token.preallocated_host_id = host.id
  ORDER BY token.created_at DESC, token.id DESC LIMIT 1
) AS enrollment ON true
LEFT JOIN collector_instances AS collector ON collector.enterprise_id = host.enterprise_id
  AND collector.resource_type = 'host' AND collector.resource_id = host.id
WHERE host.enterprise_id = $1 AND host.id = ANY($2::uuid[]);

-- name: ListBastionOnboardingFacts :many
SELECT
  scope.id AS resource_id,
  COALESCE(action.action_ref, '')::text AS action_ref,
  COALESCE(action.status, '')::text AS action_status,
  execution.id AS execution_id,
  COALESCE(execution.status, '')::text AS execution_status,
  COALESCE(execution.error_code, operation.error_code, action.error_code, '')::text AS error_code,
  CASE
    WHEN result.execution_id IS NULL THEN 'unavailable'
    WHEN result.consumed_at IS NOT NULL THEN 'consumed'
    WHEN result.expires_at <= now() THEN 'expired'
    ELSE 'available'
  END::text AS one_time_result_state,
  COALESCE(operation.id, '00000000-0000-0000-0000-000000000000'::uuid) AS operation_id,
  COALESCE(operation.status, '')::text AS operation_status,
  COALESCE(operation.stage, '')::text AS operation_stage,
  COALESCE(enrollment.status, '')::text AS enrollment_status,
  COALESCE(connector.status, '')::text AS connector_status,
  COALESCE(control_tunnel.status, '')::text AS control_tunnel_status,
  COALESCE(control_tunnel.updated_at, connector.updated_at, operation.updated_at,
    execution.updated_at, action.updated_at, scope.updated_at) AS projection_updated_at
FROM bastion_scopes AS scope
LEFT JOIN LATERAL (
  SELECT candidate.* FROM pending_actions AS candidate
  WHERE candidate.enterprise_id = scope.enterprise_id
    AND candidate.resource_type = 'bastion_scope'
    AND candidate.action_type IN ('bastion_scope.create','bastion.enrollment.rotate',
      'bastion.connector.install.retry','bastion.connector.replace')
    AND COALESCE(candidate.result_resource_id, candidate.resource_id) = scope.id
  ORDER BY candidate.updated_at DESC, candidate.id DESC LIMIT 1
) AS action ON true
LEFT JOIN executions AS execution ON execution.pending_action_id = action.id
LEFT JOIN execution_one_time_results AS result ON result.execution_id = execution.id
LEFT JOIN LATERAL (
  SELECT candidate.* FROM connector_install_operations AS candidate
  WHERE candidate.enterprise_id = scope.enterprise_id AND candidate.bastion_scope_id = scope.id
  ORDER BY candidate.created_at DESC, candidate.id DESC LIMIT 1
) AS operation ON true
LEFT JOIN LATERAL (
  SELECT token.status FROM connector_enrollment_tokens AS token
  WHERE token.enterprise_id = scope.enterprise_id AND token.bastion_scope_id = scope.id
  ORDER BY token.created_at DESC, token.id DESC LIMIT 1
) AS enrollment ON true
LEFT JOIN connectors AS connector ON connector.id = scope.active_connector_id AND connector.enterprise_id = scope.enterprise_id
LEFT JOIN connector_control_tunnels AS control_tunnel ON control_tunnel.enterprise_id = scope.enterprise_id
  AND control_tunnel.bastion_scope_id = scope.id AND control_tunnel.status <> 'removed'
WHERE scope.enterprise_id = $1 AND scope.id = ANY($2::uuid[]);
