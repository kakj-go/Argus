package card

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrNotFound              = errors.New("Card not found")
	ErrReadOnly              = errors.New("system Card is read only")
	ErrVersionConflict       = errors.New("Card version conflict")
	ErrStaticValidation      = errors.New("Card static validation failed")
	ErrRuntimeValidation     = errors.New("Card runtime validation failed")
	ErrBindingInvalid        = errors.New("Card binding invalid")
	ErrBindingExpired        = errors.New("Card binding expired")
	ErrBindingConsumed       = errors.New("Card binding consumed")
	ErrPresentationInvalid   = errors.New("Card presentation invalidated")
	ErrDependencyUnavailable = errors.New("Card dependency unavailable")
)

var requiredScenarios = []string{"default", "empty", "error", "large", "light", "dark", "zh-CN", "en-US"}

type Service struct {
	Store           *postgres.Store
	Idempotency     postgres.Idempotency
	Tools           *mcp.Registry
	PresentationTTL time.Duration
	ValidationTTL   time.Duration
	RuntimeVersion  string
	MaxPresentation int
}

type DraftInput struct {
	Slug        string                     `json:"slug"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	HTML        []byte                     `json:"html"`
	Manifest    json.RawMessage            `json:"manifest"`
	Bindings    []Binding                  `json:"bindings"`
	Demos       map[string]json.RawMessage `json:"demos"`
}

func (service Service) CreateGeneratedRevision(ctx context.Context, actorID, enterpriseID, cardID uuid.UUID, expectedRevision int32, input DraftInput) (db.InteractiveCard, db.CardVersion, error) {
	if service.Store == nil || input.Name == "" || expectedRevision < 1 {
		return db.InteractiveCard{}, db.CardVersion{}, ErrStaticValidation
	}
	var card db.InteractiveCard
	var version db.CardVersion
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		current, err := q.GetInteractiveCardForUpdate(ctx, db.GetInteractiveCardForUpdateParams{ID: cardID, EnterpriseID: nullUUID(enterpriseID)})
		if err != nil {
			return ErrNotFound
		}
		if current.Source != "enterprise" {
			return ErrReadOnly
		}
		if current.LatestRevision != expectedRevision || current.Lifecycle == "deprecated" {
			return ErrVersionConflict
		}
		revision := expectedRevision + 1
		contentHash := ContentHash(input.HTML)
		manifest, err := normalizedManifest(input.Manifest, current.ID, int(revision), current.Source, contentHash)
		if err != nil {
			return err
		}
		if issues := ValidateStatic(StaticInput{HTML: input.HTML, Manifest: manifest, Bindings: input.Bindings, Demos: input.Demos, ExpectedHash: contentHash}); len(issues) > 0 {
			return fmt.Errorf("%w: %s", ErrStaticValidation, issues[0].Code)
		}
		version, err = service.createVersion(ctx, q, current, actorID, revision, "draft", manifest, input.HTML, input.Bindings, input.Demos)
		if err != nil {
			return err
		}
		name := input.Name
		if name == "" {
			name = current.Name
		}
		description := input.Description
		card, err = q.UpdateInteractiveCardDraft(ctx, db.UpdateInteractiveCardDraftParams{ID: current.ID, EnterpriseID: nullUUID(enterpriseID), Name: name,
			Description: description, LatestRevision: revision, Version: current.Version})
		if err != nil {
			return ErrVersionConflict
		}
		return service.audit(ctx, q, actorID, enterpriseID, "interactive_card.revise", current.ID, "success", fmt.Sprint(revision))
	})
	return card, version, err
}

type ConfigurationInput struct {
	BaseRevision    int32
	ExpectedVersion int64
	Name            string
	Description     string
	Bindings        []Binding
	Demos           map[string]json.RawMessage
}

type ValidationStart struct {
	Run   db.CardValidationRun
	Nonce string
}

type ScenarioEvidence struct {
	Scenario              string
	Ready                 bool
	ProtocolViolations    int
	RuntimeErrors         int
	SeriousA11yViolations int
	MissingRequiredSlots  []string
	SizeViolation         bool
}

type ValidationEvidence struct {
	Nonce          string
	ContentHash    string
	RuntimeVersion string
	Scenarios      []ScenarioEvidence
}

type PresentationResult struct {
	Presentation   db.CardPresentation
	Instance       db.CardInstance
	Card           db.InteractiveCard
	Version        db.CardVersion
	InitialData    map[string]any
	QueryBindings  map[string]string
	ActionBindings map[string]string
	Locale         string
	LocaleFallback bool
	Partial        bool
}

func (service Service) List(ctx context.Context, enterpriseID uuid.UUID) ([]db.InteractiveCard, error) {
	return service.Store.Queries.ListInteractiveCards(ctx, nullUUID(enterpriseID))
}

func (service Service) Get(ctx context.Context, enterpriseID, cardID uuid.UUID) (db.InteractiveCard, error) {
	value, err := service.Store.Queries.GetInteractiveCard(ctx, db.GetInteractiveCardParams{ID: cardID, EnterpriseID: nullUUID(enterpriseID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.InteractiveCard{}, ErrNotFound
	}
	return value, err
}

func (service Service) ListVersions(ctx context.Context, enterpriseID, cardID uuid.UUID) ([]db.CardVersion, error) {
	return service.Store.Queries.ListCardVersions(ctx, db.ListCardVersionsParams{CardID: cardID, EnterpriseID: nullUUID(enterpriseID)})
}

func (service Service) GetVersion(ctx context.Context, enterpriseID, cardID uuid.UUID, revision int32) (db.CardVersion, error) {
	value, err := service.Store.Queries.GetCardVersion(ctx, db.GetCardVersionParams{CardID: cardID, Revision: revision, EnterpriseID: nullUUID(enterpriseID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CardVersion{}, ErrNotFound
	}
	return value, err
}

// CreateDraft is called only by the Agent command handler after the fixed Run
// model returns a schema-valid CardDraftBundle. Browsers never call it directly.
func (service Service) CreateDraft(ctx context.Context, actorID, enterpriseID uuid.UUID, input DraftInput) (db.InteractiveCard, db.CardVersion, error) {
	if service.Store == nil || input.Slug == "" || input.Name == "" {
		return db.InteractiveCard{}, db.CardVersion{}, ErrStaticValidation
	}
	contentHash := ContentHash(input.HTML)
	manifest, err := normalizedManifest(input.Manifest, uuid.Nil, 1, "enterprise", contentHash)
	if err != nil {
		return db.InteractiveCard{}, db.CardVersion{}, err
	}
	if issues := ValidateStatic(StaticInput{HTML: input.HTML, Manifest: manifest, Bindings: input.Bindings, Demos: input.Demos, ExpectedHash: contentHash}); len(issues) > 0 {
		return db.InteractiveCard{}, db.CardVersion{}, fmt.Errorf("%w: %s", ErrStaticValidation, issues[0].Code)
	}
	var card db.InteractiveCard
	var version db.CardVersion
	err = service.Store.InTx(ctx, func(q *db.Queries) error {
		cardID := newID()
		manifest, normalizeErr := normalizedManifest(manifest, cardID, 1, "enterprise", contentHash)
		if normalizeErr != nil {
			return normalizeErr
		}
		card, err = q.CreateInteractiveCard(ctx, db.CreateInteractiveCardParams{ID: cardID, EnterpriseID: nullUUID(enterpriseID), Source: "enterprise",
			Slug: input.Slug, Name: input.Name, Description: input.Description, Lifecycle: "draft", Enabled: false, Availability: "disabled", LatestRevision: 1, CreatedBy: nullUUID(actorID)})
		if err != nil {
			return err
		}
		version, err = service.createVersion(ctx, q, card, actorID, 1, "draft", manifest, input.HTML, input.Bindings, input.Demos)
		if err != nil {
			return err
		}
		return service.audit(ctx, q, actorID, enterpriseID, "interactive_card.create", card.ID, "success", "draft")
	})
	return card, version, err
}

func (service Service) CreateConfigurationVersion(ctx context.Context, actorID, enterpriseID, cardID uuid.UUID, input ConfigurationInput, idempotencyKey string) (db.CardVersion, error) {
	request := struct {
		CardID uuid.UUID          `json:"card_id"`
		Input  ConfigurationInput `json:"input"`
	}{cardID, input}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID.String(), "interactive_card.revise", idempotencyKey, request, 201,
		func(q *db.Queries) (db.CardVersion, error) {
			card, err := q.GetInteractiveCardForUpdate(ctx, db.GetInteractiveCardForUpdateParams{ID: cardID, EnterpriseID: nullUUID(enterpriseID)})
			if errors.Is(err, pgx.ErrNoRows) {
				return db.CardVersion{}, ErrNotFound
			}
			if err != nil {
				return db.CardVersion{}, err
			}
			if card.Source != "enterprise" {
				return db.CardVersion{}, ErrReadOnly
			}
			if card.Version != input.ExpectedVersion || card.Lifecycle == "deprecated" || input.BaseRevision < 1 || input.BaseRevision > card.LatestRevision {
				return db.CardVersion{}, ErrVersionConflict
			}
			base, err := q.GetCardVersion(ctx, db.GetCardVersionParams{CardID: cardID, Revision: input.BaseRevision, EnterpriseID: nullUUID(enterpriseID)})
			if err != nil {
				return db.CardVersion{}, ErrNotFound
			}
			revision := card.LatestRevision + 1
			manifest, err := normalizedManifest(base.Manifest, card.ID, int(revision), card.Source, hex.EncodeToString(base.ContentHash))
			if err != nil {
				return db.CardVersion{}, err
			}
			if issues := ValidateStatic(StaticInput{HTML: base.EntrypointHtml, Manifest: manifest, Bindings: input.Bindings, Demos: input.Demos}); len(issues) > 0 {
				return db.CardVersion{}, fmt.Errorf("%w: %s", ErrStaticValidation, issues[0].Code)
			}
			created, err := service.createVersion(ctx, q, card, actorID, revision, "draft", manifest, base.EntrypointHtml, input.Bindings, input.Demos)
			if err != nil {
				return db.CardVersion{}, err
			}
			_, err = q.UpdateInteractiveCardDraft(ctx, db.UpdateInteractiveCardDraftParams{ID: card.ID, EnterpriseID: nullUUID(enterpriseID), Name: input.Name,
				Description: input.Description, LatestRevision: revision, Version: input.ExpectedVersion})
			if err != nil {
				return db.CardVersion{}, ErrVersionConflict
			}
			if err := service.audit(ctx, q, actorID, enterpriseID, "interactive_card.revise", card.ID, "success", fmt.Sprint(revision)); err != nil {
				return db.CardVersion{}, err
			}
			return created, nil
		})
}

func (service Service) StartValidation(ctx context.Context, actorID, enterpriseID, cardID uuid.UUID, revision int32, runtimeVersion, idempotencyKey string) (ValidationStart, error) {
	if runtimeVersion == "" || service.RuntimeVersion != "" && runtimeVersion != service.RuntimeVersion {
		return ValidationStart{}, ErrRuntimeValidation
	}
	request := struct {
		CardID         uuid.UUID `json:"card_id"`
		Revision       int32     `json:"revision"`
		RuntimeVersion string    `json:"runtime_version"`
	}{cardID, revision, runtimeVersion}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID.String(), "interactive_card.validation.start", idempotencyKey, request, 201,
		func(q *db.Queries) (ValidationStart, error) {
			version, err := q.GetCardVersion(ctx, db.GetCardVersionParams{CardID: cardID, Revision: revision, EnterpriseID: nullUUID(enterpriseID)})
			if err != nil {
				return ValidationStart{}, ErrNotFound
			}
			bindings, demos, err := service.versionInputsWithQueries(ctx, q, version.ID)
			if err != nil {
				return ValidationStart{}, err
			}
			if issues := ValidateStatic(StaticInput{HTML: version.EntrypointHtml, Manifest: version.Manifest, Bindings: bindings, Demos: demos}); len(issues) > 0 {
				return ValidationStart{}, fmt.Errorf("%w: %s", ErrStaticValidation, issues[0].Code)
			}
			nonce, hash, err := randomSecret(32)
			if err != nil {
				return ValidationStart{}, err
			}
			ttl := service.ValidationTTL
			if ttl <= 0 {
				ttl = 30 * time.Minute
			}
			if version.Status != "draft" && version.Status != "validating" && version.Status != "validated" && version.Status != "retired" {
				return ValidationStart{}, ErrRuntimeValidation
			}
			if _, err := q.SetCardVersionStatus(ctx, db.SetCardVersionStatusParams{ID: version.ID, Status: "validating"}); err != nil {
				return ValidationStart{}, err
			}
			run, err := q.CreateCardValidationRun(ctx, db.CreateCardValidationRunParams{ID: newID(), CardVersionID: version.ID, EnterpriseID: enterpriseID,
				ActorUserID: actorID, ContentHash: version.ContentHash, RuntimeVersion: runtimeVersion, NonceHash: hash, RequiredScenarios: requiredScenarios,
				ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(ttl), Valid: true}})
			if err != nil {
				return ValidationStart{}, err
			}
			return ValidationStart{Run: run, Nonce: nonce}, nil
		})
}

func (service Service) SubmitEvidence(ctx context.Context, actorID, enterpriseID, runID uuid.UUID, evidence ValidationEvidence, idempotencyKey string) (db.CardValidationRun, error) {
	request := struct {
		RunID    uuid.UUID          `json:"run_id"`
		Evidence ValidationEvidence `json:"evidence"`
	}{runID, evidence}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID.String(), "interactive_card.validation.evidence", idempotencyKey, request, 200,
		func(q *db.Queries) (db.CardValidationRun, error) {
			run, err := q.GetCardValidationRunForUpdate(ctx, db.GetCardValidationRunForUpdateParams{ID: runID, EnterpriseID: enterpriseID})
			if err != nil || run.ActorUserID != actorID || run.Status != "pending" || time.Now().UTC().After(run.ExpiresAt.Time) {
				return db.CardValidationRun{}, ErrRuntimeValidation
			}
			nonceHash := sha256.Sum256([]byte(evidence.Nonce))
			if !slices.Equal(nonceHash[:], run.NonceHash) || evidence.RuntimeVersion != run.RuntimeVersion || !strings.EqualFold(evidence.ContentHash, hex.EncodeToString(run.ContentHash)) {
				return db.CardValidationRun{}, ErrRuntimeValidation
			}
			passed := make([]string, 0, len(evidence.Scenarios))
			issues := make([]ValidationIssue, 0)
			seen := map[string]bool{}
			for _, scenario := range evidence.Scenarios {
				if !slices.Contains(requiredScenarios, scenario.Scenario) || seen[scenario.Scenario] {
					return db.CardValidationRun{}, ErrRuntimeValidation
				}
				seen[scenario.Scenario] = true
				if scenario.Ready && scenario.ProtocolViolations == 0 && scenario.RuntimeErrors == 0 && scenario.SeriousA11yViolations == 0 && len(scenario.MissingRequiredSlots) == 0 && !scenario.SizeViolation {
					passed = append(passed, scenario.Scenario)
				} else {
					issues = append(issues, ValidationIssue{Code: "CARD_RUNTIME_VALIDATION_FAILED", Message: "Runtime validation failed for " + scenario.Scenario})
				}
			}
			status := "passed"
			if len(passed) != len(requiredScenarios) || len(issues) > 0 {
				status = "failed"
			}
			encodedIssues, _ := json.Marshal(issues)
			result, err := q.FinishCardValidationRun(ctx, db.FinishCardValidationRunParams{ID: run.ID, EnterpriseID: enterpriseID, Status: status, PassedScenarios: passed, Issues: encodedIssues})
			if err != nil {
				return db.CardValidationRun{}, err
			}
			versionStatus := "validated"
			if status != "passed" {
				versionStatus = "draft"
			}
			_, err = q.SetCardVersionStatus(ctx, db.SetCardVersionStatusParams{ID: run.CardVersionID, Status: versionStatus})
			if err != nil {
				return db.CardValidationRun{}, err
			}
			if err := service.audit(ctx, q, actorID, enterpriseID, "interactive_card.validate", run.CardVersionID, validationAuditResult(status), status); err != nil {
				return db.CardValidationRun{}, err
			}
			return result, nil
		})
}

func (service Service) ChangeState(ctx context.Context, actorID, enterpriseID, cardID uuid.UUID, action string, expectedVersion int64, revision *int32, idempotencyKey string) (db.InteractiveCard, error) {
	request := struct {
		CardID          uuid.UUID `json:"card_id"`
		Action          string    `json:"action"`
		ExpectedVersion int64     `json:"expected_version"`
		Revision        *int32    `json:"revision,omitempty"`
	}{cardID, action, expectedVersion, revision}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID.String(), "interactive_card.state."+action, idempotencyKey, request, 200,
		func(q *db.Queries) (db.InteractiveCard, error) {
			card, err := q.GetInteractiveCardForUpdate(ctx, db.GetInteractiveCardForUpdateParams{ID: cardID, EnterpriseID: nullUUID(enterpriseID)})
			if errors.Is(err, pgx.ErrNoRows) {
				return db.InteractiveCard{}, ErrNotFound
			}
			if err != nil {
				return db.InteractiveCard{}, err
			}
			if card.Source != "enterprise" {
				return db.InteractiveCard{}, ErrReadOnly
			}
			if card.Version != expectedVersion {
				return db.InteractiveCard{}, ErrVersionConflict
			}
			var result db.InteractiveCard
			switch action {
			case "disable":
				result, err = q.DisableInteractiveCard(ctx, db.DisableInteractiveCardParams{ID: card.ID, EnterpriseID: nullUUID(enterpriseID), Version: expectedVersion})
			case "deprecate":
				result, err = q.DeprecateInteractiveCard(ctx, db.DeprecateInteractiveCardParams{ID: card.ID, EnterpriseID: nullUUID(enterpriseID), Version: expectedVersion})
			case "activate", "rollback":
				selected := card.LatestRevision
				if revision != nil {
					selected = *revision
				}
				version, getErr := q.GetCardVersion(ctx, db.GetCardVersionParams{CardID: card.ID, Revision: selected, EnterpriseID: nullUUID(enterpriseID)})
				if getErr != nil || (version.Status != "validated" && version.Status != "retired" && version.Status != "active") {
					return db.InteractiveCard{}, ErrRuntimeValidation
				}
				validation, validationErr := q.GetLatestPassedCardValidation(ctx, db.GetLatestPassedCardValidationParams{CardVersionID: version.ID, EnterpriseID: enterpriseID})
				if validationErr != nil || !slices.Equal(validation.ContentHash, version.ContentHash) || service.RuntimeVersion != "" && validation.RuntimeVersion != service.RuntimeVersion {
					return db.InteractiveCard{}, ErrRuntimeValidation
				}
				bindings, demos, inputErr := service.versionInputsWithQueries(ctx, q, version.ID)
				if inputErr != nil {
					return db.InteractiveCard{}, inputErr
				}
				if issues := ValidateStatic(StaticInput{HTML: version.EntrypointHtml, Manifest: version.Manifest, Bindings: bindings, Demos: demos}); len(issues) > 0 {
					return db.InteractiveCard{}, ErrStaticValidation
				}
				if err = q.RetireActiveCardVersions(ctx, db.RetireActiveCardVersionsParams{CardID: card.ID, ID: version.ID}); err != nil {
					return db.InteractiveCard{}, err
				}
				if _, err = q.SetCardVersionStatus(ctx, db.SetCardVersionStatusParams{ID: version.ID, Status: "active"}); err != nil {
					return db.InteractiveCard{}, err
				}
				result, err = q.ActivateInteractiveCard(ctx, db.ActivateInteractiveCardParams{ID: card.ID, EnterpriseID: nullUUID(enterpriseID), ActiveVersionID: nullUUID(version.ID), Version: expectedVersion})
			default:
				return db.InteractiveCard{}, ErrBindingInvalid
			}
			if err != nil {
				return db.InteractiveCard{}, ErrVersionConflict
			}
			if err := service.audit(ctx, q, actorID, enterpriseID, "interactive_card."+action, card.ID, "success", action); err != nil {
				return db.InteractiveCard{}, err
			}
			return result, nil
		})
}

func (service Service) createVersion(ctx context.Context, q *db.Queries, card db.InteractiveCard, actorID uuid.UUID, revision int32, status string,
	manifest, html json.RawMessage, bindings []Binding, demos map[string]json.RawMessage) (db.CardVersion, error) {
	content := []byte(html)
	contentHash := sha256.Sum256(content)
	manifestHash := sha256.Sum256(manifest)
	version, err := q.CreateCardVersion(ctx, db.CreateCardVersionParams{ID: newID(), CardID: card.ID, Revision: revision, Status: status,
		Manifest: manifest, EntrypointHtml: content, ContentHash: contentHash[:], ManifestHash: manifestHash[:], CreatedBy: nullUUID(actorID)})
	if err != nil {
		return db.CardVersion{}, err
	}
	for _, binding := range bindings {
		schemaHash, decodeErr := hex.DecodeString(strings.TrimPrefix(binding.SchemaHash, "sha256:"))
		if decodeErr != nil || len(schemaHash) != sha256.Size {
			return db.CardVersion{}, ErrBindingInvalid
		}
		_, err = q.CreateCardSlotBinding(ctx, db.CreateCardSlotBindingParams{ID: newID(), CardVersionID: version.ID, SlotName: binding.SlotName,
			SlotKind: binding.SlotKind, Mode: binding.Mode, ToolID: binding.ToolID, OutputSchemaVersion: binding.OutputSchemaVersion, SchemaHash: schemaHash,
			FieldPath: binding.FieldPath, ValueType: binding.ValueType, SemanticType: pgtype.Text{String: binding.SemanticType, Valid: binding.SemanticType != ""}})
		if err != nil {
			return db.CardVersion{}, err
		}
	}
	for scenario, data := range demos {
		_, err = q.CreateCardDemoScenario(ctx, db.CreateCardDemoScenarioParams{ID: newID(), CardVersionID: version.ID, Scenario: scenario, Data: data, ByteSize: int32(len(data))})
		if err != nil {
			return db.CardVersion{}, err
		}
	}
	return version, nil
}

func (service Service) versionInputs(ctx context.Context, versionID uuid.UUID) ([]Binding, map[string]json.RawMessage, error) {
	return service.versionInputsWithQueries(ctx, service.Store.Queries, versionID)
}

func (service Service) versionInputsWithQueries(ctx context.Context, q *db.Queries, versionID uuid.UUID) ([]Binding, map[string]json.RawMessage, error) {
	rows, err := q.ListCardSlotBindings(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	bindings := make([]Binding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, Binding{SlotName: row.SlotName, SlotKind: row.SlotKind, Mode: row.Mode, ToolID: row.ToolID,
			OutputSchemaVersion: row.OutputSchemaVersion, SchemaHash: hex.EncodeToString(row.SchemaHash), FieldPath: row.FieldPath, ValueType: row.ValueType, SemanticType: row.SemanticType.String})
	}
	demoRows, err := q.ListCardDemoScenarios(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	demos := make(map[string]json.RawMessage, len(demoRows))
	for _, row := range demoRows {
		demos[row.Scenario] = row.Data
	}
	return bindings, demos, nil
}

func normalizedManifest(raw json.RawMessage, cardID uuid.UUID, revision int, source, hash string) (json.RawMessage, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, ErrStaticValidation
	}
	if cardID != uuid.Nil {
		value["card_id"] = cardID.String()
	}
	value["revision"] = revision
	value["source"] = source
	value["entrypoint_hash"] = strings.TrimPrefix(hash, "sha256:")
	encoded, err := json.Marshal(value)
	return encoded, err
}

func randomSecret(size int) (string, []byte, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", nil, err
	}
	plain := base64.RawURLEncoding.EncodeToString(buffer)
	hash := sha256.Sum256([]byte(plain))
	return plain, hash[:], nil
}

func validationAuditResult(status string) string {
	if status == "passed" {
		return "success"
	}
	return "failure"
}

func (service Service) audit(ctx context.Context, q *db.Queries, actorID, enterpriseID uuid.UUID, action string, resourceID uuid.UUID, result, status string) error {
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: nullUUID(enterpriseID), ActorType: "enterprise_user", ActorID: actorID.String(),
		Action: action, ResourceType: "interactive_card", ResourceID: resourceID.String(), Result: result, Details: map[string]any{"status": status}})
	return err
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

func nullUUID(value uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: value, Valid: value != uuid.Nil}
}
