import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import {
  ApiProvider,
  createConfiguredApiClient,
  setApiErrorTranslator,
} from "@argus/api-client";
import { usePlatformAuthStore } from "@argus/auth";
import {
  initializeTheme,
  LocaleProvider,
  ThemeProvider,
  type SupportedLocale,
} from "@argus/ui";
import "@argus/ui/styles.css";
import i18n from "./i18n";
import "./styles.css";
import { AuthProvider } from "./components/auth-provider";
import { router } from "./router";
import { PlatformSetupGate } from "./setup/setup-gate";
import {
  listenForSetupTokenFragments,
  setupCredentialStore,
} from "./setup/setup-credential";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 15_000, retry: 1, refetchOnWindowFocus: false },
  },
});

initializeTheme();

setApiErrorTranslator((code, _messageKey, params) => {
  const key = `errors.codes.${code}`;
  const translated = i18n.t(key, { defaultValue: "", ...(params ?? {}) });
  return translated === key ? undefined : translated || undefined;
});

function syncLocale(locale: SupportedLocale) {
  void i18n.changeLanguage(locale);
}

async function createApi() {
  const mode = import.meta.env.VITE_API_MODE ?? "";
  if (mode === "mock") {
    const params = new URLSearchParams(window.location.search);
    if (params.get("reset") === "1") {
      for (const key of Object.keys(window.localStorage)) {
        if (key.startsWith("argus-mock:")) window.localStorage.removeItem(key);
      }
    }
    const initialized = params.get("initialized");
    return createConfiguredApiClient({
      portal: "platform",
      mode,
      mock:
        initialized === null
          ? undefined
          : { initialized: initialized === "true" },
      locale: () =>
        document.documentElement.lang === "en-US" ? "en-US" : "zh-CN",
      on_authentication_invalidated: handleAuthenticationInvalidated,
    });
  }
  return createConfiguredApiClient({
    portal: "platform",
    mode,
    base_url: import.meta.env.VITE_API_BASE_URL,
    locale: () =>
      document.documentElement.lang === "en-US" ? "en-US" : "zh-CN",
    on_authentication_invalidated: handleAuthenticationInvalidated,
  });
}

function handleAuthenticationInvalidated() {
  queryClient.clear();
  usePlatformAuthStore.getState().clear();
  if (window.location.pathname !== "/login") {
    window.location.assign("/login");
  }
}

async function bootstrap() {
  setupCredentialStore.consumeFromFragment(
    window.location,
    window.history,
  );
  listenForSetupTokenFragments(window, setupCredentialStore);
  const apiClient = await createApi();
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <ThemeProvider>
        <LocaleProvider onLocaleChange={syncLocale}>
          <ApiProvider client={apiClient}>
            <QueryClientProvider client={queryClient}>
              <PlatformSetupGate>
                <AuthProvider>
                  <RouterProvider router={router} />
                </AuthProvider>
              </PlatformSetupGate>
            </QueryClientProvider>
          </ApiProvider>
        </LocaleProvider>
      </ThemeProvider>
    </StrictMode>,
  );
}

void bootstrap().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  createRoot(document.getElementById("root")!).render(
    <main className="argus-auth-state" role="alert">
      <h1>Client unavailable</h1>
      <p>{message}</p>
    </main>,
  );
});
