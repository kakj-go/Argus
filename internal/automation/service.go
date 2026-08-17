package automation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/kakj-go/Argus/internal/action"
	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrUnavailable     = errors.New("automation unavailable")
	ErrVersionConflict = errors.New("automation version conflict")
	ErrOverlap         = errors.New("automation run overlaps an active run")
	ErrPolicyRequired  = errors.New("automation write requires approval policy")
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type Input struct {
	Name, ToolID, Cron, Timezone string
	ServiceAccountID             uuid.UUID
	ToolInput                    map[string]any
	ExpectedVersion              int64
}

type Service struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
	Tools       *mcp.Registry
}

func (service Service) Validate(ctx context.Context, enterpriseID uuid.UUID, input Input) (time.Time, error) {
	next, _, err := service.validate(ctx, enterpriseID, input)
	return next, err
}

func (service Service) validate(ctx context.Context, enterpriseID uuid.UUID, input Input) (time.Time, int64, error) {
	location, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return time.Time{}, 0, ErrUnavailable
	}
	schedule, err := cronParser.Parse(input.Cron)
	if err != nil {
		return time.Time{}, 0, ErrUnavailable
	}
	account, err := service.Store.Queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: input.ServiceAccountID, EnterpriseID: enterpriseID})
	if err != nil || account.Status != "active" || !slices.Contains(account.AllowedToolIds, input.ToolID) {
		return time.Time{}, 0, ErrUnavailable
	}
	metadata, ok := service.Tools.Lookup(input.ToolID)
	if !ok || metadata.Visibility != mcp.Visible || metadata.Execute == nil {
		return time.Time{}, 0, ErrUnavailable
	}
	if metadata.Validate != nil {
		if err := metadata.Validate(input.ToolInput); err != nil {
			return time.Time{}, 0, err
		}
	}
	return schedule.Next(time.Now().In(location)).UTC(), account.AuthorizationVersion, nil
}

func (service Service) Create(ctx context.Context, actorID string, enterpriseID uuid.UUID, input Input, key string) (db.Automation, error) {
	next, authorizationVersion, err := service.validate(ctx, enterpriseID, input)
	if err != nil {
		return db.Automation{}, err
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "automation.create", key, input, 201,
		func(q *db.Queries) (db.Automation, error) {
			payload, err := json.Marshal(input.ToolInput)
			if err != nil {
				return db.Automation{}, err
			}
			value, err := q.CreateAutomation(ctx, db.CreateAutomationParams{ID: newID(), EnterpriseID: enterpriseID, Name: input.Name,
				ServiceAccountID: input.ServiceAccountID, AuthorizationVersion: authorizationVersion, ToolID: input.ToolID, ToolInput: payload, Cron: input.Cron, Timezone: input.Timezone,
				NextRunAt: pgtype.Timestamptz{Time: next, Valid: true}})
			if err != nil {
				return db.Automation{}, err
			}
			_, err = q.CreateAutomationRevision(ctx, automationRevisionParams(value))
			return value, err
		})
}

func (service Service) Update(ctx context.Context, enterpriseID, id uuid.UUID, input Input) (db.Automation, error) {
	next, authorizationVersion, err := service.validate(ctx, enterpriseID, input)
	if err != nil {
		return db.Automation{}, err
	}
	payload, err := json.Marshal(input.ToolInput)
	if err != nil {
		return db.Automation{}, err
	}
	var value db.Automation
	err = service.Store.InTx(ctx, func(q *db.Queries) error {
		updated, updateErr := q.UpdateAutomation(ctx, db.UpdateAutomationParams{ID: id, EnterpriseID: enterpriseID, Name: input.Name,
			ServiceAccountID: input.ServiceAccountID, AuthorizationVersion: authorizationVersion, ToolID: input.ToolID, ToolInput: payload, Cron: input.Cron, Timezone: input.Timezone,
			NextRunAt: pgtype.Timestamptz{Time: next, Valid: true}, Version: input.ExpectedVersion})
		if updateErr != nil {
			return updateErr
		}
		if _, updateErr = q.CreateAutomationRevision(ctx, automationRevisionParams(updated)); updateErr != nil {
			return updateErr
		}
		value = updated
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Automation{}, ErrVersionConflict
	}
	return value, err
}

func automationRevisionParams(value db.Automation) db.CreateAutomationRevisionParams {
	return db.CreateAutomationRevisionParams{AutomationID: value.ID, EnterpriseID: value.EnterpriseID, Revision: value.Revision,
		ServiceAccountID: value.ServiceAccountID, AuthorizationVersion: value.AuthorizationVersion, ToolID: value.ToolID,
		ToolInput: value.ToolInput, Cron: value.Cron, Timezone: value.Timezone}
}

func (service Service) ChangeState(ctx context.Context, enterpriseID, id uuid.UUID, status string, expectedVersion int64) (db.Automation, error) {
	if status != "enabled" && status != "disabled" {
		return db.Automation{}, ErrUnavailable
	}
	value, err := service.Store.Queries.SetAutomationStatus(ctx, db.SetAutomationStatusParams{ID: id, EnterpriseID: enterpriseID, Status: status, Version: expectedVersion})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Automation{}, ErrVersionConflict
	}
	return value, err
}

func (service Service) List(ctx context.Context, enterpriseID uuid.UUID) ([]db.Automation, error) {
	return service.Store.Queries.ListAutomations(ctx, enterpriseID)
}

func (service Service) Get(ctx context.Context, enterpriseID, id uuid.UUID) (db.Automation, error) {
	return service.Store.Queries.GetAutomation(ctx, db.GetAutomationParams{ID: id, EnterpriseID: enterpriseID})
}

