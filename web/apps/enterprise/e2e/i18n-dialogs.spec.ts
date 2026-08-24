import { expect, test, type Page } from "@playwright/test";

const enterpriseOrigin =
  process.env.ARGUS_E2E_ENTERPRISE_ORIGIN ?? "http://127.0.0.1:4173";
const platformOrigin =
  process.env.ARGUS_E2E_PLATFORM_ORIGIN ?? "http://127.0.0.1:4174";

async function login(page: Page, origin: string, username: string) {
  const url =
    origin === platformOrigin
      ? `${origin}/login?initialized=true&reset=1`
      : `${origin}/login`;
  await page.goto(url);
  await page.locator('input[autocomplete="username"]').fill(username);
  await page.locator('input[autocomplete="current-password"]').fill("123456");
  await page.locator('form button[type="submit"]').click();
  const mfaInput = page.locator('input[autocomplete="one-time-code"]');
  await expect
    .poll(
      async () => (await mfaInput.isVisible()) || !/\/login/.test(page.url()),
    )
    .toBe(true);
  if (await mfaInput.isVisible()) {
    await mfaInput.fill("123456");
    await page.locator('form button[type="submit"]').click();
  }
  await expect(page).not.toHaveURL(/\/login/);
}

for (const locale of ["zh-CN", "en-US"] as const) {
  test(`Kubernetes drawer resolves every visible key in ${locale}`, async ({
    page,
  }) => {
    await page.addInitScript((nextLocale) => {
      window.localStorage.setItem("argus.locale", nextLocale);
      window.localStorage.setItem("argus.theme", "light");
    }, locale);
    await login(page, enterpriseOrigin, "root");
    await page.goto(`${enterpriseOrigin}/kubernetes`);

    const addLabel = locale === "zh-CN" ? "添加集群" : "Add cluster";
    await page.getByRole("button", { name: addLabel, exact: true }).click();

    const drawer = page.getByRole("dialog");
    await expect(drawer).toBeVisible();
    await expect(
      drawer.getByLabel(locale === "zh-CN" ? "接入方式" : "Connection mode"),
    ).toBeVisible();
    await expect(drawer.getByLabel("API Server")).toBeVisible();
    await expect(drawer).not.toContainText("kubernetes.form.");
  });
}

test("Platform quota drawer resolves shared Sandbox keys", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("argus.locale", "zh-CN");
    window.localStorage.setItem("argus.theme", "light");
  });
  await login(page, platformOrigin, "admin");
  await page.getByRole("link", { name: "OpenSandbox" }).click();
  await expect(page).toHaveURL(`${platformOrigin}/sandbox`);
  await page.getByRole("tab", { name: "企业配额" }).click();
  await page.getByRole("button", { name: "编辑配额" }).first().click();

  const drawer = page.getByRole("dialog");
  await expect(drawer).toBeVisible();
  await expect(
    drawer.getByRole("group", { name: "允许的 Profile" }),
  ).toBeVisible();
  await expect(drawer.getByLabel("并发会话")).toBeVisible();
  await expect(drawer).not.toContainText("enterprises.quota.");
});
