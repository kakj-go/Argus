-- name: ListInteractiveCards :many
SELECT * FROM interactive_cards
WHERE source = 'system' OR enterprise_id = $1
ORDER BY source, slug, id;

-- name: GetInteractiveCard :one
SELECT * FROM interactive_cards
WHERE id = $1 AND (source = 'system' OR enterprise_id = $2);

-- name: GetInteractiveCardForUpdate :one
SELECT * FROM interactive_cards
WHERE id = $1 AND (source = 'system' OR enterprise_id = $2)
FOR UPDATE;

-- name: GetSystemCardBySlug :one
SELECT * FROM interactive_cards WHERE source = 'system' AND slug = $1;

-- name: CreateInteractiveCard :one
INSERT INTO interactive_cards (id, enterprise_id, source, slug, name, description, lifecycle, enabled,
    availability, latest_revision, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: UpdateInteractiveCardDraft :one
UPDATE interactive_cards SET name = $3, description = $4, latest_revision = $5,
    version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND source = 'enterprise' AND lifecycle <> 'deprecated' AND version = $6
RETURNING *;

-- name: ActivateInteractiveCard :one
UPDATE interactive_cards SET active_version_id = $3, lifecycle = 'active', enabled = true,
    availability = 'available', version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND source = 'enterprise' AND lifecycle <> 'deprecated' AND version = $4
RETURNING *;

-- name: DisableInteractiveCard :one
UPDATE interactive_cards SET enabled = false, availability = 'disabled', version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND source = 'enterprise' AND lifecycle <> 'deprecated' AND version = $3
RETURNING *;

-- name: DeprecateInteractiveCard :one
UPDATE interactive_cards SET enabled = false, availability = 'disabled', lifecycle = 'deprecated',
    version = version + 1, updated_at = now()
WHERE id = $1 AND enterprise_id = $2 AND source = 'enterprise' AND lifecycle <> 'deprecated' AND version = $3
RETURNING *;

-- name: UpsertSystemInteractiveCard :one
INSERT INTO interactive_cards (id, source, slug, name, description, lifecycle, enabled, availability, latest_revision)
VALUES ($1,'system',$2,$3,$4,'active',$5,$6,$7)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
    enabled = EXCLUDED.enabled, availability = EXCLUDED.availability,
    latest_revision = GREATEST(interactive_cards.latest_revision, EXCLUDED.latest_revision),
    version = interactive_cards.version + 1, updated_at = now()
WHERE (interactive_cards.name, interactive_cards.description, interactive_cards.enabled, interactive_cards.availability, interactive_cards.latest_revision)
    IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.description, EXCLUDED.enabled, EXCLUDED.availability, EXCLUDED.latest_revision)
RETURNING *;

-- name: SetSystemCardActiveVersion :one
UPDATE interactive_cards SET active_version_id = $2, lifecycle = 'active', updated_at = now()
WHERE id = $1 AND source = 'system' AND active_version_id IS DISTINCT FROM $2 RETURNING *;

