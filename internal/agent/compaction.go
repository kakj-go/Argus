package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/conversation"
	"github.com/kakj-go/Argus/internal/integration/modelprovider"
	modelservice "github.com/kakj-go/Argus/internal/model"
	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type Compactor struct {
	Store          *postgres.Store
	Models         modelservice.Service
	EndpointPolicy modelprovider.PublicEndpointPolicy
}

func (compactor Compactor) Handle(ctx context.Context, task runtime.Task) error {
	var payload conversation.AgentTask
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return runtime.Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: err, Permanent: true}
	}
	run, err := compactor.Store.Queries.GetRun(ctx, db.GetRunParams{ID: payload.RunID, EnterpriseID: payload.EnterpriseID})
	if err != nil {
		return err
	}
	events, err := compactor.Store.Queries.ListRunConversationEvents(ctx, db.ListRunConversationEventsParams{RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return err
	}
	through, firstKept, ok := ChooseCompactionBoundary(events)
	if !ok {
		return compactor.fail(ctx, run, "CONTEXT_COMPACTION_FAILED")
	}
	selected := make([]db.ConversationEvent, 0)
	for _, event := range events {
		if event.Sequence <= through {
			selected = append(selected, event)
		}
	}
	source, err := json.Marshal(selected)
	if err != nil {
		return err
	}
	sourceHash := sha256.Sum256(source)
	summary, summaryTokens, err := compactor.generateSummary(ctx, run, selected)
	if err != nil {
		return compactor.fail(ctx, run, "CONTEXT_COMPACTION_FAILED")
	}
	typedCheckpoint := run.Checkpoint
	snapshotPayload, _ := json.Marshal(map[string]any{"typed_checkpoint": json.RawMessage(typedCheckpoint), "narrative_summary": summary, "source_hash": sourceHash[:]})
	snapshotHash := sha256.Sum256(snapshotPayload)
	revision, err := compactor.Store.Queries.NextContextSnapshotRevision(ctx, db.NextContextSnapshotRevisionParams{RunID: run.ID, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return err
	}
	err = compactor.Store.InTx(ctx, func(q *db.Queries) error {
		if err := q.SupersedeContextSnapshots(ctx, db.SupersedeContextSnapshotsParams{RunID: run.ID, EnterpriseID: run.EnterpriseID}); err != nil {
			return err
		}
		snapshot, createErr := q.CreateContextSnapshot(ctx, db.CreateContextSnapshotParams{ID: newAgentID(), EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID, RunID: run.ID, Revision: revision,
			SourceFromSequence: events[0].Sequence, SourceThroughSequence: through, FirstKeptSequence: firstKept, TypedCheckpoint: typedCheckpoint, NarrativeSummary: summary,
			CompactionModelID: run.ModelID, CompactionModelRevision: run.ModelRevision, PromptVersion: "argus.compaction/v1", EstimatedTokensBefore: int32((len(source) + 3) / 4),
			ActualTokensAfter: int32(summaryTokens), SourceHash: sourceHash[:], SnapshotHash: snapshotHash[:], Status: "active"})
		if errors.Is(createErr, pgx.ErrNoRows) {
			return nil
		}
		if createErr != nil {
			return createErr
		}
		if _, eventErr := conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID, RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Type: "context_compacted", ActorType: "system", Payload: map[string]any{"snapshot_id": snapshot.ID.String(), "source_through_sequence": through, "first_kept_sequence": firstKept, "estimated_tokens_before": (len(source) + 3) / 4, "actual_tokens_after": (len(snapshotPayload) + 3) / 4}, Classification: "internal"}); eventErr != nil {
			return eventErr
		}
		current, getErr := q.GetRun(ctx, db.GetRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID})
		if getErr != nil {
			return getErr
		}
		if terminalRun(current.Status) {
			return nil
		}
		updated, updateErr := q.UpdateRunStatus(ctx, db.UpdateRunStatusParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "pending", Version: current.Version})
		if updateErr != nil {
			return updateErr
		}
		payloadJSON, _ := json.Marshal(conversation.AgentTask{RunID: run.ID, EnterpriseID: run.EnterpriseID, Reason: "compaction_completed"})
		_, taskErr := q.CreateRuntimeTask(ctx, db.CreateRuntimeTaskParams{ID: newAgentID(), EnterpriseID: uuid.NullUUID{UUID: run.EnterpriseID, Valid: true}, Queue: "agent", RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Payload: payloadJSON, MaxAttempts: 5, AvailableAt: pgtype.Timestamptz{Time: updated.UpdatedAt.Time, Valid: true}})
		return taskErr
	})
	return err
}

