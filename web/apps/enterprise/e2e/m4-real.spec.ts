import { enterpriseOrigin, platformOrigin } from "./origins";
import { expect, test, type Page } from "@playwright/test";
import { createMfaLogin } from "./helpers/mfa-login";

const enabled = process.env.ARGUS_M4_E2E === "1";
const enterpriseUsername = process.env.ARGUS_M4_ENTERPRISE_USERNAME ?? "";
const enterprisePassword = process.env.ARGUS_M4_ENTERPRISE_PASSWORD ?? "";
const platformUsername = process.env.ARGUS_M4_PLATFORM_USERNAME ?? "";
const platformPassword = process.env.ARGUS_M4_PLATFORM_PASSWORD ?? "";
const enterpriseLogin = createMfaLogin("enterprise");
const platformLogin = createMfaLogin("platform");

test.describe("M4 real Agent and governance flow", () => {
  test.skip(!enabled, "M4 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial", timeout: 120_000 });

  test("renders persisted Chat, model, approval, and execution facts", async ({
    page,
  }) => {
    await enterpriseLogin(
      page,
      `${enterpriseOrigin}/login`,
      enterpriseUsername,
      enterprisePassword,
    );

    await page.goto(`${enterpriseOrigin}/`);
    await expect(page.getByText("M4 recovery flow", { exact: true })).toBeVisible();

    await page.goto(`${enterpriseOrigin}/settings/ai`);
    await expect(page.getByText("M4 Replay chat_completions", { exact: true })).toBeVisible();
    await expect(page.getByText("M4 Replay responses", { exact: true })).toBeVisible();

    await page.goto(`${enterpriseOrigin}/approvals`);
    await expect(
      page.getByRole("button", { name: /Update host.*(Succeeded|已成功)/ }).first(),
    ).toBeVisible();

    await page.goto(`${enterpriseOrigin}/tasks`);
    await expect(page.getByText(/Succeeded|已成功/).first()).toBeVisible();
    const refreshButton = page.getByRole("button", { name: /Refresh|刷新/ });
    await expect(refreshButton).toBeVisible();
    await expect(
      page.locator(".argus-page__header").getByRole("button", {
        name: /Refresh|刷新/,
      }),
    ).toHaveCount(1);
    const refreshBox = await refreshButton.boundingBox();
    expect(refreshBox).not.toBeNull();
    expect(refreshBox!.width).toBeLessThanOrEqual(40);

    await expectNoCredentialInBrowserState(page, "m4-write-only-key");
  });

  test("renders governed Sandbox objects in the platform audience", async ({
    page,
  }) => {
    await platformLogin(
      page,
      `${platformOrigin}/login`,
      platformUsername,
      platformPassword,
    );
    await page.goto(`${platformOrigin}/sandbox`);
    await expect(page.getByText("M4 OpenSandbox", { exact: true })).toBeVisible();
    await page.getByRole("tab", { name: "Sandbox Profiles" }).click();
    await expect(page.getByText("M4 smoke", { exact: true })).toBeVisible();
    await expectNoCredentialInBrowserState(page, "write-only");
  });
});

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
