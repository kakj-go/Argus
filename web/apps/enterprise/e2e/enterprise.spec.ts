import { expect, test, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const enterpriseOrigin =
  process.env.ARGUS_E2E_ENTERPRISE_ORIGIN ?? "http://127.0.0.1:4173";
const platformOrigin =
  process.env.ARGUS_E2E_PLATFORM_ORIGIN ?? "http://127.0.0.1:4174";
const cardOrigin = process.env.ARGUS_E2E_CARD_ORIGIN ?? "http://127.0.0.1:4176";

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    if (!window.localStorage.getItem("argus.locale")) {
      window.localStorage.setItem("argus.locale", "zh-CN");
    }
    if (!window.localStorage.getItem("argus.theme")) {
      window.localStorage.setItem("argus.theme", "dark");
    }
  });
});

async function login(page: Page, username = "root") {
  await page.goto("/login");
  await page.getByLabel("用户名").fill(username);
  await page.getByLabel("密码").fill("123456");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
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

function fieldFor(control: Locator) {
  return control.locator(
    "xpath=ancestor::*[contains(concat(' ', normalize-space(@class), ' '), ' argus-field ')][1]",
  );
}

for (const locale of ["zh-CN", "en-US"] as const) {
  for (const theme of ["light", "dark"] as const) {
    test(`a11y matrix: ${locale} ${theme}`, async ({ page }) => {
      await page.addInitScript(
        ({ locale: nextLocale, theme: nextTheme }) => {
          window.localStorage.setItem("argus.locale", nextLocale);
          window.localStorage.setItem("argus.theme", nextTheme);
        },
        { locale, theme },
      );
      const expectAppearance = async () => {
        await expect(page.locator("html")).toHaveAttribute("lang", locale);
        await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
        await expectNoSeriousAccessibilityViolations(page);
      };

      await page.goto("/login");
      await expectAppearance();
      await page.locator('input[autocomplete="username"]').fill("root");
      await page
        .locator('input[autocomplete="current-password"]')
        .fill("123456");
      await page.locator('form button[type="submit"]').click();
      await expect(page).not.toHaveURL(/\/login/);
      await expectAppearance();
      await page.goto("/hosts");
      await expectAppearance();
      await page.goto("/settings/org");
      await expectAppearance();

      await page.goto(`${platformOrigin}/login`);
      await expectAppearance();
      await page.locator('input[autocomplete="username"]').fill("admin");
      await page
        .locator('input[autocomplete="current-password"]')
        .fill("123456");
      await page.locator('form button[type="submit"]').click();
      await expect(page).toHaveURL(`${platformOrigin}/`);
      await expectAppearance();

      await page.goto(`${platformOrigin}/?initialized=false&reset=1`);
      await expectAppearance();
    });
  }
}

test("initialized platform entry opens the login page", async ({ page }) => {
  await page.goto(`${platformOrigin}/?initialized=true&reset=1`);
  await expect(page).toHaveURL(`${platformOrigin}/login`);
});

test("setup marks required fields and initializes without an admin email", async ({
  page,
}) => {
  await page.goto(`${platformOrigin}/login?initialized=false&reset=1`);
  await expect(page.getByText("缺少初始化凭据")).toBeVisible();
  await expect(page.getByLabel("Setup Token")).toHaveCount(0);

  await page.goto(
    `${platformOrigin}/login?initialized=false&reset=1#argus_setup_token=setup-token-e2e`,
  );
  await expect(page).toHaveURL(
    `${platformOrigin}/login?initialized=false&reset=1`,
  );
  await expect(page.getByLabel("Setup Token")).toHaveCount(0);

  const email = page.getByLabel("邮箱");
  await expect(fieldFor(email).locator(".argus-field__required")).toHaveCount(
    0,
  );
  const username = page.getByLabel("登录名");
  await expect(
    fieldFor(username).locator(".argus-field__required"),
  ).toHaveCount(1);

  await page.getByLabel("平台显示名称").fill("Argus E2E");
  await page.getByLabel("外部访问地址").fill("https://argus-e2e.example.com");
  await username.fill("e2eadmin");
  await page.locator('input[name="admin.displayName"]').fill("E2E 管理员");
  await page
    .locator('input[name="admin.password"]')
    .fill("StrongSetupPass2026!");
  await page
    .locator('input[name="admin.confirmPassword"]')
    .fill("StrongSetupPass2026!");
  await page.getByRole("button", { name: "下一步" }).click();
  await page.getByRole("button", { name: /初始化|提交/ }).click();
  await expect(page).toHaveURL(`${platformOrigin}/login`);
  await expect(page.locator('input[autocomplete="username"]')).toBeVisible();
});

test("card runtime executes a cross-origin bridge and enforces CSP", async ({
  page,
}) => {
  await page.goto("/login");
  await page.evaluate(
    async ({ cardOrigin, enterpriseOrigin }) => {
      const html = `
      <button id="query" type="button">Query</button>
      <button id="action" type="button">Action</button>
      <output id="result"></output>
      <span id="network">pending</span>
      <script>
        document.getElementById("query").onclick = async () => {
          const result = await window.argusCard.query("query-1");
          document.getElementById("result").textContent = result.answer;
        };
        document.getElementById("action").onclick = async () => {
          const result = await window.argusCard.action("action-1");
          document.getElementById("result").textContent = result.status;
        };
        fetch("${enterpriseOrigin}/csp-probe")
          .then(() => document.getElementById("network").textContent = "escaped")
          .catch(() => document.getElementById("network").textContent = "blocked");
      </script>`;
      const digest = await crypto.subtle.digest(
        "SHA-256",
        new TextEncoder().encode(html),
      );
      const hash = Array.from(new Uint8Array(digest), (byte) =>
        byte.toString(16).padStart(2, "0"),
      ).join("");
      const iframe = document.createElement("iframe");
      iframe.id = "bridge-card";
      iframe.sandbox.add("allow-scripts");
      iframe.sandbox.add("allow-same-origin");
      iframe.src = `${cardOrigin}/?parent_origin=${encodeURIComponent(window.location.origin)}`;
      document.body.append(iframe);
      await new Promise<void>((resolve) =>
        iframe.addEventListener("load", () => resolve(), { once: true }),
      );
      const channel = new MessageChannel();
      const nonce = "nonce-1234567890";
      let hostSequence = 1;
      const messages: unknown[] = [];
      await new Promise<void>((resolve) => {
        channel.port1.onmessage = (event) => {
          const message = event.data as {
            type: string;
            payload: Record<string, unknown>;
          };
          messages.push(message);
          if (
            message.type === "query.invoke" ||
            message.type === "action.invoke"
          ) {
            hostSequence += 1;
            channel.port1.postMessage({
              bridge_version: "argus.card_bridge/v1",
              message_id: `host-${hostSequence}`,
              nonce,
              sequence: hostSequence,
              type: "binding.result",
              payload: {
                request_id: message.payload.request_id,
                ok: true,
                data:
                  message.type === "query.invoke"
                    ? { answer: "query-ok" }
                    : { status: "action-ok" },
              },
            });
          }
          if (
            message.type === "card.ready" ||
            message.type === "bridge.error"
          ) {
            resolve();
          }
        };
        channel.port1.start();
        iframe.contentWindow!.postMessage(
          {
            bridge_version: "argus.card_bridge/v1",
            message_id: "hello-1",
            nonce,
            sequence: 1,
            type: "host.hello",
            payload: {
              html,
              entrypoint_hash: hash,
              allowed_resources: ["inline_script"],
              max_message_bytes: 1024 * 1024,
              locale: "zh-CN",
              color_scheme: "dark",
              render_plan: {
                schema_version: "argus.render_plan/v1",
                card_id: "card-e2e",
                card_revision: 1,
                card_instance_id: "instance-e2e",
                data_bindings: [],
                query_binding_ids: { list: "query-1" },
                action_binding_ids: { commit: "action-1" },
                locale: "zh-CN",
                color_scheme: "dark",
              },
              initial_data: {},
            },
          },
          cardOrigin,
          [channel.port2],
        );
      });
      const state = window as typeof window & {
        __cardMessages?: unknown[];
        __sendCardContext?: () => void;
      };
      state.__cardMessages = messages;
      state.__sendCardContext = () => {
        hostSequence += 1;
        channel.port1.postMessage({
          bridge_version: "argus.card_bridge/v1",
          message_id: `host-${hostSequence}`,
          nonce,
          sequence: hostSequence,
          type: "host.context",
          payload: {
            locale: "en-US",
            color_scheme: "light",
            design_tokens: {},
          },
        });
      };
    },
    { cardOrigin, enterpriseOrigin },
  );

  const card = page.frameLocator("#bridge-card");
  const handshakeMessages = await page.evaluate(
    () =>
      (
        window as typeof window & {
          __cardMessages?: Array<{ type?: string; payload?: unknown }>;
        }
      ).__cardMessages ?? [],
  );
  expect(
    handshakeMessages.find((message) => message.type === "bridge.error"),
  ).toBeUndefined();
  await card.getByRole("button", { name: "Query" }).click();
  await expect(card.locator("#result")).toHaveText("query-ok");
  await card.getByRole("button", { name: "Action" }).click();
  await expect(card.locator("#result")).toHaveText("action-ok");
  await expect(card.locator("#network")).toHaveText("blocked");
  await page.evaluate(() => {
    (
      window as typeof window & { __sendCardContext?: () => void }
    ).__sendCardContext?.();
  });
  await expect(card.locator("html")).toHaveAttribute("lang", "en-US");
  await expect(card.locator("html")).toHaveAttribute(
    "data-color-scheme",
    "light",
  );
  const messages = await page.evaluate(
    () =>
      (
        window as typeof window & {
          __cardMessages?: Array<{ payload?: unknown }>;
        }
      ).__cardMessages ?? [],
  );
  expect(
    messages.some((message) =>
      JSON.stringify(message.payload).includes("params"),
    ),
  ).toBe(false);
});

test("card runtime rejects a wrong parent origin and entrypoint hash", async ({
  page,
}) => {
  await page.goto("/login");
  const result = await page.evaluate(async (cardOrigin) => {
    const makeFrame = async (id: string, parentOrigin: string) => {
      const iframe = document.createElement("iframe");
      iframe.id = id;
      iframe.sandbox.add("allow-scripts");
      iframe.sandbox.add("allow-same-origin");
      iframe.src = `${cardOrigin}/?parent_origin=${encodeURIComponent(parentOrigin)}`;
      document.body.append(iframe);
      await new Promise<void>((resolve) =>
        iframe.addEventListener("load", () => resolve(), { once: true }),
      );
      return iframe;
    };
    const hello = (entrypointHash: string) => ({
      bridge_version: "argus.card_bridge/v1",
      message_id: "hello-security",
      nonce: "nonce-1234567890",
      sequence: 1,
      type: "host.hello",
      payload: {
        html: '<p id="trusted">trusted</p>',
        entrypoint_hash: entrypointHash,
        allowed_resources: [],
        max_message_bytes: 1024 * 1024,
        locale: "zh-CN",
        color_scheme: "dark",
        render_plan: {
          schema_version: "argus.render_plan/v1",
          card_id: "card-security",
          card_revision: 1,
          card_instance_id: "instance-security",
          data_bindings: [],
          query_binding_ids: {},
          action_binding_ids: {},
          locale: "zh-CN",
          color_scheme: "dark",
        },
        initial_data: {},
      },
    });

    const wrongOrigin = await makeFrame(
      "wrong-origin-card",
      "https://evil.example.test",
    );
    const ignoredChannel = new MessageChannel();
    let originMessage = false;
    ignoredChannel.port1.onmessage = () => {
      originMessage = true;
    };
    ignoredChannel.port1.start();
    wrongOrigin.contentWindow!.postMessage(hello("0".repeat(64)), cardOrigin, [
      ignoredChannel.port2,
    ]);
    await new Promise((resolve) => setTimeout(resolve, 200));

    const wrongHash = await makeFrame(
      "wrong-hash-card",
      window.location.origin,
    );
    const hashChannel = new MessageChannel();
    const hashError = await new Promise<string | null>((resolve) => {
      const timer = setTimeout(() => resolve(null), 1000);
      hashChannel.port1.onmessage = (event) => {
        const message = event.data as {
          type?: string;
          payload?: { code?: string };
        };
        if (message.type === "bridge.error") {
          clearTimeout(timer);
          resolve(message.payload?.code ?? null);
        }
      };
      hashChannel.port1.start();
      wrongHash.contentWindow!.postMessage(hello("0".repeat(64)), cardOrigin, [
        hashChannel.port2,
      ]);
    });
    return { originMessage, hashError };
  }, cardOrigin);
  expect(result).toEqual({
    originMessage: false,
    hashError: "ENTRYPOINT_HASH_MISMATCH",
  });
});

test("protected routes redirect to login and login redirects back", async ({
  page,
}) => {
  await page.goto("/hosts");
  await expect(page).toHaveURL(/\/login\?/);
  await expect(page.getByRole("heading", { name: "登录 Argus" })).toBeVisible();
  await page.getByLabel("用户名").fill("root");
  await page.getByLabel("密码").fill("123456");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(/\/hosts$/);
  await expect(page.getByRole("heading", { name: "主机" })).toBeVisible();
});

test("chat shell lists conversations and links to the admin console", async ({
  page,
}) => {
  await login(page);
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("button", { name: "新建会话" })).toBeVisible();
  const accountRightGap = await page
    .locator(".argus-topbar")
    .evaluate((topbar) => {
      const actions = topbar.querySelector(".argus-topbar__actions");
      if (!actions) return Number.POSITIVE_INFINITY;
      return Math.round(
        topbar.getBoundingClientRect().right -
          actions.getBoundingClientRect().right,
      );
    });
  expect(accountRightGap).toBe(20);
  await page.getByRole("link", { name: "进入管理后台" }).click();
  await expect(page).toHaveURL(/\/hosts$/);
  await expect(page.getByRole("link", { name: /待审批/ })).toBeVisible();
  await expect(page.getByRole("link", { name: /Sandbox/ })).toHaveCount(0);
  await page.getByRole("link", { name: /企业审计/ }).click();
  await expect(page).toHaveURL(/\/settings\/audit$/);
  await expect(page.getByRole("heading", { name: "企业审计" })).toBeVisible();
  await expect(page.getByRole("table")).toContainText("确认待执行操作");
  await expect(page.getByRole("table")).not.toContainText(
    "pending_action.confirm",
  );
  await page.getByRole("link", { name: "返回智能会话" }).click();
  await expect(page).toHaveURL(/\/$/);
});

