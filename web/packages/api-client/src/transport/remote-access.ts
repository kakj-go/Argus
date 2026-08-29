import type { SessionTicketResult } from "../generated/contracts";
import { WebSocketTransport } from "./websocket";

export type RemoteAccessServerFrame =
  | { protocol: "argus.remote_access/v1"; type: "server_ready"; sequence: number; session_id: string; mode: "ssh_pty" | "winrs_line"; nonce: string; idle_timeout_seconds: number; max_duration_seconds: number }
  | { protocol: "argus.remote_access/v1"; type: "output"; sequence: number; stream: "stdout" | "stderr"; data: string }
  | { protocol: "argus.remote_access/v1"; type: "state"; sequence: number; status: string; reason?: string }
  | { protocol: "argus.remote_access/v1"; type: "error"; sequence: number; code: string; message: string; terminal: boolean };

export class RemoteAccessConnection {
  private clientSequence = 0;
  private serverSequence = 0;
  private ticket?: string;
  private nonce?: string;
  private opened = false;
  private readonly transport: WebSocketTransport;

  constructor(
    result: SessionTicketResult,
    private readonly options: {
      cols: number;
      rows: number;
      onFrame(frame: RemoteAccessServerFrame): void;
      onOpen?(): void;
      onError?(reason: string): void;
      onClose?(reason: string): void;
    },
  ) {
    this.ticket = result.ticket;
    this.transport = new WebSocketTransport({
      max_message_bytes: 64 * 1024,
      on_message: (data) => this.onMessage(data),
      on_close: (event) => {
        this.opened = false;
        options.onClose?.(event.reason || "connection_lost");
      },
      on_error: () => {
        options.onError?.("connection_failed");
      },
      on_protocol_error: (code) => {
        options.onError?.(code);
        options.onClose?.(code);
      },
    });
    const socket = this.transport.connect(result.websocket_url);
    socket.addEventListener("open", () => {
      this.opened = true;
      options.onOpen?.();
      const ticket = this.ticket;
      this.ticket = undefined;
      this.nonce = crypto.randomUUID().replaceAll("-", "");
      this.send({ type: "client_hello", ticket, nonce: this.nonce, cols: options.cols, rows: options.rows });
    }, { once: true });
  }

  input(data: string): void { if (this.opened) this.send({ type: "input", data }); }
  resize(cols: number, rows: number): void { if (this.opened) this.send({ type: "resize", cols, rows }); }
  ping(): void { if (this.opened) this.send({ type: "ping" }); }
  close(reason = "client_close"): void {
    if (this.opened) {
      try { this.send({ type: "close", reason }); } catch { /* Closing an already-lost socket is idempotent. */ }
    }
    this.opened = false;
    this.transport.close(1000, reason);
    this.ticket = undefined;
    this.nonce = undefined;
  }

  private send(frame: Record<string, unknown>): void {
    this.transport.send(JSON.stringify({ protocol: "argus.remote_access/v1", sequence: ++this.clientSequence, ...frame }));
  }

  private onMessage(data: string | ArrayBuffer | Blob): void {
    if (typeof data !== "string") { this.close("binary_frame_rejected"); return; }
    let frame: RemoteAccessServerFrame;
    try { frame = JSON.parse(data) as RemoteAccessServerFrame; } catch { this.close("invalid_json"); return; }
    if (frame.protocol !== "argus.remote_access/v1" || frame.sequence !== this.serverSequence + 1) { this.close("invalid_sequence"); return; }
    if (this.serverSequence === 0 && (frame.type !== "server_ready" || frame.nonce !== this.nonce)) { this.close("invalid_nonce"); return; }
    this.serverSequence = frame.sequence;
    if (frame.type === "server_ready") this.nonce = undefined;
    this.options.onFrame(frame);
    if ((frame.type === "error" && frame.terminal) || (frame.type === "state" && ["terminated", "failed", "connection_lost", "invalidated"].includes(frame.status))) {
      this.transport.close(1000, frame.type === "error" ? frame.code : frame.status);
    }
  }
}
