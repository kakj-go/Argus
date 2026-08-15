import { create } from "zustand";
import type {
  ArgusApiClient,
  LoginInput,
  SessionInfo,
} from "@argus/api-client";

const STORAGE_KEY = "argus.session";

function loadSession(): SessionInfo | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as SessionInfo) : null;
  } catch {
    return null;
  }
}

function persistSession(session: SessionInfo | null): void {
  if (typeof window === "undefined") return;
  if (session) {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
  } else {
    window.localStorage.removeItem(STORAGE_KEY);
  }
}

type AuthState = {
  /** Current session; hydrated synchronously from localStorage. */
  session: SessionInfo | null;
  /** Replaces the session after login or session refresh. */
  setSession: (session: SessionInfo | null) => void;
  login: (client: ArgusApiClient, input: LoginInput) => Promise<SessionInfo>;
  logout: (client: ArgusApiClient) => Promise<void>;
};

export const useAuthStore = create<AuthState>((set, get) => ({
  session: loadSession(),
  setSession: (session) => {
    persistSession(session);
    set({ session });
  },
  login: async (client, input) => {
    const session = await client.auth.login(input);
    get().setSession(session);
    return session;
  },
  logout: async (client) => {
    try {
      await client.auth.logout();
    } finally {
      get().setSession(null);
    }
  },
}));
