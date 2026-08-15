import type {
  AuditOrigin,
  ChatStreamEvent,
  ListQuery,
  Page,
  PendingAction,
  Task,
  User,
} from "../types";
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
  params: Record<string, unknown>;
  conversationId?: string;
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
  mustFind<T>(
    items: T[],
    predicate: (item: T) => boolean,
    what: string,
  ): T;
  emitTask(task: Task): void;
}

/** Two-phase action engine and chat streaming, see mock/engine.ts. */
export interface Engine {
  createPendingAction(input: CreatePendingActionInput): PendingAction;
  getAction(actionRef: string): PendingAction;
  ensureNotExpired(action: PendingAction): void;
  startExecution(action: PendingAction): Task;
  streamReply(
    conversationId: string,
    text: string,
  ): AsyncIterable<ChatStreamEvent>;
}

export type MockContext = BaseContext & Engine;
