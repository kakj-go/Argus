// Package conversation owns immutable conversation events and recoverable runs.
package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrRunAlreadyActive   = errors.New("conversation already has an active run")
	ErrRunNotCancellable  = errors.New("run is not cancellable")
	ErrConversationClosed = errors.New("conversation is archived")
	ErrModelUnavailable   = errors.New("selected model is unavailable")
)

type Service struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
}

type MessageAccepted struct {
	Event db.ConversationEvent `json:"event"`
	Run   db.Run               `json:"run"`
}

type AgentTask struct {
	RunID        uuid.UUID       `json:"run_id"`
	EnterpriseID uuid.UUID       `json:"enterprise_id"`
	Reason       string          `json:"reason"`
	Command      *MessageCommand `json:"command,omitempty"`
	ExecutionRef string          `json:"execution_ref,omitempty"`
	ActionRef    string          `json:"action_ref,omitempty"`
}

type MessageCommand struct {
	Type             string     `json:"type"`
	CardID           *uuid.UUID `json:"card_id,omitempty"`
	ExpectedRevision *int32     `json:"expected_revision,omitempty"`
}

type ToolResultView struct {
	Artifact    db.Artifact
	Result      db.GetToolResultByArtifactRow
	ContentHash string
}

func (service Service) List(ctx context.Context, enterpriseID, ownerID uuid.UUID, limit int32) ([]db.Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return service.Store.Queries.ListConversations(ctx, db.ListConversationsParams{EnterpriseID: enterpriseID, OwnerUserID: ownerID, Limit: limit})
}

func (service Service) Get(ctx context.Context, enterpriseID, ownerID, conversationID uuid.UUID) (db.Conversation, error) {
	return service.Store.Queries.GetConversation(ctx, db.GetConversationParams{ID: conversationID, EnterpriseID: enterpriseID, OwnerUserID: ownerID})
}

func (service Service) Create(ctx context.Context, actorID string, enterpriseID, ownerID, modelID uuid.UUID, title, idempotencyKey string) (db.Conversation, error) {
	if title == "" {
		title = "New conversation"
	}
	input := struct {
		ModelID uuid.UUID `json:"model_id"`
		Title   string    `json:"title"`
	}{modelID, title}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "conversation.create", idempotencyKey, input, 201,
		func(q *db.Queries) (db.Conversation, error) {
			model, err := q.GetAIModel(ctx, db.GetAIModelParams{ID: modelID, EnterpriseID: enterpriseID})
			if err != nil || model.Status != "enabled" || model.HealthStatus != "healthy" {
				return db.Conversation{}, ErrModelUnavailable
			}
			return q.CreateConversation(ctx, db.CreateConversationParams{ID: newID(), EnterpriseID: enterpriseID,
				OwnerUserID: ownerID, Title: title, SelectedModelID: modelID})
		})
}

func (service Service) Update(ctx context.Context, enterpriseID, ownerID, conversationID uuid.UUID, expectedVersion int64, title, status *string, modelID *uuid.UUID) (db.Conversation, error) {
	params := db.UpdateConversationParams{ID: conversationID, EnterpriseID: enterpriseID, OwnerUserID: ownerID, ExpectedVersion: expectedVersion}
	if title != nil {
		params.Title = pgtype.Text{String: *title, Valid: true}
	}
	if status != nil {
		params.Status = pgtype.Text{String: *status, Valid: true}
	}
	if modelID != nil {
		model, err := service.Store.Queries.GetAIModel(ctx, db.GetAIModelParams{ID: *modelID, EnterpriseID: enterpriseID})
		if err != nil || model.Status != "enabled" || model.HealthStatus != "healthy" {
			return db.Conversation{}, ErrModelUnavailable
		}
		params.SelectedModelID = uuid.NullUUID{UUID: *modelID, Valid: true}
	}
	return service.Store.Queries.UpdateConversation(ctx, params)
}

