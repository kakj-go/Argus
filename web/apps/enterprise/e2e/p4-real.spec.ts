import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Locator, type Page } from "@playwright/test";

import { createMfaLogin } from "./helpers/mfa-login";

const enabled = process.env.ARGUS_P4_E2E === "1";
const username = process.env.ARGUS_P4_ENTERPRISE_USERNAME ?? "";
const password = process.env.ARGUS_P4_ENTERPRISE_PASSWORD ?? "";
const loginWithMfa = createMfaLogin("enterprise");
const hostFlow = '[data-testid="host-onboarding-flow"]';
const bastionFlow = '[data-testid="bastion-onboarding-flow"]';

test.describe("PlanV4 real onboarding acceptance", () => {
  test.skip(!enabled, "PlanV4 Kubernetes environment is not active");
  test.describe.configure({ mode: "serial", timeout: 180_000 });

  test("renders converged resources, shared wizard steps, and public DTOs without enrollment secrets", async ({
    page,
  }) => {
    const consoleMessages: string[] = [];
    page.on("console", (message) => consoleMessages.push(message.text()));
    await page.addInitScript(() => {
      window.localStorage.setItem("argus.locale", "zh-CN");
      window.localStorage.setItem("argus.theme", "light");
    });
    await loginWithMfa(page, "/login", username, password);
    await page.goto("/hosts");

    const commandScope = scopeCard(page, "p4-command-bastion");
    const directScope = scopeCard(page, "p4-direct-install");
    const tunnelScope = scopeCard(page, "p4-control-tunnel");
    await expect(commandScope).toContainText("堡垒机在线");
    await expect(directScope).toContainText("堡垒机在线");
    await expect(directScope).not.toContainText("控制隧道");
    await expect(tunnelScope).toContainText("堡垒机在线");
    await expect(tunnelScope).toContainText("控制隧道 · 隧道已建立");
    await expect(
      page.getByText("p4-restricted-member", { exact: true }),
    ).toBeVisible();

    await verifyHostWizardSteps(page);
    await verifyBastionWizardSteps(page);

    const publicPayloads = await loadP4PublicPayloads(page);
    expect(findForbiddenKeys(publicPayloads)).toEqual([]);
    const publicJson = JSON.stringify(publicPayloads);
    expect(publicJson).not.toMatch(
      /curl\s+-fsSL|--token(?:=|\s)|action_one_time_result/i,
    );

    const browserState = await page.evaluate(() => {
      const cache = (
        window as typeof window & {
          __ARGUS_DEV_QUERY_CACHE_SNAPSHOT__?: () => unknown;
        }
      ).__ARGUS_DEV_QUERY_CACHE_SNAPSHOT__?.();
      return JSON.stringify({
        url: window.location.href,
        localStorage: { ...window.localStorage },
        sessionStorage: { ...window.sessionStorage },
        queryCache: cache,
      });
    });
    expect(browserState).not.toMatch(
      /curl\s+-fsSL|--token(?:=|\s)|enrollment[_-]?token/i,
    );
    expect(consoleMessages.join("\n")).not.toMatch(
      /curl\s+-fsSL|--token(?:=|\s)|enrollment[_-]?token/i,
    );

    const variants = [
      { locale: "zh-CN", theme: "light", width: 1366, height: 768 },
      { locale: "zh-CN", theme: "dark", width: 1920, height: 1080 },
      { locale: "en-US", theme: "light", width: 1920, height: 1080 },
      { locale: "en-US", theme: "dark", width: 1366, height: 768 },
    ] as const;
    for (const variant of variants) {
      await page.setViewportSize({
        width: variant.width,
        height: variant.height,
      });
      await page.evaluate(({ locale, theme }) => {
        window.localStorage.setItem("argus.locale", locale);
        window.localStorage.setItem("argus.theme", theme);
      }, variant);
      await page.reload();
      await expect(
        page.getByText("p4-control-tunnel", { exact: true }),
      ).toBeVisible();
      await expect(page.locator("body")).not.toContainText(/hosts\.|common\./);
      await expectNoSeriousAccessibilityViolations(page);
    }
  });
});

async function verifyHostWizardSteps(page: Page) {
  await page.getByRole("button", { name: /添加普通主机|Add Host/i }).click();
  const dialog = page.getByRole("dialog", {
    name: /添加普通主机|Add Host/i,
  });
  const flow = dialog.locator(hostFlow);
  await expect(flow).toHaveAttribute("data-phase", "select_mode");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(5);
  await expect(flow.locator("form, input, textarea")).toHaveCount(0);
  await dialog.getByRole("button", { name: /下一步|Next/i }).click();
  await expect(flow).toHaveAttribute("data-phase", "details");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(0);
  await expect(dialog.getByLabel(/主机名|Host name/i)).toBeFocused();
  await dialog.getByRole("button", { name: /关闭|Close/i }).click();
  await expect(dialog).not.toBeVisible();
}

