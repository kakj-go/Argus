export type ApiContext = {
  enterpriseId?: string;
  requestId?: string;
  locale?: "zh-CN" | "en-US";
};

export type ApiError = {
  code: string;
  message_key: string;
  params?: Record<string, string | number | boolean>;
  message?: string;
  requestId?: string;
};

export function createApiClient(baseUrl: string, context: ApiContext = {}) {
  return {
    async request<T>(path: string, init?: RequestInit): Promise<T> {
      const response = await fetch(new URL(path, baseUrl), {
        ...init,
        credentials: "include",
        headers: {
          "content-type": "application/json",
          ...(context.enterpriseId
            ? { "x-argus-enterprise": context.enterpriseId }
            : {}),
          "accept-language":
            context.locale ??
            (typeof document === "undefined"
              ? "zh-CN"
              : document.documentElement.lang || "zh-CN"),
          ...init?.headers,
        },
      });
      if (!response.ok) throw (await response.json()) as ApiError;
      return response.json() as Promise<T>;
    },
  };
}

export * from "./types";
export type { ArgusApiClient } from "./client";
export { createMockApiClient } from "./mock";
export type { MockApiClient, MockOptions } from "./mock";
export { ApiProvider, useApi } from "./react";
export type { ApiProviderProps } from "./react";
