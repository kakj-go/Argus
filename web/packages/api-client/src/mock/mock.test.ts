import { describe, expect, it } from "vitest";
import type { AgentEvent, StreamEventEnvelope } from "../generated/contracts";
import { createMockApiClient, type MockApiClient } from "./index";

function makeClient(): MockApiClient {
  return createMockApiClient({ persist: false, delay: 0, stepDelay: 1 });
}

function login(client: MockApiClient, username: string) {
  return client.auth.login({ username, password: "123456" });
}

function agentEvent(envelope: StreamEventEnvelope): AgentEvent | null {
  return envelope.event_type === "agent_event"
    ? (envelope.data as AgentEvent)
    : null;
}

async function waitFor(
  condition: () => boolean | Promise<boolean>,
  timeoutMs = 5000,
): Promise<void> {
  const start = Date.now();
  for (;;) {
    if (await condition()) return;
    if (Date.now() - start > timeoutMs) throw new Error("waitFor timeout");
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
}

describe("auth", () => {
  it("logs in, reports me() and rejects bad credentials", async () => {
    const client = makeClient();
    const session = await login(client, "root");
    expect(session.user.username).toBe("root");
    expect(session.session.enterprise_id).toBe("ent-acme");
    const me = await client.auth.me();
    expect(me.user.id).toBe("u-root");
    await expect(
      client.auth.login({ username: "root", password: "wrong-password" }),
    ).rejects.toThrow("invalid credentials");
  });

  it("keeps platform and enterprise API domains mutually exclusive", async () => {
    const client = makeClient();
    const platformSession = await login(client, "admin");
    expect(platformSession.session.audience).toBe("platform");
    await expect(client.hosts.list()).rejects.toThrow(
      "enterprise enterpriseUser required",
    );

    await login(client, "root");
    await expect(client.platform.enterprises.list()).rejects.toThrow(
      "platform super administrator required",
    );
  });

  it("keeps an enterprise identity in exactly one enterprise", async () => {
    const client = makeClient();
    await login(client, "wanglei");
    expect((await client.hosts.list()).items.length).toBe(9);
    expect((await client.auth.me()).session.department_id).toBe("dept-sre");

    await client.auth.logout();
    await expect(client.auth.me()).rejects.toThrow("unauthenticated");
  });

  it("changes the password and revokes the current session", async () => {
    const client = makeClient();
    await login(client, "root");
    await client.auth.changePassword({
      current_password: "123456",
      new_password: "N7!qP4@vL9#sT2$x",
      expected_version: 1,
    });
    await expect(client.auth.me()).rejects.toThrow("unauthenticated");
    await expect(
      client.auth.login({ username: "root", password: "123456" }),
    ).rejects.toThrow("invalid credentials");
    await expect(
      client.auth.login({
        username: "root",
        password: "N7!qP4@vL9#sT2$x",
      }),
    ).resolves.toMatchObject({ session: { audience: "enterprise" } });
  });
});

describe("machine credentials", () => {
  it("creates, rotates, and revokes API keys with contract-shaped secrets", async () => {
    const client = makeClient();
    await login(client, "root");
    const created = await client.org.createApiKey("sa-ci", {
      name: "automation",
    });
    expect(created.secret).toMatch(
      /^argus_ak_[A-Za-z0-9_-]{6,32}\.[A-Za-z0-9_-]{32,256}$/,
    );

    const rotated = await client.org.rotateApiKey(created.api_key.id);
    expect(rotated.secret).not.toBe(created.secret);
    expect(
      (await client.org.listApiKeys("sa-ci")).find(
        (key) => key.id === created.api_key.id,
      )?.status,
    ).toBe("revoked");

    await client.org.revokeApiKey(rotated.api_key.id);
    expect(
      (await client.org.listApiKeys("sa-ci")).find(
        (key) => key.id === rotated.api_key.id,
      )?.status,
    ).toBe("revoked");
  });
});

describe("enterprise users", () => {
  it("updates department and enforces disabled login state", async () => {
    const client = makeClient();
    await login(client, "root");

    const moved = await client.org.updateEnterpriseUser("u-lina", {
      department_id: "dept-sre",
      status: "disabled",
    });
    expect(moved.department_id).toBe("dept-sre");
    expect(moved.status).toBe("disabled");

    await client.auth.logout();
    await expect(login(client, "lina")).rejects.toThrow("invalid credentials");

    await login(client, "root");
    const enabled = await client.org.updateEnterpriseUser("u-lina", {
      status: "active",
    });
    expect(enabled.status).toBe("active");
    await client.auth.logout();
    await expect(login(client, "lina")).resolves.toMatchObject({
      session: { department_id: "dept-sre" },
    });
  });

  it("soft-disables and restores an empty department", async () => {
    const client = makeClient();
    await login(client, "root");
    const department = await client.org.createDepartment({
      name: "Temporary",
    });

    await client.org.deleteDepartment(department.id);
    expect(
      (await client.org.listDepartments()).find(
        (entry) => entry.id === department.id,
      )?.status,
    ).toBe("disabled");

    await client.org.updateDepartment(department.id, { status: "active" });
    expect(
      (await client.org.listDepartments()).find(
        (entry) => entry.id === department.id,
      )?.status,
    ).toBe("active");
  });
});

describe("hosts CRUD", () => {
  it("creates and updates through generated preview contracts", async () => {
    const client = makeClient();
    await login(client, "chenxi");

    const before = await client.hosts.list();
    expect(before.items.length).toBe(9);

    const connectionTest = await client.hosts.createConnectionTest({
      address: "10.0.9.99",
      port: 22,
      platform: "linux",
      connection_mode: "via_bastion",
      bastion_scope_id: "scope-sh",
      credential_id: "sec-ssh-prod",
      username: "root",
    });
    const action = await client.hosts.previewCreateResource({
      name: "mock-host-01",
      address: "10.0.9.99",
      port: 22,
      platform: "linux",
      connection_mode: "via_bastion",
      bastion_scope_id: "scope-sh",
      credential_id: "sec-ssh-prod",
      username: "root",
      environment: "production",
      labels: {},
      connection_test_id: connectionTest.id,
    });
    expect(action.status).toBe("awaiting_confirmation");
    expect(action.risk).toBe("write");

    const result = await client.approvals.confirm(action.action_ref);
    expect(result.execution).toBeUndefined();
    expect(result.pending_action.status).toBe("succeeded");

    const hosts = await client.hosts.list({ query: "mock-host-01" });
    expect(hosts.items.length).toBe(1);
    const created = hosts.items[0];
    expect(created?.connection_mode).toBe("via_bastion");

    const scope = await client.connectors.getBastionScope("scope-sh");
    expect(scope.member_count).toBeGreaterThan(0);

    const update = await client.hosts.previewUpdateResource(created?.id ?? "", {
      labels: { role: "batch" },
      expected_version: created?.resource_version ?? 1,
    });
    const updateResult = await client.approvals.confirm(update.action_ref);
    expect(updateResult.pending_action.status).toBe("succeeded");
    expect((await client.hosts.get(created?.id ?? "")).labels["role"]).toBe(
      "batch",
    );

    const deletion = await client.hosts.previewDeleteResource(
      created?.id ?? "",
      2,
    );
    expect(deletion.available_actions).toContain("confirm");

    const audits = await client.audit.list({ action: "host.create.commit" });
    expect(audits.items.length).toBeGreaterThan(0);
  });

  it("tests connections and manages collector install wizard", async () => {
    const client = makeClient();
    await login(client, "chenxi");

    const test = await client.hosts.createConnectionTest({
      address: "10.0.1.11",
      port: 22,
      platform: "linux",
      connection_mode: "via_bastion",
      bastion_scope_id: "scope-sh",
      credential_id: "sec-ssh-prod",
      username: "root",
    });
    expect(test.status).toBe("succeeded");

    expect(await client.hosts.getCollector("host-cache-bj-01")).toBeNull();
    const install = await client.hosts.previewCollectorInstall(
      "host-cache-bj-01",
      { profile: "host-basic", telemetryRoute: "edge-gw-bj" },
    );
    const { execution } = await client.approvals.confirm(install.action_ref);
    await waitFor(async () => {
      const current = await client.tasks.get(execution?.execution_id ?? "");
      return current.status === "succeeded";
    });
    const collector = await client.hosts.getCollector("host-cache-bj-01");
    expect(collector?.status).toBe("converged");

    const route = await client.approvals.preview({
      tool: "telemetry.collector.route",
      input_data: { hostId: "host-cache-bj-01", route: "direct_argus" },
    });
    const routeExecution = await client.approvals.confirm(route.action_ref);
    await waitFor(async () => {
      const current = await client.tasks.get(
        routeExecution.execution?.execution_id ?? "",
      );
      return current.status === "succeeded";
    });
  });
});

describe("pending actions and approvals", () => {
  it("confirms a write action and streams task events to completion", async () => {
    const client = makeClient();
    await login(client, "chenxi");

    const action = await client.approvals.preview({
      tool: "telemetry.collector.upgrade",
      input_data: { toVersion: "v24.2.0", batch: 3 },
    });
    expect(action.status).toBe("awaiting_confirmation");

    const events: string[] = [];
    const { execution } = await client.approvals.confirm(action.action_ref);
    expect(execution).toBeDefined();
    const taskId = execution?.execution_id ?? "";
    const unsubscribe = client.tasks.subscribeTask(taskId, (event) => {
      if (event.type === "task_updated") events.push(event.task.status);
    });

    await waitFor(async () => {
      const current = await client.tasks.get(taskId);
      return current.status === "succeeded";
    });
    unsubscribe();

    const done = await client.tasks.get(taskId);
    expect(done.progress).toBe(100);
    expect(done.steps.every((step) => step.status === "done")).toBe(true);
    expect(done.logs.length).toBeGreaterThan(0);
    expect(events).toContain("running");
    expect(events).toContain("succeeded");

    const finished = await client.approvals.get(action.action_ref);
    expect(finished.status).toBe("succeeded");
    expect(finished.execution_ref).toBe(taskId);
  });

  it("routes dangerous actions through approval and executes on approve", async () => {
    const client = makeClient();
    await login(client, "chenxi");

    const action = await client.approvals.preview({
      tool: "kubernetes.workload.restart",
      input_data: {
        clusterId: "k8s-staging",
        namespace: "staging",
        workload: "payment-worker",
      },
    });
    expect(action.risk).toBe("dangerous");
    expect(action.approval?.required).toBe(true);

    const confirmed = await client.approvals.confirm(action.action_ref);
    expect(confirmed.pending_action.status).toBe("awaiting_approval");
    expect(confirmed.execution).toBeUndefined();

    // Separation of duty: the creator cannot approve their own action.
    await expect(client.approvals.approve(action.action_ref)).rejects.toThrow(
      "separation of duty",
    );

    await login(client, "wanglei");
    const approved = await client.approvals.approve(action.action_ref, "ok");
    expect(approved.status).toBe("executing");
    expect(approved.execution_ref).toBeDefined();

    await waitFor(async () => {
      const current = await client.approvals.get(action.action_ref);
      return current.status === "succeeded";
    });
  });

  it("rejects an approval with a reason", async () => {
    const client = makeClient();
    await login(client, "lina");
    const action = await client.approvals.preview({
      tool: "kubernetes.workload.restart",
      input_data: { clusterId: "k8s-staging", workload: "payment-worker" },
    });
    await client.approvals.confirm(action.action_ref);

    await login(client, "wanglei");
    const rejected = await client.approvals.reject(
      action.action_ref,
      "变更窗口外",
    );
    expect(rejected.status).toBe("rejected");
    expect(rejected.result_summary).toBe("变更窗口外");
  });

  it("cancels an awaiting action", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    const action = await client.approvals.preview({
      tool: "connector.cert.rotate",
      input_data: { connectorId: "conn-sh-01" },
    });
    const cancelled = await client.approvals.cancel(action.action_ref);
    expect(cancelled.status).toBe("cancelled");
  });
});

