import { defineConfig, devices } from "@playwright/test";

// 本机代理环境下保证被测入口直连：Playwright 会把 HTTPS_PROXY 等环境变量
// 传给浏览器，需在此剥离并显式声明 bypass（loopback 覆盖 mock dev server，
// argus.dev 域覆盖集群域名直连模式）。
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
process.env.NO_PROXY = "127.0.0.1,localhost,.argus.dev";
process.env.no_proxy = "127.0.0.1,localhost,.argus.dev";

const enterpriseOrigin =
  process.env.ARGUS_E2E_ENTERPRISE_ORIGIN ?? "http://127.0.0.1:4173";
const platformOrigin =
  process.env.ARGUS_E2E_PLATFORM_ORIGIN ?? "http://127.0.0.1:4174";
const cardOrigin = process.env.ARGUS_E2E_CARD_ORIGIN ?? "http://127.0.0.1:4176";
const port = (origin: string) => new URL(origin).port;

// 集群 E2E 通过域名访问 Ingress：浏览器用 host-resolver-rules 把公开域名
// 钉到负载均衡地址（无需改 /etc/hosts），自签证书在测试上下文中跳过校验。
const hostResolver = process.env.ARGUS_E2E_HOST_RESOLVER ?? "";
const chromiumLaunchOptions = hostResolver
  ? { args: [`--host-resolver-rules=${hostResolver}`] }
  : {};
const ignoreHTTPSErrors = Boolean(hostResolver);

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  retries: process.env.CI ? 2 : 0,
  reporter: "list",
  outputDir: process.env.ARGUS_E2E_ARTIFACTS ?? "test-results",
  use: {
    baseURL: enterpriseOrigin,
    trace: "retain-on-failure",
    ignoreHTTPSErrors,
  },
  webServer: process.env.ARGUS_E2E_EXTERNAL
    ? undefined
    : [
        {
          command: `node ../../../scripts/run-vite.mjs enterprise ${port(enterpriseOrigin)} mock`,
          url: enterpriseOrigin,
          reuseExistingServer: !process.env.CI,
        },
        {
          command: `node ../../../scripts/run-vite.mjs platform ${port(platformOrigin)} mock`,
          url: platformOrigin,
          reuseExistingServer: !process.env.CI,
        },
        {
          command: `node ../../../scripts/run-vite.mjs card ${port(cardOrigin)}`,
          url: cardOrigin,
          reuseExistingServer: !process.env.CI,
        },
      ],
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: chromiumLaunchOptions,
      },
    },
  ],
});
