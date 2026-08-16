import { describe, expect, it } from "vitest";
import type { AgentEvent } from "@argus/api-client/contracts";
import {
  initialConversationProjection,
  reduceAgentEvent,
} from "./event-reducer";

function event(
  sequence: number,
  event_type: AgentEvent["event_type"],
  payload: Record<string, unknown>,
): AgentEvent {
  return {
    schema_version: "argus.agent_event/v1",
    event_id: `event-${sequence}`,
    sequence,
    run_id: "run-1",
    event_type,
    occurred_at: new Date().toISOString(),
    payload,
  };
}

describe("conversation event reducer", () => {
  it("merges ordered deltas and rejects duplicate or out-of-order events", () => {
    const first = reduceAgentEvent(
      initialConversationProjection,
      event(1, "message_delta", { delta: "hello" }),
    );
    const duplicate = reduceAgentEvent(
      first,
      event(1, "message_delta", { delta: " duplicate" }),
    );
    const stale = reduceAgentEvent(
      duplicate,
      event(0, "message_delta", { delta: " stale" }),
    );
    expect(stale).toBe(first);
    expect(stale.message_text).toBe("hello");
  });

  it("projects compaction status and token counts without retaining private summaries", () => {
    const started = reduceAgentEvent(
      initialConversationProjection,
      event(1, "context_compaction_started", {
        tokens_before: 12000,
        narrative_summary: "must never be projected",
        internal_prompt: "private",
      }),
    );
    const completed = reduceAgentEvent(
      started,
      event(2, "context_compaction_completed", {
        tokens_before: 12000,
        tokens_after: 4300,
        narrative_summary: "must never be projected",
      }),
    );
    expect(completed.compaction).toEqual({
      status: "completed",
      tokens_before: 12000,
      tokens_after: 4300,
    });
    expect(JSON.stringify(completed)).not.toContain("narrative_summary");
    expect(JSON.stringify(completed)).not.toContain("internal_prompt");
  });

  it("tracks tool, pending action, execution, and explicit output-limit stops", () => {
    const tool = reduceAgentEvent(
      initialConversationProjection,
      event(1, "tool_call_started", { tool_call_id: "tool-1" }),
    );
    const action = reduceAgentEvent(
      tool,
      event(2, "pending_action_created", {
        action_ref: "action-1",
        execution_ref: "execution-1",
      }),
    );
    const done = reduceAgentEvent(
      action,
      event(3, "run_completed", { stop_reason: "output_limit" }),
    );
    expect(done.tool_calls.get("tool-1")?.status).toBe("running");
    expect(done.pending_action_refs).toEqual(["action-1"]);
    expect(done.execution_refs).toEqual(["execution-1"]);
    expect(done.stop_reason).toBe("output_limit");
  });
});