describe("conversations", () => {
  it("streams tokens, tool calls and a card for a host-create request", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    const conversation = await client.conversations.create({
      title: "新增主机会话",
      selected_model_id: "model-qwen32b",
    });

    const events: StreamEventEnvelope[] = [];
    for await (const event of client.conversations.sendMessage(
      conversation.id,
      { content: "帮我新增一台主机 10.1.2.3 并接入监控" },
    )) {
      events.push(event);
    }

    const agentEvents = events.flatMap((event) => {
      const nested = agentEvent(event);
      return nested ? [nested] : [];
    });
    const types = agentEvents.map((event) => event.event_type);
    expect(types[0]).toBe("message_started");
    expect(types).toContain("tool_call_started");
    expect(types).toContain("tool_call_completed");
    expect(types).toContain("message_delta");
    expect(types).toContain("pending_action_created");
    expect(types[types.length - 1]).toBe("run_completed");

    const card = agentEvents.find(
      (event) => event.event_type === "pending_action_created",
    );
    const cardPayload = card?.payload as Record<string, unknown> | undefined;
    expect(cardPayload?.action_ref).toBeTruthy();

    const storedEvents = await client.conversations.listEvents(conversation.id);
    expect(storedEvents.length).toBe(2);
    const firstMessage = storedEvents[0]?.payload as {
      message?: { role?: string };
    };
    expect(firstMessage.message?.role).toBe("user");
    const completed = agentEvents.find(
      (event) => event.event_type === "message_completed",
    );
    const completedMessage = (
      completed?.payload as {
        message?: { role?: string; tool_calls?: unknown[]; cards?: unknown[] };
      }
    ).message;
    expect(completedMessage?.role).toBe("assistant");
    expect(completedMessage?.tool_calls).toHaveLength(3);
    expect(completedMessage?.cards).toHaveLength(1);

    // The streamed card carries a real pending action that can be confirmed.
    if (typeof cardPayload?.action_ref === "string") {
      const action = await client.approvals.get(cardPayload.action_ref);
      expect(action.schema_version).toBe("argus.pending_action/v1");
      expect(action).not.toHaveProperty("tool");
      expect(action).not.toHaveProperty("conversation_id");
    }
  });
});

