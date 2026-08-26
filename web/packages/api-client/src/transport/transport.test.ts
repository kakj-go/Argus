import { describe, expect, it, vi } from "vitest";
import { createConfiguredApiClient } from "../factory";
import { createMockApiClient } from "../mock";
import {
  ClientConfigurationError,
  ClientOperationUnavailableError,
  StreamTerminatedError,
} from "./errors";
import { HttpTransport } from "./http";
import { decodeSse, parseSseBlock, SseTransport } from "./sse";
import { WebSocketTransport } from "./websocket";

describe("configured adapter", () => {
  it("resolves a same-origin API base URL", () => {
    const transport = new HttpTransport({ base_url: "/" });
    const resolved = transport.resolve("setup/status");

    expect(resolved.pathname).toBe("/api/v1/setup/status");
    expect(resolved.origin).toBe(globalThis.location?.origin ?? "http://localhost");
  });

  it("fails closed for unknown mode and missing real URL", async () => {
    await expect(
      createConfiguredApiClient({ portal: "setup", mode: "auto" }),
    ).rejects.toBeInstanceOf(ClientConfigurationError);
    await expect(
      createConfiguredApiClient({ portal: "setup", mode: "real" }),
    ).rejects.toBeInstanceOf(ClientConfigurationError);
  });

  it("returns a stable unavailable error for portal-incompatible operations", async () => {
    const client = await createConfiguredApiClient({
      portal: "setup",
      mode: "real",
      base_url: "https://api.example.test",
    });
    await expect(
      client.auth.login({ username: "setup", password: "not-sent" }),
    ).rejects.toBeInstanceOf(
      ClientOperationUnavailableError,
    );
    await expect(
      client.auth.login({ username: "setup", password: "not-sent" }),
    ).rejects.toMatchObject({ code: "CLIENT_OPERATION_UNAVAILABLE" });
  });

  it("submits a conversation message before resuming its SSE stream", async () => {
    const encoder = new TextEncoder();
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            event: { sequence: 7 },
            run: { run_id: "run-1" },
          }),
          { status: 202, headers: { "content-type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          new ReadableStream({
            start(controller) {
              controller.enqueue(
                encoder.encode(
                  `data: ${JSON.stringify({
                    schema_version: "argus.stream_event/v1",
                    event_id: "event-8",
                    sequence: 8,
                    event_type: "stream_closing",
                    occurred_at: "2026-08-17T08:00:00Z",
                    terminal: true,
                    close_reason: "normal",
                    data: {},
                  })}\n\n`,
                ),
              );
              controller.close();
            },
          }),
          { status: 200 },
        ),
      );
    const client = await createConfiguredApiClient({
      portal: "enterprise",
      mode: "real",
      base_url: "https://api.example.test",
      csrf_token: () => "csrf",
      fetch,
    });

    const events = [];
    for await (const event of client.conversations.sendMessage(
      "conversation-1",
      {
        content: "status",
      },
    )) {
      events.push(event);
    }

    expect(events.map((event) => event.event_id)).toEqual(["event-8"]);
    const streamHeaders = fetch.mock.calls[1]?.[1]?.headers as Headers;
    expect(streamHeaders.get("last-event-id")).toBe("7");
  });

  it.each(["chat_completions", "responses"] as const)(
    "sends the explicitly selected %s model protocol",
    async (apiProtocol) => {
      const fetch = vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            compatible: false,
            checks: [
              { name: "basic", status: "failed", error_code: "MODEL_TEST" },
            ],
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        ),
      );
      const client = await createConfiguredApiClient({
        portal: "enterprise",
        mode: "real",
        base_url: "https://api.example.test",
        csrf_token: () => "csrf",
        fetch,
      });

      await client.models.testAndCreate({
        name: "primary",
        baseUrl: "https://models.example.test/v1",
        apiKey: "model-secret",
        modelId: "model-1",
        apiProtocol,
        contextWindowTokens: 128_000,
        maxOutputTokens: 8192,
        inputPricePerMillionTokens: 1,
        outputPricePerMillionTokens: 2,
      });

      const body = JSON.parse(String(fetch.mock.calls[0]?.[1]?.body));
      expect(body.api_protocol).toBe(apiProtocol);
      expect(body.context_window_tokens).toBe(128_000);
      expect(body.max_output_tokens).toBe(8192);
    },
  );

  it("sends approval decisions without action parameters", async () => {
    const json = (value: unknown) =>
      new Response(JSON.stringify(value), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        json([
          {
            approval_request_id: "approval-1",
            action_ref: "action-1",
            requirements: [],
            decisions: [],
            status: "pending",
            expires_at: "2026-08-18T00:00:00Z",
            created_at: "2026-08-17T00:00:00Z",
            updated_at: "2026-08-17T00:00:00Z",
          },
        ]),
      )
      .mockResolvedValueOnce(json({ status: "approved" }))
      .mockResolvedValueOnce(json({ action_ref: "action-1", status: "ready" }));
    const client = await createConfiguredApiClient({
      portal: "enterprise",
      mode: "real",
      base_url: "https://api.example.test",
      csrf_token: () => "csrf",
      fetch,
    });

    await client.approvals.approve("action-1", "reviewed");

    const body = JSON.parse(String(fetch.mock.calls[1]?.[1]?.body));
    expect(body).toEqual({ decision: "approved", reason: "reviewed" });
    expect(JSON.stringify(body)).not.toMatch(/params|token|plan/i);
  });

  it("uses explicit Run, ApprovalRequest, and Execution endpoints", async () => {
    const json = (value: unknown) =>
      new Response(JSON.stringify(value), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    const run = {
      schema_version: "argus.run/v1",
      run_id: "run-1",
      conversation_id: "conversation-1",
      status: "running",
      stop_reason: "none",
      created_at: "2026-08-17T00:00:00Z",
      updated_at: "2026-08-17T00:00:00Z",
    };
    const approval = {
      approval_request_id: "approval-1",
      action_ref: "action-1",
      requirements: [],
      decisions: [],
      status: "pending",
      expires_at: "2026-08-18T00:00:00Z",
      created_at: "2026-08-17T00:00:00Z",
      updated_at: "2026-08-17T00:00:00Z",
    };
    const execution = {
      execution_id: "execution-1",
      action_ref: "action-1",
      status: "result_unknown",
      created_at: "2026-08-17T00:00:00Z",
      updated_at: "2026-08-17T00:00:00Z",
    };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(json(run))
      .mockResolvedValueOnce(json({ run }))
      .mockResolvedValueOnce(json({ run }))
      .mockResolvedValueOnce(json([approval]))
      .mockResolvedValueOnce(json(approval))
      .mockResolvedValueOnce(json({ ...approval, status: "approved" }))
      .mockResolvedValueOnce(
        json({
          items: [execution],
          page: {
            next_cursor: null,
            has_more: false,
            partial: { partial: false, reasons: [] },
          },
        }),
      )
      .mockResolvedValueOnce(json(execution))
      .mockResolvedValueOnce(
        json({
          schema_version: "argus.action_one_time_result/v1",
          execution_id: "execution-1",
          result_kind: "connector_enrollment",
          enrollment: {
            enrollment_id: "00000000-0000-0000-0000-000000000001",
            install_command: "install --token redacted-in-test",
            expires_at: "2026-08-17T00:05:00Z",
          },
          expires_at: "2026-08-17T00:05:00Z",
        }),
      );
    const client = await createConfiguredApiClient({
      portal: "enterprise",
      mode: "real",
      base_url: "https://api.example.test",
      csrf_token: () => "csrf",
      fetch,
    });

    await client.runs.get("run-1");
    await client.runs.cancel("run-1");
    await client.runs.compact("run-1");
    await client.approvalRequests.list();
    await client.approvalRequests.get("approval-1");
    await client.approvalRequests.decide("approval-1", {
      decision: "approved",
      reason: "reviewed",
    });
    await client.executions.list();
    await client.executions.get("execution-1");
    await client.executions.claimOneTimeResult("execution-1");

    expect(fetch.mock.calls.map((call) => String(call[0]))).toEqual([
      "https://api.example.test/api/v1/runs/run-1",
      "https://api.example.test/api/v1/runs/run-1/cancel",
      "https://api.example.test/api/v1/runs/run-1/compact",
      "https://api.example.test/api/v1/enterprise/approval-requests",
      "https://api.example.test/api/v1/enterprise/approval-requests/approval-1",
      "https://api.example.test/api/v1/enterprise/approval-requests/approval-1/decisions",
      "https://api.example.test/api/v1/enterprise/executions",
      "https://api.example.test/api/v1/enterprise/executions/execution-1",
      "https://api.example.test/api/v1/enterprise/executions/execution-1/one-time-result",
    ]);
    expect(JSON.parse(String(fetch.mock.calls[5]?.[1]?.body))).toEqual({
      decision: "approved",
      reason: "reviewed",
    });
    expect(fetch.mock.calls[8]?.[1]?.method).toBe("POST");
    expect(fetch.mock.calls[8]?.[1]?.body).toBeUndefined();
  });

  it("keeps mock and real domain method shapes aligned", async () => {
    const mock = createMockApiClient({ persist: false, delay: 0 });
    const real = await createConfiguredApiClient({
      portal: "enterprise",
      mode: "real",
      base_url: "https://api.example.test",
    });
    for (const domain of Object.keys(real) as Array<keyof typeof real>) {
      expect(Object.keys(real[domain]).sort()).toEqual(
        Object.keys(mock[domain]).sort(),
      );
    }
  });

  it("maps bounded Kubernetes resources and Pod logs to the M3 paths", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            items: [
              {
                cluster_id: "cluster-1",
                resource_type: "pod",
                namespace: "production",
                name: "api-0",
                labels: {},
                summary: { status: "Running" },
              },
            ],
            page: {
              next_cursor: null,
              has_more: false,
              partial: { partial: false, reasons: [] },
            },
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            cluster_id: "cluster-1",
            namespace: "production",
            pod: "api-0",
            content: "ready",
            truncated: false,
            bytes: 5,
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        ),
      );
    const client = await createConfiguredApiClient({
      portal: "enterprise",
      mode: "real",
      base_url: "https://api.example.test",
      fetch,
    });

    const resources = await client.kubernetes.listResources("cluster-1", {
      resource_type: "pod",
      namespace: "production",
      query: "api",
      limit: 25,
    });
    const logs = await client.kubernetes.getPodLogs("cluster-1", {
      namespace: "production",
      pod: "api-0",
      container: "api",
      tail_lines: 200,
    });

    expect(resources.items[0]?.name).toBe("api-0");
    expect(logs.content).toBe("ready");
    expect(String(fetch.mock.calls[0]?.[0])).toBe(
      "https://api.example.test/api/v1/enterprise/kubernetes-clusters/cluster-1/resources?resource_type=pod&namespace=production&query=api&limit=25",
    );
    expect(String(fetch.mock.calls[1]?.[0])).toBe(
      "https://api.example.test/api/v1/enterprise/kubernetes-clusters/cluster-1/pod-logs?namespace=production&pod=api-0&container=api&tail_lines=200",
    );
  });
});

