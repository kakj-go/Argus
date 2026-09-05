import { enterpriseOrigin, platformOrigin, cardOrigin } from "./origins";
import { expect, test, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

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
      await page.goto(`${platformOrigin}/pki`);
      await expectAppearance();

      await page.goto(`${platformOrigin}/?initialized=false&reset=1`);
      await expectAppearance();
    });
  }
}

test("initialized platform entry opens the login page", async ({ page }) => {
  await page.goto(`${platformOrigin}/?initialized=true&reset=1`);
  await expect(page).toHaveURL(/\/login(?:\?redirect=.*)?$/);
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
  await expect(page.getByRole("link", { name: /审批中心/ })).toBeVisible();
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
  await page.getByRole("link", { name: "PKI 与信任" }).click();
  await expect(page).toHaveURL(`${platformOrigin}/pki`);
  await expect(
    page.getByRole("heading", { name: "PKI 与信任状态" }),
  ).toBeVisible();
  await expect(
    page.getByRole("term").filter({ hasText: /^Bundle SHA-256$/ }),
  ).toBeVisible();
  await expect(page.getByText("已确认", { exact: true }).first()).toBeVisible();
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

test.skip("mobile layout exposes primary navigation", async ({ page }) => {
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

  // 第 1 步只选择场景；第 2 步才挂载业务表单。
  await drawer.getByRole("button", { name: /^双向可达/ }).click();
  await drawer.getByRole("button", { name: "下一步", exact: true }).click();

  // 填写内网地址与凭据。
  await drawer.getByLabel("主机名").fill("web-e2e-01");
  await drawer.getByLabel("地址").fill("10.0.1.5");
  await drawer.getByLabel("登录账号").fill("argus");
  await drawer.getByLabel("登录凭据或密钥").click();
  await page.getByRole("option", { name: "prod-ssh-key" }).click();

  // 第二步提交后完成连接测试并进入第三步预览。
  await drawer.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(drawer.getByText("测试通过，可生成预览")).toBeVisible({
    timeout: 10_000,
  });

  // 确认卡（PreviewCommitCard）：标题、确认/取消操作。
  await expect(drawer.getByText("新增主机 web-e2e-01")).toBeVisible();
  await expect(drawer.getByRole("button", { name: "确认执行" })).toBeVisible();
  await expect(
    drawer.getByRole("button", { name: "取消" }).last(),
  ).toBeVisible();
  await drawer.getByRole("button", { name: "确认执行" }).click();
  await expect(drawer.getByText("主机创建完成")).toBeVisible();
  await drawer
    .locator(".argus-dialog__footer")
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await expect(drawer).not.toBeVisible();
  await expect(page.getByText("web-e2e-01")).toBeVisible();
});

test("add host wizard scenario cards gate planned tunnels and support self-enroll", async ({
  page,
}) => {
  await login(page);
  await page.goto("/hosts");
  await page.getByRole("button", { name: "添加普通主机" }).click();
  const drawer = page.getByRole("dialog", { name: "添加普通主机" });
  await expect(drawer).toBeVisible();

  // ⑤ 只出不进:免连接测试,表单仅名称/架构/环境/标签。
  await drawer.getByRole("button", { name: /^只出不进/ }).click();
  // 场景②(只进不出)与场景③(隧道成员)已随后端隧道链路开放,可选可提交。
  const inboundOnly = drawer.getByRole("button", { name: /^只进不出/ });
  await expect(inboundOnly).toBeEnabled();
  await expect(drawer.getByText("场景 ②").first()).toBeVisible();
  const tunnelMember = drawer.getByRole("button", {
    name: /^成员连不上堡垒机端口/,
  });
  await expect(tunnelMember).toBeEnabled();
  await drawer.getByRole("button", { name: "下一步", exact: true }).click();
  await drawer.getByLabel("主机名").fill("office-self-01");
  await drawer.getByText("前置条件", { exact: true }).waitFor();
  // 提交会立刻进入 loading/确认卡,click 可能因按钮随即禁用而抛超时——
  // 触发后以确认卡出现为准。
  await drawer.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(drawer.getByText("新增主机 office-self-01")).toBeVisible({
    timeout: 10_000,
  });
  await drawer.getByRole("button", { name: "确认执行" }).click();

  // 一次性安装命令只在确认结果中出现,关闭后不残留浏览器状态。
  await expect(drawer.getByText(/curl -fsS/)).toBeVisible({ timeout: 10_000 });
  await expect(
    drawer.getByRole("button", { name: "一行安装（推荐）", exact: true }),
  ).toHaveCount(0);
  await expect(
    drawer.getByRole("button", { name: "交互式", exact: true }),
  ).toHaveCount(0);
  await expect(
    drawer.getByRole("button", { name: "自动化", exact: true }),
  ).toHaveCount(0);
  const command = await drawer
    .getByText(/curl -fsS/)
    .first()
    .textContent();
  await drawer.getByRole("button", { name: "我已保存，关闭" }).click();
  await expect(drawer).not.toBeVisible();
  const browserState = JSON.stringify({
    local: await page.evaluate(() => localStorage),
    session: await page.evaluate(() => sessionStorage),
    url: page.url(),
  });
  expect(browserState).not.toContain(command ?? "curl -fsS");
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

test("approvals inbox: desktop tabs preserve scope deep links", async ({
  page,
}) => {
  await login(page);

  await page.goto("/approvals?approval=operation&scope=created");
  await expect(page).toHaveURL(/approval=operation/);
  await expect(page).toHaveURL(/scope=created/);
  await expect(page.getByRole("tab", { name: "操作审批" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(page.getByRole("tab", { name: "我发起的" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(
    page.getByRole("button", { name: /升级 12 个 Collector/ }),
  ).toBeVisible();

  await page.getByRole("tab", { name: "已处理" }).click();
  await expect(page).toHaveURL(/scope=done/);
  await expect(
    page.getByRole("button", { name: /新增主机 host-web-12/ }),
  ).toBeVisible();

  await page.getByRole("tab", { name: "远程访问审批" }).click();
  await expect(page).toHaveURL(/approval=remote/);
  await expect(page.getByRole("tab", { name: "远程访问审批" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(
    page.getByRole("button", { name: /重启 payment-worker/ }),
  ).toHaveCount(0);

  await page.reload();
  await expect(page).toHaveURL(/approval=remote/);
  await expect(page.getByRole("tab", { name: "远程访问审批" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
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
  await expect(drawer.getByText("Connector 替换")).toBeVisible();
  await expect(drawer.getByText("替换会执行 fencing")).toBeVisible();
  await expect(
    drawer.getByRole("button", { name: "替换 Connector" }),
  ).toBeVisible();

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
  await expect(onlineDrawer.getByText("Connector 替换")).toBeVisible();
  await expect(
    onlineDrawer.getByRole("button", { name: "替换 Connector" }),
  ).toBeVisible();
});

test("new Bastion shows one-time enrollment and can register", async ({
  page,
}) => {
  await login(page);
  await page.goto("/hosts");
  await page.getByRole("button", { name: "添加堡垒机" }).click();

  const addDrawer = page.getByRole("dialog", { name: "添加堡垒机" });
  // PlanV4 Task 06:三张场景卡(命令行/平台代装/代装+隧道)全部可选。
  await expect(
    addDrawer.getByRole("button", { name: /^堡垒机可出站访问 Argus/ }),
  ).toBeEnabled();
  await expect(
    addDrawer.getByRole("button", { name: /^平台代装（双向可达）/ }),
  ).toBeEnabled();
  await expect(
    addDrawer.getByRole("button", { name: /平台代装.*控制隧道/ }),
  ).toBeEnabled();
  await addDrawer.getByRole("button", { name: "下一步" }).click();
  await addDrawer.getByLabel("名称").fill("新堡垒机-E2E");
  await addDrawer.getByRole("button", { name: "下一步", exact: true }).click();
  await addDrawer.getByRole("button", { name: "确认执行" }).click();
  const downloadCommand =
    (await addDrawer.locator(".argus-code code").textContent()) ?? "";
  expect(downloadCommand).toContain("curl -fsS");
  expect(downloadCommand).toContain("--insecure");
  expect(downloadCommand).toContain("X-Argus-Enrollment-Token:");
  expect(downloadCommand).not.toContain("curl -fsSL");
  expect(downloadCommand).toContain("bootstrap-script?scope=linux-system");
  expect(downloadCommand).toContain("enroll_");
  expect(downloadCommand).not.toContain("\n");
  await expect(
    addDrawer.getByRole("button", { name: "一行安装（推荐）", exact: true }),
  ).toHaveCount(0);
  await expect(
    addDrawer.getByRole("button", { name: "交互式", exact: true }),
  ).toHaveCount(0);
  await expect(
    addDrawer.getByRole("button", { name: "自动化", exact: true }),
  ).toHaveCount(0);
  await addDrawer.getByRole("button", { name: "我已保存，关闭" }).click();

  const pendingScope = page
    .locator(".argus-scope-card")
    .filter({ hasText: "新堡垒机-E2E" })
    .first();
  await expect(pendingScope.getByText("安装命令已领取")).toBeVisible();
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
  const dialog = page.getByRole("dialog", {
    name: /安装 OTLP 收集器.*k8s-staging/,
  });
  await expect(dialog).toBeVisible();

  // 向导三步:绑定 → Claim 矩阵 → 预览;预览步填写内网镜像并展示生效值。
  await dialog.getByRole("button", { name: "下一步" }).click();
  await dialog.getByRole("button", { name: "下一步" }).click();
  const imageInput = dialog.getByLabel("内网镜像地址");
  await expect(imageInput).toBeVisible();
  await imageInput.fill("registry.internal.corp/argus/otelcol:0.1.0-m7");
  await expect(
    dialog.getByText("registry.internal.corp/argus/otelcol:0.1.0-m7").last(),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "生成安装预览" }).click();
  await dialog.getByRole("button", { name: "确认执行" }).click();

  // 确认后进入收敛面板:先呈现安装中,再自动收敛关闭。
  await expect(dialog.getByText("安装中")).toBeVisible();
  await expect(dialog).toHaveCount(0, { timeout: 15_000 });

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

test("remote access governance and session center expose the production flow", async ({
  page,
}) => {
  await login(page);
  await page.goto("/settings/org");
  await page.getByRole("tab", { name: /远程访问|Remote access/i }).click();
  await expect(page.getByRole("tab", { name: "访问授权" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "访问规则" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "审批流程" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "会话策略" })).toBeVisible();
  await page.getByRole("tab", { name: "访问规则" }).click();
  await expect(page.getByText("规则模拟器")).toBeVisible();

  await page.goto("/remote-sessions");
  await expect(page.getByRole("heading", { name: "远程会话" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "活动会话" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "历史会话" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "会话录像" })).toBeVisible();
  const tabsBox = await page.getByRole("tablist").boundingBox();
  const filtersBox = await page
    .locator(".argus-remote-sessions-tabs > .argus-filter-bar")
    .boundingBox();
  expect(tabsBox).not.toBeNull();
  expect(filtersBox).not.toBeNull();
  expect(filtersBox!.y - (tabsBox!.y + tabsBox!.height)).toBeGreaterThanOrEqual(
    12,
  );
});

test("org users remain renderable after the raw user directory is cached", async ({
  page,
}) => {
  await login(page);
  await page.goto("/settings/org");

  await page.getByRole("tab", { name: "远程访问" }).click();
  await expect(page.getByRole("tab", { name: "访问授权" })).toBeVisible();
  await expect(page.getByText("企业超级管理员").first()).toBeVisible();

  await page.getByRole("tab", { name: "用户" }).click();
  const userRow = page.getByRole("row", {
    name: /企业超级管理员.*企业管理员/,
  });
  await expect(userRow).toBeVisible();

  await userRow.getByRole("button", { name: "编辑" }).click();
  const drawer = page.getByRole("dialog", { name: "编辑成员" });
  await drawer.getByRole("button", { name: "资源查看者" }).click();
  await drawer.getByRole("button", { name: "提交" }).click();
  await expect(drawer).not.toBeVisible();
  await expect(userRow.getByText("资源查看者")).toBeVisible();
});

test("remote access governance supports CRUD, lifecycle, details, validation, and simulation", async ({
  page,
}) => {
  test.setTimeout(60_000);
  await login(page);
  await page.goto("/settings/org");
  await page.getByRole("tab", { name: "远程访问" }).click();

  await page.getByRole("tab", { name: "会话策略" }).click();
  await page.getByRole("button", { name: "新建会话策略" }).click();
  let drawer = page.getByRole("dialog", { name: "新建会话策略" });
  await drawer.getByLabel("名称").fill("生产会话策略");
  await expect(drawer.getByRole("switch", { name: "文件上传" })).toBeDisabled();
  await drawer.getByRole("button", { name: "保存" }).click();
  let row = page.getByRole("row").filter({ hasText: "生产会话策略" });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "启用" }).click();
  await expect(row.getByText("已启用")).toBeVisible();
  await row.getByRole("button", { name: "详情" }).click();
  await expect(
    page.getByRole("dialog", { name: "生产会话策略" }).getByText("v2"),
  ).toBeVisible();
  await page.keyboard.press("Escape");

  await page.getByRole("tab", { name: "审批流程" }).click();
  await page.getByRole("button", { name: "新建审批流程" }).click();
  drawer = page.getByRole("dialog", { name: "新建审批流程" });
  await drawer.getByLabel("名称").fill("生产审批流程");
  await drawer.getByRole("button", { name: "保存" }).click();
  await expect(drawer.getByText("请至少选择一项").first()).toBeVisible();
  await drawer.locator(".argus-governance-checks button").first().click();
  await drawer.getByRole("button", { name: "保存" }).click();
  row = page.getByRole("row").filter({ hasText: "生产审批流程" });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "启用" }).click();

  await page.getByRole("tab", { name: "访问规则" }).click();
  const simulator = page.locator(".argus-rule-simulator");
  await simulator.getByLabel("主机").click();
  await page.getByRole("option").first().click();
  await simulator.getByLabel("托管账号").click();
  await page.getByRole("option").first().click();
  await simulator.getByRole("button", { name: "运行模拟" }).click();
  await expect(
    simulator
      .locator(".argus-status-badge")
      .getByText("allowed", { exact: true }),
  ).toBeVisible();
  await expect(simulator.getByText(/REMOTE_ACCESS_ALLOWED/)).toBeVisible();

  await page.getByRole("button", { name: "新建访问规则" }).click();
  drawer = page.getByRole("dialog", { name: "新建访问规则" });
  await drawer.getByLabel("名称").fill("生产 SSH 规则");
  await drawer.getByRole("button", { name: "保存" }).click();
  await expect(
    drawer.getByText("请至少选择一个处理效果或会话策略"),
  ).toBeVisible();
  await drawer.getByLabel("会话策略").click();
  await page.getByRole("option", { name: /生产会话策略/ }).click();
  await drawer.getByRole("button", { name: "保存" }).click();
  row = page.getByRole("row").filter({ hasText: "生产 SSH 规则" });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "启用" }).click();

  await simulator.getByRole("button", { name: "运行模拟" }).click();
  await expect(
    simulator
      .locator(".argus-status-badge")
      .getByText("allowed", { exact: true }),
  ).toBeVisible();

  row = page.getByRole("row").filter({ hasText: "生产 SSH 规则" });
  await row.getByRole("button", { name: "停用" }).click();
  await page
    .getByRole("dialog", { name: "确认配置变更" })
    .getByRole("button", { name: "停用" })
    .click();
  await expect(row.getByText("已停用")).toBeVisible();
  await row.getByRole("button", { name: "归档" }).click();
  await page
    .getByRole("dialog", { name: "确认配置变更" })
    .getByRole("button", { name: "归档" })
    .click();
  await expect(row.getByText("已归档")).toBeVisible();
  await row.getByRole("button", { name: "恢复为草稿" }).click();
  await expect(row.getByText("草稿")).toBeVisible();
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

test("host delete surfaces dual-approval state instead of silently closing", async ({
  page,
}) => {
  await login(page);
  await page.goto("/hosts");

  // 选取一个已知独立主机,确认初始可见。
  const target = page.locator(".argus-host-tile", { hasText: "public-web-01" });
  await expect(target).toBeVisible();
  await target.getByRole("button", { name: "删除" }).click();

  // 第一段:删除确认框;第二段:Preview/Confirm 卡。
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "删除" }).click();
  await dialog.getByRole("button", { name: "确认执行" }).click();

  // 删除为危险操作,进入双人审批:卡片必须保持打开并给出明确引导,
  // 而不是静默关闭让用户误以为"删除没有生效/列表没有刷新"。
  await expect(dialog.getByText("已确认，等待审批通过后执行")).toBeVisible();
  await expect(dialog.getByText(/双人审批/)).toBeVisible();
  // Dialog 自身也有一个"关闭"(X)按钮,用卡片内的那个。
  await dialog
    .getByRole("button", { name: "关闭" })
    .filter({ hasText: "关闭" })
    .first()
    .click();
  await expect(dialog).toHaveCount(0);

  // 未批准前主机仍在列表(治理语义正确);审批中心出现待办。
  await expect(target).toBeVisible();
  await expect(page.getByRole("link", { name: /审批中心/ })).toBeVisible();
});