test("platform and enterprise accounts stay in their own portals", async ({
  page,
}) => {
  await page.goto("/login");
  await page.getByLabel("用户名").fill("admin");
  await page.getByLabel("密码").fill("123456");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByText(/平台管理域/)).toBeVisible();

  await page.goto(`${platformOrigin}/login`);
  await page.getByLabel("用户名").fill("admin");
  await page.getByLabel("密码").fill("123456");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(`${platformOrigin}/`);
  await expect(page.getByRole("heading", { name: "平台仪表盘" })).toBeVisible();
  await expect(page.getByRole("link", { name: "OpenSandbox" })).toBeVisible();
  await expect(
    page.locator(".argus-badge").filter({ hasText: /^平台超级管理员$/ }),
  ).toBeVisible();
  await page.getByRole("link", { name: "平台审计" }).click();
  await expect(page.getByRole("table")).toContainText("创建企业");
  await expect(page.getByRole("table")).not.toContainText(
    "platform.enterprise.create",
  );
  const platformPadding = await page
    .locator(".argus-page-content")
    .evaluate((element) =>
      Number.parseFloat(window.getComputedStyle(element).paddingLeft),
    );
  expect(platformPadding).toBe(24);
});

test("enterprise identity has no enterprise switcher", async ({ page }) => {
  await login(page, "wanglei");
  await page.goto("/hosts");
  await expect(page.getByRole("button", { name: "切换企业" })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "主机" })).toBeVisible();
});

