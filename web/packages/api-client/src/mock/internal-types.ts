export interface MockEnterpriseUserRecord {
  userId: string;
  enterpriseId: string;
  departmentId: string;
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
