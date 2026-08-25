import type {
  ActionOneTimeResult,
  PendingActionCommandResult,
  Execution,
  PendingActionPublic,
} from "../generated/contracts";

export type { Execution, PendingActionPublic } from "../generated/contracts";

export type PendingActionStatus = PendingActionPublic["status"];
export type PendingActionRisk = PendingActionPublic["risk"];

export interface PreviewActionInput {
  tool: string;
  title?: string;
  input_data: Record<string, unknown>;
  conversation_id?: string;
}

export interface PendingActionFilter {
  status?: PendingActionStatus[];
  risk?: PendingActionRisk[];
  query?: string;
  scope?: "mine" | "created" | "all";
}

export type ConfirmActionResult = PendingActionCommandResult & {
  execution?: Execution;
  one_time_result?: ActionOneTimeResult;
};
