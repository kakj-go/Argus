import type { ArgusApiClient } from "./client";
import { createRealAdapter } from "./adapters/real";
import { ClientConfigurationError } from "./transport/errors";
import type { HttpTransportOptions } from "./transport/http";

export interface ApiClientFactoryOptions extends Omit<
  HttpTransportOptions,
  "base_url"
> {
  portal: "setup" | "platform" | "enterprise";
  mode: "mock" | "real" | string;
  base_url?: string;
  mock?: { initialized?: boolean };
}

export async function createConfiguredApiClient(
  options: ApiClientFactoryOptions,
): Promise<ArgusApiClient> {
  if (options.mode === "mock") {
    const { createMockApiClient } = await import("./mock/index.js");
    return createMockApiClient(options.mock);
  }
  if (options.mode === "real") {
    if (!options.base_url) {
      throw new ClientConfigurationError(
        "VITE_API_BASE_URL is required when VITE_API_MODE=real",
      );
    }
    return createRealAdapter({
      portal: options.portal,
      base_url: options.base_url,
      fetch: options.fetch,
      locale: options.locale,
      request_id: options.request_id,
      csrf_token: options.csrf_token,
      on_authentication_invalidated: options.on_authentication_invalidated,
    }).client;
  }
  throw new ClientConfigurationError(
    `Unsupported VITE_API_MODE: ${options.mode}`,
  );
}
