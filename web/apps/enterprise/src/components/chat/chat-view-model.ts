export type MessageRole = "user" | "assistant" | "system";

export interface ToolCallTrace {
  callId: string;
  toolName: string;
  status: "running" | "success" | "failed";
  summary?: string;
  durationMs?: number;
  startedAt: string;
}

export interface CardInstance {
  id: string;
  interactiveCardId: string;
  version: string;
  title?: string;
  pendingActionRef?: string;
  actionBindingId?: string;
}

export interface CardActionResultEvent {
  type: "card_action_result";
  origin: "user_interaction";
  actorUserId: string;
  cardInstanceId: string;
  action: string;
  tool: string;
  status: "success" | "failed";
  resultRef?: string;
}

export interface ChatMessage {
  id: string;
  conversationId: string;
  role: MessageRole;
  content: string;
  createdAt: string;
  modelId?: string;
  modelRevision?: number;
  inputPricePerMillionSnapshot?: number;
  outputPricePerMillionSnapshot?: number;
  inputTokens?: number;
  outputTokens?: number;
  createdInteractiveCardId?: string;
  toolCalls?: ToolCallTrace[];
  cards?: CardInstance[];
  event?: CardActionResultEvent;
}

function record(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : null;
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function numberValue(value: unknown): number | undefined {
  return typeof value === "number" ? value : undefined;
}

export function chatMessageFromPublic(value: unknown): ChatMessage | null {
  const source = record(value);
  const messageId = stringValue(source?.message_id);
  const conversationId = stringValue(source?.conversation_id);
  const role = stringValue(source?.role);
  const content = stringValue(source?.content);
  const createdAt = stringValue(source?.created_at);
  if (
    !source ||
    !messageId ||
    !conversationId ||
    (role !== "user" && role !== "assistant" && role !== "system") ||
    content === undefined ||
    !createdAt
  ) {
    return null;
  }
  const toolCalls = Array.isArray(source.tool_calls)
    ? source.tool_calls.flatMap((entry): ToolCallTrace[] => {
        const item = record(entry);
        const callId = stringValue(item?.call_id);
        const toolName = stringValue(item?.tool_name);
        const status = stringValue(item?.status);
        const startedAt = stringValue(item?.started_at);
        if (
          !item ||
          !callId ||
          !toolName ||
          !startedAt ||
          (status !== "running" && status !== "success" && status !== "failed")
        ) {
          return [];
        }
        return [{
          callId,
          toolName,
          status,
          startedAt,
          summary: stringValue(item.summary),
          durationMs: numberValue(item.duration_ms),
        }];
      })
    : [];
  const cards = Array.isArray(source.cards)
    ? source.cards.flatMap((entry): CardInstance[] => {
        const item = record(entry);
        const id = stringValue(item?.card_instance_id);
        const interactiveCardId = stringValue(item?.interactive_card_id);
        const version = stringValue(item?.version);
        if (!item || !id || !interactiveCardId || !version) return [];
        return [{
          id,
          interactiveCardId,
          version,
          title: stringValue(item.title),
          pendingActionRef: stringValue(item.pending_action_ref),
          actionBindingId: stringValue(item.action_binding_id),
        }];
      })
    : [];
  const action = record(source.card_action_result);
  const event = action
    ? {
        type: "card_action_result" as const,
        origin: "user_interaction" as const,
        actorUserId: stringValue(action.actor_user_id) ?? "unknown",
        cardInstanceId: stringValue(action.card_instance_id) ?? "unknown",
        action: stringValue(action.action) ?? "unknown",
        tool: stringValue(action.tool) ?? "unknown",
        status: action.status === "failed" ? "failed" as const : "success" as const,
        resultRef: stringValue(action.result_ref),
      }
    : undefined;
  return {
    id: messageId,
    conversationId,
    role,
    content,
    createdAt,
    modelId: stringValue(source.model_id),
    modelRevision: numberValue(source.model_revision),
    inputPricePerMillionSnapshot: numberValue(
      source.input_price_per_million_snapshot,
    ),
    outputPricePerMillionSnapshot: numberValue(
      source.output_price_per_million_snapshot,
    ),
    inputTokens: numberValue(source.input_tokens),
    outputTokens: numberValue(source.output_tokens),
    createdInteractiveCardId: stringValue(source.created_interactive_card_id),
    toolCalls,
    cards,
    event,
  };
}