-- name: CreateCardVersion :one
INSERT INTO card_versions (id, card_id, revision, status, manifest, entrypoint_html, content_hash, manifest_hash, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: CreateSystemCardVersionIfMissing :one
INSERT INTO card_versions (id, card_id, revision, status, manifest, entrypoint_html, content_hash, manifest_hash)
VALUES ($1,$2,$3,'active',$4,$5,$6,$7)
ON CONFLICT (card_id, revision) DO UPDATE SET status = card_versions.status
RETURNING *;

-- name: GetCardVersion :one
SELECT version.* FROM card_versions version
JOIN interactive_cards card ON card.id = version.card_id
WHERE version.card_id = $1 AND version.revision = $2
  AND (card.source = 'system' OR card.enterprise_id = $3);

-- name: GetCardVersionByID :one
SELECT * FROM card_versions WHERE id = $1;

-- name: GetCardVersionForEnterprise :one
SELECT version.* FROM card_versions version
JOIN interactive_cards card ON card.id = version.card_id
WHERE version.id = $1 AND version.card_id = $2
  AND (card.source = 'system' OR card.enterprise_id = $3);

-- name: ListCardVersions :many
SELECT version.* FROM card_versions version
JOIN interactive_cards card ON card.id = version.card_id
WHERE version.card_id = $1 AND (card.source = 'system' OR card.enterprise_id = $2)
ORDER BY version.revision DESC;

-- name: SetCardVersionStatus :one
UPDATE card_versions SET status = $2 WHERE id = $1 RETURNING *;

-- name: RetireActiveCardVersions :exec
UPDATE card_versions SET status = 'retired'
WHERE card_id = $1 AND status = 'active' AND id <> $2;

-- name: CreateCardSlotBinding :one
INSERT INTO card_slot_bindings (id, card_version_id, slot_name, slot_kind, mode, tool_id,
    output_schema_version, schema_hash, field_path, value_type, semantic_type)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: ListCardSlotBindings :many
SELECT * FROM card_slot_bindings WHERE card_version_id = $1 ORDER BY slot_name;

-- name: CreateCardDemoScenario :one
INSERT INTO card_demo_scenarios (id, card_version_id, scenario, data, byte_size)
VALUES ($1,$2,$3,$4,$5) RETURNING *;

-- name: ListCardDemoScenarios :many
SELECT * FROM card_demo_scenarios WHERE card_version_id = $1 ORDER BY scenario;

-- name: CreateCardValidationRun :one
INSERT INTO card_validation_runs (id, card_version_id, enterprise_id, actor_user_id, content_hash,
    runtime_version, nonce_hash, required_scenarios, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: GetCardValidationRun :one
SELECT * FROM card_validation_runs WHERE id = $1 AND enterprise_id = $2;

-- name: GetCardValidationRunForUpdate :one
SELECT * FROM card_validation_runs WHERE id = $1 AND enterprise_id = $2 FOR UPDATE;

-- name: FinishCardValidationRun :one
UPDATE card_validation_runs SET status = $3, passed_scenarios = $4, issues = $5,
    completed_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'pending'
RETURNING *;

-- name: GetLatestPassedCardValidation :one
SELECT * FROM card_validation_runs
WHERE card_version_id = $1 AND enterprise_id = $2 AND status = 'passed' AND expires_at > now()
ORDER BY completed_at DESC LIMIT 1;

-- name: CreateCardInstance :one
INSERT INTO card_instances (id, enterprise_id, conversation_id, run_id, card_id, card_version_id,
    actor_user_id, presentation_kind, render_spec, render_spec_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: GetCardInstanceForViewer :one
SELECT instance.* FROM card_instances instance
JOIN conversations conversation ON conversation.id = instance.conversation_id AND conversation.enterprise_id = instance.enterprise_id
WHERE instance.id = $1 AND instance.enterprise_id = $2 AND conversation.owner_user_id = $3;

-- name: CreateCardDataSource :one
INSERT INTO card_data_sources (id, card_instance_id, slot_name, tool_call_id, result_ref,
    field_path, output_schema_version, source_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: ListCardDataSources :many
SELECT source.*, artifact.content, result.projection, call.input, call.tool_id
FROM card_data_sources source
JOIN artifacts artifact ON artifact.result_ref = source.result_ref
JOIN tool_results result ON result.artifact_id = artifact.id
JOIN tool_calls call ON call.id = source.tool_call_id
WHERE source.card_instance_id = $1
ORDER BY source.slot_name;

-- name: GetCardRenderSource :one
SELECT call.id AS tool_call_id, call.run_id, call.tool_id, call.input,
       result.projection, result.partial, artifact.result_ref, artifact.content_hash
FROM tool_calls call
JOIN tool_results result ON result.tool_call_id = call.id
JOIN artifacts artifact ON artifact.id = result.artifact_id
WHERE call.id = $1 AND call.run_id = $2 AND call.enterprise_id = $3
  AND call.status = 'succeeded';

-- name: CreateCardQueryBindingSpec :one
INSERT INTO card_query_binding_specs (id, card_instance_id, slot_name, tool_id, fixed_input,
    input_hash, output_schema_version, schema_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: ListCardQueryBindingSpecs :many
SELECT * FROM card_query_binding_specs WHERE card_instance_id = $1 ORDER BY slot_name;

-- name: CreateCardPresentation :one
INSERT INTO card_presentations (id, card_instance_id, enterprise_id, viewer_user_id, authorization_version,
    locale, color_scheme, locale_fallback, initial_data, partial, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: CreateCardQueryBinding :one
INSERT INTO card_query_bindings (id, binding_ref, presentation_id, binding_spec_id, enterprise_id,
    viewer_user_id, authorization_version, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: GetCardQueryBindingForInvoke :one
SELECT binding.*, spec.card_instance_id, spec.slot_name, spec.tool_id, spec.fixed_input,
    spec.input_hash, spec.output_schema_version, spec.schema_hash
FROM card_query_bindings binding
JOIN card_query_binding_specs spec ON spec.id = binding.binding_spec_id
WHERE binding.binding_ref = $1 AND binding.enterprise_id = $2;

-- name: MarkCardQueryBindingInvoked :one
UPDATE card_query_bindings SET last_invoked_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'active' AND expires_at > now()
RETURNING *;

-- name: CreateCardActionBinding :one
INSERT INTO action_bindings (id, binding_ref, pending_action_id, enterprise_id, actor_user_id, action,
    request_id, expires_at, card_instance_id, conversation_id, authorization_version, binding_source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'card')
RETURNING *;

-- name: GetCardActionBindingForUpdate :one
SELECT binding.* FROM action_bindings binding
WHERE binding.binding_ref = $1 AND binding.enterprise_id = $2 AND binding.binding_source = 'card'
FOR UPDATE;

-- name: ConsumeCardActionBinding :one
UPDATE action_bindings SET status = 'consumed', consumed_at = now()
WHERE id = $1 AND enterprise_id = $2 AND status = 'pending' AND expires_at > now()
RETURNING *;
