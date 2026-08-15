import type { ISODateString } from "./common";

export type InteractiveCardSource = "system" | "enterprise";
export type InteractiveCardLifecycle = "draft" | "active" | "deprecated";
export type SlotBindingMode = "strict" | "preferred";
export type SlotValueType = "string" | "number" | "boolean" | "array" | "object";
export type DemoScenario = "default" | "empty" | "error" | "large" | "light" | "dark" | "zh-CN" | "en-US";

export interface InteractiveCardSlot {
  name: string;
  type: SlotValueType;
  required: boolean;
  aiGenerated?: boolean;
}

export interface SlotBinding {
  slotName: string;
  mode: SlotBindingMode;
  toolName: string;
  schemaVersion: string;
  fieldPath: string;
  arrayMapping?: { itemPath?: string; labelPath?: string; valuePath?: string };
}

export interface ToolSchemaField {
  path: string;
  type: SlotValueType;
  description?: string;
}

export interface ToolOutputSchema {
  toolName: string;
  version: string;
  fields: ToolSchemaField[];
}

export interface InteractiveCardValidationIssue {
  code: string;
  message: string;
  slot?: string;
  scenario?: DemoScenario;
}

export interface InteractiveCardValidationResult {
  valid: boolean;
  checkedAt: ISODateString;
  passedScenarios: DemoScenario[];
  issues: InteractiveCardValidationIssue[];
}

export interface InteractiveCard {
  id: string;
  enterpriseId?: string;
  source: InteractiveCardSource;
  slug: string;
  name: string;
  description: string;
  version: string;
  revision: number;
  lifecycle: InteractiveCardLifecycle;
  enabled: boolean;
  htmlTemplate: string;
  slots: InteractiveCardSlot[];
  bindings: SlotBinding[];
  demoData: Record<string, unknown>;
  validation?: InteractiveCardValidationResult;
  createdBy?: string;
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface CreateInteractiveCardInput {
  slug: string;
  name: string;
  description: string;
  htmlTemplate: string;
  slots?: InteractiveCardSlot[];
  demoData?: Record<string, unknown>;
}

export interface UpdateInteractiveCardInput {
  name?: string;
  description?: string;
  htmlTemplate?: string;
  slots?: InteractiveCardSlot[];
  demoData?: Record<string, unknown>;
}

export interface InteractiveCardFilter {
  source?: InteractiveCardSource;
  lifecycle?: InteractiveCardLifecycle[];
  query?: string;
}

export interface InteractiveCardDemoRender {
  interactiveCardId: string;
  html: string;
  demoData: Record<string, unknown>;
}