async function verifyBastionWizardSteps(page: Page) {
  await page.getByRole("button", { name: /添加堡垒机|Add Bastion/i }).click();
  const dialog = page.getByRole("dialog", {
    name: /添加堡垒机|Add Bastion/i,
  });
  const flow = dialog.locator(bastionFlow);
  await expect(flow).toHaveAttribute("data-phase", "select_mode");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(3);
  await expect(flow.locator("form, input, textarea")).toHaveCount(0);
  await dialog
    .getByRole("button", {
      name: /平台代装 \+ 控制隧道|Platform install \+ control tunnel/i,
    })
    .click();
  await dialog.getByRole("button", { name: /下一步|Next/i }).click();
  await expect(flow).toHaveAttribute("data-phase", "details");
  await expect(flow).toHaveAttribute("data-mode", "direct_install_tunnel");
  await expect(flow.locator(".argus-scenario-card")).toHaveCount(0);
  await expect(dialog.getByLabel(/名称|Name/i)).toBeFocused();
  await expect(dialog.getByLabel(/地址|Address/i)).toBeVisible();
  await dialog.getByRole("button", { name: /关闭|Close/i }).click();
  await expect(dialog).not.toBeVisible();
}

async function loadP4PublicPayloads(page: Page) {
  const hosts = await getJson(page, "/api/v1/enterprise/hosts?limit=200");
  const scopes = await getJson(
    page,
    "/api/v1/enterprise/bastion-scopes?limit=200",
  );
  const connectors = await getJson(
    page,
    "/api/v1/enterprise/connectors?limit=200",
  );
  const scopeItems = objectItems(scopes).filter((scope) =>
    String(scope.name ?? "").startsWith("p4-"),
  );
  const operationIds = scopeItems
    .map((scope) => objectValue(scope.onboarding).operation_id)
    .filter(
      (value): value is string => typeof value === "string" && value !== "",
    );
  const operations: unknown[] = [];
  for (const id of operationIds) {
    operations.push(
      await getJson(
        page,
        `/api/v1/enterprise/connector-install-operations/${id}`,
      ),
    );
  }

  const tunnel = scopeItems.find((scope) => scope.name === "p4-control-tunnel");
  const direct = scopeItems.find((scope) => scope.name === "p4-direct-install");
  expect(tunnel?.control_tunnel_status).toBe("established");
  expect(direct?.control_tunnel_status).toBeUndefined();
  expect(objectValue(tunnel?.onboarding).state).toBe("registered");
  expect(objectValue(direct?.onboarding).state).toBe("registered");

  return { hosts, scopes, connectors, operations };
}

async function getJson(page: Page, path: string): Promise<unknown> {
  let response:
    | {
        body: string;
        ok: boolean;
        status: number;
      }
    | undefined;
  let lastNetworkError: unknown;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      response = await page.evaluate(async (requestPath) => {
        const result = await fetch(requestPath, {
          cache: "no-store",
          credentials: "same-origin",
          headers: { Accept: "application/json" },
        });
        return {
          body: await result.text(),
          ok: result.ok,
          status: result.status,
        };
      }, path);
      break;
    } catch (error) {
      lastNetworkError = error;
      if (attempt < 2) {
        await page.waitForTimeout(250 * (attempt + 1));
      }
    }
  }
  if (!response) {
    throw lastNetworkError instanceof Error
      ? lastNetworkError
      : new Error(`${path} failed before receiving an HTTP response`);
  }
  expect(response.ok, `${path} returned ${response.status}`).toBe(true);
  return JSON.parse(response.body) as unknown;
}

function objectItems(value: unknown): Record<string, unknown>[] {
  const items = objectValue(value).items;
  return Array.isArray(items)
    ? items.filter(
        (item): item is Record<string, unknown> =>
          typeof item === "object" && item !== null && !Array.isArray(item),
      )
    : [];
}

function objectValue(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function findForbiddenKeys(value: unknown, path = "$"): string[] {
  if (Array.isArray(value)) {
    return value.flatMap((item, index) =>
      findForbiddenKeys(item, `${path}[${index}]`),
    );
  }
  if (typeof value !== "object" || value === null) return [];
  const forbidden = new Set([
    "token",
    "command",
    "install_command",
    "uninstall_command",
    "registration_token",
    "enrollment_token",
    "operation_secret",
    "secret_envelope",
  ]);
  return Object.entries(value).flatMap(([key, item]) => [
    ...(forbidden.has(key.toLowerCase()) ? [`${path}.${key}`] : []),
    ...findForbiddenKeys(item, `${path}.${key}`),
  ]);
}

function scopeCard(page: Page, name: string): Locator {
  return page.locator(".argus-scope-card").filter({ hasText: name }).first();
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
