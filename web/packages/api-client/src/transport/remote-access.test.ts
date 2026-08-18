// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SessionTicketResult } from "../generated/contracts";
import { RemoteAccessConnection } from "./remote-access";

class FakeWebSocket extends EventTarget {
  static readonly OPEN = 1;
  static instances: FakeWebSocket[] = [];
  readonly sent: string[] = [];
  readonly url: string;
  readyState = FakeWebSocket.OPEN;
  closeCode?: number;
  closeReason?: string;

  constructor(url: string) {
    super();
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(value: string) { this.sent.push(value); }
  close(code?: number, reason?: string) { this.closeCode = code; this.closeReason = reason; }
  open() { this.dispatchEvent(new Event("open")); }
  message(value: unknown) { this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(value) })); }
}

const ticket = (): SessionTicketResult => ({
  session_id: "0198d8ee-c6b7-7e6d-8b75-88fd9133a888",
  ticket: "secret-ticket-value-that-must-remain-in-memory-only",
  websocket_url: "wss://remote.example.test/v1/sessions/0198d8ee-c6b7-7e6d-8b75-88fd9133a888",
  protocol_version: "argus.remote_access/v1",
  expires_at: new Date(Date.now() + 60_000).toISOString(),
});

describe("RemoteAccessConnection", () => {
  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
    localStorage.clear();
    sessionStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("sends the ticket once and validates the server nonce", () => {
    const frames: unknown[] = [];
    const connection = new RemoteAccessConnection(ticket(), { cols: 100, rows: 30, onFrame: (frame) => frames.push(frame) });
    const socket = FakeWebSocket.instances[0]!;
    socket.open();
    const hello = JSON.parse(socket.sent[0]!) as { ticket: string; nonce: string };
    expect(hello.ticket).toContain("secret-ticket");
    expect(hello.nonce).toHaveLength(32);
    socket.message({ protocol: "argus.remote_access/v1", type: "server_ready", sequence: 1, session_id: ticket().session_id,
      mode: "ssh_pty", nonce: hello.nonce, idle_timeout_seconds: 900, max_duration_seconds: 3600 });
    connection.input("whoami\n");
    expect(frames).toHaveLength(1);
    expect(socket.sent.slice(1).join("\n")).not.toContain("secret-ticket");
    expect(JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } })).not.toContain("secret-ticket");
  });

  it("closes a connection that does not echo the client nonce", () => {
    const connection = new RemoteAccessConnection(ticket(), { cols: 80, rows: 24, onFrame: vi.fn() });
    const socket = FakeWebSocket.instances[0]!;
    socket.open();
    socket.message({ protocol: "argus.remote_access/v1", type: "server_ready", sequence: 1, session_id: ticket().session_id,
      mode: "ssh_pty", nonce: "forged-nonce", idle_timeout_seconds: 900, max_duration_seconds: 3600 });
    expect(socket.closeReason).toBe("invalid_nonce");
    connection.close();
  });
});