describe("AI model governance", () => {
  it("tests and creates one model without returning the API key", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    const failed = await client.models.testAndCreate({
      name: "bad",
      baseUrl: "https://models.example/v1",
      apiKey: "invalid-key",
      modelId: "incompatible-model",
      apiProtocol: "chat_completions",
      contextWindowTokens: 128_000,
      maxOutputTokens: 8192,
      inputPricePerMillionTokens: 1,
      outputPricePerMillionTokens: 2,
    });
    expect(failed.created).toBe(false);
    expect(
      (await client.models.list()).some((model) => model.name === "bad"),
    ).toBe(false);

    const result = await client.models.testAndCreate({
      name: "E2E compatible",
      baseUrl: "https://models.example/v1",
      apiKey: "sk-valid-secret",
      modelId: "compatible-chat",
      apiProtocol: "responses",
      contextWindowTokens: 128_000,
      maxOutputTokens: 8192,
      inputPricePerMillionTokens: 1.25,
      outputPricePerMillionTokens: 3.5,
    });
    expect(result.created).toBe(true);
    expect(result.model?.credentialRef).toMatch(/^secret:\/\/model\//);
    expect(result.model).not.toHaveProperty("apiKey");
    expect(result.model?.compatibility.toolCalling).toBe(true);
    expect(result.model?.compatibility.structuredOutput).toBe(true);
  });

  it("combines department and personal quotas and rejects cross-department changes", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    await client.models.setQuota({
      modelId: "model-qwen7b",
      subjectType: "department",
      subjectId: "dept-sre",
      monthlyAmount: 50,
    });
    await login(client, "wanglei");
    await client.models.setQuota({
      modelId: "model-qwen7b",
      subjectType: "user",
      subjectId: "u-wanglei",
      monthlyAmount: 20,
    });
    await expect(
      client.models.setQuota({
        modelId: "model-qwen7b",
        subjectType: "user",
        subjectId: "u-lina",
        monthlyAmount: 10,
      }),
    ).rejects.toThrow("cross-department");
    await expect(
      client.models.setQuota({
        modelId: "model-qwen7b",
        subjectType: "user",
        subjectId: "u-wanglei",
        monthlyAmount: 60,
      }),
    ).rejects.toThrow("exceeds department");
    const availability = await client.models.listAvailability();
    expect(
      availability.find((entry) => entry.modelId === "model-qwen7b")?.available,
    ).toBe(true);
  });

  it("keeps the model revision and price snapshot on generated messages", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    const conversation = await client.conversations.create({
      selected_model_id: "model-qwen32b",
    });
    for await (const event of client.conversations.sendMessage(
      conversation.id,
      {
        content: "检查主机状态",
      },
    )) {
      expect(event.event_type).toBe("agent_event");
    }
    const storedEvents = await client.conversations.listEvents(conversation.id);
    const assistant = storedEvents
      .map(
        (event) =>
          (event.payload as { message?: Record<string, unknown> }).message,
      )
      .find((message) => message?.role === "assistant");
    expect(assistant?.model_id).toBe("model-qwen32b");
    expect(assistant?.model_revision).toBe(6);
    expect(assistant?.input_price_per_million_snapshot).toBe(2);
    expect(assistant?.output_price_per_million_snapshot).toBe(4.375);
  });
});

