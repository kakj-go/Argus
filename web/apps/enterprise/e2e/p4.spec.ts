import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Locator, type Page } from "@playwright/test";

const HOST_FLOW = '[data-testid="host-onboarding-flow"]';
const BASTION_FLOW = '[data-testid="bastion-onboarding-flow"]';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (!window.localStorage.getItem("argus.locale")) {
      window.localStorage.setItem("argus.locale", "zh-CN");
    }
    if (!window.localStorage.getItem("argus.theme")) {
      window.localStorage.setItem("argus.theme", "light");
    }
  });
  await login(page);
  await page.goto("/hosts");
});

test("P4-WEB-01 host modes are isolated from the details step and clean incompatible fields", async ({
  page,
}) => {
  const dialog = await openHostWizard(page);
  const flow = dialog.locator(HOST_FLOW);

  await expect(flow).toHaveAttribute("data-phase", "select_mode");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(5);
  await expect(flow.locator("form, input, textarea")).toHaveCount(0);
  await expect(dialog.locator('[aria-current="step"]')).toContainText(
    "网络环境",
  );

  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(flow).toHaveAttribute("data-phase", "details");
  await expect(flow).toHaveAttribute("data-mode", "direct_both");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(0);
  await expect(dialog.getByLabel("主机名")).toBeFocused();

  await dialog.getByLabel("主机名").fill("P4 字段保留主机");
  await dialog.getByLabel("地址").fill("10.20.30.40");
  await dialog.getByLabel("登录账号").fill("argus");
  await dialog.getByRole("button", { name: "更改", exact: true }).click();
  await expect(flow).toHaveAttribute("data-phase", "select_mode");
  await dialog.getByRole("button", { name: /^只出不进 · 自助安装/ }).click();

  const warning = page.getByRole("dialog", { name: "更改接入模式？" });
  await expect(warning).toContainText("专属字段将被清除");
  await warning.getByRole("button", { name: "确认", exact: true }).click();
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();

  await expect(flow).toHaveAttribute("data-mode", "self_enrolled");
  await expect(dialog.getByLabel("主机名")).toHaveValue("P4 字段保留主机");
  await expect(dialog.getByLabel("CPU 架构")).toBeVisible();
  await expect(dialog.getByLabel("地址")).toHaveCount(0);
  await expect(dialog.getByLabel("登录账号")).toHaveCount(0);

  await dialog.getByRole("button", { name: "更改", exact: true }).click();
  await dialog.getByRole("button", { name: /^双向可达/ }).click();
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(dialog.getByLabel("地址")).toHaveValue("");
  await expect(dialog.getByLabel("主机名")).toHaveValue("P4 字段保留主机");
});

test("P4-WEB-03 host details use a real form, focus errors, support Enter, and reset on close", async ({
  page,
}) => {
  let dialog = await openHostWizard(page);
  let flow = dialog.locator(HOST_FLOW);
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await dialog.getByLabel("主机名").fill("P4 缺字段主机");
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(dialog.getByLabel("地址")).toBeFocused();
  await expect(flow).toHaveAttribute("data-phase", "details");

  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(dialog).not.toBeVisible();

  dialog = await openHostWizard(page);
  flow = dialog.locator(HOST_FLOW);
  await expect(flow).toHaveAttribute("data-phase", "select_mode");
  await expect(flow).toHaveAttribute("data-mode", "direct_both");
  await expect(dialog.getByText("P4 缺字段主机")).toHaveCount(0);

  await dialog.getByRole("button", { name: /^只出不进 · 自助安装/ }).click();
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await dialog.getByLabel("主机名").fill("P4 Enter 自助主机");
  await dialog.getByLabel("主机名").press("Enter");
  await expect(flow).toHaveAttribute("data-phase", "confirm_command");
  await expect(dialog.getByText("新增主机 P4 Enter 自助主机")).toBeVisible();
  await dialog.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect(flow).toHaveAttribute("data-phase", "command_result");
  const command =
    (await dialog.locator(".argus-code code").textContent()) ?? "";
  expect(command).toContain("curl -fsS");
  expect(command).toContain("--insecure");
  await expectBrowserStateExcludes(page, command);
  await dialog
    .getByRole("button", { name: "我已保存，关闭", exact: true })
    .click();
  await expect(page.getByText(command, { exact: true })).toHaveCount(0);
});

