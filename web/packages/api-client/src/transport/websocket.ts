import { ClientConfigurationError } from "./errors";

export interface WebSocketTransportOptions {
  max_message_bytes?: number;
  protocols?: string | string[];
  on_message?: (data: string | ArrayBuffer | Blob) => void;
  on_close?: (event: CloseEvent) => void;
  on_protocol_error?: (code: string, detail: unknown) => void;
}

export interface WebSocketCloseState {
  code: number;
  reason: string;
  clean: boolean;
}

export class WebSocketTransport {
  private socket?: WebSocket;
  readonly max_message_bytes: number;
  close_state?: WebSocketCloseState;

  constructor(private readonly options: WebSocketTransportOptions = {}) {
    this.max_message_bytes = options.max_message_bytes ?? 1024 * 1024;
  }

  connect(url: string): WebSocket {
    if (!/^wss?:\/\//.test(url)) {
      throw new ClientConfigurationError("WebSocket URL must use ws:// or wss://");
    }
    this.close(1000, "replaced_connection");
    this.socket = new WebSocket(url, this.options.protocols);
    this.close_state = undefined;
    this.socket.addEventListener("message", this.onMessage);
    this.socket.addEventListener("close", this.onClose);
    return this.socket;
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    const size = typeof data === "string" ? new TextEncoder().encode(data).byteLength : data instanceof Blob ? data.size : data.byteLength;
    if (size > this.max_message_bytes) throw new RangeError("WebSocket message exceeds configured size limit");
    if (this.socket?.readyState !== WebSocket.OPEN) throw new Error("WebSocket is not open");
    this.socket.send(data);
  }

  close(code = 1000, reason = "normal"): void {
    this.socket?.removeEventListener("message", this.onMessage);
    this.socket?.removeEventListener("close", this.onClose);
    this.socket?.close(code, reason);
    this.socket = undefined;
  }

  private readonly onMessage = (event: MessageEvent<string | ArrayBuffer | Blob>) => {
    const size = messageBytes(event.data);
    if (size > this.max_message_bytes) {
      this.options.on_protocol_error?.("MESSAGE_TOO_LARGE", { size });
      this.socket?.close(1009, "message_too_large");
      return;
    }
    this.options.on_message?.(event.data);
  };

  private readonly onClose = (event: CloseEvent) => {
    this.close_state = {
      code: event.code,
      reason: event.reason || "unspecified",
      clean: event.wasClean,
    };
    this.options.on_close?.(event);
    this.socket = undefined;
  };
}

function messageBytes(data: string | ArrayBufferLike | Blob | ArrayBufferView): number {
  if (typeof data === "string") return new TextEncoder().encode(data).byteLength;
  if (data instanceof Blob) return data.size;
  return data.byteLength;
}