func (service Service) ListRuns(ctx context.Context, enterpriseID, id uuid.UUID, limit int32) ([]db.AutomationRun, error) {
	return service.Store.Queries.ListAutomationRuns(ctx, db.ListAutomationRunsParams{AutomationID: id, EnterpriseID: enterpriseID, Limit: limit})
}

type TaskPayload struct {
	RunID        uuid.UUID `json:"run_id"`
	EnterpriseID uuid.UUID `json:"enterprise_id"`
}

type Runner struct {
	Store    *postgres.Store
	Tools    *mcp.Registry
	Workflow action.Service
}

func (runner Runner) Handle(ctx context.Context, task runtime.Task) error {
	var payload TaskPayload
	if json.Unmarshal(task.Payload, &payload) != nil || payload.RunID == uuid.Nil || payload.EnterpriseID == uuid.Nil {
		return runtime.Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: ErrUnavailable, Permanent: true}
	}
	run, err := runner.Store.Queries.GetAutomationRun(ctx, db.GetAutomationRunParams{ID: payload.RunID, EnterpriseID: payload.EnterpriseID})
	if err != nil || (run.Status != "pending" && run.Status != "running") {
		return err
	}
	automation, err := runner.Store.Queries.GetAutomation(ctx, db.GetAutomationParams{ID: run.AutomationID, EnterpriseID: payload.EnterpriseID})
	if err != nil || automation.Status != "enabled" {
		return runner.fail(ctx, run, "AUTOMATION_DISABLED")
	}
	revision, err := runner.Store.Queries.GetAutomationRevision(ctx, db.GetAutomationRevisionParams{AutomationID: run.AutomationID, EnterpriseID: payload.EnterpriseID, Revision: run.AutomationRevision})
	if err != nil {
		return runner.fail(ctx, run, "CLIENT_OPERATION_UNAVAILABLE")
	}
	account, err := runner.Store.Queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: revision.ServiceAccountID, EnterpriseID: payload.EnterpriseID})
	if err != nil || account.Status != "active" || account.AuthorizationVersion != revision.AuthorizationVersion || !slices.Contains(account.AllowedToolIds, revision.ToolID) {
		return runner.fail(ctx, run, "AUTHORIZATION_VERSION_STALE")
	}
	var input map[string]any
	if json.Unmarshal(revision.ToolInput, &input) != nil {
		return runner.fail(ctx, run, "TOOL_INPUT_INVALID")
	}
	metadata, ok := runner.Tools.Lookup(revision.ToolID)
	if !ok || validateAutomationTool(revision.ToolID, metadata) != nil {
		return runner.fail(ctx, run, "CLIENT_OPERATION_UNAVAILABLE")
	}
	result, err := runner.Tools.Call(ctx, mcp.Call{ToolID: revision.ToolID, Caller: "automation", Enterprise: payload.EnterpriseID.String(),
		Subject: account.ID.String(), SubjectType: "service_account", Input: input})
	if err != nil {
		code := "TOOL_EXECUTION_FAILED"
		if errors.Is(err, mcp.ErrPermissionDenied) {
			code = "AUTHORIZATION_DENIED"
		} else if errors.Is(err, mcp.ErrInputInvalid) {
			code = "TOOL_INPUT_INVALID"
		}
		return runner.fail(ctx, run, code)
	}
	if metadata.Risk == "read" {
		content, _ := json.Marshal(result.Structured)
		if len(content) > 4<<20 {
			return runner.fail(ctx, run, "TOOL_RESULT_TOO_LARGE")
		}
		ref := "automation_result_" + newID().String()
		hash := sha256Bytes(content)
		_, err = runner.Store.Queries.CreateArtifact(ctx, db.CreateArtifactParams{ID: newID(), ResultRef: ref, EnterpriseID: payload.EnterpriseID,
			ContentType: "application/json", DataClassification: "internal", Content: content, ContentHash: hash, ByteSize: int32(len(content))})
		if err != nil {
			return err
		}
		_, err = runner.Store.Queries.UpdateAutomationRun(ctx, db.UpdateAutomationRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "succeeded", ResultRef: pgtype.Text{String: ref, Valid: true}})
		return err
	}
	actionRef, _ := result.Structured["action_ref"].(string)
	if actionRef == "" {
		return runner.fail(ctx, run, "TOOL_INPUT_INVALID")
	}
	confirmation, err := runner.Workflow.StartAutomationApproval(ctx, payload.EnterpriseID, actionRef)
	if errors.Is(err, action.ErrApprovalRequired) {
		return runner.fail(ctx, run, "APPROVAL_POLICY_REQUIRED")
	}
	if err != nil {
		return err
	}
	_, err = runner.Store.Queries.UpdateAutomationRun(ctx, db.UpdateAutomationRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "waiting_approval",
		PendingActionID: uuid.NullUUID{UUID: confirmation.PendingAction.ID, Valid: true}})
	return err
}

func validateAutomationTool(toolID string, metadata mcp.Metadata) error {
	if metadata.Visibility != mcp.Visible {
		return ErrUnavailable
	}
	if metadata.Risk != "read" && !strings.HasSuffix(toolID, ".preview") {
		return ErrUnavailable
	}
	return nil
}

func (runner Runner) fail(ctx context.Context, run db.AutomationRun, code string) error {
	_, err := runner.Store.Queries.UpdateAutomationRun(ctx, db.UpdateAutomationRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "failed",
		ErrorCode: pgtype.Text{String: code, Valid: true}})
	return err
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}