func (compactor Compactor) generateSummary(ctx context.Context, run db.Run, events []db.ConversationEvent) (string, int, error) {
	revision, err := compactor.Store.Queries.GetEnabledAIModelRevision(ctx, db.GetEnabledAIModelRevisionParams{ModelID: run.ModelID, EnterpriseID: run.EnterpriseID, Revision: run.ModelRevision})
	if err != nil {
		return "", 0, err
	}
	credential, err := compactor.Models.LeaseCredential(ctx, run.EnterpriseID, revision.ID)
	if err != nil {
		return "", 0, err
	}
	defer clear(credential)
	safeEvents, err := json.Marshal(sanitize(events))
	if err != nil {
		return "", 0, err
	}
	sequence, err := compactor.Store.Queries.NextRunStepSequence(ctx, db.NextRunStepSequenceParams{RunID: run.ID, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return "", 0, err
	}
	step, err := compactor.Store.Queries.CreateRunStep(ctx, db.CreateRunStepParams{ID: newAgentID(), RunID: run.ID, EnterpriseID: run.EnterpriseID,
		Sequence: sequence, StepType: "context_compaction", Status: "running"})
	if err != nil {
		return "", 0, err
	}
	projectionHash := sha256.Sum256(safeEvents)
	call, err := compactor.Store.Queries.CreateModelCall(ctx, db.CreateModelCallParams{ID: newAgentID(), EnterpriseID: run.EnterpriseID, RunID: run.ID,
		StepID: step.ID, ModelID: run.ModelID, ModelRevision: run.ModelRevision, CallKind: "compaction", ProjectionHash: projectionHash[:],
		InputPriceSnapshot: revision.InputPricePerMillion, OutputPriceSnapshot: revision.OutputPricePerMillion})
	if err != nil {
		return "", 0, err
	}
	user, err := compactor.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: run.ActorUserID, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return "", 0, err
	}
	inputEstimate := int64((len(safeEvents) + 3) / 4)
	reservation, err := compactor.Models.ReserveQuota(ctx, call, user.DepartmentID, user.ID,
		tokenAmount(inputEstimate, min(int64(revision.MaxOutputTokens), 2048), revision.InputPricePerMillion, revision.OutputPricePerMillion))
	if err != nil {
		return "", 0, err
	}
	provider := modelprovider.Provider{Protocol: modelprovider.Protocol(revision.ApiProtocol), BaseURL: revision.BaseUrl,
		APIKey: string(credential), Client: compactor.EndpointPolicy.Client()}
	var output strings.Builder
	var inputTokens, outputTokens int64
	started := time.Now()
	err = provider.Stream(ctx, modelprovider.Request{Model: revision.ProviderModelID, MaxTokens: min(int(revision.MaxOutputTokens), 2048), Messages: []modelprovider.Message{
		{Role: "system", Content: "Summarize the conversation facts for future context. Do not include credentials, private plans, tokens, prompts, or hidden tool data."},
		{Role: "user", Content: string(safeEvents)},
	}}, func(event modelprovider.Event) error {
		if event.Type == "text_delta" {
			output.WriteString(event.Text)
		}
		if event.Type == "usage" || event.Type == "completed" {
			if event.Input > 0 {
				inputTokens = event.Input
			}
			if event.Output > 0 {
				outputTokens = event.Output
			}
		}
		return nil
	})
	if inputTokens == 0 {
		inputTokens = inputEstimate
	}
	if outputTokens == 0 {
		outputTokens = int64((output.Len() + 3) / 4)
	}
	amount := tokenAmount(inputTokens, outputTokens, revision.InputPricePerMillion, revision.OutputPricePerMillion)
	status, code := "succeeded", ""
	if err != nil || strings.TrimSpace(output.String()) == "" {
		status, code = "failed", "CONTEXT_COMPACTION_FAILED"
	}
	_, finishErr := compactor.Store.Queries.FinishModelCall(ctx, db.FinishModelCallParams{ID: call.ID, EnterpriseID: call.EnterpriseID,
		InputTokens: inputTokens, OutputTokens: outputTokens, Amount: amount, LatencyMs: time.Since(started).Milliseconds(),
		StopReason: pgtype.Text{String: "compaction", Valid: true}, Status: status, ErrorCode: pgtype.Text{String: code, Valid: code != ""}})
	_ = compactor.Models.SettleQuota(ctx, reservation, amount)
	if err != nil {
		return "", 0, err
	}
	if finishErr != nil {
		return "", 0, finishErr
	}
	if _, err = compactor.Store.Queries.FinishRunStep(ctx, db.FinishRunStepParams{ID: step.ID, EnterpriseID: run.EnterpriseID, Status: status}); err != nil {
		return "", 0, err
	}
	summary := strings.TrimSpace(output.String())
	if summary == "" {
		return "", 0, fmt.Errorf("empty compaction summary")
	}
	return summary, int(outputTokens), nil
}

func ChooseCompactionBoundary(events []db.ConversationEvent) (through, firstKept int64, ok bool) {
	if len(events) < 4 {
		return 0, 0, false
	}
	target := len(events) / 2
	for index := target; index >= 0; index-- {
		event := events[index]
		if safeBoundary(event.EventType) {
			if index+1 >= len(events) {
				continue
			}
			return event.Sequence, events[index+1].Sequence, true
		}
	}
	return 0, 0, false
}

func safeBoundary(eventType string) bool {
	switch eventType {
	case "assistant_message", "model_usage", "run_state_changed", "context_compacted":
		return true
	default:
		return false
	}
}

func deterministicSummary(events []db.ConversationEvent) string {
	var output strings.Builder
	for _, event := range events {
		if event.EventType != "user_message" && event.EventType != "assistant_message" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		content, _ := payload["content"].(string)
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		if output.Len() > 0 {
			output.WriteByte('\n')
		}
		output.WriteString(event.EventType)
		output.WriteString(": ")
		output.WriteString(content)
		if output.Len() > 8192 {
			break
		}
	}
	value := output.String()
	if len(value) > 8192 {
		value = value[:8192]
	}
	return value
}

func (compactor Compactor) fail(ctx context.Context, run db.Run, errorCode string) error {
	current, err := compactor.Store.Queries.GetRun(ctx, db.GetRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return err
	}
	_, err = compactor.Store.Queries.UpdateRunStatus(ctx, db.UpdateRunStatusParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "waiting_system", StopReason: pgtype.Text{String: "context_compaction_failed", Valid: true}, ErrorCode: pgtype.Text{String: errorCode, Valid: true}, Version: current.Version})
	return err
}