describe("HttpTransport", () => {
  it("does not duplicate an /api base path", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    );
    const transport = new HttpTransport({
      base_url: "/api",
      fetch,
    });
    await transport.request("enterprise/auth/session");
    expect(String(fetch.mock.calls[0]?.[0])).toBe(
      "http://localhost/api/v1/enterprise/auth/session",
    );
  });

  it("applies v1 base URL, cookies, locale, request ID and JSON body", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
    const transport = new HttpTransport({
      base_url: "https://api.example.test/control/",
      fetch,
      locale: () => "en-US",
      request_id: () => "request-1",
      csrf_token: () => "csrf-1",
    });
    await expect(
      transport.request("widgets", {
        method: "POST",
        body: { name: "one" },
        csrf: true,
      }),
    ).resolves.toEqual({ ok: true });
    const [url, init] = fetch.mock.calls[0] as [URL, RequestInit];
    expect(url.href).toBe("https://api.example.test/control/api/v1/widgets");
    expect(init.credentials).toBe("include");
    const headers = init.headers as Headers;
    expect(headers.get("accept-language")).toBe("en-US");
    expect(headers.get("x-request-id")).toBe("request-1");
    expect(headers.get("x-csrf-token")).toBe("csrf-1");
    expect(headers.has("x-argus-enterprise")).toBe(false);
  });

  it("handles 204 and fails before network when CSRF is missing", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 204 }));
    const transport = new HttpTransport({
      base_url: "https://api.example.test",
      fetch,
    });
    await expect(
      transport.request("logout", { method: "POST" }),
    ).resolves.toBeUndefined();
    await expect(
      transport.request("mutate", { method: "POST", csrf: true }),
    ).rejects.toMatchObject({ code: "CSRF_TOKEN_MISSING" });
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("converts structured HTTP failures to ApiError", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: "FORBIDDEN",
          message_key: "errors.forbidden",
          request_id: "request-2",
          retryable: false,
        }),
        { status: 403 },
      ),
    );
    const transport = new HttpTransport({
      base_url: "https://api.example.test",
      fetch,
    });
    await expect(transport.request("secret")).rejects.toMatchObject({
      code: "FORBIDDEN",
      status: 403,
      request_id: "request-2",
    });
  });

  it("notifies the portal when an authenticated session is invalidated", async () => {
    const invalidated = vi.fn();
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: "AUTHORIZATION_VERSION_STALE",
          message_key: "errors.auth.authorization_version_stale",
          request_id: "request-stale",
          retryable: false,
        }),
        { status: 409 },
      ),
    );
    const transport = new HttpTransport({
      base_url: "https://api.example.test",
      fetch,
      on_authentication_invalidated: invalidated,
    });

    await expect(
      transport.request("enterprise/executions"),
    ).rejects.toMatchObject({
      code: "AUTHORIZATION_VERSION_STALE",
    });
    expect(invalidated).toHaveBeenCalledOnce();
    expect(invalidated.mock.calls[0]?.[0]).toMatchObject({
      code: "AUTHORIZATION_VERSION_STALE",
    });
  });
});

