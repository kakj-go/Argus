import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import {
  ApiProvider,
  createMockApiClient,
  type ArgusApiClient,
} from "@argus/api-client";
import {
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
import { router } from "./router";

/** VITE_API_MODE 控制 API 模式；当前仅实现 mock，其余值回退到 mock。 */
function createApi(): ArgusApiClient {
  const mode = import.meta.env.VITE_API_MODE ?? "mock";
  if (mode !== "mock") {
    console.warn(`[argus] VITE_API_MODE="${mode}" 暂未支持，回退到 mock`);
  }
  return createMockApiClient();
}

const apiClient = createApi();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 15_000, retry: 1, refetchOnWindowFocus: false },
  },
});

initializeTheme();

function syncLocale(locale: SupportedLocale) {
  void i18n.changeLanguage(locale);
}

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
