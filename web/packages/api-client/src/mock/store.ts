import type {
  AIModel,
  ApiKey,
  ApprovalPolicy,
  AuditEvent,
  InteractiveCard,
  DataScope,
  Enterprise,
  EnterpriseAdmin,
  EnterpriseSandboxQuota,
  Department,
  ModelQuota,
  PendingActionPublic,
  PlatformState,
  Role,
  RoleBinding,
  SandboxBackend,
  SandboxImage,
  SandboxProfile,
  SandboxSessionMeta,
  SandboxUsagePoint,
  Secret,
  ServiceAccount,
  ModelUsagePoint,
  Unsubscribe,
  User,
} from "../types";
import type {
  CollectionClaim,
  CollectorInstallState,
  Conversation,
  K8sNodeBinding,
  RemoteSession,
  TaskViewModel,
} from "../provisional";
import type { MockChatMessage } from "./chat-types";
import type {
  MockActionPlanRecord,
  MockEnterpriseUserRecord,
} from "./internal-types";
import type {
  ConnectorEnrollmentToken,
  ConnectorUninstallCommand,
  MockBastionScope,
  MockConnector,
  MockHost,
  MockKubernetesCluster,
} from "./resource-models";

/** Whole in-memory database backing the mock client. */
export interface MockDb {
  schemaVersion: 11;
  seq: Record<string, number>;
  platformState: { state: PlatformState; name: string };
  enterprises: Enterprise[];
  enterpriseAdmins: EnterpriseAdmin[];
  users: User[];
  credentials: Record<string, string>;
  enterpriseUsers: MockEnterpriseUserRecord[];
  departments: Department[];
  roles: Role[];
  roleBindings: RoleBinding[];
  dataScopes: DataScope[];
  approvalPolicies: ApprovalPolicy[];
  serviceAccounts: ServiceAccount[];
  apiKeys: ApiKey[];
  secrets: Secret[];
  hosts: MockHost[];
  connectors: MockConnector[];
  bastionScopes: MockBastionScope[];
  enrollmentTokens: ConnectorEnrollmentToken[];
  uninstallCommands: ConnectorUninstallCommand[];
  remoteSessions: RemoteSession[];
  clusters: MockKubernetesCluster[];
  nodeBindings: K8sNodeBinding[];
  collectionClaims: CollectionClaim[];
  collectors: CollectorInstallState[];
  tasks: TaskViewModel[];
  pendingActions: PendingActionPublic[];
  actionPlans: Record<string, MockActionPlanRecord>;
  conversations: Conversation[];
  messages: MockChatMessage[];
  models: AIModel[];
  modelQuotas: ModelQuota[];
  usagePoints: ModelUsagePoint[];
  interactiveCards: InteractiveCard[];
  auditEvents: AuditEvent[];
  sandboxBackends: SandboxBackend[];
  sandboxImages: SandboxImage[];
  sandboxProfiles: SandboxProfile[];
  sandboxQuotas: EnterpriseSandboxQuota[];
  sandboxSessions: SandboxSessionMeta[];
  sandboxUsage: SandboxUsagePoint[];
  session: { userId: string | null; enterpriseId: string | null };
}

export const STORAGE_PREFIX = "argus-mock:";
const DB_KEY = `${STORAGE_PREFIX}db-v11`;

function storage(): Storage | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage;
  } catch {
    return null;
  }
}

export function loadDb(): MockDb | null {
  const store = storage();
  if (!store) return null;
  const raw = store.getItem(DB_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as MockDb;
    return parsed.schemaVersion === 11 ? parsed : null;
  } catch {
    return null;
  }
}

export function saveDb(db: MockDb): void {
  const store = storage();
  if (!store) return;
  try {
    store.setItem(DB_KEY, JSON.stringify(db));
  } catch {
    // Quota or serialization failures must not break the mock.
  }
}

/** Removes every `argus-mock:*` key. */
export function clearStoredDb(): void {
  const store = storage();
  if (!store) return;
  const keys: string[] = [];
  for (let i = 0; i < store.length; i += 1) {
    const key = store.key(i);
    if (key?.startsWith(STORAGE_PREFIX)) keys.push(key);
  }
  for (const key of keys) store.removeItem(key);
}

/** Sequential id factory whose counter persists with the database. */
export function nextId(db: MockDb, prefix: string): string {
  const value = (db.seq[prefix] ?? 0) + 1;
  db.seq[prefix] = value;
  return `${prefix}-${String(value).padStart(4, "0")}`;
}

/** Minimal topic-based event bus for task/chat subscriptions. */
export class Emitter {
  private readonly listeners = new Map<string, Set<(event: never) => void>>();

  on<T>(topic: string, listener: (event: T) => void): Unsubscribe {
    let set = this.listeners.get(topic);
    if (!set) {
      set = new Set();
      this.listeners.set(topic, set);
    }
    const fn = listener as (event: never) => void;
    set.add(fn);
    return () => {
      set.delete(fn);
    };
  }

  emit<T>(topic: string, event: T): void {
    const set = this.listeners.get(topic);
    if (!set) return;
    for (const listener of [...set]) {
      (listener as (e: T) => void)(event);
    }
  }
}
