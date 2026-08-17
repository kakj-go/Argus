import { expect, test, type Page } from "@playwright/test";

const enabled = process.env.ARGUS_M4_E2E === "1";
const enterpriseUsername = process.env.ARGUS_M4_ENTERPRISE_USERNAME ?? "";
const enterprisePassword = process.env.ARGUS_M4_ENTERPRISE_PASSWORD ?? "";
const platformUsername = process.env.ARGUS_M4_PLATFORM_USERNAME ?? "";
const platformPassword = process.env.ARGUS_M4_PLATFORM_PASSWORD ?? "";

test.describe("M4 real Agent and governance flow", () => {
  test.skip(!enabled, "M4 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial" });

  test("renders persisted Chat, model, approval, execution, and automation facts", async ({
    page,
  }) => {
    await login(
      page,
      "http://127.0.0.1:4173/login",
      enterpriseUsername,
      enterprisePassword,
    );

    await page.goto("http://127.0.0.1:4173/");
    await expect(page.getByText("M4 recovery flow", { exact: true })).toBeVisible();

    await page.goto("http://127.0.0.1:4173/settings/ai");
    await expect(page.getByText("M4 Replay chat_completions", { exact: true })).toBeVisible();
    await expect(page.getByText("M4 Replay responses", { exact: true })).toBeVisible();

    await page.goto("http://127.0.0.1:4173/approvals");
    await expect(
      page.getByRole("button", { name: /Update host.*(Succeeded|已成功)/ }).first(),
    ).toBeVisible();

    await page.goto("http://127.0.0.1:4173/tasks");
    await expect(page.getByText(/Succeeded|已成功/).first()).toBeVisible();

    await page.goto("http://127.0.0.1:4173/automations");
    await expect(page.getByText("M4 host inventory", { exact: true })).toBeVisible();
    await expectNoCredentialInBrowserState(page, "m4-write-only-key");
  });

  test("renders governed Sandbox objects in the platform audience", async ({
    page,
  }) => {
    await login(
      page,
      "http://127.0.0.1:4174/login",
      platformUsername,
      platformPassword,
    );
    await page.goto("http://127.0.0.1:4174/sandbox");
    await expect(page.getByText("M4 OpenSandbox", { exact: true })).toBeVisible();
    await page.getByRole("tab", { name: "Sandbox Profiles" }).click();
    await expect(page.getByText("M4 smoke", { exact: true })).toBeVisible();
    await expectNoCredentialInBrowserState(page, "write-only");
  });
});

async function login(
  page: Page,
  url: string,
  username: string,
  password: string,
) {
  expect(username).not.toBe("");
  expect(password).not.toBe("");
  await page.goto(url);
  await page.locator('input[autocomplete="username"]').fill(username);
  await page.locator('input[autocomplete="current-password"]').fill(password);
  await page.locator('form button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login/);
}

async function expectNoCredentialInBrowserState(
  page: Page,
  credential: string,
) {
  const state = await page.evaluate(() => ({
    localStorage: JSON.stringify(window.localStorage),
    sessionStorage: JSON.stringify(window.sessionStorage),
    url: window.location.href,
  }));
  expect(JSON.stringify(state)).not.toContain(credential);
}
