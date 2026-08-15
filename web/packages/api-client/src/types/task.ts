import type {
  AuditOrigin,
  ISODateString,
  ResourceRef,
  RiskLevel,
} from "./common";

export type TaskType =
  | "host_onboard"
  | "host_command"
  | "collector_install"
  | "collector_upgrade"
  | "kubernetes_change"
  | "certificate_rotation"
  | "generic";

/** Run/Task state machine from docs/04 §4. */
export type TaskStatus =
  | "pending"
  | "running"
  | "waiting_input"
  | "waiting_approval"
  | "succeeded"
  | "failed"
  | "cancelled"
  | "timed_out";

export type TaskStepStatus =
  | "pending"
  | "running"
  | "done"
  | "failed"
  | "skipped";

export interface TaskStep {
  id: string;
  name: string;
  status: TaskStepStatus;
  detail?: string;
  startedAt?: ISODateString;
  finishedAt?: ISODateString;
}

export interface TaskLogEntry {
  timestamp: ISODateString;
  level: "info" | "warn" | "error";
  message: string;
}

export interface Task {
  id: string;
  enterpriseId: string;
  type: TaskType;
  title: string;
  status: TaskStatus;
  origin: AuditOrigin;
  createdBy: string;
  createdByName?: string;
  relatedResources: ResourceRef[];
  steps: TaskStep[];
  logs: TaskLogEntry[];
  pendingActionId?: string;
  /** 0-100 derived from completed steps. */
  progress: number;
  startedAt?: ISODateString;
  finishedAt?: ISODateString;
  error?: string;
  createdAt: ISODateString;
}

export interface TaskFilter {
  status?: TaskStatus[];
  type?: TaskType[];
  query?: string;
}

export type TaskEvent =
  | { type: "task_updated"; task: Task }
  | { type: "step_updated"; taskId: string; step: TaskStep }
  | { type: "log_appended"; taskId: string; entry: TaskLogEntry };

/** Pending Action state machine from docs/04 §11. */
export type PendingActionStatus =
  | "awaiting_confirmation"
  | "awaiting_approval"
  | "ready"
  | "executing"
  | "succeeded"
  | "failed"
  | "cancelled"
  | "expired"
  | "rejected";

export interface DiffLine {
  kind: "add" | "remove" | "change" | "note";
  text: string;
}

export interface ApprovalDecision {
  userId: string;
  userName?: string;
  decision: "approved" | "rejected";
  reason?: string;
  at: ISODateString;
}

/** Approval requirement snapshot computed at preview time. */
export interface ApprovalRequirement {
  required: boolean;
  policyId?: string;
  policyName?: string;
  minApprovers: number;
  approverRoleIds: string[];
  separationOfDuty: boolean;
  decisions: ApprovalDecision[];
}

export interface PendingAction {
  id: string;
  /** Public reference visible to model, browser and cards. */
  actionRef: string;
  enterpriseId: string;
  /** Mutation tool prefix, e.g. "host.create" (implies host.create.preview/.commit). */
  tool: string;
  title: string;
  summary: string;
  riskLevel: RiskLevel;
  /** Public preview projection shown on confirm cards. */
  preview: Record<string, unknown>;
  /** Immutable plan parameters, restored server-side at commit. */
  params: Record<string, unknown>;
  diff: DiffLine[];
  planHash: string;
  expiresAt: ISODateString;
  status: PendingActionStatus;
  createdBy: string;
  createdByName?: string;
  conversationId?: string;
  approval?: ApprovalRequirement;
  taskId?: string;
  resultSummary?: string;
  createdAt: ISODateString;
  updatedAt: ISODateString;
}

export interface PreviewActionInput {
  tool: string;
  title?: string;
  params: Record<string, unknown>;
  conversationId?: string;
}

export interface PendingActionFilter {
  status?: PendingActionStatus[];
  riskLevel?: RiskLevel[];
  query?: string;
}

export interface ConfirmActionResult {
  pendingAction: PendingAction;
  /** Present once the action entered execution. */
  task?: Task;
}