test("P4-WEB-02 bastion A/B/C keep selection and details in separate steps", async ({
  page,
}) => {
  const dialog = await openBastionWizard(page);
  const flow = dialog.locator(BASTION_FLOW);

  await expect(flow).toHaveAttribute("data-phase", "select_mode");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(3);
  await expect(flow.locator("form, input, textarea")).toHaveCount(0);

  await dialog.getByRole("button", { name: /^平台代装（双向可达）/ }).click();
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(flow).toHaveAttribute("data-mode", "direct_install");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(0);
  await expect(dialog.getByLabel("名称")).toBeFocused();

  await dialog.getByLabel("名称").fill("P4 代装字段保留");
  await dialog.getByLabel("地址").fill("192.0.2.44");
  await dialog.getByLabel("登录账号").fill("argus");
  await dialog.getByRole("button", { name: "更改", exact: true }).click();
  await dialog.getByRole("button", { name: /^平台代装 \+ 控制隧道/ }).click();
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();

  await expect(flow).toHaveAttribute("data-mode", "direct_install_tunnel");
  await expect(dialog.getByLabel("名称")).toHaveValue("P4 代装字段保留");
  await expect(dialog.getByLabel("地址")).toHaveValue("192.0.2.44");
  await expect(dialog.getByLabel("登录账号")).toHaveValue("argus");

  await dialog.getByRole("button", { name: "更改", exact: true }).click();
  await dialog.getByRole("button", { name: /^堡垒机可出站访问 Argus/ }).click();
  const warning = page.getByRole("dialog", { name: "更改接入模式？" });
  await warning.getByRole("button", { name: "确认", exact: true }).click();
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(flow).toHaveAttribute("data-mode", "command");
  await expect(dialog.getByLabel("名称")).toHaveValue("P4 代装字段保留");
  await expect(dialog.getByLabel("地址")).toHaveCount(0);
});

test("P4-WEB-07 bastion detail fields keep the complete focus outline visible", async ({
  page,
}) => {
  const dialog = await openBastionWizard(page);
  const flow = dialog.locator(BASTION_FLOW);

  await dialog.getByRole("button", { name: "下一步", exact: true }).click();

  await expectFocusOutlineInsideScrollRegion(dialog.getByLabel("名称"), flow);
  await expectFocusOutlineInsideScrollRegion(dialog.getByLabel("标签"), flow);
});

