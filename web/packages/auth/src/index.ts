import { create, type StoreApi, type UseBoundStore } from "zustand";
import type { ArgusApiClient, LoginInput, SessionInfo } from "@argus/api-client";

export type AuthAudience = "platform" | "enterprise";
export type AuthStatus =
  | "unknown"
  | "checking"
  | "authenticated"
  | "anonymous"
  | "unavailable";

interface BootstrapHint {
  audience: AuthAudience;
  user_id: string;
  locale?: "zh-CN" | "en-US";
  expires_at?: string;
}

export interface AuthState {
  audience: AuthAudience;
  status: AuthStatus;
  session: SessionInfo | null;
  hint: BootstrapHint | null;
  error: string | null;
  restore: (client: ArgusApiClient) => Promise<void>;
  login: (client: ArgusApiClient, input: LoginInput) => Promise<SessionInfo>;
  logout: (client: ArgusApiClient) => Promise<void>;
  clear: () => void;
}

function storageKey(audience: AuthAudience): string {
  return `argus.auth_hint.${audience}`;
}

function loadHint(audience: AuthAudience): BootstrapHint | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(storageKey(audience));
    if (!raw) return null;
    const hint = JSON.parse(raw) as BootstrapHint;
    return hint.audience === audience ? hint : null;
  } catch {
    return null;
  }
}

function isAudience(session: SessionInfo, audience: AuthAudience): boolean {
  return session.session.audience === audience;
}

function persistHint(audience: AuthAudience, session: SessionInfo | null): void {
  if (typeof window === "undefined") return;
  if (!session) {
    window.localStorage.removeItem(storageKey(audience));
    return;
  }
  const hint: BootstrapHint = {
    audience,
    user_id: session.session.user_id,
    locale: session.session.locale,
    expires_at: session.session.expires_at,
  };
  window.localStorage.setItem(storageKey(audience), JSON.stringify(hint));
}

function createAuthStore(
  audience: AuthAudience,
): UseBoundStore<StoreApi<AuthState>> {
  return create<AuthState>((set, get) => ({
    audience,
    status: "unknown",
    session: null,
    hint: loadHint(audience),
    error: null,
    restore: async (client) => {
      set({ status: "checking", error: null, session: null });
      try {
        const session = await client.auth.me();
        if (!isAudience(session, audience)) {
          persistHint(audience, null);
          set({ status: "anonymous", session: null, hint: null });
          return;
        }
        persistHint(audience, session);
        set({
          status: "authenticated",
          session,
          hint: loadHint(audience),
          error: null,
        });
      } catch (error) {
        const unavailable =
          error instanceof Error &&
          "code" in error &&
          error.code === "CLIENT_OPERATION_UNAVAILABLE";
        set({
          status: unavailable ? "unavailable" : "anonymous",
          session: null,
          error: unavailable
            ? error instanceof Error
              ? error.message
              : String(error)
            : null,
        });
      }
    },
    login: async (client, input) => {
      set({ error: null });
      const session = await client.auth.login(input);
      if (!isAudience(session, audience)) {
        await client.auth.logout().catch(() => undefined);
        persistHint(audience, null);
        set({ status: "anonymous", session: null });
        throw new Error(`Unexpected ${audience} session audience`);
      }
      persistHint(audience, session);
      set({ status: "authenticated", session, hint: loadHint(audience) });
      return session;
    },
    logout: async (client) => {
      try {
        await client.auth.logout();
      } finally {
        get().clear();
      }
    },
    clear: () => {
      persistHint(audience, null);
      set({ status: "anonymous", session: null, hint: null, error: null });
    },
  }));
}

export const usePlatformAuthStore = createAuthStore("platform");
export const useEnterpriseAuthStore = createAuthStore("enterprise");
