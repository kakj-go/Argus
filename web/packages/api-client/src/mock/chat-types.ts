export interface MockToolCallTrace {
  callId: string;
  toolName: string;
  status: "running" | "success" | "failed";
  summary?: string;
  durationMs?: number;
  startedAt: string;
}

export interface MockCardInstance {
  id: string;
  interactiveCardId: string;
  version: string;
  title?: string;
  pendingActionRef?: string;
  actionBindingId?: string;
}

export interface MockCardActionResultEvent {
  type: "card_action_result";
  origin: "user_interaction";
  actorUserId: string;
  cardInstanceId: string;
  action: string;
  tool: string;
  status: "success" | "failed";
  resultRef?: string;
}

export interface MockChatMessage {
  id: string;
  conversationId: string;
  role: "user" | "assistant" | "system";
  content: string;
  createdAt: string;
  modelId?: string;
  modelRevision?: number;
  inputPricePerMillionSnapshot?: number;
  outputPricePerMillionSnapshot?: number;
  inputTokens?: number;
  outputTokens?: number;
  createdInteractiveCardId?: string;
  toolCalls?: MockToolCallTrace[];
  cards?: MockCardInstance[];
  event?: MockCardActionResultEvent;
}

export type MockChatStreamEvent =
  | { type: "message_start"; messageId: string }
  | { type: "token"; messageId: string; delta: string }
  | { type: "tool_call"; messageId: string; toolCall: MockToolCallTrace }
  | { type: "tool_call_update"; messageId: string; callId: string; status: "success" | "failed"; durationMs: number; summary?: string }
  | { type: "card"; messageId: string; card: MockCardInstance }
  | { type: "interactive_card_created"; messageId: string; interactiveCardId: string }
  | { type: "card_action_result"; messageId: string; event: MockCardActionResultEvent }
  | { type: "message_done"; message: MockChatMessage }
  | { type: "error"; message: string };
