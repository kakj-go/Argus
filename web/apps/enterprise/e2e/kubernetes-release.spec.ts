import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("argus.locale", "zh-CN");
    window.localStorage.setItem("argus.theme", "dark");
  });
});

test("all three SPAs serve deep links after refresh", async ({ page }) => {
  for (const target of [
    "http://127.0.0.1:4173/hosts",
    "http://127.0.0.1:4174/sandbox",
    "http://127.0.0.1:4175/setup/deep-link",
  ]) {
    const response = await page.goto(target);
    expect(response?.status()).toBe(200);
    await page.reload();
    await expect(page.locator("#root")).not.toBeEmpty();
  }
});

test("platform login reaches enterprise and sandbox administration", async ({
  page,
}) => {
  await page.goto("http://127.0.0.1:4174/login");
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
  await page.goto("http://127.0.0.1:4175/");
  await page.getByLabel("Setup Token").fill("stp_e2e_release");
  await page.getByRole("button", { name: "下一步" }).click();

  await page.getByLabel("平台显示名称").fill("Argus Evaluation");
  await page.getByLabel("外部访问地址").fill("http://127.0.0.1:4173");
  await page.getByLabel("登录名").fill("e2eadmin");
  await page.getByLabel("显示名", { exact: true }).fill("E2E 管理员");
  await page.getByLabel("邮箱").fill("e2e@example.com");
  const passwords = page.locator('input[type="password"]');
  await passwords.nth(0).fill("ArgusE2Epass123!");
  await passwords.nth(1).fill("ArgusE2Epass123!");
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: "确认初始化" }).click();

  await expect(
    page.getByRole("heading", { name: "系统初始化成功" }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "系统已完成初始化" }),
  ).toBeVisible();
  await expect(page.getByLabel("Setup Token")).toHaveCount(0);
});
