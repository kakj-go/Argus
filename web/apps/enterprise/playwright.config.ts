import { defineConfig, devices } from "@playwright/test";

// 本机代理环境下保证 loopback 直连：Playwright 会把 HTTPS_PROXY 等
// 环境变量传给浏览器，需在此剥离并显式声明 bypass。
for (const key of [
  "http_proxy",
  "https_proxy",
  "all_proxy",
  "HTTP_PROXY",
  "HTTPS_PROXY",
  "ALL_PROXY",
]) {
  delete process.env[key];
}
process.env.NO_PROXY = "127.0.0.1,localhost";
process.env.no_proxy = "127.0.0.1,localhost";

const enterpriseOrigin =
  process.env.ARGUS_E2E_ENTERPRISE_ORIGIN ?? "http://127.0.0.1:4173";
const platformOrigin =
  process.env.ARGUS_E2E_PLATFORM_ORIGIN ?? "http://127.0.0.1:4174";
const setupOrigin =
  process.env.ARGUS_E2E_SETUP_ORIGIN ?? "http://127.0.0.1:4175";
const cardOrigin = process.env.ARGUS_E2E_CARD_ORIGIN ?? "http://127.0.0.1:4176";
const port = (origin: string) => new URL(origin).port;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  retries: process.env.CI ? 2 : 0,
  reporter: "list",
  outputDir: process.env.ARGUS_E2E_ARTIFACTS ?? "test-results",
  use: { baseURL: enterpriseOrigin, trace: "retain-on-failure" },
  webServer: process.env.ARGUS_E2E_EXTERNAL
    ? undefined
    : [
        {
          command: `VITE_API_MODE=mock pnpm exec vite --host 0.0.0.0 --port ${port(enterpriseOrigin)}`,
          url: enterpriseOrigin,
          reuseExistingServer: !process.env.CI,
        },
        {
          command: `VITE_API_MODE=mock pnpm --filter @argus/platform exec vite --host 0.0.0.0 --port ${port(platformOrigin)}`,
          url: platformOrigin,
          reuseExistingServer: !process.env.CI,
        },
        {
          command: `VITE_API_MODE=mock VITE_PLATFORM_URL=${platformOrigin}/login pnpm --filter @argus/setup exec vite --host 0.0.0.0 --port ${port(setupOrigin)}`,
          url: setupOrigin,
          reuseExistingServer: !process.env.CI,
        },
        {
          command: `pnpm --filter @argus/card-runtime exec vite --host 0.0.0.0 --port ${port(cardOrigin)}`,
          url: cardOrigin,
          reuseExistingServer: !process.env.CI,
        },
      ],
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
