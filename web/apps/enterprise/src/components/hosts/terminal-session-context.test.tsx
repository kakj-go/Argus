// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteAccessSession, SessionTicketResult } from "@argus/api-client";
import { TerminalSessionProvider, useTerminalSessions } from "@argus/api-client";

class FakeWebSocket extends EventTarget {
  static readonly OPEN = 1;
  static instances: FakeWebSocket[] = [];
  readyState = FakeWebSocket.OPEN;
  closeCalls = 0;
  constructor() {
    super();
    FakeWebSocket.instances.push(this);
  }
  send() {}
  close() { this.closeCalls += 1; }
}

const session = {
  id: "session-1",
  host_id: "host-1",
  managed_account_id: "account-1",
  protocol: "ssh",
} as RemoteAccessSession;
const ticket = {
  session_id: session.id,
  ticket: "ticket",
  websocket_url: "wss://remote.example.test/v1/sessions/session-1",
  protocol_version: "argus.remote_access/v1",
  expires_at: new Date(Date.now() + 60_000).toISOString(),
} as SessionTicketResult;
const terminateApi = vi.fn().mockResolvedValue(undefined);

function Harness() {
  const terminal = useTerminalSessions();
  return (
    <div>
      <button onClick={() => void terminal.attachSession(session.id, session, ticket, "host-1", "argus")}>attach</button>
      <button onClick={() => terminal.hideSession(session.id)}>hide</button>
      <button onClick={() => terminal.showSession(session.id)}>show</button>
      <button onClick={() => void terminal.terminateSession(session.id, terminateApi)}>terminate</button>
      <output data-testid="session-count">{terminal.sessions.size}</output>
      <output data-testid="hidden">{String(terminal.sessions.get(session.id)?.hidden ?? false)}</output>
      <output data-testid="dock">{String(terminal.dockOpen)}</output>
    </div>
  );
}

function renderHarness() {
  return render(
    <TerminalSessionProvider>
      <Harness />
    </TerminalSessionProvider>,
  );
}

describe("TerminalSessionProvider lifecycle", () => {
  beforeEach(() => {
    FakeWebSocket.instances = [];
    terminateApi.mockClear();
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("hides and shows a session without removing its connection record", async () => {
    renderHarness();
    fireEvent.click(screen.getByRole("button", { name: "attach" }));
    await waitFor(() => expect(screen.getByTestId("session-count")).toHaveTextContent("1"));
    fireEvent.click(screen.getByRole("button", { name: "hide" }));
    expect(screen.getByTestId("hidden")).toHaveTextContent("true");
    expect(screen.getByTestId("session-count")).toHaveTextContent("1");
    expect(FakeWebSocket.instances[0]?.closeCalls).toBe(0);
    fireEvent.click(screen.getByRole("button", { name: "show" }));
    expect(screen.getByTestId("hidden")).toHaveTextContent("false");
    expect(screen.getByTestId("dock")).toHaveTextContent("true");
  });

  it("terminates locally once and remains safe when called again", async () => {
    renderHarness();
    fireEvent.click(screen.getByRole("button", { name: "attach" }));
    await waitFor(() => expect(screen.getByTestId("session-count")).toHaveTextContent("1"));
    fireEvent.click(screen.getByRole("button", { name: "terminate" }));
    await waitFor(() => expect(screen.getByTestId("session-count")).toHaveTextContent("0"));
    fireEvent.click(screen.getByRole("button", { name: "terminate" }));
    expect(screen.getByTestId("session-count")).toHaveTextContent("0");
    expect(terminateApi).toHaveBeenCalledTimes(1);
    expect(FakeWebSocket.instances[0]?.closeCalls).toBe(1);
  });
});
