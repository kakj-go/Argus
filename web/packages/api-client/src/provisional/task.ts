import type {
  AuditOrigin,
  ISODateString,
  ResourceRef,
} from "../types/common";

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

export interface TaskViewModel {
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
  | { type: "task_updated"; task: TaskViewModel }
  | { type: "step_updated"; taskId: string; step: TaskStep }
  | { type: "log_appended"; taskId: string; entry: TaskLogEntry };
