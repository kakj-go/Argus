package card

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const SystemCatalogRevision = 4

type SystemCatalogCard struct {
	Slug              string
	Name              string
	Description       string
	PresentationKind  string
	ToolID            string
	Path              string
	ValueType         string
	SemanticType      string
	Actions           []string
	QueryRefresh      bool
	DependencyPending bool
}

func SystemCatalog() []SystemCatalogCard {
	return []SystemCatalogCard{
		{Slug: "host-list", Name: "Host list", Description: "System renderer for authorized Host inventory.", PresentationKind: "table", ToolID: "host.list", Path: "$.items", ValueType: "array", SemanticType: "resource_collection", QueryRefresh: true},
		{Slug: "kubernetes-cluster-list", Name: "Kubernetes clusters", Description: "System renderer for authorized Kubernetes clusters.", PresentationKind: "table", ToolID: "kubernetes.cluster.list", Path: "$.items", ValueType: "array", SemanticType: "resource_collection", QueryRefresh: true},
		{Slug: "connector-status-list", Name: "Connector status", Description: "System renderer for Connector status.", PresentationKind: "table", ToolID: "connector.list", Path: "$.items", ValueType: "array", SemanticType: "resource_collection", QueryRefresh: true},
		{Slug: "pending-action", Name: "Pending action", Description: "System renderer for preview, confirmation, and cancellation.", PresentationKind: "pending_action", ToolID: "pending_action.get", Path: "$", ValueType: "object", SemanticType: "pending_action", Actions: []string{"confirm", "cancel"}},
		{Slug: "telemetry-overview", Name: "Telemetry overview", Description: "Validated system renderer activated when M7 registers a compatible Telemetry schema.", PresentationKind: "metric", ToolID: "telemetry.overview", Path: "$", ValueType: "object", SemanticType: "telemetry_overview", DependencyPending: true},
	}
}

func (service Service) SyncSystemCatalog(ctx context.Context) error {
	if service.Store == nil || service.Tools == nil {
		return ErrDependencyUnavailable
	}
	for _, definition := range SystemCatalog() {
		definition := definition
		if err := service.Store.InTx(ctx, func(q *db.Queries) error { return service.syncSystemCard(ctx, q, definition) }); err != nil {
			return fmt.Errorf("sync system Card %s: %w", definition.Slug, err)
		}
	}
	return nil
}

func (service Service) syncSystemCard(ctx context.Context, q *db.Queries, definition SystemCatalogCard) error {
	cardID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("argus.system-card/"+definition.Slug))
	availability := "available"
	enabled := true
	metadata, dependencyAvailable := service.Tools.Lookup(definition.ToolID)
	if definition.DependencyPending || !dependencyAvailable || metadata.OutputSchemaHash == "" {
		availability = "dependency_pending"
		enabled = false
	}
	card, err := q.UpsertSystemInteractiveCard(ctx, db.UpsertSystemInteractiveCardParams{ID: cardID, Slug: definition.Slug, Name: definition.Name,
		Description: definition.Description, Enabled: enabled, Availability: availability, LatestRevision: SystemCatalogRevision})
	if errors.Is(err, pgx.ErrNoRows) {
		card, err = q.GetSystemCardBySlug(ctx, definition.Slug)
	}
	if err != nil {
		return err
	}
	html := systemCardHTML(definition)
	contentHash := sha256.Sum256(html)
	manifest, err := systemManifest(cardID, definition, contentHash)
	if err != nil {
		return err
	}
	manifestHash := sha256.Sum256(manifest)
	versionID := uuid.NewSHA1(cardID, []byte(fmt.Sprintf("revision/%d", SystemCatalogRevision)))
	version, err := q.CreateSystemCardVersionIfMissing(ctx, db.CreateSystemCardVersionIfMissingParams{ID: versionID, CardID: card.ID,
		Revision: SystemCatalogRevision, Manifest: manifest, EntrypointHtml: html, ContentHash: contentHash[:], ManifestHash: manifestHash[:]})
	if err != nil {
		return err
	}
	if !slices.Equal(version.ContentHash, contentHash[:]) || !slices.Equal(version.ManifestHash, manifestHash[:]) {
		return fmt.Errorf("system Card %s revision %d changed without a catalog revision bump", definition.Slug, SystemCatalogRevision)
	}
	existing, err := q.ListCardSlotBindings(ctx, version.ID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		schemaHash := make([]byte, sha256.Size)
		outputVersion := "dependency_pending/v1"
		if dependencyAvailable {
			decoded, decodeErr := hexDecode(metadata.OutputSchemaHash)
			if decodeErr == nil {
				schemaHash = decoded
			}
			outputVersion = metadata.OutputVersion
		}
		if _, err := q.CreateCardSlotBinding(ctx, db.CreateCardSlotBindingParams{ID: newID(), CardVersionID: version.ID, SlotName: "primary", SlotKind: "data",
			Mode: "strict", ToolID: definition.ToolID, OutputSchemaVersion: outputVersion, SchemaHash: schemaHash, FieldPath: definition.Path, ValueType: definition.ValueType}); err != nil {
			return err
		}
		if definition.QueryRefresh {
			if _, err := q.CreateCardSlotBinding(ctx, db.CreateCardSlotBindingParams{ID: newID(), CardVersionID: version.ID, SlotName: "refresh", SlotKind: "query",
				Mode: "strict", ToolID: definition.ToolID, OutputSchemaVersion: outputVersion, SchemaHash: schemaHash, FieldPath: "$", ValueType: "object"}); err != nil {
				return err
			}
		}
		for _, action := range definition.Actions {
			if _, err := q.CreateCardSlotBinding(ctx, db.CreateCardSlotBindingParams{ID: newID(), CardVersionID: version.ID, SlotName: action, SlotKind: "action",
				Mode: "strict", ToolID: "pending_action." + action, OutputSchemaVersion: outputVersion, SchemaHash: schemaHash, FieldPath: "$", ValueType: "object",
				SemanticType: pgtype.Text{String: "pending_action", Valid: true}}); err != nil {
				return err
			}
		}
		for _, scenario := range requiredScenarios {
			data := json.RawMessage(`{"primary":null}`)
			if _, err := q.CreateCardDemoScenario(ctx, db.CreateCardDemoScenarioParams{ID: newID(), CardVersionID: version.ID, Scenario: scenario, Data: data, ByteSize: int32(len(data))}); err != nil {
				return err
			}
		}
	}
	if err = q.RetireActiveCardVersions(ctx, db.RetireActiveCardVersionsParams{CardID: card.ID, ID: version.ID}); err != nil {
		return err
	}
	_, err = q.SetSystemCardActiveVersion(ctx, db.SetSystemCardActiveVersionParams{ID: card.ID, ActiveVersionID: nullUUID(version.ID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func systemManifest(cardID uuid.UUID, definition SystemCatalogCard, contentHash [sha256.Size]byte) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"schema_version": "argus.card_manifest/v1", "card_id": cardID.String(), "revision": SystemCatalogRevision, "source": "system",
		"entrypoint_hash": fmt.Sprintf("%x", contentHash[:]), "bridge_version": "argus.card_bridge/v1", "max_message_bytes": 1024 * 1024,
		"slots":             systemSlots(definition),
		"allowed_resources": []string{"inline_style", "inline_script"}, "supported_locales": []string{"zh-CN", "en-US"}, "default_locale": "zh-CN",
		"supported_color_schemes": []string{"light", "dark"}, "presentation_kind": definition.PresentationKind,
	})
}

