import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { createMfaLogin } from "./helpers/mfa-login";

const enabled = process.env.ARGUS_M6_E2E === "1";
const username = process.env.ARGUS_M6_ENTERPRISE_USERNAME ?? "";
const password = process.env.ARGUS_M6_ENTERPRISE_PASSWORD ?? "";
const userId = process.env.ARGUS_M6_USER_ID ?? "";
const sshHostId = process.env.ARGUS_M6_SSH_HOST_ID ?? "";
const winrsHostId = process.env.ARGUS_M6_WINRS_HOST_ID ?? "";
const winrsAccountId = process.env.ARGUS_M6_WINRS_ACCOUNT_ID ?? "";
const loginWithMfa = createMfaLogin("enterprise");

test.describe("M6 real remote access flow", () => {
  test.skip(!enabled, "M6 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial" });

  test("renders grants and completes a WinRS PowerShell session with recording", async ({ page }) => {
    test.setTimeout(120_000);
    const websocketURLs: string[] = [];
    page.on("websocket", (socket) => websocketURLs.push(socket.url()));
    await login(page);

    await page.goto("/settings/org");
    await page.getByRole("tab", { name: /远程访问|Remote access/i }).click();
    await expect(page.getByText(userId, { exact: false }).first()).toBeVisible();
    await expect(page.getByText(winrsHostId, { exact: false }).first()).toBeVisible();

    await page.goto(`/hosts/${winrsHostId}`);
    await page.getByRole("tab", { name: /终端与会话|Terminal/i }).click();
    await page.getByLabel(/登录账号|Account/i).click();
    await page.getByRole("option", { name: "argus" }).click();
    await page.getByLabel(/事由|Reason/i).fill("M6 browser WinRS recording");
    await page.getByRole("button", { name: /建立会话|Open session/i }).click();

    const input = page.getByRole("textbox", { name: /输入命令|Type a command/i });
    await expect(input).toBeVisible({ timeout: 30_000 });
    await input.fill("whoami");
    await input.press("Enter");
    await expect(page.getByText("argus\\m6-e2e", { exact: false })).toBeVisible({ timeout: 30_000 });
    expect(websocketURLs.some((url) => url.includes(`/v1/sessions/`) && !url.includes("ticket="))).toBe(true);

    await page.getByRole("button", { name: /强制终止|Terminate/i }).click();
    const recording = page.getByRole("button", { name: /录像|Recording/i }).last();
    await expect(recording).toBeVisible({ timeout: 30_000 });
    await recording.click();
    await expect(page.getByText(/会话录像|Session recording/i)).toBeVisible();
    await expect(page.getByLabel(/录像进度|Recording position/i)).toBeVisible();

    const browserState = await page.evaluate(() => JSON.stringify({
      localStorage: window.localStorage,
      sessionStorage: window.sessionStorage,
      url: window.location.href,
    }));
    expect(browserState).not.toContain("M6-e2e-winrs-password");
    expect(browserState).not.toContain("argus.remote_access/v1");
  });

  for (const locale of ["zh-CN", "en-US"] as const) {
    for (const theme of ["light", "dark"] as const) {
      test(`remote access a11y: ${locale} ${theme}`, async ({ page }) => {
        await page.addInitScript(({ nextLocale, nextTheme }) => {
          window.localStorage.setItem("argus.locale", nextLocale);
          window.localStorage.setItem("argus.theme", nextTheme);
        }, { nextLocale: locale, nextTheme: theme });
        await login(page);
        await page.goto("/settings/org");
        await page.getByRole("tab", { name: /远程访问|Remote access/i }).click();
        await expectAppearance(page, locale, theme);
        await expectNoSeriousAccessibilityViolations(page);

        await page.goto(`/hosts/${sshHostId}`);
        await page.getByRole("tab", { name: /终端与会话|Terminal/i }).click();
        await expectAppearance(page, locale, theme);
        await expectNoSeriousAccessibilityViolations(page);
      });
    }
  }
});

async function login(page: Page) {
  expect(username).not.toBe("");
  expect(password).not.toBe("");
  expect(userId).not.toBe("");
  expect(sshHostId).not.toBe("");
  expect(winrsHostId).not.toBe("");
  expect(winrsAccountId).not.toBe("");
  await loginWithMfa(page, "/login", username, password);
}

async function expectAppearance(page: Page, locale: string, theme: string) {
  await expect(page.locator("html")).toHaveAttribute("lang", locale);
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
}

async function expectNoSeriousAccessibilityViolations(page: Page) {
  const result = await new AxeBuilder({ page }).analyze();
  expect(result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical")).toEqual([]);
}
