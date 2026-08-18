package card

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type renderSpec struct {
	Actions []renderAction `json:"actions,omitempty"`
}

type renderAction struct {
	SlotName  string `json:"slot_name"`
	ActionRef string `json:"action_ref"`
	Action    string `json:"action"`
}

type BindingActionResult struct {
	Status        string
	PendingAction db.PendingAction
	Value         any
}

type QueryBindingResult struct {
	Value   map[string]any `json:"value"`
	Partial bool           `json:"partial"`
}

// CreatePresentation rematerializes all data with the current viewer identity.
// Stored Tool artifacts are provenance only and are never returned directly.
func (service Service) CreatePresentation(ctx context.Context, actorID, enterpriseID, instanceID uuid.UUID, authorizationVersion int64, locale, colorScheme, idempotencyKey string) (PresentationResult, error) {
	if service.Store == nil || service.Tools == nil {
		return PresentationResult{}, ErrDependencyUnavailable
	}
	request := struct {
		InstanceID           uuid.UUID `json:"card_instance_id"`
		AuthorizationVersion int64     `json:"authorization_version"`
		Locale               string    `json:"locale"`
		ColorScheme          string    `json:"color_scheme"`
	}{instanceID, authorizationVersion, locale, colorScheme}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID.String(), "interactive_card.presentation.create", idempotencyKey, request, 201,
		func(q *db.Queries) (PresentationResult, error) {
			return service.createPresentation(ctx, q, actorID, enterpriseID, instanceID, authorizationVersion, locale, colorScheme)
		})
}

