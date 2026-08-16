import type { ISODateString } from "../types/common";

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

export interface InteractiveCardCreateCommand {
  type: "interactive_card.create";
}

export interface SendMessageInput {
  text: string;
  command?: InteractiveCardCreateCommand;
}

export interface CreateConversationInput {
  title?: string;
  selectedModelId?: string;
}
