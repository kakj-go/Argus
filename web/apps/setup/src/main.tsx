import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  ApiProvider,
  createConfiguredApiClient,
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
import App from "./app";

/**
 * Setup 应用必须通过 VITE_API_MODE 显式选择 mock 或 real。
 *
 * 演示用初始化状态控制（优先级从高到低）：
 * 1. URL 参数 `?initialized=true|false`
 * 2. localStorage `argus.setup.initialized` = "true" | "false"
 * 3. 默认 false —— 未初始化，进入初始化向导
 *
 * mock 数据持久化在 localStorage（`argus-mock:*`）：提交成功后平台状态即为
 * initialized，刷新后向导保持关闭；追加 `?reset=1` 可清空并重新播种，
 * 恢复未初始化状态以便重新演练。
 */
async function createApi() {
  const mode = import.meta.env.VITE_API_MODE;
  if (mode === "real" && !import.meta.env.VITE_PLATFORM_URL) {
    throw new Error("VITE_PLATFORM_URL is required when VITE_API_MODE=real");
  }
  if (mode === "mock") {
    const params = new URLSearchParams(window.location.search);
    const fromUrl = params.get("initialized");
    const fromStore = window.localStorage.getItem("argus.setup.initialized");
    const initialized = fromUrl
      ? fromUrl === "true"
      : fromStore
        ? fromStore === "true"
        : false;
    if (params.get("reset")) {
      for (const key of Object.keys(window.localStorage)) {
        if (key.startsWith("argus-mock:")) window.localStorage.removeItem(key);
      }
    }
    return createConfiguredApiClient({
      mode,
      mock: { initialized },
      locale: () =>
        document.documentElement.lang === "en-US" ? "en-US" : "zh-CN",
    });
  }
  return createConfiguredApiClient({
    mode: mode ?? "",
    base_url: import.meta.env.VITE_API_BASE_URL,
    locale: () =>
      document.documentElement.lang === "en-US" ? "en-US" : "zh-CN",
  });
}

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
  const apiClient = await createApi();
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <ThemeProvider>
        <LocaleProvider onLocaleChange={syncLocale}>
          <ApiProvider client={apiClient}>
            <QueryClientProvider client={queryClient}>
              <App />
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