func systemCardHTML(definition SystemCatalogCard) []byte {
	if len(definition.Actions) > 0 {
		return []byte(`<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;font:14px system-ui;color:CanvasText;background:Canvas}main{padding:12px}pre{white-space:pre-wrap}.actions{display:flex;gap:8px}button{font:inherit}</style></head><body><main><strong>` +
			definition.Name + `</strong><pre id="content" data-slot="primary"></pre><div class="actions"><button type="button" data-slot="confirm" id="confirm">Confirm</button><button type="button" data-slot="cancel" id="cancel">Cancel</button></div></main><script>(function(){var api=window.argusCard;function render(value){document.getElementById("content").textContent=JSON.stringify(value.primary||null,null,2)}render(api.data);api.onData(render);document.getElementById("confirm").addEventListener("click",function(){var id=api.bindings.action.confirm;if(id)api.action(id)});document.getElementById("cancel").addEventListener("click",function(){var id=api.bindings.action.cancel;if(id)api.action(id)})})()</script></body></html>`)
	}
	if definition.QueryRefresh {
		return []byte(`<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;font:14px system-ui;color:CanvasText;background:Canvas}main{padding:12px}pre{white-space:pre-wrap}button{font:inherit}</style></head><body><main><strong>` +
			definition.Name + `</strong><button type="button" data-slot="refresh" id="refresh">Refresh</button><pre id="content" data-slot="primary"></pre></main><script>(function(){var api=window.argusCard;function render(value){document.getElementById("content").textContent=JSON.stringify(value.primary||value.items||[],null,2)}render(api.data);api.onData(render);document.getElementById("refresh").addEventListener("click",function(){var id=api.bindings.query.refresh;if(id)api.query(id).then(render)})})()</script></body></html>`)
	}
	return []byte(`<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;font:14px system-ui;color:CanvasText;background:Canvas}main{padding:12px}pre{white-space:pre-wrap}</style></head><body><main><strong>` +
		definition.Name + `</strong><pre id="content" data-slot="primary"></pre></main><script>(function(){var api=window.argusCard;function render(value){document.getElementById("content").textContent=JSON.stringify(value.primary||null,null,2)}render(api.data);api.onData(render)})()</script></body></html>`)
}

func systemSlots(definition SystemCatalogCard) []map[string]any {
	slots := []map[string]any{{"name": "primary", "kind": "data", "required": true, "value_type": definition.ValueType}}
	if definition.QueryRefresh {
		slots = append(slots, map[string]any{"name": "refresh", "kind": "query", "required": false, "value_type": "object"})
	}
	for _, action := range definition.Actions {
		slots = append(slots, map[string]any{"name": action, "kind": "action", "required": true, "value_type": "object"})
	}
	return slots
}

func hexDecode(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("empty hash")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("invalid hash")
	}
	return decoded, nil
}

func systemCardExists(ctx context.Context, q *db.Queries, slug string) bool {
	_, err := q.GetSystemCardBySlug(ctx, slug)
	return err == nil || !errors.Is(err, pgx.ErrNoRows)
}