describe("SSE transport", () => {
  it("parses comments, event names and multiline data", () => {
    expect(
      parseSseBlock(
        ': heartbeat\nid: event-1\nevent: agent_event\ndata: {"a":1,\ndata: "b":2}',
      ),
    ).toEqual({ id: "event-1", event: "agent_event", data: '{"a":1,\n"b":2}' });
    expect(parseSseBlock(": heartbeat")).toBeNull();
  });

  it("decodes split frames", async () => {
    const encoder = new TextEncoder();
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoder.encode("id: 1\ndata: one\n"));
        controller.enqueue(encoder.encode("\nid: 2\ndata: two\n\n"));
        controller.close();
      },
    });
    const frames = [];
    for await (const frame of decodeSse(stream)) frames.push(frame);
    expect(frames.map((frame) => frame.data)).toEqual(["one", "two"]);
  });

  it("sends Last-Event-ID, ignores duplicates, and stops on terminal envelopes", async () => {
    const encoder = new TextEncoder();
    const base = {
      schema_version: "argus.stream_event/v1",
      event_id: "event-1",
      sequence: 1,
      event_type: "agent_event",
      occurred_at: new Date().toISOString(),
      terminal: false,
      data: {},
    };
    const payload = [
      base,
      base,
      {
        ...base,
        event_id: "event-2",
        sequence: 2,
        terminal: true,
        event_type: "stream_closing",
        close_reason: "normal",
      },
    ]
      .map((item) => `data: ${JSON.stringify(item)}\n\n`)
      .join("");
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(encoder.encode(payload));
            controller.close();
          },
        }),
        { status: 200 },
      ),
    );
    const transport = new HttpTransport({
      base_url: "https://api.example.test",
      fetch,
    });
    const events = [];
    for await (const event of new SseTransport(transport).stream(
      "conversations/c1/events",
      { last_event_id: "event-0" },
    ))
      events.push(event);
    expect(events.map((event) => event.event_id)).toEqual([
      "event-1",
      "event-2",
    ]);
    const headers = fetch.mock.calls[0]?.[1]?.headers as Headers;
    expect(headers.get("last-event-id")).toBe("event-0");
  });

  it("resumes after disconnect, preserves dedupe state, and applies stream headers", async () => {
    const encoder = new TextEncoder();
    const event = (id: string, sequence: number, terminal = false) => ({
      schema_version: "argus.stream_event/v1",
      event_id: id,
      sequence,
      event_type: terminal ? "stream_closing" : "agent_event",
      occurred_at: new Date().toISOString(),
      terminal,
      ...(terminal ? { close_reason: "normal" } : {}),
      resume_cursor: id,
      data: {},
    });
    const response = (items: unknown[]) =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(
              encoder.encode(
                items
                  .map((item) => `data: ${JSON.stringify(item)}\n\n`)
                  .join(""),
              ),
            );
            controller.close();
          },
        }),
        { status: 200 },
      );
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(response([event("event-1", 1)]))
      .mockResolvedValueOnce(
        response([event("event-1", 1), event("event-2", 2, true)]),
      );
    const transport = new HttpTransport({
      base_url: "https://api.example.test",
      fetch,
      locale: () => "en-US",
      request_id: () => "request-stream",
    });
    const events = [];
    for await (const item of new SseTransport(transport).stream("events", {
      retry_delay_ms: 0,
    })) {
      events.push(item);
    }
    expect(events.map((item) => item.event_id)).toEqual(["event-1", "event-2"]);
    expect(fetch).toHaveBeenCalledTimes(2);
    const secondHeaders = fetch.mock.calls[1]?.[1]?.headers as Headers;
    expect(secondHeaders.get("last-event-id")).toBe("event-1");
    expect(secondHeaders.get("accept-language")).toBe("en-US");
    expect(secondHeaders.get("x-request-id")).toBe("request-stream");
  });

  it("turns stale cursors into a stable terminal error", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: "STREAM_CURSOR_STALE",
          message_key: "errors.stream_cursor_stale",
          request_id: "r",
          retryable: false,
        }),
        { status: 409 },
      ),
    );
    const stream = new SseTransport(
      new HttpTransport({ base_url: "https://api.example.test", fetch }),
    ).stream("events");
    await expect(stream.next()).rejects.toBeInstanceOf(StreamTerminatedError);
  });

  it("terminates immediately when authorization is invalidated in-band", async () => {
    const encoder = new TextEncoder();
    const payload = {
      schema_version: "argus.stream_event/v1",
      event_id: "event-auth",
      sequence: 1,
      event_type: "authorization_invalidated",
      occurred_at: new Date().toISOString(),
      terminal: true,
      close_reason: "authorization_revoked",
      data: {},
    };
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(
              encoder.encode(`data: ${JSON.stringify(payload)}\n\n`),
            );
            controller.close();
          },
        }),
        { status: 200 },
      ),
    );
    const stream = new SseTransport(
      new HttpTransport({ base_url: "https://api.example.test", fetch }),
    ).stream("events");
    await expect(stream.next()).rejects.toMatchObject({
      code: "AUTHORIZATION_VERSION_STALE",
    });
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