test("credential settings separates secrets, credentials, and managed accounts", async ({
  page,
}) => {
  await login(page);
  await page.goto("/settings/secrets");

  await expect(
    page.getByRole("tab", { name: "密钥", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("button", { name: "新建密钥" })).toBeVisible();

  await page.getByRole("button", { name: "选择新建类型" }).click();
  await expect(page.getByRole("menuitem", { name: "新建密钥" })).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "新建连接凭证" }),
  ).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "新建托管账号" }),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "新建密钥" }).click();
  const secretDrawer = page.getByRole("dialog", { name: "新建密钥" });
  await expect(
    fieldFor(secretDrawer.getByLabel("名称")).locator(".argus-field__required"),
  ).toHaveCount(1);
  await expect(
    fieldFor(secretDrawer.getByLabel("描述")).locator(".argus-field__required"),
  ).toHaveCount(0);
  await expect(
    fieldFor(secretDrawer.getByLabel("值")).locator(".argus-field__required"),
  ).toHaveCount(1);
  await page.keyboard.press("Escape");

  await page.getByRole("button", { name: "选择新建类型" }).click();
  await page.getByRole("menuitem", { name: "新建托管账号" }).click();
  await expect(
    page.getByRole("dialog", { name: "新建托管账号" }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("tab", { name: "托管账号" })).toHaveAttribute(
    "aria-selected",
    "true",
  );

  await page.getByRole("tab", { name: "连接凭证" }).click();
  await expect(
    page.getByRole("button", { name: "新建连接凭证" }),
  ).toBeVisible();
  await expect(page.getByRole("region", { name: "连接凭证" })).toBeVisible();
  await page.getByRole("button", { name: "新建连接凭证" }).click();
  const credentialDrawer = page.getByRole("dialog", { name: "连接凭证" });
  await expect(credentialDrawer).toBeVisible();
  await expect(
    fieldFor(credentialDrawer.getByLabel("名称")).locator(
      ".argus-field__required",
    ),
  ).toHaveCount(1);
  await expect(
    fieldFor(credentialDrawer.getByLabel("用户名")).locator(
      ".argus-field__required",
    ),
  ).toHaveCount(0);
  await expect(
    fieldFor(credentialDrawer.getByLabel("Secret 引用")).locator(
      ".argus-field__required",
    ),
  ).toHaveCount(1);
  await page.keyboard.press("Escape");

  await page.getByRole("tab", { name: "托管账号" }).click();
  await expect(
    page.getByRole("button", { name: "新建托管账号" }),
  ).toBeVisible();
  await expect(page.getByRole("region", { name: "托管账号" })).toBeVisible();
});

