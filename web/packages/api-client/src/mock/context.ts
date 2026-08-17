import type {
  AuditOrigin,
  ConfirmActionResult,
  ListQuery,
  Page,
  PendingActionPublic,
  User,
} from "../types";
import type { TaskViewModel } from "../provisional";
import type { MockChatStreamEvent as ChatStreamEvent } from "./chat-types";
import type { Emitter, MockDb } from "./store";

export interface AuditEntry {
  resourceType?: string;
  resourceId?: string;
  summary: string;
  result?: "success" | "failure" | "denied";
  origin?: AuditOrigin;
  platform?: boolean;
}

export interface CreatePendingActionInput {
  tool: string;
  title?: string;
  input_data: Record<string, unknown>;
  conversation_id?: string;
}

/** Shared plumbing handed to every mock domain implementation. */
export interface BaseContext {
  readonly db: MockDb;
  readonly options: { delay?: number; stepDelay: number };
  readonly emitter: Emitter;
  nowIso(): string;
  pause(ms?: number): Promise<void>;
  save(): void;
  actor(): Pick<User, "id" | "displayName">;
  requirePlatform(): void;
  enterpriseId(): string;
  audit(action: string, entry: AuditEntry): void;
  paginate<T>(items: T[], query?: ListQuery): Page<T>;
  mustFind<T>(items: T[], predicate: (item: T) => boolean, what: string): T;
  emitTask(task: TaskViewModel): void;
}

/** Two-phase action engine and chat streaming, see mock/engine.ts. */
export interface Engine {
  createPendingAction(input: CreatePendingActionInput): PendingActionPublic;
  getAction(actionRef: string): PendingActionPublic;
  ensureNotExpired(action: PendingActionPublic): void;
  commitResourceAction(
    action: PendingActionPublic,
  ): ConfirmActionResult | undefined;
  startExecution(action: PendingActionPublic): TaskViewModel;
  streamReply(
    conversationId: string,
    text: string,
  ): AsyncIterable<ChatStreamEvent>;
}

export type MockContext = BaseContext & Engine;
