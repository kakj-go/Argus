import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("argus.locale", "zh-CN");
    window.localStorage.setItem("argus.theme", "dark");
  });
  await page.goto("/login");
  // chenxi is the stable mock fixture whose MFA state starts disabled.
  await page.getByLabel("用户名").fill("chenxi");
  await page.getByLabel("密码").fill("123456");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
});

test("M8 account assurance flow keeps one-time material in dialogs", async ({
  page,
}) => {
  await page.goto("/account");
  await page.getByRole("button", { name: "设置认证器" }).click();
  const enrollment = page.getByRole("dialog", { name: "绑定认证器" });
  await expect(enrollment.getByText("JBSWY3DPEHPK3PXP")).toBeVisible();
  await enrollment.getByLabel("6 位验证码").fill("123456");
  await enrollment.getByRole("button", { name: "验证并启用" }).click();

  const recovery = page.getByRole("dialog", { name: "恢复码" });
  await expect(recovery.getByRole("listitem")).toHaveCount(10);
  await recovery.getByRole("button", { name: "OK" }).click();
  await expect(recovery).toBeHidden();
  await expect(page.getByText("JBSWY3DPEHPK3PXP")).toHaveCount(0);

  await page.getByLabel("验证码或恢复码").fill("123456");
  await page.getByRole("button", { name: "完成五分钟 Step-up" }).click();
  await expect(page.getByText("Step-up 有效至")).toBeVisible();

  await page.getByLabel("原因").fill("本地恢复演练需要紧急访问");
  await page.getByLabel("工单引用").fill("LOCAL-M8-001");
  await page.getByRole("button", { name: "创建紧急会话" }).click();
  await expect(page.getByText("LOCAL-M8-001")).toBeVisible();
  await page.getByRole("button", { name: "撤销", exact: true }).click();
  await expect(page.getByText("没有活动的 Break Glass 会话")).toBeVisible();

  const accessibility = await new AxeBuilder({ page }).analyze();
  expect(
    accessibility.violations.filter(
      (violation) =>
        violation.impact === "serious" || violation.impact === "critical",
    ),
  ).toEqual([]);
});