describe("WebSocket transport", () => {
  it("records close state and rejects oversized incoming messages", () => {
    const listeners = new Map<string, (event: Event) => void>();
    const close = vi.fn();
    class FakeWebSocket {
      static OPEN = 1;
      readyState = 1;
      addEventListener(type: string, listener: EventListener) {
        listeners.set(type, listener as (event: Event) => void);
      }
      removeEventListener() {}
      send() {}
      close = close;
    }
    vi.stubGlobal("WebSocket", FakeWebSocket);
    const violation = vi.fn();
    const transport = new WebSocketTransport({
      max_message_bytes: 4,
      on_protocol_error: violation,
    });
    transport.connect("wss://api.example.test/stream");
    listeners.get("message")?.(
      new MessageEvent("message", { data: "oversized" }),
    );
    expect(violation).toHaveBeenCalledWith("MESSAGE_TOO_LARGE", { size: 9 });
    expect(close).toHaveBeenCalledWith(1009, "message_too_large");
    listeners.get("close")?.(
      new CloseEvent("close", {
        code: 1001,
        reason: "server_drain",
        wasClean: true,
      }),
    );
    expect(transport.close_state).toEqual({
      code: 1001,
      reason: "server_drain",
      clean: true,
    });
    vi.unstubAllGlobals();
  });
});
