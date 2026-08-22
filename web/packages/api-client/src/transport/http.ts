import type { ApiError as ApiErrorBody } from "../generated/contracts";
import { ApiError } from "./errors";

export interface HttpTransportOptions {
  base_url: string;
  fetch?: typeof fetch;
  locale?: () => "zh-CN" | "en-US";
  request_id?: () => string;
  csrf_token?: () => string | undefined | Promise<string | undefined>;
  on_authentication_invalidated?: (error: ApiError) => void;
}

export interface HttpRequestOptions extends Omit<RequestInit, "body"> {
  body?: unknown;
  csrf?: boolean;
}

function normalizeBaseUrl(base_url: string): URL {
  const browserOrigin = globalThis.location?.origin ?? "http://localhost";
  const url = new URL(base_url, browserOrigin);
  url.pathname = `${url.pathname.replace(/\/$/, "")}/api/v1/`;
  return url;
}

async function readError(response: Response): Promise<ApiErrorBody> {
  try {
    return (await response.json()) as ApiErrorBody;
  } catch {
    return {
      code: "HTTP_ERROR",
      message_key: "errors.http_error",
      message: response.statusText || `HTTP ${response.status}`,
      request_id: response.headers.get("x-request-id") ?? "unknown",
      retryable: response.status >= 500,
    };
  }
}

export class HttpTransport {
  readonly api_base_url: URL;
  private readonly fetch_impl: typeof fetch;

  constructor(private readonly options: HttpTransportOptions) {
    this.api_base_url = normalizeBaseUrl(options.base_url);
    this.fetch_impl = options.fetch ?? globalThis.fetch.bind(globalThis);
  }

  resolve(path: string): URL {
    return new URL(path.replace(/^\//, ""), this.api_base_url);
  }

  raw(path: string, init: RequestInit = {}): Promise<Response> {
    const headers = new Headers(init.headers);
    if (!headers.has("accept-language")) {
      headers.set("accept-language", this.options.locale?.() ?? "zh-CN");
    }
    if (!headers.has("x-request-id")) {
      headers.set(
        "x-request-id",
        this.options.request_id?.() ?? crypto.randomUUID(),
      );
    }
    return this.fetch_impl(this.resolve(path), {
      ...init,
      credentials: "include",
      headers,
    });
  }

  async request<T>(path: string, options: HttpRequestOptions = {}): Promise<T> {
    const headers = new Headers(options.headers);
    headers.set("accept", "application/json");
    headers.set("accept-language", this.options.locale?.() ?? "zh-CN");
    headers.set(
      "x-request-id",
      this.options.request_id?.() ?? crypto.randomUUID(),
    );

    if (options.body !== undefined)
      headers.set("content-type", "application/json");
    if (options.csrf) {
      const token = await this.options.csrf_token?.();
      if (!token) {
        throw new ApiError(
          {
            code: "CSRF_TOKEN_MISSING",
            message_key: "errors.csrf_token_missing",
            request_id: headers.get("x-request-id")!,
            retryable: false,
          },
          0,
        );
      }
      headers.set("x-csrf-token", token);
    }

    const response = await this.raw(path, {
      ...options,
      body:
        options.body === undefined ? undefined : JSON.stringify(options.body),
      headers,
    });
    if (!response.ok) {
      const error = new ApiError(await readError(response), response.status);
      if (
        error.code === "AUTHORIZATION_VERSION_STALE" ||
        error.code === "SESSION_EXPIRED" ||
        error.code === "SESSION_REVOKED" ||
        error.code === "ENTERPRISE_SUSPENDED" ||
        error.code === "ENTERPRISE_DISABLED"
      ) {
        this.options.on_authentication_invalidated?.(error);
      }
      throw error;
    }
    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }
}
