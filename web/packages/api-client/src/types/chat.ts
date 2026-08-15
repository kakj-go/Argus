import type { ISODateString } from "./common";

export interface Conversation {
  id: string;
  enterpriseId: string;
  title: string;
  createdBy: string;
  selectedModelId: string;
  status: "active" | "archived";
  lastMessageAt: ISODateString;
  createdAt: ISODateString;
}

export type MessageRole = "user" | "assistant" | "system";

export interface ToolCallTrace {
  callId: string;
  toolName: string;
  status: "running" | "success" | "failed";
  summary?: string;
  durationMs?: number;
  startedAt: ISODateString;
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
  createdAt: ISODateString;
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

export interface InteractiveCardCreateCommand {
  type: "interactive_card.create";
}

export interface SendMessageInput {
  text: string;
  command?: InteractiveCardCreateCommand;
}

export type ChatStreamEvent =
  | { type: "message_start"; messageId: string }
  | { type: "token"; messageId: string; delta: string }
  | { type: "tool_call"; messageId: string; toolCall: ToolCallTrace }
  | { type: "tool_call_update"; messageId: string; callId: string; status: "success" | "failed"; durationMs: number; summary?: string }
  | { type: "card"; messageId: string; card: CardInstance }
  | { type: "interactive_card_created"; messageId: string; interactiveCardId: string }
  | { type: "card_action_result"; messageId: string; event: CardActionResultEvent }
  | { type: "message_done"; message: ChatMessage }
  | { type: "error"; message: string };

export interface CreateConversationInput {
  title?: string;
  selectedModelId?: string;
}