test("P4-WEB-04 mode A returns one in-memory command and leaves no browser residue", async ({
  page,
}) => {
  const consoleMessages: string[] = [];
  page.on("console", (message) => consoleMessages.push(message.text()));
  const dialog = await openBastionWizard(page);
  const flow = dialog.locator(BASTION_FLOW);

  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await dialog.getByLabel("名称").fill("P4 A 命令堡垒机");
  await dialog.getByLabel("名称").press("Enter");
  await expect(flow).toHaveAttribute("data-phase", "confirm_command");
  await expect(dialog.getByText("添加堡垒机 P4 A 命令堡垒机")).toBeVisible();
  await expect(
    dialog.getByText("创建稳定的 Bastion Scope 和一次性注册信息"),
  ).toBeVisible();
  await expect(dialog.getByText("Create bastion", { exact: true })).toHaveCount(
    0,
  );
  await dialog.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect(flow).toHaveAttribute("data-phase", "command_result");

  const downloadCommand =
    (await dialog.locator(".argus-code code").textContent()) ?? "";
  expect(downloadCommand).toContain("curl -fsS");
  expect(downloadCommand).toContain("--proto '=https'");
  expect(downloadCommand).toContain("--tlsv1.2");
  expect(downloadCommand).toContain("--insecure");
  expect(downloadCommand).toContain("X-Argus-Enrollment-Token:");
  expect(downloadCommand).not.toContain("curl -fsSL");
  expect(downloadCommand).toContain("bootstrap-script?scope=linux-system");
  expect(downloadCommand).toContain("enroll_");
  expect(downloadCommand).not.toContain("\n");
  await expect(
    dialog.getByText("自签名证书快速接入", { exact: true }),
  ).toBeVisible();
  await expectBrowserStateExcludes(page, downloadCommand);
  expect(consoleMessages.join("\n")).not.toContain(downloadCommand);
  await expect(
    dialog.getByRole("button", { name: "一行安装（推荐）", exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "交互式", exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "自动化", exact: true }),
  ).toHaveCount(0);

  await dialog
    .getByRole("button", { name: "我已保存，关闭", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText(downloadCommand, { exact: true })).toHaveCount(0);
  await expectBrowserStateExcludes(page, downloadCommand);
});

test("P4-WEB-04 deleted mode A scope releases its name for recreation", async ({
  page,
}) => {
  const name = "P4 删除后同名重建";
  await createModeABastion(page, name);

  const firstCard = scopeCard(page, name);
  await expect(firstCard).toBeVisible();
  await firstCard.getByRole("button", { name: "删除" }).click();
  await firstCard.getByRole("button", { name: "确认执行" }).click();
  await expect(firstCard.getByText("已确认，等待审批通过后执行")).toBeVisible();
  await firstCard.getByRole("button", { name: "关闭" }).click();

  await logout(page, /企业超级管理员 root/);
  await login(page, "chenxi");
  await page.goto("/approvals?approval=operation&scope=mine");
  await page.getByRole("button", { name: new RegExp(name) }).click();
  const detail = page.locator(".argus-approval-detail");
  await detail.getByLabel("批准").fill("同意删除后重建验证");
  await detail.getByRole("button", { name: "批准" }).click();
  await expect
    .poll(() =>
      page.evaluate((scopeName) => {
        const raw = localStorage.getItem("argus-mock:db-v13");
        if (!raw) return true;
        const value = JSON.parse(raw) as {
          bastionScopes?: Array<{ name?: string }>;
        };
        return !value.bastionScopes?.some((scope) => scope.name === scopeName);
      }, name),
    )
    .toBe(true);

  await logout(page, /陈曦 chenxi/);
  await login(page);
  await page.goto("/hosts");
  await expect(scopeCard(page, name)).toHaveCount(0);

  await createModeABastion(page, name);
  await expect(scopeCard(page, name)).toBeVisible();
});

for (const mode of [
  {
    key: "direct_install",
    button: /^平台代装（双向可达）/,
    name: "P4 B 后台安装",
  },
  {
    key: "direct_install_tunnel",
    button: /^平台代装 \+ 控制隧道/,
    name: "P4 C 后台安装",
  },
] as const) {
  test(`P4-WEB-02 ${mode.key} enters a durable operation without exposing a command`, async ({
    page,
  }) => {
    const dialog = await openBastionWizard(page);
    const flow = dialog.locator(BASTION_FLOW);
    await dialog.getByRole("button", { name: mode.button }).click();
    await dialog.getByRole("button", { name: "下一步", exact: true }).click();
    await fillBastionDirectDetails(page, dialog, mode.name);
    await dialog.getByRole("button", { name: "下一步", exact: true }).click();

    await expect(flow).toHaveAttribute("data-phase", "verify");
    await expect(dialog.getByText("连接测试结果")).toBeVisible();
    await expect(dialog.locator(".argus-code code")).toHaveCount(0);
    await expect(dialog).not.toContainText("复制命令");
    await dialog.getByRole("button", { name: "确认执行", exact: true }).click();

    await expect(flow).toHaveAttribute("data-phase", "installing");
    await expect(dialog.getByText("Connector 安装进度")).toBeVisible();
    await expect(dialog.locator(".argus-operation-timeline")).toBeVisible();
    await expect(dialog.locator(".argus-code code")).toHaveCount(0);
    await expect(dialog).not.toContainText("一次性安装命令");
    if (mode.key === "direct_install_tunnel") {
      await expect(dialog.getByText("控制隧道状态")).toBeVisible();
    }

    await dialog
      .locator(".argus-dialog__footer")
      .getByRole("button", { name: "关闭", exact: true })
      .click();
    await expect(dialog).not.toBeVisible();
    const card = scopeCard(page, mode.name);
    await expect(card).toBeVisible();
    await expect(card).toContainText("堡垒机在线", { timeout: 10_000 });
    if (mode.key === "direct_install_tunnel") {
      await expect(card).toContainText("控制隧道 · 隧道已建立");
    } else {
      await expect(card).not.toContainText("控制隧道");
    }
  });
}

test("P4-WEB-04/06 pending cards follow the server onboarding projection", async ({
  page,
}) => {
  const available = scopeCard(page, "上海灾备堡垒机");
  await expect(
    available.getByRole("button", { name: "领取安装命令" }),
  ).toBeVisible();

  const installing = scopeCard(page, "P4 代装进行中");
  await expect(installing).toContainText("Connector 安装中");
  await installing.getByRole("button", { name: "查看安装进度" }).click();
  let operationDialog = page.getByRole("dialog", {
    name: "Connector 安装进度",
  });
  await expect(
    operationDialog.locator(".argus-operation-timeline"),
  ).toBeVisible();
  await expect(operationDialog).not.toContainText(/hosts\.|bastionOperation\./);
  await operationDialog.getByRole("button", { name: "关闭" }).click();

  const failed = scopeCard(page, "P4 隧道代装失败");
  await expect(failed).toContainText("Connector 安装失败");
  await expect(failed.getByRole("button", { name: "重试安装" })).toBeVisible();
  await failed.getByRole("button", { name: "查看失败详情" }).click();
  operationDialog = page.getByRole("dialog", { name: "Connector 安装进度" });
  await expect(operationDialog).not.toContainText("CONNECTOR_INSTALL_FAILED");
  await operationDialog.getByRole("button", { name: "关闭" }).click();

  const consumed = scopeCard(page, "P4 命令已领取");
  await expect(consumed).toContainText("安装命令已领取");
  await expect(
    consumed.getByRole("button", { name: "重新生成安装命令" }),
  ).toBeVisible();
  const approval = scopeCard(page, "P4 等待审批");
  await expect(approval).toContainText("等待审批");
  await expect(approval.getByRole("link", { name: "查看审批" })).toBeVisible();
});

const visualMatrix = [
  { locale: "zh-CN", theme: "light", width: 1366, height: 768 },
  { locale: "zh-CN", theme: "dark", width: 1920, height: 1080 },
  { locale: "en-US", theme: "light", width: 1920, height: 1080 },
  { locale: "en-US", theme: "dark", width: 1366, height: 768 },
] as const;

for (const variant of visualMatrix) {
  test(`P4-WEB-07 ${variant.locale} ${variant.theme} ${variant.width}x${variant.height}`, async ({
    page,
  }) => {
    await page.setViewportSize({
      width: variant.width,
      height: variant.height,
    });
    await page.evaluate(({ locale, theme }) => {
      localStorage.setItem("argus.locale", locale);
      localStorage.setItem("argus.theme", theme);
    }, variant);
    await page.reload();
    await expectNoSeriousAccessibilityViolations(page);
    const addHost = variant.locale === "zh-CN" ? "添加普通主机" : "Add Host";
    await page.getByRole("button", { name: addHost, exact: true }).click();
    const dialog = page.getByRole("dialog", { name: addHost });
    await expect(
      dialog.locator(HOST_FLOW).locator(".argus-scenario-card"),
    ).toHaveCount(5);
    await expect(dialog).not.toContainText(/hosts\.|common\./);
    await expectSingleWizardScrollRegion(dialog);
    await expectNoSeriousAccessibilityViolations(page, ".argus-dialog--wizard");
    await dialog
      .getByRole("button", {
        name: variant.locale === "zh-CN" ? "关闭" : "Close",
        exact: true,
      })
      .click();

    const addBastion =
      variant.locale === "zh-CN" ? "添加堡垒机" : "Add Bastion";
    await page.getByRole("button", { name: addBastion, exact: true }).click();
    const bastionDialog = page.getByRole("dialog", { name: addBastion });
    const bastionName =
      variant.locale === "zh-CN" ? "P4 双语堡垒机" : "P4 bilingual bastion";
    await bastionDialog
      .getByRole("button", {
        name: variant.locale === "zh-CN" ? "下一步" : "Next",
        exact: true,
      })
      .click();
    const nameInput = bastionDialog.getByLabel(
      variant.locale === "zh-CN" ? "名称" : "Name",
    );
    await nameInput.fill(bastionName);
    await nameInput.press("Enter");
    await expect(bastionDialog.locator(BASTION_FLOW)).toHaveAttribute(
      "data-phase",
      "confirm_command",
    );

    if (variant.locale === "zh-CN") {
      await expect(
        bastionDialog.getByText(`添加堡垒机 ${bastionName}`),
      ).toBeVisible();
      await expect(
        bastionDialog.getByText("创建稳定的 Bastion Scope 和一次性注册信息"),
      ).toBeVisible();
      await expect(
        bastionDialog.getByText("Create bastion", { exact: true }),
      ).toHaveCount(0);
    } else {
      await expect(
        bastionDialog.getByText(`Add bastion ${bastionName}`),
      ).toBeVisible();
      await expect(
        bastionDialog.getByText(
          "Create a stable Bastion Scope and one-time enrollment",
        ),
      ).toBeVisible();
      await expect(
        bastionDialog.getByText("创建稳定的 Bastion Scope 和一次性注册信息"),
      ).toHaveCount(0);
    }
    await expectNoSeriousAccessibilityViolations(page, ".argus-dialog--wizard");
  });
}

async function login(page: Page, username = "root") {
  await page.goto("/login");
  await page.locator('input[autocomplete="username"]').fill(username);
  await page.locator('input[autocomplete="current-password"]').fill("123456");
  await page.locator('form button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login/);
}

async function logout(page: Page, accountName: RegExp) {
  await page.getByRole("button", { name: accountName }).click();
  await page.getByText("退出登录", { exact: true }).click();
  await expect(page).toHaveURL(/\/login/);
}

async function openHostWizard(page: Page) {
  await page.getByRole("button", { name: "添加普通主机", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "添加普通主机" });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function openBastionWizard(page: Page) {
  await page.getByRole("button", { name: "添加堡垒机", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "添加堡垒机" });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function createModeABastion(page: Page, name: string) {
  const dialog = await openBastionWizard(page);
  await dialog.getByRole("button", { name: "下一步", exact: true }).click();
  await dialog.getByLabel("名称").fill(name);
  await dialog.getByLabel("名称").press("Enter");
  await dialog.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect(dialog.locator(BASTION_FLOW)).toHaveAttribute(
    "data-phase",
    "command_result",
  );
  await dialog
    .getByRole("button", { name: "我已保存，关闭", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
}

async function fillBastionDirectDetails(
  page: Page,
  dialog: Locator,
  name: string,
) {
  await dialog.getByLabel("名称").fill(name);
  await dialog.getByLabel("地址").fill("192.0.2.55");
  await dialog.getByLabel("登录账号").fill("argus");
  await dialog.getByLabel("登录凭据或密钥").click();
  await page.getByRole("option", { name: "prod-ssh-key" }).click();
}

function scopeCard(page: Page, name: string) {
  return page.locator(".argus-scope-card").filter({ hasText: name }).first();
}

async function expectBrowserStateExcludes(page: Page, sensitive: string) {
  const state = await page.evaluate(() => {
    const cacheSnapshot = (
      window as typeof window & {
        __ARGUS_DEV_QUERY_CACHE_SNAPSHOT__?: () => unknown;
      }
    ).__ARGUS_DEV_QUERY_CACHE_SNAPSHOT__?.();
    return JSON.stringify({
      url: window.location.href,
      localStorage: { ...window.localStorage },
      sessionStorage: { ...window.sessionStorage },
      queryCache: cacheSnapshot,
    });
  });
  expect(state).not.toContain(sensitive);
}

async function expectSingleWizardScrollRegion(dialog: Locator) {
  const scrollableClasses = await dialog.evaluate((root) =>
    Array.from(root.querySelectorAll<HTMLElement>("*"))
      .filter((element) => {
        const style = getComputedStyle(element);
        return (
          /(auto|scroll)/.test(style.overflowY) &&
          element.scrollHeight > element.clientHeight + 1
        );
      })
      .map((element) => element.className),
  );
  expect(scrollableClasses.length).toBeLessThanOrEqual(1);
  if (scrollableClasses.length === 1) {
    expect(String(scrollableClasses[0])).toContain(
      "argus-wizard-dialog__content",
    );
  }
}

async function expectFocusOutlineInsideScrollRegion(
  control: Locator,
  scrollRegion: Locator,
) {
  await control.focus();
  await expect(control).toBeFocused();

  const [controlBox, scrollRegionBox, outline] = await Promise.all([
    control.boundingBox(),
    scrollRegion.boundingBox(),
    control.evaluate((element) => {
      const style = getComputedStyle(element);
      return {
        offset: Number.parseFloat(style.outlineOffset) || 0,
        style: style.outlineStyle,
        width: Number.parseFloat(style.outlineWidth) || 0,
      };
    }),
  ]);
  expect(controlBox).not.toBeNull();
  expect(scrollRegionBox).not.toBeNull();
  expect(outline.style).not.toBe("none");
  expect(outline.width).toBeGreaterThan(0);

  const focusExtent = outline.width + Math.max(0, outline.offset);
  const controlEdges = {
    bottom: controlBox!.y + controlBox!.height + focusExtent,
    left: controlBox!.x - focusExtent,
    right: controlBox!.x + controlBox!.width + focusExtent,
    top: controlBox!.y - focusExtent,
  };
  const scrollRegionEdges = {
    bottom: scrollRegionBox!.y + scrollRegionBox!.height,
    left: scrollRegionBox!.x,
    right: scrollRegionBox!.x + scrollRegionBox!.width,
    top: scrollRegionBox!.y,
  };
  const roundingTolerance = 0.5;

  expect(controlEdges.left).toBeGreaterThanOrEqual(
    scrollRegionEdges.left - roundingTolerance,
  );
  expect(controlEdges.top).toBeGreaterThanOrEqual(
    scrollRegionEdges.top - roundingTolerance,
  );
  expect(controlEdges.right).toBeLessThanOrEqual(
    scrollRegionEdges.right + roundingTolerance,
  );
  expect(controlEdges.bottom).toBeLessThanOrEqual(
    scrollRegionEdges.bottom + roundingTolerance,
  );
}

async function expectNoSeriousAccessibilityViolations(
  page: Page,
  include?: string,
) {
  const builder = new AxeBuilder({ page });
  const result = await (include ? builder.include(include) : builder).analyze();
  expect(
    result.violations.filter(
      (violation) =>
        violation.impact === "serious" || violation.impact === "critical",
    ),
  ).toEqual([]);
}