func (service Service) createPresentation(ctx context.Context, q *db.Queries, actorID, enterpriseID, instanceID uuid.UUID, authorizationVersion int64, locale, colorScheme string) (PresentationResult, error) {
	instance, err := q.GetCardInstanceForViewer(ctx, db.GetCardInstanceForViewerParams{ID: instanceID, EnterpriseID: enterpriseID, OwnerUserID: actorID})
	if errors.Is(err, pgx.ErrNoRows) || instance.Status != "active" {
		return PresentationResult{}, ErrPresentationInvalid
	}
	if err != nil {
		return PresentationResult{}, err
	}
	card, err := q.GetInteractiveCard(ctx, db.GetInteractiveCardParams{ID: instance.CardID, EnterpriseID: nullUUID(enterpriseID)})
	if err != nil {
		return PresentationResult{}, err
	}
	version, err := q.GetCardVersionForEnterprise(ctx, db.GetCardVersionForEnterpriseParams{ID: instance.CardVersionID, CardID: card.ID, EnterpriseID: nullUUID(enterpriseID)})
	if err != nil || version.CardID != card.ID {
		return PresentationResult{}, ErrPresentationInvalid
	}
	resolvedLocale, fallback, err := presentationLocale(version.Manifest, locale)
	if err != nil || colorScheme != "light" && colorScheme != "dark" {
		return PresentationResult{}, ErrBindingInvalid
	}
	dataSources, err := q.ListCardDataSources(ctx, instance.ID)
	if err != nil {
		return PresentationResult{}, err
	}
	initialData := make(map[string]any, len(dataSources))
	partial := false
	for _, source := range dataSources {
		metadata, exists := service.Tools.Lookup(source.ToolID)
		if !exists || metadata.OutputVersion != source.OutputSchemaVersion || !metadata.CardSafe {
			return PresentationResult{}, ErrPresentationInvalid
		}
		var input map[string]any
		if err := json.Unmarshal(source.Input, &input); err != nil {
			return PresentationResult{}, ErrPresentationInvalid
		}
		call := mcp.Call{ToolID: source.ToolID, Caller: "card_presentation", Enterprise: enterpriseID.String(), Subject: actorID.String(), SubjectType: "user", Input: input}
		var result mcp.Result
		if metadata.Risk == "read" {
			result, err = service.Tools.Call(ctx, call)
		} else {
			var projection map[string]any
			err = json.Unmarshal(source.Projection, &projection)
			result.Structured = projection
		}
		if err != nil {
			return PresentationResult{}, ErrPresentationInvalid
		}
		projected, itemPartial, err := service.Tools.ProjectForCard(ctx, call, result)
		if err != nil {
			return PresentationResult{}, ErrPresentationInvalid
		}
		value, err := EvaluateJSONPath(projected, source.FieldPath)
		if err != nil {
			return PresentationResult{}, ErrBindingInvalid
		}
		initialData[source.SlotName] = value
		partial = partial || itemPartial
	}
	encodedData, err := json.Marshal(initialData)
	if err != nil {
		return PresentationResult{}, err
	}
	limit := service.MaxPresentation
	if limit <= 0 {
		limit = 1024 * 1024
	}
	if len(encodedData) > limit {
		return PresentationResult{}, ErrBindingInvalid
	}
	ttl := service.PresentationTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	expiresAt := time.Now().UTC().Add(ttl)
	result := PresentationResult{Instance: instance, Card: card, Version: version, InitialData: initialData, Locale: resolvedLocale,
		LocaleFallback: fallback, Partial: partial, QueryBindings: map[string]string{}, ActionBindings: map[string]string{}}
	presentation, err := q.CreateCardPresentation(ctx, db.CreateCardPresentationParams{ID: newID(), CardInstanceID: instance.ID, EnterpriseID: enterpriseID,
		ViewerUserID: actorID, AuthorizationVersion: authorizationVersion, Locale: resolvedLocale, ColorScheme: colorScheme, LocaleFallback: fallback,
		InitialData: encodedData, Partial: partial, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
	if err != nil {
		return PresentationResult{}, err
	}
	result.Presentation = presentation
	specs, err := q.ListCardQueryBindingSpecs(ctx, instance.ID)
	if err != nil {
		return PresentationResult{}, err
	}
	for _, spec := range specs {
		metadata, exists := service.Tools.Lookup(spec.ToolID)
		if !exists || metadata.Risk != "read" || metadata.OutputVersion != spec.OutputSchemaVersion || !hashMatches(metadata.OutputSchemaHash, spec.SchemaHash) {
			return PresentationResult{}, ErrBindingInvalid
		}
		bindingRef, _, err := randomSecret(24)
		if err != nil {
			return PresentationResult{}, err
		}
		binding, err := q.CreateCardQueryBinding(ctx, db.CreateCardQueryBindingParams{ID: newID(), BindingRef: "card_q_" + bindingRef,
			PresentationID: presentation.ID, BindingSpecID: spec.ID, EnterpriseID: enterpriseID, ViewerUserID: actorID,
			AuthorizationVersion: authorizationVersion, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
		if err != nil {
			return PresentationResult{}, err
		}
		result.QueryBindings[spec.SlotName] = binding.BindingRef
	}
	var spec renderSpec
	if err := json.Unmarshal(instance.RenderSpec, &spec); err != nil {
		return PresentationResult{}, ErrBindingInvalid
	}
	for _, action := range spec.Actions {
		if action.Action != "confirm" && action.Action != "cancel" {
			return PresentationResult{}, ErrBindingInvalid
		}
		pending, err := q.GetPendingAction(ctx, db.GetPendingActionParams{ActionRef: action.ActionRef, EnterpriseID: enterpriseID})
		if err != nil || pending.CreatorSubjectID != actorID || pending.AuthorizationVersion != authorizationVersion {
			return PresentationResult{}, ErrPresentationInvalid
		}
		actionExpiry := expiresAt
		if pending.ExpiresAt.Time.Before(actionExpiry) {
			actionExpiry = pending.ExpiresAt.Time
		}
		bindingRef, _, err := randomSecret(24)
		if err != nil {
			return PresentationResult{}, err
		}
		binding, err := q.CreateCardActionBinding(ctx, db.CreateCardActionBindingParams{ID: newID(), BindingRef: "card_a_" + bindingRef,
			PendingActionID: pending.ID, EnterpriseID: enterpriseID, ActorUserID: nullUUID(actorID), Action: action.Action,
			RequestID: cardActionRequestID(presentation.ID, action.SlotName), ExpiresAt: pgtype.Timestamptz{Time: actionExpiry, Valid: true}, CardInstanceID: nullUUID(instance.ID),
			ConversationID: nullUUID(instance.ConversationID), AuthorizationVersion: pgtype.Int8{Int64: authorizationVersion, Valid: true}})
		if err != nil {
			return PresentationResult{}, err
		}
		result.ActionBindings[action.SlotName] = binding.BindingRef
	}
	return result, nil
}

func cardActionRequestID(presentationID uuid.UUID, slotName string) string {
	return presentationID.String() + ":" + slotName
}

func (service Service) InvokeQueryBinding(ctx context.Context, actorID, enterpriseID uuid.UUID, authorizationVersion int64, bindingRef, idempotencyKey string) (QueryBindingResult, error) {
	request := struct {
		BindingRef           string `json:"binding_ref"`
		AuthorizationVersion int64  `json:"authorization_version"`
	}{bindingRef, authorizationVersion}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID.String(), "interactive_card.query_binding.invoke", idempotencyKey, request, 200,
		func(q *db.Queries) (QueryBindingResult, error) {
			return service.invokeQueryBinding(ctx, q, actorID, enterpriseID, authorizationVersion, bindingRef)
		})
}

func (service Service) invokeQueryBinding(ctx context.Context, q *db.Queries, actorID, enterpriseID uuid.UUID, authorizationVersion int64, bindingRef string) (QueryBindingResult, error) {
	row, err := q.GetCardQueryBindingForInvoke(ctx, db.GetCardQueryBindingForInvokeParams{BindingRef: bindingRef, EnterpriseID: enterpriseID})
	if errors.Is(err, pgx.ErrNoRows) || row.Status != "active" || time.Now().UTC().After(row.ExpiresAt.Time) {
		return QueryBindingResult{}, ErrBindingExpired
	}
	if err != nil {
		return QueryBindingResult{}, err
	}
	if row.ViewerUserID != actorID || row.AuthorizationVersion != authorizationVersion {
		return QueryBindingResult{}, ErrPresentationInvalid
	}
	metadata, exists := service.Tools.Lookup(row.ToolID)
	if !exists || metadata.Risk != "read" || metadata.OutputVersion != row.OutputSchemaVersion || !hashMatches(metadata.OutputSchemaHash, row.SchemaHash) {
		return QueryBindingResult{}, ErrPresentationInvalid
	}
	var input map[string]any
	if err := json.Unmarshal(row.FixedInput, &input); err != nil {
		return QueryBindingResult{}, ErrBindingInvalid
	}
	inputHash := sha256.Sum256(row.FixedInput)
	if !slicesEqual(inputHash[:], row.InputHash) {
		return QueryBindingResult{}, ErrBindingInvalid
	}
	call := mcp.Call{ToolID: row.ToolID, Caller: "card_query_binding", Enterprise: enterpriseID.String(), Subject: actorID.String(), SubjectType: "user", Input: input}
	value, err := service.Tools.Call(ctx, call)
	if err != nil {
		return QueryBindingResult{}, err
	}
	projected, partial, err := service.Tools.ProjectForCard(ctx, call, value)
	if err != nil {
		return QueryBindingResult{}, err
	}
	if _, err := q.MarkCardQueryBindingInvoked(ctx, db.MarkCardQueryBindingInvokedParams{ID: row.ID, EnterpriseID: enterpriseID}); err != nil {
		return QueryBindingResult{}, ErrBindingExpired
	}
	return QueryBindingResult{Value: projected, Partial: partial}, nil
}

func presentationLocale(raw json.RawMessage, requested string) (string, bool, error) {
	var manifest struct {
		Supported []string `json:"supported_locales"`
		Default   string   `json:"default_locale"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || len(manifest.Supported) == 0 || manifest.Default == "" {
		return "", false, ErrBindingInvalid
	}
	for _, locale := range manifest.Supported {
		if locale == requested {
			return requested, false, nil
		}
	}
	return manifest.Default, true, nil
}

func hashMatches(encoded string, raw []byte) bool {
	decoded, err := hex.DecodeString(encoded)
	return err == nil && slicesEqual(decoded, raw)
}

func slicesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}

func invalidBinding(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBindingInvalid}, args...)...)
}
