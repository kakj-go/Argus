package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	"github.com/kakj-go/Argus/internal/conversation"
	"github.com/kakj-go/Argus/internal/integration/modelprovider"
	"github.com/kakj-go/Argus/internal/mcp"
	modelservice "github.com/kakj-go/Argus/internal/model"
	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	maxAgentTurns      = 8
	maxToolResultBytes = 4 << 20
	maxProjectionBytes = 64 << 10
)

type Loop struct {
	Store          *postgres.Store
	Models         modelservice.Service
	Tools          *mcp.Registry
	EndpointPolicy modelprovider.PublicEndpointPolicy
}

type pendingToolCall struct{ ID, Name, Arguments string }

type toolExecutionResult struct {
	message   string
	actionRef string
}

func (loop Loop) Handle(ctx context.Context, task runtime.Task) error {
	var payload conversation.AgentTask
	if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.RunID == uuid.Nil || payload.EnterpriseID == uuid.Nil {
		return runtime.Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: errors.New("invalid agent task"), Permanent: true}
	}
	run, err := loop.Store.Queries.GetRun(ctx, db.GetRunParams{ID: payload.RunID, EnterpriseID: payload.EnterpriseID})
	if err != nil {
		return err
	}
	if terminalRun(run.Status) {
		return nil
	}
	user, err := loop.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: run.ActorUserID, EnterpriseID: run.EnterpriseID})
	if err != nil || user.Status != "active" || user.AuthorizationVersion != run.AuthorizationVersion {
		return loop.finishRun(ctx, run, "failed", "authorization_invalidated", "AUTHORIZATION_VERSION_STALE")
	}
	revision, err := loop.Store.Queries.GetEnabledAIModelRevision(ctx, db.GetEnabledAIModelRevisionParams{ModelID: run.ModelID, EnterpriseID: run.EnterpriseID, Revision: run.ModelRevision})
	if err != nil {
		return loop.finishRun(ctx, run, "failed", "model_unavailable", "MODEL_COMPATIBILITY_FAILED")
	}
	credential, err := loop.Models.LeaseCredential(ctx, run.EnterpriseID, revision.ID)
	if err != nil {
		return err
	}
	defer clear(credential)

	provider := modelprovider.Provider{Protocol: modelprovider.Protocol(revision.ApiProtocol), BaseURL: revision.BaseUrl,
		APIKey: string(credential), Client: loop.EndpointPolicy.Client()}
	messages, currentInput, err := loop.messages(ctx, run)
	if err != nil {
		return err
	}
	if payload.Reason == "execution_verify" {
		verification, verifyErr := loop.executionVerification(ctx, run, payload)
		if verifyErr != nil {
			return verifyErr
		}
		messages = append(messages, modelprovider.Message{Role: "user", Content: verification})
		currentInput = verification
	}
	for turn := 0; turn < maxAgentTurns; turn++ {
		if cancelled, err := loop.cancelled(ctx, run); err != nil || cancelled {
			return err
		}
		projection, err := loop.context(ctx, run, revision, messages, currentInput)
		if err != nil {
			return loop.waitForCompaction(ctx, run, "CONTEXT_TOO_LARGE")
		}
		if projection.NeedsHardCompaction() {
			return loop.waitForCompaction(ctx, run, "")
		}
		if projection.NeedsSoftCompaction() {
			_ = loop.enqueueCompaction(ctx, run, "soft_limit")
		}

		step, updatedRun, err := loop.startStep(ctx, run, projection)
		if err != nil {
			return err
		}
		run = updatedRun
		modelCall, err := loop.createModelCall(ctx, run, step, revision, projection)
		if err != nil {
			_ = loop.failStep(ctx, run, step)
			return err
		}
		reservedAmount := tokenAmount(int64(projection.EstimatedTokens), int64(revision.MaxOutputTokens), revision.InputPricePerMillion, revision.OutputPricePerMillion)
		reservation, err := loop.Models.ReserveQuota(ctx, modelCall, user.DepartmentID, user.ID, reservedAmount)
		if err != nil {
			_ = loop.finishModelCall(ctx, modelCall, revision, 0, 0, 0, "quota_exceeded", "failed", "MODEL_QUOTA_EXCEEDED")
			_ = loop.failStep(ctx, run, step)
			return loop.finishRun(ctx, run, "failed", "quota_exceeded", "MODEL_QUOTA_EXCEEDED")
		}
		request := modelprovider.Request{Model: revision.ProviderModelID, Messages: messages, Tools: loop.modelTools(), MaxTokens: int(revision.MaxOutputTokens)}
		started := time.Now()
		var text strings.Builder
		var delta strings.Builder
		lastDeltaFlush := time.Now()
		flushDelta := func() error {
			if delta.Len() == 0 {
				return nil
			}
			value := delta.String()
			delta.Reset()
			lastDeltaFlush = time.Now()
			return loop.persistDelta(ctx, run, step, value)
		}
		calls := map[string]*pendingToolCall{}
		var inputTokens, outputTokens int64
		stopReason := "completed"
		err = provider.Stream(ctx, request, func(event modelprovider.Event) error {
			if cancelled, checkErr := loop.cancelled(ctx, run); checkErr != nil || cancelled {
				if checkErr != nil {
					return checkErr
				}
				return context.Canceled
			}
			switch event.Type {
			case "text_delta":
				text.WriteString(event.Text)
				delta.WriteString(event.Text)
				if delta.Len() >= 8<<10 || time.Since(lastDeltaFlush) >= 250*time.Millisecond {
					return flushDelta()
				}
			case "tool_call_delta", "tool_call_done":
				key := event.ToolCallID
				if key == "" {
					key = fmt.Sprintf("call_%d", len(calls)+1)
				}
				call := calls[key]
				if call == nil {
					call = &pendingToolCall{ID: key}
					calls[key] = call
				}
				if event.ToolName != "" {
					call.Name = event.ToolName
				}
				if event.Type == "tool_call_done" {
					call.Arguments = event.Arguments
				} else {
					call.Arguments += event.Arguments
				}
			case "usage":
				inputTokens += event.Input
				outputTokens += event.Output
			case "completed":
				if event.StopReason != "" {
					stopReason = event.StopReason
				}
				if event.Input > 0 {
					inputTokens = event.Input
				}
				if event.Output > 0 {
					outputTokens = event.Output
				}
			}
			return nil
		})
		if flushErr := flushDelta(); err == nil && flushErr != nil {
			err = flushErr
		}
		if err != nil {
			_ = loop.finishModelCall(ctx, modelCall, revision, inputTokens, outputTokens, time.Since(started), stopReason, "failed", "MODEL_COMPATIBILITY_FAILED")
			_ = loop.Models.SettleQuota(ctx, reservation, tokenAmount(inputTokens, outputTokens, revision.InputPricePerMillion, revision.OutputPricePerMillion))
			_ = loop.failStep(ctx, run, step)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return runtime.Error{ErrorCode: "MODEL_COMPATIBILITY_FAILED", Cause: err}
		}
		if inputTokens == 0 {
			inputTokens = int64(projection.EstimatedTokens)
		}
		if outputTokens == 0 {
			outputTokens = int64((text.Len() + 3) / 4)
		}
		if err := loop.finishModelCall(ctx, modelCall, revision, inputTokens, outputTokens, time.Since(started), stopReason, "succeeded", ""); err != nil {
			_ = loop.failStep(ctx, run, step)
			return err
		}
		if err := loop.Models.SettleQuota(ctx, reservation, tokenAmount(inputTokens, outputTokens, revision.InputPricePerMillion, revision.OutputPricePerMillion)); err != nil {
			_ = loop.failStep(ctx, run, step)
			return err
		}
		if len(calls) == 0 {
			if err := loop.persistAssistant(ctx, run, step, text.String(), inputTokens, outputTokens); err != nil {
				return err
			}
			return loop.finishRun(ctx, run, "succeeded", stopReason, "")
		}
		if !allowsToolExecution(stopReason) {
			if _, finishErr := loop.Store.Queries.FinishRunStep(ctx, db.FinishRunStepParams{ID: step.ID, EnterpriseID: run.EnterpriseID, Status: "failed"}); finishErr != nil {
				return finishErr
			}
			return loop.finishRun(ctx, run, "failed", "model_output_incomplete", "TOOL_INPUT_INVALID")
		}
		ordered := orderedCalls(calls)
		toolResults, err := loop.executeToolCalls(ctx, run, step, ordered)
		if err != nil {
			_ = loop.failStep(ctx, run, step)
			return err
		}
		for index, call := range ordered {
			resultMessage := toolResults[index].message
			messages = append(messages, modelprovider.Message{Role: "assistant", Content: "Tool call: " + call.Name + " " + call.Arguments}, modelprovider.Message{Role: "user", Content: resultMessage})
		}
		if _, err := loop.Store.Queries.FinishRunStep(ctx, db.FinishRunStepParams{ID: step.ID, EnterpriseID: run.EnterpriseID, Status: "succeeded"}); err != nil {
			return err
		}
		if actionRef := firstActionRef(toolResults); actionRef != "" {
			return loop.waitForAction(ctx, run, actionRef)
		}
	}
	return loop.finishRun(ctx, run, "failed", "output_limit", "RUN_STEP_LIMIT_REACHED")
}

