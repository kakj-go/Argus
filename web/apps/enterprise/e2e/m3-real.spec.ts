import { expect, test, type Page } from "@playwright/test";

const enabled = process.env.ARGUS_M3_E2E === "1";
const username = process.env.ARGUS_M3_ENTERPRISE_USERNAME ?? "";
const password = process.env.ARGUS_M3_ENTERPRISE_PASSWORD ?? "";

test.describe("M3 real resource flow", () => {
  test.skip(!enabled, "M3 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial" });

  test.beforeEach(async ({ page }) => {
    expect(username).not.toBe("");
    expect(password).not.toBe("");
    await login(page);
  });

  test("renders real Secret metadata without retaining its value", async ({
    page,
  }) => {
    await page.goto("/settings/secrets");
    await expect(
      page.getByText("m3-ssh-password", { exact: true }),
    ).toBeVisible();
    const browserState = await page.evaluate(() =>
      JSON.stringify({
        localStorage: window.localStorage,
        sessionStorage: window.sessionStorage,
        url: window.location.href,
      }),
    );
    expect(browserState).not.toContain("M3-e2e-ssh-password");
  });

  test("creates and disables a managed account through the real adapter", async ({
    page,
  }) => {
    await page.goto("/settings/secrets");
    const section = page.locator("section.argus-settings-section").filter({
      has: page.getByRole("heading", { name: /托管账号|Managed accounts/i }),
    });
    await expect(section).toBeVisible();
    await section
      .getByRole("button", { name: /新建托管账号|New managed account/i })
      .click();

    let drawer = page.getByRole("dialog", {
      name: /新建托管账号|New managed account/i,
    });
    await drawer.locator(".argus-select").nth(0).click();
    await page.getByRole("option", { name: "m3-public-host" }).click();
    await drawer.getByRole("textbox").fill("m3-ui-account");
    await drawer.locator(".argus-select").nth(2).click();
    await page.getByRole("option", { name: "m3-ssh (ssh)" }).click();
    const createResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/enterprise/managed-accounts") &&
        response.request().method() === "POST",
    );
    await drawer.getByRole("button", { name: /提交|Submit/i }).click();
    expect((await createResponse).status()).toBe(201);

    const row = section.getByRole("row").filter({ hasText: "m3-ui-account" });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: /编辑|Edit/i }).click();
    drawer = page.getByRole("dialog", {
      name: /编辑托管账号|Edit managed account/i,
    });
    await drawer.locator(".argus-select").nth(3).click();
    await page.getByRole("option", { name: /禁用|Disabled/i }).click();
    const updateResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/v1/enterprise/managed-accounts/") &&
        response.request().method() === "PUT",
    );
    await drawer.getByRole("button", { name: /提交|Submit/i }).click();
    expect((await updateResponse).status()).toBe(200);
    await expect(row).toContainText(/禁用|Disabled/i);
  });

  test("renders remaining Hosts after label-based revocation", async ({
    page,
  }) => {
    await page.goto("/hosts");
    await expect(
      page.getByText("m3-public-host", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText("m3-bastion-2", { exact: true })).toBeVisible();
    await expect(page.getByText("m3-bastion", { exact: true })).toHaveCount(0);
    await expect(
      page.getByText("m3-private-host", { exact: true }),
    ).toHaveCount(0);
  });

  test("cancels a Host preview through the real Pending Action endpoint", async ({
    page,
  }) => {
    await page.goto("/hosts");
    const tile = page
      .locator(".argus-host-tile")
      .filter({ hasText: "m3-public-host" });
    await tile.getByRole("button", { name: /编辑|Edit/i }).click();
    const drawer = page.getByRole("dialog", {
      name: /编辑主机.*m3-public-host|Edit host.*m3-public-host/i,
    });
    const previewResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/v1/enterprise/hosts/") &&
        response.url().endsWith("/actions/preview-update") &&
        response.request().method() === "POST",
    );
    await drawer.getByRole("button", { name: /保存|Save/i }).click();
    expect((await previewResponse).status()).toBe(201);

    const preview = drawer.locator(".argus-preview-card");
    await expect(preview).toBeVisible();
    const cancelResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/v1/enterprise/pending-actions/") &&
        response.url().endsWith("/cancel") &&
        response.request().method() === "POST",
    );
    await preview.getByRole("button", { name: /取消|Cancel/i }).click();
    expect((await cancelResponse).status()).toBe(200);
    await expect(preview).toHaveCount(0);
  });

  test("renders the in-cluster Kubernetes connection", async ({ page }) => {
    await page.goto("/kubernetes");
    await expect(
      page.getByText("m3-in-cluster", { exact: true }),
    ).toBeVisible();
  });

  test("shows a new in-cluster install command exactly once", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await page.goto("/kubernetes");
    await page
      .getByRole("button", { name: /添加集群|Add cluster/i })
      .first()
      .click();
    const drawer = page.getByRole("dialog", {
      name: /添加集群|Add cluster/i,
    });
    const inputs = drawer.locator("input");
    await inputs.nth(0).fill("m3-ui-in-cluster");
    await inputs.nth(1).fill("https://kubernetes.default.svc");
    await drawer.locator(".argus-select").nth(0).click();
    await page.getByRole("option", { name: /集群内|In-cluster/i }).click();
    await drawer.locator("textarea").nth(0).fill("team=m3\nroute=in-cluster");

    const previewResponse = page.waitForResponse(
      (response) =>
        response
          .url()
          .endsWith(
            "/api/v1/enterprise/kubernetes-clusters/actions/preview-create",
          ) && response.request().method() === "POST",
    );
    await drawer
      .getByRole("button", { name: /生成预览|Generate preview/i })
      .click();
    expect((await previewResponse).status()).toBe(201);

    const confirmResponse = page.waitForResponse(
      (response) =>
        response.url().includes("/api/v1/enterprise/pending-actions/") &&
        response.url().endsWith("/confirm") &&
        response.request().method() === "POST",
    );
    await drawer.getByRole("button", { name: /确认执行|Confirm/i }).click();
    const confirmedResponse = await confirmResponse;
    expect(confirmedResponse.status()).toBe(200);
    const confirmed = (await confirmedResponse.json()) as {
      execution?: { execution_id?: string };
    };
    const executionId = confirmed.execution?.execution_id ?? "";
    expect(executionId).not.toBe("");

    await expect(page).toHaveURL(/\/login/, { timeout: 90_000 });
    await login(page);
    await page.goto("/tasks");
    const executionRow = page.locator("tr").filter({ hasText: executionId });
    const claim = executionRow.getByRole("button", {
      name: /领取安装命令|Claim install command/i,
    });
    await expect(claim).toBeVisible({ timeout: 120_000 });
    await claim.click();
    await expect(
      page.getByText(/安装命令仅显示一次|Install command shown once/i),
    ).toBeVisible();
    const command = await page.locator(".argus-code code").textContent();
    expect(command).toContain("argus-connector");
    const browserState = await page.evaluate(() =>
      JSON.stringify({
        localStorage: window.localStorage,
        sessionStorage: window.sessionStorage,
        url: window.location.href,
      }),
    );
    expect(browserState).not.toContain(command);
    await page.getByRole("button", { name: /关闭命令|Close command/i }).click();
    await expect(claim).toHaveCount(0);
  });
});

async function login(page: Page) {
  await page.goto("/login");
  await page.locator('input[autocomplete="username"]').fill(username);
  await page.locator('input[autocomplete="current-password"]').fill(password);
  await page.locator('form button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login/);
}
