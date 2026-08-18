export interface MockEnterpriseUserRecord {
  userId: string;
  enterpriseId: string;
  departmentId: string;
}

export interface MockConversationRecord {
  id: string;
  enterpriseId: string;
  title: string;
  createdBy: string;
  selectedModelId: string;
  status: "active" | "archived";
  lastMessageAt: string;
  createdAt: string;
  version?: number;
}

export interface MockApprovalDecision {
  actor_user_id: string;
  actor_name?: string;
  decision: "approved" | "rejected";
  reason?: string;
  decided_at: string;
}

export interface MockActionPlanRecord {
  tool: string;
  enterprise_id: string;
  created_by: string;
  created_by_name?: string;
  conversation_id?: string;
  task_id?: string;
  input_data: Record<string, unknown>;
  approval_decisions: MockApprovalDecision[];
}

export type MockCardScenario = "default" | "empty" | "error" | "large" | "light" | "dark" | "zh-CN" | "en-US";

export interface MockInteractiveCard {
  id: string;
  enterpriseId?: string;
  source: "system" | "enterprise";
  slug: string;
  name: string;
  description: string;
  version: string;
  revision: number;
  lifecycle: "draft" | "active" | "deprecated";
  enabled: boolean;
  htmlTemplate: string;
  slots: Array<{
    name: string;
    type: "string" | "number" | "boolean" | "array" | "object";
    required: boolean;
    aiGenerated?: boolean;
  }>;
  bindings: Array<{
    slotName: string;
    mode: "strict" | "preferred";
    toolName: string;
    schemaVersion: string;
    fieldPath: string;
  }>;
  demoData: Record<string, unknown>;
  validation?: {
    valid: boolean;
    checkedAt: string;
    passedScenarios: MockCardScenario[];
    issues: Array<{ code: string; message: string; slot?: string; scenario?: MockCardScenario }>;
  };
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}
