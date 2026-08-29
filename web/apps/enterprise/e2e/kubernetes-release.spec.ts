import { enterpriseOrigin, platformOrigin, cardOrigin } from "./origins";
import { expect, test } from "@playwright/test";

test.skip(
  !process.env.ARGUS_E2E_EXTERNAL,
  "requires a deployed Kubernetes release",
);

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("argus.locale", "zh-CN");
    window.localStorage.setItem("argus.theme", "dark");
  });
});

test("all Web origins serve deep links after refresh", async ({ page }) => {
  for (const target of [
    { url: `${enterpriseOrigin}/hosts`, root: "#root", populated: true },
    { url: `${platformOrigin}/sandbox`, root: "#root", populated: true },
    {
      url: `${cardOrigin}/runtime`,
      root: "#card-root",
      populated: false,
    },
  ]) {
    const response = await page.goto(target.url);
    expect(response?.status()).toBe(200);
    await page.reload();
    const root = page.locator(target.root);
    await expect(root).toBeAttached();
    if (target.populated) await expect(root).not.toBeEmpty();
  }
});

test("platform login reaches enterprise and sandbox administration", async ({
  page,
}) => {
  await page.goto(`${platformOrigin}/login?initialized=true&reset=1`);
  await page.getByLabel("用户名").fill("admin");
  await page.getByLabel("密码").fill("123456");
  await page.getByRole("button", { name: "登录", exact: true }).click();

  await page.getByRole("link", { name: "企业管理", exact: true }).click();
  await expect(page).toHaveURL(/\/enterprises$/);
  await expect(page.getByRole("heading", { name: "企业管理" })).toBeVisible();
  await expect(page.getByRole("button", { name: "创建企业" })).toBeVisible();

  await page.getByRole("link", { name: "OpenSandbox" }).click();
  await expect(page).toHaveURL(/\/sandbox$/);
  await expect(
    page.getByRole("heading", { name: "OpenSandbox 基座" }),
  ).toBeVisible();
  await expect(page.getByRole("tab", { name: "服务连接" })).toBeVisible();
});

test("setup completes once and remains permanently locked", async ({
  page,
}) => {
  await page.goto(
    `${platformOrigin}/login?initialized=false&reset=1#argus_setup_token=stp_e2e_release`,
  );
  await expect(page).toHaveURL(
    `${platformOrigin}/login?initialized=false&reset=1`,
  );
  await expect(page.getByLabel("Setup Token")).toHaveCount(0);

  await page.getByLabel("平台显示名称").fill("Argus Evaluation");
  await page.getByLabel("外部访问地址").fill(`${enterpriseOrigin}`);
  await page.getByLabel("登录名").fill("e2eadmin");
  await page.locator('input[name="admin.displayName"]').fill("E2E 管理员");
  await page.getByLabel("邮箱").fill("e2e@example.com");
  await page.locator('input[name="admin.password"]').fill("SecureRelease2026!");
  await page
    .locator('input[name="admin.confirmPassword"]')
    .fill("SecureRelease2026!");
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "确认初始化" }).click();

  await expect(page).toHaveURL(`${platformOrigin}/login`);
  await expect(page.locator('input[autocomplete="username"]')).toBeVisible();
  await page.reload();
  await expect(page).toHaveURL(`${platformOrigin}/login`);
  await expect(page.getByLabel("Setup Token")).toHaveCount(0);
});