func (loop Loop) HandleExhausted(ctx context.Context, task runtime.Task, cause error) error {
	var payload conversation.AgentTask
	if err := json.Unmarshal(task.Payload, &payload); err != nil || payload.RunID == uuid.Nil || payload.EnterpriseID == uuid.Nil {
		return nil
	}
	run, err := loop.Store.Queries.GetRun(ctx, db.GetRunParams{ID: payload.RunID, EnterpriseID: payload.EnterpriseID})
	if err != nil || terminalRun(run.Status) {
		return err
	}
	errorCode := "TASK_FAILED"
	type coded interface{ Code() string }
	var value coded
	if errors.As(cause, &value) && value.Code() != "" {
		errorCode = value.Code()
	}
	if run.CurrentStepID.Valid {
		_, _ = loop.Store.Queries.FinishRunStep(ctx, db.FinishRunStepParams{ID: run.CurrentStepID.UUID, EnterpriseID: run.EnterpriseID, Status: "failed"})
	}
	return loop.finishRun(ctx, run, "failed", "worker_attempts_exhausted", errorCode)
}

func (loop Loop) executeToolCalls(ctx context.Context, run db.Run, step db.RunStep, calls []*pendingToolCall) ([]toolExecutionResult, error) {
	results := make([]toolExecutionResult, len(calls))
	if !loop.parallelSafe(calls) {
		for index, call := range calls {
			value, err := loop.executeTool(ctx, run, step, *call)
			if err != nil {
				return nil, err
			}
			results[index] = value
		}
		return results, nil
	}
	group, groupCtx := errgroup.WithContext(ctx)
	for index, call := range calls {
		index, call := index, call
		group.Go(func() error {
			value, err := loop.executeTool(groupCtx, run, step, *call)
			if err == nil {
				results[index] = value
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func (loop Loop) parallelSafe(calls []*pendingToolCall) bool {
	if len(calls) < 2 || loop.Tools == nil {
		return false
	}
	for _, call := range calls {
		metadata, ok := loop.Tools.Lookup(call.Name)
		if !ok || metadata.Risk != "read" || metadata.ExecutionMode != mcp.ParallelSafe {
			return false
		}
	}
	return true
}

func (loop Loop) messages(ctx context.Context, run db.Run) ([]modelprovider.Message, string, error) {
	events, err := loop.Store.Queries.ListRunConversationEvents(ctx, db.ListRunConversationEventsParams{RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return nil, "", err
	}
	messages := []modelprovider.Message{{Role: "system", Content: "You are Argus. Use only governed tools. Never invent action confirmation or commit capabilities."}}
	current := ""
	for _, event := range events {
		var payload map[string]any
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		content, _ := payload["content"].(string)
		switch event.EventType {
		case "user_message":
			messages = append(messages, modelprovider.Message{Role: "user", Content: content})
			current = content
		case "assistant_message":
			messages = append(messages, modelprovider.Message{Role: "assistant", Content: content})
		case "tool_call_result":
			encoded, _ := json.Marshal(payload["projection"])
			messages = append(messages, modelprovider.Message{Role: "user", Content: "Tool result: " + string(encoded)})
		}
	}
	return messages, current, nil
}

func (loop Loop) executionVerification(ctx context.Context, run db.Run, task conversation.AgentTask) (string, error) {
	if task.ActionRef == "" || task.ExecutionRef == "" {
		return "", runtime.Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: errors.New("verify task is incomplete"), Permanent: true}
	}
	action, err := loop.Store.Queries.GetPendingAction(ctx, db.GetPendingActionParams{ActionRef: task.ActionRef, EnterpriseID: run.EnterpriseID})
	if err != nil || !action.RunID.Valid || action.RunID.UUID != run.ID {
		return "", runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: errors.New("verify action is not bound to run"), Permanent: true}
	}
	execution, err := loop.Store.Queries.GetExecutionByAction(ctx, db.GetExecutionByActionParams{ActionRef: task.ActionRef, EnterpriseID: run.EnterpriseID})
	if err != nil || execution.ExecutionRef != task.ExecutionRef || !execution.RunID.Valid || execution.RunID.UUID != run.ID {
		return "", runtime.Error{ErrorCode: "ACTION_INVALIDATED", Cause: errors.New("verify execution is not bound to run"), Permanent: true}
	}
	projection, _ := json.Marshal(map[string]any{"schema_version": "argus.execution_verification/v1", "action_ref": action.ActionRef,
		"action_status": action.Status, "execution_ref": execution.ExecutionRef, "execution_status": execution.Status,
		"resource_type": action.ResultResourceType, "resource_id": action.ResultResourceID, "resource_version": action.ResultResourceVersion,
		"result_summary": action.ResultSummary, "error_code": action.ErrorCode})
	return "Verify this deterministic execution result and report it to the user: " + string(projection), nil
}

func (loop Loop) context(ctx context.Context, run db.Run, revision db.AiModelRevision, messages []modelprovider.Message, current string) (ContextProjection, error) {
	var checkpoint any
	_ = json.Unmarshal(run.Checkpoint, &checkpoint)
	var snapshot any
	active, err := loop.Store.Queries.GetActiveContextSnapshot(ctx, db.GetActiveContextSnapshotParams{RunID: run.ID, EnterpriseID: run.EnterpriseID})
	if err == nil {
		snapshot = map[string]any{"typed_checkpoint": json.RawMessage(active.TypedCheckpoint), "narrative_summary": active.NarrativeSummary, "source_hash": hex.EncodeToString(active.SourceHash)}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ContextProjection{}, err
	}
	return AssembleContext(ContextInput{ContextWindow: int(revision.ContextWindowTokens), MaxOutput: int(revision.MaxOutputTokens), System: messages[0], ToolCatalog: loop.modelTools(), Checkpoint: checkpoint, Snapshot: snapshot, RecentTail: messages[1:], CurrentInput: current})
}

func (loop Loop) startStep(ctx context.Context, run db.Run, projection ContextProjection) (db.RunStep, db.Run, error) {
	sequence, err := loop.Store.Queries.NextRunStepSequence(ctx, db.NextRunStepSequenceParams{RunID: run.ID, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return db.RunStep{}, db.Run{}, err
	}
	step, err := loop.Store.Queries.CreateRunStep(ctx, db.CreateRunStepParams{ID: newAgentID(), RunID: run.ID, EnterpriseID: run.EnterpriseID, Sequence: sequence, StepType: "model_call", Status: "running"})
	if err != nil {
		return db.RunStep{}, db.Run{}, err
	}
	updated, err := loop.Store.Queries.SetRunCurrentStep(ctx, db.SetRunCurrentStepParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "running", CurrentStepID: uuid.NullUUID{UUID: step.ID, Valid: true}, Version: run.Version})
	return step, updated, err
}

func (loop Loop) createModelCall(ctx context.Context, run db.Run, step db.RunStep, revision db.AiModelRevision, projection ContextProjection) (db.ModelCall, error) {
	hash, _ := hex.DecodeString(projection.Hash)
	return loop.Store.Queries.CreateModelCall(ctx, db.CreateModelCallParams{ID: newAgentID(), EnterpriseID: run.EnterpriseID, RunID: run.ID, StepID: step.ID, ModelID: run.ModelID, ModelRevision: run.ModelRevision, CallKind: "inference", ProjectionHash: hash, InputPriceSnapshot: revision.InputPricePerMillion, OutputPriceSnapshot: revision.OutputPricePerMillion})
}

func (loop Loop) finishModelCall(ctx context.Context, call db.ModelCall, revision db.AiModelRevision, input, output int64, latency time.Duration, reason, status, errorCode string) error {
	amount := tokenAmount(input, output, revision.InputPricePerMillion, revision.OutputPricePerMillion)
	_, err := loop.Store.Queries.FinishModelCall(ctx, db.FinishModelCallParams{ID: call.ID, EnterpriseID: call.EnterpriseID, InputTokens: input, OutputTokens: output, Amount: amount, LatencyMs: latency.Milliseconds(), StopReason: pgtype.Text{String: reason, Valid: reason != ""}, Status: status, ErrorCode: pgtype.Text{String: errorCode, Valid: errorCode != ""}})
	return err
}

func (loop Loop) failStep(ctx context.Context, run db.Run, step db.RunStep) error {
	_, err := loop.Store.Queries.FinishRunStep(ctx, db.FinishRunStepParams{ID: step.ID, EnterpriseID: run.EnterpriseID, Status: "failed"})
	return err
}

func (loop Loop) executeTool(ctx context.Context, run db.Run, step db.RunStep, call pendingToolCall) (toolExecutionResult, error) {
	var input map[string]any
	if call.Name == "" || json.Unmarshal([]byte(call.Arguments), &input) != nil {
		return toolExecutionResult{}, runtime.Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: errors.New("incomplete tool call"), Permanent: true}
	}
	encoded, _ := json.Marshal(input)
	inputHash := sha256.Sum256(encoded)
	record, err := loop.Store.Queries.CreateToolCall(ctx, db.CreateToolCallParams{ID: newAgentID(), CallID: call.ID, EnterpriseID: run.EnterpriseID, RunID: run.ID, StepID: step.ID, ToolID: call.Name, Input: encoded, InputHash: inputHash[:], Status: "running"})
	if err != nil {
		return toolExecutionResult{}, err
	}
	metadata, available := loop.Tools.Lookup(call.Name)
	if !available || metadata.Visibility != mcp.Visible {
		_, _ = loop.Store.Queries.FinishToolCall(ctx, db.FinishToolCallParams{ID: record.ID, Status: "failed", ErrorCode: pgtype.Text{String: "CLIENT_OPERATION_UNAVAILABLE", Valid: true}})
		return toolExecutionResult{}, runtime.Error{ErrorCode: "CLIENT_OPERATION_UNAVAILABLE", Cause: mcp.ErrToolNotAvailable, Permanent: true}
	}
	result, err := loop.Tools.Call(ctx, mcp.Call{ToolID: call.Name, Caller: "model", Enterprise: run.EnterpriseID.String(), Subject: run.ActorUserID.String(), SubjectType: "user", RunID: run.ID.String(), Input: input})
	if err != nil {
		code := "TOOL_EXECUTION_FAILED"
		if errors.Is(err, mcp.ErrPermissionDenied) {
			code = "AUTHORIZATION_DENIED"
		} else if errors.Is(err, mcp.ErrInputInvalid) {
			code = "TOOL_INPUT_INVALID"
		}
		_, _ = loop.Store.Queries.FinishToolCall(ctx, db.FinishToolCallParams{ID: record.ID, Status: "failed", ErrorCode: pgtype.Text{String: code, Valid: true}})
		return toolExecutionResult{}, runtime.Error{ErrorCode: code, Cause: err, Permanent: true}
	}
	full, _ := json.Marshal(result.Structured)
	maxBytes := min(maxToolResultBytes, metadata.MaxResultBytes)
	if len(full) > maxBytes {
		return toolExecutionResult{}, runtime.Error{ErrorCode: "TOOL_RESULT_TOO_LARGE", Cause: errors.New("tool result exceeds 4 MiB"), Permanent: true}
	}
	resultRef := result.ResultRef
	if resultRef == "" {
		resultRef, _ = opaqueRef("result_")
	}
	projection, projectionPartial, err := encodeToolResultProjection(resultRef, call, full, result)
	if err != nil {
		return toolExecutionResult{}, runtime.Error{ErrorCode: "TOOL_RESULT_TOO_LARGE", Cause: err, Permanent: true}
	}
	fullHash := sha256.Sum256(full)
	projectionHash := sha256.Sum256(projection)
	artifact, err := loop.Store.Queries.CreateArtifact(ctx, db.CreateArtifactParams{ID: newAgentID(), ResultRef: resultRef, EnterpriseID: run.EnterpriseID, ConversationID: uuid.NullUUID{UUID: run.ConversationID, Valid: true}, RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, ContentType: "application/json", DataClassification: "internal", Content: full, ContentHash: fullHash[:], ByteSize: int32(len(full))})
	if err != nil {
		return toolExecutionResult{}, err
	}
	if _, err = loop.Store.Queries.CreateToolResult(ctx, db.CreateToolResultParams{ID: newAgentID(), ToolCallID: record.ID, EnterpriseID: run.EnterpriseID, ArtifactID: artifact.ID, Projection: projection, ProjectionHash: projectionHash[:], ProjectionBytes: int32(len(projection)), Partial: projectionPartial}); err != nil {
		return toolExecutionResult{}, err
	}
	if _, err = loop.Store.Queries.FinishToolCall(ctx, db.FinishToolCallParams{ID: record.ID, Status: "succeeded"}); err != nil {
		return toolExecutionResult{}, err
	}
	err = loop.Store.InTx(ctx, func(q *db.Queries) error {
		_, eventErr := conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID, RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, StepID: uuid.NullUUID{UUID: step.ID, Valid: true}, Type: "tool_call_result", ActorType: "service", Payload: map[string]any{"tool_call_id": call.ID, "tool_id": call.Name, "result_ref": resultRef, "projection": json.RawMessage(projection)}, ArtifactRef: resultRef, Classification: "internal"})
		return eventErr
	})
	actionRef, _ := result.Structured["action_ref"].(string)
	return toolExecutionResult{message: "Tool result " + resultRef + ": " + string(projection), actionRef: actionRef}, err
}

func firstActionRef(results []toolExecutionResult) string {
	for _, result := range results {
		if result.actionRef != "" {
			return result.actionRef
		}
	}
	return ""
}

func (loop Loop) waitForAction(ctx context.Context, run db.Run, actionRef string) error {
	return loop.Store.InTx(ctx, func(q *db.Queries) error {
		current, err := q.GetRun(ctx, db.GetRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID})
		if err != nil {
			return err
		}
		if _, err = q.UpdateRunStatus(ctx, db.UpdateRunStatusParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "waiting_input",
			StopReason: pgtype.Text{String: "pending_action_confirmation", Valid: true}, Version: current.Version}); err != nil {
			return err
		}
		_, err = conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID,
			RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Type: "pending_action_created", ActorType: "service",
			Payload: map[string]any{"action_ref": actionRef, "card": map[string]any{"card_instance_id": "pending_action_" + actionRef,
				"interactive_card_id": "argus.pending_action", "version": "v1", "title": "Pending action", "pending_action_ref": actionRef}}, Classification: "internal"})
		return err
	})
}

