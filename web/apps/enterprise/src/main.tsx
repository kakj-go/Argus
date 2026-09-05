import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import {
  ApiProvider,
  createConfiguredApiClient,
  setApiErrorTranslator,
  TerminalSessionProvider,
} from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import {
  ErrorBoundary,
  initializeTheme,
  LocaleProvider,
  ThemeProvider,
  type SupportedLocale,
} from "@argus/ui";
import "@argus/ui/styles.css";
import i18n from "./i18n";
import "./styles.css";
import "./styles/shell-layout.css";
import { AuthProvider } from "./components/auth-provider";
import { loadRuntimeConfig } from "./lib/runtime-config";
import { router } from "./router";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 15_000, retry: 1, refetchOnWindowFocus: false },
  },
});

// Development-only acceptance hook. It exposes a detached snapshot so P4 E2E
// can prove that one-time results never enter TanStack Query without allowing
// tests to mutate the live cache. Production bundles remove this branch.
if (import.meta.env.DEV) {
  Object.defineProperty(window, "__ARGUS_DEV_QUERY_CACHE_SNAPSHOT__", {
    configurable: true,
    value: () =>
      structuredClone(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => ({
            queryHash: query.queryHash,
            data: query.state.data,
          })),
      ),
  });
}

initializeTheme();

setApiErrorTranslator((code, _messageKey, params) => {
  const key = `errors.codes.${code}`;
  const translated = i18n.t(key, { defaultValue: "", ...(params ?? {}) });
  return translated === key ? undefined : translated || undefined;
});

function syncLocale(locale: SupportedLocale) {
  void i18n.changeLanguage(locale);
}

async function bootstrap() {
  const mode = import.meta.env.VITE_API_MODE;
  if (mode === "real") {
    const runtime = await loadRuntimeConfig();
    if (!runtime.cardOrigin && !import.meta.env.VITE_CARD_ORIGIN) {
      throw new Error(
        "Card runtime origin is unavailable: /argus-runtime.json served neither cardOrigin nor VITE_CARD_ORIGIN was set",
      );
    }
  }
  const apiClient = await createConfiguredApiClient({
    portal: "enterprise",
    mode: mode ?? "",
    base_url: import.meta.env.VITE_API_BASE_URL,
    locale: () =>
      document.documentElement.lang === "en-US" ? "en-US" : "zh-CN",
    on_authentication_invalidated: () => {
      queryClient.clear();
      useEnterpriseAuthStore.getState().clear();
      if (window.location.pathname !== "/login") {
        window.location.assign("/login");
      }
    },
  });
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <ErrorBoundary onReset={() => queryClient.clear()}>
        <ThemeProvider>
          <LocaleProvider onLocaleChange={syncLocale}>
            <ApiProvider client={apiClient}>
              <TerminalSessionProvider>
                <QueryClientProvider client={queryClient}>
                  <AuthProvider>
                    <RouterProvider router={router} />
                  </AuthProvider>
                </QueryClientProvider>
              </TerminalSessionProvider>
            </ApiProvider>
          </LocaleProvider>
        </ThemeProvider>
      </ErrorBoundary>
    </StrictMode>,
  );
}

void bootstrap().catch((error: unknown) => {
  console.error(error);
  createRoot(document.getElementById("root")!).render(
    <main className="argus-auth-state" role="alert">
      <h1>{i18n.t("common.clientUnavailable")}</h1>
      <p>{i18n.t("common.clientUnavailableDescription")}</p>
    </main>,
  );
});
