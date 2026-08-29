import { enterpriseOrigin } from "./origins";
import { expect, test, type Page } from "@playwright/test";
import { createMfaLogin } from "./helpers/mfa-login";

const enabled = process.env.ARGUS_M5_E2E === "1";
const username = process.env.ARGUS_M5_ENTERPRISE_USERNAME ?? "";
const password = process.env.ARGUS_M5_ENTERPRISE_PASSWORD ?? "";
const cardName = process.env.ARGUS_M5_CARD_NAME ?? "";
const revision = Number(process.env.ARGUS_M5_REVISION ?? "1");
const loginWithMfa = createMfaLogin("enterprise");

test.describe("M5 real Card publication flow", () => {
  test.skip(!enabled, "M5 Kubernetes environment is not active");

  test("runs browser evidence for all scenarios and activates the selected revision", async ({
    page,
  }) => {
    await login(page);
    await page.goto(`${enterpriseOrigin}/settings/interactive-cards`);
    const row = page.locator("article").filter({ hasText: cardName }).first();
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: /配置|Configure/ }).click();

    if (revision > 1) {
      await page.getByRole("combobox").first().click();
      await page.getByRole("option", { name: new RegExp(`r${revision}\\b`) }).click();
    }

    const validate = page.getByRole("button", { name: /验证|Validate/ });
    await validate.click();
    const activate = page.getByRole("button", {
      name: revision > 1 ? /激活此版本|Activate revision/ : /启用|Enable/,
    });
    await expect(activate).toBeEnabled({ timeout: 120_000 });
    await activate.click();
    await expect(page.getByRole("button", { name: /停用|Disable/ })).toBeVisible({
      timeout: 30_000,
    });

    const state = await page.evaluate(() => ({
      localStorage: JSON.stringify(window.localStorage),
      sessionStorage: JSON.stringify(window.sessionStorage),
      url: window.location.href,
    }));
    expect(JSON.stringify(state)).not.toContain(password);
  });
});

async function login(page: Page) {
  expect(cardName).not.toBe("");
  await loginWithMfa(
    page,
    `${enterpriseOrigin}/login`,
    username,
    password,
  );
}