func (service Service) AddMessage(ctx context.Context, actorID string, enterpriseID, ownerID, conversationID uuid.UUID, authorizationVersion int64, locale, content string, command *MessageCommand, idempotencyKey string) (MessageAccepted, error) {
	input := struct {
		ConversationID uuid.UUID       `json:"conversation_id"`
		Content        string          `json:"content"`
		Command        *MessageCommand `json:"command,omitempty"`
	}{conversationID, content, command}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "conversation.message.create", idempotencyKey, input, 202,
		func(q *db.Queries) (MessageAccepted, error) {
			conversation, err := q.LockConversation(ctx, db.LockConversationParams{ID: conversationID, EnterpriseID: enterpriseID, OwnerUserID: ownerID})
			if err != nil {
				return MessageAccepted{}, err
			}
			if conversation.Status != "active" {
				return MessageAccepted{}, ErrConversationClosed
			}
			if _, err := q.GetActiveRunForConversation(ctx, db.GetActiveRunForConversationParams{ConversationID: conversationID, EnterpriseID: enterpriseID}); err == nil {
				return MessageAccepted{}, ErrRunAlreadyActive
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return MessageAccepted{}, err
			}
			model, err := q.GetAIModel(ctx, db.GetAIModelParams{ID: conversation.SelectedModelID, EnterpriseID: enterpriseID})
			if err != nil || model.Status != "enabled" || model.HealthStatus != "healthy" {
				return MessageAccepted{}, ErrModelUnavailable
			}
			checkpoint, _ := json.Marshal(map[string]any{"schema_version": "argus.run_checkpoint/v1", "goal": content,
				"authorization_version": authorizationVersion, "completed_step_ids": []string{}, "tool_call_refs": []string{},
				"active_public_pending_action_refs": []string{}, "execution_refs": []string{}})
			run, err := q.CreateRun(ctx, db.CreateRunParams{ID: newID(), ConversationID: conversationID, EnterpriseID: enterpriseID,
				ActorUserID: ownerID, ModelID: model.ID, ModelRevision: int32(model.Revision), Locale: locale,
				AuthorizationVersion: authorizationVersion, Checkpoint: checkpoint})
			if err != nil {
				return MessageAccepted{}, err
			}
			messagePayload := map[string]any{"content": content}
			if command != nil {
				messagePayload["command"] = command
			}
			event, err := AppendEvent(ctx, q, EventInput{EnterpriseID: enterpriseID, ConversationID: conversationID,
				RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Type: "user_message", ActorType: "user", ActorID: actorID,
				Payload: messagePayload, Classification: "internal"})
			if err != nil {
				return MessageAccepted{}, err
			}
			payload, _ := json.Marshal(AgentTask{RunID: run.ID, EnterpriseID: enterpriseID, Reason: "user_message", Command: command})
			if _, err := q.CreateRuntimeTask(ctx, db.CreateRuntimeTaskParams{ID: newID(), EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true},
				Queue: "agent", RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Payload: payload, MaxAttempts: 5,
				AvailableAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}); err != nil {
				return MessageAccepted{}, err
			}
			return MessageAccepted{Event: event, Run: run}, nil
		})
}

func (service Service) ListEvents(ctx context.Context, enterpriseID, ownerID, conversationID uuid.UUID, after int64, limit int32) ([]db.ConversationEvent, error) {
	if _, err := service.Get(ctx, enterpriseID, ownerID, conversationID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	return service.Store.Queries.ListConversationEvents(ctx, db.ListConversationEventsParams{ConversationID: conversationID,
		EnterpriseID: enterpriseID, Sequence: after, Limit: limit})
}

func (service Service) GetRun(ctx context.Context, enterpriseID, runID uuid.UUID) (db.Run, error) {
	return service.Store.Queries.GetRun(ctx, db.GetRunParams{ID: runID, EnterpriseID: enterpriseID})
}

func (service Service) CancelRun(ctx context.Context, actorID string, enterpriseID, runID uuid.UUID, idempotencyKey string) (db.Run, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "run.cancel", idempotencyKey, runID, 200,
		func(q *db.Queries) (db.Run, error) {
			run, err := q.GetRun(ctx, db.GetRunParams{ID: runID, EnterpriseID: enterpriseID})
			if err != nil {
				return db.Run{}, err
			}
			if run.Status != "pending" && run.Status != "running" && run.Status != "waiting_input" && run.Status != "waiting_approval" && run.Status != "waiting_system" {
				return db.Run{}, ErrRunNotCancellable
			}
			return q.UpdateRunStatus(ctx, db.UpdateRunStatusParams{ID: run.ID, EnterpriseID: enterpriseID, Status: "cancelled",
				StopReason: pgtype.Text{String: "user_cancelled", Valid: true}, Version: run.Version})
		})
}

