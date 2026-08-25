export const SETUP_TOKEN_FRAGMENT_KEY = "argus_setup_token";

type SetupLocation = Pick<Location, "hash" | "pathname" | "search">;
type SetupHistory = Pick<History, "replaceState" | "state">;
type SetupFragmentTarget = {
  location: SetupLocation;
  history: SetupHistory;
  addEventListener: (type: "hashchange", listener: () => void) => void;
  removeEventListener: (type: "hashchange", listener: () => void) => void;
};

export type SetupCredentialStore = {
  clear: () => void;
  consumeFromFragment: (
    location: SetupLocation,
    history: SetupHistory,
  ) => string;
  getSnapshot: () => string;
  subscribe: (listener: () => void) => () => void;
};

/**
 * Consumes the one-time setup credential before React renders and immediately
 * removes it from the visible URL. The value is kept in memory only.
 */
export function consumeSetupTokenFromFragment(
  location: SetupLocation,
  history: SetupHistory,
): string {
  const rawFragment = location.hash.startsWith("#")
    ? location.hash.slice(1)
    : location.hash;
  const params = new URLSearchParams(rawFragment);
  const token = params.get(SETUP_TOKEN_FRAGMENT_KEY)?.trim() ?? "";
  if (!token) return "";

  params.delete(SETUP_TOKEN_FRAGMENT_KEY);
  const remainingFragment = params.toString();
  const cleanUrl = `${location.pathname}${location.search}${remainingFragment ? `#${remainingFragment}` : ""}`;
  history.replaceState(history.state, "", cleanUrl);
  return token;
}

export function createSetupCredentialStore(): SetupCredentialStore {
  let setupToken = "";
  const listeners = new Set<() => void>();

  const publish = (nextToken: string) => {
    if (setupToken === nextToken) return;
    setupToken = nextToken;
    for (const listener of listeners) listener();
  };

  return {
    clear: () => publish(""),
    consumeFromFragment: (location, history) => {
      const token = consumeSetupTokenFromFragment(location, history);
      if (token) publish(token);
      return token;
    },
    getSnapshot: () => setupToken,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

export function listenForSetupTokenFragments(
  target: SetupFragmentTarget,
  store: SetupCredentialStore,
): () => void {
  const consume = () => {
    store.consumeFromFragment(target.location, target.history);
  };
  target.addEventListener("hashchange", consume);
  return () => target.removeEventListener("hashchange", consume);
}

export const setupCredentialStore = createSetupCredentialStore();
