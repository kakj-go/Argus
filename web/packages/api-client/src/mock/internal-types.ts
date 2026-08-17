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
