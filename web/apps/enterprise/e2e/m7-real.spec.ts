import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const enabled = process.env.ARGUS_M7_E2E === "1";
const enterpriseUsername = process.env.ARGUS_M7_ENTERPRISE_USERNAME ?? "";
const enterprisePassword = process.env.ARGUS_M7_ENTERPRISE_PASSWORD ?? "";
const clusterId = process.env.ARGUS_M7_CLUSTER_ID ?? "";
const hostId = process.env.ARGUS_M7_HOST_ID ?? "";

const variants = [
  { locale: "zh-CN", theme: "light" },
  { locale: "zh-CN", theme: "dark" },
  { locale: "en-US", theme: "light" },
  { locale: "en-US", theme: "dark" },
] as const;

for (const variant of variants) test.describe(`M7 real telemetry flow ${variant.locale} ${variant.theme}`, () => {
  test.skip(!enabled, "M7 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial" });

  test.beforeEach(async ({ page }) => {
    await page.addInitScript(({ locale, theme }) => {
      window.localStorage.setItem("argus.locale", locale);
      window.localStorage.setItem("argus.theme", theme);
    }, variant);
    await login(page);
  });

  test("renders authorized Kubernetes Metrics, Logs, and Traces", async ({
    page,
  }) => {
    expect(clusterId).not.toBe("");
    await page.goto(`/kubernetes/${clusterId}`);

    await page.getByRole("tab", { name: /指标|Metrics/ }).click();
    await expect(page.getByRole("img", { name: /指标时间序列|metric time series/i })).toBeVisible();

    await page.getByRole("tab", { name: /日志|Logs/ }).click();
    const logRegion = page.getByRole("region", { name: /条日志|log records/i });
    await expect(logRegion).toBeVisible();
    await expect(logRegion.getByRole("row").nth(1)).toBeVisible();

    await page.getByRole("tab", { name: /链路|Traces/ }).click();
    await expect(page.getByRole("list", { name: /条 Trace|traces/i })).toBeVisible();
    await expect(
      page.getByRole("listitem").filter({ hasText: "argus-m7-e2e / argus-m7-e2e" }),
    ).toBeVisible();
    await expectNoSeriousAccessibilityViolations(page);
  });

  test("renders the converged Linux Host Collector and its three signals", async ({
    page,
  }) => {
    expect(hostId).not.toBe("");
    await page.goto(`/hosts/${hostId}`);
    await page.getByRole("tab", { name: /组件|Components/ }).click();
    await expect(page.getByText(/监控中|Monitoring/).first()).toBeVisible();
    await expect(
      page.getByText(/R(\d+) \/ (?:目标|target) R\1/),
    ).toBeVisible();

    await page.getByRole("tab", { name: /指标|Metrics/ }).click();
    await expect(
      page.getByRole("img", { name: /指标时间序列|metric time series/i }),
    ).toBeVisible();
    await page.getByRole("tab", { name: /日志|Logs/ }).click();
    await expect(
      page.getByRole("cell", {
        name: "argus m7 e2e log.host-systemd",
        exact: true,
      }),
    ).toBeVisible();
    await page.getByRole("tab", { name: /链路|Traces/ }).click();
    await expect(
      page.getByText("argus-m7-e2e.host-systemd").first(),
    ).toBeVisible();
    await expectNoSeriousAccessibilityViolations(page);
  });

  test("shows retention, usage, and the locked support matrix", async ({
    page,
  }) => {
    await page.goto("/settings/org");
    await page.getByRole("tab", { name: /遥测|Telemetry/ }).click();

    await expect(page.getByText(/Evaluation 保留期|Evaluation retention/)).toBeVisible();
    await expect(page.getByRole("heading", { name: /近月用量|Monthly usage/ })).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "linux_arm64", exact: true }).first(),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "windows_amd64", exact: true }).first(),
    ).toBeVisible();
    await expect(page.getByText(/待实体验证|Validation pending/).first()).toBeVisible();
    await expectNoSeriousAccessibilityViolations(page);
  });
});

async function login(page: Page) {
  expect(enterpriseUsername).not.toBe("");
  expect(enterprisePassword).not.toBe("");
  await page.goto("/login");
  await page.locator('input[autocomplete="username"]').fill(enterpriseUsername);
  await page.locator('input[autocomplete="current-password"]').fill(enterprisePassword);
  await page.locator('form button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login/);
}

async function expectNoSeriousAccessibilityViolations(page: Page) {
  const result = await new AxeBuilder({ page }).analyze();
  expect(
    result.violations.filter(
      (violation) =>
        violation.impact === "serious" || violation.impact === "critical",
    ),
  ).toEqual([]);
}