func (loop Loop) persistAssistant(ctx context.Context, run db.Run, step db.RunStep, text string, input, output int64) error {
	return loop.Store.InTx(ctx, func(q *db.Queries) error {
		if _, err := conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID, RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, StepID: uuid.NullUUID{UUID: step.ID, Valid: true}, Type: "assistant_message", ActorType: "model", ActorID: run.ModelID.String(), Payload: map[string]any{"content": text}, Classification: "internal"}); err != nil {
			return err
		}
		_, err := conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID, RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, StepID: uuid.NullUUID{UUID: step.ID, Valid: true}, Type: "model_usage", ActorType: "system", Payload: map[string]any{"input_tokens": input, "output_tokens": output}, Classification: "internal"})
		if err != nil {
			return err
		}
		_, err = q.FinishRunStep(ctx, db.FinishRunStepParams{ID: step.ID, EnterpriseID: run.EnterpriseID, Status: "succeeded"})
		return err
	})
}
func (loop Loop) persistDelta(ctx context.Context, run db.Run, step db.RunStep, text string) error {
	return loop.Store.InTx(ctx, func(q *db.Queries) error {
		_, err := conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID, RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, StepID: uuid.NullUUID{UUID: step.ID, Valid: true}, Type: "run_state_changed", ActorType: "model", Payload: map[string]any{"agent_event_type": "message_delta", "delta": text}, Classification: "internal"})
		return err
	})
}
func (loop Loop) finishRun(ctx context.Context, run db.Run, status, reason, errorCode string) error {
	return loop.Store.InTx(ctx, func(q *db.Queries) error {
		current, err := q.GetRun(ctx, db.GetRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID})
		if err != nil {
			return err
		}
		if terminalRun(current.Status) {
			return nil
		}
		if _, err = q.UpdateRunStatus(ctx, db.UpdateRunStatusParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: status, StopReason: pgtype.Text{String: reason, Valid: reason != ""}, ErrorCode: pgtype.Text{String: errorCode, Valid: errorCode != ""}, Version: current.Version}); err != nil {
			return err
		}
		payload := map[string]any{"status": status, "stop_reason": reason}
		if errorCode != "" {
			payload["error_code"] = errorCode
		}
		_, err = conversation.AppendEvent(ctx, q, conversation.EventInput{EnterpriseID: run.EnterpriseID, ConversationID: run.ConversationID, RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Type: "run_state_changed", ActorType: "system", Payload: payload, Classification: "internal"})
		return err
	})
}
func (loop Loop) cancelled(ctx context.Context, run db.Run) (bool, error) {
	value, err := loop.Store.Queries.GetRun(ctx, db.GetRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID})
	return err == nil && value.Status == "cancelled", err
}
func (loop Loop) waitForCompaction(ctx context.Context, run db.Run, errorCode string) error {
	if err := loop.enqueueCompaction(ctx, run, "hard_limit"); err != nil {
		return err
	}
	current, err := loop.Store.Queries.GetRun(ctx, db.GetRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID})
	if err != nil {
		return err
	}
	_, err = loop.Store.Queries.UpdateRunStatus(ctx, db.UpdateRunStatusParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "waiting_system", StopReason: pgtype.Text{String: "context_compaction", Valid: true}, ErrorCode: pgtype.Text{String: errorCode, Valid: errorCode != ""}, Version: current.Version})
	return err
}
func (loop Loop) enqueueCompaction(ctx context.Context, run db.Run, reason string) error {
	payload, _ := json.Marshal(conversation.AgentTask{RunID: run.ID, EnterpriseID: run.EnterpriseID, Reason: reason})
	_, err := loop.Store.Queries.CreateRuntimeTask(ctx, db.CreateRuntimeTaskParams{ID: newAgentID(), EnterpriseID: uuid.NullUUID{UUID: run.EnterpriseID, Valid: true}, Queue: "compaction", RunID: uuid.NullUUID{UUID: run.ID, Valid: true}, Payload: payload, MaxAttempts: 3, AvailableAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}})
	return err
}
func (loop Loop) modelTools() []modelprovider.Tool {
	if loop.Tools == nil {
		return nil
	}
	catalog := loop.Tools.ModelCatalog()
	result := make([]modelprovider.Tool, 0, len(catalog))
	for _, item := range catalog {
		result = append(result, modelprovider.Tool{Name: item.ID, Description: "Governed Argus tool", Schema: item.InputSchema})
	}
	return result
}
func orderedCalls(values map[string]*pendingToolCall) []*pendingToolCall {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]*pendingToolCall, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}
func terminalRun(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled" || status == "timed_out"
}

func allowsToolExecution(stopReason string) bool {
	switch strings.ToLower(strings.TrimSpace(stopReason)) {
	case "length", "max_tokens", "incomplete", "content_filter", "failed", "cancelled":
		return false
	default:
		return true
	}
}
func opaqueRef(prefix string) (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}
func tokenAmount(input, output int64, inputPrice, outputPrice pgtype.Numeric) pgtype.Numeric {
	in, _ := inputPrice.Float64Value()
	out, _ := outputPrice.Float64Value()
	amount := float64(input)*in.Float64/1_000_000 + float64(output)*out.Float64/1_000_000
	var result pgtype.Numeric
	_ = result.Scan(fmt.Sprintf("%.8f", amount))
	return result
}
func newAgentID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}
