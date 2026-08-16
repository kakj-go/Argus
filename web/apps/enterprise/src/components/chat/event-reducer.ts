import type {
  AgentEvent,
  ConversationEvent,
  Run,
  RunCheckpoint,
  StreamEventEnvelope,
} from "@argus/api-client/contracts";
import {
  chatMessageFromPublic,
  type CardInstance,
  type ChatMessage,
  type ToolCallTrace,
} from "./chat-view-model";

export type ChatStopReason =
  "completed" | "cancelled" | "failed" | "output_limit";

export type CompactionView = {
  status: "idle" | "running" | "completed" | "failed";
  tokens_before?: number;
  tokens_after?: number;
};

export type ConversationProjection = {
  last_sequence: number;
  event_ids: ReadonlySet<string>;
  events: readonly ConversationEvent[];
  run?: Run;
  checkpoint?: RunCheckpoint;
  message_text: string;
  message_id?: string;
  tool_calls: ReadonlyMap<string, ToolCallTrace>;
  cards: readonly CardInstance[];
  completed_message?: ChatMessage;
  pending_action_refs: readonly string[];
  execution_refs: readonly string[];
  compaction: CompactionView;
  stop_reason?: ChatStopReason;
};

export const initialConversationProjection: ConversationProjection = {
  last_sequence: 0,
  event_ids: new Set(),
  events: [],
  message_text: "",
  tool_calls: new Map(),
  cards: [],
  pending_action_refs: [],
  execution_refs: [],
  compaction: { status: "idle" },
};

function objectPayload(payload: unknown): Record<string, unknown> {
  return typeof payload === "object" && payload !== null
    ? (payload as Record<string, unknown>)
    : {};
}

function stringField(
  payload: Record<string, unknown>,
  key: string,
): string | undefined {
  return typeof payload[key] === "string" ? payload[key] : undefined;
}

export function reduceAgentEvent(
  state: ConversationProjection,
  event: AgentEvent,
): ConversationProjection {
  if (
    state.event_ids.has(event.event_id) ||
    event.sequence <= state.last_sequence
  )
    return state;
  const payload = objectPayload(event.payload);
  const event_ids = new Set(state.event_ids).add(event.event_id);
  const tool_calls = new Map(state.tool_calls);
  let message_text = state.message_text;
  let message_id = state.message_id;
  let cards = state.cards;
  let completed_message = state.completed_message;
  let pending_action_refs = state.pending_action_refs;
  let execution_refs = state.execution_refs;
  let compaction = state.compaction;
  let stop_reason = state.stop_reason;

  if (event.event_type === "message_started") {
    message_id = stringField(payload, "message_id") ?? event.message_id;
  } else if (event.event_type === "message_delta") {
    message_text += stringField(payload, "delta") ?? "";
  } else if (event.event_type === "tool_call_started") {
    const ref = stringField(payload, "tool_call_id");
    if (ref) {
      tool_calls.set(ref, {
        callId: ref,
        toolName: stringField(payload, "tool_name") ?? "tool",
        status: "running",
        startedAt: stringField(payload, "started_at") ?? event.occurred_at,
      });
    }
  } else if (event.event_type === "tool_call_completed") {
    const ref = stringField(payload, "tool_call_id");
    const current = ref ? tool_calls.get(ref) : undefined;
    if (ref) {
      tool_calls.set(ref, {
        callId: ref,
        toolName: current?.toolName ?? "tool",
        startedAt: current?.startedAt ?? event.occurred_at,
        status: payload.status === "failed" ? "failed" : "success",
        summary: stringField(payload, "summary"),
        durationMs:
          typeof payload.duration_ms === "number"
            ? payload.duration_ms
            : undefined,
      });
    }
  } else if (event.event_type === "pending_action_created") {
    const ref = stringField(payload, "action_ref");
    if (ref && !pending_action_refs.includes(ref))
      pending_action_refs = [...pending_action_refs, ref];
  } else if (event.event_type === "context_compaction_started") {
    compaction = {
      status: "running",
      tokens_before: Number(payload.tokens_before) || undefined,
    };
  } else if (event.event_type === "context_compaction_completed") {
    compaction = {
      status: "completed",
      tokens_before: Number(payload.tokens_before) || compaction.tokens_before,
      tokens_after: Number(payload.tokens_after) || undefined,
    };
  } else if (event.event_type === "context_compaction_failed") {
    compaction = { ...compaction, status: "failed" };
  } else if (event.event_type === "run_completed") {
    stop_reason =
      payload.stop_reason === "output_limit" ? "output_limit" : "completed";
  } else if (event.event_type === "run_failed") {
    stop_reason = "failed";
  }
  const card = objectPayload(payload.card);
  const cardId = stringField(card, "card_instance_id");
  const interactiveCardId = stringField(card, "interactive_card_id");
  const version = stringField(card, "version");
  if (cardId && interactiveCardId && version && !cards.some((item) => item.id === cardId)) {
    cards = [...cards, {
      id: cardId,
      interactiveCardId,
      version,
      title: stringField(card, "title"),
      pendingActionRef: stringField(card, "pending_action_ref"),
      actionBindingId: stringField(card, "action_binding_id"),
    }];
  }
  if (event.event_type === "message_completed") {
    completed_message = chatMessageFromPublic(payload.message) ?? undefined;
  }
  const execution_ref = stringField(payload, "execution_ref");
  if (execution_ref && !execution_refs.includes(execution_ref))
    execution_refs = [...execution_refs, execution_ref];

  return {
    ...state,
    last_sequence: event.sequence,
    event_ids,
    message_text,
    message_id,
    tool_calls,
    cards,
    completed_message,
    pending_action_refs,
    execution_refs,
    compaction,
    stop_reason,
  };
}

export function reduceConversationEvent(
  state: ConversationProjection,
  event: ConversationEvent,
): ConversationProjection {
  if (
    state.event_ids.has(event.event_id) ||
    event.sequence <= state.last_sequence
  )
    return state;
  return {
    ...state,
    last_sequence: event.sequence,
    event_ids: new Set(state.event_ids).add(event.event_id),
    events: [...state.events, event],
  };
}

export function reduceStreamEnvelope(
  state: ConversationProjection,
  envelope: StreamEventEnvelope,
): ConversationProjection {
  if (envelope.event_type !== "agent_event") return state;
  return reduceAgentEvent(state, envelope.data as AgentEvent);
}
