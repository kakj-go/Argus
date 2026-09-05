import { enterpriseOrigin, platformOrigin } from "./origins";
import { expect, test, type Page } from "@playwright/test";

const enabled = process.env.ARGUS_M2_E2E === "1";
const platformUsername = process.env.ARGUS_M2_PLATFORM_USERNAME ?? "";
const platformPassword = process.env.ARGUS_M2_PLATFORM_PASSWORD ?? "";
const platformMfaCode = process.env.ARGUS_M2_PLATFORM_MFA_CODE ?? "";
const enterpriseUsername = process.env.ARGUS_M2_ENTERPRISE_USERNAME ?? "";
const enterprisePassword = process.env.ARGUS_M2_ENTERPRISE_PASSWORD ?? "";
const enterpriseMfaCode = process.env.ARGUS_M2_ENTERPRISE_MFA_CODE ?? "";

test.describe("M2 real identity flow", () => {
  test.skip(!enabled, "M2 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial" });

  test("platform setup route is locked after initialization", async ({
    page,
  }) => {
    await page.goto(`${platformOrigin}/setup`);
    await expect(page).toHaveURL(`${platformOrigin}/login`);
    await expect(page.locator('input[name="admin.password"]')).toHaveCount(0);
    await expect(
      page.locator('input[name="admin.confirmPassword"]'),
    ).toHaveCount(0);
    await expect(page.locator('input[autocomplete="username"]')).toBeVisible();
    await expect(
      page.locator('input[autocomplete="current-password"]'),
    ).toBeVisible();
  });

  test("platform portal authenticates against the real audience", async ({
    page,
  }) => {
    await login(
      page,
      `${platformOrigin}/login`,
      platformUsername,
      platformPassword,
      platformMfaCode,
    );
    await expect(page).toHaveURL(`${platformOrigin}/`);
    await page.goto(`${platformOrigin}/pki`);
    await expect(page.getByRole("heading", { name: /PKI/ })).toBeVisible();
    await expect(page.getByText("Bundle SHA-256", { exact: true })).toBeVisible();
    await expect(page.locator(".argus-pki-fingerprints code").first()).toBeVisible();
    await expectNoCredentialInBrowserState(page, platformPassword);
  });

  test("enterprise portal restores a real session without retaining credentials", async ({
    page,
  }) => {
    await login(
      page,
      `${enterpriseOrigin}/login`,
      enterpriseUsername,
      enterprisePassword,
      enterpriseMfaCode,
    );
    await expect(page).not.toHaveURL(/\/login/);
    await page.reload();
    await expect(page).not.toHaveURL(/\/login/);
    await page.goto(`${enterpriseOrigin}/settings/org`);
    await expect(page).not.toHaveURL(/\/login/);
    await expectNoCredentialInBrowserState(page, enterprisePassword);
  });
});

async function login(
  page: Page,
  url: string,
  username: string,
  password: string,
  mfaCode = "",
) {
  expect(username).not.toBe("");
  expect(password).not.toBe("");
  await page.goto(url);
  await page.locator('input[autocomplete="username"]').fill(username);
  await page.locator('input[autocomplete="current-password"]').fill(password);
  await page.locator('form button[type="submit"]').click();
  const mfaInput = page.locator('input[autocomplete="one-time-code"]');
  await expect
    .poll(
      async () => (await mfaInput.isVisible()) || !/\/login/.test(page.url()),
    )
    .toBe(true);
  if (await mfaInput.isVisible()) {
    expect(mfaCode).not.toBe("");
    await mfaInput.fill(mfaCode);
    await page.locator('form button[type="submit"]').click();
  }
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