test("component demo is reachable in dev mode", async ({ page }) => {
  test.skip(
    Boolean(process.env.ARGUS_E2E_EXTERNAL),
    "dev-only route is not shipped in the production image",
  );
  await login(page);
  await page.goto("/demo");
  await expect(
    page.getByRole("heading", { name: "为复杂运维，保持安静与清晰。" }),
  ).toBeVisible();
  await expect(page.getByText("StatusBadge", { exact: true })).toBeVisible();
});

test("mobile layout exposes primary navigation", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);
  await expect(page.getByRole("navigation").last()).toBeVisible();
  await page.getByRole("link", { name: "管理", exact: true }).click();
  await expect(page).toHaveURL(/\/hosts$/);
  await expect(page.getByRole("heading", { name: "主机" })).toBeVisible();
});

test("theme and locale preferences switch and persist", async ({ page }) => {
  await login(page);
  await page.goto("/hosts");
  await page.getByRole("button", { name: "切换到浅色模式" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");

  await page.getByRole("button", { name: "切换语言" }).click();
  await page.getByRole("menuitem", { name: "English" }).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  await expect(page.getByRole("link", { name: "Back to chat" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Hosts" })).toBeVisible();

  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
});

test("built-in role names follow the active locale", async ({ page }) => {
  await login(page);
  await page.goto("/settings/org");
  await page.getByRole("tab", { name: "角色", exact: true }).click();
  await expect(
    page.getByRole("cell", { name: "企业管理员 内置", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "资源查看者 内置", exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "切换语言" }).click();
  await page.getByRole("menuitem", { name: "English" }).click();

  await expect(
    page.getByRole("cell", { name: "Enterprise Admin Built-in", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "Resource Viewer Built-in", exact: true }),
  ).toBeVisible();
});

test("add host wizard walks three steps to the confirm card", async ({
  page,
}) => {
  await login(page);
  await page.goto("/hosts");
  await page.getByRole("button", { name: "添加普通主机" }).click();
  const drawer = page.getByRole("dialog", { name: "添加普通主机" });
  await expect(drawer).toBeVisible();

  // 第 1 步：选择直连；Direct Executor 可连接其部署网络可达的内网目标。
  await drawer.getByRole("button", { name: /^直连/ }).click();
  await drawer.getByRole("button", { name: "下一步" }).click();

  // 第 2 步：填写内网地址，验证直连模式不再要求公网地址。
  await drawer.getByLabel("主机名").fill("web-e2e-01");
  await drawer.getByLabel("地址").fill("10.0.1.5");
  await drawer.getByLabel("登录账号").fill("argus");
  await drawer.getByLabel("登录凭据或密钥").click();
  await page.getByRole("option", { name: "prod-ssh-key" }).click();
  await drawer.getByRole("button", { name: "下一步" }).click();

  // 第 3 步：模拟连接测试（前端约 900ms 后返回成功）后生成预览。
  await drawer.getByRole("button", { name: "开始测试" }).click();
  await expect(drawer.getByText("测试通过，可生成预览")).toBeVisible({
    timeout: 10_000,
  });
  await drawer.getByRole("button", { name: "生成预览" }).click();

  // 确认卡（PreviewCommitCard）：标题、确认/取消操作。
  await expect(drawer.getByText("新增主机 web-e2e-01")).toBeVisible();
  await expect(drawer.getByRole("button", { name: "确认执行" })).toBeVisible();
  await expect(drawer.getByRole("button", { name: "取消" })).toBeVisible();
});

test("approvals inbox: open a pending action and approve it", async ({
  page,
}) => {
  await login(page);
  await page.goto("/approvals");
  await page.getByRole("button", { name: /重启 payment-worker/ }).click();

  const detail = page.locator(".argus-approval-detail");
  await expect(detail.getByText("重启 payment-worker")).toBeVisible();
  const approveButton = detail.getByRole("button", { name: "批准" });
  await expect(approveButton).toBeVisible();
  await detail.getByLabel("批准").fill("同意，窗口内执行");
  await approveButton.click();

  // 审批记录出现「已批准」，操作进入执行（mock 定时推进任务步骤）。
  await expect(detail.getByText("已批准").first()).toBeVisible();
  await expect(approveButton).not.toBeVisible();
  await expect(detail.getByText(/待执行|执行中/)).toBeVisible();
});

test("approvals inbox: reject a pending action with a reason", async ({
  page,
}) => {
  await login(page);
  await page.goto("/approvals");
  await page.getByRole("button", { name: /重启 payment-worker/ }).click();

  const detail = page.locator(".argus-approval-detail");
  await detail.getByRole("button", { name: "驳回" }).click();
  const dialog = page.getByRole("dialog", { name: "驳回操作" });
  await expect(dialog).toBeVisible();
  await dialog.getByPlaceholder(/驳回理由/).fill("变更窗口已过，请重新申请");
  await dialog.getByRole("button", { name: "确认驳回" }).click();

  await expect(detail.getByText("已驳回").first()).toBeVisible();
  await expect(detail.getByText(/变更窗口已过/).first()).toBeVisible();
});

test("ai settings: test and create a model in one step", async ({ page }) => {
  await login(page);
  await page.goto("/settings/ai");
  await page.getByRole("button", { name: "添加模型" }).click();

  const drawer = page.getByRole("dialog", {
    name: "添加 OpenAI Compatible 模型",
  });
  await expect(drawer).toBeVisible();
  await expect(
    fieldFor(drawer.getByLabel("API Key")).locator(".argus-field__required"),
  ).toHaveCount(1);
  await drawer.getByLabel("名称").fill("e2e-推理模型");
  await drawer.getByLabel("API 地址").fill("https://llm-gw.internal/v1");
  await drawer.getByLabel("API Key").fill("sk-e2e-test");
  await drawer.getByLabel("模型 ID").fill("e2e-chat");
  await drawer.getByLabel("每百万输入 Token 金额").fill("1.25");
  await drawer.getByLabel("每百万输出 Token 金额").fill("3.50");
  await drawer.getByRole("button", { name: "测试并创建" }).click();
  await expect(drawer).not.toBeVisible();

  const row = page.getByRole("row", { name: /e2e-推理模型/ });
  await expect(row).toBeVisible();
  await expect(row.getByText("健康")).toBeVisible();
  await row.getByRole("button", { name: "编辑" }).click();
  const editDrawer = page.getByRole("dialog", { name: "编辑模型" });
  await expect(
    fieldFor(editDrawer.getByLabel("API Key")).locator(
      ".argus-field__required",
    ),
  ).toHaveCount(0);
  await editDrawer.getByRole("button", { name: "保存" }).click();
  await expect(editDrawer).not.toBeVisible();
});

test("host and bastion collector statuses expose contract-backed actions", async ({
  page,
}) => {
  await login(page);
  await page.goto("/hosts");

  // 种子里 cache-bj-01 已是「监控中」，主机安装路径由 kubernetes 用例覆盖；
  // 这里验证主机行的收集器状态入口直达详情页「组件与采集」。
  await page
    .getByRole("button", { name: "查看 cache-bj-01 的 OTLP 收集器" })
    .click();
  await expect(page).toHaveURL(/\/hosts\/host-cache-bj-01#otlp-collector$/);
  await expect(page.getByRole("tab", { name: "组件与采集" })).toHaveAttribute(
    "data-state",
    "active",
  );
  await page.goto("/hosts");

  const shanghaiScope = page
    .locator(".argus-scope-card")
    .filter({ hasText: "上海机房堡垒机-01" })
    .first();
  const scopeHeader = shanghaiScope.locator(".argus-scope-card__head");
  await expect(
    scopeHeader.getByRole("link", { name: "上海机房堡垒机-01" }),
  ).toHaveAttribute("href", "/hosts/host-gw-sh-01");
  await expect(scopeHeader.getByText("堡垒机", { exact: true })).toBeVisible();
  await expect(scopeHeader.getByText("监控中", { exact: true })).toBeVisible();
  await expect(scopeHeader.getByRole("button", { name: "编辑" })).toBeVisible();
  await expect(scopeHeader.getByRole("button", { name: "删除" })).toHaveCount(
    0,
  );
  await expect(shanghaiScope.locator(".argus-scope-card__bastion")).toHaveCount(
    0,
  );

  await page
    .getByRole("button", { name: "查看 gw-sh-01 的 OTLP 收集器" })
    .click();
  await expect(page).toHaveURL(/\/hosts\/host-gw-sh-01#otlp-collector$/);
  await expect(page.getByRole("tab", { name: "组件与采集" })).toHaveAttribute(
    "data-state",
    "active",
  );
});

test("online Bastion gates replacement and deletion while members remain", async ({
  page,
}) => {
  await login(page);
  await page.goto("/hosts");

  const scope = page
    .locator(".argus-scope-card")
    .filter({ hasText: "上海机房堡垒机-01" })
    .first();
  await scope
    .locator(".argus-scope-card__head")
    .getByRole("button", { name: "编辑" })
    .click();

  const drawer = page.getByRole("dialog", {
    name: /编辑堡垒机.*上海机房堡垒机-01/,
  });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByLabel("名称")).toHaveValue("上海机房堡垒机-01");
  await expect(drawer.getByLabel("环境")).toBeVisible();
  await expect(drawer.getByLabel("标签")).toBeVisible();
  await expect(drawer.getByLabel("地址")).toHaveCount(0);
  await expect(drawer.getByLabel("端口")).toHaveCount(0);
  await expect(drawer.getByText("堡垒机安装/更新命令")).toBeVisible();
  await expect(drawer.getByText("当前堡垒机仍在线")).toBeVisible();
  await expect(drawer.getByRole("button", { name: "生成新命令" })).toHaveCount(
    0,
  );

  await drawer.getByLabel("名称").fill("上海核心堡垒机-E2E");
  await drawer.getByRole("button", { name: "保存", exact: true }).click();
  await drawer.getByRole("button", { name: "确认执行" }).click();
  await expect(drawer).not.toBeVisible();

  const updatedScope = page
    .locator(".argus-scope-card")
    .filter({ hasText: "上海核心堡垒机-E2E" })
    .first();
  await expect(updatedScope.getByText("成员 3")).toBeVisible();
  await expect(
    updatedScope
      .locator(".argus-scope-card__head")
      .getByRole("button", { name: "删除堡垒机" }),
  ).toHaveCount(0);

  await updatedScope
    .locator(".argus-scope-card__head")
    .getByRole("button", { name: "编辑" })
    .click();
  const onlineDrawer = page.getByRole("dialog", {
    name: /编辑堡垒机.*上海核心堡垒机-E2E/,
  });
  await expect(onlineDrawer.getByText("当前堡垒机仍在线")).toBeVisible();
  await expect(
    onlineDrawer.getByRole("button", { name: "生成新命令" }),
  ).toHaveCount(0);
});

test("new Bastion shows one-time enrollment and can register", async ({
  page,
}) => {
  await login(page);
  await page.goto("/hosts");
  await page.getByRole("button", { name: "添加堡垒机" }).click();

  const addDrawer = page.getByRole("dialog", { name: "添加堡垒机" });
  await addDrawer.getByLabel("名称").fill("新堡垒机-E2E");
  await addDrawer.getByRole("button", { name: "创建并生成令牌" }).click();
  await addDrawer.getByRole("button", { name: "确认执行" }).click();
  await expect(addDrawer.locator(".argus-code code")).toContainText(
    "--token enroll_",
  );
  await addDrawer.getByRole("button", { name: "关闭" }).click();

  const pendingScope = page
    .locator(".argus-scope-card")
    .filter({ hasText: "新堡垒机-E2E" })
    .first();
  await expect(pendingScope.getByText("等待堡垒机注册")).toBeVisible();
  await pendingScope.getByRole("button", { name: /模拟堡垒机上线/ }).click();
  await expect(pendingScope.getByText("堡垒机在线")).toBeVisible();
  await expect(
    pendingScope.getByRole("button", { name: /模拟堡垒机上线/ }),
  ).toHaveCount(0);
});

test("kubernetes collector statuses install or open monitoring directly", async ({
  page,
}) => {
  await login(page);
  await page.goto("/kubernetes");

  await page
    .getByRole("button", { name: "为 k8s-staging 安装 OTLP 收集器" })
    .click();
  await expect(
    page.getByRole("dialog", { name: /安装 OTLP 收集器.*k8s-staging/ }),
  ).toBeVisible();
  await page.keyboard.press("Escape");

  await page
    .getByRole("button", { name: "查看 k8s-prod-east 的 OTLP 收集器" })
    .click();
  await expect(page).toHaveURL(/\/kubernetes\/k8s-prod-east#otlp-collector$/);
  await expect(page.getByRole("tab", { name: "OTLP 收集器" })).toHaveAttribute(
    "data-state",
    "active",
  );
});

test("telemetry views expose accessible Metrics, Logs, Traces, usage, and partial states", async ({
  page,
}) => {
  await login(page);
  await page.goto("/kubernetes/k8s-prod-east");

  await page.getByRole("tab", { name: "指标" }).click();
  await expect(page.getByText("结果已裁剪")).toBeVisible();
  await expect(page.getByRole("img", { name: /指标时间序列/ })).toBeVisible();
  await page.getByText("查看数据表").click();
  await expect(page.getByRole("columnheader", { name: "序列" })).toBeVisible();

  await page.getByRole("tab", { name: "日志" }).click();
  await expect(page.getByRole("region", { name: /条日志/ })).toBeVisible();
  await expect(page.getByText("credential=[REDACTED]")).toBeVisible();

  await page.getByRole("tab", { name: "链路" }).click();
  await expect(page.getByRole("list", { name: /条 Trace/ })).toBeVisible();

  await page.goto("/settings/org");
  await page.getByRole("tab", { name: "遥测" }).click();
  await expect(page.getByText("Evaluation 保留期")).toBeVisible();
  await expect(page.getByRole("heading", { name: "近月用量" })).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "windows_amd64", exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByText("待实体验证").first()).toBeVisible();
  await expectNoSeriousAccessibilityViolations(page);
});

test("telemetry query builder and DSL editor execute all three language models", async ({
  page,
}) => {
  await login(page);
  await page.goto("/kubernetes/k8s-prod-east");

  await page.getByRole("tab", { name: "指标", exact: true }).click();
  await expect(page.getByRole("tab", { name: "查询构建器" })).toHaveAttribute(
    "data-state",
    "active",
  );
  await page.getByLabel("指标名").fill("http_requests_total");
  await expect(
    page.locator(".argus-telemetry-query-head textarea[readonly]"),
  ).toHaveValue(/http_requests_total/);
  await page.getByRole("button", { name: "执行查询" }).click();
  await expect(page.locator(".argus-telemetry-query-meta")).toContainText(
    "matrix",
  );

  await page.getByRole("tab", { name: "DSL 编辑器" }).click();
  await page
    .getByLabel("PROMQL DSL")
    .fill('sum by (service) (rate(http_requests_total{status=~"5.."}[5m]))');
  await page.getByRole("button", { name: "执行查询" }).click();
  await expect(page.locator(".argus-telemetry-query-meta")).toContainText(
    "aaaaaaaaaaaa",
  );

  await page.getByRole("tab", { name: "日志", exact: true }).click();
  await page.getByRole("tab", { name: "DSL 编辑器" }).click();
  await page
    .getByLabel("KQL DSL")
    .fill('{service_name="argus-demo"} |= "error" | json');
  await page.getByRole("button", { name: "执行查询" }).click();
  await expect(page.locator(".argus-telemetry-query-meta")).toContainText(
    "log_entries",
  );

  await page.getByRole("tab", { name: "链路", exact: true }).click();
  await page.getByRole("tab", { name: "DSL 编辑器" }).click();
  await page
    .getByLabel("SKYWALKING_GRAPHQL DSL")
    .fill('{service.name="argus-demo"} > {service.name="database"}');
  await page.getByRole("button", { name: "执行查询" }).click();
  await expect(page.locator(".argus-telemetry-query-meta")).toContainText(
    "traces",
  );
  await expectNoSeriousAccessibilityViolations(page);
});

test("chat administrator can create a disabled interactive card", async ({
  page,
}) => {
  await login(page);
  await page.getByRole("button", { name: "创建交互卡片" }).click();
  await page.getByRole("option", { name: /创建交互卡片/ }).click();
  await page.getByRole("textbox", { name: "发送" }).fill("主机容量表");
  await page.getByRole("button", { name: "发送" }).click();
  await expect(page.getByText(/已创建“主机容量表”草稿/)).toBeVisible({
    timeout: 30_000,
  });
  await page.getByRole("link", { name: "进入管理后台" }).click();
  await page.getByRole("link", { name: /交互卡片/ }).click();
  const row = page.locator(".argus-ic-row").filter({ hasText: "主机容量表" });
  await expect(row).toBeVisible();
  await expect(row.getByText("草稿")).toBeVisible();
});

test("regular user has no interactive-card slash command", async ({ page }) => {
  await login(page, "lina");
  await expect(page.getByRole("button", { name: "创建交互卡片" })).toHaveCount(
    0,
  );
  await page.getByRole("textbox", { name: "发送" }).fill("/");
  await expect(page.getByRole("listbox")).toHaveCount(0);
});

test("chatbox: host-create request confirms via card to success", async ({
  page,
}) => {
  await login(page);
  await expect(page).toHaveURL(/\/$/);

  await page.getByRole("textbox", { name: "发送" }).fill("新增主机 10.0.1.5");
  await page.getByRole("button", { name: "发送" }).click();

  // 流式回复（工具调用 + token + 卡片）结束后出现确认卡。
  const card = page.getByTestId("pending-action-card");
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(card.getByText("新增主机")).toBeVisible();
  await expect(card.getByText(/10\.0\.1\.5/)).toBeVisible();

  await card.getByRole("button", { name: "确认执行" }).click();
  // 卡片原地轮询执行状态，直到 mock 任务步骤推进到成功（"已创建主机 …"）。
  await expect
    .poll(async () => card.getByText(/已创建主机/).count(), { timeout: 30_000 })
    .toBeGreaterThan(0);
  await expect(card.getByRole("link", { name: /查看任务/ })).toBeVisible();
});

test("tasks: list renders and the detail drawer opens", async ({ page }) => {
  await login(page);
  await page.goto("/tasks");
  await expect(page.getByRole("heading", { name: "任务记录" })).toBeVisible();

  const firstTask = page.locator(".argus-task-name").first();
  await expect(firstTask).toBeVisible();
  const taskTitle = await firstTask.locator("span").first().innerText();
  await firstTask.click();

  const drawer = page.getByRole("dialog", { name: taskTitle });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByText("执行步骤")).toBeVisible();
  await expect(drawer.getByText("日志")).toBeVisible();
});

test("org bindings tab: grant, reflect on users tab, and revoke", async ({
  page,
}) => {
  await login(page);
  await page.goto("/settings/org");
  await page.getByRole("tab", { name: "授权绑定" }).click();

  await page.getByRole("button", { name: "新建授权" }).click();
  const drawer = page.getByRole("dialog", { name: "新建授权绑定" });
  await expect(drawer).toBeVisible();

  // 主体（主体类型默认为用户）：李娜。
  await drawer.getByRole("combobox").nth(1).click();
  await page.getByRole("option", { name: /李娜/ }).click();
  // 角色：资源操作员，并绑定非生产资源数据范围。
  await drawer.getByRole("combobox").nth(2).click();
  await page.getByRole("option", { name: "资源操作员" }).click();
  await drawer.getByRole("button", { name: "提交" }).click();
  await expect(drawer).not.toBeVisible();

  const row = page.getByRole("row", { name: /李娜.*资源操作员/ });
  await expect(row).toBeVisible();

  // 用户 tab 的角色 Badge 由 RoleBinding 派生，应同步出现。
  await page.getByRole("tab", { name: "用户" }).click();
  const userRow = page.getByRole("row", { name: /李娜/ });
  await expect(userRow.getByText("资源操作员")).toBeVisible();

  // 回到授权 tab 删除该绑定。
  await page.getByRole("tab", { name: "授权绑定" }).click();
  await page
    .getByRole("row", { name: /李娜.*资源操作员/ })
    .getByRole("button", { name: "删除" })
    .click();
  const dialog = page.getByRole("dialog", { name: "删除授权绑定" });
  await dialog.getByRole("button", { name: "确认" }).click();
  await expect(page.getByRole("row", { name: /李娜.*资源操作员/ })).toHaveCount(
    0,
  );
});

test("org data scopes persist structured label requirements", async ({
  page,
}) => {
  await login(page);
  await page.goto("/settings/org");
  await page.getByRole("tab", { name: "数据权限" }).click();
  await page.getByRole("button", { name: "新建数据权限" }).click();

  const drawer = page.getByRole("dialog", { name: "新建数据权限范围" });
  await drawer.getByLabel("名称").fill("生产主机范围 E2E");
  await drawer.locator(".argus-settings-check-option").first().click();
  await drawer.locator(".argus-settings-selector-list .argus-button").click();
  await drawer.getByLabel("标签键").fill("environment");
  await drawer.getByLabel("标签值，多个值用逗号分隔").fill("production");
  await drawer.getByRole("button", { name: "提交" }).click();

  const row = page.getByRole("row", { name: /生产主机范围 E2E/ });
  await expect(row).toContainText("主机");
  await expect(row).toContainText("environment eq (production)");
});

test("org role drawer: permission matrix uses credential, not secret", async ({
  page,
}) => {
  await login(page);
  await page.goto("/settings/org");
  await page.getByRole("tab", { name: "角色" }).click();
  await page.getByRole("button", { name: "新建角色" }).click();

  const drawer = page.getByRole("dialog", { name: "新建角色" });
  await expect(drawer).toBeVisible();

  const resources = drawer.locator(".argus-perm-matrix__resource");
  await expect(resources.filter({ hasText: /^连接凭证$/ })).toBeVisible();
  await expect(resources.filter({ hasText: /^密钥$/ })).toHaveCount(0);

  // has 的内部定位器会以行元素为根重新求值，不能从 drawer 起链。
  const credentialRow = drawer.locator(".argus-perm-matrix__row").filter({
    has: page.locator(".argus-perm-matrix__resource", {
      hasText: /^连接凭证$/,
    }),
  });
  await expect(
    credentialRow.getByRole("checkbox", { name: "管理" }),
  ).toBeVisible();
  await expect(
    credentialRow.getByRole("checkbox", { name: "使用" }),
  ).toBeVisible();
  await expect(
    credentialRow.getByRole("checkbox", { name: "查看原值" }),
  ).toBeVisible();
});