func (service Service) RequestCompaction(ctx context.Context, actorID string, enterpriseID, runID uuid.UUID, idempotencyKey string) (db.Run, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "run.compact", idempotencyKey, runID, 202,
		func(q *db.Queries) (db.Run, error) {
			run, err := q.GetRun(ctx, db.GetRunParams{ID: runID, EnterpriseID: enterpriseID})
			if err != nil {
				return db.Run{}, err
			}
			payload, _ := json.Marshal(AgentTask{RunID: run.ID, EnterpriseID: enterpriseID, Reason: "manual"})
			_, err = q.CreateRuntimeTask(ctx, db.CreateRuntimeTaskParams{ID: newID(), EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true},
				Queue: "compaction", RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Payload: payload, MaxAttempts: 3,
				AvailableAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}})
			return run, err
		})
}

func (service Service) GetToolResult(ctx context.Context, enterpriseID, ownerID uuid.UUID, resultRef string) (ToolResultView, error) {
	artifact, err := service.Store.Queries.GetArtifactByRef(ctx, db.GetArtifactByRefParams{ResultRef: resultRef, EnterpriseID: enterpriseID})
	if err != nil {
		return ToolResultView{}, err
	}
	if !artifact.ConversationID.Valid {
		return ToolResultView{}, pgx.ErrNoRows
	}
	if _, err := service.Get(ctx, enterpriseID, ownerID, artifact.ConversationID.UUID); err != nil {
		return ToolResultView{}, err
	}
	result, err := service.Store.Queries.GetToolResultByArtifact(ctx, db.GetToolResultByArtifactParams{ArtifactID: artifact.ID, EnterpriseID: enterpriseID})
	return ToolResultView{Artifact: artifact, Result: result, ContentHash: hex.EncodeToString(artifact.ContentHash)}, err
}

type EventInput struct {
	EnterpriseID   uuid.UUID
	ConversationID uuid.UUID
	RunID          uuid.NullUUID
	StepID         uuid.NullUUID
	Type           string
	ActorType      string
	ActorID        string
	Payload        any
	ArtifactRef    string
	Classification string
}

// AppendEvent appends one immutable fact. Callers must serialize appends by
// locking the conversation or otherwise owning the active Run task.
func AppendEvent(ctx context.Context, q *db.Queries, input EventInput) (db.ConversationEvent, error) {
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return db.ConversationEvent{}, err
	}
	hash := sha256.Sum256(payload)
	sequence, err := q.NextConversationSequence(ctx, input.ConversationID)
	if err != nil {
		return db.ConversationEvent{}, err
	}
	return q.CreateConversationEvent(ctx, db.CreateConversationEventParams{ID: newID(), EnterpriseID: input.EnterpriseID,
		ConversationID: input.ConversationID, RunID: input.RunID, StepID: input.StepID, Sequence: int64(sequence), EventType: input.Type,
		ActorType: input.ActorType, ActorID: pgtype.Text{String: input.ActorID, Valid: input.ActorID != ""}, Payload: payload,
		ContentHash: hash[:], ArtifactRef: pgtype.Text{String: input.ArtifactRef, Valid: input.ArtifactRef != ""},
		DataClassification: input.Classification})
}

func ParseSequence(value string) int64 {
	sequence, _ := strconv.ParseInt(value, 10, 64)
	return max(sequence, 0)
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}
