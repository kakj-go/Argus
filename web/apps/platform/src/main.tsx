import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { ApiProvider, createConfiguredApiClient } from "@argus/api-client";
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

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 15_000, retry: 1, refetchOnWindowFocus: false },
  },
});

initializeTheme();

function syncLocale(locale: SupportedLocale) {
  void i18n.changeLanguage(locale);
}

async function bootstrap() {
  const apiClient = await createConfiguredApiClient({
    portal: "platform",
    mode: import.meta.env.VITE_API_MODE ?? "",
    base_url: import.meta.env.VITE_API_BASE_URL,
    locale: () =>
      document.documentElement.lang === "en-US" ? "en-US" : "zh-CN",
    on_authentication_invalidated: () => {
      queryClient.clear();
      usePlatformAuthStore.getState().clear();
      if (window.location.pathname !== "/login") {
        window.location.assign("/login");
      }
    },
  });
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <ThemeProvider>
        <LocaleProvider onLocaleChange={syncLocale}>
          <ApiProvider client={apiClient}>
            <QueryClientProvider client={queryClient}>
              <AuthProvider>
                <RouterProvider router={router} />
              </AuthProvider>
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
