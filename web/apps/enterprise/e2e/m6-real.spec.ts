import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { createMfaLogin, createMfaProof } from "./helpers/mfa-login";

const enabled = process.env.ARGUS_M6_E2E === "1";
const username = process.env.ARGUS_M6_ENTERPRISE_USERNAME ?? "";
const password = process.env.ARGUS_M6_ENTERPRISE_PASSWORD ?? "";
const sshHostId = process.env.ARGUS_M6_SSH_HOST_ID ?? "";
const winrsHostId = process.env.ARGUS_M6_WINRS_HOST_ID ?? "";
const loginWithMfa = createMfaLogin("enterprise");
const nextEnterpriseMfaProof = createMfaProof("enterprise");

test.describe("M6 real remote access flow", () => {
  test.skip(!enabled, "M6 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial", timeout: 120_000 });

  test("renders grants and completes a WinRS PowerShell session with recording", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    const websocketURLs: string[] = [];
    const sockets: Array<{ url(): string; isClosed(): boolean }> = [];
    page.on("websocket", (socket) => {
      websocketURLs.push(socket.url());
      sockets.push(socket);
    });
    await login(page);

    await page.goto("/settings/org");
    await page.getByRole("tab", { name: /远程访问|Remote access/i }).click();
    const winrsGrant = page
      .getByRole("row")
      .filter({ hasText: username })
      .filter({ hasText: "m6-winrs-host" });
    await expect(winrsGrant).toBeVisible();
    await expect(winrsGrant).toContainText("argus");
    await expect(winrsGrant).toContainText("winrs");

    await page.goto(`/hosts/${winrsHostId}`);
    await page.getByRole("tab", { name: /终端与会话|Terminal/i }).click();
    await page.getByLabel(/登录账号|Account/i).click();
    await page.getByRole("option", { name: "argus" }).click();
    await page.getByLabel(/事由|Reason/i).fill("M6 browser WinRS recording");
    await page.getByRole("button", { name: /建立会话|Open session/i }).click();
    await expect(
      page.getByRole("dialog", {
        name: /验证后建立远程会话|Verify before opening/i,
      }),
    ).toBeVisible();
    await page
      .getByLabel(/验证码或恢复码|Authenticator or recovery code/i)
      .fill(await nextEnterpriseMfaProof());
    await page
      .getByRole("button", { name: /验证并继续|Verify and continue/i })
      .click();

    const input = page.getByRole("textbox", {
      name: /输入命令|Type a command/i,
    });
    await expect(input).toBeVisible({ timeout: 30_000 });
    const dock = page.getByRole("region", {
      name: /远程终端|Remote terminal/i,
    });
    await expect(dock).toBeVisible();
    const separator = dock.getByRole("separator");
    await expect(separator).toHaveAttribute("aria-valuenow", "50");
    const separatorBox = await separator.boundingBox();
    const workspaceBox = await page.locator(".argus-app-workspace").boundingBox();
    expect(separatorBox).not.toBeNull();
    expect(workspaceBox).not.toBeNull();
    await page.mouse.move(separatorBox!.x + separatorBox!.width / 2, separatorBox!.y + 5);
    await page.mouse.down();
    await page.mouse.move(workspaceBox!.x + workspaceBox!.width / 2, workspaceBox!.y + workspaceBox!.height * 0.25);
    await page.mouse.up();
    const resizedPercent = Number(await separator.getAttribute("aria-valuenow"));
    expect(resizedPercent).toBeGreaterThanOrEqual(20);
    expect(resizedPercent).toBeLessThanOrEqual(80);
    expect(resizedPercent).not.toBe(50);
    await input.fill("whoami");
    await input.press("Enter");
    await expect(page.getByText("argus\\m6-e2e", { exact: false })).toBeVisible(
      { timeout: 30_000 },
    );
    expect(
      websocketURLs.some(
        (url) => url.includes(`/v1/sessions/`) && !url.includes("ticket="),
      ),
    ).toBe(true);
    expect(sockets.length).toBeGreaterThan(0);
    expect(sockets[0]!.isClosed()).toBe(false);

    await dock.getByRole("button", { name: /收起终端|Collapse terminal/i }).click();
    await expect(dock).toBeHidden();
    await expect(
      page.getByRole("button", { name: /展开终端|Open terminal/i }),
    ).toBeVisible();
    expect(sockets[0]!.isClosed()).toBe(false);
    await page.getByRole("button", { name: /展开终端|Open terminal/i }).click();
    await expect(dock).toBeVisible();

    await dock.getByRole("button", { name: /强制终止|Terminate/i }).click();
    await expect(
      page.getByRole("button", { name: /收起终端|Collapse terminal/i }),
    ).toBeVisible();
    const recording = page
      .getByRole("button", { name: /录像|Recording/i })
      .last();
    await expect(recording).toBeVisible({ timeout: 30_000 });
    await recording.click();
    await expect(page.getByText(/会话录像|Session recording/i)).toBeVisible();
    await expect(page.getByLabel(/录像进度|Recording position/i)).toBeVisible();

    const browserState = await page.evaluate(() =>
      JSON.stringify({
        localStorage: window.localStorage,
        sessionStorage: window.sessionStorage,
        url: window.location.href,
      }),
    );
    expect(browserState).not.toContain("M6-e2e-winrs-password");
    expect(browserState).not.toContain("argus.remote_access/v1");
  });

  test("renders the SSH PTY prompt, forwards input, and switches dock placement", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await login(page);

    await page.goto(`/hosts/${sshHostId}`);
    await page.getByRole("tab", { name: /终端与会话|Terminal/i }).click();
    await page.getByLabel(/登录账号|Account/i).click();
    await page.getByRole("option", { name: "argus" }).click();
    await page.getByLabel(/事由|Reason/i).fill("M6 browser SSH PTY interaction");
    await page.getByRole("button", { name: /建立会话|Open session/i }).click();
    await expect(
      page.getByRole("dialog", {
        name: /验证后建立远程会话|Verify before opening/i,
      }),
    ).toBeVisible();
    await page
      .getByLabel(/验证码或恢复码|Authenticator or recovery code/i)
      .fill(await nextEnterpriseMfaProof());
    await page
      .getByRole("button", { name: /验证并继续|Verify and continue/i })
      .click();

    const dock = page.getByRole("region", {
      name: /远程终端|Remote terminal/i,
    });
    await expect(dock).toBeVisible({ timeout: 30_000 });
    // 就绪即代表远端 shell 已在运行：提示符与 banner 由远端输出，前端不伪造。
    const xterm = dock.locator(".argus-terminal__xterm");
    await expect(xterm).toContainText("Argus M6 SSH PTY ready", {
      timeout: 30_000,
    });

    await xterm.click();
    await page.keyboard.type("whoami");
    await page.keyboard.press("Enter");
    await expect(xterm).toContainText("argus", { timeout: 30_000 });

    // 停靠位置切换：左侧停靠后 Dock 移到主内容区左边，切回底部恢复。
    const shell = page.locator(".argus-app-shell");
    await expect(shell).toHaveAttribute("data-terminal-dock", "bottom");
    await dock
      .getByRole("button", { name: /停靠到左侧|Dock to left/i })
      .click();
    await expect(shell).toHaveAttribute("data-terminal-dock", "left");
    const dockBox = await dock.boundingBox();
    const workspaceBox = await page
      .locator(".argus-app-workspace")
      .boundingBox();
    expect(dockBox).not.toBeNull();
    expect(workspaceBox).not.toBeNull();
    expect(dockBox!.x).toBeLessThan(workspaceBox!.x);
    await dock
      .getByRole("button", { name: /停靠到底部|Dock to bottom/i })
      .click();
    await expect(shell).toHaveAttribute("data-terminal-dock", "bottom");

    // 收起/展开期间 WebSocket 与输出缓冲保持，会话可继续。
    await dock.getByRole("button", { name: /收起终端|Collapse terminal/i }).click();
    await expect(dock).toBeHidden();
    await page
      .getByRole("button", { name: /展开终端|Open terminal/i })
      .click();
    const reopened = page.getByRole("region", {
      name: /远程终端|Remote terminal/i,
    });
    await expect(reopened).toBeVisible();
    await expect(reopened.locator(".argus-terminal__xterm")).toContainText(
      "argus",
    );

    await reopened
      .getByRole("button", { name: /强制终止|Terminate/i })
      .click();
    // 会话终止后从 Dock 移除，面板随之收起。
    await expect(reopened).toBeHidden({ timeout: 30_000 });
  });

  for (const locale of ["zh-CN", "en-US"] as const) {
    for (const theme of ["light", "dark"] as const) {
      test(`remote access a11y: ${locale} ${theme}`, async ({ page }) => {
        await page.addInitScript(
          ({ nextLocale, nextTheme }) => {
            window.localStorage.setItem("argus.locale", nextLocale);
            window.localStorage.setItem("argus.theme", nextTheme);
          },
          { nextLocale: locale, nextTheme: theme },
        );
        await login(page);
        await page.goto("/settings/org");
        await page
          .getByRole("tab", { name: /远程访问|Remote access/i })
          .click();
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
  expect(sshHostId).not.toBe("");
  expect(winrsHostId).not.toBe("");
  await loginWithMfa(page, "/login", username, password);
}

async function expectAppearance(page: Page, locale: string, theme: string) {
  await expect(page.locator("html")).toHaveAttribute("lang", locale);
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
}

async function expectNoSeriousAccessibilityViolations(page: Page) {
  const result = await new AxeBuilder({ page }).analyze();
  expect(
    result.violations.filter(
      (violation) =>
        violation.impact === "serious" || violation.impact === "critical",
    ),
  ).toEqual([]);
}