describe("interactive cards", () => {
  it("enforces binding validation and keeps system cards read-only", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    const card = await client.interactiveCards.create({
      slug: "host-table-test",
      name: "主机表",
      description: "test",
      htmlTemplate: '<div data-slot="items"></div>',
      slots: [{ name: "items", type: "array", required: true }],
      demoData: { items: [] },
    });
    await expect(client.interactiveCards.enable(card.id)).rejects.toThrow(
      "validation gate",
    );
    await client.interactiveCards.updateBindings(card.id, [
      {
        slotName: "items",
        mode: "strict",
        toolName: "host.list",
        schemaVersion: "2026-08-01",
        fieldPath: "items",
      },
    ]);
    expect((await client.interactiveCards.validate(card.id)).valid).toBe(true);
    expect((await client.interactiveCards.enable(card.id)).enabled).toBe(true);

    const system = (
      await client.interactiveCards.list({ source: "system" })
    )[0]!;
    await expect(client.interactiveCards.disable(system.id)).rejects.toThrow(
      "read-only",
    );
  });
});

describe("platform", () => {
  it("manages the enterprise lifecycle and admins", async () => {
    const client = makeClient();
    await login(client, "admin");

    const listed = await client.platform.enterprises.list();
    expect(listed.items.length).toBe(2);

    const created = await client.platform.enterprises.create({
      name: "Initech",
      code: "initech",
    });
    expect(created.status).toBe("active");
    expect((await client.platform.enterprises.list()).items.length).toBe(3);

    const updated = await client.platform.enterprises.update(created.id, {
      remark: "demo",
    });
    expect(updated.remark).toBe("demo");

    expect((await client.platform.enterprises.suspend(created.id)).status).toBe(
      "suspended",
    );
    expect(
      (await client.platform.enterprises.activate(created.id)).status,
    ).toBe("active");
    expect((await client.platform.enterprises.disable(created.id)).status).toBe(
      "disabled",
    );

    const admin = await client.platform.admins.create({
      enterpriseId: created.id,
      username: "initech-admin",
      displayName: "Initech 管理员",
    });
    expect(admin.credentialStatus).toBe("temporary_password");
    expect(
      (await client.platform.admins.disable(admin.id)).credentialStatus,
    ).toBe("disabled");

    const audits = await client.platform.audit.list({
      action: "platform.enterprise.create",
    });
    expect(audits.items.length).toBeGreaterThan(0);
    expect(audits.items.every((event) => event.enterpriseId === null)).toBe(
      true,
    );
  });
});

