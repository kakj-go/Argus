import type { StreamEventEnvelope } from "../generated/contracts";
import { ApiError, StreamTerminatedError } from "./errors";
import { HttpTransport } from "./http";

const NON_RETRYABLE = new Set([
  "CURSOR_EXPIRED",
  "STREAM_CURSOR_STALE",
  "AUTHORIZATION_VERSION_STALE",
]);

export interface SseFrame {
  id?: string;
  event?: string;
  data: string;
}

export interface SseStreamOptions {
  last_event_id?: string;
  signal?: AbortSignal;
  max_retries?: number;
  retry_delay_ms?: number;
}

export function parseSseBlock(block: string): SseFrame | null {
  let id: string | undefined;
  let event: string | undefined;
  const data: string[] = [];
  for (const line of block.split(/\r?\n/)) {
    if (line === "" || line.startsWith(":")) continue;
    const separator = line.indexOf(":");
    const field = separator < 0 ? line : line.slice(0, separator);
    let value = separator < 0 ? "" : line.slice(separator + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "id") id = value;
    else if (field === "event") event = value;
    else if (field === "data") data.push(value);
  }
  if (data.length === 0) return null;
  return { id, event, data: data.join("\n") };
}

export async function* decodeSse(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<SseFrame> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    for (;;) {
      const { done, value } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      const blocks = buffer.split(/\r?\n\r?\n/);
      buffer = blocks.pop() ?? "";
      for (const block of blocks) {
        const frame = parseSseBlock(block);
        if (frame) yield frame;
      }
      if (done) break;
    }
    const finalFrame = parseSseBlock(buffer);
    if (finalFrame) yield finalFrame;
  } finally {
    reader.releaseLock();
  }
}

export class SseTransport {
  constructor(private readonly http: HttpTransport) {}

  async *stream(
    path: string,
    options: SseStreamOptions = {},
  ): AsyncGenerator<StreamEventEnvelope> {
    const maxRetries = options.max_retries ?? 3;
    const retryDelay = options.retry_delay_ms ?? 250;
    const seen = new Set<string>();
    let lastEventId = options.last_event_id;
    let retries = 0;

    for (;;) {
      if (options.signal?.aborted) return;
      const headers = new Headers({ accept: "text/event-stream" });
      if (lastEventId) headers.set("last-event-id", lastEventId);
      try {
        const response = await this.http.raw(path, {
          headers,
          signal: options.signal,
        });
        if (!response.ok) {
          const body = (await response.json()) as ConstructorParameters<typeof ApiError>[0];
          if (NON_RETRYABLE.has(body.code)) {
            throw new StreamTerminatedError(body.code, body.message ?? body.message_key);
          }
          throw new ApiError(body, response.status);
        }
        if (!response.body) {
          throw new StreamTerminatedError(
            "STREAM_BODY_MISSING",
            "SSE response has no body",
          );
        }

        let terminal = false;
        for await (const frame of decodeSse(response.body)) {
          const envelope = JSON.parse(frame.data) as StreamEventEnvelope;
          const key = `${envelope.event_id}:${envelope.sequence}`;
          lastEventId = envelope.resume_cursor ?? frame.id ?? envelope.event_id;
          if (seen.has(key)) continue;
          seen.add(key);
          if (
            envelope.event_type === "authorization_invalidated" ||
            envelope.close_reason === "authorization_revoked" ||
            envelope.close_reason === "session_expired"
          ) {
            throw new StreamTerminatedError(
              "AUTHORIZATION_VERSION_STALE",
              "Stream authorization is no longer valid",
            );
          }
          yield envelope;
          if (envelope.terminal) {
            terminal = true;
            break;
          }
        }
        if (terminal) return;
      } catch (error) {
        if (options.signal?.aborted) return;
        if (error instanceof StreamTerminatedError) throw error;
        if (error instanceof ApiError && !error.retryable) throw error;
        if (retries >= maxRetries) {
          throw new StreamTerminatedError(
            "STREAM_DISCONNECTED",
            error instanceof Error ? error.message : "SSE stream disconnected",
          );
        }
      }
      retries += 1;
      await waitForRetry(retryDelay * 2 ** (retries - 1), options.signal);
    }
  }
}

function waitForRetry(delay: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const timer = globalThis.setTimeout(resolve, delay);
    signal?.addEventListener(
      "abort",
      () => {
        globalThis.clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });
}
