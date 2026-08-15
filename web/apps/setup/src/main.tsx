import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
import App from "./app";

/**
 * Setup 应用当前仅支持 mock API（VITE_API_MODE 默认 "mock"）。
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
function createApi(): ArgusApiClient {
  const mode = import.meta.env.VITE_API_MODE ?? "mock";
  if (mode !== "mock") {
    console.warn(`[argus] VITE_API_MODE="${mode}" 暂未支持，回退到 mock`);
  }
  const params = new URLSearchParams(window.location.search);
  const fromUrl = params.get("initialized");
  const fromStore = window.localStorage.getItem("argus.setup.initialized");
  const initialized = fromUrl
    ? fromUrl === "true"
    : fromStore
      ? fromStore === "true"
      : false;
  const client = createMockApiClient({ initialized });
  if (params.get("reset")) client.reset();
  return client;
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
            <App />
          </QueryClientProvider>
        </ApiProvider>
      </LocaleProvider>
    </ThemeProvider>
  </StrictMode>,
);