describe("setup", () => {
  it("runs the one-time initialization on an uninitialized platform", async () => {
    const client = createMockApiClient({
      persist: false,
      delay: 0,
      initialized: false,
    });
    expect((await client.setup.status()).state).toBe("uninitialized");

    const result = await client.setup.submit({
      setupToken: "setup-token-123",
      platformName: "Argus Demo",
      defaultLocale: "zh-CN",
      timezone: "Asia/Shanghai",
      externalUrl: "https://argus.example",
      superAdmin: {
        username: "root",
        displayName: "超级管理员",
        password: "a-strong-password",
      },
    });
    expect(result.success).toBe(true);
    expect((await client.setup.status()).state).toBe("initialized");

    await expect(
      client.setup.submit({
        setupToken: "again",
        platformName: "x",
        defaultLocale: "zh-CN",
        timezone: "UTC",
        externalUrl: "https://x.example",
        superAdmin: {
          username: "root2",
          displayName: "x",
          password: "another-password",
        },
      }),
    ).rejects.toThrow("already initialized");
  });
});

describe("connector generated port", () => {
  it("returns snake_case Connector and Bastion pages", async () => {
    const client = makeClient();
    await login(client, "chenxi");

    const scopes = await client.connectors.listBastionScopes();
    const scope = scopes.items.find((entry) => entry.id === "scope-sh");
    expect(scope?.active_connector_id).toBe("conn-sh-01");
    expect(scope?.member_count).toBeGreaterThan(0);
    expect(scopes.page.partial.partial).toBe(false);

    const connectors = await client.connectors.list();
    const connector = connectors.items.find(
      (entry) => entry.id === "conn-sh-01",
    );
    expect(connector?.role).toBe("bastion");
    expect(connector?.connection_epoch).toBeGreaterThan(0);
  });

  it("updates Bastion metadata only after explicit confirmation", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    const before = await client.connectors.getBastionScope("scope-sh");
    const action = await client.connectors.previewUpdateBastionScope(
      before.id,
      {
        name: "上海核心堡垒机",
        environment: "staging",
        labels: { region: "cn-east", tier: "core" },
        expected_version: before.resource_version,
      },
    );
    expect((await client.connectors.getBastionScope(before.id)).name).toBe(
      before.name,
    );

    const result = await client.approvals.confirm(action.action_ref);
    expect(result.pending_action.status).toBe("succeeded");
    expect(result.execution).toBeUndefined();
    const updated = await client.connectors.getBastionScope(before.id);
    expect(updated.name).toBe("上海核心堡垒机");
    expect(updated.resource_version).toBe(before.resource_version + 1);
  });

  it("uninstalls an online Connector only after explicit confirmation", async () => {
    const client = makeClient();
    await login(client, "chenxi");
    const before = await client.connectors.get("conn-sh-01");
    const action = await client.connectors.previewUninstallConnector(
      before.id,
      before.version,
    );
    expect((await client.connectors.get(before.id)).status).toBe("online");

    const result = await client.approvals.confirm(action.action_ref);
    expect(result.pending_action.status).toBe("executing");
    await waitFor(
      async () =>
        (await client.connectors.get(before.id)).status === "uninstalled",
    );
    expect((await client.connectors.get(before.id)).status).toBe("uninstalled");
    expect((await client.connectors.getBastionScope("scope-sh")).status).toBe(
      "uninstalled",
    );
  });
});
